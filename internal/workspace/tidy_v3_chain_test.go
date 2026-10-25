// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// chainWork is one repository with n retirable legacy worktrees t1..tn, one worktree that stays
// ("keep"), and one stored retirement plan per retirable worktree, all planned before the first
// apply, as a plan set is (Spec 041 FR-030).
type chainWork struct {
	t      *testing.T
	source string
	state  string
	dests  []string
	plans  []V3Plan
	c      tidyV3RetirementCoordinator
	keep   string
}

func newChainWork(t *testing.T, state string, n int, sharedParent bool) *chainWork {
	t.Helper()
	w := &chainWork{t: t, source: fixtureGit(t), state: state, c: defaultTidyV3RetirementCoordinator()}
	parent := gitTempDir(t)
	for i := 1; i <= n; i++ {
		if !sharedParent {
			parent = gitTempDir(t)
		}
		dest := filepath.Join(parent, fmt.Sprintf("t%d", i))
		gitFixture(t, w.source, "worktree", "add", "-q", "-b", fmt.Sprintf("t%d", i), dest, "HEAD")
		w.dests = append(w.dests, dest)
	}
	w.keep = filepath.Join(gitTempDir(t), "keep")
	gitFixture(t, w.source, "worktree", "add", "-q", "-b", "keep", w.keep, "HEAD")
	// Every retirable worktree has a commit of its own that the base contains, so no branch
	// shares a tip with another worktree.
	for i, dest := range w.dests {
		gitFixture(t, dest, "commit", "--allow-empty", "-qm", fmt.Sprintf("work of t%d", i+1))
		gitFixture(t, w.source, "merge", "-q", "--no-ff", "-m", fmt.Sprintf("merge t%d", i+1), fmt.Sprintf("t%d", i+1))
	}
	gitFixture(t, w.source, "branch", "old", "HEAD")
	gitFixture(t, w.source, "commit", "--allow-empty", "-qm", "later chosen base")
	for _, dest := range w.dests {
		planned := w.c.plan(context.Background(), V3PlanOptions{Path: w.source, Base: "HEAD", StateDir: state, CompletedWorktree: dest, Owner: "operator", RemoveRegistration: true, RemoveFiles: dest, ProtectWorktrees: []string{}})
		if planned.Status != "ok" || planned.Plan == nil || !v3RetirementEligible(*planned.Plan.Retirement) {
			t.Fatalf("plan %s: %s %+v", dest, planned.Status, planned.Diagnostics)
		}
		w.plans = append(w.plans, *planned.Plan)
	}
	return w
}

// others names every plan except index i, the way a plan set names its other sub-plans.
func (w *chainWork) others(i int, extra ...string) []string {
	out := []string{}
	for j, p := range w.plans {
		if j != i {
			out = append(out, p.PlanID)
		}
	}
	return append(out, extra...)
}

func (w *chainWork) apply(i int, siblings []string) V3Result {
	return w.applyWith(w.c, i, siblings)
}

func (w *chainWork) applyWith(c tidyV3RetirementCoordinator, i int, siblings []string) V3Result {
	w.t.Helper()
	p := w.plans[i]
	return c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: w.dests[i], Owner: "operator", ProtectWorktrees: []string{}, SiblingPlans: siblings})
}

func (w *chainWork) mustApply(i int, siblings []string) V3Result {
	w.t.Helper()
	r := w.apply(i, siblings)
	if r.Status != "ok" {
		w.t.Fatalf("apply t%d: %s %+v", i+1, r.Status, r.Diagnostics)
	}
	return r
}

func (w *chainWork) requireRetired(i int) {
	w.t.Helper()
	p := w.plans[i]
	for _, path := range []string{w.dests[i], p.Retirement.Registration.Path} {
		if _, e := os.Lstat(path); !os.IsNotExist(e) {
			w.t.Fatalf("t%d: %s remains: %v", i+1, path, e)
		}
	}
	recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
	if got := gitFixture(w.t, w.source, "rev-parse", recovery.Name); got != recovery.CommitID {
		w.t.Fatalf("t%d: recovery %s missing", i+1, recovery.Name)
	}
	if got := gitFixture(w.t, w.source, "rev-parse", p.Retirement.Target.HeadRef); got != p.Retirement.Target.CommitID {
		w.t.Fatalf("t%d: branch changed", i+1)
	}
}

func (w *chainWork) requireUntouched(i int) {
	w.t.Helper()
	p := w.plans[i]
	for _, path := range []string{w.dests[i], p.Retirement.Registration.Path} {
		if _, e := os.Lstat(path); e != nil {
			w.t.Fatalf("t%d: %s was touched: %v", i+1, path, e)
		}
	}
	recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
	if got := gitFixture(w.t, w.source, "for-each-ref", "--format=%(refname)", recovery.Name); got != "" {
		w.t.Fatalf("t%d: recovery %s exists", i+1, recovery.Name)
	}
}

func (w *chainWork) requireKeep() {
	w.t.Helper()
	if _, e := os.Stat(w.keep); e != nil {
		w.t.Fatal("the worktree that stays was removed")
	}
	if !strings.Contains(gitFixture(w.t, w.source, "worktree", "list", "--porcelain"), w.keep) {
		w.t.Fatal("the worktree that stays lost its registration")
	}
}

func requireEvidenceChanged(t *testing.T, r V3Result) {
	t.Helper()
	if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "evidence-changed" {
		t.Fatalf("want evidence-changed, got %s %+v", r.Status, r.Diagnostics)
	}
}

// Issue 285: three independent retirements of one repository in one plan set all apply.
func TestTidyV3ChainAppliesEveryRetirementOfOnePlanSet(t *testing.T) {
	for _, shared := range []bool{true, false} {
		t.Run(fmt.Sprintf("shared-parent=%t", shared), func(t *testing.T) {
			w := newChainWork(t, privateDir(t), 3, shared)
			for i := range w.plans {
				w.mustApply(i, w.others(i))
			}
			for i := range w.plans {
				w.requireRetired(i)
			}
			w.requireKeep()
			if got := strings.Count(gitFixture(t, w.source, "for-each-ref", "--format=%(refname)", "refs/stw/recovery/"), "\n") + 1; got != 3 {
				t.Fatalf("want 3 recovery references, got %d", got)
			}
			if got := strings.Count(gitFixture(t, w.source, "worktree", "list", "--porcelain"), "worktree "); got != 2 {
				t.Fatalf("want the primary and the kept worktree, got %d", got)
			}
		})
	}
}

// The order in which the rows apply does not matter, and a row that was not applied yet is
// explained by nothing.
func TestTidyV3ChainAppliesRowsInAnyOrder(t *testing.T) {
	w := newChainWork(t, privateDir(t), 3, true)
	w.mustApply(2, w.others(2))
	w.mustApply(0, w.others(0))
	w.requireUntouched(1)
	w.mustApply(1, w.others(1))
	for i := range w.plans {
		w.requireRetired(i)
	}
	w.requireKeep()
}

// Each retirement is followed by its branch leg, which is planned and applied fresh. The leg's
// journaled effects explain the repository for the next row as the retirement's do.
func TestTidyV3ChainAppliesRetirementsWithTheirBranchLegs(t *testing.T) {
	w := newChainWork(t, privateDir(t), 3, true)
	var legs []string
	for i := range w.plans {
		r := w.mustApply(i, w.others(i, legs...))
		leg, ok := TidyV3BranchLeg(w.plans[i])
		if !ok {
			t.Fatal("an attached integrated worktree has a leg")
		}
		lr := ApplyTidyV3BranchLeg(context.Background(), leg, r)
		if lr.Status != "ok" || lr.Plan == nil {
			t.Fatalf("leg t%d: %s %+v", i+1, lr.Status, lr.Diagnostics)
		}
		legs = append(legs, lr.Plan.PlanID)
	}
	for i := range w.plans {
		w.requireRetired(i)
		if got := gitFixture(t, w.source, "for-each-ref", "--format=%(refname)", fmt.Sprintf("refs/heads/t%d", i+1)); got != "" {
			t.Fatalf("branch t%d remains", i+1)
		}
	}
	w.requireKeep()
	// Three retirement and three leg recovery references.
	if got := strings.Count(gitFixture(t, w.source, "for-each-ref", "--format=%(refname)", "refs/stw/recovery/"), "\n") + 1; got != 6 {
		t.Fatalf("want 6 recovery references, got %d", got)
	}
}

// Everything the journals do not explain stays strictly compared.
func TestTidyV3ChainRefusesWhatNoJournalExplains(t *testing.T) {
	cases := map[string]func(w *chainWork){
		"new commit on the base": func(w *chainWork) {
			gitFixture(t, w.source, "commit", "--allow-empty", "-qm", "unrelated")
		},
		"new worktree registered": func(w *chainWork) {
			gitFixture(t, w.source, "worktree", "add", "-q", "-b", "extra", filepath.Join(gitTempDir(t), "extra"), "HEAD")
		},
		"new branch": func(w *chainWork) {
			gitFixture(t, w.source, "branch", "unrelated", "HEAD")
		},
		"moved branch": func(w *chainWork) {
			gitFixture(t, w.source, "branch", "-f", "old", "HEAD~1")
		},
		"target dirty": func(w *chainWork) {
			if e := os.WriteFile(filepath.Join(w.dests[1], "dirty"), []byte("x"), 0600); e != nil {
				t.Fatal(e)
			}
		},
		"target admin file touched": func(w *chainWork) {
			if e := os.WriteFile(filepath.Join(w.plans[1].Retirement.Registration.Path, "extra"), []byte("x"), 0600); e != nil {
				t.Fatal(e)
			}
		},
		"unrelated sibling in the destination parent": func(w *chainWork) {
			if e := os.Mkdir(filepath.Join(filepath.Dir(w.dests[1]), "unrelated"), 0700); e != nil {
				t.Fatal(e)
			}
		},
		"unrelated entry in the registration parent": func(w *chainWork) {
			if e := os.Mkdir(filepath.Join(filepath.Dir(w.plans[1].Retirement.Registration.Path), "unrelated"), 0700); e != nil {
				t.Fatal(e)
			}
		},
		"foreign recovery reference": func(w *chainWork) {
			gitFixture(t, w.source, "update-ref", "refs/stw/recovery/v3/foreign", gitFixture(t, w.source, "rev-parse", "HEAD"))
		},
		"worktree locked": func(w *chainWork) {
			gitFixture(t, w.source, "worktree", "lock", w.dests[1])
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := newChainWork(t, privateDir(t), 3, true)
			w.mustApply(0, w.others(0))
			mutate(w)
			r := w.apply(1, w.others(1))
			if r.Status == "ok" {
				t.Fatalf("an unexplained change was accepted: %+v", r)
			}
			// A changed worktree or admin directory is refused by its own content check
			// before the evidence comparison; every other change is evidence-changed.
			if name != "worktree locked" && name != "target dirty" && name != "target admin file touched" {
				requireEvidenceChanged(t, r)
			}
			w.requireUntouched(1)
			w.requireUntouched(2)
		})
	}
}

// A sibling that stopped halfway left effects of its own, which no complete journal explains.
func TestTidyV3ChainIncompleteSiblingContributesNothing(t *testing.T) {
	w := newChainWork(t, privateDir(t), 3, true)
	failing := w.c
	failing.remove = func(context.Context, string, []string, string, []string, []string) (string, int, error) {
		return "", 1, nil
	}
	r := w.applyWith(failing, 0, w.others(0))
	if r.Status == "ok" {
		t.Fatalf("injected removal failure: %+v", r)
	}
	if j := readLegacyRetirementJournal(t, w.plans[0]); j.Phase == "complete" {
		t.Fatal("the failed sibling must leave an incomplete journal")
	}
	requireEvidenceChanged(t, w.apply(1, w.others(1)))
	w.requireUntouched(1)
}

// A sibling that was never applied (failed before its journal, skipped) changes nothing.
func TestTidyV3ChainSiblingWithoutJournalContributesNothing(t *testing.T) {
	w := newChainWork(t, privateDir(t), 3, true)
	w.mustApply(1, w.others(1))
	w.requireUntouched(0)
	w.mustApply(2, w.others(2))
	w.requireRetired(1)
	w.requireRetired(2)
	w.requireUntouched(0)
}

// A sibling named by a caller must be a plan of this state directory and repository.
func TestTidyV3ChainRejectsForgedSiblings(t *testing.T) {
	w := newChainWork(t, privateDir(t), 2, true)
	other := newChainWork(t, w.state, 2, true)
	other.mustApply(0, nil)
	unknown, e := randomID()
	if e != nil {
		t.Fatal(e)
	}
	cases := map[string][]string{
		"completed plan of another repository": {other.plans[0].PlanID},
		"unknown plan":                         {unknown},
		"the plan itself":                      {w.plans[1].PlanID},
		"malformed":                            {"../escape"},
	}
	for name, ids := range cases {
		t.Run(name, func(t *testing.T) {
			r := w.apply(1, append(w.others(1), ids...))
			if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "invalid-options" {
				t.Fatalf("forged sibling accepted: %s %+v", r.Status, r.Diagnostics)
			}
			w.requireUntouched(1)
		})
	}
}

// A sibling applied before this plan was stored is already part of its baseline.
func TestTidyV3ChainRefusesSiblingAlreadyInTheBaseline(t *testing.T) {
	w := newChainWork(t, privateDir(t), 2, true)
	w.mustApply(0, nil)
	again := w.c.plan(context.Background(), V3PlanOptions{Path: w.source, Base: "HEAD", StateDir: w.state, CompletedWorktree: w.dests[1], Owner: "operator", RemoveRegistration: true, RemoveFiles: w.dests[1], ProtectWorktrees: []string{}})
	if again.Plan == nil {
		t.Fatal(again.Diagnostics)
	}
	w.plans[1] = *again.Plan
	requireEvidenceChanged(t, w.apply(1, []string{w.plans[0].PlanID}))
	w.requireUntouched(1)
	// The plan made after the sibling needs no sibling.
	w.mustApply(1, nil)
	w.requireRetired(1)
}

// A completed set replays as a verified no-op, row by row.
func TestTidyV3ChainReplayOfACompletedSetIsANoOp(t *testing.T) {
	w := newChainWork(t, privateDir(t), 3, true)
	for i := range w.plans {
		w.mustApply(i, w.others(i))
	}
	before := map[int]layoutProtectedFile{}
	for i, p := range w.plans {
		before[i] = layoutProtectedFileAt(t, p.StateDir+"/tidy-v3-operations/"+p.PlanID+".json")
	}
	calls := 0
	replay := w.c
	replay.remove = func(context.Context, string, []string, string, []string, []string) (string, int, error) {
		calls++
		return "", 1, nil
	}
	for i := range w.plans {
		if r := w.applyWith(replay, i, w.others(i)); r.Status != "ok" {
			t.Fatalf("replay t%d: %s %+v", i+1, r.Status, r.Diagnostics)
		}
		after := layoutProtectedFileAt(t, w.plans[i].StateDir+"/tidy-v3-operations/"+w.plans[i].PlanID+".json")
		if !reflect.DeepEqual(after, before[i]) {
			t.Fatalf("replay of t%d changed its journal", i+1)
		}
	}
	if calls != 0 {
		t.Fatal("a replay removed something")
	}
}

// Two repositories in one state directory, applied row by row interleaved.
func TestTidyV3ChainInterleavedRepositoriesStayIndependent(t *testing.T) {
	state := privateDir(t)
	a := newChainWork(t, state, 2, true)
	b := newChainWork(t, state, 2, true)
	a.mustApply(0, a.others(0))
	b.mustApply(0, b.others(0))
	a.mustApply(1, a.others(1))
	b.mustApply(1, b.others(1))
	for _, w := range []*chainWork{a, b} {
		w.requireRetired(0)
		w.requireRetired(1)
		w.requireKeep()
	}
	// A plan of the other repository is never a sibling.
	c := newChainWork(t, state, 2, true)
	d := newChainWork(t, state, 1, true)
	c.mustApply(0, c.others(0))
	r := c.apply(1, append(c.others(1), d.plans[0].PlanID))
	if r.Status == "ok" || r.Diagnostics[0].Code != "invalid-options" {
		t.Fatalf("cross-repository sibling accepted: %+v", r)
	}
}

// A reference plan of the same repository follows the same rule, in either order.
func TestTidyV3ChainMixedRetirementAndReferencePlans(t *testing.T) {
	for _, retireFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("retire-first=%t", retireFirst), func(t *testing.T) {
			w := newChainWork(t, privateDir(t), 2, true)
			planned := w.c.refs.plan(context.Background(), V3PlanOptions{Path: w.source, Base: "HEAD", StateDir: w.state, Refs: []string{"refs/heads/old"}, ProtectWorktrees: []string{}})
			if planned.Status != "ok" || planned.Plan == nil || len(planned.Plan.Candidates) != 1 || planned.Plan.Candidates[0].Decision != "delete" {
				t.Fatalf("reference plan: %+v %+v", planned.Diagnostics, planned.Plan)
			}
			ref := planned.Plan.PlanID
			applyRef := func(siblings []string) V3Result {
				return defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: w.state, PlanID: ref, Authorize: true, ProtectWorktrees: []string{}, SiblingPlans: siblings})
			}
			if retireFirst {
				w.mustApply(0, append(w.others(0), ref))
				w.mustApply(1, append(w.others(1), ref))
				if r := applyRef(w.others(-1)); r.Status != "ok" {
					t.Fatalf("reference apply: %s %+v", r.Status, r.Diagnostics)
				}
			} else {
				if r := applyRef(w.others(-1)); r.Status != "ok" {
					t.Fatalf("reference apply: %s %+v", r.Status, r.Diagnostics)
				}
				w.mustApply(0, append(w.others(0), ref))
				w.mustApply(1, append(w.others(1), ref))
			}
			w.requireRetired(0)
			w.requireRetired(1)
			if got := gitFixture(t, w.source, "for-each-ref", "--format=%(refname)", "refs/heads/old"); got != "" {
				t.Fatal("the reference plan did not delete its branch")
			}
		})
	}
}
