// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"

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
