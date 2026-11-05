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
	if !tidyLimitMentions(r, "submodule.*") || !tidyLimitMentions(r, "gitlinks") {
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

type unappliableCase struct {
	name, code, class string // code "" means a plan is stored and applies
	setup             func(t *testing.T, dir string)
	plainOnly         bool
}

func unappliableCases() []unappliableCase {
	cfg := func(key, value string) func(*testing.T, string) {
		return func(t *testing.T, dir string) { gitFixture(t, dir, "config", key, value) }
	}
	return []unappliableCase{
		{name: "plain-repository", setup: func(*testing.T, string) {}},
		{name: "hooksPath", code: "git-unsafe-configuration", class: "core.hooksPath", setup: func(t *testing.T, dir string) {
			gitFixture(t, dir, "config", "core.hooksPath", filepath.Join(gitTempDir(t), "hooks"))
		}},
		{name: "default-hook", code: "git-hooks-refused", class: "default-hooks", setup: func(t *testing.T, dir string) {
			hook := filepath.Join(dir, ".git", "hooks", "reference-transaction")
			if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "submodule-recurse-false", code: "git-unsafe-configuration", class: "submodule.*", setup: cfg("submodule.recurse", "false")},
		{name: "submodule-url", code: "git-unsafe-configuration", class: "submodule.*", setup: cfg("submodule.x.url", "file:///nonexistent/x.git")},
		{name: "fsmonitor-true", code: "git-unsafe-configuration", class: "core.fsmonitor", setup: cfg("core.fsmonitor", "true")},
		{name: "fsmonitor-FALSE", code: "git-unsafe-configuration", class: "core.fsmonitor", setup: cfg("core.fsmonitor", "FALSE")},
		{name: "fsmonitor-false", setup: cfg("core.fsmonitor", "false")},
		{name: "gitlink", code: "git-tree-refused", class: "gitlinks", plainOnly: true, setup: func(t *testing.T, dir string) {
			gitFixture(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+gitFixture(t, dir, "rev-parse", "HEAD")+",member")
			gitFixture(t, dir, "commit", "-qm", "add a gitlink")
		}},
	}
}

func requireNoStoredPlan(t *testing.T, state string, r TidyResult) {
	t.Helper()
	if r.Plan != nil || r.Retirement != nil || len(r.Actions) != 0 {
		t.Fatalf("refused plan carries a plan or actions: %+v", r)
	}
	if entries, err := os.ReadDir(filepath.Join(state, "tidy-plans")); err == nil && len(entries) != 0 {
		t.Fatalf("refused plan left %d files in tidy-plans", len(entries))
	}
}

// A plan stored with --state-dir must be applicable: the legacy apply observes
// without a planning session. A plan is refused, before any store write, iff
// apply would refuse the repository.
func TestTidyPlanWithStateIsRefusedIffApplyWouldRefuse(t *testing.T) {
	for _, c := range unappliableCases() {
		t.Run(c.name, func(t *testing.T) {
			dir, base := planTidyConfigFixture(t)
			c.setup(t, dir)
			state := privateDir(t)
			r := PlanTidy(context.Background(), TidyPlanOptions{Path: dir, Base: base, StateDir: state})
			if c.code == "" {
				if r.Status != "ok" || r.Plan == nil || r.Plan.PlanID == "" {
					t.Fatalf("plan refused: %s %+v", r.Status, r.Diagnostics)
				}
				if a := ApplyTidy(context.Background(), TidyApplyOptions{StateDir: state, PlanID: r.Plan.PlanID, Authorize: true}); a.Status != "ok" {
					t.Fatalf("apply of a stored plan failed: %s %+v", a.Status, a.Diagnostics)
				}
				return
			}
			if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != c.code || !strings.Contains(r.Diagnostics[0].Message, c.class) || !strings.Contains(r.Diagnostics[0].Message, "--state-dir") {
				t.Fatalf("expected %s naming %s: %s %+v", c.code, c.class, r.Status, r.Diagnostics)
			}
			requireNoStoredPlan(t, state, r)
		})
	}
}

func TestTidyRetirementPlanIsRefusedBeforeAnyStoreWrite(t *testing.T) {
	for _, c := range unappliableCases() {
		if c.plainOnly {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			source := fixtureGit(t)
			state := privateDir(t)
			dest := filepath.Join(gitTempDir(t), "task")
			// The ownership evidence binds the configuration, so it is set before
			// creation; classes that creation itself refuses cannot reach retirement.
			c.setup(t, source)
			plan := PlanCreate(ctx, PlanOptions{Path: source, Base: "HEAD", Destination: dest, Task: "cfg-task", Owner: "fixture", StateDir: state})
			if plan.Status != "ok" {
				t.Skipf("task creation refuses this class: %s", plan.Error())
			}
			if apply := Apply(ctx, ApplyOptions{StateDir: state, PlanID: plan.Plan.PlanID, Authorize: true}); apply.Status != "ok" {
				t.Fatal(apply.Error())
			}
			before := storeFileCount(t, state)
			r := PlanTidy(ctx, TidyPlanOptions{Path: source, Base: "HEAD", Task: "cfg-task", Owner: "fixture", StateDir: state, RemoveRegistration: true, RemoveFiles: dest})
			if c.code == "" {
				if r.Status != "ok" || r.Retirement == nil {
					t.Fatalf("retirement plan refused: %s %+v", r.Status, r.Diagnostics)
				}
				if a := ApplyTidy(ctx, TidyApplyOptions{StateDir: state, PlanID: r.Retirement.PlanID, Authorize: true}); a.Status != "ok" {
					t.Fatalf("apply of a stored retirement plan failed: %s %+v", a.Status, a.Diagnostics)
				}
				return
			}
			if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != c.code || !strings.Contains(r.Diagnostics[0].Message, c.class) || strings.Contains(r.Diagnostics[0].Message, "--state-dir") {
				t.Fatalf("expected %s naming %s without state advice: %s %+v", c.code, c.class, r.Status, r.Diagnostics)
			}
			requireNoStoredPlan(t, state, r)
			if after := storeFileCount(t, state); after != before {
				t.Fatalf("refused retirement plan changed the store: %d -> %d files", before, after)
			}
		})
	}
}

func storeFileCount(t *testing.T, root string) int {
	t.Helper()
	n := 0
	filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
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

func TestTidyPlanNamesExecutableDefaultHookInTheStatelessLimitation(t *testing.T) {
	dir, base := planTidyConfigFixture(t)
	hook := filepath.Join(dir, ".git", "hooks", "reference-transaction")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if r := planTidyOnce(dir, base); r.Status != "ok" || !tidyLimitMentions(r, "default-hooks") {
		t.Fatalf("no default-hooks warning: %s %+v", r.Status, r.Limitations)
	}
}
