// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func TestObservationWindowScalesWithTheItems(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		n    int
		want time.Duration
	}{
		{0, 60 * time.Second},
		{1, 61 * time.Second},
		{61, 121 * time.Second},
		{191, 251 * time.Second},
		{540, 10 * time.Minute},  // exactly the cap
		{1024, 10 * time.Minute}, // the observation limit of worktrees
	} {
		got := observationWindow(ctx, tc.n)
		if got != tc.want {
			t.Errorf("n=%d: %s, want %s", tc.n, got, tc.want)
		}
	}
}

func TestObservationWindowCapHolds(t *testing.T) {
	if got := observationWindow(context.Background(), 100000); got != 10*time.Minute {
		t.Fatalf("%s", got)
	}
	if got := observationWindow(context.Background(), -5); got != 60*time.Second {
		t.Fatalf("negative count: %s", got)
	}
}

func TestExplicitTimeoutReplacesTheScaledWindow(t *testing.T) {
	ctx := WithObservationTimeout(context.Background(), 15*time.Minute)
	for _, n := range []int{0, 61, 100000} {
		if got := observationWindow(ctx, n); got != 15*time.Minute {
			t.Errorf("n=%d: %s, want 15m", n, got)
		}
	}
	// A smaller explicit value is honoured too, below the old fixed window.
	if got := observationWindow(WithObservationTimeout(context.Background(), 5*time.Second), 999); got != 5*time.Second {
		t.Errorf("%s", got)
	}
	// Above the documented maximum the window is the maximum.
	if got := observationWindow(WithObservationTimeout(context.Background(), 5*time.Hour), 1); got != ObservationTimeoutMax {
		t.Errorf("%s", got)
	}
	// Zero or negative means no override.
	if got := observationWindow(WithObservationTimeout(context.Background(), 0), 61); got != 121*time.Second {
		t.Errorf("%s", got)
	}
}

// The window the observer applies to the content reads is the one for the count of observed items (worktrees plus local branches).
func TestObserveAppliesTheWindowOfTheItemCount(t *testing.T) {
	repo := observeRepo(t)
	observeAdd(t, repo, "w1")
	observeAdd(t, repo, "w2")
	// The hooks below are not synchronized: one item at a time.
	obs := defaultWorktreeObserver()
	var asked []int
	obs.window = func(_ context.Context, n int) time.Duration { asked = append(asked, n); return windowForTest(n) }
	var left, proofLeft time.Duration
	ancestry := obs.refs.ancestry
	obs.refs.ancestry = func(ctx context.Context, s Snapshot, target, base string) (bool, error) {
		if d, ok := ctx.Deadline(); ok && proofLeft == 0 {
			proofLeft = time.Until(d)
		}
		return ancestry(ctx, s, target, base)
	}
	obs.read = func(ctx context.Context, bin, dir string, args ...string) (string, error) {
		if d, ok := ctx.Deadline(); ok && left == 0 {
			left = time.Until(d)
		}
		return gitRead(ctx, bin, dir, args...)
	}
	if _, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase, Width: 1}); err != nil {
		t.Fatal(err)
	}
	// three worktrees (the primary and two added) and the local branches of the fixture
	want := 3 + len(strings.Fields(gitFixture(t, repo, "for-each-ref", "--format=%(refname)", "refs/heads/")))
	if len(asked) == 0 || asked[len(asked)-1] != want {
		t.Fatalf("the window was asked for %v items, want the last to be %d (worktrees plus local branches)", asked, want)
	}
	if left < 6*time.Hour {
		t.Fatalf("content reads ran under %s, want the 7h window", left)
	}
	if proofLeft < 6*time.Hour {
		t.Fatalf("the proof phase ran under %s, want the rest of the 7h window", proofLeft)
	}
}

func TestObserveJJAppliesTheWindowOfTheItemCount(t *testing.T) {
	repo := observeJJRepo(t)
	observeJJAdd(t, repo, "w1")
	obs := defaultWorktreeObserver()
	var asked []int
	obs.window = func(_ context.Context, n int) time.Duration { asked = append(asked, n); return windowForTest(n) }
	var left time.Duration
	real := obs.retire
	obs.retire = func(ctx context.Context, source, base, destination, name, repo string) (inspection.JJRetirementState, error) {
		if d, ok := ctx.Deadline(); ok && left == 0 {
			left = time.Until(d)
		}
		return real(ctx, source, base, destination, name, repo)
	}
	_, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: "main"})
	skipIfJJSandboxUnavailable(t, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) == 0 || asked[len(asked)-1] < 3 || left < 6*time.Hour {
		t.Fatalf("asked %v, content deadline %s", asked, left)
	}
}

// windowForTest is 1 h for the base read (no items yet) and 7 h once the items are counted, so a
// context that keeps the base window cannot pass for the scaled one.
func windowForTest(n int) time.Duration {
	if n == 0 {
		return time.Hour
	}
	return 7 * time.Hour
}

// The proof phase budget is the window's rest, never below the old 60 s.
func TestProofPhaseTimeoutIsTheRestOfTheWindowWithAFloor(t *testing.T) {
	for _, tc := range []struct{ rest, want time.Duration }{
		{-time.Second, integrationPhaseTimeout}, {0, integrationPhaseTimeout}, {30 * time.Second, integrationPhaseTimeout},
		{5 * time.Minute, 5 * time.Minute}, {7 * time.Hour, 7 * time.Hour},
	} {
		if got := proofPhaseTimeout(tc.rest); got != tc.want {
			t.Errorf("rest %s: %s, want %s", tc.rest, got, tc.want)
		}
	}
}

// The forge-free phase budget of the engine is its phaseTimeout (60 s when unset): after two
// minutes a new tip is exhausted by default and not with a scaled phase.
func TestIntegrationPhaseBudgetFollowsTheEngine(t *testing.T) {
	t0 := time.Unix(1000, 0)
	clock := t0
	for name, tc := range map[string]struct {
		phase time.Duration
		want  error
	}{"default": {0, errIntegrationBudget}, "scaled": {time.Hour, nil}} {
		clock = t0
		e := integrationEngine{now: func() time.Time { return clock }, phaseTimeout: tc.phase}
		r := &integrationRun{engine: e}
		if err := r.beginTip(); err != nil {
			t.Fatalf("%s: first tip: %v", name, err)
		}
		clock = t0.Add(2 * time.Minute)
		if err := r.beginTip(); err != tc.want {
			t.Errorf("%s: second tip after 2 min: %v, want %v", name, err, tc.want)
		}
	}
}

// The engine budget of the proof phase is the scaled one: a fake clock that moves 15 s on every
// ancestry proof (one per distinct tip, inside the 20 s tip budget) would exhaust the old 60 s
// phase at the fifth tip; with the window's rest no tip is exhausted.
func TestObserveProofPhaseBudgetIsTheScaledOne(t *testing.T) {
	// A synthetic clock and unsynchronized hooks: one item at a time keeps the test deterministic.
	repo := observeRepo(t)
	for _, n := range []string{"p1", "p2", "p3", "p4", "p5", "p6"} {
		commitIn(t, observeAdd(t, repo, n), n, n+"\n")
	}
	obs := defaultWorktreeObserver()
	obs.window = func(_ context.Context, n int) time.Duration { return windowForTest(n) }
	clock := time.Unix(1000, 0)
	obs.refs.integration.now = func() time.Time { return clock }
	ancestry := obs.refs.ancestry
	obs.refs.ancestry = func(ctx context.Context, s Snapshot, target, base string) (bool, error) {
		clock = clock.Add(15 * time.Second)
		return ancestry(ctx, s, target, base)
	}
	o, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase, Width: 1})
	if err != nil {
		t.Fatal(err)
	}
	proved := 0
	for _, w := range o.Worktrees {
		if w.Incomplete == "proof-budget-exhausted" {
			t.Errorf("%s: proof-budget-exhausted although the window leaves hours", filepath.Base(w.Path))
		}
		if !w.Primary && w.Incomplete == "" {
			proved++
		}
	}
	if proved < 6 {
		t.Fatalf("only %d worktrees were judged", proved)
	}
}

// The proof phase stays inside the caller's own deadline (--timeout): a window of 7 h under a
// 30 minute parent leaves the content reads and the proof phase at most 30 minutes.
func TestObserveWindowsStayInsideTheParentDeadline(t *testing.T) {
	// A synthetic clock and unsynchronized hooks: one item at a time keeps the test deterministic.
	repo := observeRepo(t)
	commitIn(t, observeAdd(t, repo, "q1"), "q", "q\n")
	obs := defaultWorktreeObserver()
	obs.window = func(_ context.Context, n int) time.Duration { return windowForTest(n) }
	var left, proofLeft time.Duration
	read := obs.read
	obs.read = func(ctx context.Context, bin, dir string, args ...string) (string, error) {
		if d, ok := ctx.Deadline(); ok && left == 0 {
			left = time.Until(d)
		}
		return read(ctx, bin, dir, args...)
	}
	ancestry := obs.refs.ancestry
	obs.refs.ancestry = func(ctx context.Context, s Snapshot, target, base string) (bool, error) {
		if d, ok := ctx.Deadline(); ok && proofLeft == 0 {
			proofLeft = time.Until(d)
		}
		return ancestry(ctx, s, target, base)
	}
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if _, err := obs.observe(parent, ObserveOptions{Path: repo, Base: observeBase, Width: 1}); err != nil {
		t.Fatal(err)
	}
	for name, d := range map[string]time.Duration{"content": left, "proof": proofLeft} {
		if d <= 0 || d > 30*time.Minute {
			t.Errorf("%s phase deadline %s, want within the 30 minute parent", name, d)
		}
	}
}
