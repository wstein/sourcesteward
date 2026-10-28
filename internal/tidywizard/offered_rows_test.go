// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"context"
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"os"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// A repository that planning refuses keeps every row that would be offered, with the code of the
// refusal; rows that are already kept keep their own, more specific reason.
func TestBlockedRepositoryKeepsEveryOfferedRow(t *testing.T) {
	live := stwTree("a")
	live.Branch = "feat/live"
	dirty := stwTree("d")
	dirty.Dirty = true
	f := repo([]Worktree{live, dirty}, []Branch{{Name: "feat/b", Proof: ProofAncestor}, {Name: "feat/unmerged", Proof: ProofNone, Ahead: 2}})
	f.TrackingRefs = []TrackingRef{{Name: "refs/remotes/origin/old", Proof: ProofBranchAbsent}}
	f.Blocked = workspace.AdmissionUnknownWorkspacePath
	got := classifyRepo(f, cfg())
	if len(got) != 5 {
		t.Fatalf("%d rows: %+v", len(got), got)
	}
	want := map[string]string{
		"worktree:r:/ws/wt/a": "unknown-workspace-path", "branch:r:feat/b": "unknown-workspace-path",
		"tracking-ref:r:refs/remotes/origin/old": "unknown-workspace-path",
		"worktree:r:/ws/wt/d":                    "dirty", "branch:r:feat/unmerged": "unmerged",
	}
	for _, c := range got {
		if c.Verdict != VerdictRetain || c.Reason != want[c.ID] || len(c.Effects) != 0 {
			t.Errorf("%s: %s %q effects %v, want retain %q without effects", c.ID, c.Verdict, c.Reason, c.Effects, want[c.ID])
		}
	}
	f.Blocked = ""
	offered := 0
	for _, c := range classifyRepo(f, cfg()) {
		if c.Verdict == VerdictSafe {
			offered++
		}
	}
	if offered != 3 {
		t.Errorf("without the block three rows are offered, got %d", offered)
	}
}

// The adapter asks the registrations check once per repository, keeps the repository and adds one
// notice that names the remedy.
func TestAdapterBlocksARepositoryWithOneNotice(t *testing.T) {
	obs := workspace.WorktreeObservation{BaseOID: "base", Worktrees: []workspace.WorktreeEvidence{
		evid("/ws/r", "refs/heads/main"),
		{Path: "/ws/wt/gone", HeadRef: "refs/heads/g", HeadOID: "g1", Locked: true, LockReason: "stw:g", Prunable: true, DirMissing: true},
		evid("/ws/wt/b", "refs/heads/b"),
	}, Branches: []workspace.BranchEvidence{{Name: "refs/heads/feat/x", OID: "x1", Proof: "ancestor"}}}
	obs.Worktrees[0].Primary = true
	a, _ := testAdapter(obs, nil)
	asked := 0
	a.Registrations = func(_ context.Context, regs []workspace.RegistrationFact, state string) (workspace.RegistrationRefusal, bool) {
		asked++
		if len(regs) != 3 || state != "" {
			t.Errorf("regs %+v state %q", regs, state)
		}
		return workspace.RegistrationRefusal{Path: "/ws/wt/gone", Code: workspace.AdmissionUnknownWorkspacePath, Locked: true}, true
	}
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	if asked != 1 {
		t.Errorf("the check ran %d times", asked)
	}
	named := 0
	for _, n := range res.Repositories[0].Notices {
		if strings.Contains(n, "git worktree unlock") && strings.Contains(n, "git worktree prune") && strings.Contains(n, "/ws/wt/gone") {
			named++
		}
	}
	if named != 1 {
		t.Errorf("want one notice with the remedy, got %q", res.Repositories[0].Notices)
	}
	for _, c := range res.Candidates {
		if c.Verdict == VerdictSafe || c.Verdict == VerdictDecision {
			t.Errorf("a blocked repository offers nothing: %+v", c)
		}
	}
	// The overlap with the state directory has its own notice text and no unlock advice.
	a.Registrations = func(context.Context, []workspace.RegistrationFact, string) (workspace.RegistrationRefusal, bool) {
		return workspace.RegistrationRefusal{Path: "/ws/wt/b", Code: workspace.AdmissionStateOverlap}, true
	}
	res = Classify(context.Background(), a, []string{"r"}, cfg())
	if n := strings.Join(res.Repositories[0].Notices, "\n"); !strings.Contains(n, "--state-dir") || strings.Contains(n, "git worktree unlock") {
		t.Errorf("%s", n)
	}
}

// A branch that planning keeps as the base or as workspace-used is kept: at the commit of the base,
// or at the commit of any registration (HEAD of a detached or stale one) and not only by name.
func TestBranchAtTheBaseOrAtARegistrationTipIsKept(t *testing.T) {
	detached := stwTree("d")
	detached.Branch, detached.Tip = "", "t-detached"
	gone := stwTree("gone")
	gone.Branch, gone.Tip, gone.DirMissing, gone.Prunable = "feat/gone", "t-gone", true, true
	f := repo([]Worktree{detached, gone}, []Branch{
		{Name: "at-base", Tip: "base-oid", Proof: ProofAncestor},
		{Name: "at-detached", Tip: "t-detached", Proof: ProofAncestor},
		{Name: "feat/gone", Tip: "other", Proof: ProofAncestor},
		{Name: "at-gone-tip", Tip: "t-gone", Proof: ProofAncestor},
		{Name: "free", Tip: "free-oid", Proof: ProofAncestor},
		{Name: "unknown-tip", Proof: ProofAncestor},
	})
	f.BaseOID = "base-oid"
	got := map[string]Candidate{}
	for _, c := range classifyRepo(f, cfg()) {
		got[c.ID] = c
	}
	for id, reason := range map[string]string{
		"branch:r:at-base": "selected-base", "branch:r:at-detached": "branch-attached-elsewhere", "branch:r:feat/gone": "branch-attached-elsewhere",
		"branch:r:at-gone-tip": "branch-attached-elsewhere", "branch:r:free": "", "branch:r:unknown-tip": "",
	} {
		c := got[id]
		if reason == "" && c.Verdict != VerdictSafe || reason != "" && (c.Verdict != VerdictRetain || c.Reason != reason) {
			t.Errorf("%s: %s %q, want reason %q", id, c.Verdict, c.Reason, reason)
		}
	}
}

// The physical and registration checks are those of the Git plans: a jj repository has no Git
// registrations and no plan kind, so none of them runs for it.
func TestAdapterRunsNoGitAdmissionForAJJRepository(t *testing.T) {
	obs := workspace.WorktreeObservation{Backend: "jj", Worktrees: []workspace.WorktreeEvidence{evid("/ws/r", "refs/heads/main"), evid("/ws/wt/jj", "")}}
	obs.Worktrees[0].Primary = true
	a, _ := testAdapter(obs, nil)
	asked := false
	a.Registrations = func(context.Context, []workspace.RegistrationFact, string) (workspace.RegistrationRefusal, bool) {
		asked = true
		return workspace.RegistrationRefusal{Path: "/ws/wt/jj", Code: workspace.AdmissionUnknownWorkspacePath}, true
	}
	a.DestinationAdmission = func(string) error { asked = true; return os.ErrPermission }
	a.ParentAdmission = func(string) error { asked = true; return os.ErrPermission }
	f, err := a.Facts(context.Background(), "r")
	if err != nil || asked || f.Blocked != "" {
		t.Fatalf("err %v asked %t blocked %q", err, asked, f.Blocked)
	}
	for _, w := range f.Worktrees {
		if w.Admission != "" {
			t.Errorf("%s: %q", w.Path, w.Admission)
		}
	}
}

// D1 for every scope and for several worktrees under one refused parent (the live shape: many
// worktrees directly under /tmp). Each is kept, whoever owns it, and the parent is checked once.
func TestParentAdmissionKeepsEveryWorktreeUnderOneRefusedParentOfEveryScope(t *testing.T) {
	own := evid("/shared/own", "refs/heads/o")
	own.Locked, own.LockReason = true, "stw:own"
	other := evid("/shared/whr-codex", "refs/heads/codex/x") // owner codex by branch convention, scope other
	hinted := evid("/shared/hinted", "refs/heads/h")         // only a commit author: scope unknown
	plain := evid("/shared/whr-2", "refs/heads/w2")
	obs := workspace.WorktreeObservation{Worktrees: []workspace.WorktreeEvidence{evid("/ws/r", "refs/heads/main"), own, other, hinted, plain}}
	obs.Worktrees[0].Primary = true
	a, _ := testAdapter(obs, map[string]derive.TaskLookup{"own": found("own", "werner", "/shared/own")})
	asked := map[string]int{}
	a.ParentAdmission = func(parent string) error { asked[parent]++; return os.ErrPermission }
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	scopes := map[owner.Scope]bool{}
	kept := 0
	for _, c := range res.Candidates {
		if c.Kind != KindWorktree || c.Reason == "primary-checkout" {
			continue
		}
		scopes[c.Scope] = true
		if c.Verdict != VerdictRetain || c.Reason != "retirement-parent-foreign" {
			t.Errorf("%s (scope %s): %s %q, want retain retirement-parent-foreign", c.ID, c.Scope, c.Verdict, c.Reason)
		}
		kept++
	}
	if kept != 4 || !scopes[owner.ScopeOwn] || !scopes[owner.ScopeOther] || !scopes[owner.ScopeUnknown] {
		t.Errorf("fixture: %d rows, scopes %v", kept, scopes)
	}
	if asked["/shared"] != 1 {
		t.Errorf("the parent is checked once per repository, got %d", asked["/shared"])
	}
}

// The destination check refuses a worktree whose own directory fails, whatever its parent.
func TestDestinationAdmissionKeepsTheWorktree(t *testing.T) {
	obs := workspace.WorktreeObservation{Worktrees: []workspace.WorktreeEvidence{evid("/ws/r", "refs/heads/main"), evid("/ws/wt/a", "refs/heads/a")}}
	obs.Worktrees[0].Primary = true
	a, _ := testAdapter(obs, nil)
	a.DestinationAdmission = func(string) error { return os.ErrPermission }
	res := Classify(context.Background(), a, []string{"r"}, cfg())
	for _, c := range res.Candidates {
		if c.Kind == KindWorktree && c.Reason != "primary-checkout" && c.Reason != "retirement-parent-foreign" {
			t.Errorf("%+v", c)
		}
	}
}

// The state directory of the plans reaches the registration check (the overlap rule).
func TestAdapterPassesThePlanStoreToTheRegistrationCheck(t *testing.T) {
	a := NewAdapter(nil, []derive.Store{{Path: "/state/one"}})
	if a.State != "/state/one" {
		t.Fatalf("State %q", a.State)
	}
	obs := workspace.WorktreeObservation{Worktrees: []workspace.WorktreeEvidence{evid("/ws/r", "refs/heads/main")}}
	b, _ := testAdapter(obs, nil)
	b.State = "/state/one"
	got := ""
	b.Registrations = func(_ context.Context, _ []workspace.RegistrationFact, state string) (workspace.RegistrationRefusal, bool) {
		got = state
		return workspace.RegistrationRefusal{}, false
	}
	if _, err := b.Facts(context.Background(), "r"); err != nil || got != "/state/one" {
		t.Errorf("state %q err %v", got, err)
	}
}

// A decision row (a lock whose record does not match) in a refused repository is kept too.
func TestBlockedRepositoryKeepsTheDecisionRow(t *testing.T) {
	w := stwTree("d")
	w.Record, w.TaskOwner = RecordMismatch, ""
	f := repo([]Worktree{w}, nil)
	if c := only(t, f, cfg()); c.Verdict != VerdictDecision {
		t.Fatalf("fixture: %s", c.Verdict)
	}
	f.Blocked = workspace.AdmissionUnknownWorkspacePath
	if c := only(t, f, cfg()); c.Verdict != VerdictRetain || c.Reason != "unknown-workspace-path" || len(c.Effects) != 0 {
		t.Errorf("%+v", c)
	}
}
