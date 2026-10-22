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

const searchBase = "search/issues?q=user%3A"
const searchTail = "+is%3Apr+is%3Aopen+archived%3Afalse&per_page=100&page="
const searchOrder = "&sort=created&order=asc"

func searchPath(owner string, page int) string {
	return fmt.Sprintf("%s%s%s%d%s", searchBase, owner, searchTail, page, searchOrder)
}

func pullsReaderFor(f *fakeForge, host string) *WatchReader {
	client, err := newSDKClientGuardMode(host, "fixturetoken", f.t.TempDir(), f, guardPulls)
	if err != nil {
		f.t.Fatal(err)
	}
	r := newWatchReader(&ghAPI{client: client, host: host})
	r.mode = guardPulls
	return r
}

func TestValidOwner(t *testing.T) {
	good := []string{"a", "wstein", "A-b-9", strings.Repeat("a", 39)}
	bad := []string{"", strings.Repeat("a", 40), strings.Repeat("a", 256), "x+is:private", "a b", "a%0ab", "a\nb", "a:b", "a+b", "a/b", "ä", "a‮b", "a[bot]", "a%41", "a&b", "a#b", "a?b", "a\x00", "a,b", "a*"}
	for _, s := range good {
		if !ValidOwner(s) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range bad {
		if ValidOwner(s) {
			t.Errorf("%q admitted", s)
		}
	}
}

func TestSearchEndpointGeneratorIsExact(t *testing.T) {
	got, err := SearchEndpoint("wstein", 3)
	want := "search/issues?q=user%3Awstein+is%3Apr+is%3Aopen+archived%3Afalse&per_page=100&page=3&sort=created&order=asc"
	if err != nil || got != want {
		t.Fatalf("%q %v", got, err)
	}
	for _, p := range []int{0, 11, -1} {
		if _, err := SearchEndpoint("wstein", p); err == nil {
			t.Errorf("page %d admitted", p)
		}
	}
	for _, o := range []string{"x+is:private", "a%0ab", "ä", strings.Repeat("a", 256), ""} {
		if _, err := SearchEndpoint(o, 1); err == nil {
			t.Errorf("owner %q admitted", o)
		}
	}
}

// The guard admits exactly one search shape and only in pulls mode.
func TestGuardSearchShapeOnlyInPullsMode(t *testing.T) {
	cases := []struct {
		name, path string
		want       bool
	}{
		{"shape", searchPath("wstein", 1), true},
		{"page10", searchPath("wstein", 10), true},
		{"page11", searchPath("wstein", 11), false},
		{"page0", searchPath("wstein", 0), false},
		{"page01", searchPath("wstein", 1)[:len(searchPath("wstein", 1))-len(searchOrder)-1] + "01" + searchOrder, false},
		{"owner injection plus", searchPath("x+is:private", 1), false},
		{"owner injection encoded", searchPath("x+is%3Aprivate", 1), false},
		{"owner newline", searchPath("a%0ab", 1), false},
		{"owner encoded letter", searchPath("%41", 1), false},
		{"owner unicode", searchPath("ä", 1), false},
		{"owner utf8 escaped", searchPath("%C3%A4", 1), false},
		{"owner 256", searchPath(strings.Repeat("a", 256), 1), false},
		{"owner 40", searchPath(strings.Repeat("a", 40), 1), false},
		{"owner 39", searchPath(strings.Repeat("a", 39), 1), true},
		{"owner empty", searchPath("", 1), false},
		{"lowercase escapes", strings.Replace(searchPath("a", 1), "%3A", "%3a", 1), false},
		{"extra param", searchPath("a", 1) + "&advanced_search=true", false},
		{"extra q", searchPath("a", 1) + "&q=user%3Ab", false},
		{"prefix param", "search/issues?x=1&q=user%3Aa" + searchTail[:len(searchTail)] + "1" + searchOrder, false},
		{"reordered", "search/issues?per_page=100&q=user%3Aa+is%3Apr+is%3Aopen+archived%3Afalse&page=1&sort=created&order=asc", false},
		{"sort updated", strings.Replace(searchPath("a", 1), "sort=created", "sort=updated", 1), false},
		{"order desc", strings.Replace(searchPath("a", 1), "order=asc", "order=desc", 1), false},
		{"per_page 50", strings.Replace(searchPath("a", 1), "per_page=100", "per_page=50", 1), false},
		{"space plus", strings.Replace(searchPath("a", 1), "+is%3Aopen", "+is%3Aopen+is%3Aprivate", 1), false},
		{"no archived", "search/issues?q=user%3Aa+is%3Apr+is%3Aopen&per_page=100&page=1&sort=created&order=asc", false},
		{"is:issue", "search/issues?q=user%3Aa+is%3Aissue+is%3Aopen+archived%3Afalse&per_page=100&page=1&sort=created&order=asc", false},
		{"other search", "search/code?q=user%3Aa", false},
		{"search repos", "search/repositories?q=user%3Aa", false},
		{"search bare", "search/issues", false},
		{"trailing slash", "search/issues/?q=a", false},
		{"dot segments", "search/../search/issues?q=a", false},
	}
	for _, c := range cases {
		for mode, want := range map[guardMode]bool{guardOneShot: false, guardWatch: false, guardPulls: c.want} {
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

// Mutants of the mode wiring: neither the legacy watch field nor any other path
// into watch or one-shot mode may reach the search shape.
func TestGuardSearchUnreachableFromWatchAndOneShotVariants(t *testing.T) {
	ok := searchPath("wstein", 1)
	variants := map[string]guardedTransport{
		"zero value":   {host: "api.github.com", prefix: "/"},
		"legacy watch": {host: "api.github.com", prefix: "/", watch: true},
		"watch mode":   {host: "api.github.com", prefix: "/", mode: guardWatch},
		"watch+legacy": {host: "api.github.com", prefix: "/", mode: guardWatch, watch: true},
	}
	for name, g := range variants {
		req, _ := http.NewRequest("GET", "https://api.github.com/"+ok, nil)
		if g.allowed(req) {
			t.Errorf("%s reached the search shape", name)
		}
	}
	if !admitsSearch(guardPulls) || admitsSearch(guardWatch) || admitsSearch(guardOneShot) {
		t.Fatal("admitsSearch must be true for pulls mode only")
	}
	// The pulls guard stays GET-only, https-only and bound to the API host.
	g := guardedTransport{host: "api.github.com", prefix: "/", mode: guardPulls}
	for name, build := range map[string]func() *http.Request{
		"post":   func() *http.Request { r, _ := http.NewRequest("POST", "https://api.github.com/"+ok, nil); return r },
		"http":   func() *http.Request { r, _ := http.NewRequest("GET", "http://api.github.com/"+ok, nil); return r },
		"host":   func() *http.Request { r, _ := http.NewRequest("GET", "https://evil.example/"+ok, nil); return r },
		"head":   func() *http.Request { r, _ := http.NewRequest("HEAD", "https://api.github.com/"+ok, nil); return r },
		"userin": func() *http.Request { r, _ := http.NewRequest("GET", "https://u@api.github.com/"+ok, nil); return r },
		"frag":   func() *http.Request { r, _ := http.NewRequest("GET", "https://api.github.com/"+ok+"#x", nil); return r },
		"prefix": func() *http.Request {
			r, _ := http.NewRequest("GET", "https://api.github.com/api/v3/"+ok, nil)
			return r
		},
	} {
		if g.allowed(build()) {
			t.Errorf("%s admitted", name)
		}
	}
	if req, _ := http.NewRequest("GET", "https://api.github.com/"+ok, nil); !g.allowed(req) {
		t.Fatal("pulls mode must admit the shape")
	}
	// Enterprise prefix.
	ge := guardedTransport{host: "ghe.example.com", prefix: "/api/v3/", mode: guardPulls}
	req, _ := http.NewRequest("GET", "https://ghe.example.com/api/v3/"+ok, nil)
	if !ge.allowed(req) {
		t.Fatal("enterprise prefix refused")
	}
}

// The reader of watch mode refuses the search read before any request is made.
func TestWatchReaderCannotSearch(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, `{}`) }
	r := f.reader("github.com")
	_, err := r.SearchOwnerPage(context.Background(), "wstein", 1)
	assertWatchCode(t, err, CodeMalformed)
	if len(f.log) != 0 {
		t.Fatalf("request sent: %v", f.log)
	}
	// Even a hand-built pulls reader over a watch-mode client is stopped by the guard.
	hostile := newWatchReader(r.api)
	hostile.mode = guardPulls
	if _, err := hostile.SearchOwnerPage(context.Background(), "wstein", 1); err == nil {
		t.Fatal("guard let the search through")
	}
	if len(f.log) != 0 {
		t.Fatalf("request reached the forge: %v", f.log)
	}
}

func TestSearchOwnerPageRefusesBadInputBeforeSending(t *testing.T) {
	f := newFakeForge(t)
	r := pullsReaderFor(f, "github.com")
	for _, o := range []string{"x+is:private", "a%0ab", "ä", strings.Repeat("a", 256), ""} {
		_, err := r.SearchOwnerPage(context.Background(), o, 1)
		assertWatchCode(t, err, CodeMalformed)
	}
	if _, err := r.SearchOwnerPage(context.Background(), "a", 11); err == nil {
		t.Fatal("page 11")
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
}

func searchItem(repoURL string, n int) string {
	return fmt.Sprintf(`{"number":%d,"title":"t","repository_url":%q,"pull_request":{"url":"x"}}`, n, repoURL)
}

func searchBody(total int, incomplete bool, items ...string) string {
	return fmt.Sprintf(`{"total_count":%d,"incomplete_results":%v,"items":[%s]}`, total, incomplete, strings.Join(items, ","))
}

func TestParseOwnerSearch(t *testing.T) {
	body := searchBody(4, false,
		searchItem("https://api.github.com/repos/WStein/Alpha", 1),
		searchItem("https://api.github.com/repos/wstein/alpha", 2),
		searchItem("https://api.github.com/repos/wstein/beta", 3),
		searchItem("https://api.github.com/repos/wstein/gamma.git", 4))
	res, err := ParseOwnerSearch([]byte(body), "wstein", "github.com")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Repos, []string{"WStein/Alpha", "wstein/beta", "wstein/gamma.git"}) || res.Ignored != 0 || res.TotalCount != 4 || res.Incomplete {
		t.Fatalf("%+v", res)
	}
}

func TestParseOwnerSearchIgnoresForeignAndMalformedItems(t *testing.T) {
	cases := map[string]string{
		"other owner":        "https://api.github.com/repos/evil/x",
		"other host":         "https://api.evil.example/repos/wstein/x",
		"http":               "http://api.github.com/repos/wstein/x",
		"enterprise prefix":  "https://api.github.com/api/v3/repos/wstein/x",
		"userinfo":           "https://u@api.github.com/repos/wstein/x",
		"query":              "https://api.github.com/repos/wstein/x?a=1",
		"fragment":           "https://api.github.com/repos/wstein/x#f",
		"extra segment":      "https://api.github.com/repos/wstein/x/pulls",
		"no name":            "https://api.github.com/repos/wstein",
		"bad name":           "https://api.github.com/repos/wstein/a%20b",
		"traversal":          "https://api.github.com/repos/wstein/..",
		"empty":              "",
		"owner prefix trick": "https://api.github.com/repos/wstein2/x",
		"port":               "https://api.github.com:444/repos/wstein/x",
	}
	for name, u := range cases {
		body := searchBody(2, false, searchItem(u, 1), searchItem("https://api.github.com/repos/wstein/ok", 2))
		res, err := ParseOwnerSearch([]byte(body), "wstein", "github.com")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !reflect.DeepEqual(res.Repos, []string{"wstein/ok"}) || res.Ignored != 1 {
			t.Errorf("%s: %+v", name, res)
		}
	}
}

func TestParseOwnerSearchEnterpriseHost(t *testing.T) {
	body := searchBody(1, false, searchItem("https://ghe.example.com/api/v3/repos/acme/x", 1), searchItem("https://api.ghe.example.com/repos/acme/y", 2))
	res, err := ParseOwnerSearch([]byte(body), "acme", "ghe.example.com")
	if err != nil || !reflect.DeepEqual(res.Repos, []string{"acme/x"}) || res.Ignored != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	body = searchBody(1, false, searchItem("https://api.acme.ghe.com/repos/acme/x", 1))
	res, err = ParseOwnerSearch([]byte(body), "acme", "acme.ghe.com")
	if err != nil || !reflect.DeepEqual(res.Repos, []string{"acme/x"}) {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestParseOwnerSearchMalformedDocuments(t *testing.T) {
	for name, body := range map[string]string{
		"not json":       `nope`,
		"array":          `[]`,
		"no items":       `{"total_count":0,"incomplete_results":false}`,
		"items null":     `{"total_count":0,"incomplete_results":false,"items":null}`,
		"items object":   `{"total_count":0,"incomplete_results":false,"items":{}}`,
		"negative total": `{"total_count":-1,"incomplete_results":false,"items":[]}`,
		"no total":       `{"incomplete_results":false,"items":[]}`,
		"no incomplete":  `{"total_count":0,"items":[]}`,
		"string total":   `{"total_count":"1","incomplete_results":false,"items":[]}`,
		"item not obj":   `{"total_count":1,"incomplete_results":false,"items":[1]}`,
		"101 items":      searchBody(101, false, strings.Split(strings.Repeat(searchItem("https://api.github.com/repos/wstein/x", 1)+"|", 101), "|")[:101]...),
	} {
		if _, err := ParseOwnerSearch([]byte(body), "wstein", "github.com"); err == nil {
			t.Errorf("%s accepted", name)
		} else {
			assertWatchCode(t, err, CodeMalformed)
		}
	}
	// A result without number or repository_url is an ignored item, not a failure.
	res, err := ParseOwnerSearch([]byte(searchBody(2, false, `{}`, `{"number":0,"repository_url":"https://api.github.com/repos/wstein/x"}`)), "wstein", "github.com")
	if err != nil || len(res.Repos) != 0 || res.Ignored != 2 {
		t.Fatalf("%+v %v", res, err)
	}
}

// pagedSearch serves pages of a fixture search. Each page has per items.
func pagedSearch(f *fakeForge, owner string, total, perPage int, incomplete bool) {
	pages := (total + perPage - 1) / perPage
	if pages == 0 {
		pages = 1
	}
	for p := 1; p <= 10; p++ {
		p := p
		f.handlers[searchPath(owner, p)] = func(*http.Request) *http.Response {
			var items []string
			for i := (p - 1) * perPage; i < p*perPage && i < total; i++ {
				items = append(items, searchItem(fmt.Sprintf("https://api.github.com/repos/%s/r%03d", owner, i), i+1))
			}
			h := map[string]string{"X-RateLimit-Limit": "30", "X-RateLimit-Remaining": "29", "X-RateLimit-Reset": "1700000000", "X-RateLimit-Resource": "search"}
			if p < pages {
				h["Link"] = fmt.Sprintf(`<https://api.github.com/search/issues?q=x&page=%d>; rel="next"`, p+1)
			}
			return forgeResponse(200, searchBody(total, incomplete, items...), h)
		}
	}
}

func TestDiscoverOwnerPagesThroughTheLinkHeader(t *testing.T) {
	f := newFakeForge(t)
	pagedSearch(f, "wstein", 250, 100, false)
	r := pullsReaderFor(f, "github.com")
	d := r.DiscoverOwner(context.Background(), "wstein")
	if d.Code != "" || len(d.Repos) != 250 || d.Owner != "wstein" {
		t.Fatalf("%+v", d)
	}
	if !reflect.DeepEqual(f.log, []string{searchPath("wstein", 1), searchPath("wstein", 2), searchPath("wstein", 3)}) {
		t.Fatalf("%v", f.log)
	}
	if c := r.TakeCalls()[KindSearch]; c.Requests != 3 {
		t.Fatalf("%+v", c)
	}
	// The search bucket never feeds the core assessment.
	if r.LastRateLimit().Known {
		t.Fatalf("search headers leaked into the core state: %+v", r.LastRateLimit())
	}
	for _, e := range f.etags {
		if e != "" {
			t.Fatal("search is unconditional")
		}
	}
}

func TestDiscoverOwnerEmptyIsNotAnError(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
	d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
	if d.Code != "" || len(d.Repos) != 0 || len(f.log) != 1 {
		t.Fatalf("%+v %v", d, f.log)
	}
}

func TestDiscoverOwnerIncompleteTakesPrecedence(t *testing.T) {
	for _, total := range []int{0, 5, 5000} {
		f := newFakeForge(t)
		f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response {
			return response(200, searchBody(total, true, searchItem("https://api.github.com/repos/wstein/a", 1)))
		}
		d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
		if d.Code != CodeSearchIncomplete || !reflect.DeepEqual(d.Repos, []string{"wstein/a"}) {
			t.Fatalf("total %d: %+v", total, d)
		}
	}
}

func TestDiscoverOwnerTooManyResultsStopsAtTenPages(t *testing.T) {
	f := newFakeForge(t)
	pagedSearch(f, "wstein", 1000, 100, false)
	// Pretend more exist: total above the 1000 cap and a next link on page 10.
	for p := 1; p <= 10; p++ {
		p := p
		f.handlers[searchPath("wstein", p)] = func(*http.Request) *http.Response {
			var items []string
			for i := 0; i < 100; i++ {
				items = append(items, searchItem(fmt.Sprintf("https://api.github.com/repos/wstein/r%04d", (p-1)*100+i), 1))
			}
			return forgeResponse(200, searchBody(1500, false, items...), map[string]string{"Link": fmt.Sprintf(`<https://api.github.com/search/issues?q=x&page=%d>; rel="next"`, p+1)})
		}
	}
	d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
	if d.Code != CodeSearchTooMany || len(d.Repos) != 1000 || len(f.log) != 10 {
		t.Fatalf("code=%q repos=%d requests=%d", d.Code, len(d.Repos), len(f.log))
	}
	for _, l := range f.log {
		if strings.Contains(l, "page=11") {
			t.Fatalf("page 11 requested: %v", f.log)
		}
	}
}

func TestDiscoverOwnerTotalAboveCapWithoutNextStillTooMany(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response {
		return response(200, searchBody(1001, false, searchItem("https://api.github.com/repos/wstein/a", 1)))
	}
	d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
	if d.Code != CodeSearchTooMany || len(d.Repos) != 1 {
		t.Fatalf("%+v", d)
	}
}

func TestDiscoverOwnerLinkAnomalies(t *testing.T) {
	for name, link := range map[string]string{
		"skips page":    `<https://api.github.com/search/issues?q=x&page=3>; rel="next"`,
		"same page":     `<https://api.github.com/search/issues?q=x&page=1>; rel="next"`,
		"unusable next": `<::>; rel="next"`,
	} {
		f := newFakeForge(t)
		f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response {
			return forgeResponse(200, searchBody(101, false, searchItem("https://api.github.com/repos/wstein/a", 1)), map[string]string{"Link": link})
		}
		d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
		if d.Code != CodeMalformed || len(f.log) != 1 {
			t.Errorf("%s: %+v %v", name, d, f.log)
		}
	}
}

func TestDiscoverOwnerIgnoredItemsReportMalformed(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response {
		return response(200, searchBody(2, false, searchItem("https://api.github.com/repos/evil/x", 1), searchItem("https://api.github.com/repos/wstein/ok", 2)))
	}
	d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
	if d.Code != CodeMalformed || d.Ignored != 1 || !reflect.DeepEqual(d.Repos, []string{"wstein/ok"}) {
		t.Fatalf("%+v", d)
	}
}

func TestDiscoverOwnerFailureSemantics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		header map[string]string
		body   string
		want   string
	}{
		{"unauthorized", 401, nil, `{"message":"Bad credentials"}`, CodeUnauthorized},
		{"throttle 403", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": "1700000100"}, `{}`, CodeRateLimited},
		{"throttle 429", 429, map[string]string{"Retry-After": "30"}, `{}`, CodeRateLimited},
		{"forbidden", 403, nil, `{"message":"nope"}`, CodeForbidden},
		{"validation 422", 422, nil, `{"message":"Validation Failed","errors":[{"message":"secret"}]}`, CodeMalformed},
		{"5xx", 503, nil, `{}`, CodeHTTP5xx},
		{"not json", 200, nil, `<html>`, CodeMalformed},
	} {
		f := newFakeForge(t)
		f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response { return forgeResponse(tc.status, tc.body, tc.header) }
		d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
		if d.Code != tc.want || len(d.Repos) != 0 || strings.Contains(d.Message, "secret") || strings.Contains(d.Message, "fixturetoken") {
			t.Errorf("%s: %+v", tc.name, d)
		}
	}
}

func TestDiscoverOwnerThrottleMidwayKeepsPartialRepos(t *testing.T) {
	f := newFakeForge(t)
	pagedSearch(f, "wstein", 250, 100, false)
	f.handlers[searchPath("wstein", 2)] = func(*http.Request) *http.Response {
		return forgeResponse(403, `{}`, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": "1700000100", "X-RateLimit-Resource": "search"})
	}
	r := pullsReaderFor(f, "github.com")
	d := r.DiscoverOwner(context.Background(), "wstein")
	if d.Code != CodeRateLimited || len(d.Repos) != 100 || len(f.log) != 2 {
		t.Fatalf("%+v %v", d, f.log)
	}
	if r.LastRateLimit().Known {
		t.Fatal("search throttle fed the core state")
	}
}

func TestDiscoverOwnerRefusesInvalidOwnerWithoutRequest(t *testing.T) {
	f := newFakeForge(t)
	r := pullsReaderFor(f, "github.com")
	for _, o := range []string{"x+is:private", "a%0ab", "ä", strings.Repeat("a", 256)} {
		if d := r.DiscoverOwner(context.Background(), o); d.Code != CodeMalformed {
			t.Errorf("%q: %+v", o, d)
		}
	}
	if len(f.log) != 0 {
		t.Fatal(f.log)
	}
}

func TestDiscoverOwnerOversizedBodyIsPageTooLarge(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, strings.Repeat("x", maxBytes+1)) }
	d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
	if d.Code != CodePageTooLarge {
		t.Fatalf("%+v", d)
	}
}

func TestDiscoverOwnersStopsSendingAfterThrottleAndDedupes(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("one", 1)] = func(*http.Request) *http.Response {
		return forgeResponse(403, `{}`, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": fmt.Sprint(time.Now().Add(time.Hour).Unix()), "X-RateLimit-Resource": "search"})
	}
	f.handlers[searchPath("two", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
	r := pullsReaderFor(f, "github.com")
	ds := r.DiscoverOwners(context.Background(), []string{"one", "ONE", "two"})
	if len(ds) != 2 || ds[0].Owner != "one" || ds[0].Code != CodeRateLimited || ds[1].Owner != "two" || ds[1].Code != CodeRateLimited {
		t.Fatalf("%+v", ds)
	}
	if len(f.log) != 1 {
		t.Fatalf("requests after the throttle: %v", f.log)
	}
}

func TestDiscoverOwnersContinuesWhenBudgetRemains(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("one", 1)] = func(*http.Request) *http.Response {
		return forgeResponse(200, searchBody(1, false, searchItem("https://api.github.com/repos/one/a", 1)), map[string]string{"X-RateLimit-Remaining": "5", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": fmt.Sprint(time.Now().Add(time.Hour).Unix())})
	}
	f.handlers[searchPath("two", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
	ds := pullsReaderFor(f, "github.com").DiscoverOwners(context.Background(), []string{"one", "two"})
	if len(ds) != 2 || ds[0].Code != "" || ds[1].Code != "" || len(f.log) != 2 {
		t.Fatalf("%+v %v", ds, f.log)
	}
}

func TestDiscoverOwnersSkipsWhenSearchBudgetIsKnownEmpty(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("one", 1)] = func(*http.Request) *http.Response {
		return forgeResponse(200, searchBody(0, false), map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": fmt.Sprint(time.Now().Add(time.Hour).Unix())})
	}
	ds := pullsReaderFor(f, "github.com").DiscoverOwners(context.Background(), []string{"one", "two"})
	if ds[0].Code != "" || ds[1].Code != CodeRateLimited || len(f.log) != 1 {
		t.Fatalf("%+v %v", ds, f.log)
	}
}
