// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file is the Spec 035 watch reader: typed, REST GET-only reads that return
// response metadata (ETag, Link, rate-limit headers), treat 304 as a typed result,
// and classify failures to the watch-error codes. It sleeps never; callers decide
// when to poll again. The one-shot operations do not use it.

// Codes of the watch-error event (contracts/watch-events.md) produced by this layer.
const (
	CodeRateLimited  = "rate-limited"
	CodeNetwork      = "network"
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeHTTP5xx      = "http-5xx"
	CodeMalformed    = "malformed-response"
	CodeTooManyPRs   = "too-many-open-prs"
	CodePageTooLarge = "page-too-large"
	CodeRepoMoved    = "repo-moved"
)

// WatchError is the only error type of the watch reader. Message is fixed,
// bounded text: neither response bodies, headers nor the token reach it.
type WatchError struct {
	Code    string
	Message string
	// Status is the HTTP status when there was a response. GitHub reports an
	// inaccessible repository as 404; it is classified as CodeForbidden with
	// Status 404 so that callers can tell a deleted branch (branches/BASE) apart.
	Status int
	// NewName is the owner/name from the Location header of a refused 301, if it
	// names the repository (never followed).
	NewName string
	// RateLimit and RetryAfter carry the evidence of CodeRateLimited.
	RateLimit  RateInfo
	RetryAfter time.Duration
	cause      error
}

func (e *WatchError) Error() string { return "watch read failed: " + e.Code + ": " + e.Message }
func (e *WatchError) Unwrap() error { return e.cause }

// RateInfo is the primary rate limit state from X-RateLimit-* headers or rate_limit.
type RateInfo struct {
	Known     bool
	Limit     int
	Remaining int
	Reset     time.Time
}

// WatchKind groups requests for the per-poll call accounting.
type WatchKind string

const (
	KindList      WatchKind = "list"
	KindDetail    WatchKind = "detail"
	KindCommits   WatchKind = "commits"
	KindBranch    WatchKind = "branch"
	KindCommit    WatchKind = "commit"
	KindChecks    WatchKind = "checks"
	KindRateLimit WatchKind = "rate_limit"
)

// CallCount counts requests of one kind; NotModified is the subset answered 304.
type CallCount struct{ Requests, NotModified int }

// WatchResponse is one successful read. For a 304, NotModified is true, Body is
// empty and ETag is the validator that was sent, unchanged (weak prefix included).
type WatchResponse struct {
	Status      int
	NotModified bool
	ETag        string
	Link        string
	Body        []byte
	RateLimit   RateInfo
}

// WatchReader reads for `stw watch`. It is safe for concurrent use.
type WatchReader struct {
	api     *ghAPI
	timeout time.Duration
	mu      sync.Mutex
	calls   map[WatchKind]CallCount
	last    RateInfo
	// halved holds the repositories whose list pages exceeded the body limit at 25; in
	// memory only, so a restart tries 25 again.
	halved map[string]bool
}

func newWatchReader(api *ghAPI) *WatchReader {
	return &WatchReader{api: api, timeout: 30 * time.Second, calls: map[WatchKind]CallCount{}}
}

// NewWatchReader qualifies gh exactly like the one-shot operations and builds a
// client whose guard admits the watch endpoints. target is the absolute repository
// root that bounds the gh configuration check.
func NewWatchReader(ctx context.Context, target, host string) (*WatchReader, func(), error) {
	return NewWatchReaderWithTransport(ctx, target, host, nil)
}

// NewWatchReaderWithTransport is the in-process injection seam for fixtures.
func NewWatchReaderWithTransport(ctx context.Context, target, host string, rt http.RoundTripper) (*WatchReader, func(), error) {
	api, cleanup, err := newGHMode(ctx, target, host, rt, true)
	if err != nil {
		return nil, cleanup, err
	}
	return newWatchReader(api), cleanup, nil
}

// TakeCalls returns the per-kind request counts since the last call and resets them.
// Requests that cost rate limit are Requests - NotModified, except KindRateLimit
// which is free.
func (r *WatchReader) TakeCalls() map[WatchKind]CallCount {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.calls
	r.calls = map[WatchKind]CallCount{}
	return out
}

// LastRateLimit is the most recent rate-limit state seen in any response header.
func (r *WatchReader) LastRateLimit() RateInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

func (r *WatchReader) count(kind WatchKind, notModified bool) {
	r.mu.Lock()
	c := r.calls[kind]
	c.Requests++
	if notModified {
		c.NotModified++
	}
	r.calls[kind] = c
	r.mu.Unlock()
}

func (r *WatchReader) isHalved(repo string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.halved[repo]
}

func (r *WatchReader) markHalved(repo string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.halved == nil {
		r.halved = map[string]bool{}
	}
	r.halved[repo] = true
}

func malformed(msg string) *WatchError { return &WatchError{Code: CodeMalformed, Message: msg} }

func repoPath(repo string) (string, error) {
	if !validRepo(repo) {
		return "", malformed("invalid repository name")
	}
	return "repos/" + repo, nil
}

func validETag(s string) bool {
	if len(s) > 256 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func validSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}

// PullListPage reads one page of open PRs. perPage is 25 or 12; page is bounded by
// the allowlist (80 and 167).
func (r *WatchReader) PullListPage(ctx context.Context, repo string, perPage, page int, etag string) (WatchResponse, error) {
	base, err := repoPath(repo)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindList, base+"/pulls?state=open&per_page="+strconv.Itoa(perPage)+"&page="+strconv.Itoa(page), etag, true)
}

// PullDetail reads pulls/N.
func (r *WatchReader) PullDetail(ctx context.Context, repo string, number int, etag string) (WatchResponse, error) {
	base, err := repoPath(repo)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindDetail, base+"/pulls/"+strconv.Itoa(number), etag, false)
}

// PullCommits reads page 1..3 of pulls/N/commits (100 per page).
func (r *WatchReader) PullCommits(ctx context.Context, repo string, number, page int, etag string) (WatchResponse, error) {
	base, err := repoPath(repo)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindCommits, base+"/pulls/"+strconv.Itoa(number)+"/commits?per_page=100&page="+strconv.Itoa(page), etag, false)
}

// Branch reads branches/BASE; the name is encoded by EncodeBranchSegment.
func (r *WatchReader) Branch(ctx context.Context, repo, name, etag string) (WatchResponse, error) {
	base, err := repoPath(repo)
	if err != nil {
		return WatchResponse{}, err
	}
	seg, err := EncodeBranchSegment(name)
	if err != nil {
		return WatchResponse{}, malformed("invalid branch name")
	}
	return r.fetch(ctx, KindBranch, base+"/branches/"+seg, etag, false)
}

// Commit reads commits/SHA (40 lowercase hex).
func (r *WatchReader) Commit(ctx context.Context, repo, sha, etag string) (WatchResponse, error) {
	base, err := repoPath(repo)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindCommit, base+"/commits/"+sha, etag, false)
}

// CheckRuns reads commits/SHA/check-runs?per_page=100&page=N (page 1..10).
func (r *WatchReader) CheckRuns(ctx context.Context, repo, sha string, page int, etag string) (WatchResponse, error) {
	return r.checks(ctx, repo, sha, "check-runs", page, etag)
}

// Statuses reads commits/SHA/statuses?per_page=100&page=N (page 1..10).
func (r *WatchReader) Statuses(ctx context.Context, repo, sha string, page int, etag string) (WatchResponse, error) {
	return r.checks(ctx, repo, sha, "statuses", page, etag)
}

func (r *WatchReader) checks(ctx context.Context, repo, sha, which string, page int, etag string) (WatchResponse, error) {
	base, err := repoPath(repo)
	if err != nil {
		return WatchResponse{}, err
	}
	return r.fetch(ctx, KindChecks, base+"/commits/"+sha+"/"+which+"?per_page=100&page="+strconv.Itoa(page), etag, false)
}

// RateLimit reads rate_limit (free of rate limit) and returns the core resource.
func (r *WatchReader) RateLimit(ctx context.Context) (RateInfo, error) {
	res, err := r.fetch(ctx, KindRateLimit, "rate_limit", "", false)
	if err != nil {
		return RateInfo{}, err
	}
	var doc struct {
		Resources struct {
			Core struct {
				Limit, Remaining int
				Reset            int64
			} `json:"core"`
		} `json:"resources"`
	}
	if json.Unmarshal(res.Body, &doc) != nil || doc.Resources.Core.Limit <= 0 || doc.Resources.Core.Remaining < 0 || doc.Resources.Core.Reset <= 0 {
		return RateInfo{}, malformed("rate_limit response is not usable")
	}
	c := doc.Resources.Core
	info := RateInfo{Known: true, Limit: c.Limit, Remaining: c.Remaining, Reset: time.Unix(c.Reset, 0)}
	r.mu.Lock()
	r.last = info
	r.mu.Unlock()
	return info, nil
}

func rateFromHeader(h http.Header) RateInfo {
	limit, e1 := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, e2 := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	reset, e3 := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || limit <= 0 || remaining < 0 || reset <= 0 {
		return RateInfo{}
	}
	return RateInfo{Known: true, Limit: limit, Remaining: remaining, Reset: time.Unix(reset, 0)}
}

// fetch is the one conditional GET. endpoint is the escaped path and query without
// the API prefix; the guard re-validates it against the allowlist before any
// credential leaves the process.
func (r *WatchReader) fetch(parent context.Context, kind WatchKind, endpoint, etag string, list bool) (WatchResponse, error) {
	if !validETag(etag) {
		return WatchResponse{}, malformed("invalid ETag")
	}
	if !watchReadEndpoint(endpoint) && !transportReadEndpoint(endpoint) {
		return WatchResponse{}, malformed("endpoint is not an allowed watch read")
	}
	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()
	host, prefix := apiDestination(r.api.host)
	u, err := url.Parse("https://" + host + prefix + endpoint)
	if err != nil {
		return WatchResponse{}, malformed("cannot prepare watch read")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return WatchResponse{}, malformed("cannot prepare watch read")
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	res, err := r.api.client.Do(req)
	if err != nil {
		r.count(kind, false)
		return WatchResponse{}, r.requestFailure(ctx, res, err, list)
	}
	defer res.Body.Close()
	rate := rateFromHeader(res.Header)
	if rate.Known {
		r.mu.Lock()
		r.last = rate
		r.mu.Unlock()
	}
	if res.StatusCode == http.StatusNotModified {
		r.count(kind, true)
		if etag == "" {
			return WatchResponse{}, malformed("unsolicited 304 response")
		}
		out := WatchResponse{Status: 304, NotModified: true, ETag: etag, Link: res.Header.Get("Link"), RateLimit: rate}
		return out, nil
	}
	r.count(kind, false)
	if res.StatusCode != http.StatusOK {
		return WatchResponse{}, r.statusFailure(res, rate)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		if len(body) > maxBytes || errors.Is(err, errBodyLimit) {
			return WatchResponse{}, oversize(list)
		}
		return WatchResponse{}, r.requestFailure(ctx, nil, err, list)
	}
	etagOut := res.Header.Get("ETag")
	if !validETag(etagOut) {
		etagOut = ""
	}
	return WatchResponse{Status: 200, ETag: etagOut, Link: res.Header.Get("Link"), Body: body, RateLimit: rate}, nil
}

func oversize(list bool) *WatchError {
	if list {
		return &WatchError{Code: CodePageTooLarge, Message: "list page exceeds the one MiB body limit", cause: errBodyLimit}
	}
	return &WatchError{Code: CodeMalformed, Message: "response exceeds the one MiB body limit", cause: errBodyLimit}
}

func (r *WatchReader) requestFailure(ctx context.Context, res *http.Response, err error, list bool) error {
	if res != nil && res.Body != nil {
		res.Body.Close()
	}
	if res != nil && (res.StatusCode == http.StatusMovedPermanently || res.StatusCode == http.StatusPermanentRedirect) {
		return repoMoved(res, r.apiHost())
	}
	if res != nil && res.StatusCode >= 300 && res.StatusCode < 400 {
		return malformed("redirects are not followed")
	}
	if errors.Is(err, errBodyLimit) {
		return oversize(list)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return &WatchError{Code: CodeNetwork, Message: "request canceled or exceeded its deadline", cause: ctxErr}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &WatchError{Code: CodeNetwork, Message: "request canceled or exceeded its deadline", cause: err}
	}
	return &WatchError{Code: CodeNetwork, Message: "HTTP read failed"}
}

// repoMoved reports a refused 301. GitHub usually names the new repository by id
// (/repositories/ID/...); only a /repos/OWNER/NAME/ path on the same origin gives a name.
func repoMoved(res *http.Response, host string) *WatchError {
	e := &WatchError{Code: CodeRepoMoved, Status: res.StatusCode, Message: "repository moved; update the repository list (redirect not followed)"}
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil || (loc.Host != "" && loc.Host != host) {
		return e
	}
	parts := strings.Split(strings.TrimPrefix(loc.Path, "/"), "/")
	for i := 0; i+2 < len(parts) && i < 3; i++ {
		if parts[i] == "repos" && validRepo(parts[i+1]+"/"+parts[i+2]) {
			e.NewName = parts[i+1] + "/" + parts[i+2]
			e.Message = "repository moved to " + e.NewName + "; update the repository list (redirect not followed)"
			break
		}
	}
	return e
}

func (r *WatchReader) apiHost() string {
	host, _ := apiDestination(r.api.host)
	return host
}

func (r *WatchReader) statusFailure(res *http.Response, rate RateInfo) error {
	code := res.StatusCode
	if code == http.StatusMovedPermanently || code == http.StatusPermanentRedirect {
		return repoMoved(res, r.apiHost())
	}
	e := &WatchError{Status: code, RateLimit: rate}
	switch {
	case code == http.StatusUnauthorized:
		e.Code, e.Message = CodeUnauthorized, "authentication was refused"
	case code == http.StatusForbidden || code == http.StatusTooManyRequests:
		if code == http.StatusTooManyRequests || rateLimitEvidence(res, rate) {
			e.Code, e.Message = CodeRateLimited, "rate limit reached"
			e.RetryAfter = retryAfter(res.Header)
		} else {
			e.Code, e.Message = CodeForbidden, "access was refused"
		}
	case code == http.StatusNotFound:
		e.Code, e.Message = CodeForbidden, "resource not found or not accessible"
	case code >= 500:
		e.Code, e.Message = CodeHTTP5xx, "server error"
	case code >= 300 && code < 400:
		e.Code, e.Message = CodeMalformed, "redirects are not followed"
	default:
		e.Code, e.Message = CodeMalformed, "unexpected HTTP status"
	}
	return e
}

func retryAfter(h http.Header) time.Duration {
	if s, err := strconv.Atoi(h.Get("Retry-After")); err == nil && s >= 0 && s <= 24*3600 {
		return time.Duration(s) * time.Second
	}
	return 0
}

// rateLimitEvidence decides whether a 403 is a rate limit: exhausted primary
// remaining, a Retry-After header (secondary limit), or the documented message.
func rateLimitEvidence(res *http.Response, rate RateInfo) bool {
	if rate.Known && rate.Remaining == 0 || res.Header.Get("X-RateLimit-Remaining") == "0" || res.Header.Get("Retry-After") != "" {
		return true
	}
	text, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	lower := strings.ToLower(string(text))
	return strings.Contains(lower, "rate limit") || strings.Contains(lower, "abuse detection")
}

// nextPageFromLink returns the page number of the rel="next" entry of a Link header.
// The target URL itself is never requested: callers ask for the page number
// through the typed function, which re-validates it.
func nextPageFromLink(link string) (int, bool) {
	for _, entry := range strings.Split(link, ",") {
		entry = strings.TrimSpace(entry)
		if !strings.HasPrefix(entry, "<") {
			continue
		}
		end := strings.Index(entry, ">")
		if end < 0 {
			continue
		}
		next := false
		for _, param := range strings.Split(entry[end+1:], ";") {
			k, v, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "rel") {
				for _, rel := range strings.Fields(strings.Trim(strings.TrimSpace(v), `"`)) {
					if strings.EqualFold(rel, "next") {
						next = true
					}
				}
			}
		}
		if !next {
			continue
		}
		u, err := url.Parse(entry[1:end])
		if err != nil {
			return 0, false
		}
		page, err := strconv.Atoi(u.Query().Get("page"))
		if err != nil || page < 1 || page > 1000000 {
			return 0, false
		}
		return page, true
	}
	return 0, false
}

// splitRel returns the rel tokens of every entry of a Link header.
func splitRel(link string) []string {
	var rels []string
	for _, entry := range strings.Split(link, ",") {
		entry = strings.TrimSpace(entry)
		end := strings.Index(entry, ">")
		if !strings.HasPrefix(entry, "<") || end < 0 {
			continue
		}
		for _, param := range strings.Split(entry[end+1:], ";") {
			if k, v, ok := strings.Cut(strings.TrimSpace(param), "="); ok && strings.EqualFold(strings.TrimSpace(k), "rel") {
				rels = append(rels, strings.Fields(strings.ToLower(strings.Trim(strings.TrimSpace(v), `"`)))...)
			}
		}
	}
	return rels
}
