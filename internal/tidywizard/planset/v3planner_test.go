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

const (
	repoPath  = "/repos/r"
	planStore = "/state/plans"
)

func okResult(id string, decisions ...string) workspace.V3Result {
	p := &workspace.V3Plan{PlanID: id}
	for _, d := range decisions {
		p.Candidates = append(p.Candidates, workspace.V3Candidate{Decision: d, Reason: "reason-" + d})
	}
	return workspace.V3Result{Status: "ok", Plan: p}
}

type recorder struct {
	opts   []workspace.V3PlanOptions
	result workspace.V3Result
}

func (r *recorder) plan(_ context.Context, o workspace.V3PlanOptions) workspace.V3Result {
	r.opts = append(r.opts, o)
	return r.result
}

func newPlanner(r *recorder, mutate ...func(*V3Planner)) V3Planner {
	p := V3Planner{
		Repos:     map[string]RepoSpec{"r": {Name: "r", Path: repoPath, Base: "refs/heads/main", Remote: "origin", Online: true}},
		PlanStore: planStore,
		PlanFunc:  r.plan,
	}
	for _, m := range mutate {
		m(&p)
	}
	return p
}

func spec(kind SubKind, items ...tidywizard.Candidate) SubSpec {
	s := SubSpec{Repo: "r", Kind: kind, Items: items}
	if kind == KindRetire {
		s.Worktree = items[0].Path
	}
	return s
}

func reasonOf(t *testing.T, err error) string {
	t.Helper()
	var u *Unplannable
	if !errors.As(err, &u) {
		t.Fatalf("err %v is not Unplannable", err)
	}
	return u.Reason
}

func TestOptionsForBranches(t *testing.T) {
	r := &recorder{}
	b1, b2 := own(tidywizard.KindBranch, "r", "feat-b"), own(tidywizard.KindBranch, "r", "feat-a")
	o, err := newPlanner(r).Options(spec(KindBranches, b1, b2))
	if err != nil {
		t.Fatal(err)
	}
	if o.Path != repoPath || o.Base != "refs/heads/main" || o.StateDir != planStore || strings.Join(o.Refs, ",") != "refs/heads/feat-a,refs/heads/feat-b" || o.Task != "" || o.Online {
		t.Fatalf("%+v", o)
	}
}

func TestOptionsForTrackingRefs(t *testing.T) {
	r := &recorder{}
	c := own(tidywizard.KindTrackingRef, "r", "refs/remotes/origin/old")
	c.Branch = "refs/remotes/origin/old"
	o, err := newPlanner(r).Options(spec(KindTracking, c))
	if err != nil {
		t.Fatal(err)
	}
	if o.TrackingRemote != "origin" || !o.Online || len(o.Refs) != 1 || o.Refs[0] != "refs/remotes/origin/old" || o.StateDir != planStore {
		t.Fatalf("%+v", o)
	}
	c.Branch = "refs/remotes/upstream/old"
	if _, err := newPlanner(r).Options(spec(KindTracking, c)); reasonOf(t, err) != ReasonTrackingRefUnmappable {
		t.Errorf("a reference of another remote: %v", err)
	}
	noRemote := newPlanner(r, func(p *V3Planner) {
		p.Repos["r"] = RepoSpec{Name: "r", Path: repoPath, Base: "refs/heads/main", Online: true}
	})
	c.Branch = "refs/remotes/origin/old"
	if _, err := noRemote.Options(spec(KindTracking, c)); reasonOf(t, err) != ReasonTrackingRemoteMissing {
		t.Errorf("no remote: %v", err)
	}
	offline := newPlanner(r, func(p *V3Planner) {
		p.Repos["r"] = RepoSpec{Name: "r", Path: repoPath, Base: "refs/heads/main", Remote: "origin"}
	})
	if _, err := offline.Options(spec(KindTracking, c)); reasonOf(t, err) != ReasonTrackingOnline {
		t.Errorf("offline: %v", err)
	}
}

func TestOptionsForOwnedRetirementUsesTheStoreOfTheRecord(t *testing.T) {
	r := &recorder{}
	w := own(tidywizard.KindWorktree, "r", "wt")
	w.Task = "task-1"
	p := newPlanner(r, func(p *V3Planner) {
		p.RecordStore = func(task string) (string, string) { return "/state/records", "" }
	})
	o, err := p.Options(spec(KindRetire, w))
	if err != nil {
		t.Fatal(err)
	}
	if o.Task != "task-1" || o.Owner != "claude" || !o.RemoveRegistration || o.RemoveFiles != w.Path || o.StateDir != "/state/records" || o.CompletedWorktree != "" || len(o.Refs) != 0 {
		t.Fatalf("%+v", o)
	}
	for _, reason := range []string{"registration-record-missing", "registration-record-ambiguous"} {
		missing := newPlanner(r, func(p *V3Planner) {
			p.RecordStore = func(string) (string, string) { return "", reason }
		})
		if _, err := missing.Options(spec(KindRetire, w)); reasonOf(t, err) != reason {
			t.Errorf("%s: %v", reason, err)
		}
	}
}

func TestOptionsForLegacyCompletionOfAnUnregisteredWorktree(t *testing.T) {
	r := &recorder{}
	f := foreign(tidywizard.KindWorktree, "r", "whr010-a")
	o, err := newPlanner(r).Options(spec(KindRetire, f))
	if err != nil {
		t.Fatal(err)
	}
	if o.CompletedWorktree != f.Path || o.RemoveFiles != f.Path || !o.RemoveRegistration || o.Owner != "codex" || o.StateDir != planStore || o.Task != "" {
		t.Fatalf("%+v", o)
	}
	for name, label := range map[string]string{"unknown owner": "", "author hint": "author:Jane Doe", "path separator": "co/dex"} {
		u := f
		u.Owner = label
		if label == "" {
			u.Scope, u.OwnerSource = owner.ScopeUnknown, owner.SourceUnknown
		}
		if _, err := newPlanner(r).Options(spec(KindRetire, u)); reasonOf(t, err) != ReasonLegacyOwnerUnknown {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLegacyCompletionRefusesAnUnknownScopeEvenWithALabel(t *testing.T) {
	u := foreign(tidywizard.KindWorktree, "r", "whr011-a")
	u.Owner, u.Scope, u.OwnerSource = "unknown", owner.ScopeUnknown, owner.SourceUnknown
	if _, err := newPlanner(&recorder{}).Options(spec(KindRetire, u)); reasonOf(t, err) != ReasonLegacyOwnerUnknown {
		t.Fatal(err)
	}
}

func TestOptionsNeverGuessMissingInputs(t *testing.T) {
	r := &recorder{}
	b := own(tidywizard.KindBranch, "r", "x")
	cases := map[string]struct {
		mutate func(*V3Planner)
		reason string
	}{
		"no plan store": {func(p *V3Planner) { p.PlanStore = "" }, ReasonPlanStoreMissing},
		"no base":       {func(p *V3Planner) { p.Repos["r"] = RepoSpec{Name: "r", Path: repoPath} }, ReasonBaseMissing},
		"no repository": {func(p *V3Planner) { p.Repos = map[string]RepoSpec{} }, ReasonRepositoryUnknown},
		"no path":       {func(p *V3Planner) { p.Repos["r"] = RepoSpec{Name: "r", Base: "refs/heads/main"} }, ReasonRepositoryUnknown},
	}
	for name, tc := range cases {
		if _, err := newPlanner(r, tc.mutate).Options(spec(KindBranches, b)); reasonOf(t, err) != tc.reason {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A retirement of an owned worktree needs no plan store of its own (the record's store is used).
	w := own(tidywizard.KindWorktree, "r", "wt")
	w.Task = "t"
	p := newPlanner(r, func(p *V3Planner) { p.PlanStore = "" })
	p.RecordStore = func(string) (string, string) { return "/state/records", "" }
	if _, err := p.Options(spec(KindRetire, w)); err != nil {
		t.Errorf("owned retirement without plan store: %v", err)
	}
	if len(r.opts) != 0 {
		t.Error("mapping alone must not plan")
	}
}

func TestStaleRegistrationHasNoPlanKind(t *testing.T) {
	r := &recorder{}
	s := own(tidywizard.KindStaleWorktree, "r", "gone")
	_, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{s}, Chosen: ids(s), Mode: human}, newPlanner(r))
	if !errors.Is(err, ErrNotPlannable) || reasonOf(t, err) != ReasonStaleNoPlanKind {
		t.Fatal(err)
	}
	if len(r.opts) != 0 {
		t.Error("nothing is planned for a stale registration")
	}
}

func TestPlanReturnsThePlanIDOnlyWhenEverySelectedItemIsDeletable(t *testing.T) {
	b := own(tidywizard.KindBranch, "r", "x")
	r := &recorder{result: okResult("planid1", "delete")}
	got, err := newPlanner(r).Plan(context.Background(), spec(KindBranches, b))
	if err != nil || got.ID != "planid1" || got.StateDir != planStore || len(r.opts) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	r = &recorder{result: okResult("planid2", "delete", "retain")}
	if _, err := newPlanner(r).Plan(context.Background(), spec(KindBranches, b)); reasonOf(t, err) != "reason-retain" {
		t.Errorf("a retained reference refuses the sub-plan with its reason: %v", err)
	}
	r = &recorder{result: workspace.V3Result{Status: "refused", Diagnostics: []workspace.Diagnostic{{Code: "state-overlap", Message: "x"}}}}
	if _, err := newPlanner(r).Plan(context.Background(), spec(KindBranches, b)); err == nil || !strings.Contains(err.Error(), "state-overlap") {
		t.Errorf("a refused plan: %v", err)
	}
	r = &recorder{result: workspace.V3Result{Status: "ok"}}
	if _, err := newPlanner(r).Plan(context.Background(), spec(KindBranches, b)); err == nil {
		t.Error("an ok result without a plan must fail")
	}
}

func TestPlanRetirementNeedsBothActionsDeletable(t *testing.T) {
	w := own(tidywizard.KindWorktree, "r", "wt")
	w.Task = "t"
	mk := func(a, b string) workspace.V3Result {
		return workspace.V3Result{Status: "ok", Plan: &workspace.V3Plan{PlanID: "ret1", Retirement: &workspace.V3Retirement{Actions: []workspace.V3RetirementAction{{Decision: a, Reason: "r1"}, {Decision: b, Reason: "r2"}}}}}
	}
	withStore := func(r *recorder) V3Planner {
		return newPlanner(r, func(p *V3Planner) { p.RecordStore = func(string) (string, string) { return "/state/records", "" } })
	}
	r := &recorder{result: mk("delete", "delete")}
	if got, err := withStore(r).Plan(context.Background(), spec(KindRetire, w)); err != nil || got.ID != "ret1" || got.StateDir != "/state/records" {
		t.Fatalf("%+v %v", got, err)
	}
	r = &recorder{result: mk("delete", "retain")}
	if _, err := withStore(r).Plan(context.Background(), spec(KindRetire, w)); reasonOf(t, err) != "r2" {
		t.Errorf("retained retirement: %v", err)
	}
}

func TestV3PlannerOnARealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	state := filepath.Join(root, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
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
	p := V3Planner{Repos: map[string]RepoSpec{"r": {Name: "r", Path: repo, Base: "refs/heads/main"}}, PlanStore: state, PlanFunc: workspace.PlanTidyV3}
	b := own(tidywizard.KindBranch, "r", "merged")
	got, err := p.Plan(context.Background(), spec(KindBranches, b))
	if err != nil || got.ID == "" || got.StateDir != state {
		t.Fatalf("plan: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(state, "tidy-v3-plans", got.ID+".json")); err != nil {
		t.Fatalf("the sub-plan is stored as an ordinary v3 plan: %v", err)
	}
}

func TestNewV3PlannerWiring(t *testing.T) {
	stores := []derive.Store{{Path: "/unsafe", Issue: &derive.Issue{Code: "state-dir-unsafe"}}, {Path: "/state/one"}, {Path: "/state/two"}}
	p := NewV3Planner([]RepoSpec{{Name: "r", Path: repoPath, Base: "refs/heads/main"}}, stores)
	if p.PlanStore != "/state/one" {
		t.Errorf("the plan store is the first safe store, got %q", p.PlanStore)
	}
	if p.PlanFunc == nil || p.Repos["r"].Path != repoPath {
		t.Error("production wiring incomplete")
	}
	if path, reason := p.RecordStore("no-such-task"); path != "" || reason != "registration-record-missing" {
		t.Errorf("a record in no store: %q %q", path, reason)
	}
	w := own(tidywizard.KindWorktree, "r", "wt")
	w.Task = "no-such-task"
	if _, err := p.Options(spec(KindRetire, w)); reasonOf(t, err) != "registration-record-missing" {
		t.Errorf("%v", err)
	}
}

func TestPreflightRefusesBeforeTheFirstSubPlanIsPlanned(t *testing.T) {
	r := &recorder{result: okResult("planid1", "delete")}
	p := newPlanner(r, func(p *V3Planner) {
		p.Repos["r"] = RepoSpec{Name: "r", Path: repoPath, Base: "refs/heads/main", Remote: "origin"} // offline
	})
	b := own(tidywizard.KindBranch, "r", "x") // plannable, and its id sorts before the tracking reference
	tr := own(tidywizard.KindTrackingRef, "r", "refs/remotes/origin/old")
	tr.Branch = "refs/remotes/origin/old"
	_, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{b, tr}, Chosen: ids(b, tr), Mode: human}, p)
	if reasonOf(t, err) != ReasonTrackingOnline {
		t.Fatalf("err %v", err)
	}
	if len(r.opts) != 0 {
		t.Fatalf("the branch sub-plan was planned (%d) although the tracking sub-plan cannot be", len(r.opts))
	}
}

func TestLockMismatchRowWithATaskUsesTheLegacyCompletion(t *testing.T) {
	// A locked-stw decision row carries a task but its owner is not from a creation record.
	d := cand(tidywizard.KindWorktree, "r", "locked", tidywizard.VerdictDecision, "codex", owner.SourcePathConvention, owner.ScopeOther)
	d.Task = "task-9"
	called := false
	p := newPlanner(&recorder{}, func(p *V3Planner) {
		p.RecordStore = func(string) (string, string) { called = true; return "/state/records", "" }
	})
	o, err := p.Options(spec(KindRetire, d))
	if err != nil {
		t.Fatal(err)
	}
	if o.CompletedWorktree != d.Path || o.Task != "" || o.Owner != "codex" || called {
		t.Fatalf("%+v record lookup %t", o, called)
	}
}

func TestOptionsRemainingRefusalsHaveStableReasons(t *testing.T) {
	r := &recorder{}
	f := foreign(tidywizard.KindWorktree, "r", "whr1")
	w := own(tidywizard.KindWorktree, "r", "wt")
	w.Task = "t"
	b := own(tidywizard.KindBranch, "r", "x")
	tr := own(tidywizard.KindTrackingRef, "r", "t")
	tr.Branch = "refs/remotes/origin/"
	noStore := func(p *V3Planner) { p.PlanStore = "" }
	cases := map[string]struct {
		planner V3Planner
		spec    SubSpec
		reason  string
	}{
		"legacy completion without a plan store":   {newPlanner(r, noStore), spec(KindRetire, f), ReasonPlanStoreMissing},
		"owned retirement without a record lookup": {newPlanner(r), spec(KindRetire, w), "registration-record-missing"},
		"branch spelled as a full reference":       {newPlanner(r), spec(KindBranches, func() tidywizard.Candidate { c := b; c.Branch = "refs/heads/x"; return c }()), ReasonBranchUnmappable},
		"empty branch name":                        {newPlanner(r), spec(KindBranches, func() tidywizard.Candidate { c := b; c.Branch = ""; return c }()), ReasonBranchUnmappable},
		"tracking ref empty after the remote":      {newPlanner(r), spec(KindTracking, tr), ReasonTrackingRefUnmappable},
		"no item":                                  {newPlanner(r), SubSpec{Repo: "r", Kind: KindBranches}, ReasonPlanShapeInvalid},
		"unknown kind":                             {newPlanner(r), SubSpec{Repo: "r", Kind: "format-disk", Items: []tidywizard.Candidate{b}}, ReasonPlanShapeInvalid},
		"two worktrees in one retirement":          {newPlanner(r), SubSpec{Repo: "r", Kind: KindRetire, Worktree: f.Path, Items: []tidywizard.Candidate{f, f}}, ReasonPlanShapeInvalid},
		"retirement without a worktree":            {newPlanner(r), SubSpec{Repo: "r", Kind: KindRetire, Items: []tidywizard.Candidate{f}}, ReasonPlanShapeInvalid},
		"retirement of another worktree":           {newPlanner(r), SubSpec{Repo: "r", Kind: KindRetire, Worktree: "/w/other", Items: []tidywizard.Candidate{f}}, ReasonPlanShapeInvalid},
	}
	for name, tc := range cases {
		if _, err := tc.planner.Options(tc.spec); reasonOf(t, err) != tc.reason {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPlanRefusesAnOkResultWithoutADeletablePlanOrWithAnErrorStatus(t *testing.T) {
	b := own(tidywizard.KindBranch, "r", "x")
	p := &workspace.V3Plan{PlanID: "planid1", Candidates: []workspace.V3Candidate{{Decision: "delete"}}}
	r := &recorder{result: workspace.V3Result{Status: "refused", Plan: p}}
	if _, err := newPlanner(r).Plan(context.Background(), spec(KindBranches, b)); err == nil {
		t.Fatal("a result whose status is not ok must fail even when it carries a plan")
	}
}
