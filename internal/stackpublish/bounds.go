// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import "time"

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

// DefaultLimits returns the documented ceilings.
func DefaultLimits() Limits { return Limits{} }

// Validate refuses a zero, negative or above-ceiling limit.
func (l Limits) Validate() error { return nil }

// WithTimeout shortens the operation bound; it must be positive and at most the ceiling.
func (l Limits) WithTimeout(time.Duration) (Limits, error) { return Limits{}, nil }

// BoundError reports a reached bound or a cancellation; it is never success.
type BoundError struct {
	Reason Reason
	Bound  string
}

func (e *BoundError) Error() string { return "" }

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
// from the same instance; nothing resets per source or action.
type Budget struct{}

// NewBudget starts a budget at now().
func NewBudget(Limits, Clock) *Budget { return &Budget{} }

// Request counts one remote request.
func (b *Budget) Request() error { return nil }

// Page counts one page of the named collection.
func (b *Budget) Page(string) error { return nil }

// Records counts n more records.
func (b *Budget) Records(int) error { return nil }

// Bytes counts n more bytes of one kind.
func (b *Budget) Bytes(ByteKind, int) error { return nil }

// Cancel marks the operation cancelled; no next action may start.
func (b *Budget) Cancel() {}

// Err is nil while the operation may continue, else a *BoundError.
func (b *Budget) Err() error { return nil }

// RequestTimeout is the time one request or native push may take now.
func (b *Budget) RequestTimeout() time.Duration { return 0 }

// LocalReadTimeout is the time one local observation may take now.
func (b *Budget) LocalReadTimeout() time.Duration { return 0 }

// Requests is the number of requests counted so far.
func (b *Budget) Requests() int { return 0 }

// CheckInput refuses input or body text above the input bound before it is decoded.
func CheckInput(l Limits, n int) error { return nil }

// ReadWorkers is the width of independent-read fanout: at most the limit and at least one.
func ReadWorkers(l Limits, tasks int) int { return 0 }

// Indexed is one fanout result with the input index it belongs to.
type Indexed[T any] struct {
	Index int
	Value T
}

// OrderByInputIndex returns exactly n values ordered by input index. A missing, repeated or
// out-of-range index is an error: a lost result never becomes success.
func OrderByInputIndex[T any](results []Indexed[T], n int) ([]T, error) { return nil, nil }

// Display escapes hostile text and shortens it to at most max bytes without splitting an escape or
// a rune. truncated reports a shortening; it never changes the identity used by a guard.
func Display(s string, max int) (shown string, truncated bool) { return "", false }
