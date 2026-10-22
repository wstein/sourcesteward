// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"net/http"
	"strings"
	"testing"
)

const sha40 = "0123456789abcdef0123456789abcdef01234567"

func watchGoodEndpoints() []string {
	return []string{
		"repos/a/b/pulls?state=open&per_page=25&page=1",
		"repos/a/b/pulls?state=open&per_page=25&page=80",
		"repos/a/b/pulls?state=open&per_page=12&page=167",
		"repos/a/b/pulls/7",
		"repos/a/b/pulls/7/commits?per_page=100&page=3",
		"repos/a/b/branches/main",
		"repos/a/b/branches/feat%2Fx",
		"repos/a/b/branches/release%2Fv1.2",
		"repos/a/b/branches/feature%2F%C3%BCn%C3%AF",
		"repos/a/b/commits/" + sha40,
		"repos/a/b/commits/" + sha40 + "/check-runs?per_page=100&page=1",
		"repos/a/b/commits/" + sha40 + "/statuses?per_page=100&page=2",
		"rate_limit",
	}
}

func TestWatchEndpointAllowlist(t *testing.T) {
	for _, ep := range watchGoodEndpoints() {
		if !watchReadEndpoint(ep) && !transportReadEndpoint(ep) {
			t.Errorf("refused %s", ep)
		}
	}
	for _, ep := range []string{
		"repos/a/b/pulls?state=open&per_page=25&page=81",
		"repos/a/b/pulls?state=open&per_page=12&page=168",
		"repos/a/b/pulls?state=open&per_page=25&page=0",
		"repos/a/b/pulls?state=open&per_page=100&page=1",
		"repos/a/b/pulls?state=closed&per_page=25&page=1",
		"repos/a/b/pulls?state=open&per_page=25&page=1&x=1",
		"repos/a/b/pulls/7/commits?per_page=100&page=4",
		"repos/a/b/pulls/0/commits?per_page=100&page=1",
		"repos/a/b/pulls/7/commits?per_page=30&page=1",
		"repos/a/b/branches/feat/x",
		"repos/a/b/branches/",
		"repos/a/b/branches/feat%2fx",
		"repos/a/b/branches/..%2F",
		"repos/a/b/branches/%252F",
		"repos/a/b/branches/%2e%2e",
		"repos/a/b/branches/%00",
		"repos/a/b/branches/%5C",
		"repos/a/b/branches/main?x=1",
		"repos/a/b/commits/abc",
		"repos/a/b/commits/" + strings.ToUpper(sha40),
		"repos/a/b/commits/" + sha40 + "/comments",
		"repos/../b/commits/" + sha40,
		"repos/a/../commits/" + sha40,
		"repos/a/../../rate_limit",
		"repos/a/b/branches/main/../../x",
		"rate_limit?x=1",
		"rate_limit/",
		"user",
		"repos/a/b/git/refs",
	} {
		if watchReadEndpoint(ep) {
			t.Errorf("accepted %s", ep)
		}
	}
}

// Watch-only endpoints must stay unreachable for the one-shot guard and RawPath is
// accepted only in watch mode for a segment produced by EncodeBranchSegment.
func TestWatchGuardScope(t *testing.T) {
	for _, watch := range []bool{false, true} {
		calls := 0
		client, err := newSDKClientMode("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			return response(200, `{}`), nil
		}), watch)
		if err != nil {
			t.Fatal(err)
		}
		base := "https://api.github.com/"
		allowedWhenWatch := []string{
			"repos/a/b/pulls?state=open&per_page=25&page=1", "repos/a/b/pulls/7/commits?per_page=100&page=1", "repos/a/b/branches/main", "repos/a/b/branches/feat%2Fx",
			"repos/a/b/branches/feature%2F%C3%BCn%C3%AF", "repos/a/b/commits/" + sha40, "rate_limit",
		}
		for _, ep := range allowedWhenWatch {
			before := calls
			req, _ := http.NewRequest("GET", base+ep, nil)
			res, err := client.Do(req)
			if res != nil {
				res.Body.Close()
			}
			if watch && (err != nil || calls != before+1) {
				t.Errorf("watch refused %s: %v", ep, err)
			}
			if !watch && (err == nil || calls != before) {
				t.Errorf("one-shot guard accepted watch endpoint %s", ep)
			}
		}
		for _, ep := range []string{
			"repos/a/b/branches/..%2F", "repos/a/b/branches/%252F", "repos/a/b/branches/%2e%2e", "repos/a/b/branches/%2E%2E", "repos/a/b/branches/%00", "repos/a/b/branches/%5C",
			"repos/a/b/branches/feat%2fx", "repos/a/b/branches/a%2Fb%2F", "repos/a/b/branches/a%20b", "repos/a/b/branches/%7F", "repos/a/b%2Fc/pulls/1", "repos%2Fa/b/pulls/1",
			"repos/a/b/pulls%2F1", "repos/a/b/pulls/1%2Fcommits?per_page=100&page=1",
		} {
			before := calls
			req, err := http.NewRequest("GET", base+ep, nil)
			if err != nil {
				continue
			}
			res, err := client.Do(req)
			if res != nil {
				res.Body.Close()
			}
			if err == nil || calls != before {
				t.Errorf("watch=%v accepted attack %s", watch, ep)
			}
		}
	}
}

func TestWatchGuardRefusesInconsistentRawPath(t *testing.T) {
	g := guardedTransport{next: roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(200, `{}`), nil }), host: "api.github.com", prefix: "/", watch: true}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/a/b/branches/feat%2Fx", nil)
	req.URL.Path = "/repos/a/b/branches/other"
	if _, err := g.RoundTrip(req); err == nil {
		t.Fatal("Path and RawPath disagree but were accepted")
	}
	req, _ = http.NewRequest("GET", "https://api.github.com/repos/a/b/branches/feat%2Fx", nil)
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "HEAD"} {
		r := req.Clone(req.Context())
		r.Method = m
		if _, err := g.RoundTrip(r); err == nil {
			t.Errorf("method %s accepted", m)
		}
	}
	if res, err := g.RoundTrip(req); err != nil {
		t.Fatal(err)
	} else {
		res.Body.Close()
	}
}

// A request built with a literal Path that contains an escape sequence has no
// RawPath; the wire form is its escaped path, so %2F becomes %252F and must be refused.
func TestWatchGuardValidatesWireFormOfLiteralPath(t *testing.T) {
	g := guardedTransport{next: roundTripFunc(func(r *http.Request) (*http.Response, error) { return response(200, `{}`), nil }), host: "api.github.com", prefix: "/", watch: true}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/a/b/branches/main", nil)
	req.URL.Path = "/repos/a/b/branches/feat%2Fx"
	req.URL.RawPath = ""
	if res, err := g.RoundTrip(req); err == nil {
		res.Body.Close()
		t.Fatal("literal Path with %2F was accepted although it is sent as %252F")
	}
}
