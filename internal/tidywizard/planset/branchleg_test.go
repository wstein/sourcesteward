// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"errors"
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
	events   []string
	legs     map[string]workspace.V3Result // by retirement sub-plan id
	noLeg    map[string]bool
	retired  []workspace.V3Result
	checkErr error
}

func (l *legApplier) Apply(ctx context.Context, s Step) workspace.V3Result {
	l.events = append(l.events, "apply:"+string(s.SubPlan.Kind)+":"+s.SubPlan.Worktree)
	return l.fakeApplier.Apply(ctx, s)
}

func (l *legApplier) CheckBranchLeg(_ context.Context, s Step) error {
	l.events = append(l.events, "check:"+s.SubPlan.Worktree)
	return l.checkErr
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
	want := "check:/w/w1,check:/w/w2,apply:retire:/w/w1,leg:/w/w1,apply:retire:/w/w2,leg:/w/w2" // every choice is checked before the first effect
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

func TestAChosenBranchDeletionWithoutALegIsReportedAsPartial(t *testing.T) {
	w := withBranch("w1") // the owner chose the deletion, the retirement plan has no leg (HEAD attached to the base)
	dir, set := stored(t, w)
	l := &legApplier{noLeg: map[string]bool{}}
	l.noLeg[set.SubPlans[0].ID] = true
	res := Apply(context.Background(), req(dir, set, human, l))
	if res.Status != "partial" || len(res.Outcomes) != 2 {
		t.Fatalf("%+v", res)
	}
	if o := res.Outcomes[1]; o.Kind != KindBranchLeg || o.Status != StatusSkipped || o.Code != CodeBranchLegAbsent {
		t.Fatalf("%+v", o)
	}
}

func TestACheckThatRefusesTheLegStopsTheSetBeforeAnyEffect(t *testing.T) {
	w1, w2 := withBranch("w1"), withBranch("w2")
	dir, set := stored(t, w1, w2)
	l := &legApplier{checkErr: errors.New("the plan deletes another branch")}
	res := Apply(context.Background(), req(dir, set, human, l))
	if res.Status != "refused" || res.Code != CodeBranchLegMismatch || len(res.Outcomes) != 0 {
		t.Fatalf("%+v", res)
	}
	for _, e := range l.events {
		if strings.HasPrefix(e, "apply:") || strings.HasPrefix(e, "leg:") {
			t.Fatalf("an effect ran before the refusal: %v", l.events)
		}
	}
}

func TestOnlyRetirementsWithTheBranchEffectAreChecked(t *testing.T) {
	plain := own(tidywizard.KindWorktree, "r", "plain")
	branch := own(tidywizard.KindBranch, "r", "b")
	branch.Effects = []string{"delete-branch:b"} // a branch sub-plan is not a retirement: no leg and no check
	dir, set := stored(t, plain, branch)
	l := &legApplier{checkErr: errors.New("must not be asked")}
	res := Apply(context.Background(), req(dir, set, human, l))
	if res.Status != "ok" || len(res.Outcomes) != 2 {
		t.Fatalf("%+v", res)
	}
	for _, e := range l.events {
		if strings.HasPrefix(e, "leg:") || strings.HasPrefix(e, "check:") {
			t.Fatalf("%v", l.events)
		}
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
	if res.Outcomes[3].Code != "cancelled" {
		t.Fatalf("the leg of a cancelled retirement is cancelled, not a failed one: %+v", res.Outcomes[3])
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

func chosen(branch string) []Item {
	return []Item{{ID: "i", Effects: []string{"remove-registration", "remove-files", "delete-branch:" + branch}, Task: "t", OwnerSource: owner.SourceTaskRecord}}
}

func TestV3ApplierRefusesALegThatWouldDeleteAnotherBranchThanTheChosenOne(t *testing.T) {
	mk := func(ref string, hasLeg bool) V3Applier {
		return V3Applier{
			ReadFunc: func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
				return workspace.V3StoredRetirement{Plan: workspace.V3Plan{PlanID: "p"}}, nil
			},
			LegOf: func(workspace.V3Plan) (workspace.V3BranchLeg, bool) { return workspace.V3BranchLeg{Ref: ref}, hasLeg },
		}
	}
	step := func(items []Item) Step {
		return Step{SubPlan: SubPlan{ID: "p", StateDir: "/s", Kind: KindRetire}, Items: items}
	}
	if err := mk("refs/heads/feat", true).CheckBranchLeg(context.Background(), step(chosen("feat"))); err != nil {
		t.Errorf("the chosen branch is the one the plan deletes: %v", err)
	}
	if err := mk("refs/heads/other", true).CheckBranchLeg(context.Background(), step(chosen("feat"))); err == nil {
		t.Error("the plan deletes another branch than the chosen one")
	}
	for _, ref := range []string{"refs/heads/x-feat", "refs/heads/feat-2", "refs/heads/team/feat", "refs/heads/feat/sub", "refs/heads/Feat"} {
		if err := mk(ref, true).CheckBranchLeg(context.Background(), step(chosen("feat"))); err == nil {
			t.Errorf("the plan deletes %s, which only resembles the chosen branch feat", ref)
		}
	}
	if err := mk("", false).CheckBranchLeg(context.Background(), step(chosen("feat"))); err != nil {
		t.Errorf("no leg is reported afterwards, not refused: %v", err)
	}
	two := chosen("feat")
	two[0].Effects = append(two[0].Effects, "delete-branch:other")
	if err := mk("refs/heads/feat", true).CheckBranchLeg(context.Background(), step(two)); err == nil {
		t.Error("two chosen branch deletions for one worktree are ambiguous")
	}
	failing := V3Applier{ReadFunc: func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
		return workspace.V3StoredRetirement{}, errors.New("unreadable")
	}}
	if err := failing.CheckBranchLeg(context.Background(), step(chosen("feat"))); err == nil {
		t.Error("a plan that cannot be read cannot be checked")
	}
}

func TestV3ApplierTreatsACompletedRetirementWithTheBranchGoneAsAlreadyApplied(t *testing.T) {
	calls := 0
	a := V3Applier{
		ApplyFunc: func(context.Context, workspace.V3ApplyOptions) workspace.V3Result {
			calls++
			return refusedWith("evidence-changed")
		},
		ReadFunc: func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
			return workspace.V3StoredRetirement{Plan: workspace.V3Plan{PlanID: "p"}, Completed: true, BranchGone: true}, nil
		},
		LegFunc: func(context.Context, workspace.V3BranchLeg, workspace.V3Result) workspace.V3Result {
			t.Error("the branch is already gone: no leg is run")
			return workspace.V3Result{Status: "ok"}
		},
	}
	st := Step{SubPlan: SubPlan{ID: "p", StateDir: "/s", Kind: KindRetire}, Items: chosen("feat")}
	r := a.Apply(context.Background(), st)
	if r.Status != "ok" || calls != 0 {
		t.Fatalf("%+v calls %d", r, calls)
	}
	if leg, ok := a.ApplyBranchLeg(context.Background(), st, r); !ok || leg.Status != "ok" {
		t.Fatalf("the replay of the leg: %v %+v", ok, leg)
	}
	// The branch is still there (or at another tip): the ordinary apply decides, and its stale
	// detection is not weakened.
	a.ReadFunc = func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
		return workspace.V3StoredRetirement{Plan: workspace.V3Plan{PlanID: "p"}, Completed: true}, nil
	}
	if r := a.Apply(context.Background(), st); r.Status == "ok" || calls != 1 {
		t.Fatalf("%+v calls %d", r, calls)
	}
	// A retirement that was not completed is applied normally even when the branch is gone.
	a.ReadFunc = func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
		return workspace.V3StoredRetirement{Plan: workspace.V3Plan{PlanID: "p"}, BranchGone: true}, nil
	}
	if r := a.Apply(context.Background(), st); r.Status == "ok" || calls != 2 {
		t.Fatalf("%+v calls %d", r, calls)
	}
	// A reading failure falls back to the ordinary apply.
	a.ReadFunc = func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
		return workspace.V3StoredRetirement{}, errors.New("unreadable")
	}
	if r := a.Apply(context.Background(), st); r.Status == "ok" || calls != 3 {
		t.Fatalf("%+v calls %d", r, calls)
	}
	// Only a retirement is replayed this way.
	a.ReadFunc = func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
		t.Error("a branch sub-plan reads no retirement")
		return workspace.V3StoredRetirement{}, nil
	}
	a.Apply(context.Background(), Step{SubPlan: SubPlan{ID: "q", StateDir: "/s", Kind: KindBranches}, Items: chosen("feat")})
	// Without a chosen branch deletion the retirement is never replayed this way: the owner did not
	// select the branch, so its disappearance is a changed fact like any other.
	a.ReadFunc = func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
		t.Error("no branch deletion was chosen: nothing to read")
		return workspace.V3StoredRetirement{Completed: true, BranchGone: true}, nil
	}
	before := calls
	plain := Step{SubPlan: SubPlan{ID: "p", StateDir: "/s", Kind: KindRetire}, Items: []Item{{ID: "i", Effects: []string{"remove-registration"}}}}
	if r := a.Apply(context.Background(), plain); r.Status == "ok" || calls != before+1 {
		t.Fatalf("%+v calls %d", r, calls)
	}
}

func TestACheckWithoutAChosenBranchReadsNothing(t *testing.T) {
	a := V3Applier{ReadFunc: func(context.Context, string, string) (workspace.V3StoredRetirement, error) {
		t.Error("no branch deletion was chosen: the plan must not be read")
		return workspace.V3StoredRetirement{}, nil
	}}
	if err := a.CheckBranchLeg(context.Background(), Step{SubPlan: SubPlan{Kind: KindRetire}, Items: []Item{{ID: "i", Effects: []string{"remove-files"}}}}); err != nil {
		t.Fatal(err)
	}
}

type realLeg struct {
	repo, wt, state string
	set             Set
	git             func(dir string, args ...string) string
	req             ApplyRequest
}

// newRealLeg creates a worktree with the workspace operations, switches it to a branch, merges that
// into the base, and stores a plan set that retires the worktree and deletes the merged branch
// (an owned retirement, so the creation record is found in the state directory). chosen is the
// branch deletion the owner selected with the item.
func newRealLeg(t *testing.T, chosen string) *realLeg {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	f := &realLeg{repo: filepath.Join(root, "repo"), state: filepath.Join(root, "state")}
	parent := filepath.Join(root, "parent") // holds only the worktree: sibling evidence must be stable
	f.wt = filepath.Join(parent, "wt")
	for _, d := range []struct {
		path string
		mode os.FileMode
	}{{f.state, 0700}, {f.repo, 0755}, {parent, 0755}} {
		if err := os.Mkdir(d.path, d.mode); err != nil {
			t.Fatal(err)
		}
	}
	f.git = func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	f.git(f.repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(f.repo, "file"), []byte("content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.git(f.repo, "add", "file")
	f.git(f.repo, "commit", "-q", "-m", "base")
	planned := workspace.PlanCreate(context.Background(), workspace.PlanOptions{Path: f.repo, Base: "refs/heads/main", Destination: f.wt, Task: "task1", Owner: "claude", StateDir: f.state, CallerDir: f.repo})
	if planned.Plan == nil {
		t.Fatalf("create plan: %+v", planned)
	}
	if r := workspace.Apply(context.Background(), workspace.ApplyOptions{StateDir: f.state, PlanID: planned.Plan.PlanID, Authorize: true}); r.Status != "ok" {
		t.Fatalf("create apply: %+v", r)
	}
	f.git(f.wt, "switch", "-q", "-c", "feat")
	f.git(f.wt, "commit", "-q", "--allow-empty", "-m", "work")
	f.git(f.repo, "merge", "-q", "--no-ff", "-m", "merge", "feat")

	planner := NewV3Planner([]RepoSpec{{Name: "r", Path: f.repo, Base: "refs/heads/main"}}, []derive.Store{{Path: f.state}})
	c := cand(tidywizard.KindWorktree, "r", "wt", tidywizard.VerdictSafe, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	c.Path, c.ID, c.Task, c.Branch = f.wt, "worktree:r:"+f.wt, "task1", "feat"
	c.Effects = []string{"remove-registration", "remove-files", "delete-branch:" + chosen}
	set, _, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{c}, Chosen: []string{c.ID}, Mode: human}, planner)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := Store(f.state, set); err != nil {
		t.Fatal(err)
	}
	f.set = set
	f.req = ApplyRequest{StateDirs: []string{f.state}, PlanID: set.PlanID, Mode: human, Authorize: true, Applier: V3Applier{ApplyFunc: workspace.ApplyTidyV3}}
	return f
}

func (f *realLeg) branchExists(name string) bool {
	return strings.Contains(f.git(f.repo, "branch", "--list", name), name)
}

func TestBranchLegOnARealRepository(t *testing.T) {
	f := newRealLeg(t, "feat")
	res := Apply(context.Background(), f.req)
	if res.Status != "ok" || len(res.Outcomes) != 2 || res.Outcomes[1].Kind != KindBranchLeg || res.Outcomes[1].Status != StatusApplied {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(f.wt); !os.IsNotExist(err) {
		t.Error("the worktree must be gone")
	}
	if f.branchExists("feat") {
		t.Error("the merged branch must be deleted by the leg")
	}
	// Both journals (the retirement and the branch deletion) carry the mode evidence of the run,
	// and history shows it.
	h := workspace.HistoryTidyV3(context.Background(), workspace.V3HistoryOptions{Path: f.repo, StateDir: f.state})
	withMode := 0
	for _, e := range h.Entries {
		if e.RunMode != nil && e.RunMode.Mode == "human" && e.RunMode.Caller == human.Caller {
			withMode++
		}
	}
	if h.Status != "ok" || len(h.Entries) != 2 || withMode != 2 {
		t.Fatalf("history %+v", h)
	}
}

func TestReplayOfACompletedSetWithTheLegIsAVerifiedNoOp(t *testing.T) {
	f := newRealLeg(t, "feat")
	if res := Apply(context.Background(), f.req); res.Status != "ok" {
		t.Fatalf("first apply: %+v", res)
	}
	res := Apply(context.Background(), f.req)
	if res.Status != "ok" || len(res.Outcomes) != 2 || res.Outcomes[0].Status != StatusApplied || res.Outcomes[1].Status != StatusApplied {
		t.Fatalf("the replay of a completed set must be ok: %+v", res)
	}
}

func TestReplayStillDetectsABranchThatWasRecreatedAtAnotherTip(t *testing.T) {
	f := newRealLeg(t, "feat")
	if res := Apply(context.Background(), f.req); res.Status != "ok" {
		t.Fatalf("first apply: %+v", res)
	}
	f.git(f.repo, "branch", "feat", "HEAD") // same name, a tip the plan never recorded
	res := Apply(context.Background(), f.req)
	if res.Status == "ok" || res.Outcomes[0].Status != StatusStale {
		t.Fatalf("a branch that moved must not be replayed as applied: %+v", res)
	}
	if !f.branchExists("feat") {
		t.Error("the replay must not delete the recreated branch")
	}
}

func TestAChosenBranchThatTheRetirementWouldNotDeleteRefusesBeforeAnyEffect(t *testing.T) {
	f := newRealLeg(t, "other") // the worktree is attached to feat, the owner chose other
	res := Apply(context.Background(), f.req)
	if res.Status != "refused" || res.Code != CodeBranchLegMismatch || len(res.Outcomes) != 0 {
		t.Fatalf("%+v", res)
	}
	if _, err := os.Stat(f.wt); err != nil || !f.branchExists("feat") {
		t.Fatal("nothing may have changed")
	}
}
