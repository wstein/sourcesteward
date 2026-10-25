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
				real := filepath.Join(parent, "real")
				if err := os.Mkdir(real, 0o755); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(parent, "link")
				if err := os.Symlink(real, link); err != nil {
					t.Fatal(err)
				}
				staleRegistration(t, dir, link, "gone", false)
			case "inside-state":
				staleRegistration(t, dir, state, "gone", false)
			}
			base := gitFixture(t, dir, "symbolic-ref", "HEAD")
			r := defaultTidyV3RefsCoordinator().plan(context.Background(), V3PlanOptions{Path: dir, Base: base, StateDir: state, ProtectWorktrees: []string{}})
			if r.Status == "ok" || r.Plan != nil {
				t.Fatalf("%s became executable authority", kind)
			}
		})
	}
}
