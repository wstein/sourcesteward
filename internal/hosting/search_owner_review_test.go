// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Recorded public responses (see testdata/README.md) decode through ParseOwnerSearch.
func TestParseOwnerSearchRecordedFixtures(t *testing.T) {
	user, err := ParseOwnerSearch(readFixture(t, "search_user.json"), "octocat", "github.com")
	if err != nil || !reflect.DeepEqual(user.Repos, []string{"octocat/Hello-World"}) || user.Ignored != 0 || user.Incomplete || user.TotalCount < 1 {
		t.Fatalf("%+v %v", user, err)
	}
	org, err := ParseOwnerSearch(readFixture(t, "search_org.json"), "github", "github.com")
	if err != nil || !reflect.DeepEqual(org.Repos, []string{"github/email_reply_parser", "github/pages-gem"}) || org.Ignored != 0 {
		t.Fatalf("%+v %v", org, err)
	}
	// An organisation response read for another owner yields nothing usable.
	foreign, err := ParseOwnerSearch(readFixture(t, "search_org.json"), "octocat", "github.com")
	if err != nil || len(foreign.Repos) != 0 || foreign.Ignored != 3 {
		t.Fatalf("%+v %v", foreign, err)
	}
	empty, err := ParseOwnerSearch(readFixture(t, "search_empty.json"), "octocat", "github.com")
	if err != nil || len(empty.Repos) != 0 || empty.TotalCount != 0 || empty.Incomplete {
		t.Fatalf("%+v %v", empty, err)
	}
}

func TestDiscoverOwnerRecordedIncompleteAndTruncated(t *testing.T) {
	for file, want := range map[string]string{"search_incomplete.json": CodeSearchIncomplete, "search_truncated.json": CodeSearchTooMany, "search_user.json": CodeSearchTooMany, "search_empty.json": ""} {
		f := newFakeForge(t)
		owner := "octocat"
		if file == "search_truncated.json" {
			owner = "github"
		}
		body := string(readFixture(t, file))
		f.handlers[searchPath(owner, 1)] = func(*http.Request) *http.Response { return response(200, body) }
		d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), owner)
		if d.Code != want {
			t.Errorf("%s: code %q want %q (%+v)", file, d.Code, want, d)
		}
		if file != "search_empty.json" && len(d.Repos) == 0 {
			t.Errorf("%s: repositories seen must still be returned", file)
		}
	}
}

// F1: every search throttle empties the search bucket, with or without headers.
func TestSearchThrottleEmptiesTheBucket(t *testing.T) {
	future := fmt.Sprint(time.Now().Add(time.Hour).Unix())
	for name, tc := range map[string]struct {
		status int
		header map[string]string
	}{
		"429 bare":                     {429, nil},
		"429 retry-after":              {429, map[string]string{"Retry-After": "30"}},
		"403 retry-after with budget":  {403, map[string]string{"Retry-After": "60", "X-RateLimit-Remaining": "12", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": future}},
		"403 reset in the past":        {403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": "1700000100"}},
		"403 exhausted, future reset":  {403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": future}},
		"403 retry-after without rate": {403, map[string]string{"Retry-After": "90"}},
	} {
		f := newFakeForge(t)
		f.handlers[searchPath("one", 1)] = func(*http.Request) *http.Response { return forgeResponse(tc.status, `{}`, tc.header) }
		f.handlers[searchPath("two", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
		ds := pullsReaderFor(f, "github.com").DiscoverOwners(context.Background(), []string{"one", "two"})
		if len(ds) != 2 || ds[0].Code != CodeRateLimited || ds[1].Code != CodeRateLimited || len(f.log) != 1 {
			t.Errorf("%s: %+v log=%v", name, ds, f.log)
		}
	}
}

// The bucket is empty for at least a minute, and for longer when Retry-After or the reset say so.
func TestSearchThrottleWindow(t *testing.T) {
	for name, tc := range map[string]struct {
		header   map[string]string
		min, max time.Duration
	}{
		"floor":       {map[string]string{"Retry-After": "5"}, 59 * time.Second, 70 * time.Second},
		"retry-after": {map[string]string{"Retry-After": "600"}, 599 * time.Second, 610 * time.Second},
		"reset":       {map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Limit": "30", "X-RateLimit-Reset": fmt.Sprint(time.Now().Add(300 * time.Second).Unix())}, 298 * time.Second, 310 * time.Second},
	} {
		f := newFakeForge(t)
		f.handlers[searchPath("one", 1)] = func(*http.Request) *http.Response { return forgeResponse(429, `{}`, tc.header) }
		r := pullsReaderFor(f, "github.com")
		r.DiscoverOwner(context.Background(), "one")
		r.mu.Lock()
		w := time.Until(r.searchLast.Reset)
		known, rem := r.searchLast.Known, r.searchLast.Remaining
		r.mu.Unlock()
		if !known || rem != 0 || w < tc.min || w > tc.max {
			t.Errorf("%s: known=%v remaining=%d window=%v", name, known, rem, w)
		}
	}
}

// F3: the real constructor builds a reader whose guard and mode admit the search; the
// request reaches the transport.
func TestNewPullsReaderWithTransportSearches(t *testing.T) {
	d := t.TempDir()
	target := filepath.Join(d, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(d, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nif [ \"$1\" = \"--version\" ];then echo 'gh version 2.102.0';exit 0;fi\necho testsecret\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_PATH", script)
	var seen []string
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = append(seen, req.URL.RequestURI())
		return response(200, searchBody(0, false)), nil
	})
	r, cleanup, err := NewPullsReaderWithTransport(context.Background(), target, "github.com", rt)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := r.SearchOwnerPage(context.Background(), "wstein", 1); err != nil {
		t.Fatal(err)
	}
	if d := r.DiscoverOwner(context.Background(), "wstein"); d.Code != "" {
		t.Fatalf("%+v", d)
	}
	if len(seen) != 2 || seen[0] != "/"+searchPath("wstein", 1) {
		t.Fatalf("%v", seen)
	}
	// The watch constructor over the same transport cannot search.
	w, cw, err := NewWatchReaderWithTransport(context.Background(), target, "github.com", rt)
	if err != nil {
		t.Fatal(err)
	}
	defer cw()
	if _, err := w.SearchOwnerPage(context.Background(), "wstein", 1); err == nil || len(seen) != 2 {
		t.Fatalf("watch reader searched: %v %v", err, seen)
	}
}

// Defence in depth: the reader refuses the search by its own mode even over a client whose
// guard would admit it, both through SearchOwnerPage and through fetch.
func TestReaderModeRefusesSearchOverAPermissiveGuard(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
	r := pullsReaderFor(f, "github.com")
	r.mode = guardWatch
	if _, err := r.SearchOwnerPage(context.Background(), "wstein", 1); err == nil {
		t.Fatal("SearchOwnerPage ignored the reader mode")
	}
	ep, _ := SearchEndpoint("wstein", 1)
	if _, err := r.fetch(context.Background(), KindSearch, ep, "", true); err == nil {
		t.Fatal("fetch ignored the reader mode")
	}
	if d := r.DiscoverOwner(context.Background(), "wstein"); d.Code != CodeMalformed || d.Message != "search is not allowed for this reader" {
		t.Fatalf("%+v", d)
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
	// An invalid owner on a proper reader keeps its own message.
	if d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "a+b"); d.Message != "invalid owner" {
		t.Fatalf("%+v", d)
	}
}

// F4: GitHub login rules.
func TestValidOwnerFollowsGitHubLoginRules(t *testing.T) {
	for _, s := range []string{"a", "a1", "a-b", "a-b-c", "9-9", strings.Repeat("a", 39)} {
		if !ValidOwner(s) {
			t.Errorf("%q refused", s)
		}
	}
	for _, s := range []string{"-a", "a-", "a--b", "-", "--", "a--", "-a-b", strings.Repeat("a", 38) + "-"} {
		if ValidOwner(s) {
			t.Errorf("%q admitted", s)
		}
		if _, err := SearchEndpoint(s, 1); err == nil {
			t.Errorf("%q generated", s)
		}
		if searchReadEndpoint(searchPath(s, 1)) {
			t.Errorf("%q matched by the guard", s)
		}
	}
}

// A repository seen on two pages under two spellings is one name, in the first spelling.
func TestDiscoverOwnerDedupesAcrossPages(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response {
		return forgeResponse(200, searchBody(101, false, searchItem("https://api.github.com/repos/wstein/Repo", 1)), map[string]string{"Link": `<https://api.github.com/search/issues?q=x&page=2>; rel="next"`})
	}
	f.handlers[searchPath("wstein", 2)] = func(*http.Request) *http.Response {
		return response(200, searchBody(101, false, searchItem("https://api.github.com/repos/WSTEIN/repo", 2)))
	}
	d := pullsReaderFor(f, "github.com").DiscoverOwner(context.Background(), "wstein")
	if d.Code != "" || !reflect.DeepEqual(d.Repos, []string{"wstein/Repo"}) || len(f.log) != 2 {
		t.Fatalf("%+v %v", d, f.log)
	}
}
