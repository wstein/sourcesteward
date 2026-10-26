// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"errors"
	"net/http"
	"sync"
)

// TidyRunMaxRequests is the per-run request cap of the online reads of the tidy wizard (Spec 044
// D13): all member sessions of one run together send at most this many requests. Every session
// keeps its own 128-request, 32 MiB and 30-second budget (Spec 031 FR-018).
const TidyRunMaxRequests = 512

// TidyStopReason names why the online reads of a run stopped; the empty reason means they did not.
type TidyStopReason string

// The reasons that stop every later online read of the run (Spec 044 D13).
const (
	// TidyStopRateLimit is a 429, or a 403 that reports an exhausted rate limit.
	TidyStopRateLimit TidyStopReason = "rate-limit"
	// TidyStopServer is any 5xx.
	TidyStopServer TidyStopReason = "server-error"
	// TidyStopRunCap is the request cap of the run.
	TidyStopRunCap TidyStopReason = "run-cap"
)

// ErrTidyRunStopped is returned for every request after the run stopped, and for a request that is
// not a read; nothing is sent.
var ErrTidyRunStopped = errors.New("tidy online run stopped")

// ErrTidyAuthentication marks that no usable GitHub credential or gh executable was found when a
// tidy session was opened. Its text is the one the session always had.
var ErrTidyAuthentication = errors.New("tidy hosting authentication unavailable")

// TidyRunBudget is the shared inner transport of the member sessions of one wizard run. It counts
// each request before it is sent (a failure counts too), refuses anything that is not a GET or a
// HEAD, and, once a 429, an exhausted 403, a 5xx or the run cap has been seen, sends nothing more:
// nothing is retried. It sits below the fixed github.com transport guard, which still applies to
// every request; it adds no host and no method.
type TidyRunBudget struct {
	next  http.RoundTripper
	owned *http.Transport
	max   int

	mu       sync.Mutex
	requests int
	stop     TidyStopReason
}

// NewTidyRunBudget wraps next (nil selects the production HTTPS transport of the hosting layer)
// with a cap of max requests (at most TidyRunMaxRequests).
func NewTidyRunBudget(next http.RoundTripper, max int) *TidyRunBudget {
	b := &TidyRunBudget{next: next, max: min(max, TidyRunMaxRequests)}
	if next == nil {
		b.owned = newHTTPTransport()
		b.next = b.owned
	}
	return b
}

// Close releases the idle connections of the transport this budget created.
func (b *TidyRunBudget) Close() {
	if b.owned != nil {
		b.owned.CloseIdleConnections()
	}
}

// Requests is the number of requests counted so far.
func (b *TidyRunBudget) Requests() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requests
}

// Stopped reports why the run's online reads stopped, or the empty reason.
func (b *TidyRunBudget) Stopped() TidyStopReason {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stop
}

func (b *TidyRunBudget) halt(reason TidyStopReason) {
	b.mu.Lock()
	if b.stop == "" {
		b.stop = reason
	}
	b.mu.Unlock()
}

// RoundTrip implements http.RoundTripper.
func (b *TidyRunBudget) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return nil, ErrTidyRunStopped
	}
	b.mu.Lock()
	if b.stop == "" && b.requests >= b.max {
		b.stop = TidyStopRunCap
	}
	if b.stop != "" {
		b.mu.Unlock()
		return nil, ErrTidyRunStopped
	}
	b.requests++
	b.mu.Unlock()
	res, err := b.next.RoundTrip(req)
	if res != nil {
		switch {
		case res.StatusCode == http.StatusTooManyRequests:
			b.halt(TidyStopRateLimit)
		case res.StatusCode == http.StatusForbidden && (res.Header.Get("X-RateLimit-Remaining") == "0" || res.Header.Get("Retry-After") != ""):
			b.halt(TidyStopRateLimit)
		case res.StatusCode >= 500:
			b.halt(TidyStopServer)
		}
	}
	return res, err
}
