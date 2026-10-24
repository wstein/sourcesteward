// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"testing"
)

func TestReadTidyV3RetirementTellsCompletionAndTheBranchState(t *testing.T) {
	w := ownedPositionFixture(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), w.Options)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	id := planned.Plan.PlanID
	read := func() V3StoredRetirement {
		t.Helper()
		r, err := ReadTidyV3Retirement(context.Background(), w.Options.StateDir, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Plan.PlanID != id {
			t.Fatalf("plan %q", r.Plan.PlanID)
		}
		return r
	}
	if r := read(); r.Completed || r.BranchGone {
		t.Fatalf("before the apply: %+v", r)
	}
	leg, ok := TidyV3BranchLeg(*planned.Plan)
	if !ok {
		t.Fatal("leg expected")
	}
	retirement := c.apply(context.Background(), ownedApplyOptions(w.Options, *planned.Plan))
	if retirement.Status != "ok" {
		t.Fatalf("retirement: %+v", retirement.Diagnostics)
	}
	if r := read(); !r.Completed || r.BranchGone {
		t.Fatalf("after the retirement the branch still exists: %+v", r)
	}
	if r := ApplyTidyV3BranchLeg(context.Background(), leg, retirement); r.Status != "ok" {
		t.Fatalf("leg: %+v", r.Diagnostics)
	}
	if r := read(); !r.Completed || !r.BranchGone {
		t.Fatalf("after the leg: %+v", r)
	}
	// A branch of the same name at another tip is not "gone": the replay must see it.
	gitFixture(t, w.Source, "branch", w.Branch, "HEAD")
	if r := read(); !r.Completed || r.BranchGone {
		t.Fatalf("a recreated branch: %+v", r)
	}
	if _, err := ReadTidyV3Retirement(context.Background(), w.Options.StateDir, "no-such-plan"); err == nil {
		t.Error("an unknown plan is an error")
	}
	if _, err := ReadTidyV3Retirement(context.Background(), "relative", id); err == nil {
		t.Error("a non-canonical state directory is an error")
	}
}

func TestReadTidyV3RetirementDestinationReappearsAndDetachedHead(t *testing.T) {
	w := ownedPositionFixture(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), w.Options)
	if planned.Status != "ok" {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	if r := c.apply(context.Background(), ownedApplyOptions(w.Options, *planned.Plan)); r.Status != "ok" {
		t.Fatalf("retirement: %+v", r.Diagnostics)
	}
	if e := os.Mkdir(w.Dest, 0700); e != nil {
		t.Fatal(e)
	}
	r, err := ReadTidyV3Retirement(context.Background(), w.Options.StateDir, planned.Plan.PlanID)
	if err != nil || r.Completed || r.BranchGone {
		t.Fatalf("a destination that exists again is not completed: %+v %v", r, err)
	}

	d := ownedPositionFixture(t)
	gitFixture(t, d.Dest, "switch", "-q", "--detach")
	dp := c.plan(context.Background(), d.Options)
	if dp.Status != "ok" || dp.Plan.Retirement.Target.HeadRef != "" {
		t.Fatalf("detached plan: %+v", dp.Diagnostics)
	}
	if r := c.apply(context.Background(), ownedApplyOptions(d.Options, *dp.Plan)); r.Status != "ok" {
		t.Fatalf("retirement: %+v", r.Diagnostics)
	}
	got, err := ReadTidyV3Retirement(context.Background(), d.Options.StateDir, dp.Plan.PlanID)
	if err != nil || !got.Completed || got.BranchGone {
		t.Fatalf("a detached head has no branch to be gone: %+v %v", got, err)
	}
}

func TestReadTidyV3RetirementOfAReferencePlanIsNeverCompleted(t *testing.T) {
	f := newHistoryFix(t, []string{"a1"})
	p := f.plans[0]
	r, err := ReadTidyV3Retirement(context.Background(), f.state, p.PlanID)
	if err != nil || r.Completed || r.BranchGone || r.Plan.PlanID != p.PlanID {
		t.Fatalf("%+v %v", r, err)
	}
}
