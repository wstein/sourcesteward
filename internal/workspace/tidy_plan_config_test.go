// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #390: the offline v1/v2 plan (`stw tidy plan --base`) refused the same
// repositories the v3 and wizard plans read (Spec 031). It reads them read-only.
func planTidyConfigFixture(t *testing.T) (dir, base string) {
	t.Helper()
	dir = fixtureGit(t)
	return dir, gitFixture(t, dir, "symbolic-ref", "HEAD")
}

func planTidyOnce(dir, base string) TidyResult {
	return PlanTidy(context.Background(), TidyPlanOptions{Path: dir, Base: base})
}

func TestTidyPlanReadsHooksPathWithoutRunningAnyHook(t *testing.T) {
	dir, base := planTidyConfigFixture(t)
	hooks := filepath.Join(gitTempDir(t), "hooks")
	marker := filepath.Join(gitTempDir(t), "hook-ran")
	if err := os.MkdirAll(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho ran >> '" + marker + "'\n"
	for _, name := range []string{"pre-commit", "post-checkout", "post-commit", "post-merge", "reference-transaction", "pre-auto-gc", "fsmonitor-watchman"} {
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	gitFixture(t, dir, "config", "core.hooksPath", hooks)
	r := planTidyOnce(dir, base)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("plan refused: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
	}
	if !tidyLimitMentions(r, "core.hooksPath") {
		t.Fatalf("no neutralization warning: %+v", r.Limitations)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a hook ran during planning: %v", err)
	}
}

func TestTidyPlanReadsSubmodulesWithoutTouchingThem(t *testing.T) {
	dir, base := planTidyConfigFixture(t)
	marker := filepath.Join(gitTempDir(t), "update-ran")
	gitmodules := "[submodule \"member\"]\n\tpath = member\n\turl = file:///nonexistent/member.git\n"
	if err := os.WriteFile(filepath.Join(dir, ".gitmodules"), []byte(gitmodules), 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "add", ".gitmodules")
	gitFixture(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+gitFixture(t, dir, "rev-parse", "HEAD")+",member")
	gitFixture(t, dir, "commit", "-qm", "add a gitlink")
	gitFixture(t, dir, "config", "submodule.member.url", "file:///nonexistent/member.git")
	gitFixture(t, dir, "config", "submodule.member.active", "true")
	gitFixture(t, dir, "config", "submodule.member.update", "!echo ran > '"+marker+"'; true")
	gitFixture(t, dir, "config", "submodule.recurse", "true")
	r := planTidyOnce(dir, base)
	if r.Status != "ok" || r.Plan == nil {
		t.Fatalf("plan refused: status=%s diagnostics=%+v", r.Status, r.Diagnostics)
	}
	if !tidyLimitMentions(r, "submodule.*") {
		t.Fatalf("no neutralization warning: %+v", r.Limitations)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a submodule update helper ran during planning: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "member")); err == nil && len(entries) > 0 {
		t.Fatalf("a submodule was populated: %v", entries)
	}
	if modules, err := os.Stat(filepath.Join(dir, ".git", "modules")); err == nil {
		t.Fatalf("submodule metadata was created: %v", modules.Name())
	}
}

func TestTidyPlanStillRefusesUnsafeConfigurationNamingTheKey(t *testing.T) {
	for key, name := range map[string]string{"hook.pre.command": "hook.*", "core.attributesFile": "core.attributesfile", "core.sparseCheckout": "core.sparsecheckout"} {
		t.Run(key, func(t *testing.T) {
			dir, base := planTidyConfigFixture(t)
			gitFixture(t, dir, "config", key, "true")
			r := planTidyOnce(dir, base)
			if r.Plan != nil || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "git-unsafe-configuration" || !strings.Contains(r.Diagnostics[0].Message, `"`+name+`"`) {
				t.Fatalf("unsafe key %s not refused by name: status=%s %+v", key, r.Status, r.Diagnostics)
			}
		})
	}
}

func tidyLimitMentions(r TidyResult, class string) bool {
	for _, l := range r.Limitations {
		if strings.HasPrefix(l, "Read-only plan:") && strings.Contains(l, class) {
			return true
		}
	}
	return false
}

// A plan stored with --state must be applicable: the legacy apply observes
// without a planning session and refuses hooksPath, so planning refuses it.
func TestTidyPlanWithStateRefusesHooksPathInsteadOfUnappliablePlan(t *testing.T) {
	dir, base := planTidyConfigFixture(t)
	gitFixture(t, dir, "config", "core.hooksPath", filepath.Join(gitTempDir(t), "hooks"))
	state := filepath.Join(gitTempDir(t), "state")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	r := PlanTidy(context.Background(), TidyPlanOptions{Path: dir, Base: base, StateDir: state})
	if r.Plan != nil || r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "git-unsafe-configuration" || !strings.Contains(r.Diagnostics[0].Message, "core.hooksPath") {
		t.Fatalf("expected plan-time refusal naming core.hooksPath: %s %+v", r.Status, r.Diagnostics)
	}
}

func TestTidyPlanIgnoresDisabledFsmonitorAndSubmoduleRecurse(t *testing.T) {
	dir, base := planTidyConfigFixture(t)
	gitFixture(t, dir, "config", "core.fsmonitor", "false")
	gitFixture(t, dir, "config", "submodule.recurse", "false")
	r := planTidyOnce(dir, base)
	if r.Status != "ok" || tidyLimitMentions(r, "core.fsmonitor") || tidyLimitMentions(r, "submodule.*") {
		t.Fatalf("false warning: %s %+v", r.Status, r.Limitations)
	}
}
