// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func readerForMode(f *fakeForge, mode guardMode) *WatchReader {
	client, err := newSDKClientGuardMode("github.com", "fixturetoken", f.t.TempDir(), f, mode)
	if err != nil {
		f.t.Fatal(err)
	}
	r := newWatchReader(&ghAPI{client: client, host: "github.com"})
	r.mode = mode
	return r
}

func TestIssuesPageReadsTheFixedShapeAndNeverFollowsLink(t *testing.T) {
	f := newFakeForge(t)
	link := `<https://evil.example/repos/o/r/issues?page=2>; rel="next", <https://api.github.com/repos/o/r/issues?page=9>; rel="last"`
	f.serve(issuesListBase+"1", "", `[{"number":1}]`, map[string]string{"Link": link})
	for _, mode := range []guardMode{guardIssues, guardStatus} {
		f.log = nil
		res, err := readerForMode(f, mode).IssuesPage(context.Background(), "o/r", 1)
		if err != nil || res.Status != 200 || string(res.Body) != `[{"number":1}]` || res.Link != link {
			t.Fatalf("mode %d: %+v %v", mode, res, err)
		}
		if len(f.log) != 1 || f.log[0] != issuesListBase+"1" {
			t.Fatalf("mode %d requested %v", mode, f.log)
		}
	}
}

func TestIssuesPageRefusesBadInputAndOtherReadersBeforeSending(t *testing.T) {
	f := newFakeForge(t)
	f.serve(issuesListBase+"1", "", `[]`, nil)
	r := readerForMode(f, guardIssues)
	for _, repo := range []string{"", "o", "o/r/x", "o/..", "o/r?x=1"} {
		_, err := r.IssuesPage(context.Background(), repo, 1)
		assertWatchCode(t, err, CodeMalformed)
	}
	for _, page := range []int{0, 11, -1} {
		_, err := r.IssuesPage(context.Background(), "o/r", page)
		assertWatchCode(t, err, CodeMalformed)
	}
	for _, mode := range []guardMode{guardOneShot, guardWatch, guardPulls} {
		_, err := readerForMode(f, mode).IssuesPage(context.Background(), "o/r", 1)
		assertWatchCode(t, err, CodeMalformed)
		if _, err := readerForMode(f, mode).Viewer(context.Background()); err == nil {
			t.Errorf("mode %d read the viewer", mode)
		}
	}
	// Defence in depth: a reader that claims issues mode over a guard of another mode is stopped by that guard.
	for _, mode := range []guardMode{guardWatch, guardPulls} {
		hostile := readerForMode(f, mode)
		hostile.mode = guardIssues
		if _, err := hostile.IssuesPage(context.Background(), "o/r", 1); err == nil {
			t.Errorf("the %d guard let the issue list through", mode)
		}
		if _, err := hostile.Viewer(context.Background()); err == nil {
			t.Errorf("the %d guard let the viewer through", mode)
		}
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
}

func TestIssuesReaderCannotReachTheWatchSetOrThePRSearch(t *testing.T) {
	f := newFakeForge(t)
	prSearch, _ := SearchEndpoint("wstein", 1)
	r := readerForMode(f, guardIssues)
	for _, ep := range []string{"repos/a/b/pulls?state=open&per_page=25&page=1", "rate_limit", "repos/a/b/branches/main", prSearch} {
		if _, err := r.fetch(context.Background(), KindList, ep, "", true); err == nil {
			t.Errorf("%s was sent", ep)
		}
	}
	if _, err := r.SearchOwnerPage(context.Background(), "wstein", 1); err == nil {
		t.Fatal("the issues reader searched pull requests")
	}
	if d := r.DiscoverOwner(context.Background(), "wstein"); d.Code != CodeMalformed {
		t.Fatalf("%+v", d)
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
	// The pulls and status readers do not reach the issue search either.
	issueSearch, _ := IssueSearchEndpoint("wstein", 1)
	for _, mode := range []guardMode{guardPulls, guardStatus, guardWatch} {
		if _, err := readerForMode(f, mode).fetch(context.Background(), KindSearch, issueSearch, "", true); err == nil {
			t.Errorf("mode %d sent the issue search", mode)
		}
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
}

func TestIssuesPageStatusClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
		code   string
	}{
		{"410 is issues-disabled", 410, nil, CodeIssuesDisabled},
		{"401", 401, nil, CodeUnauthorized},
		{"403", 403, nil, CodeForbidden},
		{"403 with evidence", 403, map[string]string{"X-RateLimit-Remaining": "0"}, CodeRateLimited},
		{"404", 404, nil, CodeForbidden},
		{"500", 500, nil, CodeHTTP5xx},
		{"418", 418, nil, CodeMalformed},
	}
	for _, c := range cases {
		f := newFakeForge(t)
		f.handlers[issuesListBase+"1"] = func(*http.Request) *http.Response { return forgeResponse(c.status, `{"message":"x"}`, c.header) }
		_, err := readerForMode(f, guardIssues).IssuesPage(context.Background(), "o/r", 1)
		we := assertWatchCode(t, err, c.code)
		if we.Status != c.status {
			t.Errorf("%s: status %d", c.name, we.Status)
		}
	}
	// Only the issue list maps 410: the viewer read keeps the generic classification.
	f := newFakeForge(t)
	f.handlers["user"] = func(*http.Request) *http.Response { return forgeResponse(410, `{}`, nil) }
	_, err := readerForMode(f, guardIssues).Viewer(context.Background())
	assertWatchCode(t, err, CodeMalformed)
}

func TestIssuesPageOversizeIsPageTooLarge(t *testing.T) {
	f := newFakeForge(t)
	f.serve(issuesListBase+"1", "", "["+strings.Repeat(" ", maxBytes), nil)
	_, err := readerForMode(f, guardIssues).IssuesPage(context.Background(), "o/r", 1)
	assertWatchCode(t, err, CodePageTooLarge)
	f.serve(issuesListBase+"1", "", "["+strings.Repeat(" ", maxBytes-2)+"]", nil)
	if res, err := readerForMode(f, guardIssues).IssuesPage(context.Background(), "o/r", 1); err != nil || len(res.Body) != maxBytes {
		t.Fatalf("exactly one MiB must pass: %d %v", len(res.Body), err)
	}
}

func TestViewerReadsUserOnceAndValidatesTheLogin(t *testing.T) {
	f := newFakeForge(t)
	f.serve("user", "", `{"login":"Wstein","type":"User"}`, nil)
	for _, mode := range []guardMode{guardIssues, guardStatus} {
		f.log = nil
		got, err := readerForMode(f, mode).Viewer(context.Background())
		if err != nil || got != "Wstein" || len(f.log) != 1 || f.log[0] != "user" {
			t.Fatalf("mode %d: %q %v %v", mode, got, err, f.log)
		}
	}
	f.serve("user", "", `{"login":"x[bot]\u001b"}`, nil)
	_, err := readerForMode(f, guardIssues).Viewer(context.Background())
	assertWatchCode(t, err, CodeMalformed)
	for _, c := range []struct {
		status int
		code   string
	}{{401, CodeUnauthorized}, {403, CodeForbidden}, {404, CodeForbidden}, {500, CodeHTTP5xx}} {
		c := c
		f.handlers["user"] = func(*http.Request) *http.Response { return forgeResponse(c.status, `{}`, nil) }
		_, err := readerForMode(f, guardIssues).Viewer(context.Background())
		we := assertWatchCode(t, err, c.code)
		if we.Status != c.status {
			t.Errorf("status %d: %d", c.status, we.Status)
		}
	}
	if c := readerForMode(f, guardIssues).TakeCalls(); len(c) != 0 {
		t.Fatalf("%v", c)
	}
}

func TestIssueReadsAreCountedByKind(t *testing.T) {
	f := newFakeForge(t)
	f.serve(issuesListBase+"1", "", `[]`, nil)
	f.serve("user", "", `{"login":"a"}`, nil)
	r := readerForMode(f, guardIssues)
	if _, err := r.IssuesPage(context.Background(), "o/r", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Viewer(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := r.TakeCalls()
	if calls[KindIssues].Requests != 1 || calls[KindUser].Requests != 1 || len(calls) != 2 {
		t.Fatalf("%v", calls)
	}
}

func TestNewIssuesReaderIsIssuesModeWith30SecondRequests(t *testing.T) {
	var sent []string
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent = append(sent, strings.TrimPrefix(r.URL.RequestURI(), "/"))
		return response(200, `[]`), nil
	})
	target := fixtureTarget(t)
	r, cleanup, err := NewIssuesReaderWithTransport(context.Background(), target, "github.com", rt)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if r.mode != guardIssues || r.RequestTimeout() != DefaultRequestTimeout || r.RequestTimeout() != 30*time.Second {
		t.Fatalf("%v %v", r.mode, r.RequestTimeout())
	}
	if _, err := r.IssuesPage(context.Background(), "o/r", 2); err != nil || len(sent) != 1 || sent[0] != issuesListBase+"2" {
		t.Fatalf("%v %v", sent, err)
	}
	// The real guard of the constructor refuses what the mode does not admit.
	if _, err := r.PullListPage(context.Background(), "o/r", 25, 1, ""); err == nil || len(sent) != 1 {
		t.Fatalf("the issues reader sent a watch read: %v %v", sent, err)
	}
}
