// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/watchexport"
)

// ---- the http-reader scenario format ----

type hRepeat struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// hRoute is one URI of the fake forge (path and query without the API prefix).
type hRoute struct {
	URI                string            `json:"uri"`
	Status             int               `json:"status"`
	Headers            map[string]string `json:"headers"`
	Body               string            `json:"body"`
	BodyRepeat         *hRepeat          `json:"body_repeat"`
	ContentLength      int               `json:"declared_content_length"` // 0: not set
	ETag               string            `json:"etag"`
	NotModifiedOnMatch bool              `json:"not_modified_on_matching_validator"`
	EchoAuth           bool              `json:"echo_authorization_header"`
}

func (r hRoute) body() string {
	if r.BodyRepeat != nil {
		return strings.Repeat(r.BodyRepeat.Text, r.BodyRepeat.Count)
	}
	return r.Body
}

func (f *fakeForge) apply(r hRoute) {
	status := r.Status
	if status == 0 {
		status = 200
	}
	hdr := r.Headers
	if hdr == nil {
		hdr = map[string]string{}
	}
	if r.NotModifiedOnMatch {
		f.serve(r.URI, r.ETag, r.body(), hdr)
		return
	}
	f.handlers[r.URI] = func(req *http.Request) *http.Response {
		h := map[string]string{}
		for k, v := range hdr {
			h[k] = v
		}
		if r.ETag != "" {
			h["ETag"] = r.ETag
		}
		body := r.body()
		if r.EchoAuth {
			body = "echo " + req.Header.Get("Authorization")
		}
		res := forgeResponse(status, body, h)
		if r.ContentLength > 0 {
			res.ContentLength = int64(r.ContentLength)
		}
		return res
	}
}

type hCall struct {
	Op           string   `json:"op"`
	Repo         string   `json:"repo"`
	Number       int      `json:"number"`
	Page         int      `json:"page"`
	PerPage      int      `json:"per_page"`
	Name         string   `json:"name"`
	SHA          string   `json:"sha"`
	ETag         string   `json:"if_none_match"`
	MaxOpen      int      `json:"max_open_prs"`
	Prior        string   `json:"prior_list_state"` // none, previous, per_page_only
	PriorPerPage int      `json:"prior_per_page"`
	NewReader    bool     `json:"new_reader_before"`
	Halved       bool     `json:"reader_already_halved"`
	SetRoutes    []hRoute `json:"set_routes"`
	Note         string   `json:"note"`
}

type hRate struct {
	Known     bool  `json:"known"`
	Limit     int   `json:"limit"`
	Remaining int   `json:"remaining"`
	ResetUnix int64 `json:"reset_unix"`
}

type hErr struct {
	Code               string `json:"code"`
	Status             int    `json:"status"`
	NewName            string `json:"new_name"`
	RetryAfterSeconds  int    `json:"retry_after_seconds"`
	Rate               *hRate `json:"rate_limit"`
	MessageLeaksSecret bool   `json:"message_leaks_secret_or_body"`
	MessageBounded     bool   `json:"message_at_most_200_bytes"`
	Canceled           bool   `json:"canceled"`
}

type hResp struct {
	Status      int    `json:"status"`
	NotModified bool   `json:"not_modified"`
	ETag        string `json:"etag"`
	Link        string `json:"link"`
	Body        string `json:"body"`
	Rate        hRate  `json:"rate_limit"`
}

type hPage struct {
	ETag    string `json:"etag"`
	HasLink bool   `json:"has_link"`
	Items   int    `json:"items"`
}

type hList struct {
	Items     int       `json:"items"`
	Numbers   []int     `json:"numbers"`
	Unchanged bool      `json:"unchanged"`
	PerPage   int       `json:"per_page"`
	Pages     []hPage   `json:"pages"`
	First     *PullItem `json:"first_item"`
}

type hReq struct {
	URI         string `json:"uri"`
	IfNoneMatch string `json:"if_none_match"`
}

type hOut struct {
	Call     int    `json:"call"`
	Error    *hErr  `json:"error"`
	Response *hResp `json:"response"`
	List     *hList `json:"list"`
	Rate     *hRate `json:"rate_info"`
	Requests []hReq `json:"requests"`
}

type hDoc struct {
	watchexport.Common
	Host     string   `json:"host"`
	Routes   []hRoute `json:"routes"`
	Calls    []hCall  `json:"calls"`
	Expected []hOut   `json:"expected"`
}

type hx struct {
	t     *testing.T
	doc   *hDoc
	f     *fakeForge
	r     *WatchReader
	state PullListState
}

func newHX(t *testing.T, name, desc string, tests ...string) *hx {
	x := &hx{t: t, f: newFakeForge(t)}
	x.doc = &hDoc{Common: watchexport.NewCommon(name, watchexport.KindHTTP, desc, tests...), Host: "github.com", Routes: []hRoute{}, Calls: []hCall{}, Expected: []hOut{}}
	x.r = x.f.reader("github.com")
	return x
}

func (x *hx) route(r hRoute) *hx {
	x.doc.Routes = append(x.doc.Routes, r)
	x.f.apply(r)
	return x
}

func (x *hx) serve(uri, etag, body string, header map[string]string) *hx {
	return x.route(hRoute{URI: uri, ETag: etag, Body: body, Headers: header, NotModifiedOnMatch: true})
}

func (x *hx) fixed(uri string, status int, body string, header map[string]string) *hx {
	return x.route(hRoute{URI: uri, Status: status, Body: body, Headers: header})
}

func rateOf(i RateInfo) hRate {
	return hRate{Known: i.Known, Limit: i.Limit, Remaining: i.Remaining, ResetUnix: func() int64 {
		if i.Reset.IsZero() {
			return 0
		}
		return i.Reset.Unix()
	}()}
}

func errOf(err error) *hErr {
	var we *WatchError
	if !errors.As(err, &we) {
		return &hErr{Code: "other"}
	}
	e := &hErr{Code: we.Code, Status: we.Status, NewName: we.NewName, RetryAfterSeconds: int(we.RetryAfter / time.Second),
		MessageBounded: len(we.Message) <= 200, Canceled: errors.Is(err, context.Canceled)}
	if we.RateLimit.Known {
		r := rateOf(we.RateLimit)
		e.Rate = &r
	}
	for _, secret := range []string{"fixturetoken", "Bearer", "token ", "Bad credentials", "secondary", "xxxx", "secret", "evil"} {
		if strings.Contains(err.Error(), secret) || strings.Contains(we.Message, secret) {
			e.MessageLeaksSecret = true
		}
	}
	return e
}

func (x *hx) do(c hCall) *hOut {
	x.t.Helper()
	for _, r := range c.SetRoutes {
		x.doc.Routes = append(x.doc.Routes, r)
		x.f.apply(r)
	}
	if c.NewReader {
		x.r = x.f.reader("github.com")
	}
	if c.Halved {
		x.r.markHalved(c.Repo)
	}
	before := len(x.f.log)
	ctx := context.Background()
	out := hOut{Call: len(x.doc.Calls) + 1, Requests: []hReq{}}
	var resp WatchResponse
	var err error
	switch c.Op {
	case "pull_detail":
		resp, err = x.r.PullDetail(ctx, c.Repo, c.Number, c.ETag)
	case "pull_list_page":
		resp, err = x.r.PullListPage(ctx, c.Repo, c.PerPage, c.Page, c.ETag)
	case "pull_commits":
		resp, err = x.r.PullCommits(ctx, c.Repo, c.Number, c.Page, c.ETag)
	case "branch":
		resp, err = x.r.Branch(ctx, c.Repo, c.Name, c.ETag)
	case "commit":
		resp, err = x.r.Commit(ctx, c.Repo, c.SHA, c.ETag)
	case "check_runs":
		resp, err = x.r.CheckRuns(ctx, c.Repo, c.SHA, c.Page, c.ETag)
	case "statuses":
		resp, err = x.r.Statuses(ctx, c.Repo, c.SHA, c.Page, c.ETag)
	case "rate_limit":
		var info RateInfo
		info, err = x.r.RateLimit(ctx)
		if err == nil {
			r := rateOf(info)
			out.Rate = &r
		}
	case "list_open_pulls":
		prior := PullListState{}
		switch c.Prior {
		case "previous":
			prior = x.state
		case "per_page_only":
			prior = PullListState{PerPage: c.PriorPerPage}
		}
		var res PullListResult
		res, err = x.r.ListOpenPulls(ctx, c.Repo, prior, c.MaxOpen)
		if err == nil {
			x.state = res.State
			l := &hList{Items: len(res.Items), Numbers: []int{}, Unchanged: res.Unchanged, PerPage: res.State.PerPage, Pages: []hPage{}}
			for _, it := range res.Items {
				l.Numbers = append(l.Numbers, it.Number)
			}
			if len(res.Items) > 0 {
				first := res.Items[0]
				l.First = &first
			}
			for _, p := range res.State.Pages {
				l.Pages = append(l.Pages, hPage{ETag: p.ETag, HasLink: p.Link != "", Items: len(p.Items)})
			}
			out.List = l
		}
	default:
		x.t.Fatalf("op %q", c.Op)
	}
	if err != nil {
		out.Error = errOf(err)
	} else if c.Op != "rate_limit" && c.Op != "list_open_pulls" {
		out.Response = &hResp{Status: resp.Status, NotModified: resp.NotModified, ETag: resp.ETag, Link: resp.Link, Body: string(resp.Body), Rate: rateOf(resp.RateLimit)}
	}
	for i := before; i < len(x.f.log); i++ {
		out.Requests = append(out.Requests, hReq{x.f.log[i], x.f.etags[i]})
	}
	x.doc.Calls = append(x.doc.Calls, c)
	x.doc.Expected = append(x.doc.Expected, out)
	return &x.doc.Expected[len(x.doc.Expected)-1]
}

func (x *hx) must(cond bool, msg string) {
	x.t.Helper()
	if !cond {
		x.t.Fatalf("%s: translation guard: %s", x.doc.Name, msg)
	}
}

func (x *hx) write(dir string) {
	if err := watchexport.Write(dir, x.doc.Name, x.doc); err != nil {
		x.t.Fatal(err)
	}
}

func listCall(max int, prior string) hCall {
	return hCall{Op: "list_open_pulls", Repo: "a/b", MaxOpen: max, Prior: prior}
}

func TestExportScenarios(t *testing.T) {
	dir, err := watchexport.OutDir()
	if err != nil {
		t.Skip("STW_EXPORT_DIR not set: " + err.Error())
	}
	exportHostingVectors(t, dir)
	exportReaderScenarios(t, dir)
	exportPullsScenarios(t, dir)
}

func put(t *testing.T, dir string, v *watchexport.VectorDoc) {
	t.Helper()
	if err := watchexport.Write(dir, v.Name, v); err != nil {
		t.Fatal(err)
	}
}

func exportHostingVectors(t *testing.T, dir string) {
	{
		v := watchexport.NewVectors("branch-segment-encode", "encode_branch_segment", "A branch name as one URL path segment: `/` becomes %2F, non-ASCII is percent-encoded; names git cannot use (empty, `..`, leading `-`, control characters, `@{`, lock suffix, a `%` ...) and names over 255 bytes are refused (the PR is then observed without the base tip read).", nil,
			"watch_branch_test.go:TestEncodeBranchSegment", "watch_branch_test.go:TestEncodeBranchSegmentRefusesInvalidNames")
		long := strings.Repeat("a", 256)
		for _, in := range []string{"main", "feat/x", "release/v1.2", "feature/ünï", "日本", "", "..", "a..b", "-x", "a@{b", "a/", "/a", "a//b", "x.lock", "a/x.lock", "a\x00b", "a\nb", "a\x7fb", "a b", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", "a\\b", "a%2Fb", "%252F", "\xff", ".hidden", "a/.b", "a.", "@", long, strings.Repeat("a", 255)} {
			got, err := EncodeBranchSegment(in)
			name := in
			if len(name) > 40 {
				name = fmt.Sprintf("%d bytes", len(in))
			}
			v.Add(fmt.Sprintf("%q", name), map[string]string{"name": in}, map[string]any{"ok": err == nil, "segment": got})
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("branch-segment-guard", "branch_segment_valid", "The raw path segment the transport accepts for a branch: only what EncodeBranchSegment produces (an uppercase %2F, percent-encoded UTF-8); double encoding, dot segments, control characters, a literal slash and lowercase escapes are refused.", nil,
			"watch_branch_test.go:TestBranchSegmentGuardRefusesAttackStrings")
		for _, seg := range []string{"..%2F", "%252F", "%2e%2e", "%2E%2E", "%00", "%5C", "%5c", "feat%2fx", "feat/x", "feat%2Fx%2F", "a%20b", "ü", "", "%", "%zz", "%2e", "a%2Eb%2E", "feat%252Fx", "x%0Ay", "%7F", "main", "feat%2Fx", "release%2Fv1.2", "%C3%BC", "feature%2F%C3%BCn%C3%AF"} {
			v.Add(fmt.Sprintf("%q", seg), map[string]string{"segment": seg}, map[string]bool{"valid": branchSegmentValid(seg)})
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("rate-limit-assess", "assess_rate_limit", "The slowdown policy: none; slow (remaining below 10 percent of the limit: double the interval); wait (remaining below 50 or exhausted: wait until reset + 5 s).", nil,
			"watch_ratelimit_test.go:TestAssessRateLimit")
		reset := time.Unix(1700000000, 0)
		for _, k := range []struct {
			name string
			in   RateInfo
		}{{"unknown", RateInfo{}}, {"plenty", RateInfo{Known: true, Limit: 5000, Remaining: 4000, Reset: reset}}, {"exactly 10 percent", RateInfo{Known: true, Limit: 5000, Remaining: 500, Reset: reset}},
			{"below 10 percent", RateInfo{Known: true, Limit: 5000, Remaining: 499, Reset: reset}}, {"49 left", RateInfo{Known: true, Limit: 5000, Remaining: 49, Reset: reset}},
			{"50 left is slow, not wait", RateInfo{Known: true, Limit: 5000, Remaining: 50, Reset: reset}}, {"exhausted", RateInfo{Known: true, Limit: 60, Remaining: 0, Reset: reset}},
			{"limit 60, 5 left (below 10 percent, below 50)", RateInfo{Known: true, Limit: 60, Remaining: 5, Reset: reset}}} {
			d := AssessRateLimit(k.in)
			advice := map[RateAdvice]string{AdviceNone: "none", AdviceSlow: "slow", AdviceWait: "wait"}[d.Advice]
			until := int64(0)
			if !d.Until.IsZero() {
				until = d.Until.Unix()
			}
			v.Add(k.name, rateOf(k.in), map[string]any{"advice": advice, "until_unix": until})
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("rate-limited-wait-until", "rate_limited_until", "Until when a rate-limited answer holds the run: retry-after plus 5 s, else reset plus 5 s (only if in the future), else 65 s from now.", nil,
			"watch_ratelimit_test.go:TestRateLimitedWaitUntil")
		now := time.Unix(1699999000, 0)
		reset := time.Unix(1700000000, 0)
		type in struct {
			NowUnix           int64 `json:"now_unix"`
			RetryAfterSeconds int   `json:"retry_after_seconds"`
			Rate              hRate `json:"rate_limit"`
		}
		for _, k := range []struct {
			name string
			e    *WatchError
		}{{"retry-after", &WatchError{Code: CodeRateLimited, RetryAfter: 60 * time.Second, RateLimit: RateInfo{Known: true, Reset: reset}}},
			{"reset", &WatchError{Code: CodeRateLimited, RateLimit: RateInfo{Known: true, Reset: reset}}},
			{"stale reset in the past", &WatchError{Code: CodeRateLimited, RateLimit: RateInfo{Known: true, Reset: now.Add(-time.Hour)}}},
			{"nothing known", &WatchError{Code: CodeRateLimited}}} {
			v.Add(k.name, in{now.Unix(), int(k.e.RetryAfter / time.Second), rateOf(k.e.RateLimit)}, map[string]int64{"until_unix": RateLimitedUntil(k.e, now).Unix()})
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("link-header-next-page", "next_page_from_link", "The page number of the `rel=\"next\"` entry of a Link header (rel with or without quotes); missing, malformed, zero and overflowing numbers give nothing.", nil,
			"watch_reader_test.go:TestWatchLinkHeaderParsing")
		for _, link := range []string{
			`<https://api.github.com/repositories/1/pulls?state=open&per_page=25&page=2>; rel="next", <https://api.github.com/repositories/1/pulls?state=open&per_page=25&page=4>; rel="last"`,
			`<https://api.github.com/repos/a/b/pulls?page=3&state=open&per_page=25>; rel="next"`,
			`<https://api.github.com/repos/a/b/pulls?state=open&per_page=25&page=4>; rel="last", <https://api.github.com/repos/a/b/pulls?state=open&per_page=25&page=1>; rel="first"`,
			``, `<https://api.github.com/x?page=abc>; rel="next"`, `<https://api.github.com/x?page=0>; rel="next"`, `<https://api.github.com/x?page=99999999999999999999>; rel="next"`,
			`garbage; rel="next"`, `<https://api.github.com/x?page=2>; rel="prev"`, `<https://api.github.com/x?page=2>; rel=next`} {
			n, ok := nextPageFromLink(link)
			v.Add(link, map[string]string{"link": link}, map[string]any{"found": ok, "page": n})
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("retry-after-header", "parse_retry_after", "The Retry-After header in seconds: 0 to 86400 accepted, anything else (negative, over a day, garbage, empty) is no evidence.", nil,
			"watch_reader_test.go:TestWatchRetryAfterUpperBound")
		for _, h := range []string{"0", "30", "86400", "86401", "-1", "abc", ""} {
			hd := http.Header{}
			hd.Set("Retry-After", h)
			v.Add(fmt.Sprintf("%q", h), map[string]string{"header": h}, map[string]int{"seconds": int(retryAfter(hd) / time.Second)})
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("rate-limit-headers", "parse_rate_headers", "The X-RateLimit-* headers: a negative remaining is not evidence (unknown); zero remaining is.", nil,
			"watch_reader_test.go:TestWatchNegativeRateRemainingIsUnknown")
		for _, rem := range []string{"-1", "0", "4990"} {
			hd := http.Header{}
			hd.Set("X-RateLimit-Limit", "5000")
			hd.Set("X-RateLimit-Remaining", rem)
			hd.Set("X-RateLimit-Reset", "1700000000")
			v.Add("remaining "+rem, map[string]string{"limit": "5000", "remaining": rem, "reset": "1700000000"}, rateOf(rateFromHeader(hd)))
		}
		put(t, dir, v)
	}
	{
		v := watchexport.NewVectors("watch-endpoint-allowlist", "watch_read_endpoint_allowed", "The GET endpoints the watch reader may call (pulls list with page bounds 80 at 25 and 167 at 12, pulls/N, its commits up to page 3, branches with an encoded segment, commits/SHA with check-runs and statuses, rate_limit) and what it refuses. Only GET exists (the Go guard refuses every write method on every endpoint; Rust must have no write path).", nil,
			"watch_endpoint_test.go:TestWatchEndpointAllowlist", "watch_ratelimit_test.go:TestWatchGuardRefusesEveryWriteMethodOnEveryWatchEndpoint")
		sha := "0123456789abcdef0123456789abcdef01234567"
		for _, ep := range append(watchGoodEndpoints(), "repos/a/b/pulls?state=open&per_page=25&page=81", "repos/a/b/pulls?state=open&per_page=12&page=168", "repos/a/b/pulls?state=open&per_page=25&page=0",
			"repos/a/b/pulls?state=open&per_page=100&page=1", "repos/a/b/pulls?state=closed&per_page=25&page=1", "repos/a/b/pulls?state=open&per_page=25&page=1&x=1", "repos/a/b/pulls/7/commits?per_page=100&page=4",
			"repos/a/b/pulls/0/commits?per_page=100&page=1", "repos/a/b/pulls/7/commits?per_page=30&page=1", "repos/a/b/branches/feat/x", "repos/a/b/branches/", "repos/a/b/branches/feat%2fx",
			"repos/a/b/branches/..%2F", "repos/a/b/branches/%252F", "repos/a/b/branches/%2e%2e", "repos/a/b/branches/%00", "repos/a/b/branches/%5C", "repos/a/b/branches/main?x=1", "repos/a/b/commits/abc",
			"repos/a/b/commits/"+strings.ToUpper(sha), "repos/a/b/commits/"+sha+"/comments", "repos/../b/commits/"+sha, "repos/a/../commits/"+sha, "repos/a/../../rate_limit", "repos/a/b/branches/main/../../x",
			"rate_limit?x=1", "rate_limit/", "user", "repos/a/b/git/refs") {
			v.Add(ep, map[string]string{"endpoint": ep, "method": "GET"}, map[string]bool{"allowed": watchReadEndpoint(ep) || transportReadEndpoint(ep)})
		}
		put(t, dir, v)
	}
}

func exportReaderScenarios(t *testing.T, dir string) {
	{
		x := newHX(t, "http-conditional-round-trip", "A GET with and without a validator: a 200 carries ETag, body and the rate-limit headers, a request without a validator sends no If-None-Match; a 304 is a typed result (not an error) with no body; the accounting counts 2 requests and 1 not modified.",
			"watch_reader_test.go:TestWatchConditionalRoundTripAndNotModified")
		x.serve("repos/a/b/pulls/7", `"v1"`, `{"number":7}`, map[string]string{"X-RateLimit-Limit": "5000", "X-RateLimit-Remaining": "4990", "X-RateLimit-Reset": "1700000000"})
		o := x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: 7})
		x.must(o.Response.Status == 200 && o.Response.ETag == `"v1"` && o.Requests[0].IfNoneMatch == "" && o.Response.Rate.Remaining == 4990, "first")
		o = x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: 7, ETag: `"v1"`})
		x.must(o.Response.NotModified && o.Response.Status == 304 && o.Response.ETag == `"v1"` && o.Response.Body == "" && o.Requests[0].IfNoneMatch == `"v1"`, "304")
		x.write(dir)
	}
	{
		x := newHX(t, "http-unsolicited-304-is-malformed", "A 304 without a validator in the request is a malformed response.", "watch_reader_test.go:TestWatchUnsolicited304IsMalformed")
		x.fixed("repos/a/b/pulls/7", 304, "", nil)
		o := x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: 7})
		x.must(o.Error != nil && o.Error.Code == CodeMalformed, "malformed")
		x.write(dir)
	}
	{
		x := newHX(t, "http-error-classification", "HTTP status and body to error code: 401 unauthorized; 403 forbidden (404 too, with its status kept); a 403 with rate-limit evidence (remaining 0, Retry-After, or the secondary-limit text) and 429 rate-limited; 5xx http-5xx; any other success status and a body over 1 MiB malformed. The error text never carries the response body or a token and is bounded.",
			"watch_reader_test.go:TestWatchErrorClassification")
		cases := []struct {
			code   int
			body   string
			header map[string]string
		}{
			{401, `{"message":"Bad credentials"}`, nil}, {403, `{"message":"Resource not accessible"}`, nil},
			{403, `{"message":"API rate limit exceeded"}`, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1700000100"}},
			{403, `{"message":"You have exceeded a secondary rate limit."}`, map[string]string{"Retry-After": "60"}}, {403, `{"message":"You have exceeded a secondary rate limit"}`, nil},
			{429, `{}`, nil}, {404, `{"message":"Not Found"}`, nil}, {500, `oops`, nil}, {503, `oops`, nil}, {201, `{}`, nil},
		}
		want := []string{CodeUnauthorized, CodeForbidden, CodeRateLimited, CodeRateLimited, CodeRateLimited, CodeRateLimited, CodeForbidden, CodeHTTP5xx, CodeHTTP5xx, CodeMalformed, CodeMalformed}
		for i, c := range cases {
			x.fixed(fmt.Sprintf("repos/a/b/pulls/%d", i+1), c.code, c.body, c.header)
		}
		x.route(hRoute{URI: "repos/a/b/pulls/11", Status: 200, BodyRepeat: &hRepeat{"x", maxBytes + 1}})
		for i := 1; i <= 11; i++ {
			o := x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: i})
			x.must(o.Error != nil && o.Error.Code == want[i-1] && !o.Error.MessageLeaksSecret && o.Error.MessageBounded, fmt.Sprintf("case %d: %+v", i, o.Error))
		}
		x.write(dir)
	}
	{
		x := newHX(t, "http-rate-limited-carries-reset-and-retry-after", "A rate-limited error carries the Retry-After seconds and the reset time from the headers.", "watch_reader_test.go:TestWatchRateLimitedCarriesResetAndRetryAfter")
		x.fixed("repos/a/b/pulls/7", 403, `{"message":"secondary rate limit"}`, map[string]string{"Retry-After": "90", "X-RateLimit-Remaining": "12", "X-RateLimit-Limit": "5000", "X-RateLimit-Reset": "1700000100"})
		o := x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: 7})
		x.must(o.Error.Code == CodeRateLimited && o.Error.RetryAfterSeconds == 90 && o.Error.Rate.ResetUnix == 1700000100, "carried")
		x.write(dir)
	}
	{
		x := newHX(t, "http-negative-remaining-is-no-rate-limit-evidence", "A 403 whose only rate evidence is a negative remaining is forbidden, not rate-limited.", "watch_reader_test.go:TestWatchNegativeRateRemainingIsUnknown")
		x.fixed("repos/a/b/pulls/7", 403, `{"message":"Resource not accessible"}`, map[string]string{"X-RateLimit-Limit": "5000", "X-RateLimit-Remaining": "-1", "X-RateLimit-Reset": "1700000000"})
		o := x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: 7})
		x.must(o.Error.Code == CodeForbidden, "forbidden")
		x.write(dir)
	}
	{
		x := newHX(t, "http-redirects-refused-and-repo-moved", "A 301 is never followed: repo-moved, with the new owner/name from the Location only if it names the repository safely (a hostile name, another host, an id URL or no Location give no name); any other redirect is malformed; the body of the redirect never leaks.",
			"watch_reader_test.go:TestWatchRedirectsRefusedAndRepoMoved")
		cases := []struct {
			location, want, newName string
			status                  int
		}{
			{"https://api.github.com/repos/new/name/pulls/%d", CodeRepoMoved, "new/name", 301}, {"https://api.github.com/repositories/12345/pulls/%d", CodeRepoMoved, "", 301},
			{"https://api.github.com/repos/../x/pulls/%d", CodeRepoMoved, "", 301}, {"https://evil.test/repos/new/name/pulls/%d", CodeRepoMoved, "", 301}, {"", CodeRepoMoved, "", 301}, {"https://evil.test/secret", CodeMalformed, "", 302},
		}
		for i, c := range cases {
			n := i + 1
			h := map[string]string{}
			if c.location != "" {
				loc := c.location
				if strings.Contains(loc, "%d") {
					loc = fmt.Sprintf(loc, n)
				}
				h["Location"] = loc
			}
			x.fixed(fmt.Sprintf("repos/a/b/pulls/%d", n), c.status, "secret", h)
		}
		x.fixed("repos/new/name/pulls/1", 200, "{}", nil)
		for i, c := range cases {
			o := x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: i + 1})
			x.must(o.Error.Code == c.want && o.Error.NewName == c.newName && len(o.Requests) == 1 && !o.Error.MessageLeaksSecret, fmt.Sprintf("redirect %d: %+v", i, o.Error))
		}
		x.write(dir)
	}
	{
		x := newHX(t, "http-typed-functions-and-refused-inputs", "Every typed read (list page, pulls/N, its commits, branch tip with an encoded name, commit, check runs, statuses, rate_limit) sends exactly its allowlisted GET; invalid inputs (bad repo names, number 0, a header-injecting validator, page and page-size bounds, a raw or dot branch name, a non-SHA commit) are refused before any request.",
			"watch_reader_test.go:TestWatchTypedFunctionsAndRefusedInputs")
		sha := "0123456789abcdef0123456789abcdef01234567"
		for _, ep := range []string{"repos/a/b/pulls?state=open&per_page=25&page=2", "repos/a/b/pulls/7", "repos/a/b/pulls/7/commits?per_page=100&page=2", "repos/a/b/branches/feat%2Fx",
			"repos/a/b/commits/" + sha, "repos/a/b/commits/" + sha + "/check-runs?per_page=100&page=1", "repos/a/b/commits/" + sha + "/statuses?per_page=100&page=1"} {
			x.serve(ep, `"e"`, `{}`, nil)
		}
		x.fixed("rate_limit", 200, `{"resources":{"core":{"limit":5000,"remaining":4000,"reset":1700000000}}}`, nil)
		for _, c := range []hCall{{Op: "pull_list_page", Repo: "a/b", PerPage: 25, Page: 2}, {Op: "pull_detail", Repo: "a/b", Number: 7}, {Op: "pull_commits", Repo: "a/b", Number: 7, Page: 2},
			{Op: "branch", Repo: "a/b", Name: "feat/x"}, {Op: "commit", Repo: "a/b", SHA: sha}, {Op: "check_runs", Repo: "a/b", SHA: sha, Page: 1}, {Op: "statuses", Repo: "a/b", SHA: sha, Page: 1}, {Op: "rate_limit"}} {
			o := x.do(c)
			x.must(o.Error == nil && len(o.Requests) == 1, c.Op)
		}
		for _, c := range []hCall{
			{Op: "pull_detail", Repo: "a/../b", Number: 7}, {Op: "pull_detail", Repo: "a/b/c", Number: 7}, {Op: "pull_detail", Repo: "../b", Number: 7}, {Op: "pull_detail", Repo: "a/b", Number: 0},
			{Op: "pull_detail", Repo: "a/b", Number: 7, ETag: "bad\r\nX: y"}, {Op: "pull_list_page", Repo: "a/b", PerPage: 100, Page: 1}, {Op: "pull_list_page", Repo: "a/b", PerPage: 25, Page: 81},
			{Op: "pull_list_page", Repo: "a/b", PerPage: 12, Page: 168}, {Op: "pull_commits", Repo: "a/b", Number: 7, Page: 4}, {Op: "branch", Repo: "a/b", Name: ".."}, {Op: "branch", Repo: "a/b", Name: "%2e%2e"},
			{Op: "branch", Repo: "a/b", Name: "a%2Fb"}, {Op: "commit", Repo: "a/b", SHA: "main"}, {Op: "check_runs", Repo: "a/b", SHA: sha, Page: 11}} {
			o := x.do(c)
			x.must(o.Error != nil && len(o.Requests) == 0, "refused input "+c.Op)
		}
		x.write(dir)
	}
	{
		x := newHX(t, "http-token-never-in-errors", "A server that echoes the Authorization header in an error body does not make the token reachable from the error.", "watch_reader_test.go:TestWatchTokenNeverInErrorsOrBodies")
		x.route(hRoute{URI: "repos/a/b/pulls/7", Status: 500, EchoAuth: true})
		o := x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: 7})
		x.must(o.Error.Code == CodeHTTP5xx && !o.Error.MessageLeaksSecret, "token")
		x.write(dir)
	}
	{
		x := newHX(t, "http-not-modified-keeps-the-sent-validator", "A 304 returns the validator that was sent, weak prefix included, even if the server repeats another form.", "watch_reader_test.go:TestWatchNotModifiedKeepsSentValidator")
		x.fixed("repos/a/b/pulls/7", 304, "", map[string]string{"ETag": `"strong"`})
		o := x.do(hCall{Op: "pull_detail", Repo: "a/b", Number: 7, ETag: `W/"v1"`})
		x.must(o.Response.NotModified && o.Response.ETag == `W/"v1"`, "validator")
		x.write(dir)
	}
}

func exportPullsScenarios(t *testing.T, dir string) {
	threePagesX := func(x *hx, v [3]string) {
		x.serve(listURI(25, 1), `"p1`+v[0]+`"`, pageBody(1, 25), linkNext(25, 2))
		x.serve(listURI(25, 2), `"p2`+v[1]+`"`, pageBody(26, 50), linkNext(25, 3))
		x.serve(listURI(25, 3), `"p3`+v[2]+`"`, pageBody(51, 60), nil)
	}
	{
		x := newHX(t, "pulls-paginate-with-link-across-three-pages", "The open list is read page by page by the Link rel=next header (25 per page, the next page must be exactly the current plus one); the items are parsed to number, title, user, labels, draft, head sha and ref, base ref and sha; each page's ETag and Link are kept.",
			"watch_pulls_test.go:TestListOpenPullsPaginatesWithLinkAcrossThreePages")
		threePagesX(x, [3]string{"a", "a", "a"})
		o := x.do(listCall(2000, "none"))
		x.must(o.List.Items == 60 && !o.List.Unchanged && o.List.PerPage == 25 && len(o.Requests) == 3, "three pages")
		x.write(dir)
	}
	{
		x := newHX(t, "pulls-304-reuses-stored-link-and-items", "With the stored state every page is requested conditionally; all 304 means unchanged with the stored items; the Link chain comes from the stored Link because GitHub's 304 has none; a changed middle page is replaced while the others stay 304.",
			"watch_pulls_test.go:TestListOpenPulls304ReusesStoredLinkAndItems", "watch_pulls_test.go:TestListOpenPullsStoredLinkSurvivesLinklessNotModified")
		threePagesX(x, [3]string{"a", "a", "a"})
		x.do(listCall(2000, "none"))
		o := x.do(listCall(2000, "previous"))
		x.must(o.List.Unchanged && o.List.Items == 60 && len(o.Requests) == 3 && o.Requests[0].IfNoneMatch == `"p1a"`, "all 304")
		o = x.do(hCall{Op: "list_open_pulls", Repo: "a/b", MaxOpen: 2000, Prior: "previous", SetRoutes: []hRoute{
			{URI: listURI(25, 2), ETag: `"p2b"`, Body: pageBody(26, 49), Headers: linkNext(25, 3)}}, Note: "page 2 changed to 24 items"})
		x.must(!o.List.Unchanged && o.List.Items == 59, "page 2 changed")
		x.write(dir)
	}
	{
		x := newHX(t, "pulls-halves-page-size-and-restarts-at-page-one", "A page over 1 MiB halves the page size to 12 and restarts at page 1; 12 is kept for the repository (conditional requests at 12 afterwards).",
			"watch_pulls_test.go:TestListOpenPullsHalvesAndRestartsAtPageOne")
		x.serve(listURI(25, 1), `"x1"`, pageBody(1, 25), linkNext(25, 2))
		x.route(hRoute{URI: listURI(25, 2), Status: 200, BodyRepeat: &hRepeat{"x", maxBytes + 1}})
		x.serve(listURI(12, 1), `"y1"`, pageBody(1, 12), linkNext(12, 2))
		x.serve(listURI(12, 2), `"y2"`, pageBody(13, 24), linkNext(12, 3))
		x.serve(listURI(12, 3), `"y3"`, pageBody(25, 30), nil)
		o := x.do(listCall(2000, "none"))
		x.must(o.List.PerPage == 12 && o.List.Items == 30 && len(o.Requests) == 5, "halved")
		o = x.do(listCall(2000, "previous"))
		x.must(o.List.Unchanged && o.Requests[0].URI == listURI(12, 1) && o.Requests[0].IfNoneMatch == `"y1"`, "kept")
		x.write(dir)
	}
	{
		x := newHX(t, "pulls-page-too-large-at-twelve", "A page over 1 MiB at 12 per page (by body or by declared length) is page-too-large after exactly two requests.", "watch_pulls_test.go:TestListOpenPullsPageTooLargeAtTwelve")
		x.route(hRoute{URI: listURI(25, 1), Status: 200, BodyRepeat: &hRepeat{"x", maxBytes + 1}})
		x.route(hRoute{URI: listURI(12, 1), Status: 200, BodyRepeat: &hRepeat{"x", maxBytes + 1}})
		o := x.do(listCall(2000, "none"))
		x.must(o.Error.Code == CodePageTooLarge && len(o.Requests) == 2, "too large")
		x.write(dir)
		y := newHX(t, "pulls-page-too-large-by-declared-length", "The declared Content-Length alone over 1 MiB gives page-too-large too.", "watch_pulls_test.go:TestListOpenPullsPageTooLargeAtTwelve")
		y.route(hRoute{URI: listURI(25, 1), Status: 200, Body: "[]", ContentLength: maxBytes + 1})
		y.route(hRoute{URI: listURI(12, 1), Status: 200, Body: "[]", ContentLength: maxBytes + 1})
		o = y.do(listCall(2000, "none"))
		y.must(o.Error.Code == CodePageTooLarge, "declared")
		y.write(dir)
	}
	{
		for _, tc := range []struct{ perPage, maxOpen, wantPages int }{{25, 2000, 80}, {12, 2000, 167}, {25, 30, 2}, {25, 25, 1}} {
			x := newHX(t, fmt.Sprintf("pulls-too-many-open-prs-%d-per-page-max-%d", tc.perPage, tc.maxOpen), fmt.Sprintf("An endless list at %d per page with max_open_prs %d stops with too-many-open-prs after exactly %d pages.", tc.perPage, tc.maxOpen, tc.wantPages),
				"watch_pulls_test.go:TestListOpenPullsTooManyOpenPRs")
			for p := 1; p <= tc.wantPages+1; p++ {
				x.serve(listURI(tc.perPage, p), fmt.Sprintf(`"e%d"`, p), "["+pullJSON(p)+"]", linkNext(tc.perPage, p+1))
			}
			c := listCall(tc.maxOpen, "per_page_only")
			c.PriorPerPage = tc.perPage
			c.Halved = tc.perPage == 12
			o := x.do(c)
			x.must(o.Error.Code == CodeTooManyPRs && len(o.Requests) == tc.wantPages, "pages")
			x.write(dir)
		}
		x := newHX(t, "pulls-too-many-open-prs-on-the-last-page", "More items than max_open_prs on the last page is too-many-open-prs.", "watch_pulls_test.go:TestListOpenPullsTooManyOpenPRs")
		x.serve(listURI(25, 1), `"e"`, pageBody(1, 25), nil)
		o := x.do(listCall(10, "none"))
		x.must(o.Error.Code == CodeTooManyPRs, "last page")
		x.write(dir)
	}
	{
		x := newHX(t, "pulls-rejects-bad-bounds-links-and-items", "max_open_prs outside 1..2000 is refused without a request; a Link that skips a page, goes backwards or has no number is never followed (malformed); malformed list bodies (not an array, number 0, short sha, non-objects, not JSON) are malformed.",
			"watch_pulls_test.go:TestListOpenPullsRejectsBadBoundsAndLinks")
		for _, m := range []int{0, -1, WatchMaxOpenPRs + 1} {
			o := x.do(listCall(m, "none"))
			x.must(o.Error.Code == CodeMalformed && len(o.Requests) == 0, "bounds")
		}
		for i, link := range []string{linkNext(25, 3)["Link"], linkNext(25, 1)["Link"], `<https://api.github.com/x?page=zzz>; rel="next"`} {
			c := listCall(2000, "none")
			c.Note = fmt.Sprintf("link %d", i)
			c.SetRoutes = []hRoute{{URI: listURI(25, 1), ETag: `"e"`, Body: pageBody(1, 3), Headers: map[string]string{"Link": link}, NotModifiedOnMatch: true}}
			c.NewReader = true
			o := x.do(c)
			x.must(o.Error.Code == CodeMalformed && len(o.Requests) == 1, "link")
		}
		for _, body := range []string{`{"a":1}`, `[{"number":0}]`, `[{"number":1,"head":{"sha":"short"}}]`, `[1]`, `not json`} {
			c := listCall(2000, "none")
			c.Note = "body " + body
			c.SetRoutes = []hRoute{{URI: listURI(25, 1), ETag: `"e"`, Body: body, NotModifiedOnMatch: true}}
			c.NewReader = true
			o := x.do(c)
			x.must(o.Error.Code == CodeMalformed, "body")
		}
		x.write(dir)
	}
	{
		x := newHX(t, "pulls-errors-pass-through", "A 301 on the list is repo-moved with the new name; a rate-limited list is not retried.", "watch_pulls_test.go:TestListOpenPullsErrorsPassThrough")
		x.fixed(listURI(25, 1), 301, "x", map[string]string{"Location": "https://api.github.com/repos/new/name/pulls?state=open&per_page=25&page=1"})
		o := x.do(listCall(2000, "none"))
		x.must(o.Error.Code == CodeRepoMoved && o.Error.NewName == "new/name", "moved")
		o = x.do(hCall{Op: "list_open_pulls", Repo: "a/b", MaxOpen: 2000, Prior: "none", NewReader: true, SetRoutes: []hRoute{{URI: listURI(25, 1), Status: 403, Body: `{"message":"secondary rate limit"}`, Headers: map[string]string{"Retry-After": "30"}}}})
		x.must(o.Error.Code == CodeRateLimited && len(o.Requests) == 1, "rate limited")
		x.write(dir)
	}
	{
		x := newHX(t, "pulls-probes-past-a-full-last-page-on-304", "A 304 reuses the stored Link, so an exactly full last page could hide a reopened PR behind it: the next page is probed once (first time unconditionally, then conditionally); a 200 is authoritative and needs no probe; a short last page is never probed.",
			"watch_pulls_test.go:TestListOpenPullsProbesPastAFullLastPageOnNotModified")
		x.serve(listURI(25, 1), `"p1"`, pageBody(1, 25), nil)
		x.serve(listURI(25, 2), `"p2"`, "[]", nil)
		o := x.do(listCall(2000, "none"))
		x.must(o.List.Items == 25 && len(o.Requests) == 1, "200 authoritative")
		o = x.do(listCall(2000, "previous"))
		x.must(o.List.Unchanged && len(o.Requests) == 2 && o.Requests[1].IfNoneMatch == "", "probe")
		o = x.do(listCall(2000, "previous"))
		x.must(o.List.Unchanged && len(o.Requests) == 2 && o.Requests[1].IfNoneMatch == `"p2"`, "conditional probe")
		o = x.do(hCall{Op: "list_open_pulls", Repo: "a/b", MaxOpen: 2000, Prior: "previous", SetRoutes: []hRoute{{URI: listURI(25, 2), ETag: `"p2b"`, Body: pageBody(26, 26), NotModifiedOnMatch: true}}, Note: "an older PR is reopened"})
		x.must(!o.List.Unchanged && o.List.Items == 26, "reopened PR")
		x.write(dir)
		y := newHX(t, "pulls-short-last-page-is-not-probed", "A short last page (24 of 25) is never probed on a 304.", "watch_pulls_test.go:TestListOpenPullsProbesPastAFullLastPageOnNotModified")
		y.serve(listURI(25, 1), `"q1"`, pageBody(1, 24), nil)
		y.do(listCall(2000, "none"))
		o = y.do(listCall(2000, "previous"))
		y.must(len(o.Requests) == 1, "no probe")
		y.write(dir)
	}
	{
		x := newHX(t, "pulls-no-probe-beyond-the-allowlist-bound", "At the page bound (80 full pages) a 304 listing does not probe a page beyond the allowlist: 80 requests.", "watch_pulls_test.go:TestListOpenPullsNoProbeBeyondAllowlistBound")
		for p := 1; p <= 80; p++ {
			next := linkNext(25, p+1)
			if p == 80 {
				next = nil
			}
			x.serve(listURI(25, p), fmt.Sprintf(`"e%d"`, p), pageBody((p-1)*25+1, p*25), next)
		}
		o := x.do(listCall(2000, "none"))
		x.must(o.List.Items == 2000, "2000")
		o = x.do(listCall(2000, "previous"))
		x.must(len(o.Requests) == 80, "80")
		x.write(dir)
	}
	{
		x := newHX(t, "pulls-halved-page-size-is-in-memory-only", "The halved page size lives in the reader until restart (per repository); it is not read from the stored state: a new reader with the stored 12-sized state starts at 25 without validators.",
			"watch_pulls_test.go:TestHalvedPageSizeIsInMemoryOnly")
		x.serve(listURI(25, 1), `"x1"`, pageBody(1, 25), linkNext(25, 2))
		x.route(hRoute{URI: listURI(25, 2), Status: 200, BodyRepeat: &hRepeat{"x", maxBytes + 1}})
		x.serve(listURI(12, 1), `"y1"`, pageBody(1, 12), linkNext(12, 2))
		x.serve(listURI(12, 2), `"y2"`, pageBody(13, 20), nil)
		o := x.do(listCall(2000, "none"))
		x.must(o.List.PerPage == 12, "halved")
		o = x.do(listCall(2000, "previous"))
		x.must(o.Requests[0].URI == listURI(12, 1), "kept")
		stateAB := x.state
		c := hCall{Op: "list_open_pulls", Repo: "c/d", MaxOpen: 2000, Prior: "none", SetRoutes: []hRoute{{URI: "repos/c/d/pulls?state=open&per_page=25&page=1", ETag: `"z"`, Body: pageBody(1, 3), NotModifiedOnMatch: true}}, Note: "another repository is unaffected"}
		o = x.do(c)
		x.must(o.Requests[0].URI == "repos/c/d/pulls?state=open&per_page=25&page=1", "other repo")
		// the stored state of a/b (12 per page) goes to a new reader: restart at 25
		x.state = stateAB
		o = x.do(hCall{Op: "list_open_pulls", Repo: "a/b", MaxOpen: 2000, Prior: "previous", NewReader: true, Note: "restart with the stored 12-sized state"})
		x.must(o.Requests[0].URI == listURI(25, 1) && o.Requests[0].IfNoneMatch == "", "restart at 25")
		x.write(dir)
	}
	_ = sort.Strings
}
