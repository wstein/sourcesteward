// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"testing"
)

// Retirement admits worktrees of the owner's own large repositories (SourceSteward
// has 1,113 tracked files): plan and apply must complete well above 1,024 files.
func TestTidyV3LegacyRetirementOfLargeWorktreesPlansAndApplies(t *testing.T) {
	for _, count := range []int{1100, 5000} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			if testing.Short() && count > 2000 {
				t.Skip("large fixture")
			}
			o, _ := tidyV3CommittedLegacyFixture(t, false, count)
			o.StateDir = privateDir(t)
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Status != "ok" || planned.Plan == nil {
				t.Fatalf("plan: %+v", planned.Diagnostics)
			}
			p := *planned.Plan
			applied := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Owner: o.Owner, CompletedWorktree: o.CompletedWorktree, ProtectWorktrees: o.ProtectWorktrees})
			if applied.Status != "ok" {
				t.Fatalf("apply: %+v", applied.Diagnostics)
			}
			if _, e := os.Lstat(o.CompletedWorktree); !os.IsNotExist(e) {
				t.Fatal("worktree remains", e)
			}
		})
	}
}

// Above the retirement bound the refusal is named, never a truncation.
func TestTidyV3LegacyRetirementRefusesAboveTheNamedBound(t *testing.T) {
	if testing.Short() {
		t.Skip("large fixture")
	}
	o, _ := tidyV3CommittedLegacyFixture(t, false, retirementManifestEntries+50)
	o.StateDir = privateDir(t)
	planned := defaultTidyV3RetirementCoordinator().plan(context.Background(), o)
	if planned.Status == "ok" || len(planned.Diagnostics) == 0 || planned.Diagnostics[0].Code != "retirement-file-limit" {
		t.Fatalf("want retirement-file-limit: %+v %+v", planned.Status, planned.Diagnostics)
	}
}
