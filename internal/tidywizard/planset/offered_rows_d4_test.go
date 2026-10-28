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
	fail    map[string]error
	planned int
}

func (p *admitPlanner) Admit(_ context.Context, s SubSpec) error { return p.fail[string(s.Kind)] }
func (p *admitPlanner) Plan(_ context.Context, s SubSpec) (Planned, error) {
	p.planned++
	return Planned{ID: fmt.Sprintf("sub%d", p.planned), StateDir: "/state"}, nil
}
