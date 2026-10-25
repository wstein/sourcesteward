// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func TestObservationWindowScalesWithTheRegistrations(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		n    int
		want time.Duration
	}{
		{0, 60 * time.Second},
		{1, 60*time.Second + 500*time.Millisecond},
		{61, 90*time.Second + 500*time.Millisecond},
		{999, 559*time.Second + 500*time.Millisecond},
		{1024, 572 * time.Second}, // the observation limit of registrations
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
	if got := observationWindow(WithObservationTimeout(context.Background(), 0), 61); got != 90*time.Second+500*time.Millisecond {
		t.Errorf("%s", got)
	}
}

// The window the observer applies to the content reads is the one for the count of registrations.
func TestObserveAppliesTheWindowOfTheRegistrationCount(t *testing.T) {
	repo := observeRepo(t)
	observeAdd(t, repo, "w1")
	observeAdd(t, repo, "w2")
	obs := defaultWorktreeObserver()
	var asked []int
	obs.window = func(_ context.Context, n int) time.Duration { asked = append(asked, n); return 7 * time.Hour }
	var left time.Duration
	obs.read = func(ctx context.Context, bin, dir string, args ...string) (string, error) {
		if d, ok := ctx.Deadline(); ok && left == 0 {
			left = time.Until(d)
		}
		return gitRead(ctx, bin, dir, args...)
	}
	if _, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase}); err != nil {
		t.Fatal(err)
	}
	if len(asked) == 0 || asked[len(asked)-1] != 3 {
		t.Fatalf("the window was asked for %v registrations, want the last to be 3", asked)
	}
	if left < 6*time.Hour {
		t.Fatalf("content reads ran under %s, want the 7h window", left)
	}
}

func TestObserveJJAppliesTheWindowOfTheRegistrationCount(t *testing.T) {
	repo := observeJJRepo(t)
	observeJJAdd(t, repo, "w1")
	obs := defaultWorktreeObserver()
	var asked []int
	obs.window = func(_ context.Context, n int) time.Duration { asked = append(asked, n); return 7 * time.Hour }
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
	if len(asked) == 0 || asked[len(asked)-1] != 2 || left < 6*time.Hour {
		t.Fatalf("asked %v, content deadline %s", asked, left)
	}
}
