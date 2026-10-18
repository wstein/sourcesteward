// SPDX-License-Identifier: EUPL-1.2
package hosting

// These readers collect observations only. Policy predicates and deployment
// acceptance belong to later operations; a readable activation is not trusted
// merely because its environment has a particular name.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type readyOutcome string

const (
	readyOK                readyOutcome = "ok"
	readyAbsentProven      readyOutcome = "absent-proven"
	readyDenied            readyOutcome = "denied"
	readyNotFoundAmbiguous readyOutcome = "not-found-ambiguous"
	readyUnsupported       readyOutcome = "unsupported"
	readyMalformed         readyOutcome = "malformed"
	readyLimit             readyOutcome = "limit"
	readyTransport         readyOutcome = "transport"
	readyCanceled          readyOutcome = "canceled"
)

type readySource struct {
	Host       string
	Name       string
	Repository string
	Selector   string
	Outcome    readyOutcome
	Complete   bool
	Requests   int
}
type readyEvidence[T any] struct {
	Source readySource
	Value  T
}

// One operation shares this budget with its separately pinned policy repository.
// Readers are sequential; they are not safe for concurrent use.
type readyReadBudget struct {
	started                      time.Time
	requests, ciRows, statusRows int
}
type readySources struct {
	ctx    context.Context
	api    *ghAPI
	repo   Repository
	budget *readyReadBudget
}

func newReadySources(ctx context.Context, api *ghAPI, repo Repository) *readySources {
	return &readySources{ctx: ctx, api: api, repo: repo, budget: &readyReadBudget{started: time.Now()}}
}
func (s *readySources) forRepository(repo Repository) *readySources {
	return &readySources{ctx: s.ctx, api: s.api, repo: repo, budget: s.budget}
}
func (s *readySources) source(name, selector string) readySource {
	return readySource{Host: s.repo.Host, Name: name, Repository: s.repo.Owner + "/" + s.repo.Name, Selector: selector, Outcome: readyOK}
}
func (s *readySources) prefix() string { return "repos/" + s.repo.Owner + "/" + s.repo.Name }
func finishReady[T any](src readySource, value T, valid bool) readyEvidence[T] {
	if src.Outcome == readyOK {
		if valid {
			normalized, err := json.Marshal(value)
			if err != nil {
				src.Outcome = readyMalformed
			} else if len(normalized) > maxBytes {
				src.Outcome = readyLimit
			} else {
				src.Complete = true
			}
		} else {
			src.Outcome = readyMalformed
		}
	}
	if !src.Complete {
		var zero T
		value = zero
	}
	return readyEvidence[T]{Source: src, Value: value}
}

var readySegment = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var readyEnvironment = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]{0,99}$`)

func readyBaseRef(ref string) bool {
	if len(ref) == 0 || len(ref) > 200 || strings.Contains(ref, "..") {
		return false
	}
	for _, part := range strings.Split(ref, "/") {
		if !readySegment.MatchString(part) || strings.HasPrefix(part, ".") || strings.HasPrefix(part, "-") || strings.HasSuffix(part, ".lock") || part == "protection" {
			return false
		}
	}
	return true
}

// readyEndpoint is separate from fixedEndpoint: legacy builders retain their
// exact allowlist, query order and decoding semantics.
func readyEndpoint(endpoint string) bool {
	path, query, hasQuery := strings.Cut(endpoint, "?")
	if hasQuery && query == "" {
		return false
	}
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "repos" || !validRepo(parts[1]+"/"+parts[2]) {
		return false
	}
	tail := strings.Join(parts[3:], "/")
	if tail == "" {
		return len(parts) == 3 && query == ""
	}
	pageQuery := regexp.MustCompile(`^per_page=100&page=([1-9]|10)$`)
	switch {
	case tail == "pulls":
		return regexp.MustCompile(`^state=open&per_page=100&page=([1-9]|10)$`).MatchString(query)
	case strings.HasPrefix(tail, "pulls/"):
		p := strings.Split(tail, "/")
		if len(p) < 2 || !readyPositive(p[1]) {
			return false
		}
		return (len(p) == 2 && query == "") || (len(p) == 3 && p[2] == "files" && pageQuery.MatchString(query))
	case strings.HasPrefix(tail, "branches/"):
		ref := strings.TrimPrefix(tail, "branches/")
		ref = strings.TrimSuffix(ref, "/protection")
		return query == "" && readyBaseRef(ref)
	case strings.HasPrefix(tail, "rules/branches/"):
		return readyBaseRef(strings.TrimPrefix(tail, "rules/branches/")) && pageQuery.MatchString(query)
	case strings.HasPrefix(tail, "compare/"):
		refs := strings.Split(strings.TrimPrefix(tail, "compare/"), "...")
		return len(refs) == 2 && fullSHA(refs[0]) && fullSHA(refs[1]) && query == "per_page=1&page=2"
	case strings.HasPrefix(tail, "commits/"):
		p := strings.Split(tail, "/")
		if len(p) != 3 || !fullSHA(p[1]) {
			return false
		}
		return p[2] == "statuses" && pageQuery.MatchString(query) || p[2] == "check-runs" && strings.HasPrefix(query, "filter=all&") && pageQuery.MatchString(strings.TrimPrefix(query, "filter=all&"))
	case strings.HasPrefix(tail, "issues/comments/"):
		return readyPositive(strings.TrimPrefix(tail, "issues/comments/")) && query == ""
	case tail == "contents/.stw/ready-policy.json":
		return strings.HasPrefix(query, "ref=") && fullSHA(strings.TrimPrefix(query, "ref="))
	case strings.HasPrefix(tail, "environments/"):
		p := strings.Split(tail, "/")
		return len(p) == 4 && readyEnvironment.MatchString(p[1]) && p[2] == "variables" && p[3] == "STW_READY_BASELINE" && query == ""
	}
	return false
}
func readyPositive(v string) bool {
	if v == "" || v[0] == '0' {
		return false
	}
	n, e := strconv.ParseInt(v, 10, 64)
	return e == nil && n > 0 && strconv.FormatInt(n, 10) == v
}

// Parent cancellation takes precedence over the operation's own limits.
func (s *readySources) stopped(ctx context.Context) readyOutcome {
	if s.ctx.Err() != nil {
		return readyCanceled
	}
	if ctx.Err() != nil || time.Since(s.budget.started) >= 30*time.Second {
		return readyLimit
	}
	return readyOK
}
func (s *readySources) get(src *readySource, endpoint string) []byte {
	if s.ctx.Err() != nil {
		src.Outcome = readyCanceled
		return nil
	}
	if s.budget.requests >= 80 || time.Since(s.budget.started) >= 30*time.Second {
		src.Outcome = readyLimit
		return nil
	}
	if s.api == nil || s.api.client == nil || !validHost(s.api.host) || !validRepo(s.repo.Owner+"/"+s.repo.Name) || s.repo.Host != s.api.host || !readyEndpoint(endpoint) {
		src.Outcome = readyMalformed
		return nil
	}
	host, prefix := apiDestination(s.api.host)
	path, query, _ := strings.Cut(endpoint, "?")
	u := url.URL{Scheme: "https", Host: host, Path: prefix + path, RawQuery: query}
	ctx, cancel := context.WithDeadline(s.ctx, s.budget.started.Add(30*time.Second))
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		src.Outcome = readyMalformed
		return nil
	}
	s.budget.requests++
	src.Requests++
	res, err := s.api.client.Do(req)
	if outcome := s.stopped(ctx); outcome != readyOK {
		if res != nil && res.Body != nil {
			res.Body.Close()
		}
		src.Outcome = outcome
		return nil
	}
	if err != nil {
		src.Outcome = readyTransport
		if errors.Is(err, errBodyLimit) {
			src.Outcome = readyLimit
		}
		return nil
	}
	defer res.Body.Close()
	headerBytes := 0
	for name, values := range res.Header {
		for _, value := range values {
			headerBytes += len(name) + len(value) + 4
		}
	}
	if headerBytes > 32*1024 {
		src.Outcome = readyLimit
		return nil
	}
	// Error bodies are never decoded, retained or included in diagnostics.
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403:
		src.Outcome = readyDenied
		return nil
	case res.StatusCode == 404:
		src.Outcome = readyNotFoundAmbiguous
		return nil
	case res.StatusCode == 422 || res.StatusCode == 415:
		src.Outcome = readyUnsupported
		return nil
	case res.StatusCode != 200:
		src.Outcome = readyTransport
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if outcome := s.stopped(ctx); outcome != readyOK {
		src.Outcome = outcome
		return nil
	}
	if errors.Is(err, errBodyLimit) || len(raw) > maxBytes {
		src.Outcome = readyLimit
		return nil
	}
	if err != nil {
		src.Outcome = readyTransport
		return nil
	}
	if !readyJSON(raw) {
		src.Outcome = readyMalformed
		return nil
	}
	return raw
}

// Check JSON before typed decoding: invalid UTF-8, duplicate keys, excessive
// nesting and large strings must not disappear through permissive decoding.
func readyJSON(raw []byte) bool {
	if !utf8.Valid(raw) || len(raw) == 0 || bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int, string) bool
	walk = func(depth int, key string) bool {
		tok, e := d.Token()
		if e != nil {
			return false
		}
		switch v := tok.(type) {
		case json.Delim:
			if depth >= 16 {
				return false
			}
			switch v {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return false
					}
					name, ok := k.(string)
					if !ok || len(name) > 2048 || seen[name] {
						return false
					}
					seen[name] = true
					if !walk(depth+1, name) {
						return false
					}
				}
				end, e := d.Token()
				return e == nil && end == json.Delim('}')
			case '[':
				for d.More() {
					if !walk(depth+1, key) {
						return false
					}
				}
				end, e := d.Token()
				return e == nil && end == json.Delim(']')
			}
			return false
		case string:
			limit := 2048
			switch key {
			case "body", "summary":
				limit = 64 * 1024
			case "content":
				limit = 90 * 1024
			case "value":
				limit = 16 * 1024
			}
			return len(v) <= limit
		}
		return true
	}
	if !walk(0, "") {
		return false
	}
	_, e := d.Token()
	return e == io.EOF
}

type readyObject map[string]json.RawMessage

func object(raw []byte) readyObject {
	var v readyObject
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}
func (o readyObject) str(k string) (string, bool) {
	var s string
	v, ok := o[k]
	e := json.Unmarshal(v, &s)
	return s, ok && string(v) != "null" && e == nil
}
func (o readyObject) integer(k string) (int64, bool) {
	var n int64
	v, ok := o[k]
	e := json.Unmarshal(v, &n)
	return n, ok && string(v) != "null" && e == nil
}
func (o readyObject) boolean(k string) (bool, bool) {
	var b bool
	v, ok := o[k]
	e := json.Unmarshal(v, &b)
	return b, ok && string(v) != "null" && e == nil
}
func (o readyObject) nullableBool(k string) (*bool, bool) {
	var b *bool
	v, ok := o[k]
	e := json.Unmarshal(v, &b)
	return b, ok && e == nil
}
func (o readyObject) nullableString(k string) (*string, bool) {
	var s *string
	v, ok := o[k]
	e := json.Unmarshal(v, &s)
	return s, ok && e == nil
}
func readyTime(v string) bool { _, e := time.Parse(time.RFC3339, v); return e == nil }

type readyRef struct {
	SHA, Ref, Repository string
	RepositoryID         *int64
}
type readyPullRequest struct {
	ID                    int64
	Number                int
	State                 string
	Merged, Draft         bool
	Head, Base            readyRef
	ChangedFiles          int
	Mergeable, Rebaseable *bool
	MergeableState        string
}

func decodeReadyRef(raw []byte, nullableRepo bool) (readyRef, bool) {
	o := object(raw)
	sha, a := o.str("sha")
	ref, b := o.str("ref")
	r := readyRef{SHA: sha, Ref: ref}
	if !a || !b || !fullSHA(sha) || ref == "" {
		return r, false
	}
	repo, exists := o["repo"]
	if !exists {
		return r, false
	}
	if string(repo) == "null" {
		return r, nullableRepo
	}
	ro := object(repo)
	id, c := ro.integer("id")
	name, d := ro.str("full_name")
	r.Repository = name
	r.RepositoryID = &id
	return r, c && d && id > 0 && validRepo(name)
}
func decodeReadyPR(raw []byte, full bool) (readyPullRequest, bool) {
	o := object(raw)
	id, a := o.integer("id")
	n, b := o.integer("number")
	state, c := o.str("state")
	draft, d := o.boolean("draft")
	head, e := decodeReadyRef(o["head"], true)
	base, f := decodeReadyRef(o["base"], false)
	p := readyPullRequest{ID: id, Number: int(n), State: state, Draft: draft, Head: head, Base: base}
	valid := a && b && c && d && e && f && id > 0 && n > 0 && (state == "open" || state == "closed")
	if full {
		merged, g := o.boolean("merged")
		count, h := o.integer("changed_files")
		merge, i := o.nullableBool("mergeable")
		rebase, j := o.nullableBool("rebaseable")
		ms, k := o.str("mergeable_state")
		p.Merged = merged
		p.ChangedFiles = int(count)
		p.Mergeable = merge
		p.Rebaseable = rebase
		p.MergeableState = ms
		valid = valid && g && h && i && j && k && count >= 0
	}
	return p, valid
}
func (s *readySources) pullRequest(number int) readyEvidence[readyPullRequest] {
	src := s.source("pr", strconv.Itoa(number))
	if number <= 0 {
		src.Outcome = readyMalformed
		return finishReady(src, readyPullRequest{}, false)
	}
	raw := s.get(&src, fmt.Sprintf("%s/pulls/%d", s.prefix(), number))
	v, ok := decodeReadyPR(raw, true)
	ok = ok && v.Number == number && strings.EqualFold(v.Base.Repository, src.Repository)
	return finishReady(src, v, ok)
}

type readyRepository struct {
	ID                                   int64
	FullName, DefaultBranch              string
	AllowRebase, AllowMerge, AllowSquash bool
}

func (s *readySources) repository() readyEvidence[readyRepository] {
	src := s.source("repository", "")
	o := object(s.get(&src, s.prefix()))
	id, a := o.integer("id")
	name, b := o.str("full_name")
	branch, c := o.str("default_branch")
	rebase, d := o.boolean("allow_rebase_merge")
	merge, e := o.boolean("allow_merge_commit")
	squash, f := o.boolean("allow_squash_merge")
	return finishReady(src, readyRepository{id, name, branch, rebase, merge, squash}, a && b && c && d && e && f && id > 0 && strings.EqualFold(name, src.Repository) && branch != "")
}

type readyBranch struct {
	Name, SHA string
	Protected bool
}

func (s *readySources) branch(ref string) readyEvidence[readyBranch] {
	src := s.source("base-branch", ref)
	if !readyBaseRef(ref) {
		src.Outcome = readyUnsupported
		return finishReady(src, readyBranch{}, false)
	}
	o := object(s.get(&src, s.prefix()+"/branches/"+ref))
	name, a := o.str("name")
	sha, b := object(o["commit"]).str("sha")
	protected, c := o.boolean("protected")
	return finishReady(src, readyBranch{name, sha, protected}, a && b && c && name == ref && fullSHA(sha))
}

// Constraint documents retain all fields for the later closed supported-subset
// evaluator. A successful read alone makes no claim that a rule is supported.
func (s *readySources) protection(ref string) readyEvidence[readyObject] {
	src := s.source("classic-protection", ref)
	if !readyBaseRef(ref) {
		src.Outcome = readyUnsupported
		return finishReady(src, readyObject(nil), false)
	}
	o := object(s.get(&src, s.prefix()+"/branches/"+ref+"/protection"))
	return finishReady(src, o, o != nil && len(o) > 0)
}

type readyRule struct {
	Type               string
	RulesetID          int64
	SourceType, Source string
	Parameters         json.RawMessage
	Raw                readyObject
}

func (s *readySources) rules(ref string) readyEvidence[[]readyRule] {
	src := s.source("effective-rules", ref)
	if !readyBaseRef(ref) {
		src.Outcome = readyUnsupported
		return finishReady(src, []readyRule(nil), false)
	}
	rows := collectReady(s, &src, func(page int) string {
		return fmt.Sprintf("%s/rules/branches/%s?per_page=100&page=%d", s.prefix(), ref, page)
	}, 100, func(raw json.RawMessage) (readyRule, string, bool) {
		o := object(raw)
		kind, a := o.str("type")
		id, b := o.integer("ruleset_id")
		st, c := o.str("ruleset_source_type")
		source, d := o.str("ruleset_source")
		return readyRule{kind, id, st, source, o["parameters"], o}, fmt.Sprintf("%d/%s", id, kind), a && b && c && d && kind != "" && id > 0
	})
	return finishReady(src, rows, true)
}

type readyCompare struct {
	BaseSHA, MergeBaseSHA, Status   string
	AheadBy, BehindBy, TotalCommits int64
}

func (s *readySources) compare(base, head string) readyEvidence[readyCompare] {
	src := s.source("compare", base+"..."+head)
	if !fullSHA(base) || !fullSHA(head) {
		src.Outcome = readyMalformed
		return finishReady(src, readyCompare{}, false)
	}
	o := object(s.get(&src, s.prefix()+"/compare/"+base+"..."+head+"?per_page=1&page=2"))
	b, a := object(o["base_commit"]).str("sha")
	m, c := object(o["merge_base_commit"]).str("sha")
	status, d := o.str("status")
	ahead, e := o.integer("ahead_by")
	behind, f := o.integer("behind_by")
	total, g := o.integer("total_commits")
	return finishReady(src, readyCompare{b, m, status, ahead, behind, total}, a && c && d && e && f && g && b == base && fullSHA(m) && ahead >= 0 && behind >= 0 && total >= 0)
}

func collectReady[T any](s *readySources, src *readySource, endpoint func(int) string, limit int, decode func(json.RawMessage) (T, string, bool)) []T {
	rows := []T{}
	normalizedBytes := 0
	seen := map[string]bool{}
	for page := 1; page <= maxPages; page++ {
		raw := s.get(src, endpoint(page))
		if src.Outcome != readyOK {
			return nil
		}
		var data []json.RawMessage
		if json.Unmarshal(raw, &data) != nil || data == nil || len(data) > 100 {
			src.Outcome = readyMalformed
			return nil
		}
		for _, raw := range data {
			normalizedBytes += len(raw)
			if normalizedBytes > maxBytes {
				src.Outcome = readyLimit
				return nil
			}
			v, id, ok := decode(raw)
			if !ok || seen[id] {
				src.Outcome = readyMalformed
				return nil
			}
			seen[id] = true
			if len(rows) >= limit {
				src.Outcome = readyLimit
				return nil
			}
			rows = append(rows, v)
		}
		if len(data) < 100 {
			return rows
		}
	}
	src.Outcome = readyLimit
	return nil
}

type readyCheck struct {
	ID, AppID             int64
	Name, Status, HeadSHA string
	Conclusion, Summary   *string
}

func (s *readySources) checks(sha string) readyEvidence[[]readyCheck] {
	src := s.source("checks", sha)
	if !fullSHA(sha) {
		src.Outcome = readyMalformed
		return finishReady(src, []readyCheck(nil), false)
	}
	rows := []readyCheck{}
	seen := map[int64]bool{}
	normalizedBytes := 0
	total := int64(-1)
	for page := 1; page <= maxPages; page++ {
		o := object(s.get(&src, fmt.Sprintf("%s/commits/%s/check-runs?filter=all&per_page=100&page=%d", s.prefix(), sha, page)))
		if src.Outcome != readyOK {
			break
		}
		count, a := o.integer("total_count")
		var data []json.RawMessage
		e := json.Unmarshal(o["check_runs"], &data)
		if !a || count < 0 || e != nil || data == nil || len(data) > 100 || total >= 0 && total != count {
			src.Outcome = readyMalformed
			break
		}
		if count > 1000 {
			src.Outcome = readyLimit
			break
		}
		total = count
		for _, raw := range data {
			normalizedBytes += len(raw)
			if normalizedBytes > maxBytes {
				src.Outcome = readyLimit
				break
			}
			o := object(raw)
			id, a := o.integer("id")
			app, b := object(o["app"]).integer("id")
			name, c := o.str("name")
			status, d := o.str("status")
			head, e := o.str("head_sha")
			conclusion, f := o.nullableString("conclusion")
			summary, g := object(o["output"]).nullableString("summary")
			if !a || !b || !c || !d || !e || !f || !g || id <= 0 || app <= 0 || head != sha || seen[id] {
				src.Outcome = readyMalformed
				break
			}
			seen[id] = true
			rows = append(rows, readyCheck{id, app, name, status, head, conclusion, summary})
		}
		if src.Outcome != readyOK {
			break
		}
		if int64(len(rows)) > total || len(data) < 100 && int64(len(rows)) != total {
			src.Outcome = readyMalformed
			break
		}
		if int64(len(rows)) == total {
			if s.budget.statusRows+len(rows) > 1000 {
				src.Outcome = readyLimit
				break
			}
			s.budget.ciRows = len(rows)
			return finishReady(src, rows, true)
		}
		if page == maxPages {
			src.Outcome = readyLimit
		}
	}
	return finishReady(src, rows, false)
}

type readyStatus struct {
	ID, CreatorID                        int64
	Context, State, CreatedAt, UpdatedAt string
	TargetURL, Description               *string
}

func (s *readySources) statuses(sha string) readyEvidence[[]readyStatus] {
	src := s.source("full-statuses", sha)
	if !fullSHA(sha) {
		src.Outcome = readyMalformed
		return finishReady(src, []readyStatus(nil), false)
	}
	rows := collectReady(s, &src, func(page int) string {
		return fmt.Sprintf("%s/commits/%s/statuses?per_page=100&page=%d", s.prefix(), sha, page)
	}, 1000-s.budget.ciRows, func(raw json.RawMessage) (readyStatus, string, bool) {
		o := object(raw)
		id, a := o.integer("id")
		creator, b := object(o["creator"]).integer("id")
		context, c := o.str("context")
		state, d := o.str("state")
		created, e := o.str("created_at")
		updated, f := o.str("updated_at")
		target, g := o.nullableString("target_url")
		description, h := o.nullableString("description")
		return readyStatus{id, creator, context, state, created, updated, target, description}, strconv.FormatInt(id, 10), a && b && c && d && e && f && g && h && id > 0 && creator > 0 && context != "" && readyTime(created) && readyTime(updated)
	})
	if src.Outcome == readyOK {
		s.budget.statusRows = len(rows)
	}
	return finishReady(src, rows, true)
}
func (s *readySources) openPullRequests() readyEvidence[[]readyPullRequest] {
	src := s.source("open-prs", "")
	rows := collectReady(s, &src, func(page int) string {
		return fmt.Sprintf("%s/pulls?state=open&per_page=100&page=%d", s.prefix(), page)
	}, 1000, func(raw json.RawMessage) (readyPullRequest, string, bool) {
		v, ok := decodeReadyPR(raw, false)
		return v, strconv.FormatInt(v.ID, 10), ok && v.State == "open" && strings.EqualFold(v.Base.Repository, src.Repository)
	})
	return finishReady(src, rows, true)
}

type readyFile struct {
	Filename, Status string
	PreviousFilename *string
}

func (s *readySources) files(number, count int) readyEvidence[[]readyFile] {
	src := s.source("files", strconv.Itoa(number))
	if count < 0 || number <= 0 {
		src.Outcome = readyMalformed
		return finishReady(src, []readyFile(nil), false)
	}
	if count > 1000 {
		src.Outcome = readyLimit
		return finishReady(src, []readyFile(nil), false)
	}
	rows := collectReady(s, &src, func(page int) string {
		return fmt.Sprintf("%s/pulls/%d/files?per_page=100&page=%d", s.prefix(), number, page)
	}, count, func(raw json.RawMessage) (readyFile, string, bool) {
		o := object(raw)
		name, a := o.str("filename")
		status, b := o.str("status")
		previous, c := o.nullableString("previous_filename")
		if _, exists := o["previous_filename"]; !exists {
			c = true
		}
		return readyFile{name, status, previous}, name, a && b && c && name != "" && status != "" && (status != "renamed" || previous != nil && *previous != "")
	})
	return finishReady(src, rows, len(rows) == count)
}

type readyReport struct {
	ID, AuthorID              int64
	Body, IssueURL, UpdatedAt string
	Raw                       []byte
	Digest                    string
}

func (s *readySources) report(id int64, number int) readyEvidence[readyReport] {
	src := s.source("report", strconv.FormatInt(id, 10))
	if id <= 0 || number <= 0 {
		src.Outcome = readyMalformed
		return finishReady(src, readyReport{}, false)
	}
	raw := s.get(&src, fmt.Sprintf("%s/issues/comments/%d", s.prefix(), id))
	if src.Outcome != readyOK {
		return finishReady(src, readyReport{}, false)
	}
	o := object(raw)
	got, a := o.integer("id")
	author, b := object(o["user"]).integer("id")
	body, c := o.str("body")
	issue, d := o.str("issue_url")
	updated, e := o.str("updated_at")
	host, prefix := apiDestination(s.api.host)
	expected := "https://" + host + prefix + s.prefix() + "/issues/" + strconv.Itoa(number)
	sum := sha256.Sum256([]byte(body))
	return finishReady(src, readyReport{got, author, body, issue, updated, []byte(body), hex.EncodeToString(sum[:])}, a && b && c && d && e && got == id && author > 0 && issue == expected && readyTime(updated) && len(body) <= 64*1024)
}

type readyDocument struct {
	Raw    []byte
	Digest string
}

func document(raw []byte) readyDocument {
	sum := sha256.Sum256(raw)
	return readyDocument{raw, hex.EncodeToString(sum[:])}
}
func (s *readySources) policy(sha string) readyEvidence[readyDocument] {
	src := s.source("policy", sha)
	if !fullSHA(sha) {
		src.Outcome = readyMalformed
		return finishReady(src, readyDocument{}, false)
	}
	o := object(s.get(&src, s.prefix()+"/contents/.stw/ready-policy.json?ref="+sha))
	kind, a := o.str("type")
	encoding, b := o.str("encoding")
	size, c := o.integer("size")
	content, d := o.str("content")
	if c && size > 64*1024 {
		src.Outcome = readyLimit
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(strings.ReplaceAll(content, "\n", ""))
	return finishReady(src, document(raw), a && b && c && d && kind == "file" && encoding == "base64" && size >= 0 && size <= 64*1024 && e == nil && int64(len(raw)) == size && readyJSON(raw))
}

type readyActivationChannel struct {
	Profile, Environment string
	Accepted             bool
}

func (s *readySources) activation(channel readyActivationChannel) readyEvidence[readyDocument] {
	src := s.source("activation", channel.Environment)
	if !channel.Accepted || channel.Profile != "app" || !readyEnvironment.MatchString(channel.Environment) {
		src.Outcome = readyUnsupported
		return finishReady(src, readyDocument{}, false)
	}
	o := object(s.get(&src, s.prefix()+"/environments/"+channel.Environment+"/variables/STW_READY_BASELINE"))
	name, a := o.str("name")
	value, b := o.str("value")
	return finishReady(src, document([]byte(value)), a && b && name == "STW_READY_BASELINE" && len(value) <= 16*1024 && readyJSON([]byte(value)))
}
