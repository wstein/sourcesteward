// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"time"
)

// ObservePhase names a phase of the observation of one repository (Spec 041 progress). The words
// are the ones the progress line and the verbose summary of `stw tidy` print.
type ObservePhase string

// The phases, in the order the observation runs them. ObservePhaseStart and ObservePhaseDone are
// reported by the analysis around the observation of a repository; ObservePhaseWindow is reported
// when the observation window ended inside a phase (Note names that phase).
const (
	ObservePhaseStart     ObservePhase = "start"
	ObservePhaseRefs      ObservePhase = "references"
	ObservePhaseWorktrees ObservePhase = "worktrees"
	ObservePhaseBranches  ObservePhase = "branches"
	ObservePhaseProofs    ObservePhase = "proofs"
	ObservePhaseTracking  ObservePhase = "tracking"
	ObservePhaseWindow    ObservePhase = "window"
	// ObservePhaseCache reports the reuse of a result inside the run: Note names it, Done the hits
	// and Total the computations.
	ObservePhaseCache ObservePhase = "cache"
	ObservePhaseDone  ObservePhase = "done"
)

// ObserveEvent is one progress report: Repo, Index (1-based) and Count come from the context
// (WithObserveRepo), the rest from the observer. Done and Total count the items of the phase
// (zero Total: not known yet). Note is a short fixed word (the phase a window ended in, or the
// state of a finished repository).
type ObserveEvent struct {
	Repo         string
	Index, Count int
	Phase        ObservePhase
	Done, Total  int
	Note         string
	// Item and Took are set on the report of one finished item of a phase (a worktree, a local
	// branch, a tip that was proven): its short label and how long it took.
	Item string
	Took time.Duration
}

type observeProgressKey struct{}
type observeScopeKey struct{}

type observeScope struct {
	name         string
	index, count int
}

// WithObserveProgress carries a progress sink into the observation. The sink may be called from
// several goroutines at the same time and must not block for long; it never influences the result.
func WithObserveProgress(ctx context.Context, sink func(ObserveEvent)) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, observeProgressKey{}, sink)
}

// WithObserveRepo names the repository the events of the context belong to: its name, its
// 1-based position and the number of repositories analysed.
func WithObserveRepo(ctx context.Context, name string, index, count int) context.Context {
	return context.WithValue(ctx, observeScopeKey{}, observeScope{name, index, count})
}

// ReportObserve sends an event to the sink of the context, if there is one, completed with the
// repository of the context.
func ReportObserve(ctx context.Context, e ObserveEvent) {
	sink, ok := ctx.Value(observeProgressKey{}).(func(ObserveEvent))
	if !ok {
		return
	}
	if r, ok := ctx.Value(observeScopeKey{}).(observeScope); ok {
		e.Repo, e.Index, e.Count = r.name, r.index, r.count
	}
	sink(e)
}
