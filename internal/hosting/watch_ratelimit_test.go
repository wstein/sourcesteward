// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAssessRateLimit(t *testing.T) {
	reset := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name string
		in   RateInfo
		want RateAdvice
	}{
		{"unknown", RateInfo{}, AdviceNone},
		{"plenty", RateInfo{Known: true, Limit: 5000, Remaining: 4000, Reset: reset}, AdviceNone},
		{"exactly 10 percent", RateInfo{Known: true, Limit: 5000, Remaining: 500, Reset: reset}, AdviceNone},
		{"below 10 percent", RateInfo{Known: true, Limit: 5000, Remaining: 499, Reset: reset}, AdviceSlow},
		{"49 left", RateInfo{Known: true, Limit: 5000, Remaining: 49, Reset: reset}, AdviceWait},
		{"50 left is slow, not wait", RateInfo{Known: true, Limit: 5000, Remaining: 50, Reset: reset}, AdviceSlow},
		{"exhausted", RateInfo{Known: true, Limit: 60, Remaining: 0, Reset: reset}, AdviceWait},
	} {
		d := AssessRateLimit(tc.in)
		if d.Advice != tc.want {
			t.Errorf("%s: %v want %v", tc.name, d.Advice, tc.want)
		}
		if tc.want == AdviceWait && !d.Until.Equal(reset.Add(5*time.Second)) {
			t.Errorf("%s: until %v", tc.name, d.Until)
		}
	}
}

func TestRateLimitedWaitUntil(t *testing.T) {
	now := time.Unix(1699999000, 0)
	reset := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name string
		err  *WatchError
		want time.Time
	}{
		{"retry-after", &WatchError{Code: CodeRateLimited, RetryAfter: 60 * time.Second, RateLimit: RateInfo{Known: true, Reset: reset}}, now.Add(65 * time.Second)},
		{"reset", &WatchError{Code: CodeRateLimited, RateLimit: RateInfo{Known: true, Reset: reset}}, reset.Add(5 * time.Second)},
		{"stale reset in the past", &WatchError{Code: CodeRateLimited, RateLimit: RateInfo{Known: true, Reset: now.Add(-time.Hour)}}, now.Add(65 * time.Second)},
		{"nothing known", &WatchError{Code: CodeRateLimited}, now.Add(65 * time.Second)},
	} {
		if got := RateLimitedUntil(tc.err, now); !got.Equal(tc.want) {
			t.Errorf("%s: %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestWatchGuardRefusesEveryWriteMethodOnEveryWatchEndpoint(t *testing.T) {
	calls := 0
	client, err := newSDKClientMode("github.com", "fixturetoken", t.TempDir(), roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, "{}"), nil }), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, ep := range watchGoodEndpoints() {
		for _, m := range []string{"POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "CONNECT", "TRACE"} {
			req, _ := http.NewRequest(m, "https://api.github.com/"+ep, strings.NewReader("{}"))
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
				t.Errorf("%s %s accepted", m, ep)
			}
			req, _ = http.NewRequest(m, "https://api.github.com/"+ep, nil)
			if res, err := client.Do(req); err == nil {
				res.Body.Close()
				t.Errorf("%s %s accepted without body", m, ep)
			}
		}
	}
	if calls != 0 {
		t.Fatalf("%d write requests reached the transport", calls)
	}
}

// The watch reader must be GET-only by construction: no write verbs and no way to
// pass a method or URL in its sources.
func TestWatchSourcesHaveNoWriteVerbs(t *testing.T) {
	for _, f := range []string{"watch_reader.go", "watch_pulls.go", "watch_endpoint.go", "watch_branch.go", "watch_ratelimit.go"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"MethodPost", "MethodPut", "MethodPatch", "MethodDelete", "http.Post", "\"POST\"", "\"PUT\"", "\"PATCH\"", "\"DELETE\"", "time.Sleep", "time.After(", "time.Tick"} {
			if strings.Contains(string(b), bad) {
				t.Errorf("%s contains %s", f, bad)
			}
		}
	}
}
