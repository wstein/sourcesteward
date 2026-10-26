// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

func budgetRequest(t *testing.T, method string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, "https://api.github.com/repos/owner/repo", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// Requests are counted before they are sent; the cap of the run stops the run, and a stopped run
// sends nothing more (Spec 044 D13: nothing is retried).
func TestTidyRunBudgetCountsAndStopsAtTheCap(t *testing.T) {
	var sent atomic.Int64
	b := NewTidyRunBudget(roundTripFunc(func(*http.Request) (*http.Response, error) {
		sent.Add(1)
		return response(200, `{}`), nil
	}), 3)
	for i := 0; i < 3; i++ {
		res, err := b.RoundTrip(budgetRequest(t, http.MethodGet))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	if _, err := b.RoundTrip(budgetRequest(t, http.MethodGet)); !errors.Is(err, ErrTidyRunStopped) {
		t.Fatalf("fourth request: %v", err)
	}
	if b.Requests() != 3 || sent.Load() != 3 || b.Stopped() != TidyStopRunCap {
		t.Fatalf("requests %d sent %d stopped %q", b.Requests(), sent.Load(), b.Stopped())
	}
	if NewTidyRunBudget(nil, 100000).max != TidyRunMaxRequests {
		t.Fatal("the cap never exceeds 512")
	}
}

func TestTidyRunBudgetFailuresCountAndTheStopReasonsAreExact(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		header map[string]string
		want   TidyStopReason
	}{
		{"429", 429, nil, TidyStopRateLimit},
		{"403 exhausted", 403, map[string]string{"X-RateLimit-Remaining": "0"}, TidyStopRateLimit},
		{"403 secondary", 403, map[string]string{"Retry-After": "60"}, TidyStopRateLimit},
		{"403 permissions", 403, nil, ""},
		{"403 with quota left", 403, map[string]string{"X-RateLimit-Remaining": "4999"}, ""},
		{"404", 404, nil, ""},
		{"500", 500, nil, TidyStopServer},
		{"503", 503, nil, TidyStopServer},
		{"200", 200, nil, ""},
	} {
		var sent atomic.Int64
		b := NewTidyRunBudget(roundTripFunc(func(*http.Request) (*http.Response, error) {
			sent.Add(1)
			res := response(tc.status, `{}`)
			for k, v := range tc.header {
				res.Header.Set(k, v)
			}
			return res, nil
		}), 10)
		if res, err := b.RoundTrip(budgetRequest(t, http.MethodGet)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		} else {
			res.Body.Close()
		}
		if b.Stopped() != tc.want || b.Requests() != 1 {
			t.Errorf("%s: stopped %q requests %d", tc.name, b.Stopped(), b.Requests())
		}
		_, err := b.RoundTrip(budgetRequest(t, http.MethodGet))
		if tc.want != "" && (!errors.Is(err, ErrTidyRunStopped) || sent.Load() != 1) {
			t.Errorf("%s: a stopped run sent again (%v, %d sent)", tc.name, err, sent.Load())
		}
		if tc.want == "" && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// A transport error counts as a request and does not stop the run: it is one member's failure.
func TestTidyRunBudgetATransportErrorIsOneMembersFailure(t *testing.T) {
	b := NewTidyRunBudget(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("reset") }), 5)
	if _, err := b.RoundTrip(budgetRequest(t, http.MethodGet)); err == nil || errors.Is(err, ErrTidyRunStopped) {
		t.Fatalf("%v", err)
	}
	if b.Requests() != 1 || b.Stopped() != "" {
		t.Fatalf("requests %d stopped %q", b.Requests(), b.Stopped())
	}
}

// Every method but GET is refused before anything is counted or sent.
func TestTidyRunBudgetRefusesEveryWrite(t *testing.T) {
	var sent atomic.Int64
	b := NewTidyRunBudget(roundTripFunc(func(*http.Request) (*http.Response, error) {
		sent.Add(1)
		return response(200, `{}`), nil
	}), 10)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, "PURGE"} {
		if _, err := b.RoundTrip(budgetRequest(t, m)); !errors.Is(err, ErrTidyRunStopped) {
			t.Errorf("%s: %v", m, err)
		}
	}
	if sent.Load() != 0 || b.Requests() != 0 || b.Stopped() != "" {
		t.Fatalf("sent %d counted %d stopped %q", sent.Load(), b.Requests(), b.Stopped())
	}
}

// Seven real member sessions of 64 tips each (closed collection of ten full pages, a candidate read
// per tip) need 75 requests each: the shared run budget of 512 stops the seventh member, no
// session exceeds its own 128, and every request is a GET (Spec 044 SC-005).
func TestTidyRunBudgetBoundsSevenRealSessions(t *testing.T) {
	var gets, others atomic.Int64
	b := NewTidyRunBudget(nil, TidyRunMaxRequests)
	defer b.Close()
	finished, failed := 0, 0
	for member := 0; member < 7; member++ {
		c := &discoveryCollection{pages: 10, last: 1000, link: true, calls: map[string]int{}, t: t}
		b.next = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				others.Add(1)
			}
			gets.Add(1)
			return c.handle(req)
		})
		s := tidyFixtureSessionOver(t, b.RoundTrip)
		rows, err := s.DiscoverPullRequests(context.Background(), discoveryTargets(64), nil)
		if s.Budget().Requests > 128 {
			t.Fatalf("member %d: %d requests", member, s.Budget().Requests)
		}
		if err != nil {
			failed++
			if b.Stopped() != TidyStopRunCap {
				t.Fatalf("member %d failed without the cap: %v (%q)", member, err, b.Stopped())
			}
			continue
		}
		if len(rows) != 64 || s.Budget().Requests != 75 || c.calls["pull"] != 64 || c.calls["page"] != 10 {
			t.Fatalf("member %d: %d rows, %d requests, %+v", member, len(rows), s.Budget().Requests, c.calls)
		}
		finished++
	}
	if finished != 6 || failed != 1 || b.Requests() != TidyRunMaxRequests || gets.Load() != TidyRunMaxRequests || others.Load() != 0 {
		t.Fatalf("finished %d failed %d requests %d sent %d writes %d", finished, failed, b.Requests(), gets.Load(), others.Load())
	}
}
