// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"net/http"
	"testing"
)

func landReaderFor(f *fakeForge) *WatchReader {
	client, err := newSDKClientGuardMode("github.com", "fixturetoken", f.t.TempDir(), f, guardLand)
	if err != nil {
		f.t.Fatal(err)
	}
	r := newWatchReader(&ghAPI{client: client, host: "github.com"})
	r.mode = guardLand
	return r
}

var landShapes = []string{
	"repos/o/r",
	"repos/o/r/pulls/12",
	"repos/o/r/pulls?state=open&per_page=100&page=1",
	"repos/o/r/pulls?state=open&per_page=100&page=5",
	"repos/o/r/pulls/12/reviews?per_page=100&page=1",
	"repos/o/r/issues/12/events?per_page=100&page=1",
	"repos/o/r/issues/12/events?per_page=100&page=3",
	"repos/o/r/rules/branches/main?per_page=100&page=1",
	"repos/o/r/rules/branches/release%2F1?per_page=100&page=3",
	"repos/o/r/branches/main/protection",
	"repos/o/r/branches/main",
	"repos/o/r/branches/release%2F1",
	"repos/o/r/rulesets/77",
	"repos/o/r/commits/" + "0123456789012345678901234567890123456789" + "/check-runs?per_page=100&page=1",
	"repos/o/r/commits/" + "0123456789012345678901234567890123456789" + "/statuses?per_page=100&page=1",
	"user",
}

var landRefused = []string{
	"repos/o/r/pulls?state=open&per_page=100&page=6",
	"repos/o/r/pulls?state=open&per_page=100&page=0",
	"repos/o/r/pulls?state=all&per_page=100&page=1",
	"repos/o/r/pulls/12/reviews?per_page=100&page=2",
	"repos/o/r/pulls/012/reviews?per_page=100&page=1",
	"repos/o/r/issues/12/events?per_page=100&page=4",
	"repos/o/r/issues/12/events?per_page=100&page=0",
	"repos/o/r/issues/12/comments",
	"repos/o/r/rules/branches/main",
	"repos/o/r/rules/branches/main?per_page=100&page=4",
	"repos/o/r/rules/branches/ma%2Fin/../x?per_page=100&page=1",
	"repos/o/r/rules/branches/a%252Fb?per_page=100&page=1",
	"repos/o/r/rules/branches/-x?per_page=100&page=1",
	"repos/o/r/rules/branches/?per_page=100&page=1",
	"repos/o/r/branches/main/protection/required_status_checks",
	"repos/o/r/branches/main/rename",
	"repos/o/r/branches/main/",
	"repos/o/r/branches/-x",
	"repos/o/r/branches/ma%2Fin/../x",
	"repos/o/r/branches",
	"repos/o/r/rulesets/0x1",
	"repos/o/r/rulesets/77/extra",
	"repos/o/r/git/refs/heads/main",
	"repos/o/r/pulls/12/merge",
	"repos/o/r/pulls/12/comments",
	"repos/o/r/collaborators",
	"repos/../x/pulls/1",
	"user/emails",
	"search/issues?q=x",
}

// The land shapes are admitted in land mode only; every other mode refuses them, and land mode
// refuses neighbours (other pages, other resources, write-adjacent paths).
func TestLandGuardAdmitsExactlyTheLandShapes(t *testing.T) {
	for _, mode := range []guardMode{guardOneShot, guardWatch, guardPulls, guardStatus, guardIssues, guardLand} {
		g := guardedTransport{host: "api.github.com", prefix: "/", mode: mode}
		for _, path := range landShapes {
			req, _ := http.NewRequest("GET", "https://api.github.com/"+path, nil)
			allowedNow := g.allowed(req)
			if mode == guardLand && !allowedNow {
				t.Errorf("land mode refused %s", path)
			}
			if mode != guardLand && mode != guardOneShot && landOnly(path) && allowedNow {
				t.Errorf("mode %d admitted the land-only shape %s", mode, path)
			}
		}
		if mode == guardLand {
			for _, path := range landRefused {
				req, _ := http.NewRequest("GET", "https://api.github.com/"+path, nil)
				if g.allowed(req) {
					t.Errorf("land mode admitted %s", path)
				}
			}
			for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "HEAD"} {
				req, _ := http.NewRequest(method, "https://api.github.com/repos/o/r/pulls/12", nil)
				if g.allowed(req) {
					t.Errorf("land mode admitted %s", method)
				}
			}
		}
	}
}

// landOnly lists the shapes no other mode reads.
func landOnly(path string) bool {
	switch path {
	case "repos/o/r/pulls/12/reviews?per_page=100&page=1", "repos/o/r/issues/12/events?per_page=100&page=1", "repos/o/r/rules/branches/main?per_page=100&page=1",
		"repos/o/r/rules/branches/release%2F1?per_page=100&page=3", "repos/o/r/branches/main/protection", "repos/o/r/rulesets/77",
		"repos/o/r/pulls?state=open&per_page=100&page=5":
		return true
	}
	return false
}

func TestLandReaderReadsAndClassifiesFailures(t *testing.T) {
	f := newFakeForge(t)
	f.handlers["repos/o/r/rulesets/77"] = func(*http.Request) *http.Response { return forgeResponse(200, `{"id":77}`, nil) }
	f.handlers["repos/o/r/branches/main/protection"] = func(*http.Request) *http.Response {
		return forgeResponse(404, `{"message":"Branch not protected"}`, nil)
	}
	f.handlers["repos/o/r/rules/branches/main?per_page=100&page=1"] = func(*http.Request) *http.Response {
		return forgeResponse(403, `{"message":"Upgrade to GitHub Pro"}`, nil)
	}
	r := landReaderFor(f)
	ctx := context.Background()
	res, err := r.LandRead(ctx, "repos/o/r/rulesets/77")
	if err != nil || string(res.Body) != `{"id":77}` {
		t.Fatalf("%v %s", err, res.Body)
	}
	_, err = r.LandRead(ctx, "repos/o/r/branches/main/protection")
	if we, ok := err.(*WatchError); !ok || we.Status != 404 {
		t.Fatalf("404 must keep its status: %v", err)
	}
	_, err = r.LandRead(ctx, "repos/o/r/rules/branches/main?per_page=100&page=1")
	if we, ok := err.(*WatchError); !ok || we.Status != 403 || we.Code != CodeForbidden {
		t.Fatalf("403 must keep its status: %v", err)
	}
	if _, err = r.LandRead(ctx, "repos/o/r/pulls/12/merge"); err == nil {
		t.Fatal("a non-land endpoint must be refused before any request")
	}
	if n := len(f.log); n != 3 {
		t.Fatalf("requests %d %v", n, f.log)
	}
	// a reader of another mode refuses LandRead before a request
	other := newFakeForge(t)
	other.handlers["repos/o/r/pulls/12"] = func(*http.Request) *http.Response { return forgeResponse(200, `{}`, nil) }
	g := pullsReaderFor(other, "github.com")
	if _, err := g.LandRead(ctx, "repos/o/r/pulls/12"); err == nil {
		t.Fatal("LandRead on a pulls reader must be refused")
	}
}

func TestLandEndpointBuilders(t *testing.T) {
	cases := map[string]func() (string, error){
		"repos/o/r/rules/branches/release%2F1?per_page=100&page=2": func() (string, error) { return LandRulesEndpoint("o/r", "release/1", 2) },
		"repos/o/r/branches/main/protection":                       func() (string, error) { return LandProtectionEndpoint("o/r", "main") },
		"repos/o/r/pulls/12/reviews?per_page=100&page=1":           func() (string, error) { return LandReviewsEndpoint("o/r", 12) },
		"repos/o/r/issues/12/events?per_page=100&page=3":           func() (string, error) { return LandEventsEndpoint("o/r", 12, 3) },
		"repos/o/r/pulls?state=open&per_page=100&page=3":           func() (string, error) { return LandOpenPullsEndpoint("o/r", 3) },
		"repos/o/r/rulesets/77":                                    func() (string, error) { return LandRulesetEndpoint("o/r", 77) },
	}
	for want, build := range cases {
		if got, err := build(); err != nil || got != want {
			t.Errorf("%q %v want %q", got, err, want)
		}
	}
	if _, err := LandRulesEndpoint("o/r", "-x", 1); err == nil {
		t.Error("invalid branch accepted")
	}
	if _, err := LandOpenPullsEndpoint("o/r", 6); err == nil {
		t.Error("page 6 accepted")
	}
	if _, err := LandRulesEndpoint("o/r", "main", 4); err == nil {
		t.Error("rules page 4 accepted")
	}
	if _, err := LandEventsEndpoint("o/r", 1, 4); err == nil {
		t.Error("events page 4 accepted")
	}
	if _, err := LandReviewsEndpoint("../x", 1); err == nil {
		t.Error("invalid repo accepted")
	}
	if _, err := LandEventsEndpoint("o/r", 0, 1); err == nil {
		t.Error("number 0 accepted")
	}
}

// A path with an escaped segment (RawPath) is admitted only where a shape of that mode needs it.
func TestEscapedPathsAreAdmittedPerMode(t *testing.T) {
	cases := map[string]map[guardMode]bool{
		"repos/o/r/rules/branches/release%2F1?per_page=100&page=1": {guardLand: true},
		"repos/o/r/branches/release%2F1":                           {guardWatch: true, guardPulls: true, guardStatus: true, guardLand: true},
		"repos/o/r/pulls/1%32":                                     {},
	}
	for path, want := range cases {
		for _, mode := range []guardMode{guardOneShot, guardWatch, guardPulls, guardStatus, guardIssues, guardLand} {
			g := guardedTransport{host: "api.github.com", prefix: "/", mode: mode}
			req, err := http.NewRequest("GET", "https://api.github.com/"+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := g.allowed(req); got != want[mode] {
				t.Errorf("%s mode %d: allowed=%v want %v", path, mode, got, want[mode])
			}
		}
	}
}

// Only the modes that admit watch shapes may carry an escaped path; the check runs before the
// endpoint allowlist, so it must refuse an escaped-form request even for a shape the mode admits.
func TestRawPathRefusedOutsideWatchAndLandModes(t *testing.T) {
	for _, mode := range []guardMode{guardOneShot, guardIssues} {
		g := guardedTransport{host: "api.github.com", prefix: "/", mode: mode}
		req, err := http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !g.allowed(req) {
			t.Fatalf("mode %d: the plain shape must be admitted", mode)
		}
		req.URL.RawPath = "/repos/o/r"
		if g.allowed(req) {
			t.Errorf("mode %d: a request carrying RawPath must be refused", mode)
		}
	}
}

// A reader of a non-land mode refuses a land-only endpoint in fetch itself, with the watch
// error, before any request is built.
func TestFetchRefusesLandEndpointsOnOtherReaders(t *testing.T) {
	f := newFakeForge(t)
	r := pullsReaderFor(f, "github.com")
	_, err := r.fetch(context.Background(), KindDetail, "repos/o/r/rulesets/77", "", false)
	we, ok := err.(*WatchError)
	if !ok || we.Code != CodeMalformed || we.Message != "endpoint is not an allowed watch read" {
		t.Fatalf("want the fetch allowlist refusal, got %#v", err)
	}
	if len(f.log) != 0 {
		t.Fatalf("requests %v", f.log)
	}
}
