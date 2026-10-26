// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func siblingsOf(f *fakeApplier, id string) []string {
	for _, c := range f.calls {
		if c.SubPlan.ID == id {
			out := append([]string{}, c.Siblings...)
			sort.Strings(out)
			return out
		}
	}
	return nil
}

func retireIDs(set Set, repo string) []string {
	out := []string{}
	for _, s := range set.SubPlans {
		if s.Kind == KindRetire && s.Repo == repo {
			out = append(out, s.ID)
		}
	}
	sort.Strings(out)
	return out
}

func without(ids []string, id string) []string {
	out := []string{}
	for _, i := range ids {
		if i != id {
			out = append(out, i)
		}
	}
	return out
}

// Issue 285: a sub-plan is told which other sub-plans of its repository may have changed it.
func TestApplyNamesTheOtherSubPlansOfTheSameRepositoryAsSiblings(t *testing.T) {
	dir, set := stored(t,
		own(tidywizard.KindWorktree, "r", "a"), own(tidywizard.KindWorktree, "r", "b"), own(tidywizard.KindWorktree, "r", "c"),
		own(tidywizard.KindWorktree, "q", "d"), own(tidywizard.KindBranch, "r", "e"))
	f := &fakeApplier{}
	if res := Apply(context.Background(), req(dir, set, human, f)); res.Status != "ok" {
		t.Fatalf("%+v", res)
	}
	repoR := append(retireIDs(set, "r"), subPlanIDs(set, KindBranches, "r")...)
	sort.Strings(repoR)
	if len(repoR) != 4 {
		t.Fatalf("fixture: want 3 retirements and 1 branch plan of r, got %v", repoR)
	}
	for _, id := range repoR {
		if got, want := siblingsOf(f, id), without(repoR, id); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: siblings %v, want the others of its repository %v", id, got, want)
		}
	}
	q := retireIDs(set, "q")
	if got := siblingsOf(f, q[0]); len(got) != 0 {
		t.Errorf("a repository of its own has no sibling: %v", got)
	}
}

func subPlanIDs(set Set, kind SubKind, repo string) []string {
	out := []string{}
	for _, s := range set.SubPlans {
		if s.Kind == kind && s.Repo == repo {
			out = append(out, s.ID)
		}
	}
	return out
}

// The branch-deletion plan a leg applied is a sibling of the later rows of its repository only.
func TestApplyNamesTheAppliedBranchLegPlansAsSiblingsOfLaterRows(t *testing.T) {
	dir, set := stored(t, withBranch("a"), withBranch("b"), withBranch("c"), withBranch("d"))
	// Rows apply in the order of Ordered; the legs answer with plans of their own.
	order := []string{}
	for _, s := range set.Ordered() {
		order = append(order, s.ID)
	}
	legs := map[string]workspace.V3Result{}
	for i, id := range order {
		legs[id] = workspace.V3Result{Status: "ok", Plan: &workspace.V3Plan{PlanID: "leg" + string(rune('a'+i))}}
	}
	l := &legApplier{legs: legs}
	// The fourth leg fails, so it is no sibling of anything (nothing follows it anyway).
	if res := Apply(context.Background(), req(dir, set, human, l)); res.Status != "ok" {
		t.Fatalf("%+v", res)
	}
	for i, id := range order {
		got := siblingsOf(&l.fakeApplier, id)
		for j := range order {
			leg := "leg" + string(rune('a'+j))
			has := false
			for _, g := range got {
				has = has || g == leg
			}
			if has != (j < i) {
				t.Errorf("row %d: leg %d listed=%t, want %t (siblings %v)", i, j, has, j < i, got)
			}
		}
	}
}

// A leg that failed has no complete journal to name; a row after it is not told about it.
func TestApplyDoesNotNameTheLegPlanOfAFailedLeg(t *testing.T) {
	dir, set := stored(t, withBranch("a"), withBranch("b"))
	first := set.Ordered()[0].ID
	l := &legApplier{legs: map[string]workspace.V3Result{first: {Status: "error", Plan: &workspace.V3Plan{PlanID: "failedleg"}, Diagnostics: []workspace.Diagnostic{{Code: "evidence-changed", Message: "m"}}}}}
	Apply(context.Background(), req(dir, set, human, l))
	for _, c := range l.calls {
		for _, s := range c.Siblings {
			if s == "failedleg" {
				t.Fatalf("the plan of a failed leg was named: %v", c.Siblings)
			}
		}
	}
}

// Rows the run never reached are applied by nobody: a canceled run names nothing it did not do.
func TestApplyAfterCancellationStartsNoFurtherRow(t *testing.T) {
	dir, set := stored(t, own(tidywizard.KindWorktree, "r", "a"), own(tidywizard.KindWorktree, "r", "b"), own(tidywizard.KindWorktree, "r", "c"))
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeApplier{cancel: cancel}
	res := Apply(ctx, req(dir, set, human, f))
	if len(f.calls) != 1 || res.Status != "partial" {
		t.Fatalf("calls %d status %s", len(f.calls), res.Status)
	}
	// The next run is a new apply of the same set: every row names its siblings again.
	f2 := &fakeApplier{}
	Apply(context.Background(), req(dir, set, human, f2))
	if len(f2.calls) != 3 || len(f2.calls[0].Siblings) != 2 {
		t.Fatalf("resumed run: %d calls, siblings %v", len(f2.calls), f2.calls[0].Siblings)
	}
}

func TestV3ApplierHandsTheSiblingsToTheApply(t *testing.T) {
	var got workspace.V3ApplyOptions
	a := V3Applier{ApplyFunc: func(_ context.Context, o workspace.V3ApplyOptions) workspace.V3Result {
		got = o
		return workspace.V3Result{Status: "ok"}
	}}
	step := Step{SubPlan: SubPlan{ID: "p1", StateDir: "/s", Kind: KindBranches}, Siblings: []string{"p2", "p3"}}
	a.Apply(context.Background(), step)
	if !reflect.DeepEqual(got.SiblingPlans, []string{"p2", "p3"}) {
		t.Fatalf("siblings %v", got.SiblingPlans)
	}
	// The option is a copy: the apply cannot change the step.
	got.SiblingPlans[0] = "x"
	if step.Siblings[0] != "p2" {
		t.Fatal("the step shares its slice with the options")
	}
}

// A repository whose sub-plans live in two state directories: a plan of the other directory
// cannot be read by the apply of this one, so it is not named.
func TestApplyNamesOnlySiblingsOfTheSameStateDirectory(t *testing.T) {
	_, set := stored(t, own(tidywizard.KindWorktree, "r", "a"), own(tidywizard.KindWorktree, "r", "b"), own(tidywizard.KindWorktree, "r", "c"))
	other := stateDir(t)
	ordered := set.Ordered()
	moved := ordered[2].ID
	dir, forged := storeForged(t, set, func(s *Set) {
		for i := range s.SubPlans {
			if s.SubPlans[i].ID == moved {
				s.SubPlans[i].StateDir = other
			}
		}
	})
	f := &fakeApplier{}
	Apply(context.Background(), req(dir, forged, human, f))
	for _, c := range f.calls {
		if c.SubPlan.ID == moved {
			if len(c.Siblings) != 0 {
				t.Errorf("the plan alone in its state directory has siblings %v", c.Siblings)
			}
			continue
		}
		for _, s := range c.Siblings {
			if s == moved {
				t.Errorf("%s names %s of another state directory", c.SubPlan.ID, s)
			}
		}
	}
	if len(f.calls) != 3 {
		t.Fatalf("calls %d", len(f.calls))
	}
}
