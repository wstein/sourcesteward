// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type progressLog struct {
	mu     sync.Mutex
	events []ObserveEvent
}

func (l *progressLog) add(e ObserveEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
}

func TestObserveProgressWithoutASinkIsANoOp(t *testing.T) {
	ReportObserve(context.Background(), ObserveEvent{Phase: ObservePhaseRefs})
	ctx := WithObserveRepo(context.Background(), "r", 1, 2)
	ReportObserve(ctx, ObserveEvent{Phase: ObservePhaseRefs})
}

func TestObserveProgressCarriesTheRepository(t *testing.T) {
	var log progressLog
	ctx := WithObserveProgress(context.Background(), log.add)
	ctx = WithObserveRepo(ctx, "alpha", 3, 7)
	ReportObserve(ctx, ObserveEvent{Phase: ObservePhaseBranches, Done: 4, Total: 9})
	if len(log.events) != 1 {
		t.Fatalf("events: %+v", log.events)
	}
	got := log.events[0]
	want := ObserveEvent{Repo: "alpha", Index: 3, Count: 7, Phase: ObservePhaseBranches, Done: 4, Total: 9}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestObserveReportsEveryPhaseWithCounts(t *testing.T) {
	repo := observeRepo(t)
	observeAdd(t, repo, "task-a")
	observeAdd(t, repo, "task-b")
	var log progressLog
	ctx := WithObserveProgress(context.Background(), log.add)
	if _, err := ObserveWorktrees(ctx, ObserveOptions{Path: repo, Base: observeBase}); err != nil {
		t.Fatal(err)
	}
	last := map[ObservePhase]ObserveEvent{}
	var order []ObservePhase
	for _, e := range log.events {
		if len(order) == 0 || order[len(order)-1] != e.Phase {
			order = append(order, e.Phase)
		}
		last[e.Phase] = e
	}
	want := []ObservePhase{ObservePhaseRefs, ObservePhaseWorktrees, ObservePhaseBranches, ObservePhaseProofs}
	if len(order) != len(want) {
		t.Fatalf("phase order %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("phase order %v, want %v", order, want)
		}
	}
	// Three worktrees (the primary and two linked), three local branches (main and the two tasks).
	if e := last[ObservePhaseWorktrees]; e.Done != 3 || e.Total != 3 {
		t.Fatalf("worktrees: %+v", e)
	}
	if e := last[ObservePhaseBranches]; e.Done != 3 || e.Total != 3 {
		t.Fatalf("branches: %+v", e)
	}
	// Proofs: two linked worktrees and the branches that are not the base; the counts agree.
	if e := last[ObservePhaseProofs]; e.Total == 0 || e.Done != e.Total {
		t.Fatalf("proofs: %+v", e)
	}
}

func TestObserveReportsTheWindowThatWasHit(t *testing.T) {
	repo := observeRepo(t)
	observeAdd(t, repo, "task-a")
	var log progressLog
	ctx := WithObserveProgress(context.Background(), log.add)
	c := defaultWorktreeObserver()
	c.window = func(_ context.Context, n int) time.Duration {
		if n == 0 {
			return time.Minute
		}
		return time.Nanosecond
	}
	_, _ = c.observe(ctx, ObserveOptions{Path: repo, Base: observeBase})
	for _, e := range log.events {
		if e.Phase == ObservePhaseWindow {
			if e.Note != string(ObservePhaseWorktrees) {
				t.Fatalf("window hit in %q, want worktrees", e.Note)
			}
			return
		}
	}
	t.Fatalf("no window event in %+v", log.events)
}

func TestObserveTracesEveryGitProcess(t *testing.T) {
	repo := observeRepo(t)
	observeAdd(t, repo, "task-a")
	var mu sync.Mutex
	var calls []GitCall
	ctx := WithGitTrace(WithObserveRepo(context.Background(), "alpha", 1, 1), func(c GitCall) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, c)
	})
	if _, err := ObserveWorktrees(ctx, ObserveOptions{Path: repo, Base: observeBase}); err != nil {
		t.Fatal(err)
	}
	verbs := map[string]int{}
	for _, c := range calls {
		if c.Repo != "alpha" || len(c.Argv) == 0 || c.Duration <= 0 {
			t.Fatalf("call %+v", c)
		}
		for _, a := range c.Argv {
			if a == "-c" || strings.HasPrefix(a, "core.hooksPath") {
				t.Fatalf("the fixed prefix is in %v", c.Argv)
			}
		}
		verbs[c.Argv[0]]++
	}
	// The sandboxed reads (rev-list, ls-tree) and the inspection (rev-parse) are both traced.
	for _, v := range []string{"rev-list", "ls-tree", "rev-parse"} {
		if verbs[v] == 0 {
			t.Errorf("no %s call in %v", v, verbs)
		}
	}
}

func TestTraceArgvIsBoundedAndWithoutCredentials(t *testing.T) {
	long := strings.Repeat("x", 500)
	args := []string{"--no-replace-objects", "-c", "a=b", "fetch", "https://user:secret@example.com/r.git", long}
	for i := 0; i < 40; i++ {
		args = append(args, "p")
	}
	got := traceArgv(args)
	if got[0] != "fetch" || got[1] != "https://example.com/r.git" || len(got[2]) != traceArgLength+3 || len(got) != traceArgLimit+1 || got[len(got)-1] != "..." {
		t.Fatalf("%q", got)
	}
}

// The worktrees, the branches and the tips are read a few at a time; the observation is the same
// as the sequential one.
func TestObserveConcurrentEqualsSequential(t *testing.T) {
	repo := observeRepo(t)
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		w := observeAdd(t, repo, "task-"+n)
		if n == "b" || n == "e" {
			commitIn(t, w, "mine-"+n, "own work\n")
		}
	}
	observeWith := func(width int) WorktreeObservation {
		o, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: repo, Base: observeBase, Width: width})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	seq := observeWith(1)
	if conc := observeWith(4); !reflect.DeepEqual(seq, conc) {
		t.Fatalf("sequential %+v\nconcurrent %+v", seq, conc)
	}
}

// A window that ends while several workers are at work still gives every worktree and branch its
// evidence: an item that was not started when the window ended is observed too and reports the
// deadline as its Incomplete reason, as the sequential loop did. The slots are never left empty.
func TestObserveWindowEndingMidRunLeavesNoEmptySlot(t *testing.T) {
	repo := observeRepo(t)
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		observeAdd(t, repo, "task-"+n)
	}
	c := defaultWorktreeObserver()
	c.window = func(_ context.Context, n int) time.Duration {
		if n == 0 {
			return time.Minute
		}
		return time.Nanosecond
	}
	o, err := c.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase, Width: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Worktrees) != 9 || len(o.Branches) != 9 {
		t.Fatalf("%d worktrees, %d branches", len(o.Worktrees), len(o.Branches))
	}
	for _, w := range o.Worktrees {
		if w.Path == "" || (!w.Primary && w.Incomplete == "") {
			t.Fatalf("an empty or unjudged worktree slot: %+v", w)
		}
	}
	for _, b := range o.Branches {
		if b.Name == "" || (b.Incomplete == "" && b.Proof == "") {
			t.Fatalf("an empty branch slot: %+v", b)
		}
	}
}
