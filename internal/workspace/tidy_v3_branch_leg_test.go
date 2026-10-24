// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"strings"
	"testing"
)

// retireWithLeg plans and applies the retirement of a switched worktree and
// returns the plan, its dependent branch leg and the retirement result.
func retireWithLeg(t *testing.T, w ownedPositionWork) (V3Plan, V3BranchLeg, V3Result) {
	t.Helper()
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), w.Options)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	leg, ok := TidyV3BranchLeg(*planned.Plan)
	if !ok {
		t.Fatal("an attached, integrated worktree has a branch leg")
	}
	applied := c.apply(context.Background(), ownedApplyOptions(w.Options, *planned.Plan))
	if applied.Status != "ok" {
		t.Fatalf("retirement: %+v", applied.Diagnostics)
	}
	return *planned.Plan, leg, applied
}

func requireBranchKept(t *testing.T, w ownedPositionWork) {
	t.Helper()
	if _, e := os.Stat(w.Source); e != nil || gitFixture(t, w.Source, "for-each-ref", "refs/heads/"+w.Branch) == "" {
		t.Fatal("refused leg deleted the branch")
	}
}

func TestTidyV3BranchLegDeletesTheIntegratedBranchAfterRetirement(t *testing.T) {
	w := ownedPositionFixture(t)
	plan, leg, retirement := retireWithLeg(t, w)
	if leg.Ref != "refs/heads/b" || leg.TipOID != plan.Retirement.Target.CommitID || leg.PlanID != plan.PlanID {
		t.Fatalf("leg does not record branch, tip and dependency: %+v", leg)
	}
	if gitFixture(t, w.Source, "for-each-ref", "refs/heads/b") == "" {
		t.Fatal("the retirement alone must not delete the branch")
	}
	r := ApplyTidyV3BranchLeg(context.Background(), leg, retirement)
	if r.Status != "ok" {
		t.Fatalf("leg: %+v", r.Diagnostics)
	}
	if gitFixture(t, w.Source, "for-each-ref", "refs/heads/b") != "" {
		t.Fatal("branch not deleted")
	}
}

func TestTidyV3BranchLegIsAbsentWhenThereIsNothingToDelete(t *testing.T) {
	t.Run("detached", func(t *testing.T) {
		source, creation, task := gitRetirementFixture(t)
		planned := defaultTidyV3RetirementCoordinator().plan(context.Background(), ownedDetachedOptions(creation, task, source))
		if _, ok := TidyV3BranchLeg(*planned.Plan); ok {
			t.Fatal("a detached head has no branch")
		}
	})
	{
		t.Run("base-branch", func(t *testing.T) {
			source, creation, task := gitRetirementFixture(t)
			base := gitFixture(t, source, "symbolic-ref", "--short", "HEAD")
			gitFixture(t, source, "switch", "-q", "--detach")
			gitFixture(t, task.Destination, "switch", "-q", base)
			o := ownedDetachedOptions(creation, task, source)
			o.Base = "refs/heads/" + base
			planned := defaultTidyV3RetirementCoordinator().plan(context.Background(), o)
			if planned.Status != "ok" || planned.Plan.Retirement.Target.HeadRef != "refs/heads/"+base || !v3RetirementEligible(*planned.Plan.Retirement) {
				t.Fatalf("fixture: %+v", planned.Diagnostics)
			}
			if _, ok := TidyV3BranchLeg(*planned.Plan); ok {
				t.Fatal("the base branch never gets a leg")
			}
		})
	}
	t.Run("retained", func(t *testing.T) {
		w := ownedPositionFixture(t)
		commitUnintegratedChange(t, w.Dest, "unintegrated")
		planned := defaultTidyV3RetirementCoordinator().plan(context.Background(), w.Options)
		if _, ok := TidyV3BranchLeg(*planned.Plan); ok {
			t.Fatal("a retained worktree has no leg")
		}
	})
}

func TestTidyV3BranchLegIsSkippedWhenTheRetirementFailed(t *testing.T) {
	w := ownedPositionFixture(t)
	_, leg, retirement := retireWithLeg(t, w)
	for _, status := range []string{"refused", "partial", "error"} {
		failed := retirement
		failed.Status = status
		r := ApplyTidyV3BranchLeg(context.Background(), leg, failed)
		if r.Status == "ok" || len(r.Actions) != 1 || r.Actions[0].Status != "skipped" {
			t.Fatalf("%s: leg not skipped: %+v", status, r)
		}
		requireBranchKept(t, w)
	}
}

func TestTidyV3BranchLegRefusesWhatSpec011Protects(t *testing.T) {
	cases := []struct{ kind, code string }{
		{"changed-tip", "evidence-changed"},
		{"attached-elsewhere", "workspace-used"},
		{"workspace-ref", "workspace-used"},
		{"symbolic-ref", "symbolic-reference"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			w := ownedPositionFixture(t)
			_, leg, retirement := retireWithLeg(t, w)
			switch tc.kind {
			case "changed-tip":
				gitFixture(t, w.Source, "update-ref", "refs/heads/b", gitFixture(t, w.Source, "rev-parse", "HEAD"))
			case "attached-elsewhere":
				gitFixture(t, w.Source, "worktree", "add", "-q", w.Dest+"-other", "b")
			case "workspace-ref":
				gitFixture(t, w.Source, "worktree", "add", "-q", "--detach", w.Dest+"-other", "b")
			case "symbolic-ref":
				gitFixture(t, w.Source, "symbolic-ref", "refs/heads/alias", "refs/heads/b")
			}
			r := ApplyTidyV3BranchLeg(context.Background(), leg, retirement)
			requireDiagnosticCode(t, r, tc.code)
			if !strings.Contains(gitFixture(t, w.Source, "for-each-ref", "refs/heads/b"), "refs/heads/b") {
				t.Fatal("refused leg deleted the branch")
			}
		})
	}
}

func TestTidyV3BranchLegHonoursTheStoredProofPolicy(t *testing.T) {
	w := ownedPositionFixtureWith(t, "rebase-whitespace")
	w.Options.AcceptWhitespaceDifferences = true
	_, leg, retirement := retireWithLeg(t, w)
	if r := ApplyTidyV3BranchLeg(context.Background(), leg, retirement); r.Status != "ok" {
		t.Fatalf("branch and worktree must be judged by the same policy: %+v", r.Diagnostics)
	}
	if gitFixture(t, w.Source, "for-each-ref", "refs/heads/b") != "" {
		t.Fatal("branch not deleted")
	}
}

func TestTidyV3BranchLegBindsTheRetirementItDependsOn(t *testing.T) {
	t.Run("other plan", func(t *testing.T) {
		w := ownedPositionFixture(t)
		_, leg, retirement := retireWithLeg(t, w)
		other := *retirement.Plan
		other.PlanID = strings.Repeat("a", len(other.PlanID))
		retirement.Plan = &other
		r := ApplyTidyV3BranchLeg(context.Background(), leg, retirement)
		if r.Status == "ok" || len(r.Actions) != 1 || r.Actions[0].Status != "skipped" {
			t.Fatalf("result of another plan accepted: %+v", r)
		}
		requireBranchKept(t, w)
	})
	t.Run("worktree still present", func(t *testing.T) {
		w := ownedPositionFixture(t)
		planned := defaultTidyV3RetirementCoordinator().plan(context.Background(), w.Options)
		leg, ok := TidyV3BranchLeg(*planned.Plan)
		if !ok {
			t.Fatal("no leg")
		}
		claimed := newV3Result("tidy-apply")
		claimed.Status, claimed.Plan = "ok", planned.Plan
		r := ApplyTidyV3BranchLeg(context.Background(), leg, claimed)
		if r.Status == "ok" || len(r.Actions) != 1 || r.Actions[0].Status != "skipped" {
			t.Fatalf("leg ran while the worktree exists: %+v", r)
		}
		requireBranchKept(t, w)
	})
}
