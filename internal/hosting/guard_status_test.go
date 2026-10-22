// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

var guardModes = []struct {
	name string
	mode guardMode
}{{"oneShot", guardOneShot}, {"watch", guardWatch}, {"pulls", guardPulls}, {"status", guardStatus}}

// The full guard table of the four modes (Spec 037 amendment of Spec 022 FR-005). Columns:
// one-shot, watch, pulls, status. Every column is asserted for every row so that a widening or
// narrowing of any single mode is caught.
func TestGuardTableOfAllFourModes(t *testing.T) {
	sha40 := strings.Repeat("a", 40)
	runs := "repos/o/r/actions/workflows/release.yml/runs?head_sha=" + sha40 + "&per_page=100&page="
	cases := []struct {
		path                       string
		oneShot, watch, pulls, sta bool
	}{
		// watch shapes: watch, pulls and status; not one-shot
		{"repos/a/b/pulls?state=open&per_page=25&page=1", false, true, true, true},
		{"repos/a/b/pulls?state=open&per_page=12&page=167", false, true, true, true},
		{"repos/a/b/pulls?state=open&per_page=25&page=81", false, false, false, false},
		{"repos/a/b/pulls?state=open&per_page=100&page=1", false, false, false, false},
		{"repos/a/b/pulls/3/commits?per_page=100&page=3", false, true, true, true},
		{"rate_limit", false, true, true, true},
		{"repos/a/b/commits/" + sha40, false, true, true, true},
		{"repos/a/b/branches/main", false, true, true, true},
		{"repos/a/b/commits/" + sha40 + "/check-runs?per_page=100&page=1", true, true, true, true},
		{"repos/a/b/commits/" + sha40 + "/statuses?per_page=100&page=1", true, true, true, true},
		// pulls/N is a one-shot shape, so every mode admits it
		{"repos/a/b/pulls/3", true, true, true, true},
		// fixed release, tags, runs and repository shapes: admitted by every mode already
		{"repos/o/r/releases?per_page=100&page=1", true, true, true, true},
		{"repos/o/r/releases?per_page=100&page=10", true, true, true, true},
		{"repos/o/r/tags?per_page=100&page=1", true, true, true, true},
		{"repos/o/r/tags?per_page=100&page=10", true, true, true, true},
		{runs + "1", true, true, true, true},
		{runs + "10", true, true, true, true},
		{"repos/o/r", true, true, true, true},
		// the same shapes outside their fixed form are refused by every mode
		{"repos/o/r/releases?per_page=99&page=1", false, false, false, false},
		{"repos/o/r/releases?per_page=100&page=11", false, false, false, false},
		{"repos/o/r/releases?per_page=100&page=1&x=1", false, false, false, false},
		{"repos/o/r/releases?page=1&per_page=100", false, false, false, false},
		{"repos/o/r/tags?per_page=100&page=0", false, false, false, false},
		{"repos/o/r/releases%2Fx?per_page=100&page=1", false, false, false, false},
		{"repos/o/r/releases/1", false, false, false, false},
		{"repos/o/r/actions/workflows/release.yml/runs?head_sha=abc&per_page=100&page=1", false, false, false, false},
		{"repos/o/r/actions/workflows/../runs?head_sha=" + sha40 + "&per_page=100&page=1", false, false, false, false},
		{"repos/o/r/issues", false, false, false, false},
		{"repos/o/r/pulls?state=all&per_page=25&page=1", false, false, false, false},
		{"orgs/o/repos", false, false, false, false},
		{"user", false, false, false, false},
		// search: pulls only; status refuses it
		{"search/issues?q=user%3Aa+is%3Apr+is%3Aopen+archived%3Afalse&per_page=100&page=1&sort=created&order=asc", false, false, true, false},
		{"search/issues", false, false, false, false},
		{"search/code?q=a", false, false, false, false},
	}
	for _, c := range cases {
		for i, m := range guardModes {
			want := []bool{c.oneShot, c.watch, c.pulls, c.sta}[i]
			g := guardedTransport{next: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil }), host: "api.github.com", prefix: "/", mode: m.mode}
			req, err := http.NewRequest("GET", "https://api.github.com/"+c.path, nil)
			if err != nil {
				continue
			}
			if got := g.allowed(req); got != want {
				t.Errorf("%s %s: allowed=%v want %v", m.name, c.path, got, want)
			}
		}
	}
}

func TestGuardStatusAdmitsEscapedBranchLikeWatchOthersKeepTheirRule(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/a/b/branches/feat%2Fx", nil)
	for _, m := range guardModes {
		want := m.mode != guardOneShot
		g := guardedTransport{host: "api.github.com", prefix: "/", mode: m.mode}
		if got := g.allowed(req); got != want {
			t.Errorf("%s: %v", m.name, got)
		}
	}
}

// Method, body, host, scheme and redirect-style URL parts are refused in every mode.
func TestGuardRefusesNonReadRequestsInEveryMode(t *testing.T) {
	for _, m := range guardModes {
		g := guardedTransport{host: "api.github.com", prefix: "/", mode: m.mode}
		for name, mk := range map[string]func() *http.Request{
			"POST": func() *http.Request {
				r, _ := http.NewRequest("POST", "https://api.github.com/rate_limit", nil)
				return r
			},
			"HEAD": func() *http.Request {
				r, _ := http.NewRequest("HEAD", "https://api.github.com/repos/o/r", nil)
				return r
			},
			"other": func() *http.Request { r, _ := http.NewRequest("GET", "https://example.com/repos/o/r", nil); return r },
			"http":  func() *http.Request { r, _ := http.NewRequest("GET", "http://api.github.com/repos/o/r", nil); return r },
			"fragment": func() *http.Request {
				r, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r#x", nil)
				return r
			},
			"userinfo": func() *http.Request {
				r, _ := http.NewRequest("GET", "https://u@api.github.com/repos/o/r", nil)
				return r
			},
			"body": func() *http.Request {
				r, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", strings.NewReader("x"))
				return r
			},
			"forcequer": func() *http.Request {
				r, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r?", nil)
				return r
			},
		} {
			if g.allowed(mk()) {
				t.Errorf("%s admitted %s", m.name, name)
			}
		}
	}
}

func TestStatusReaderGuardAndModeRefuseSearch(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[searchPath("wstein", 1)] = func(*http.Request) *http.Response { return response(200, searchBody(0, false)) }
	f.handlers["repos/o/r/releases?per_page=100&page=1"] = func(*http.Request) *http.Response { return response(200, `[]`) }
	client, err := newSDKClientGuardMode("github.com", "fixturetoken", f.t.TempDir(), f, guardStatus)
	if err != nil {
		t.Fatal(err)
	}
	r := newWatchReader(&ghAPI{client: client, host: "github.com"})
	r.mode = guardStatus
	if _, err := r.SearchOwnerPage(context.Background(), "wstein", 1); err == nil {
		t.Fatal("status reader searched")
	}
	ep, _ := SearchEndpoint("wstein", 1)
	if _, err := r.fetch(context.Background(), KindSearch, ep, "", true); err == nil {
		t.Fatal("fetch searched in status mode")
	}
	// Defence in depth: even a permissive mode flag on the reader cannot make the status
	// transport send the search.
	r.mode = guardPulls
	if _, err := r.fetch(context.Background(), KindSearch, ep, "", true); err == nil {
		t.Fatal("the status guard sent the search")
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
}

func TestNewStatusReaderIsStatusModeWith15SecondRequests(t *testing.T) {
	rt := roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, `{}`), nil })
	r, cleanup, err := NewStatusReaderWithTransport(context.Background(), t.TempDir(), "github.com", rt)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if r.mode != guardStatus || r.RequestTimeout() != StatusRequestTimeout {
		t.Fatalf("%v %v", r.mode, r.RequestTimeout())
	}
	w, wc, err := NewWatchReaderWithTransport(context.Background(), t.TempDir(), "github.com", rt)
	if err != nil {
		t.Fatal(err)
	}
	defer wc()
	if w.mode != guardWatch || w.RequestTimeout() != DefaultRequestTimeout {
		t.Fatalf("the watch reader keeps its mode and 30 s: %v %v", w.mode, w.RequestTimeout())
	}
}
