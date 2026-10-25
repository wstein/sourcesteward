// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
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
	if _, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase}); err != nil {
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
