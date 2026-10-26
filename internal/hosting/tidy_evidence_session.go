// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/inspection"
)

const tidyMaxRequests = 128
const tidyMaxRawBytes int64 = 32 * 1024 * 1024

var (
	ErrTidyLimit       = errors.New("tidy hosting operation exceeded its request, byte or deadline budget")
	ErrTidyChanged     = errors.New("tidy hosting evidence changed; replan required")
	ErrTidyUnsupported = errors.New("tidy hosting proof is unsupported")
	// ErrTidyNotFound is a 404 or 422 answer; its text is the one every refused read always had.
	ErrTidyNotFound = errors.New("tidy hosting read unavailable; check repository access and rate limits")
	// ErrTidyResponseTooLarge is a response over the one MiB bound of a single read.
	ErrTidyResponseTooLarge = errors.New("tidy hosting response exceeded its size bound")
)

// TidySessionOptions describes explicit online selection. Workspace must first
// complete its native layout/base/config/protection preflight; opening a session
// additionally requalifies native ownership and selected remote before auth.
type TidySessionOptions struct {
	Online bool
	Apply  bool
	Remote string
	Repo   string
}

// TidyRepositoryID binds immutable identity and canonical provider name.
type TidyRepositoryID struct {
	ID       int64  `json:"id"`
	NodeID   string `json:"node_id"`
	FullName string `json:"full_name"`
}
type TidyRepositoryIdentity struct {
	Host string `json:"host"`
	TidyRepositoryID
	DefaultBranch string `json:"default_branch"`
}
type TidyHTTPBudget struct {
	Requests         int   `json:"requests"`
	RawBytes         int64 `json:"raw_bytes"`
	NetworkAttempted bool  `json:"network_attempted"`
}

type tidyHTTPLedger struct {
	mu        sync.Mutex
	stats     TidyHTTPBudget
	exhausted bool
}

func (b *tidyHTTPLedger) snapshot() TidyHTTPBudget { b.mu.Lock(); defer b.mu.Unlock(); return b.stats }
func (b *tidyHTTPLedger) request() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exhausted || b.stats.Requests >= tidyMaxRequests {
		b.exhausted = true
		return ErrTidyLimit
	}
	b.stats.Requests++
	b.stats.NetworkAttempted = true
	return nil
}
func (b *tidyHTTPLedger) allowance() (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.exhausted {
		return 0, ErrTidyLimit
	}
	return tidyMaxRawBytes - b.stats.RawBytes, nil
}
func (b *tidyHTTPLedger) consume(n int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stats.RawBytes += int64(n)
	if b.stats.RawBytes > tidyMaxRawBytes {
		b.exhausted = true
		return ErrTidyLimit
	}
	return nil
}

type tidyBudgetTransport struct {
	next   http.RoundTripper
	ledger *tidyHTTPLedger
}

func (t tidyBudgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Context().Err() != nil {
		return nil, ErrTidyLimit
	}
	if err := t.ledger.request(); err != nil {
		return nil, err
	}
	res, err := t.next.RoundTrip(req)
	if res != nil && res.Body != nil {
		res.Body = &tidyBudgetBody{ReadCloser: res.Body, ledger: t.ledger}
	}
	return res, err
}

type tidyBudgetBody struct {
	io.ReadCloser
	ledger *tidyHTTPLedger
}

func (b *tidyBudgetBody) Read(p []byte) (int, error) {
	remaining, err := b.ledger.allowance()
	if err != nil {
		return 0, err
	}
	if int64(len(p)) > remaining+1 {
		p = p[:remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	if limitErr := b.ledger.consume(n); limitErr != nil {
		return n, limitErr
	}
	return n, err
}

// TidySession exposes typed reads only. It owns one context, credential lifecycle
// and non-resettable ledger for discovery, repeated samples and apply boundaries.
// No error permits reusing a successful prefix as complete evidence.
type TidySession struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	cleanup    func()
	api        *ghAPI
	selected   Repository
	repository TidyRepositoryIdentity
	ledger     *tidyHTTPLedger
	closed     bool
}

type tidySessionDependencies struct {
	remotes func(context.Context, string) inspection.RemoteResult
	api     func(context.Context, string, string, http.RoundTripper) (*ghAPI, func(), error)
}

func OpenTidySession(ctx context.Context, path string, o TidySessionOptions) (*TidySession, error) {
	return OpenTidySessionWithTransport(ctx, path, o, nil)
}

// OpenTidySessionWithTransport retains native/auth qualification while permitting
// deterministic in-process provider fixtures; nil selects production HTTPS.
func OpenTidySessionWithTransport(ctx context.Context, path string, o TidySessionOptions, rt http.RoundTripper) (*TidySession, error) {
	return openTidySession(ctx, path, o, rt, tidySessionDependencies{
		remotes: func(ctx context.Context, path string) inspection.RemoteResult {
			return inspection.Remotes(ctx, inspection.Inspect(ctx, path))
		},
		api: newGHWithTransport,
	})
}
func openTidySession(parent context.Context, path string, o TidySessionOptions, rt http.RoundTripper, d tidySessionDependencies) (*TidySession, error) {
	timeout := 30 * time.Second
	if o.Apply {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	s := &TidySession{ctx: ctx, cancel: cancel, cleanup: func() {}, ledger: &tidyHTTPLedger{}}
	fail := func(err error) (*TidySession, error) { s.Close(); return s, err }
	if !o.Online || o.Remote == "" || (o.Repo != "" && !validRepo(o.Repo)) || ctx.Err() != nil {
		return fail(errors.New("tidy online selection and native preflight are required"))
	}
	remote := d.remotes(ctx, path)
	if remote.Status != "ok" || remote.Identity.Status != "ok" || remote.Identity.Backend != "git" || remote.Identity.Repository == nil || remote.Identity.Repository.ObjectFormat != "sha1" {
		return fail(ErrTidyUnsupported)
	}
	var selected *Candidate
	for _, m := range remote.Remotes {
		if m.Name != o.Remote {
			continue
		}
		if selected != nil || len(m.FetchURLs) != 1 {
			return fail(ErrTidyUnsupported)
		}
		candidate, err := parseRemote(m.Name, m.FetchURLs[0], "github.com")
		if err != nil || candidate == nil || (o.Repo != "" && !strings.EqualFold(candidate.Owner+"/"+candidate.Name, o.Repo)) || !tidyReadEndpoint("repos/"+candidate.Owner+"/"+candidate.Name) {
			return fail(ErrTidyUnsupported)
		}
		for _, raw := range m.PushURLs {
			push, err := parseRemote(m.Name, raw, "github.com")
			if err != nil || push == nil || !strings.EqualFold(push.Owner+"/"+push.Name, candidate.Owner+"/"+candidate.Name) {
				return fail(ErrTidyUnsupported)
			}
		}
		selected = candidate
	}
	if selected == nil {
		return fail(ErrTidyUnsupported)
	}
	s.selected = Repository{Host: "github.com", Owner: selected.Owner, Name: selected.Name, Selection: "explicit-remote"}
	target := remote.Identity.Repository.GitDir
	if remote.Identity.Repository.Worktree != nil {
		target = *remote.Identity.Repository.Worktree
	}
	if target == "" || ctx.Err() != nil {
		return fail(ErrTidyUnsupported)
	}
	if rt == nil {
		owned := newHTTPTransport()
		rt = owned
		s.cleanup = owned.CloseIdleConnections
	}
	api, cleanup, err := d.api(ctx, target, "github.com", tidyBudgetTransport{next: rt, ledger: s.ledger})
	if err != nil {
		return fail(ErrTidyAuthentication)
	}
	previous := s.cleanup
	s.cleanup = func() { cleanup(); previous() }
	s.api = api
	identity, err := s.readRepository(ctx)
	if err != nil {
		return fail(err)
	}
	s.repository = identity
	return s, nil
}
func (s *TidySession) Close() {
	// Cancellation must reach an in-flight read before waiting for its operation
	// lock; otherwise Close itself waits for the total operation deadline.
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.cleanup()
	}
}
func (s *TidySession) Budget() TidyHTTPBudget { return s.ledger.snapshot() }
func (s *TidySession) Repository() TidyRepositoryIdentity {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.repository
}
func (s *TidySession) operationContext(parent context.Context) (context.Context, func(), error) {
	if s.closed || s.ctx.Err() != nil || parent.Err() != nil {
		return nil, func() {}, ErrTidyLimit
	}
	ctx, cancel := context.WithCancel(s.ctx)
	stop := context.AfterFunc(parent, cancel)
	return ctx, func() { stop(); cancel() }, nil
}
func (s *TidySession) get(ctx context.Context, endpoint string) ([]byte, http.Header, error) {
	if ctx.Err() != nil {
		return nil, nil, ErrTidyLimit
	}
	if !tidyReadEndpoint(endpoint) && !tidyPRReadEndpoint(endpoint) && !tidyTipPullsEndpoint(endpoint) {
		return nil, nil, errors.New("invalid typed tidy read")
	}
	u := url.URL{Scheme: "https", Host: "api.github.com", Path: "/" + strings.SplitN(endpoint, "?", 2)[0]}
	if _, query, ok := strings.Cut(endpoint, "?"); ok {
		u.RawQuery = query
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, errors.New("cannot prepare tidy read")
	}
	res, err := s.api.client.Do(req)
	if err != nil {
		if errors.Is(err, errBodyLimit) {
			return nil, nil, ErrTidyResponseTooLarge
		}
		return nil, nil, s.readError(ctx)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if errors.Is(err, errBodyLimit) || len(raw) > maxBytes {
		return nil, nil, ErrTidyResponseTooLarge
	}
	if err != nil {
		return nil, nil, s.readError(ctx)
	}
	if ctx.Err() != nil {
		return nil, nil, ErrTidyLimit
	}
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusUnprocessableEntity {
		return nil, nil, ErrTidyNotFound
	}
	if res.StatusCode != http.StatusOK {
		return nil, nil, errors.New("tidy hosting read unavailable; check repository access and rate limits")
	}
	if !utf8.Valid(raw) || uniqueReleaseJSON(raw) != nil {
		return nil, nil, errors.New("invalid or duplicate tidy provider JSON")
	}
	return raw, res.Header, nil
}
func (s *TidySession) readError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ErrTidyLimit
	}
	if _, err := s.ledger.allowance(); err != nil {
		return err
	}
	return errors.New("tidy hosting response unavailable or exceeded its bounds")
}
func tidyPRReadEndpoint(endpoint string) bool {
	return fixedEndpoint.MatchString(endpoint) && !strings.Contains(endpoint, "/commits/")
}
func (s *TidySession) readRepository(ctx context.Context) (TidyRepositoryIdentity, error) {
	raw, _, err := s.get(ctx, "repos/"+s.selected.Owner+"/"+s.selected.Name)
	if err != nil {
		return TidyRepositoryIdentity{}, err
	}
	var value struct {
		ID            *int64  `json:"id"`
		NodeID        *string `json:"node_id"`
		FullName      *string `json:"full_name"`
		DefaultBranch *string `json:"default_branch"`
	}
	if decodeTidyJSON(raw, &value) != nil || value.ID == nil || *value.ID <= 0 || value.NodeID == nil || !tidyAtom(*value.NodeID, 256) || value.FullName == nil || !validRepo(*value.FullName) || !strings.EqualFold(*value.FullName, s.selected.Owner+"/"+s.selected.Name) || value.DefaultBranch == nil || !tidyBranchName(*value.DefaultBranch) {
		return TidyRepositoryIdentity{}, errors.New("repository identity is missing or invalid")
	}
	return TidyRepositoryIdentity{Host: "github.com", TidyRepositoryID: TidyRepositoryID{ID: *value.ID, NodeID: *value.NodeID, FullName: *value.FullName}, DefaultBranch: *value.DefaultBranch}, nil
}
func (s *TidySession) revalidateRepository(ctx context.Context) error {
	identity, err := s.readRepository(ctx)
	if err != nil {
		return err
	}
	if identity != s.repository {
		return ErrTidyChanged
	}
	return nil
}
func (s *TidySession) RevalidateRepository(parent context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel, err := s.operationContext(parent)
	defer cancel()
	if err != nil {
		return err
	}
	return s.revalidateRepository(ctx)
}
func tidyAtom(s string, limit int) bool {
	return s != "" && len(s) <= limit && utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return r <= 32 || r == 127 })
}
func tidyBranchName(name string) bool {
	if !tidyAtom(name, 4096) || name == "@" || strings.ContainsAny(name, "~^:?*[\\") || strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}
func tidyResultBound(value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maxBytes {
		return ErrTidyLimit
	}
	return nil
}

// tidyPagination validates all links but generates the next request locally.
// No Link means a short page is terminal; a full page needs another bounded
// request unless an explicit, internally consistent last-page link proves it.
func tidyPagination(header http.Header, endpoint string, page, count int, lastPage *int) (bool, error) {
	values := header.Values("Link")
	links := map[string]int{}
	path, query, _ := strings.Cut(endpoint, "?")
	expectedQuery, err := url.ParseQuery(query)
	if err != nil {
		return false, ErrTidyUnsupported
	}
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if !strings.HasPrefix(part, "<") {
				return false, errors.New("malformed tidy pagination link")
			}
			target, tail, ok := strings.Cut(part[1:], ">")
			if !ok {
				return false, errors.New("malformed tidy pagination link")
			}
			tail = strings.TrimSpace(tail)
			if !strings.HasPrefix(tail, `; rel="`) || !strings.HasSuffix(tail, `"`) {
				return false, errors.New("unsupported tidy pagination parameters")
			}
			rel := strings.TrimSuffix(strings.TrimPrefix(tail, `; rel="`), `"`)
			if rel != "next" && rel != "prev" && rel != "first" && rel != "last" {
				return false, errors.New("unsupported tidy pagination relation")
			}
			if _, exists := links[rel]; exists {
				return false, errors.New("duplicate tidy pagination relation")
			}
			u, err := url.Parse(target)
			if err != nil || u.Scheme != "https" || u.Host != "api.github.com" || u.Path != "/"+path || u.User != nil || u.RawPath != "" || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
				return false, errors.New("tidy pagination destination refused")
			}
			q, err := url.ParseQuery(u.RawQuery)
			if err != nil || len(q) != len(expectedQuery) {
				return false, errors.New("tidy pagination query refused")
			}
			for key, want := range expectedQuery {
				if key != "page" && (len(q[key]) != 1 || len(want) != 1 || q[key][0] != want[0]) {
					return false, errors.New("tidy pagination query refused")
				}
			}
			var n int
			if len(q["page"]) != 1 {
				return false, errors.New("tidy pagination page refused")
			}
			if _, err = fmt.Sscanf(q["page"][0], "%d", &n); err != nil || fmt.Sprint(n) != q["page"][0] || n < 1 || n > maxPages {
				return false, ErrTidyLimit
			}
			links[rel] = n
		}
	}
	if n, ok := links["next"]; ok && n != page+1 {
		return false, errors.New("tidy pagination did not advance exactly one page")
	}
	if n, ok := links["prev"]; ok && n != page-1 {
		return false, errors.New("tidy previous page changed")
	}
	if n, ok := links["first"]; ok && n != 1 {
		return false, errors.New("tidy first page changed")
	}
	if n, ok := links["last"]; ok && (n < page || (links["next"] != 0 && n < page+1)) {
		return false, errors.New("tidy last page changed")
	}
	if n := links["last"]; n != 0 {
		if *lastPage != 0 && *lastPage != n {
			return false, errors.New("tidy last-page total changed during pagination")
		}
		*lastPage = n
	}
	if *lastPage != 0 && (page > *lastPage || (count < 100 && page != *lastPage)) {
		return false, errors.New("tidy collection ended before its declared last page")
	}
	if count < 100 {
		if links["next"] != 0 || (links["last"] != 0 && links["last"] != page) {
			return false, errors.New("tidy short page conflicts with pagination")
		}
		return true, nil
	}
	if *lastPage == page && links["next"] == 0 {
		return true, nil
	}
	if page == maxPages {
		return false, ErrTidyLimit
	}
	return false, nil
}
