// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// A retirement planned under core.hooksPath refuses at apply, naming the class,
// before any journal, recovery reference or removal.
func TestTidyV3RetirementApplyRefusesHookConfigurationBeforeAnyEffect(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	hooks := gitTempDir(t)
	gitFixture(t, o.Path, "config", "core.hooksPath", hooks)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Status != "ok" || planned.Plan == nil || !v3RetirementEligible(*planned.Plan.Retirement) {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: o.StateDir, PlanID: planned.Plan.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "git-unsafe-configuration" || !strings.Contains(r.Diagnostics[0].Message, "core.hooksPath") {
		t.Fatalf("apply did not refuse by key class: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(o.StateDir, "tidy-v3-operations", planned.Plan.PlanID+".json")); !os.IsNotExist(err) {
		t.Fatal("a journal was written before the refusal")
	}
	if out := gitFixture(t, o.Path, "for-each-ref", "refs/stw/recovery"); out != "" {
		t.Fatalf("recovery reference created: %s", out)
	}
	if _, err := os.Stat(o.CompletedWorktree); err != nil {
		t.Fatal("worktree removed")
	}
}
