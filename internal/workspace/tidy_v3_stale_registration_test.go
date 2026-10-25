// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// staleRegistration adds a linked worktree and removes its directory without pruning, which is
// what a crashed or manually cleaned workspace leaves behind (issue 284).
func staleRegistration(t *testing.T, source, parent, name string, lock bool) string {
	t.Helper()
	path := filepath.Join(parent, name)
	gitFixture(t, source, "worktree", "add", "-q", "-b", "stale-"+name, path, "HEAD")
	if lock {
		gitFixture(t, source, "worktree", "lock", path)
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTidyV3StaleRegistrationDoesNotBlockRetirementPlanOrApply(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	stale := staleRegistration(t, o.Path, gitTempDir(t), "gone", false)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("stale registration blocked the plan: status=%s diagnostics=%+v", planned.Status, planned.Diagnostics)
	}
	p := *planned.Plan
	if !containsString(p.Retirement.RegisteredRoots, stale) {
		t.Fatalf("stale registration missing from the registered roots: %v", p.Retirement.RegisteredRoots)
	}
	applied := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if applied.Status != "ok" {
		t.Fatalf("stale registration blocked the apply: status=%s diagnostics=%+v", applied.Status, applied.Diagnostics)
	}
	if _, err := os.Lstat(o.CompletedWorktree); !os.IsNotExist(err) {
		t.Fatalf("the proven item was not retired: %v", err)
	}
	// The stale registration stays untouched: still registered, still prunable, not pruned.
	list := gitFixture(t, o.Path, "worktree", "list", "--porcelain")
	if !strings.Contains(list, "worktree "+stale) || !strings.Contains(list, "prunable gitdir file points to non-existent location") {
		t.Fatalf("stale registration was changed:\n%s", list)
	}
}

func TestTidyV3StaleRegistrationDoesNotBlockReferencePlan(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	stale := staleRegistration(t, dir, gitTempDir(t), "gone", false)
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	c := defaultTidyV3RefsCoordinator()
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("stale registration blocked the reference plan: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
	}
	for _, v := range r.Plan.Candidates {
		if v.Name == "refs/heads/stale-gone" && (v.Decision != "retain" || !containsString(v.WorkspacePaths, stale)) {
			t.Fatalf("branch of the stale registration is not retained: %+v", v)
		}
	}
}

// Everything that is not a plainly missing leaf below a canonical parent keeps the old refusal.
func TestTidyV3StaleRegistrationRefusalsStayUnchanged(t *testing.T) {
	for _, kind := range []string{"locked-missing", "replaced-by-symlink", "missing-parent", "symlinked-parent", "inside-state"} {
		t.Run(kind, func(t *testing.T) {
			dir, _ := fixtureTidyV3Git(t)
			state := privateDir(t)
			parent := gitTempDir(t)
			switch kind {
			case "locked-missing":
				staleRegistration(t, dir, parent, "gone", true)
			case "replaced-by-symlink":
				path := staleRegistration(t, dir, parent, "gone", false)
				if err := os.Symlink(dir, path); err != nil {
					t.Fatal(err)
				}
			case "missing-parent":
				staleRegistration(t, dir, filepath.Join(parent, "sub"), "gone", false)
				if err := os.RemoveAll(filepath.Join(parent, "sub")); err != nil {
					t.Fatal(err)
				}
			case "symlinked-parent":
				sub := filepath.Join(parent, "sub")
				staleRegistration(t, dir, sub, "gone", false)
				if err := os.RemoveAll(sub); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(gitTempDir(t), sub); err != nil {
					t.Fatal(err)
				}
			case "inside-state":
				staleRegistration(t, dir, state, "gone", false)
			}
			base := gitFixture(t, dir, "symbolic-ref", "HEAD")
			r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: state, ProtectWorktrees: []string{}})
			if r.Status == "ok" || r.Plan != nil {
				t.Fatalf("%s became executable authority", kind)
			}
			want := "unknown-workspace-path"
			if kind == "inside-state" {
				want = "state-overlap"
			}
			if len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != want {
				t.Fatalf("%s: got %+v, want code %s", kind, r.Diagnostics, want)
			}
		})
	}
}

// An aliased parent can arise through Git too (register below a directory, then replace the
// directory with a symlink); the rule must refuse it, and anything else that is not a plain ENOENT.
func TestAbsentBelowCanonicalParent(t *testing.T) {
	root := gitTempDir(t)
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "file"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		filepath.Join(real, "gone"):                   true,
		filepath.Join(root, "link", "gone"):           false, // aliased parent
		filepath.Join(root, "nodir", "gone"):          false, // parent missing
		filepath.Join(real, "file", "gone"):           false, // parent is not a directory
		filepath.Join(real, strings.Repeat("a", 300)): false, // ENAMETOOLONG is not absence
		real:                        false, // present
		filepath.Join(root, "link"): false, // present symlink
		filepath.Join(real, "file"): false, // present file
	} {
		if got := absentBelowCanonicalParent(path); got != want {
			t.Errorf("%s: got %v want %v", path, got, want)
		}
	}
}

// The stale registration is part of the bound native evidence: one that appears, or goes
// away, after planning is a change and refuses the apply without touching the target.
func TestTidyV3StaleRegistrationChangingAfterPlanningRefusesApply(t *testing.T) {
	for _, appears := range []bool{true, false} {
		o, _ := tidyV3LegacyFixture(t, false)
		o.StateDir = privateDir(t)
		if !appears {
			staleRegistration(t, o.Path, gitTempDir(t), "early", false)
		}
		c := defaultTidyV3RetirementCoordinator()
		planned := c.plan(context.Background(), o)
		if planned.Plan == nil {
			t.Fatal(planned.Diagnostics)
		}
		p := *planned.Plan
		if appears {
			staleRegistration(t, o.Path, gitTempDir(t), "late", false)
		} else {
			gitFixture(t, o.Path, "worktree", "prune")
		}
		applied := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
		if applied.Status == "ok" {
			t.Fatalf("appears=%v: the registration change was not detected", appears)
		}
		if len(applied.Diagnostics) == 0 || applied.Diagnostics[0].Code != "evidence-changed" {
			t.Fatalf("appears=%v: wrong refusal: %+v", appears, applied.Diagnostics)
		}
		if _, err := os.Lstat(o.CompletedWorktree); err != nil {
			t.Fatalf("appears=%v: target changed despite the refusal: %v", appears, err)
		}
	}
}

func TestTidyV3StaleRegistrationDoesNotBlockReferenceApply(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	gitFixture(t, dir, "branch", "integrated", "HEAD")
	gitFixture(t, dir, "commit", "--allow-empty", "-qm", "advance source")
	stale := staleRegistration(t, dir, gitTempDir(t), "gone", false)
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: privateDir(t), Refs: []string{"refs/heads/integrated"}, ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || len(r.Plan.Candidates) != 1 || r.Plan.Candidates[0].Decision != "delete" {
		t.Fatalf("plan failed: %+v", r)
	}
	p := *r.Plan
	applied := defaultTidyV3RefsApplyCoordinator().apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if applied.Status != "ok" {
		t.Fatalf("stale registration blocked the reference apply: status=%s diagnostics=%+v", applied.Status, applied.Diagnostics)
	}
	if out := gitFixture(t, dir, "for-each-ref", "refs/heads/integrated"); out != "" {
		t.Fatalf("selected branch remains: %s", out)
	}
	if list := gitFixture(t, dir, "worktree", "list", "--porcelain"); !strings.Contains(list, "worktree "+stale) || !strings.Contains(list, "prunable") {
		t.Fatalf("stale registration was changed:\n%s", list)
	}
}

// Git never reports a locked registration as prunable; a record that says both is not accepted
// as stale evidence either, and an absent root keeps the state-directory check on its own path.
func TestQualifyTidyV3RegistrationsRules(t *testing.T) {
	parent := gitTempDir(t)
	state := privateDir(t)
	missing := filepath.Join(parent, "gone")
	reg := func(prunable, locked, bare bool) V3NativeState {
		return V3NativeState{Registrations: []V3Registration{{Path: missing, Prunable: prunable, Locked: locked, Bare: bare}}}
	}
	ctx := context.Background()
	if err := qualifyTidyV3Registrations(ctx, reg(true, false, false), nil, state); err != nil {
		t.Fatalf("plain stale registration refused: %v", err)
	}
	for name, native := range map[string]V3NativeState{"not-prunable": reg(false, false, false), "locked": reg(true, true, false), "bare": reg(true, false, true)} {
		if err := qualifyTidyV3Registrations(ctx, native, nil, state); err == nil {
			t.Errorf("%s: missing root accepted", name)
		}
	}
	// A declared extra root is never relaxed.
	if err := qualifyTidyV3Registrations(ctx, V3NativeState{}, []string{missing}, state); err == nil {
		t.Error("missing protected root accepted")
	}
	// The state directory above or at the missing path is an overlap, not unknown.
	if err := qualifyTidyV3Registrations(ctx, reg(true, false, false), nil, parent); err == nil || tidyErrorCode(err) != "state-overlap" {
		t.Errorf("state directory containing the stale root: %v", err)
	}
}

func TestTidyV3StaleRegistrationDoesNotBlockTrackingPlan(t *testing.T) {
	o, c, _, opens := fixtureTidyV3TrackingPlanner(t)
	dir := o.Expected.Identity.WorkspaceRoot
	staleRegistration(t, dir, gitTempDir(t), "gone", false)
	r := c.plan(context.Background(), V3PlanOptions{Path: dir, Base: "HEAD", Online: true, TrackingRemote: "origin", StateDir: privateDir(t), ProtectWorktrees: []string{}})
	if r.Status != "ok" || r.Plan == nil || validateV3Result(r) != nil || *opens != 1 {
		t.Fatalf("stale registration blocked the tracking plan: status=%s diagnostics=%+v opens=%d", r.Status, r.Diagnostics, *opens)
	}
}

// The structural validator keeps rejecting bare and prunable-plus-locked records, and accepts a
// plain prunable one.
func TestValidV3NativeStateStaleRegistrationRules(t *testing.T) {
	dir, _ := fixtureTidyV3Git(t)
	stale := staleRegistration(t, dir, gitTempDir(t), "gone", false)
	base := gitFixture(t, dir, "symbolic-ref", "HEAD")
	o, err := observeTidyV3Git(context.Background(), dir, base)
	if err != nil {
		t.Fatal(err)
	}
	check := func(edit func(*V3Registration)) bool {
		n := o.Native
		n.Registrations = append([]V3Registration{}, o.Native.Registrations...)
		found := false
		for i := range n.Registrations {
			if n.Registrations[i].Path == stale {
				edit(&n.Registrations[i])
				found = true
			}
		}
		if !found {
			t.Fatal("stale registration not observed")
		}
		exp := v3SnapshotWithState(o.Expected, n)
		return validV3NativeState(n, V3Plan{SourcePath: dir, Expected: exp}, exp)
	}
	if !check(func(r *V3Registration) {
		if !r.Prunable {
			t.Fatal("not observed as prunable")
		}
	}) {
		t.Fatal("plain prunable registration rejected")
	}
	if check(func(r *V3Registration) { r.Locked = true; r.LockReason = "x" }) {
		t.Error("prunable plus locked accepted")
	}
	if check(func(r *V3Registration) { r.Bare = true }) {
		t.Error("bare accepted")
	}
}
