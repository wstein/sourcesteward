// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"time"
)

const (
	// observationPerWorktree is added to the base window for every registered worktree or
	// workspace of the repository (Spec 041, decision of 2026-10-25).
	observationPerWorktree = time.Second
	// observationWindowMax caps the scaled default window.
	observationWindowMax = 10 * time.Minute
	// ObservationTimeoutMax is the largest explicit --timeout of the wizard analysis.
	ObservationTimeoutMax = time.Hour
)

type observationTimeoutKey struct{}

// WithObservationTimeout carries an explicit --timeout into the observation: it replaces the
// scaled window of every repository. Zero or negative keeps the scaled default; a value above
// ObservationTimeoutMax is the maximum.
func WithObservationTimeout(ctx context.Context, d time.Duration) context.Context {
	if d <= 0 {
		return ctx
	}
	return context.WithValue(ctx, observationTimeoutKey{}, min(d, ObservationTimeoutMax))
}

// ObservationTimeout is the explicit --timeout carried by the context, if any.
func ObservationTimeout(ctx context.Context) (time.Duration, bool) {
	d, ok := ctx.Value(observationTimeoutKey{}).(time.Duration)
	return d, ok && d > 0
}

// observationWindow is the deadline of the observation of one repository with the given number of
// registered worktrees: the explicit --timeout when there is one, otherwise the base window plus
// observationPerWorktree for each registration, at most observationWindowMax. It scales only the
// analysis; the per-call limits and the plan, apply and restore deadlines are not derived from it.
func observationWindow(ctx context.Context, registrations int) time.Duration {
	if d, ok := ObservationTimeout(ctx); ok {
		return d
	}
	return min(planObservationTimeout+time.Duration(max(registrations, 0))*observationPerWorktree, observationWindowMax)
}

// window is the observation window of the observer for n registrations.
func (c worktreeObserver) windowFor(ctx context.Context, n int) time.Duration {
	if c.window != nil {
		return c.window(ctx, n)
	}
	return observationWindow(ctx, n)
}
