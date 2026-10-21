// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"testing"
	"time"
)

func TestV3ApplyTimeoutScalesWithTheWorkWithoutACeiling(t *testing.T) {
	for _, tc := range []struct {
		effects, entries int
		want             time.Duration
	}{{0, 0, 60 * time.Second}, {1, 0, 65 * time.Second}, {54, 0, 330 * time.Second}, {1, 5000, 60*time.Second + 5*time.Second + 250*time.Second}, {10000, 0, 60*time.Second + 50000*time.Second}} {
		if got := v3ApplyTimeout(tc.effects, tc.entries); got != tc.want {
			t.Errorf("%+v: %s", tc, got)
		}
	}
}

// An explicit --timeout replaces every default deadline of the operation, the
// shorter inner ones included; without it the defaults stand.
func TestV3TimeoutOverrideReplacesDefaults(t *testing.T) {
	if got := v3Timeout(context.Background(), 10*time.Second); got != 10*time.Second {
		t.Fatalf("default %s", got)
	}
	ctx := withV3Timeout(context.Background(), 30*time.Minute)
	if got := v3Timeout(ctx, 10*time.Second); got != 30*time.Minute {
		t.Fatalf("override %s", got)
	}
	if got := v3Timeout(withV3Timeout(context.Background(), 0), time.Second); got != time.Second {
		t.Fatalf("zero override %s", got)
	}
}

// --timeout replaces the scaled default for the whole retirement apply and its
// subordinate contexts, both ways: a short one stops the apply with a named
// code, a long one succeeds although the default is deliberately tiny.
func TestTidyV3RetirementApplyHonoursTimeoutBothWays(t *testing.T) {
	oldBase, oldEffect, oldEntry := v3ApplyBaseTimeout, v3ApplyPerEffect, v3ApplyPerEntry
	t.Cleanup(func() { v3ApplyBaseTimeout, v3ApplyPerEffect, v3ApplyPerEntry = oldBase, oldEffect, oldEntry })
	run := func(t *testing.T, base, override time.Duration) V3Result {
		o, _ := tidyV3LegacyFixture(t, false)
		o.StateDir = privateDir(t)
		c := defaultTidyV3RetirementCoordinator()
		planned := c.plan(context.Background(), o)
		if planned.Status != "ok" || planned.Plan == nil || !v3RetirementEligible(*planned.Plan.Retirement) {
			t.Fatalf("plan: %+v", planned.Diagnostics)
		}
		v3ApplyBaseTimeout, v3ApplyPerEffect, v3ApplyPerEntry = base, 0, 0
		ctx := context.Background()
		if override > 0 {
			ctx = withV3Timeout(ctx, override)
		}
		return c.apply(ctx, V3ApplyOptions{StateDir: o.StateDir, PlanID: planned.Plan.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	}
	t.Run("long override beats a tiny default", func(t *testing.T) {
		if r := run(t, time.Nanosecond, 5*time.Minute); r.Status != "ok" {
			t.Fatalf("%s %+v", r.Status, r.Diagnostics)
		}
	})
	t.Run("short override stops a generous default", func(t *testing.T) {
		r := run(t, 10*time.Minute, time.Nanosecond)
		if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "canceled" {
			t.Fatalf("%s %+v", r.Status, r.Diagnostics)
		}
	})
}
