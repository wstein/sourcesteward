// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

type fakeApplier struct {
	calls   []Step
	results map[string]workspace.V3Result // by sub-plan id
	cancel  func()                        // called after the first step
}

func (f *fakeApplier) Apply(_ context.Context, s Step) workspace.V3Result {
	f.calls = append(f.calls, s)
	if f.cancel != nil {
		f.cancel()
	}
	if r, ok := f.results[s.SubPlan.ID]; ok {
		return r
	}
	return workspace.V3Result{Status: "ok"}
}

func refusedWith(code string) workspace.V3Result {
	return workspace.V3Result{Status: "refused", Diagnostics: []workspace.Diagnostic{{Code: code, Message: "m " + code}}}
}

func stateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "s")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// stored builds and stores a set of the candidates in a fresh state directory.
func stored(t *testing.T, cands ...tidywizard.Candidate) (string, Set) {
	t.Helper()
	dir := stateDir(t)
	set, err := build(t, cands, ids(cands...), human, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Store(dir, set); err != nil {
		t.Fatal(err)
	}
	return dir, set
}

// storeForged stores a set whose content a mutation changed and whose id is recomputed, the way
// only a forging writer could produce it (Build never makes such a set).
func storeForged(t *testing.T, set Set, mutate func(*Set)) (string, Set) {
	t.Helper()
	c := set.clone()
	mutate(&c)
	c.PlanID = c.computeID()
	dir := stateDir(t)
	if err := Store(dir, c); err != nil {
		t.Fatal(err)
	}
	return dir, c
}

func req(dir string, set Set, ev owner.Evidence, a Applier) ApplyRequest {
	return ApplyRequest{StateDirs: []string{dir}, PlanID: set.PlanID, Mode: ev, Authorize: true, Applier: a}
}

func TestApplyWithoutAuthorizationChangesNothing(t *testing.T) {
	dir, set := stored(t, own(tidywizard.KindWorktree, "r", "a"))
	f := &fakeApplier{}
	r := req(dir, set, human, f)
	r.Authorize = false
	res := Apply(context.Background(), r)
	if res.Status != "refused" || res.Code != CodeAuthorizationRequired || len(f.calls) != 0 {
		t.Fatalf("%+v calls %d", res, len(f.calls))
	}
}

func TestApplyRefusesAnUnknownOrDamagedSet(t *testing.T) {
	dir, set := stored(t, own(tidywizard.KindWorktree, "r", "a"))
	f := &fakeApplier{}
	r := req(dir, set, human, f)
	r.PlanID = "000000000000"
	if res := Apply(context.Background(), r); res.Status != "refused" || res.Code != CodePlanSetNotFound || len(f.calls) != 0 {
		t.Errorf("absent: %+v", res)
	}
	path := filepath.Join(dir, "tidy-plan-sets", set.PlanID+".json")
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), `"scope":"own"`, `"scope":"other"`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if res := Apply(context.Background(), req(dir, set, human, f)); res.Status != "refused" || res.Code != CodePlanSetInvalid || len(f.calls) != 0 {
		t.Errorf("edited: %+v", res)
	}
	r.StateDirs = nil
	r.PlanID = set.PlanID
	if res := Apply(context.Background(), r); res.Code != CodePlanSetNotFound {
		t.Errorf("no store: %+v", res)
	}
}

func TestApplyRunsSubPlansInTheFixedOrderWithTheirOwnStore(t *testing.T) {
	all := []tidywizard.Candidate{
		own(tidywizard.KindTrackingRef, "a", "t"), own(tidywizard.KindBranch, "a", "b"),
		own(tidywizard.KindWorktree, "b", "w2"), own(tidywizard.KindWorktree, "a", "w1"),
	}
	dir, set := stored(t, all...)
	f := &fakeApplier{}
	res := Apply(context.Background(), req(dir, set, human, f))
	if res.Status != "ok" || len(res.Outcomes) != 4 {
		t.Fatalf("%+v", res)
	}
	var kinds []string
	for _, c := range f.calls {
		kinds = append(kinds, string(c.SubPlan.Kind)+":"+c.SubPlan.Repo)
		if c.SubPlan.StateDir != "/state/plans" || len(c.Items) != 1 {
			t.Errorf("step %+v", c)
		}
	}
	if strings.Join(kinds, ",") != "retire:a,retire:b,branches:a,tracking:a" {
		t.Fatalf("order %v", kinds)
	}
	for _, o := range res.Outcomes {
		if o.Status != StatusApplied {
			t.Errorf("%+v", o)
		}
	}
}

func TestApplyOrdersASetThatWasStoredInAnotherOrder(t *testing.T) {
	_, set := stored(t, own(tidywizard.KindTrackingRef, "a", "t"), own(tidywizard.KindWorktree, "a", "w"), own(tidywizard.KindBranch, "a", "b"))
	dir, forged := storeForged(t, set, func(s *Set) {
		for i, j := 0, len(s.SubPlans)-1; i < j; i, j = i+1, j-1 {
			s.SubPlans[i], s.SubPlans[j] = s.SubPlans[j], s.SubPlans[i]
		}
	})
	f := &fakeApplier{}
	Apply(context.Background(), req(dir, forged, human, f))
	var kinds []string
	for _, c := range f.calls {
		kinds = append(kinds, string(c.SubPlan.Kind))
	}
	if strings.Join(kinds, ",") != "retire,branches,tracking" {
		t.Fatalf("%v", kinds)
	}
}

func TestAPlanWithAuthorizedScopesForNoItemIsInvalidToApply(t *testing.T) {
	_, set := stored(t, own(tidywizard.KindWorktree, "r", "a"))
	c := set.clone()
	c.AuthorizedScopes = []owner.Scope{owner.ScopeOther}
	c.PlanID = c.computeID()
	if err := Store(stateDir(t), c); err == nil {
		t.Fatal("such a set cannot be stored")
	}
	// A record forged straight into the store is refused at load, for any mode.
	dir := stateDir(t)
	raw, _ := json.Marshal(c)
	if err := workspace.WriteTidyPlanSet(dir, c.PlanID, raw); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []owner.Evidence{agent, human} {
		f := &fakeApplier{}
		if res := Apply(context.Background(), req(dir, c, mode, f)); res.Code != CodePlanSetInvalid || len(f.calls) != 0 {
			t.Errorf("%s: %+v", mode.Mode, res)
		}
	}
}

func TestApplyReportsStaleAndFailedSubPlansAndContinues(t *testing.T) {
	w1, w2, b := own(tidywizard.KindWorktree, "r", "w1"), own(tidywizard.KindWorktree, "r", "w2"), own(tidywizard.KindBranch, "r", "b")
	dir, set := stored(t, w1, w2, b)
	var retire []SubPlan
	for _, s := range set.Ordered() {
		if s.Kind == KindRetire {
			retire = append(retire, s)
		}
	}
	f := &fakeApplier{results: map[string]workspace.V3Result{
		retire[0].ID: refusedWith("evidence-changed"),
		retire[1].ID: refusedWith("dirty-worktree"),
	}}
	res := Apply(context.Background(), req(dir, set, human, f))
	if res.Status != "partial" || len(f.calls) != 3 {
		t.Fatalf("%+v calls %d", res, len(f.calls))
	}
	got := []string{}
	for _, o := range res.Outcomes {
		got = append(got, o.Status+"/"+o.Code)
	}
	if strings.Join(got, ",") != "stale/evidence-changed,failed/dirty-worktree,applied/" {
		t.Fatalf("outcomes %v", got)
	}
	all := map[string]workspace.V3Result{}
	for _, sp := range set.SubPlans {
		all[sp.ID] = refusedWith("evidence-changed")
	}
	f = &fakeApplier{results: all}
	if res := Apply(context.Background(), req(dir, set, human, f)); res.Status != "failed" {
		t.Errorf("nothing applied is failed: %+v", res)
	}
}

func TestApplyStopsAndReportsTheRestWhenCancelled(t *testing.T) {
	dir, set := stored(t, own(tidywizard.KindWorktree, "r", "w1"), own(tidywizard.KindBranch, "r", "b"))
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeApplier{cancel: cancel}
	res := Apply(ctx, req(dir, set, human, f))
	if len(f.calls) != 1 || res.Status != "partial" || res.Outcomes[1].Status != StatusSkipped || res.Outcomes[1].Code != "cancelled" {
		t.Fatalf("%+v calls %d", res, len(f.calls))
	}
}

func TestApplyScopeNotAuthorized(t *testing.T) {
	a, f := own(tidywizard.KindWorktree, "r", "a"), foreign(tidywizard.KindWorktree, "r", "f")
	prefix := planIDFor(t, []tidywizard.Candidate{a, f}, ids(a, f), human)
	authorized, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{a, f}, Chosen: ids(a, f), Mode: human, Confirm: ttyWith("codex " + prefix + "\n")}, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		set    Set
		mutate func(*Set)
		mode   owner.Evidence
	}{
		"agent apply of a foreign item without scopes": {authorized, func(s *Set) { s.AuthorizedScopes = []owner.Scope{} }, agent},
		"human apply of a foreign item without scopes": {authorized, func(s *Set) { s.AuthorizedScopes = []owner.Scope{} }, human},
		"agent apply of a set with authorized scopes":  {authorized, func(*Set) {}, agent},
		"scopes that miss the unknown item": {authorized, func(s *Set) {
			s.Items[0].Scope = owner.ScopeUnknown // the own item becomes unknown; only "other" is authorized
			s.Items[0].Owner = ""
		}, human},
	}
	for name, tc := range cases {
		dir, set := storeForged(t, tc.set, tc.mutate)
		app := &fakeApplier{}
		r := req(dir, set, tc.mode, app)
		r.Confirm = ttyWith("codex " + set.PlanID[:6] + "\n")
		res := Apply(context.Background(), r)
		if res.Status != "refused" || res.Code != CodeScopeNotAuthorized || len(app.calls) != 0 {
			t.Errorf("%s: %+v calls %d", name, res, len(app.calls))
		}
	}
}

func TestApplyOfAuthorizedScopesNeedsTheTypedConfirmation(t *testing.T) {
	a, f := own(tidywizard.KindWorktree, "r", "a"), foreign(tidywizard.KindWorktree, "r", "f")
	prefix := planIDFor(t, []tidywizard.Candidate{a, f}, ids(a, f), human)
	set, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{a, f}, Chosen: ids(a, f), Mode: human, Confirm: ttyWith("codex " + prefix + "\n")}, &fakePlanner{})
	if err != nil {
		t.Fatal(err)
	}
	dir := stateDir(t)
	if err := Store(dir, set); err != nil {
		t.Fatal(err)
	}
	for name, open := range map[string]owner.Opener{
		"bare y": ttyWith("y\n"), "wrong word": ttyWith("claude " + set.PlanID[:6] + "\n"),
		"no terminal": noTTY, "nil opener": nil,
	} {
		app := &fakeApplier{}
		r := req(dir, set, human, app) // Authorize is true: --yes or an answered prompt is not enough
		r.Confirm = open
		res := Apply(context.Background(), r)
		if res.Status != "refused" || res.Code != CodeConfirmationRefused || len(app.calls) != 0 {
			t.Errorf("%s: %+v calls %d", name, res, len(app.calls))
		}
	}
	// The typed confirmation is the authorization of a foreign plan: it does not need Authorize.
	app := &fakeApplier{}
	r := req(dir, set, human, app)
	r.Authorize = false
	r.Confirm = ttyWith("codex " + set.PlanID[:6] + "\n")
	if res := Apply(context.Background(), r); res.Status != "ok" || len(app.calls) != 2 {
		t.Fatalf("%+v calls %d", res, len(app.calls))
	}
}

func TestApplyOwnSetNeedsNoTerminalAndAgentMayApplyIt(t *testing.T) {
	dir, set := stored(t, own(tidywizard.KindWorktree, "r", "a"))
	app := &fakeApplier{}
	r := req(dir, set, agent, app)
	r.Confirm = noTTY
	if res := Apply(context.Background(), r); res.Status != "ok" || len(app.calls) != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestApplyLooksTheSetUpInEveryStore(t *testing.T) {
	dir, set := stored(t, own(tidywizard.KindWorktree, "r", "a"))
	app := &fakeApplier{}
	r := req(dir, set, human, app)
	r.StateDirs = []string{stateDir(t), dir}
	if res := Apply(context.Background(), r); res.Status != "ok" {
		t.Fatalf("%+v", res)
	}
}

func TestV3ApplierBuildsTheOptionsOfEachSubPlanKind(t *testing.T) {
	var got []workspace.V3ApplyOptions
	a := V3Applier{ApplyFunc: func(_ context.Context, o workspace.V3ApplyOptions) workspace.V3Result {
		got = append(got, o)
		return workspace.V3Result{Status: "ok"}
	}}
	owned := own(tidywizard.KindWorktree, "r", "wt")
	owned.Task = "task-1"
	legacy := foreign(tidywizard.KindWorktree, "r", "whr1")
	items := func(c tidywizard.Candidate) []Item {
		return []Item{{ID: c.ID, Owner: c.Owner, OwnerSource: c.OwnerSource, Scope: c.Scope, Path: c.Path, Task: c.Task}}
	}
	steps := []Step{
		{SubPlan: SubPlan{ID: "p1", StateDir: "/s/records", Kind: KindRetire, Worktree: owned.Path}, Items: items(owned)},
		{SubPlan: SubPlan{ID: "p2", StateDir: "/s/plans", Kind: KindRetire, Worktree: legacy.Path}, Items: items(legacy)},
		{SubPlan: SubPlan{ID: "p3", StateDir: "/s/plans", Kind: KindBranches}},
		{SubPlan: SubPlan{ID: "p4", StateDir: "/s/plans", Kind: KindTracking}},
	}
	for _, s := range steps {
		if r := a.Apply(context.Background(), s); r.Status != "ok" {
			t.Fatal(r)
		}
	}
	for i, o := range got {
		if !o.Authorize || o.ProtectWorktrees == nil || len(o.ProtectWorktrees) != 0 || o.StateDir != steps[i].SubPlan.StateDir || o.PlanID != steps[i].SubPlan.ID {
			t.Errorf("step %d: %+v", i, o)
		}
	}
	if got[0].Owner != "" || got[0].CompletedWorktree != "" {
		t.Errorf("an owned retirement repeats no declaration: %+v", got[0])
	}
	if got[1].Owner != "codex" || got[1].CompletedWorktree != legacy.Path {
		t.Errorf("a legacy completion repeats owner and path: %+v", got[1])
	}
	if got[2].Online || !got[3].Online {
		t.Errorf("only tracking acknowledges the online reads: %+v %+v", got[2], got[3])
	}
}

func TestApplyOnARealRepositoryDeletesTheMergedBranchAndReplaysAsANoOp(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	if err := os.Mkdir(repo, 0755); err != nil {
		t.Fatal(err)
	}
	git("init", "-q", "-b", "main")
	git("commit", "-q", "--allow-empty", "-m", "base")
	git("switch", "-q", "-c", "merged")
	git("commit", "-q", "--allow-empty", "-m", "work")
	git("switch", "-q", "main")
	git("merge", "-q", "--no-ff", "-m", "merge", "merged")
	planner := V3Planner{Repos: map[string]RepoSpec{"r": {Name: "r", Path: repo, Base: "refs/heads/main"}}, PlanStore: state, PlanFunc: workspace.PlanTidyV3}
	b := own(tidywizard.KindBranch, "r", "merged")
	set, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{b}, Chosen: ids(b), Mode: human}, planner)
	if err != nil {
		t.Fatal(err)
	}
	if err := Store(state, set); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(git("branch", "--list", "merged"), "merged") {
		t.Fatal("fixture: the branch must exist before apply")
	}
	r := ApplyRequest{StateDirs: []string{state}, PlanID: set.PlanID, Mode: human, Authorize: true, Applier: V3Applier{ApplyFunc: workspace.ApplyTidyV3}}
	res := Apply(context.Background(), r)
	if res.Status != "ok" || len(res.Outcomes) != 1 || res.Outcomes[0].Status != StatusApplied {
		t.Fatalf("%+v", res)
	}
	if strings.Contains(git("branch", "--list", "merged"), "merged") {
		t.Fatal("the merged branch must be deleted")
	}
	if _, err := os.Stat(filepath.Join(state, "tidy-v3-operations", set.SubPlans[0].ID+".json")); err != nil {
		t.Errorf("the journal of the sub-plan must exist: %v", err)
	}
	again := Apply(context.Background(), r)
	if again.Status != "ok" || again.Outcomes[0].Status != StatusApplied {
		t.Fatalf("a completed replay is a verified no-op: %+v", again)
	}
}
