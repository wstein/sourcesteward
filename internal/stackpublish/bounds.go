// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"fmt"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// Limits are the finite, documented bounds of one operation (data-model.md). The shorter
// remaining operation deadline always wins; --timeout can only shorten the operation bound.
type Limits struct {
	Actions            int `json:"actions"`
	OpenLevels         int `json:"open_levels"`
	IdentityCommits    int `json:"identity_commits"`
	Requests           int `json:"requests"`
	PagesPerCollection int `json:"pages_per_collection"`
	Records            int `json:"records"`
	ResponseBytes      int `json:"response_bytes"`
	InputBytes         int `json:"input_bytes"`
	DisplayBytes       int `json:"display_bytes"`
	ErrorBytes         int `json:"error_bytes"`
	OperationSeconds   int `json:"operation_seconds"`
	RequestSeconds     int `json:"request_seconds"`
	LocalReadSeconds   int `json:"local_read_seconds"`
	ReadWorkers        int `json:"read_workers"`
}

// DefaultLimits returns the documented ceilings; no limit may exceed them.
func DefaultLimits() Limits {
	return Limits{Actions: 16, OpenLevels: 2, IdentityCommits: 200, Requests: 50, PagesPerCollection: 10, Records: 1000,
		ResponseBytes: 1 << 20, InputBytes: 64 << 10, DisplayBytes: 512, ErrorBytes: 2 << 10,
		OperationSeconds: 60, RequestSeconds: 30, LocalReadSeconds: 5, ReadWorkers: 4}
}

// Validate refuses a zero, negative or above-ceiling limit.
func (l Limits) Validate() error {
	c := DefaultLimits()
	for _, f := range []struct {
		name       string
		got, limit int
	}{
		{"actions", l.Actions, c.Actions}, {"open_levels", l.OpenLevels, c.OpenLevels}, {"identity_commits", l.IdentityCommits, c.IdentityCommits},
		{"requests", l.Requests, c.Requests}, {"pages_per_collection", l.PagesPerCollection, c.PagesPerCollection}, {"records", l.Records, c.Records},
		{"response_bytes", l.ResponseBytes, c.ResponseBytes}, {"input_bytes", l.InputBytes, c.InputBytes}, {"display_bytes", l.DisplayBytes, c.DisplayBytes},
		{"error_bytes", l.ErrorBytes, c.ErrorBytes}, {"operation_seconds", l.OperationSeconds, c.OperationSeconds},
		{"request_seconds", l.RequestSeconds, c.RequestSeconds}, {"local_read_seconds", l.LocalReadSeconds, c.LocalReadSeconds}, {"read_workers", l.ReadWorkers, c.ReadWorkers},
	} {
		if f.got < 1 || f.got > f.limit {
			return &ValidationError{Code: "limits-invalid", Field: "limits." + f.name}
		}
	}
	return nil
}

// WithTimeout shortens the operation bound. The duration must be a positive whole number of
// seconds and at most the ceiling; it can never lengthen any bound.
func (l Limits) WithTimeout(d time.Duration) (Limits, error) {
	if d <= 0 || d%time.Second != 0 || d > time.Duration(DefaultLimits().OperationSeconds)*time.Second {
		return l, &ValidationError{Code: "timeout-invalid", Field: "timeout", Usage: true}
	}
	if secs := int(d / time.Second); secs < l.OperationSeconds {
		l.OperationSeconds = secs
	}
	return l, nil
}

// BoundError reports a reached bound or a cancellation; it is never success.
type BoundError struct {
	Reason Reason
	Bound  string
}

func (e *BoundError) Error() string {
	if e.Bound == "" {
		return string(e.Reason)
	}
	return string(e.Reason) + ": " + e.Bound
}

func bound(name string) error { return &BoundError{Reason: ReasonBoundExceeded, Bound: name} }

// Clock supplies the time; the package never reads one itself.
type Clock func() time.Time

// ByteKind selects one of the independently bounded byte counts.
type ByteKind string

// Byte kinds.
const (
	BytesRaw        ByteKind = "raw"
	BytesNormalized ByteKind = "normalized"
	BytesResult     ByteKind = "result"
)

// Budget is the one operation-wide budget. Every source, retry, resample and read worker draws
// from the same instance; nothing resets per source or action. It is safe for concurrent use by
// the callers' read workers.
type Budget struct {
	mu        sync.Mutex
	limits    Limits
	now       Clock
	start     time.Time
	requests  int
	records   int
	pages     map[string]int
	bytes     map[ByteKind]int
	cancelled bool
}

// NewBudget starts a budget at now().
func NewBudget(l Limits, now Clock) *Budget {
	return &Budget{limits: l, now: now, start: now(), pages: map[string]int{}, bytes: map[ByteKind]int{}}
}

func (b *Budget) remaining() time.Duration {
	return time.Duration(b.limits.OperationSeconds)*time.Second - b.now().Sub(b.start)
}

// errLocked reports cancellation or the elapsed operation deadline; the caller holds the lock.
func (b *Budget) errLocked() error {
	if b.cancelled {
		return &BoundError{Reason: ReasonCancelled}
	}
	if b.remaining() <= 0 {
		return bound("operation_seconds")
	}
	return nil
}

// Err is nil while the operation may continue, else the cancellation or elapsed deadline.
func (b *Budget) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.errLocked()
}

// Cancel marks the operation cancelled; no next action may start.
func (b *Budget) Cancel() {
	b.mu.Lock()
	b.cancelled = true
	b.mu.Unlock()
}

// Request counts one remote request.
func (b *Budget) Request() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.errLocked(); err != nil {
		return err
	}
	if b.requests >= b.limits.Requests {
		return bound("requests")
	}
	b.requests++
	return nil
}

// Page counts one page of the named collection.
func (b *Budget) Page(collection string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.errLocked(); err != nil {
		return err
	}
	if b.pages[collection] >= b.limits.PagesPerCollection {
		return bound("pages")
	}
	b.pages[collection]++
	return nil
}

// Records counts n more records.
func (b *Budget) Records(n int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.errLocked(); err != nil {
		return err
	}
	if n < 0 || n > b.limits.Records-b.records {
		return bound("records")
	}
	b.records += n
	return nil
}

// Bytes counts n more bytes of one kind; each kind has its own bound.
func (b *Budget) Bytes(kind ByteKind, n int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.errLocked(); err != nil {
		return err
	}
	switch kind {
	case BytesRaw, BytesNormalized, BytesResult:
	default:
		return bound("bytes")
	}
	if n < 0 || n > b.limits.ResponseBytes-b.bytes[kind] {
		return bound(string(kind) + "_bytes")
	}
	b.bytes[kind] += n
	return nil
}

func (b *Budget) timeout(seconds int) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.errLocked() != nil {
		return 0
	}
	if own := time.Duration(seconds) * time.Second; own < b.remaining() {
		return own
	}
	return b.remaining()
}

// RequestTimeout is the time one request or native push may take now: the per-request bound, or
// the remaining operation time when that is shorter. An exhausted budget grants none.
func (b *Budget) RequestTimeout() time.Duration { return b.timeout(b.limits.RequestSeconds) }

// LocalReadTimeout is the time one local observation may take now, by the same rule.
func (b *Budget) LocalReadTimeout() time.Duration { return b.timeout(b.limits.LocalReadSeconds) }

// Requests is the number of requests counted so far.
func (b *Budget) Requests() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requests
}

// CheckInput refuses input or body text above the input bound before it is decoded.
func CheckInput(l Limits, n int) error {
	if n < 0 || n > l.InputBytes {
		return bound("input_bytes")
	}
	return nil
}

// ReadWorkers is the width of independent-read fanout: the limit, but never more than the tasks
// and at least one.
func ReadWorkers(l Limits, tasks int) int {
	workers := l.ReadWorkers
	if ceiling := DefaultLimits().ReadWorkers; workers > ceiling {
		workers = ceiling
	}
	if tasks < workers {
		workers = tasks
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

// Indexed is one fanout result with the input index it belongs to.
type Indexed[T any] struct {
	Index int
	Value T
}

// OrderByInputIndex returns exactly n values ordered by input index, without reordering the
// input. A missing, repeated or out-of-range index is an error: a lost result never becomes
// success.
func OrderByInputIndex[T any](results []Indexed[T], n int) ([]T, error) {
	if n < 0 || len(results) != n {
		return nil, &ValidationError{Code: "result-count", Field: "results"}
	}
	out := make([]T, n)
	seen := make([]bool, n)
	for _, r := range results {
		if r.Index < 0 || r.Index >= n || seen[r.Index] {
			return nil, &ValidationError{Code: "result-index", Field: "results[" + strconv.Itoa(r.Index) + "]"}
		}
		seen[r.Index] = true
		out[r.Index] = r.Value
	}
	return out, nil
}

// Display escapes hostile text and shortens it to at most max bytes without splitting an escape or
// a rune. truncated reports a shortening; it never changes the identity used by a guard.
func Display(s string, max int) (shown string, truncated bool) {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		piece := escapeRune(r, size, s[i])
		if len(out)+len(piece) > max {
			return string(out), true
		}
		out = append(out, piece...)
		i += size
	}
	return string(out), false
}

func escapeRune(r rune, size int, first byte) string {
	switch {
	case r == utf8.RuneError && size == 1:
		return fmt.Sprintf(`\x%02x`, first)
	case r == '\\':
		return `\\`
	case r == '\n':
		return `\n`
	case r == '\r':
		return `\r`
	case r == '\t':
		return `\t`
	case r < 0x20 || r == 0x7f:
		return fmt.Sprintf(`\x%02x`, r)
	case r >= 0x80 && r <= 0x9f, r == 0x061c, r == 0x200e, r == 0x200f, r >= 0x202a && r <= 0x202e, r == 0x2028, r == 0x2029, r >= 0x2066 && r <= 0x2069, r == 0xfeff:
		return fmt.Sprintf(`\u%04x`, r)
	}
	return string(r)
}
