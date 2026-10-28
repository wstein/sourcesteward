// SPDX-License-Identifier: EUPL-1.2

package planset

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func asRefused(err error, target **Refused) bool { return errors.As(err, target) }

// D4: the sub-plans are all checked before any is planned; a refusal stores nothing, carries
// the error prefix once and names the group and the row.
func TestD4RefusedSubPlanStoresNothingAndNamesTheGroup(t *testing.T) {
	f := newOfferedFixture(t)
	f.mergedBranch("plain")
	shared := sharedParent(t)
	f.plainWorktree(shared, "under-tmp")
	res := f.classify()
	// The analysis already keeps the worktree under /tmp; the request names it anyway, as a
	// stale or hand-made request would.
	wt := tidywizard.Candidate{ID: "worktree:r:" + shared, Repo: "r", Kind: tidywizard.KindWorktree, Verdict: tidywizard.VerdictSafe, Owner: "codex", OwnerSource: owner.SourcePathConvention, Scope: owner.ScopeOther, Path: shared, Branch: "under-tmp", Effects: []string{"remove-registration", "remove-files"}}
	br := rowOf(t, res, "branch:r:plain")
	_, _, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{wt, br}, Chosen: []string{wt.ID, br.ID}, Mode: human, Confirm: typingOpener()}, f.planner())
	if err == nil {
		t.Fatal("a worktree under a foreign parent must be refused")
	}
	if n := strings.Count(err.Error(), "planning retire sub-plan of r"); n != 1 {
		t.Errorf("the error prefix appears %d times, want once: %v", n, err)
	}
	var refused *Refused
	if !asRefused(err, &refused) || len(refused.Failures) != 1 || refused.Failures[0].Items[0] != wt.ID {
		t.Errorf("the error must name the refused group and its row: %#v", err)
	}
	plans, _ := filepath.Glob(filepath.Join(f.state, "tidy-v3-plans", "*"))
	if len(plans) != 0 {
		t.Errorf("a refused set must store no sub-plan, found %v", plans)
	}
}

func TestD4EveryFailingSubPlanIsReportedNotOnlyTheFirst(t *testing.T) {
	a := own(tidywizard.KindBranch, "r", "a")
	b := foreign(tidywizard.KindWorktree, "r", "b")
	planner := &admitPlanner{fail: map[string]error{"branches": unplannable("plan-base-missing", "no base"), "retire": unplannable("legacy-owner-unknown", "no owner")}}
	_, _, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{a, b}, Chosen: ids(a, b), Mode: human, Confirm: ttyWith("codex abcdef\n")}, planner)
	var refused *Refused
	if !asRefused(err, &refused) || len(refused.Failures) != 2 {
		t.Fatalf("want both failing sub-plans reported, got %v", err)
	}
	if planner.planned != 0 {
		t.Errorf("%d sub-plans were planned although the check failed", planner.planned)
	}
}

// admitPlanner is a Planner that fails the admission of the kinds named in fail.
type admitPlanner struct {
	fail      map[string]error
	planned   int
	planFail  error
	admitted  int
	preflight error
}

func (p *admitPlanner) Preflight(SubSpec) error { return p.preflight }

func (p *admitPlanner) Admit(_ context.Context, s SubSpec) error {
	p.admitted++
	return p.fail[string(s.Kind)]
}
func (p *admitPlanner) Plan(_ context.Context, s SubSpec) (Planned, error) {
	p.planned++
	if p.planFail != nil {
		return Planned{}, p.planFail
	}
	return Planned{ID: fmt.Sprintf("sub%d", p.planned), StateDir: "/state"}, nil
}

// V3Planner.Admit makes the plan with DryRun and judges it as Plan does; Plan itself is no dry run.
func TestV3PlannerAdmitIsADryRunWithTheSameJudgement(t *testing.T) {
	b := own(tidywizard.KindBranch, "r", "x")
	r := &recorder{result: okResult("planid1", "delete")}
	p := newPlanner(r)
	if err := p.Admit(context.Background(), spec(KindBranches, b)); err != nil {
		t.Fatal(err)
	}
	if len(r.opts) != 1 || !r.opts[0].DryRun {
		t.Fatalf("Admit must plan with DryRun: %+v", r.opts)
	}
	if _, err := p.Plan(context.Background(), spec(KindBranches, b)); err != nil || r.opts[1].DryRun {
		t.Fatalf("Plan must not be a dry run: %v %+v", err, r.opts)
	}
	// A plan that retains the item is refused by Admit as by Plan.
	r.result = okResult("planid2", "retain")
	if err := p.Admit(context.Background(), spec(KindBranches, b)); reasonOf(t, err) != "reason-retain" {
		t.Errorf("%v", err)
	}
	// A failed plan is the plain "code: message" without the prefix, which the caller adds once.
	r.result = workspace.V3Result{Status: "refused", Diagnostics: []workspace.Diagnostic{{Code: "unknown-workspace-path", Message: "boom"}}}
	if err := p.Admit(context.Background(), spec(KindBranches, b)); err == nil || err.Error() != "unknown-workspace-path: boom" {
		t.Errorf("%v", err)
	}
}

// Admit plans and stores nothing, asks no typed word and reports every failing sub-plan.
func TestAdmitPlansNothingAndAsksNoWord(t *testing.T) {
	a := own(tidywizard.KindBranch, "r", "a")
	f := foreign(tidywizard.KindWorktree, "r", "f")
	ok := &admitPlanner{}
	if err := Admit(context.Background(), Request{Candidates: []tidywizard.Candidate{a, f}, Chosen: ids(a, f), Mode: human}, ok); err != nil || ok.planned != 0 {
		t.Fatalf("err %v planned %d", err, ok.planned)
	}
	bad := &admitPlanner{fail: map[string]error{"retire": unplannable("legacy-owner-unknown", "no owner")}}
	err := Admit(context.Background(), Request{Candidates: []tidywizard.Candidate{a, f}, Chosen: ids(a, f), Mode: human}, bad)
	var refused *Refused
	if !errors.As(err, &refused) || len(refused.Failures) != 1 || refused.Failures[0].Kind != KindRetire || !errors.Is(err, ErrNotPlannable) {
		t.Fatalf("%v", err)
	}
	if !strings.Contains(err.Error(), "planning retire sub-plan of r: item cannot be planned: legacy-owner-unknown") {
		t.Errorf("%v", err)
	}
}

// OPEN QUESTION for the owner (see the pull request): a decision row (locked-stw: an stw lock whose
// creation record does not match) is offered by number, but the legacy completion refuses a locked
// registration (active-workspace) and an owner that is only a commit author. Its selection is
// therefore refused. This test pins the present behavior: the refusal is clean (one *Refused that
// names the row) and nothing is stored; the row itself stays offered until the owner decides.
func TestDecisionRowIsRefusedCleanlyAndStoresNothing(t *testing.T) {
	f := newOfferedFixture(t)
	f.mergedBranch("dec")
	dest := filepath.Join(f.root, "wts", "dec")
	planned := workspace.PlanCreate(context.Background(), workspace.PlanOptions{Path: f.repo, Base: "refs/heads/main", Destination: dest, Task: "dec", Owner: "claude", StateDir: f.state, CallerDir: f.repo})
	if planned.Plan == nil {
		t.Fatalf("%+v", planned)
	}
	if r := workspace.Apply(context.Background(), workspace.ApplyOptions{StateDir: f.state, PlanID: planned.Plan.PlanID, Authorize: true}); r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	f.git(dest, "switch", "-q", "dec")
	moved := filepath.Join(f.root, "wts", "moved")
	f.git(f.repo, "worktree", "move", "-f", "-f", dest, moved) // the creation record names the old place
	res := f.classify()
	row := rowOf(t, res, "worktree:r:"+moved)
	if row.Verdict != tidywizard.VerdictDecision || row.Reason != "locked-stw" {
		t.Fatalf("fixture: %s %q", row.Verdict, row.Reason)
	}
	_, _, err := Build(context.Background(), Request{Candidates: res.Candidates, Chosen: []string{row.ID}, Mode: human, DecisionConfirmed: map[string]bool{row.ID: true}, Confirm: typingOpener()}, f.planner())
	var refused *Refused
	if !asRefused(err, &refused) || len(refused.Failures) != 1 || refused.Failures[0].Items[0] != row.ID {
		t.Fatalf("want one clean refusal naming the row, got %v", err)
	}
	if plans, _ := filepath.Glob(filepath.Join(f.state, "tidy-v3-plans", "*")); len(plans) != 0 {
		t.Errorf("a refused decision row must store nothing: %v", plans)
	}
}

// A mapping refusal is final: the admission of that sub-plan is not asked on top of it.
func TestMappingRefusalIsNotOverwrittenByTheAdmission(t *testing.T) {
	a := own(tidywizard.KindBranch, "r", "a")
	p := &admitPlanner{preflight: unplannable("plan-base-missing", "no base")}
	err := Admit(context.Background(), Request{Candidates: []tidywizard.Candidate{a}, Chosen: ids(a), Mode: human}, p)
	var refused *Refused
	if !errors.As(err, &refused) || p.admitted != 0 || !strings.Contains(err.Error(), "plan-base-missing") {
		t.Fatalf("admitted %d: %v", p.admitted, err)
	}
}

// A sub-plan that passed the admission but fails when planned is the same typed refusal.
func TestPlanFailureAfterAdmissionIsARefusal(t *testing.T) {
	a := own(tidywizard.KindBranch, "r", "a")
	p := &admitPlanner{planFail: errors.New("boom")}
	_, _, err := Build(context.Background(), Request{Candidates: []tidywizard.Candidate{a}, Chosen: ids(a), Mode: human}, p)
	var refused *Refused
	if !errors.As(err, &refused) || len(refused.Failures) != 1 || refused.Failures[0].Items[0] != a.ID || !strings.Contains(err.Error(), "planning branches sub-plan of r: boom") {
		t.Fatalf("%v", err)
	}
}
