// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"

	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTidyV3EntryApplyPreservesNativeRecoveryAndRetry(t *testing.T) {
	dir, _, p := fixtureTidyV3NativeApply(t)
	o := V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}}
	mismatch := o
	mismatch.Online = true
	if r := ApplyTidyV3(context.Background(), mismatch); r.Status == "ok" || r.Plan != nil || r.Boundary.NetworkAttempted {
		t.Fatalf("online mismatch gained authority: %+v", r)
	}
	r := ApplyTidyV3(context.Background(), o)
	if r.Status != "ok" || validateV3Result(r) != nil {
		t.Fatalf("public v3 apply failed: %+v", r)
	}
	recovery := v3RecoveryFor(p, p.Candidates[0].ActionID, p.Candidates[0].OID)
	if gitFixture(t, dir, "rev-parse", recovery.Name) != p.Candidates[0].OID {
		t.Fatal("wrong committed-history recovery")
	}
	journal := filepath.Join(p.StateDir, "tidy-v3-operations", p.PlanID+".json")
	before := layoutProtectedFileAt(t, journal)
	if retry := ApplyTidyV3(context.Background(), o); retry.Status != "ok" || validateV3Result(retry) != nil {
		t.Fatalf("public completed retry failed: %+v", retry)
	}
	if !reflect.DeepEqual(layoutProtectedFileAt(t, journal), before) {
		t.Fatal("public completed retry rewrote journal")
	}
}

func TestTidyV3EntryCanceledApplyDoesNotReadStore(t *testing.T) {
	// A canceled caller must win before even qualifying an unavailable store.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := ApplyTidyV3(ctx, V3ApplyOptions{StateDir: filepath.Join(t.TempDir(), "missing"), PlanID: "plan", Authorize: true, ProtectWorktrees: []string{}})
	if r.Status == "ok" || r.Plan != nil || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "canceled" {
		t.Fatalf("cancellation lost to state access: %+v", r)
	}
}

// Without explicit authorization the public entry refuses before reading the
// store or selecting any coordinator, so every mode yields the same refusal and
// no native, hosting or journal effect. The mode coordinators repeat weaker
// checks with other diagnostics; asserting the code pins the entry boundary.
func TestTidyV3EntryApplyWithoutAuthorizationRefusesWithoutEffectsInEveryMode(t *testing.T) {
	snapshot := func(t *testing.T, dir string) string {
		return gitFixture(t, dir, "for-each-ref", "--format=%(refname) %(objectname) %(symref)") + "\n" + gitFixture(t, dir, "worktree", "list", "--porcelain")
	}
	refsDir, _, refsPlan := fixtureTidyV3NativeApply(t)
	tracking := fixtureTidyV3TrackingApply(t, 1)
	legacy, _ := tidyV3LegacyFixture(t, false)
	legacy.StateDir = privateDir(t)
	planned := defaultTidyV3RetirementCoordinator().plan(context.Background(), legacy)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("retirement plan unavailable: %+v", planned)
	}
	retirementPlan := *planned.Plan
	owned, _, task := fixtureTidyV3OriginalLargeOwnedTask(t)
	ownedPlanned := defaultTidyV3RetirementCoordinator().plan(context.Background(), owned)
	if ownedPlanned.Status != "ok" || ownedPlanned.Plan == nil || ownedPlanned.Plan.Mode != "owned-retirement" {
		t.Fatalf("owned retirement plan unavailable: %+v", ownedPlanned)
	}
	for _, tc := range []struct {
		mode string
		dir  string
		plan V3Plan
		o    V3ApplyOptions
	}{
		{"refs", refsDir, refsPlan, V3ApplyOptions{}},
		{"tracking-refs", tracking.dir, tracking.plan, V3ApplyOptions{Online: true}},
		{"owned-retirement", owned.Path, *ownedPlanned.Plan, V3ApplyOptions{Owner: task.Owner}},
		{"legacy-retirement", legacy.Path, retirementPlan, V3ApplyOptions{CompletedWorktree: legacy.CompletedWorktree, Owner: legacy.Owner, ProtectWorktrees: legacy.ProtectWorktrees}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			o := tc.o
			o.StateDir, o.PlanID = tc.plan.StateDir, tc.plan.PlanID
			if o.ProtectWorktrees == nil {
				o.ProtectWorktrees = []string{}
			}
			before := snapshot(t, tc.dir)
			r := ApplyTidyV3(context.Background(), o)
			if r.Status == "ok" || r.Plan != nil || r.Boundary.Complete || r.Boundary.NetworkAttempted || len(r.Actions) != 0 {
				t.Fatalf("unauthorized apply was admitted or acted: %+v", r)
			}
			if len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "authorization-required" {
				t.Fatalf("unauthorized apply was not refused at the entry: %+v", r.Diagnostics)
			}
			// The entry check precedes any store access: an unreadable store or an
			// unknown plan must not change the refusal into another diagnostic.
			unknown := o
			unknown.StateDir, unknown.PlanID = filepath.Join(t.TempDir(), "missing"), "unknown-plan"
			if r := ApplyTidyV3(context.Background(), unknown); r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "authorization-required" {
				t.Fatalf("unauthorized apply reached the store: %+v", r.Diagnostics)
			}
			if after := snapshot(t, tc.dir); after != before {
				t.Fatalf("unauthorized apply changed the repository:\n%s\n--\n%s", before, after)
			}
			if _, err := os.Stat(filepath.Join(tc.plan.StateDir, "tidy-v3-operations", tc.plan.PlanID+".json")); !os.IsNotExist(err) {
				t.Fatal("unauthorized apply wrote a journal", err)
			}
		})
	}
}
