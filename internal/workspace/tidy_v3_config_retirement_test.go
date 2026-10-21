// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// FR-021: only an initialized submodule (a submodule url or active entry, or
// populated modules) retains submodule-present; submodule.recurse alone does not.
func TestTidyV3RetirementRetainsSubmodulePresent(t *testing.T) {
	for _, c := range []struct {
		name   string
		set    func(t *testing.T, o V3PlanOptions)
		retain bool
	}{
		{"none", func(*testing.T, V3PlanOptions) {}, false},
		{"recurse-only", func(t *testing.T, o V3PlanOptions) { gitFixture(t, o.Path, "config", "submodule.recurse", "true") }, false},
		{"url", func(t *testing.T, o V3PlanOptions) {
			gitFixture(t, o.Path, "config", "submodule.member.url", "https://example.invalid/member.git")
		}, true},
		{"active", func(t *testing.T, o V3PlanOptions) {
			gitFixture(t, o.Path, "config", "submodule.member.active", "true")
		}, true},
		{"modules-directory", func(t *testing.T, o V3PlanOptions) {
			if err := os.MkdirAll(filepath.Join(o.Path, ".git", "modules", "member"), 0700); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			o, _ := tidyV3LegacyFixture(t, false)
			o.StateDir = privateDir(t)
			c.set(t, o)
			r := defaultTidyV3RetirementCoordinator().plan(context.Background(), o)
			if r.Status != "ok" || r.Plan == nil || r.Plan.Retirement == nil {
				t.Fatalf("plan refused %+v", r.Diagnostics)
			}
			retirement := r.Plan.Retirement
			if c.retain {
				if v3RetirementEligible(*retirement) || retirement.Integration.Decision != "retain" || retirement.Integration.Reason != "submodule-present" || retirement.Actions[0].Reason != "submodule-present" {
					t.Fatalf("submodule-present target not retained: %+v", retirement.Actions)
				}
			} else if !v3RetirementEligible(*retirement) {
				t.Fatalf("target without an initialized submodule is not eligible: %+v", retirement.Actions)
			}
			if err := validateV3Plan(*r.Plan, r.Plan.StateDir, r.Plan.PlanID); err != nil {
				t.Fatalf("plan does not validate: %v", err)
			}
		})
	}
}

// A retirement planned under core.hooksPath applies: stw pins hooks off for its
// own commands, so a reference-transaction hook never runs and the worktree is
// retired with its recovery reference.
func TestTidyV3RetirementApplyPinsHooksOffUnderHooksPath(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	hooks := gitTempDir(t)
	marker := filepath.Join(gitTempDir(t), "marker")
	hook := "#!/bin/sh\necho \"hook $*\" >> '" + marker + "'\nexit 1\n"
	if err := os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte(hook), 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, o.Path, "config", "core.hooksPath", hooks)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Status != "ok" || planned.Plan == nil || !v3RetirementEligible(*planned.Plan.Retirement) {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: o.StateDir, PlanID: planned.Plan.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if r.Status != "ok" {
		t.Fatalf("apply refused: %+v", r.Diagnostics)
	}
	if _, err := os.Stat(o.CompletedWorktree); !os.IsNotExist(err) {
		t.Fatal("worktree not removed")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("the repository hook ran")
	}
}
