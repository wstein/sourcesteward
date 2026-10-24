// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

type legApplier struct {
	fakeApplier
	events  []string
	legs    map[string]workspace.V3Result // by retirement sub-plan id
	noLeg   map[string]bool
	retired []workspace.V3Result
}

func (l *legApplier) Apply(ctx context.Context, s Step) workspace.V3Result {
	l.events = append(l.events, "apply:"+string(s.SubPlan.Kind)+":"+s.SubPlan.Worktree)
	return l.fakeApplier.Apply(ctx, s)
}

func (l *legApplier) ApplyBranchLeg(_ context.Context, s Step, retirement workspace.V3Result) (workspace.V3Result, bool) {
	l.events = append(l.events, "leg:"+s.SubPlan.Worktree)
	l.retired = append(l.retired, retirement)
	if l.noLeg[s.SubPlan.ID] {
		return workspace.V3Result{}, false
	}
	if r, ok := l.legs[s.SubPlan.ID]; ok {
		return r, true
	}
	return workspace.V3Result{Status: "ok"}, true
}

// withBranch is an own worktree candidate whose effects delete the branch after the retirement.
func withBranch(subject string) tidywizard.Candidate {
	c := own(tidywizard.KindWorktree, "r", subject)
	c.Effects = []string{"remove-registration", "remove-files", "delete-branch:feat-" + subject}
	return c
}

func retireID(set Set, worktree string) string {
	for _, s := range set.SubPlans {
		if s.Kind == KindRetire && s.Worktree == worktree {
			return s.ID
		}
	}
	return ""
}

func TestBranchLegRunsRightAfterItsSuccessfulRetirement(t *testing.T) {
	w1, w2 := withBranch("w1"), withBranch("w2")
	dir, set := stored(t, w1, w2)
	l := &legApplier{}
	res := Apply(context.Background(), req(dir, set, human, l))
	if res.Status != "ok" || len(res.Outcomes) != 4 {
		t.Fatalf("%+v", res)
	}
	want := "apply:retire:/w/w1,leg:/w/w1,apply:retire:/w/w2,leg:/w/w2"
	if strings.Join(l.events, ",") != want {
		t.Fatalf("events %v", l.events)
	}
	leg := res.Outcomes[1]
	if leg.Kind != KindBranchLeg || leg.SubPlan != retireID(set, "/w/w1") || leg.Status != StatusApplied || leg.Worktree != "/w/w1" {
		t.Fatalf("%+v", leg)
	}
	if l.retired[0].Status != "ok" {
		t.Error("the leg is handed the successful retirement result")
	}
}

func TestBranchLegIsSkippedWhenItsRetirementDidNotSucceed(t *testing.T) {
	for _, code := range []string{"evidence-changed", "dirty-worktree"} {
		w1, w2 := withBranch("w1"), withBranch("w2")
		dir, set := stored(t, w1, w2)
		l := &legApplier{}
		l.results = map[string]workspace.V3Result{retireID(set, "/w/w1"): refusedWith(code)}
		res := Apply(context.Background(), req(dir, set, human, l))
		if res.Status != "partial" {
			t.Fatalf("%s: %+v", code, res)
		}
		leg := res.Outcomes[1]
		if leg.Kind != KindBranchLeg || leg.Status != StatusSkipped || leg.Code != "retirement-not-applied" || leg.Worktree != "/w/w1" {
			t.Errorf("%s: %+v", code, leg)
		}
		if strings.Contains(strings.Join(l.events, ","), "leg:/w/w1") {
			t.Errorf("%s: the leg ran after a failed retirement: %v", code, l.events)
		}
		if res.Outcomes[3].Status != StatusApplied {
			t.Errorf("%s: the other worktree is independent: %+v", code, res.Outcomes)
		}
	}
}

func TestBranchLegOnlyForItemsThatSelectedTheBranchDeletion(t *testing.T) {
	plain := own(tidywizard.KindWorktree, "r", "plain") // effects: remove only
	dir, set := stored(t, plain)
	l := &legApplier{}
	res := Apply(context.Background(), req(dir, set, human, l))
	if res.Status != "ok" || len(res.Outcomes) != 1 || len(l.events) != 1 {
		t.Fatalf("%+v events %v", res, l.events)
	}
	// Only the branch deletion effect asks for the leg.
	other := own(tidywizard.KindWorktree, "r", "other")
	other.Effects = []string{"remove-registration", "delete-tracking-ref:origin/x"}
	dir2, set2 := stored(t, other)
	l2 := &legApplier{}
	if res := Apply(context.Background(), req(dir2, set2, human, l2)); len(res.Outcomes) != 1 || len(l2.events) != 1 {
		t.Fatalf("a tracking deletion is not a branch leg: %+v %v", res, l2.events)
	}
	// A skipped retirement without the effect has no skipped leg either.
	l = &legApplier{}
	l.results = map[string]workspace.V3Result{set.SubPlans[0].ID: refusedWith("evidence-changed")}
	if res := Apply(context.Background(), req(dir, set, human, l)); len(res.Outcomes) != 1 {
		t.Fatalf("%+v", res.Outcomes)
	}
}

func TestBranchLegWithoutALegForThePlanIsNotReported(t *testing.T) {
	w := withBranch("w1") // for example HEAD attached to the base branch: the plan has no leg
	dir, set := stored(t, w)
	l := &legApplier{noLeg: map[string]bool{}}
	l.noLeg[set.SubPlans[0].ID] = true
	res := Apply(context.Background(), req(dir, set, human, l))
	if res.Status != "ok" || len(res.Outcomes) != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestBranchLegFailureMakesTheSetPartialAndIsReported(t *testing.T) {
	w := withBranch("w1")
	dir, set := stored(t, w)
	l := &legApplier{legs: map[string]workspace.V3Result{}}
	l.legs[set.SubPlans[0].ID] = refusedWith("selected-base")
	res := Apply(context.Background(), req(dir, set, human, l))
	if res.Status != "partial" || len(res.Outcomes) != 2 {
		t.Fatalf("%+v", res)
	}
	if o := res.Outcomes[1]; o.Kind != KindBranchLeg || o.Status != StatusFailed || o.Code != "selected-base" {
		t.Fatalf("%+v", o)
	}
	l.legs[set.SubPlans[0].ID] = refusedWith("evidence-changed")
	if res := Apply(context.Background(), req(dir, set, human, l)); res.Outcomes[1].Status != StatusStale {
		t.Fatalf("a changed branch tip is stale: %+v", res.Outcomes[1])
	}
}

func TestBranchLegIsSkippedWhenTheRunIsCancelledAfterTheRetirement(t *testing.T) {
	w := withBranch("w1")
	dir, set := stored(t, w)
	ctx, cancel := context.WithCancel(context.Background())
	l := &legApplier{}
	l.cancel = cancel
	res := Apply(ctx, req(dir, set, human, l))
	if len(res.Outcomes) != 2 || res.Outcomes[1].Status != StatusSkipped || res.Outcomes[1].Code != "cancelled" {
		t.Fatalf("%+v", res)
	}
	if strings.Contains(strings.Join(l.events, ","), "leg:") {
		t.Fatalf("no leg after cancellation: %v", l.events)
	}
}

func TestBranchLegOfACancelledRetirementIsSkippedToo(t *testing.T) {
	w1, w2 := withBranch("w1"), withBranch("w2")
	dir, set := stored(t, w1, w2)
	ctx, cancel := context.WithCancel(context.Background())
	l := &legApplier{}
	l.cancel = cancel
	res := Apply(ctx, req(dir, set, human, l))
	var got []string
	for _, o := range res.Outcomes {
		got = append(got, string(o.Kind)+":"+o.Status)
	}
	if strings.Join(got, ",") != "retire:applied,branch-leg:skipped,retire:skipped,branch-leg:skipped" {
		t.Fatalf("%v", got)
	}
}

func TestItemRecordsTheSelectedEffectsAndTheyAreBoundToThePlanID(t *testing.T) {
	_, set := stored(t, withBranch("w1"))
	if len(set.Items) != 1 || strings.Join(set.Items[0].Effects, ",") != "remove-registration,remove-files,delete-branch:feat-w1" {
		t.Fatalf("%+v", set.Items)
	}
	c := set.clone()
	c.Items[0].Effects = []string{"remove-registration", "remove-files"}
	if c.computeID() == set.PlanID {
		t.Fatal("dropping the branch effect must change the plan id")
	}
}

func TestV3ApplierRunsTheBranchLegOfTheRetirementPlan(t *testing.T) {
	plan := &workspace.V3Plan{PlanID: "ret1"}
	var gotLeg workspace.V3BranchLeg
	var gotRetirement workspace.V3Result
	a := V3Applier{
		LegOf: func(p workspace.V3Plan) (workspace.V3BranchLeg, bool) {
			if p.PlanID != "ret1" {
				t.Errorf("leg of %q", p.PlanID)
			}
			return workspace.V3BranchLeg{PlanID: "ret1", Ref: "refs/heads/feat"}, true
		},
		LegFunc: func(_ context.Context, leg workspace.V3BranchLeg, r workspace.V3Result) workspace.V3Result {
			gotLeg, gotRetirement = leg, r
			return workspace.V3Result{Status: "ok"}
		},
	}
	ret := workspace.V3Result{Status: "ok", Plan: plan}
	r, ok := a.ApplyBranchLeg(context.Background(), Step{}, ret)
	if !ok || r.Status != "ok" || gotLeg.Ref != "refs/heads/feat" || gotRetirement.Plan != plan {
		t.Fatalf("%v %+v %+v", ok, gotLeg, gotRetirement)
	}
	noLeg := V3Applier{LegOf: func(workspace.V3Plan) (workspace.V3BranchLeg, bool) { return workspace.V3BranchLeg{}, false }}
	if _, ok := noLeg.ApplyBranchLeg(context.Background(), Step{}, ret); ok {
		t.Error("no leg for the plan")
	}
	if _, ok := a.ApplyBranchLeg(context.Background(), Step{}, workspace.V3Result{Status: "ok"}); ok {
		t.Error("a result without a plan has no leg")
	}
}

// TestBranchLegOnARealRepository creates a worktree with the workspace operations, switches it to a
// branch, merges that into the base, and retires the worktree and deletes the merged branch through
// one plan set (an owned retirement, so the creation record is found in the state directory).
func TestBranchLegOnARealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	parent := filepath.Join(root, "parent") // holds only the worktree: sibling evidence must be stable
	wt := filepath.Join(parent, "wt")
	state := filepath.Join(root, "state")
	for _, d := range []struct {
		path string
		mode os.FileMode
	}{{state, 0700}, {repo, 0755}, {parent, 0755}} {
		if err := os.Mkdir(d.path, d.mode); err != nil {
			t.Fatal(err)
		}
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git(repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "file"), []byte("content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "file")
	git(repo, "commit", "-q", "-m", "base")
	planned := workspace.PlanCreate(context.Background(), workspace.PlanOptions{Path: repo, Base: "refs/heads/main", Destination: wt, Task: "task1", Owner: "claude", StateDir: state, CallerDir: repo})
	if planned.Plan == nil {
		t.Fatalf("create plan: %+v", planned)
	}
	if r := workspace.Apply(context.Background(), workspace.ApplyOptions{StateDir: state, PlanID: planned.Plan.PlanID, Authorize: true}); r.Status != "ok" {
		t.Fatalf("create apply: %+v", r)
	}
	git(wt, "switch", "-q", "-c", "feat")
	git(wt, "commit", "-q", "--allow-empty", "-m", "work")
	git(repo, "merge", "-q", "--no-ff", "-m", "merge", "feat")

	planner := NewV3Planner([]RepoSpec{{Name: "r", Path: repo, Base: "refs/heads/main"}}, []derive.Store{{Path: state}})
	c := cand(tidywizard.KindWorktree, "r", "wt", tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	c.Path, c.ID, c.Task, c.Branch = wt, "worktree:r:"+wt, "task1", "feat"
	c.Effects = []string{"remove-registration", "remove-files", "delete-branch:feat"}
	set, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{c}, Chosen: []string{c.ID}, Mode: human}, planner)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := Store(state, set); err != nil {
		t.Fatal(err)
	}
	r := ApplyRequest{StateDirs: []string{state}, PlanID: set.PlanID, Mode: human, Authorize: true, Applier: V3Applier{ApplyFunc: workspace.ApplyTidyV3}}
	res := Apply(context.Background(), r)
	if res.Status != "ok" || len(res.Outcomes) != 2 || res.Outcomes[1].Kind != KindBranchLeg || res.Outcomes[1].Status != StatusApplied {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Error("the worktree must be gone")
	}
	if strings.Contains(git(repo, "branch", "--list", "feat"), "feat") {
		t.Error("the merged branch must be deleted by the leg")
	}
}
