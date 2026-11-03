// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// missingWorkspaceFixture builds a repository with a stale registration (directory gone) on
// stale-gone at the oldest commit, a live worktree on live at the second commit and a base that
// has advanced past both. Both branches are ancestors of the base.
func missingWorkspaceFixture(t *testing.T) (dir, stale, live, base string) {
	t.Helper()
	dir, _ = fixtureTidyV3Git(t)
	stale = staleRegistration(t, dir, gitTempDir(t), "gone", false)
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "second")
	live = filepath.Join(gitTempDir(t), "live")
	gitFixture(t, dir, "worktree", "add", "-q", "-b", "live", live, "HEAD")
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "third")
	base = gitFixture(t, dir, "symbolic-ref", "HEAD")
	return dir, stale, live, base
}

func candidateNamed(t *testing.T, cs []V3Candidate, name string) V3Candidate {
	t.Helper()
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("candidate %s missing in %+v", name, cs)
	return V3Candidate{}
}

// Issue 386: a registration whose directory is gone does not protect its branch; the plan names
// it as a stale registration and proposes git worktree prune as a separate step.
func TestTidyV3MissingWorkspaceDoesNotProtectBranch(t *testing.T) {
	dir, stale, live, base := missingWorkspaceFixture(t)
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil {
		t.Fatalf("plan failed: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
	}
	gone := candidateNamed(t, r.Plan.Candidates, "refs/heads/stale-gone")
	if gone.Decision != "delete" || gone.Reason != "native-ancestry" || len(gone.WorkspacePaths) != 0 {
		t.Fatalf("the missing workspace still protects the branch: %+v", gone)
	}
	used := candidateNamed(t, r.Plan.Candidates, "refs/heads/live")
	if used.Decision != "retain" || used.Reason != "workspace-used" || !containsString(used.WorkspacePaths, live) {
		t.Fatalf("an existing worktree must keep protecting its branch: %+v", used)
	}
	if len(r.Plan.StaleRegistrations) != 1 {
		t.Fatalf("stale registrations: %+v", r.Plan.StaleRegistrations)
	}
	s := r.Plan.StaleRegistrations[0]
	if s.Path != stale || s.HeadRef != "refs/heads/stale-gone" || s.Reason != "workspace-missing" || s.NextCommand != "git worktree prune" || s.HeadOID != gone.OID {
		t.Fatalf("stale registration record: %+v", s)
	}
	// Plan mode deletes nothing and prunes nothing.
	if out := gitFixture(t, dir, "for-each-ref", "refs/heads/stale-gone"); out == "" {
		t.Fatal("plan deleted the branch")
	}
	if list := gitFixture(t, dir, "worktree", "list", "--porcelain"); !strings.Contains(list, "worktree "+stale) {
		t.Fatalf("plan pruned the registration:\n%s", list)
	}
}

// A branch of a missing workspace is judged by the other rules: unproven work stays retained,
// with the integration reason and not the workspace one, and the path is still listed.
func TestTidyV3MissingWorkspaceBranchWithUnintegratedWorkIsRetainedByOtherRules(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	path := filepath.Join(gitTempDir(t), "ahead")
	gitFixture(t, dir, "worktree", "add", "-q", "-b", "ahead", path, "HEAD")
	if err := os.WriteFile(filepath.Join(path, "new"), []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, path, "add", ".")
	gitFixture(t, path, "commit", "-qm", "unmerged work")
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("plan failed: %+v", r.Diagnostics)
	}
	c := candidateNamed(t, r.Plan.Candidates, "refs/heads/ahead")
	if c.Decision != "retain" || c.Reason == "workspace-used" || len(c.WorkspacePaths) != 0 {
		t.Fatalf("unintegrated work of a missing workspace: %+v", c)
	}
	if len(r.Plan.StaleRegistrations) != 1 || r.Plan.StaleRegistrations[0].Path != path {
		t.Fatalf("stale registrations: %+v", r.Plan.StaleRegistrations)
	}
}

// A locked registration is never prunable and keeps protecting; no stale list without stale registrations.
func TestTidyV3NoStaleRegistrationsKeepsPlanBytes(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || r.Plan.StaleRegistrations != nil {
		t.Fatalf("stale list without stale registrations: %+v", r.Plan)
	}
}

// The branch of a missing workspace is deleted by apply, the registration stays for prune.
func TestTidyV3MissingWorkspaceBranchAppliesAndLeavesRegistration(t *testing.T) {
	dir, stale, _, base := missingWorkspaceFixture(t)
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), Refs: []string{"refs/heads/stale-gone"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || len(r.Plan.Candidates) != 1 || r.Plan.Candidates[0].Decision != "delete" {
		t.Fatalf("plan: %+v %+v", r.Status, r.Diagnostics)
	}
	p := *r.Plan
	applied := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if applied.Status != "ok" {
		t.Fatalf("apply: status=%s diagnostics=%+v", applied.Status, applied.Diagnostics)
	}
	if out := gitFixture(t, dir, "for-each-ref", "refs/heads/stale-gone"); out != "" {
		t.Fatalf("branch remains: %s", out)
	}
	if list := gitFixture(t, dir, "worktree", "list", "--porcelain"); !strings.Contains(list, "worktree "+stale) {
		t.Fatalf("apply touched the registration:\n%s", list)
	}
}

// A plan that lists a registration that is not stale, or that deletes a branch an existing worktree uses, is invalid.
func TestTidyV3MissingWorkspaceStoredPlanValidation(t *testing.T) {
	dir, _, live, base := missingWorkspaceFixture(t)
	o, err := observeTidyV3Git(context.Background(), dir, base)
	if err != nil {
		t.Fatal(err)
	}
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Plan == nil {
		t.Fatal(r.Diagnostics)
	}
	p := *r.Plan
	if !validV3NativeState(o.Native, p, p.Expected) {
		t.Fatal("honest plan rejected")
	}
	forged := p
	forged.StaleRegistrations = append([]V3StaleRegistration{}, p.StaleRegistrations...)
	forged.StaleRegistrations[0].Path = live
	if validV3NativeState(o.Native, forged, p.Expected) {
		t.Error("an existing worktree accepted as a stale registration")
	}
	forged = p
	forged.Candidates = append([]V3Candidate{}, p.Candidates...)
	for i := range forged.Candidates {
		if forged.Candidates[i].Name == "refs/heads/live" {
			forged.Candidates[i].Decision, forged.Candidates[i].Reason, forged.Candidates[i].Proof = "delete", "native-ancestry", "native-ancestry"
		}
	}
	if validV3NativeState(o.Native, forged, p.Expected) {
		t.Error("deleting a branch an existing worktree uses accepted")
	}
}

// The v1/v2 plan (stw tidy plan --base) follows the same rule and names the registration in its limitations.
func TestTidyLegacyMissingWorkspaceDoesNotProtectBranch(t *testing.T) {
	dir, stale, live, _ := missingWorkspaceFixture(t)
	o, err := (gitTidyAdapter{}).Observe(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]TidyCandidate{}
	for _, c := range o.Candidates {
		by[c.Name] = c
	}
	if c := by["refs/heads/stale-gone"]; c.Decision != "delete" || c.Proof != "native-ancestry" || len(c.WorkspacePaths) != 0 {
		t.Fatalf("the missing workspace still protects the branch: %+v", c)
	}
	if c := by["refs/heads/live"]; c.Decision != "retain" || c.Reason != "workspace-used" || !containsString(c.WorkspacePaths, live) {
		t.Fatalf("an existing worktree must keep protecting its branch: %+v", c)
	}
	found := 0
	for _, l := range o.Limitations {
		if strings.HasPrefix(l, "workspace-missing: ") && strings.Contains(l, stale) && strings.Contains(l, "`git worktree prune`") {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("limitations name the stale registration %d times: %v", found, o.Limitations)
	}
}

func TestGitTidyStaleLimitationsAreBounded(t *testing.T) {
	var works []gitTidyWorktree
	for i := 0; i < 20; i++ {
		works = append(works, gitTidyWorktree{path: "/w/" + string(rune('a'+i)), stale: true})
	}
	works = append(works, gitTidyWorktree{path: "/live"})
	got := gitTidyStaleLimitations(works)
	if len(got) != 17 || !strings.Contains(got[16], "4 more registrations") {
		t.Fatalf("%d lines: %v", len(got), got)
	}
	if got := gitTidyStaleLimitations(works[20:]); got != nil {
		t.Fatalf("a live worktree is listed: %v", got)
	}
}
