// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"time"
)

// RunTrace sees every Git process the read-only inspection starts: its directory, its arguments
// (after the fixed prefix), how long it ran and its exit code (-1 when it did not exit normally).
// It only observes; it never changes a result.
type RunTrace func(ctx context.Context, dir string, args []string, took time.Duration, exit int)

type runTraceKey struct{}

// WithRunTrace carries a RunTrace into the inspection.
func WithRunTrace(ctx context.Context, f RunTrace) context.Context {
	if f == nil {
		return ctx
	}
	return context.WithValue(ctx, runTraceKey{}, f)
}

// traceRun starts the clock of one process; the returned function reports it.
func traceRun(ctx context.Context, dir string, args []string) func(exit int) {
	f, ok := ctx.Value(runTraceKey{}).(RunTrace)
	if !ok {
		return func(int) {}
	}
	start := time.Now()
	return func(exit int) { f(ctx, dir, args, time.Since(start), exit) }
}
