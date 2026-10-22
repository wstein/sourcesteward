// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

const issueSearchTail = "+is%3Aissue+is%3Aopen+archived%3Afalse&per_page=100&page="

func issueSearchPath(owner string, page int) string {
	return fmt.Sprintf("%s%s%s%d%s", searchBase, owner, issueSearchTail, page, searchOrder)
}

func issuesReaderFor(f *fakeForge) *WatchReader { return readerForMode(f, guardIssues) }

// Every attack string, through the guard of every mode: only the issues guard admits the one
// issue search shape; the PR search shape is refused there, and the issue search shape is
// refused everywhere else.
func TestGuardIssueSearchShapeOnlyInIssuesMode(t *testing.T) {
	ok := issueSearchPath("wstein", 1)
	cases := []struct {
		name, path string
		want       bool
	}{
		{"shape", ok, true},
		{"page10", issueSearchPath("wstein", 10), true},
		{"page11", issueSearchPath("wstein", 11), false},
		{"page0", issueSearchPath("wstein", 0), false},
		{"page01", issueSearchPath("wstein", 1)[:len(ok)-len(searchOrder)-1] + "01" + searchOrder, false},
		{"owner mixed case", issueSearchPath("WsTeIn", 1), true},
		{"owner 39", issueSearchPath(strings.Repeat("a", 39), 1), true},
		{"owner 40", issueSearchPath(strings.Repeat("a", 40), 1), false},
		{"owner 4096", issueSearchPath(strings.Repeat("a", 4096), 1), false},
		{"owner empty", issueSearchPath("", 1), false},
		{"owner injection plus", issueSearchPath("x+is:private", 1), false},
		{"owner injection encoded plus", issueSearchPath("x%2Bis%3Aprivate", 1), false},
		{"owner colon", issueSearchPath("a:b", 1), false},
		{"owner encoded colon", issueSearchPath("a%3Ab", 1), false},
		{"owner q injection", issueSearchPath("a&q=user%3Ab", 1), false},
		{"owner newline", issueSearchPath("a%0ab", 1), false},
		{"owner encoded letter", issueSearchPath("%41", 1), false},
		{"owner unicode", issueSearchPath("ä", 1), false},
		{"owner utf8 escaped", issueSearchPath("%C3%A4", 1), false},
		{"owner fullwidth", issueSearchPath("ａ", 1), false},
		{"owner leading hyphen", issueSearchPath("-a", 1), false},
		{"owner double hyphen", issueSearchPath("a--b", 1), false},
		{"lowercase escapes", strings.Replace(ok, "%3A", "%3a", 1), false},
		{"extra param", ok + "&advanced_search=true", false},
		{"extra q", ok + "&q=user%3Ab", false},
		{"reordered", "search/issues?per_page=100&q=user%3Aa+is%3Aissue+is%3Aopen+archived%3Afalse&page=1&sort=created&order=asc", false},
		{"sort updated", strings.Replace(ok, "sort=created", "sort=updated", 1), false},
		{"order desc", strings.Replace(ok, "order=asc", "order=desc", 1), false},
		{"per_page 50", strings.Replace(ok, "per_page=100", "per_page=50", 1), false},
		{"extra qualifier", strings.Replace(ok, "+is%3Aopen", "+is%3Aopen+is%3Aprivate", 1), false},
		{"closed", strings.Replace(ok, "is%3Aopen", "is%3Aclosed", 1), false},
		{"archived true", strings.Replace(ok, "archived%3Afalse", "archived%3Atrue", 1), false},
		{"no archived", "search/issues?q=user%3Aa+is%3Aissue+is%3Aopen&per_page=100&page=1&sort=created&order=asc", false},
		{"both kinds", strings.Replace(ok, "is%3Aissue", "is%3Aissue+is%3Apr", 1), false},
		{"other search", "search/code?q=user%3Aa", false},
		{"search bare", "search/issues", false},
		{"trailing slash", "search/issues/?q=a", false},
		{"dot segments", "search/../search/issues?q=a", false},
	}
	for _, c := range cases {
		for mode, want := range map[guardMode]bool{guardOneShot: false, guardWatch: false, guardPulls: false, guardStatus: false, guardIssues: c.want} {
			g := guardedTransport{next: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil }), host: "api.github.com", prefix: "/", mode: mode}
			req, err := http.NewRequest("GET", "https://api.github.com/"+c.path, nil)
			if err != nil {
				continue
			}
			if got := g.allowed(req); got != want {
				t.Errorf("%s mode %d: allowed=%v want %v", c.name, mode, got, want)
			}
		}
	}
}

// The PR search shape is the pulls guard's own: the issues guard refuses it.
func TestGuardPRSearchShapeIsRefusedInIssuesMode(t *testing.T) {
	for mode, want := range map[guardMode]bool{guardOneShot: false, guardWatch: false, guardStatus: false, guardPulls: true, guardIssues: false} {
		g := guardedTransport{next: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil }), host: "api.github.com", prefix: "/", mode: mode}
		req, err := http.NewRequest("GET", "https://api.github.com/"+searchPath("wstein", 1), nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := g.allowed(req); got != want {
			t.Errorf("mode %d: allowed=%v want %v", mode, got, want)
		}
	}
}

func TestIssueSearchPageRefusesBadInputAndOtherReadersBeforeSending(t *testing.T) {
	f := newFakeForge(t)
	r := issuesReaderFor(f)
	for _, o := range []string{"x+is:private", "a:b", "a%0ab", "ä", strings.Repeat("a", 256), ""} {
		_, err := r.IssueSearchPage(context.Background(), o, 1)
		assertWatchCode(t, err, CodeMalformed)
		if d := r.DiscoverIssueOwner(context.Background(), o); d.Code != CodeMalformed {
			t.Errorf("%q: %+v", o, d)
		}
	}
	for _, p := range []int{0, 11} {
		if _, err := r.IssueSearchPage(context.Background(), "a", p); err == nil {
			t.Errorf("page %d", p)
		}
	}
	// The PR owner search stays out of the issues reader, the issue search out of the others.
	if _, err := r.SearchOwnerPage(context.Background(), "wstein", 1); err == nil {
		t.Error("the issues reader searched pull requests")
	}
	for _, mode := range []guardMode{guardPulls, guardStatus, guardWatch, guardOneShot} {
		o := readerForMode(f, mode)
		if _, err := o.IssueSearchPage(context.Background(), "wstein", 1); err == nil {
			t.Errorf("mode %d searched issues", mode)
		}
		if d := o.DiscoverIssueOwner(context.Background(), "wstein"); d.Code != CodeMalformed {
			t.Errorf("mode %d: %+v", mode, d)
		}
		if ds := o.DiscoverIssueOwners(context.Background(), []string{"wstein"}); len(ds) != 1 || ds[0].Code != CodeMalformed {
			t.Errorf("mode %d: %+v", mode, ds)
		}
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
}

// The permissive-guard case: the reader refuses by its own mode as well.
func TestIssueSearchReaderModeRefusesOverAPermissiveGuard(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
	client, err := newSDKClientGuardMode("github.com", "fixturetoken", t.TempDir(), f, guardIssues)
	if err != nil {
		t.Fatal(err)
	}
	r := newWatchReader(&ghAPI{client: client, host: "github.com"})
	for _, mode := range []guardMode{guardPulls, guardStatus, guardWatch, guardOneShot} {
		r.mode = mode
		if _, err := r.IssueSearchPage(context.Background(), "wstein", 1); err == nil {
			t.Errorf("mode %d", mode)
		}
		if _, err := r.fetch(context.Background(), KindSearch, issueSearchPath("wstein", 1), "", true); err == nil {
			t.Errorf("fetch in mode %d", mode)
		}
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
}

func TestIssueSearchRecordedFixtures(t *testing.T) {
	user, err := ParseOwnerSearch(readFixture(t, "issue_search_user.json"), "octocat", "github.com")
	if err != nil || !reflect.DeepEqual(user.Repos, []string{"octocat/Spoon-Knife"}) || user.Ignored != 0 || user.Incomplete || user.TotalCount < 1 {
		t.Fatalf("%+v %v", user, err)
	}
	org, err := ParseOwnerSearch(readFixture(t, "issue_search_org.json"), "github", "github.com")
	if err != nil || !reflect.DeepEqual(org.Repos, []string{"github/markup", "github/email_reply_parser", "github/elastomer-client"}) || org.Ignored != 0 {
		t.Fatalf("%+v %v", org, err)
	}
	// Another owner's recording yields nothing usable: a foreign repository_url is ignored.
	foreign, err := ParseOwnerSearch(readFixture(t, "issue_search_org.json"), "octocat", "github.com")
	if err != nil || len(foreign.Repos) != 0 || foreign.Ignored != 3 {
		t.Fatalf("%+v %v", foreign, err)
	}
	empty, err := ParseOwnerSearch(readFixture(t, "issue_search_empty.json"), "octocat", "github.com")
	if err != nil || len(empty.Repos) != 0 || empty.TotalCount != 0 || empty.Incomplete {
		t.Fatalf("%+v %v", empty, err)
	}
}

func TestDiscoverIssueOwnerRecordedFixtures(t *testing.T) {
	for file, tc := range map[string]struct{ owner, code string }{
		"issue_search_user.json":       {"octocat", CodeSearchTooMany},
		"issue_search_org.json":        {"github", CodeSearchTooMany},
		"issue_search_incomplete.json": {"octocat", CodeSearchIncomplete},
		"issue_search_truncated.json":  {"github", CodeSearchTooMany},
		"issue_search_empty.json":      {"octocat", ""},
	} {
		f := newFakeForge(t)
		body := string(readFixture(t, file))
		f.handlers[issueSearchPath(tc.owner, 1)] = func(*http.Request) *http.Response { return response(200, body) }
		d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), tc.owner)
		if d.Code != tc.code {
			t.Errorf("%s: code %q want %q (%+v)", file, d.Code, tc.code, d)
		}
		if file != "issue_search_empty.json" && len(d.Repos) == 0 {
			t.Errorf("%s: repositories seen must still be returned", file)
		}
	}
}

// pagedIssueSearch serves total results of owner in pages of perPage, one repository each.
func pagedIssueSearch(f *fakeForge, owner string, total, perPage int, incomplete bool) {
	pages := max((total+perPage-1)/perPage, 1)
	for p := 1; p <= 10; p++ {
		f.handlers[issueSearchPath(owner, p)] = func(*http.Request) *http.Response {
			var items []string
			for i := (p - 1) * perPage; i < p*perPage && i < total; i++ {
				items = append(items, searchItem(fmt.Sprintf("https://api.github.com/repos/%s/r%04d", owner, i), i+1))
			}
			h := map[string]string{"X-RateLimit-Limit": "30", "X-RateLimit-Remaining": "29", "X-RateLimit-Reset": "1700000000", "X-RateLimit-Resource": "search"}
			if p < pages {
				h["Link"] = fmt.Sprintf(`<https://api.github.com/search/issues?q=x&page=%d>; rel="next"`, p+1)
			}
			return forgeResponse(200, searchBody(total, incomplete, items...), h)
		}
	}
}

func TestDiscoverIssueOwnerPagesAndUsesOnlyTheSearchForNames(t *testing.T) {
	f := newFakeForge(t)
	pagedIssueSearch(f, "wstein", 250, 100, false)
	r := issuesReaderFor(f)
	d := r.DiscoverIssueOwner(context.Background(), "wstein")
	if d.Code != "" || len(d.Repos) != 250 || d.Owner != "wstein" {
		t.Fatalf("%+v", d)
	}
	if !reflect.DeepEqual(f.log, []string{issueSearchPath("wstein", 1), issueSearchPath("wstein", 2), issueSearchPath("wstein", 3)}) {
		t.Fatalf("%v", f.log)
	}
	calls := r.TakeCalls()
	if calls[KindSearch].Requests != 3 || calls[KindIssues].Requests != 0 {
		t.Fatalf("%+v", calls)
	}
	if r.LastRateLimit().Known {
		t.Fatalf("search headers leaked into the core state: %+v", r.LastRateLimit())
	}
	for _, e := range f.etags {
		if e != "" {
			t.Fatal("search is unconditional")
		}
	}
}

func TestDiscoverIssueOwnerDedupesCaseInsensitivelyAndIgnoresForeign(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response {
		return response(200, searchBody(5, false,
			searchItem("https://api.github.com/repos/wstein/Repo", 1),
			searchItem("https://api.github.com/repos/WSTEIN/repo", 2),
			searchItem("https://api.github.com/repos/other/repo", 3),
			searchItem("https://example.com/repos/wstein/x", 4),
			searchItem("https://api.github.com/repos/wstein/second", 5)))
	}
	d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein")
	if !reflect.DeepEqual(d.Repos, []string{"wstein/Repo", "wstein/second"}) || d.Ignored != 2 || d.Code != CodeMalformed || d.Message != MessageIgnoredResults {
		t.Fatalf("%+v", d)
	}
}

func TestDiscoverIssueOwnerEmptyIsNotAnError(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
	if d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein"); d.Code != "" || len(d.Repos) != 0 {
		t.Fatalf("%+v", d)
	}
}

func TestDiscoverIssueOwnerIncompleteTakesPrecedence(t *testing.T) {
	for _, total := range []int{0, 5, 5000} {
		f := newFakeForge(t)
		f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response {
			return response(200, searchBody(total, true, searchItem("https://api.github.com/repos/wstein/a", 1)))
		}
		d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein")
		if d.Code != CodeSearchIncomplete || !reflect.DeepEqual(d.Repos, []string{"wstein/a"}) {
			t.Fatalf("total %d: %+v", total, d)
		}
	}
}

func TestDiscoverIssueOwnerCapsAtTenPagesAndOneThousand(t *testing.T) {
	f := newFakeForge(t)
	for p := 1; p <= 10; p++ {
		f.handlers[issueSearchPath("wstein", p)] = func(*http.Request) *http.Response {
			var items []string
			for i := 0; i < 100; i++ {
				items = append(items, searchItem(fmt.Sprintf("https://api.github.com/repos/wstein/r%04d", (p-1)*100+i), 1))
			}
			return forgeResponse(200, searchBody(1500, false, items...), map[string]string{"Link": fmt.Sprintf(`<https://api.github.com/search/issues?q=x&page=%d>; rel="next"`, p+1)})
		}
	}
	d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein")
	if d.Code != CodeSearchTooMany || len(d.Repos) != 1000 || len(f.log) != 10 {
		t.Fatalf("code=%q repos=%d requests=%d", d.Code, len(d.Repos), len(f.log))
	}
	for _, l := range f.log {
		if strings.Contains(l, "page=11") {
			t.Fatalf("page 11 requested: %v", f.log)
		}
	}
}

func TestDiscoverIssueOwnerTotalIsTrustedAboveTheCap(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response {
		return response(200, searchBody(1001, false, searchItem("https://api.github.com/repos/wstein/a", 1)))
	}
	d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein")
	if d.Code != CodeSearchTooMany || len(d.Repos) != 1 {
		t.Fatalf("%+v", d)
	}
	f = newFakeForge(t)
	f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response {
		return response(200, searchBody(1000, false, searchItem("https://api.github.com/repos/wstein/a", 1)))
	}
	if d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein"); d.Code != "" {
		t.Fatalf("exactly 1000 is complete: %+v", d)
	}
}

func TestDiscoverIssueOwnerLinkAnomaliesAndFailures(t *testing.T) {
	for name, link := range map[string]string{
		"skips page":    `<https://api.github.com/search/issues?q=x&page=3>; rel="next"`,
		"same page":     `<https://api.github.com/search/issues?q=x&page=1>; rel="next"`,
		"unusable next": `<::>; rel="next"`,
	} {
		f := newFakeForge(t)
		f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response {
			return forgeResponse(200, searchBody(101, false, searchItem("https://api.github.com/repos/wstein/a", 1)), map[string]string{"Link": link})
		}
		d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein")
		if d.Code != CodeMalformed || len(f.log) != 1 {
			t.Errorf("%s: %+v %v", name, d, f.log)
		}
	}
	f := newFakeForge(t)
	f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, strings.Repeat("x", maxBytes+1)) }
	if d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein"); d.Code != CodePageTooLarge {
		t.Fatalf("%+v", d)
	}
	f = newFakeForge(t)
	f.handlers[issueSearchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, `[]`) }
	if d := issuesReaderFor(f).DiscoverIssueOwner(context.Background(), "wstein"); d.Code != CodeMalformed {
		t.Fatalf("%+v", d)
	}
}

func TestDiscoverIssueOwnerThrottleMidwayKeepsPartialReposAndTheCoreStateClean(t *testing.T) {
	f := newFakeForge(t)
	pagedIssueSearch(f, "wstein", 250, 100, false)
	f.handlers[issueSearchPath("wstein", 2)] = func(*http.Request) *http.Response {
		return forgeResponse(403, `{}`, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": "1700000100", "X-RateLimit-Resource": "search"})
	}
	r := issuesReaderFor(f)
	d := r.DiscoverIssueOwner(context.Background(), "wstein")
	if d.Code != CodeRateLimited || len(d.Repos) != 100 || len(f.log) != 2 {
		t.Fatalf("%+v %v", d, f.log)
	}
	if r.LastRateLimit().Known {
		t.Fatal("search throttle fed the core state")
	}
	r.mu.Lock()
	w, rem := time.Until(r.searchLast.Reset), r.searchLast.Remaining
	r.mu.Unlock()
	if rem != 0 || w < 59*time.Second {
		t.Fatalf("the search bucket is not emptied for a minute: %d %v", rem, w)
	}
}

func TestDiscoverIssueOwnersDedupesAndStopsAfterAThrottle(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[issueSearchPath("one", 1)] = func(*http.Request) *http.Response {
		return forgeResponse(429, `{}`, nil)
	}
	f.handlers[issueSearchPath("two", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
	ds := issuesReaderFor(f).DiscoverIssueOwners(context.Background(), []string{"one", "ONE", "two"})
	if len(ds) != 2 || ds[0].Owner != "one" || ds[0].Code != CodeRateLimited || ds[1].Owner != "two" || ds[1].Code != CodeRateLimited || len(f.log) != 1 {
		t.Fatalf("%+v %v", ds, f.log)
	}
}

func TestDiscoverIssueOwnersContinuesWhileBudgetRemainsAndSkipsWhenEmpty(t *testing.T) {
	future := fmt.Sprint(time.Now().Add(time.Hour).Unix())
	for remaining, want := range map[string]string{"5": "", "0": CodeRateLimited} {
		f := newFakeForge(t)
		f.handlers[issueSearchPath("one", 1)] = func(*http.Request) *http.Response {
			return forgeResponse(200, searchBody(1, false, searchItem("https://api.github.com/repos/one/a", 1)), map[string]string{"X-RateLimit-Remaining": remaining, "X-RateLimit-Limit": "30", "X-RateLimit-Reset": future})
		}
		f.handlers[issueSearchPath("two", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
		ds := issuesReaderFor(f).DiscoverIssueOwners(context.Background(), []string{"one", "two"})
		if len(ds) != 2 || ds[0].Code != "" || ds[1].Code != want {
			t.Errorf("remaining %s: %+v %v", remaining, ds, f.log)
		}
	}
}

// The real constructor builds a reader whose guard and mode admit the issue search, and only that.
func TestNewIssuesReaderWithTransportSearches(t *testing.T) {
	var seen []string
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = append(seen, req.URL.RequestURI())
		return response(200, searchBody(0, false)), nil
	})
	target := fixtureTarget(t)
	r, cleanup, err := NewIssuesReaderWithTransport(context.Background(), target, "github.com", rt)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := r.IssueSearchPage(context.Background(), "wstein", 1); err != nil {
		t.Fatal(err)
	}
	if d := r.DiscoverIssueOwner(context.Background(), "wstein"); d.Code != "" {
		t.Fatalf("%+v", d)
	}
	if len(seen) != 2 || seen[0] != "/"+issueSearchPath("wstein", 1) {
		t.Fatalf("%v", seen)
	}
	if _, err := r.SearchOwnerPage(context.Background(), "wstein", 1); err == nil || len(seen) != 2 {
		t.Fatalf("the issues reader searched pull requests: %v %v", err, seen)
	}
	p, cp, err := NewPullsReaderWithTransport(context.Background(), target, "github.com", rt)
	if err != nil {
		t.Fatal(err)
	}
	defer cp()
	if _, err := p.IssueSearchPage(context.Background(), "wstein", 1); err == nil || len(seen) != 2 {
		t.Fatalf("the pulls reader searched issues: %v %v", err, seen)
	}
}
