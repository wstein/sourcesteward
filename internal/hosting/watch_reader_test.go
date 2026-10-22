// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeForge is an in-process forge: handlers are keyed by the escaped request
// URI (path and query without the API prefix). It records every request and
// fails the test on anything but GET, so the read-only guarantee is part of
// every watch test.
type fakeForge struct {
	t        *testing.T
	mu       sync.Mutex
	handlers map[string]func(r *http.Request) *http.Response
	log      []string
	etags    []string
}

func newFakeForge(t *testing.T) *fakeForge {
	return &fakeForge{t: t, handlers: map[string]func(r *http.Request) *http.Response{}}
}

func (f *fakeForge) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodGet {
		f.t.Errorf("non-GET request reached the forge: %s %s", r.Method, r.URL)
		return nil, errors.New("non-GET")
	}
	uri := strings.TrimPrefix(r.URL.EscapedPath(), "/")
	if r.URL.RawQuery != "" {
		uri += "?" + r.URL.RawQuery
	}
	f.log = append(f.log, uri)
	f.etags = append(f.etags, r.Header.Get("If-None-Match"))
	h := f.handlers[uri]
	if h == nil {
		return forgeResponse(404, `{"message":"Not Found"}`, nil), nil
	}
	return h(r), nil
}

func forgeResponse(code int, body string, header map[string]string) *http.Response {
	res := response(code, body)
	for k, v := range header {
		res.Header.Set(k, v)
	}
	return res
}

// serve registers a body with an ETag; a matching If-None-Match yields 304.
func (f *fakeForge) serve(uri, etag, body string, header map[string]string) {
	f.handlers[uri] = func(r *http.Request) *http.Response {
		h := map[string]string{"ETag": etag}
		for k, v := range header {
			h[k] = v
		}
		if etag != "" && r.Header.Get("If-None-Match") == etag {
			return forgeResponse(304, "", h)
		}
		return forgeResponse(200, body, h)
	}
}

func (f *fakeForge) reader(host string) *WatchReader {
	client, err := newSDKClientMode(host, "fixturetoken", f.t.TempDir(), f, true)
	if err != nil {
		f.t.Fatal(err)
	}
	return newWatchReader(&ghAPI{client: client, host: host})
}

func TestWatchConditionalRoundTripAndNotModified(t *testing.T) {
	f := newFakeForge(t)
	f.serve("repos/a/b/pulls/7", `"v1"`, `{"number":7}`, map[string]string{"X-RateLimit-Limit": "5000", "X-RateLimit-Remaining": "4990", "X-RateLimit-Reset": "1700000000"})
	r := f.reader("github.com")
	first, err := r.PullDetail(context.Background(), "a/b", 7, "")
	if err != nil || first.Status != 200 || first.NotModified || first.ETag != `"v1"` || string(first.Body) != `{"number":7}` {
		t.Fatalf("%+v %v", first, err)
	}
	if f.etags[0] != "" {
		t.Fatal("unsolicited If-None-Match")
	}
	if !first.RateLimit.Known || first.RateLimit.Limit != 5000 || first.RateLimit.Remaining != 4990 || !first.RateLimit.Reset.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("rate limit headers %+v", first.RateLimit)
	}
	second, err := r.PullDetail(context.Background(), "a/b", 7, first.ETag)
	if err != nil || !second.NotModified || second.Status != 304 || second.ETag != `"v1"` || len(second.Body) != 0 {
		t.Fatalf("304 must be a typed result, not an error: %+v %v", second, err)
	}
	if f.etags[1] != `"v1"` {
		t.Fatalf("If-None-Match not sent: %q", f.etags[1])
	}
	calls := r.TakeCalls()
	if c := calls[KindDetail]; c.Requests != 2 || c.NotModified != 1 {
		t.Fatalf("accounting %+v", calls)
	}
	if again := r.TakeCalls(); len(again) != 0 {
		t.Fatalf("TakeCalls must reset: %+v", again)
	}
}

func TestWatchUnsolicited304IsMalformed(t *testing.T) {
	f := newFakeForge(t)
	f.handlers["repos/a/b/pulls/7"] = func(*http.Request) *http.Response { return forgeResponse(304, "", nil) }
	_, err := f.reader("github.com").PullDetail(context.Background(), "a/b", 7, "")
	assertWatchCode(t, err, CodeMalformed)
}

func assertWatchCode(t *testing.T, err error, code string) *WatchError {
	t.Helper()
	var we *WatchError
	if !errors.As(err, &we) || we.Code != code {
		t.Fatalf("want %s, got %#v", code, err)
	}
	for _, secret := range []string{"fixturetoken", "Bearer", "token "} {
		if strings.Contains(err.Error(), secret) || strings.Contains(we.Message, secret) {
			t.Fatalf("token or header in error: %q", err)
		}
	}
	return we
}

func TestWatchLinkHeaderParsing(t *testing.T) {
	for _, tc := range []struct {
		link string
		next int
		ok   bool
	}{
		{`<https://api.github.com/repositories/1/pulls?state=open&per_page=25&page=2>; rel="next", <https://api.github.com/repositories/1/pulls?state=open&per_page=25&page=4>; rel="last"`, 2, true},
		{`<https://api.github.com/repos/a/b/pulls?page=3&state=open&per_page=25>; rel="next"`, 3, true},
		{`<https://api.github.com/repos/a/b/pulls?state=open&per_page=25&page=4>; rel="last", <https://api.github.com/repos/a/b/pulls?state=open&per_page=25&page=1>; rel="first"`, 0, false},
		{``, 0, false},
		{`<https://api.github.com/x?page=abc>; rel="next"`, 0, false},
		{`<https://api.github.com/x?page=0>; rel="next"`, 0, false},
		{`<https://api.github.com/x?page=99999999999999999999>; rel="next"`, 0, false},
		{`garbage; rel="next"`, 0, false},
		{`<https://api.github.com/x?page=2>; rel="prev"`, 0, false},
		{`<https://api.github.com/x?page=2>; rel=next`, 2, true},
	} {
		n, ok := nextPageFromLink(tc.link)
		if n != tc.next || ok != tc.ok {
			t.Errorf("%q -> %d %v", tc.link, n, ok)
		}
	}
}

func TestWatchErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   int
		body   string
		header map[string]string
		want   string
	}{
		{"401", 401, `{"message":"Bad credentials"}`, nil, CodeUnauthorized},
		{"403 plain", 403, `{"message":"Resource not accessible"}`, nil, CodeForbidden},
		{"403 primary", 403, `{"message":"API rate limit exceeded"}`, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1700000100"}, CodeRateLimited},
		{"403 secondary", 403, `{"message":"You have exceeded a secondary rate limit."}`, map[string]string{"Retry-After": "60"}, CodeRateLimited},
		{"403 secondary no header", 403, `{"message":"You have exceeded a secondary rate limit"}`, nil, CodeRateLimited},
		{"429", 429, `{}`, nil, CodeRateLimited},
		{"404", 404, `{"message":"Not Found"}`, nil, CodeForbidden},
		{"500", 500, `oops`, nil, CodeHTTP5xx},
		{"503", 503, `oops`, nil, CodeHTTP5xx},
		{"201", 201, `{}`, nil, CodeMalformed},
		{"body limit", 200, strings.Repeat("x", maxBytes+1), nil, CodeMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeForge(t)
			f.handlers["repos/a/b/pulls/7"] = func(*http.Request) *http.Response { return forgeResponse(tc.code, tc.body, tc.header) }
			_, err := f.reader("github.com").PullDetail(context.Background(), "a/b", 7, "")
			we := assertWatchCode(t, err, tc.want)
			if strings.Contains(we.Message, "Bad credentials") || strings.Contains(we.Message, "secondary") || strings.Contains(we.Message, "xxxx") {
				t.Fatalf("response text leaked into the error: %q", we.Message)
			}
			if len(we.Message) > 200 {
				t.Fatalf("unbounded message")
			}
			if tc.want == CodeForbidden && we.Status != tc.code {
				t.Fatalf("status %d", we.Status)
			}
		})
	}
}

func TestWatchRateLimitedCarriesResetAndRetryAfter(t *testing.T) {
	f := newFakeForge(t)
	f.handlers["repos/a/b/pulls/7"] = func(*http.Request) *http.Response {
		return forgeResponse(403, `{"message":"secondary rate limit"}`, map[string]string{"Retry-After": "90", "X-RateLimit-Remaining": "12", "X-RateLimit-Limit": "5000", "X-RateLimit-Reset": "1700000100"})
	}
	_, err := f.reader("github.com").PullDetail(context.Background(), "a/b", 7, "")
	we := assertWatchCode(t, err, CodeRateLimited)
	if we.RetryAfter != 90*time.Second || !we.RateLimit.Reset.Equal(time.Unix(1700000100, 0)) {
		t.Fatalf("%+v", we)
	}
}

func TestWatchRedirectsRefusedAndRepoMoved(t *testing.T) {
	for _, tc := range []struct {
		name, location, want, newName string
		status                        int
	}{
		{"301 by name", "https://api.github.com/repos/new/name/pulls/7", CodeRepoMoved, "new/name", 301},
		{"301 by id", "https://api.github.com/repositories/12345/pulls/7", CodeRepoMoved, "", 301},
		{"301 hostile name", "https://api.github.com/repos/../x/pulls/7", CodeRepoMoved, "", 301},
		{"301 other host", "https://evil.test/repos/new/name/pulls/7", CodeRepoMoved, "", 301},
		{"301 no location", "", CodeRepoMoved, "", 301},
		{"302", "https://evil.test/secret", CodeMalformed, "", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeForge(t)
			hits := 0
			f.handlers["repos/a/b/pulls/7"] = func(*http.Request) *http.Response {
				hits++
				h := map[string]string{}
				if tc.location != "" {
					h["Location"] = tc.location
				}
				return forgeResponse(tc.status, "secret", h)
			}
			f.handlers["repos/new/name/pulls/7"] = func(*http.Request) *http.Response { hits += 100; return response(200, "{}") }
			_, err := f.reader("github.com").PullDetail(context.Background(), "a/b", 7, "")
			we := assertWatchCode(t, err, tc.want)
			if hits != 1 {
				t.Fatalf("redirect followed: %d", hits)
			}
			if we.NewName != tc.newName {
				t.Fatalf("new name %q", we.NewName)
			}
			if tc.newName != "" && !strings.Contains(we.Message, tc.newName) {
				t.Fatalf("message lacks the new name: %q", we.Message)
			}
			if strings.Contains(we.Message, "secret") || strings.Contains(we.Message, "evil") {
				t.Fatalf("leak: %q", we.Message)
			}
		})
	}
}

func TestWatchTypedFunctionsAndRefusedInputs(t *testing.T) {
	f := newFakeForge(t)
	for _, ep := range []string{
		"repos/a/b/pulls?state=open&per_page=25&page=2", "repos/a/b/pulls/7", "repos/a/b/pulls/7/commits?per_page=100&page=2", "repos/a/b/branches/feat%2Fx",
		"repos/a/b/commits/" + sha40, "repos/a/b/commits/" + sha40 + "/check-runs?per_page=100&page=1", "repos/a/b/commits/" + sha40 + "/statuses?per_page=100&page=1",
	} {
		f.serve(ep, `"e"`, `{}`, nil)
	}
	f.handlers["rate_limit"] = func(*http.Request) *http.Response {
		return response(200, `{"resources":{"core":{"limit":5000,"remaining":4000,"reset":1700000000}}}`)
	}
	r := f.reader("github.com")
	ctx := context.Background()
	steps := []struct {
		kind WatchKind
		do   func() (WatchResponse, error)
	}{
		{KindList, func() (WatchResponse, error) { return r.PullListPage(ctx, "a/b", 25, 2, "") }},
		{KindDetail, func() (WatchResponse, error) { return r.PullDetail(ctx, "a/b", 7, "") }},
		{KindCommits, func() (WatchResponse, error) { return r.PullCommits(ctx, "a/b", 7, 2, "") }},
		{KindBranch, func() (WatchResponse, error) { return r.Branch(ctx, "a/b", "feat/x", "") }},
		{KindCommit, func() (WatchResponse, error) { return r.Commit(ctx, "a/b", sha40, "") }},
		{KindChecks, func() (WatchResponse, error) { return r.CheckRuns(ctx, "a/b", sha40, 1, "") }},
		{KindChecks, func() (WatchResponse, error) { return r.Statuses(ctx, "a/b", sha40, 1, "") }},
	}
	for i, s := range steps {
		if _, err := s.do(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	info, err := r.RateLimit(ctx)
	if err != nil || info.Limit != 5000 || info.Remaining != 4000 || !info.Reset.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("%+v %v", info, err)
	}
	calls := r.TakeCalls()
	for _, k := range []WatchKind{KindList, KindDetail, KindCommits, KindBranch, KindCommit, KindChecks, KindRateLimit} {
		if calls[k].Requests == 0 {
			t.Errorf("kind %s not counted: %+v", k, calls)
		}
	}
	if calls[KindChecks].Requests != 2 {
		t.Errorf("checks counted %d", calls[KindChecks].Requests)
	}
	sent := len(f.log)
	for _, bad := range []func() error{
		func() error { _, e := r.PullDetail(ctx, "a/../b", 7, ""); return e },
		func() error { _, e := r.PullDetail(ctx, "a/b/c", 7, ""); return e },
		func() error { _, e := r.PullDetail(ctx, "../b", 7, ""); return e },
		func() error { _, e := r.PullDetail(ctx, "a/b", 0, ""); return e },
		func() error { _, e := r.PullDetail(ctx, "a/b", 7, "bad\r\nX: y"); return e },
		func() error { _, e := r.PullListPage(ctx, "a/b", 100, 1, ""); return e },
		func() error { _, e := r.PullListPage(ctx, "a/b", 25, 81, ""); return e },
		func() error { _, e := r.PullListPage(ctx, "a/b", 12, 168, ""); return e },
		func() error { _, e := r.PullCommits(ctx, "a/b", 7, 4, ""); return e },
		func() error { _, e := r.Branch(ctx, "a/b", "..", ""); return e },
		func() error { _, e := r.Branch(ctx, "a/b", "%2e%2e", ""); return e },
		func() error { _, e := r.Branch(ctx, "a/b", "a%2Fb", ""); return e },
		func() error { _, e := r.Commit(ctx, "a/b", "main", ""); return e },
		func() error { _, e := r.CheckRuns(ctx, "a/b", sha40, 11, ""); return e },
	} {
		if bad() == nil {
			t.Errorf("invalid input accepted")
		}
	}
	if len(f.log) != sent {
		t.Fatalf("refused inputs reached the forge: %v", f.log[sent:])
	}
}

func TestWatchCancellationAndDeadline(t *testing.T) {
	client, err := newSDKClientMode("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }), true)
	if err != nil {
		t.Fatal(err)
	}
	r := newWatchReader(&ghAPI{client: client, host: "github.com"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = r.PullDetail(ctx, "a/b", 7, "")
	assertWatchCode(t, err, CodeNetwork)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not visible: %v", err)
	}
	r.timeout = 20 * time.Millisecond
	start := time.Now()
	_, err = r.PullDetail(context.Background(), "a/b", 7, "")
	assertWatchCode(t, err, CodeNetwork)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("per-request deadline not applied: %v", err)
	}
}

func TestWatchTokenNeverInErrorsOrBodies(t *testing.T) {
	f := newFakeForge(t)
	f.handlers["repos/a/b/pulls/7"] = func(r *http.Request) *http.Response {
		return forgeResponse(500, "echo "+r.Header.Get("Authorization"), nil)
	}
	_, err := f.reader("github.com").PullDetail(context.Background(), "a/b", 7, "")
	we := assertWatchCode(t, err, CodeHTTP5xx)
	if fmt.Sprint(we) != we.Error() || strings.Contains(fmt.Sprintf("%#v", we), "fixturetoken") {
		t.Fatal("token reachable from the error value")
	}
}

func TestNewWatchReaderUsesGhTokenAndWatchGuard(t *testing.T) {
	d := t.TempDir()
	target := filepath.Join(d, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(d, "gh")
	body := "#!/bin/sh\nif [ \"$1\" = \"--version\" ];then echo 'gh version 2.102.0';exit 0;fi\necho testsecret\n"
	if err := os.WriteFile(script, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", script)
	var auth string
	r, cleanup, err := NewWatchReaderWithTransport(context.Background(), target, "github.com", roundTripFunc(func(req *http.Request) (*http.Response, error) {
		auth = req.Header.Get("Authorization")
		return response(200, `{}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := r.Branch(context.Background(), "a/b", "feat/x", ""); err != nil || auth != "token testsecret" {
		t.Fatalf("%v %q", err, auth)
	}
}

// The validator that was sent is kept, weak prefix included, even when the server's
// 304 repeats a different (strong) form.
func TestWatchNotModifiedKeepsSentValidator(t *testing.T) {
	f := newFakeForge(t)
	f.handlers["repos/a/b/pulls/7"] = func(r *http.Request) *http.Response {
		return forgeResponse(304, "", map[string]string{"ETag": `"strong"`})
	}
	res, err := f.reader("github.com").PullDetail(context.Background(), "a/b", 7, `W/"v1"`)
	if err != nil || !res.NotModified || res.ETag != `W/"v1"` {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestWatchRetryAfterUpperBound(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{{"0", 0}, {"30", 30 * time.Second}, {"86400", 24 * time.Hour}, {"86401", 0}, {"-1", 0}, {"abc", 0}, {"", 0}} {
		h := http.Header{}
		h.Set("Retry-After", tc.header)
		if got := retryAfter(h); got != tc.want {
			t.Errorf("Retry-After %q -> %v, want %v", tc.header, got, tc.want)
		}
	}
}

func TestWatchNegativeRateRemainingIsUnknown(t *testing.T) {
	h := http.Header{}
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", "-1")
	h.Set("X-RateLimit-Reset", "1700000000")
	if rateFromHeader(h).Known {
		t.Fatal("negative X-RateLimit-Remaining accepted")
	}
	h.Set("X-RateLimit-Remaining", "0")
	if !rateFromHeader(h).Known {
		t.Fatal("zero remaining must be accepted")
	}
	// A 403 whose only rate evidence is a negative remaining is not a rate limit.
	f := newFakeForge(t)
	f.handlers["repos/a/b/pulls/7"] = func(*http.Request) *http.Response {
		return forgeResponse(403, `{"message":"Resource not accessible"}`, map[string]string{"X-RateLimit-Limit": "5000", "X-RateLimit-Remaining": "-1", "X-RateLimit-Reset": "1700000000"})
	}
	_, err := f.reader("github.com").PullDetail(context.Background(), "a/b", 7, "")
	assertWatchCode(t, err, CodeForbidden)
}
