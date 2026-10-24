// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ownedPositionWork is the real sequence of issue #174: a worktree created by
// workspace apply (detached), `git switch -c b`, a commit, `git push -u` to a
// bare remote (which writes branch.b.remote/merge into the common config) and
// an integration into the base: a merge (the branch tip is an ancestor of the
// base) or a rebase-merge (a different SHA with the same patch).
type ownedPositionWork struct {
	Source, Dest, Branch string
	Creation             MutationPlan
	Task                 TaskRecord
	Options              V3PlanOptions
}

func ownedPositionFixture(t *testing.T) ownedPositionWork {
	t.Helper()
	return ownedPositionFixtureWith(t, "merge")
}

// mode is "merge", "rebase" (same patch, new SHA) or "rebase-whitespace" (the
// rebased patch differs from the branch only in whitespace).
func ownedPositionFixtureWith(t *testing.T, mode string) ownedPositionWork {
	t.Helper()
	source, creation, task := gitRetirementFixture(t)
	bare := gitTempDir(t)
	gitFixture(t, bare, "init", "-q", "--bare")
	gitFixture(t, source, "remote", "add", "origin", bare)
	gitFixture(t, task.Destination, "switch", "-q", "-c", "b")
	if e := os.WriteFile(filepath.Join(task.Destination, "work"), []byte("task work\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitFixture(t, task.Destination, "add", "work")
	gitFixture(t, task.Destination, "commit", "-qm", "task work")
	gitFixture(t, task.Destination, "push", "-q", "-u", "origin", "b")
	if mode != "merge" {
		gitFixture(t, source, "cherry-pick", "-n", "b")
		if mode == "rebase-whitespace" {
			if e := os.WriteFile(filepath.Join(source, "work"), []byte("task  work\n"), 0600); e != nil {
				t.Fatal(e)
			}
			gitFixture(t, source, "add", "work")
		}
		gitFixture(t, source, "commit", "-qm", "task work (rebase-merged)")
		if gitFixture(t, source, "rev-parse", "HEAD") == gitFixture(t, task.Destination, "rev-parse", "HEAD") {
			t.Fatal("rebase-merge kept the SHA")
		}
	} else {
		gitFixture(t, source, "merge", "-q", "--no-ff", "-m", "merge b", "b")
	}
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}
	return ownedPositionWork{Source: source, Dest: task.Destination, Branch: "b", Creation: creation, Task: task, Options: o}
}

func requireOperationCode(t *testing.T, e error, code string) {
	t.Helper()
	var op operationError
	if !errors.As(e, &op) || op.Code != code {
		t.Fatalf("want %s, got %v", code, e)
	}
}

func TestTidyV3OwnedRetirementAfterBranchSwitchPushAndMerge(t *testing.T) {
	w := ownedPositionFixture(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), w.Options)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("retirement of a switched, pushed, merged worktree: %+v", planned.Diagnostics)
	}
	p := *planned.Plan
	if p.Mode != "owned-retirement" || p.Retirement.Target.HeadRef != "refs/heads/b" || p.Retirement.Integration.Decision != "delete" {
		t.Fatalf("plan does not bind the attached, integrated head: %+v", p.Retirement)
	}
	applied := c.apply(context.Background(), V3ApplyOptions{StateDir: w.Creation.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if applied.Status != "ok" {
		t.Fatalf("apply: %+v", applied.Diagnostics)
	}
	if _, e := os.Lstat(w.Dest); !os.IsNotExist(e) {
		t.Fatal("directory not removed")
	}
	if got := gitFixture(t, w.Source, "worktree", "list", "--porcelain"); got == "" || strings.Contains(got, w.Dest) {
		t.Fatal("registration not removed")
	}
}

func ownedDetachedOptions(creation MutationPlan, task TaskRecord, source string) V3PlanOptions {
	return V3PlanOptions{Path: source, Base: "HEAD", StateDir: creation.StateDir, Task: task.TaskID, Owner: task.Owner, RemoveRegistration: true, RemoveFiles: task.Destination, ProtectWorktrees: []string{}}
}

func ownedApplyOptions(o V3PlanOptions, p V3Plan) V3ApplyOptions {
	return V3ApplyOptions{StateDir: o.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}}
}

func requireDiagnosticCode(t *testing.T, r V3Result, code string) {
	t.Helper()
	if r.Status == "ok" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != code {
		t.Fatalf("want %s, got status %s %+v", code, r.Status, r.Diagnostics)
	}
}

// Verify keeps the creation digest equality for the workspace and handoff
// paths; the identity part alone ignores the digest (Spec 041 FR-020a).
func TestGitVerifySplitsIdentityFromCreationDigest(t *testing.T) {
	d, creation, task := gitRetirementFixture(t)
	gitFixture(t, d, "config", "unrelated.key", "changed")
	if _, e := (gitAdapter{}).Verify(context.Background(), creation); e == nil {
		t.Fatal("creation digest equality dropped from Verify")
	} else {
		requireOperationCode(t, e, "git-stale-plan")
	}
	identity, e := (gitAdapter{}).VerifyIdentity(context.Background(), creation)
	if e != nil || !identity.Complete || !sameOwner(identity.Identity, task.Native) {
		t.Fatalf("identity part must not need the creation digest: %v", e)
	}
}

// A config key, the Git binary or a hook added before planning no longer blocks
// the retirement of an unswitched (detached) owned worktree.
func TestRetirementUnrelatedChangesStillRetireWithoutBranchSwitch(t *testing.T) {
	for _, kind := range []string{"config-key", "git-binary", "hook-before-planning"} {
		t.Run(kind, func(t *testing.T) {
			source, creation, task := gitRetirementFixture(t)
			switch kind {
			case "config-key":
				gitFixture(t, source, "config", "unrelated.key", "changed")
			case "git-binary":
				real, e := gitExecutable()
				if e != nil {
					t.Fatal(e)
				}
				bin := gitTempDir(t)
				wrapper := "#!/bin/sh\nexec " + real + " \"$@\"\n"
				if e = os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0700); e != nil {
					t.Fatal(e)
				}
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				if now, e := gitExecutable(); e != nil || now == real {
					t.Fatalf("Git binary identity did not change: %v", e)
				}
			case "hook-before-planning":
				if e := os.WriteFile(filepath.Join(source, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
					t.Fatal(e)
				}
			}
			o := ownedDetachedOptions(creation, task, source)
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Status != "ok" || planned.Plan == nil {
				t.Fatalf("plan: %+v", planned.Diagnostics)
			}
			if kind == "hook-before-planning" && len(planned.Plan.ConfigNotes) == 0 {
				t.Fatal("a hook added before planning is a plan note")
			}
			if applied := c.apply(context.Background(), ownedApplyOptions(o, *planned.Plan)); applied.Status != "ok" {
				t.Fatalf("apply: %+v", applied.Diagnostics)
			}
			if _, e := os.Lstat(task.Destination); !os.IsNotExist(e) {
				t.Fatal("directory not removed")
			}
		})
	}
}

// A hook or key added between plan and apply is the existing evidence-changed
// refusal and changes nothing.
func TestRetirementChangeBetweenPlanAndApplyRefusesEvidenceChanged(t *testing.T) {
	for _, kind := range []string{"hook", "config-key"} {
		t.Run(kind, func(t *testing.T) {
			source, creation, task := gitRetirementFixture(t)
			o := ownedDetachedOptions(creation, task, source)
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Status != "ok" {
				t.Fatalf("plan: %+v", planned.Diagnostics)
			}
			if kind == "hook" {
				if e := os.WriteFile(filepath.Join(source, ".git", "hooks", "pre-commit"), []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
					t.Fatal(e)
				}
			} else {
				gitFixture(t, source, "config", "unrelated.key", "changed")
			}
			requireDiagnosticCode(t, c.apply(context.Background(), ownedApplyOptions(o, *planned.Plan)), "evidence-changed")
			if _, e := os.Lstat(task.Destination); e != nil {
				t.Fatal("refused apply changed the worktree")
			}
		})
	}
}

func TestRetirementUnsafeConfigurationKeepsItsCode(t *testing.T) {
	source, creation, task := gitRetirementFixture(t)
	gitFixture(t, source, "config", "includeIf.gitdir:/never.path", "x")
	r := defaultTidyV3RetirementCoordinator().plan(context.Background(), ownedDetachedOptions(creation, task, source))
	requireDiagnosticCode(t, r, "git-unsafe-configuration")
}

// The v2 retirement path keeps the creation digest equality.
func TestV2RetirementKeepsCreationDigestEquality(t *testing.T) {
	d, creation, task := gitRetirementFixture(t)
	gitFixture(t, d, "config", "unrelated.key", "changed")
	if _, e := (gitRetirementAdapter{}).Observe(context.Background(), d, "HEAD", task, creation); e == nil {
		t.Fatal("v2 retirement ignored the creation digest")
	}
}

// Refusals of the position rule keep exact identity; each has its own code.
func TestTidyV3OwnedRetirementAfterBranchSwitchRefusals(t *testing.T) {
	cases := []struct {
		kind, code string
	}{
		{"dirty-tracked", "workspace-failed"},
		{"dirty-untracked", "retirement-unrecorded-entry"},
		{"wrong-owner", "invalid-ownership"},
		{"moved-lock", "active-workspace"},
		{"foreign-lock", "active-workspace"},
		{"gitdir-pointer", "git-ownership-refused"},
		{"unrelated-attached", "changed-ownership"},
		{"unrelated-detached", "changed-ownership"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			w := ownedPositionFixture(t)
			o := w.Options
			switch tc.kind {
			case "dirty-tracked":
				if e := os.WriteFile(filepath.Join(w.Dest, "file"), []byte("dirty"), 0600); e != nil {
					t.Fatal(e)
				}
			case "dirty-untracked":
				if e := os.WriteFile(filepath.Join(w.Dest, "new"), []byte("x"), 0600); e != nil {
					t.Fatal(e)
				}
			case "wrong-owner":
				o.Owner = "other"
			case "moved-lock":
				if e := os.WriteFile(filepath.Join(w.Task.Native.GitDir, "locked"), []byte("stw:other\n"), 0600); e != nil {
					t.Fatal(e)
				}
			case "foreign-lock":
				if e := os.WriteFile(filepath.Join(w.Task.Native.GitDir, "locked"), []byte("actor lock\n"), 0600); e != nil {
					t.Fatal(e)
				}
			case "gitdir-pointer":
				if e := os.WriteFile(filepath.Join(w.Dest, ".git"), []byte("gitdir: /elsewhere\n"), 0600); e != nil {
					t.Fatal(e)
				}
			case "unrelated-attached", "unrelated-detached":
				gitFixture(t, w.Dest, "switch", "-q", "--orphan", "unrelated")
				gitFixture(t, w.Dest, "commit", "-q", "--allow-empty", "-m", "unrelated root")
				if tc.kind == "unrelated-detached" {
					gitFixture(t, w.Dest, "switch", "-q", "--detach")
				}
			}
			requireDiagnosticCode(t, defaultTidyV3RetirementCoordinator().plan(context.Background(), o), tc.code)
			if _, e := os.Lstat(w.Dest); e != nil {
				t.Fatal("refused plan changed the worktree")
			}
		})
	}
}

func TestTidyV3OwnedRetirementRetainsUnintegratedBranch(t *testing.T) {
	w := ownedPositionFixture(t)
	commitUnintegratedChange(t, w.Dest, "later")
	planned := defaultTidyV3RetirementCoordinator().plan(context.Background(), w.Options)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	if r := planned.Plan.Retirement; r.Integration.Decision != "retain" || r.Integration.Reason != "integration-not-proven" || v3RetirementEligible(*r) {
		t.Fatalf("unintegrated branch became eligible: %+v", r.Integration)
	}
}

// The descendant check is a new restriction: a detached checkout that lacks
// the recorded creation commit is not the owned worktree (FR-021).
func TestTidyV3OwnedRetirementRefusesDetachedCheckoutWithoutCreationCommit(t *testing.T) {
	source, creation, task := gitRetirementFixture(t)
	gitFixture(t, task.Destination, "switch", "-q", "--orphan", "unrelated")
	gitFixture(t, task.Destination, "commit", "-q", "--allow-empty", "-m", "unrelated root")
	gitFixture(t, task.Destination, "switch", "-q", "--detach")
	requireDiagnosticCode(t, defaultTidyV3RetirementCoordinator().plan(context.Background(), ownedDetachedOptions(creation, task, source)), "changed-ownership")
}

// Moving HEAD, the branch tip, the base or the fresh safety digest between
// plan and apply is the existing evidence-changed refusal and changes nothing.
func TestTidyV3OwnedRetirementAfterBranchSwitchStaleBetweenPlanAndApply(t *testing.T) {
	for _, kind := range []string{"head", "branch-tip", "base", "safety-digest"} {
		t.Run(kind, func(t *testing.T) {
			w := ownedPositionFixture(t)
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), w.Options)
			if planned.Status != "ok" || planned.Plan == nil {
				t.Fatalf("plan: %+v", planned.Diagnostics)
			}
			switch kind {
			case "head":
				gitFixture(t, w.Dest, "commit", "-q", "--allow-empty", "-m", "moved head")
			case "branch-tip":
				gitFixture(t, w.Dest, "update-ref", "refs/heads/b", gitFixture(t, w.Source, "rev-parse", "HEAD"))
			case "base":
				gitFixture(t, w.Source, "commit", "-q", "--allow-empty", "-m", "moved base")
			case "safety-digest":
				gitFixture(t, w.Source, "config", "unrelated.key", "changed")
			}
			requireDiagnosticCode(t, c.apply(context.Background(), ownedApplyOptions(w.Options, *planned.Plan)), "evidence-changed")
			if _, e := os.Lstat(w.Dest); e != nil {
				t.Fatal("refused apply removed the directory")
			}
			if gitFixture(t, w.Source, "worktree", "list", "--porcelain") == "" || gitFixture(t, w.Source, "rev-parse", "--verify", "-q", "refs/heads/b") == "" {
				t.Fatal("refused apply changed registration or branch")
			}
		})
	}
}

// Retirement reuses the Spec 011 forge-free integration proofs on the real HEAD:
// a rebase-merged head (different SHA, same patch) retires (T001 as written).
func TestTidyV3OwnedRetirementAfterBranchSwitchPushAndRebaseMerge(t *testing.T) {
	w := ownedPositionFixtureWith(t, "rebase")
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), w.Options)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("plan: %+v", planned.Diagnostics)
	}
	if r := planned.Plan.Retirement; r.Integration.Decision != "delete" || r.Integration.Proof != "patch-id" || !v3RetirementEligible(*r) {
		t.Fatalf("rebase-merged head not proven: %+v", r.Integration)
	}
	if applied := c.apply(context.Background(), ownedApplyOptions(w.Options, *planned.Plan)); applied.Status != "ok" {
		t.Fatalf("apply: %+v", applied.Diagnostics)
	}
	if _, e := os.Lstat(w.Dest); !os.IsNotExist(e) {
		t.Fatal("directory not removed")
	}
}

func TestTidyV3OwnedRetirementRetainsWhatTheProofsDoNotCover(t *testing.T) {
	cases := []struct {
		name, mode string
		extra      bool
		change     func(*V3PlanOptions)
		retained   bool
	}{
		{"extra-unmerged-commit", "rebase", true, nil, true},
		{"whitespace-only", "rebase-whitespace", false, nil, true},
		{"whitespace-accepted", "rebase-whitespace", false, func(o *V3PlanOptions) { o.AcceptWhitespaceDifferences = true }, false},
		{"ancestor-only-keeps-rebase", "rebase", false, func(o *V3PlanOptions) { o.IntegrationProofs = []string{"ancestor"} }, true},
		{"patch-id-only-proves-rebase", "rebase", false, func(o *V3PlanOptions) { o.IntegrationProofs = []string{"patch-id"} }, false},
		{"patch-id-only-skips-ancestry", "merge", false, func(o *V3PlanOptions) { o.IntegrationProofs = []string{"patch-id"} }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := ownedPositionFixtureWith(t, tc.mode)
			if tc.extra {
				commitUnintegratedChange(t, w.Dest, "extra")
			}
			if tc.change != nil {
				tc.change(&w.Options)
			}
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), w.Options)
			if planned.Status != "ok" || planned.Plan == nil {
				t.Fatalf("plan: %+v", planned.Diagnostics)
			}
			r := planned.Plan.Retirement
			if tc.retained != !v3RetirementEligible(*r) || tc.retained && r.Integration.Reason != "integration-not-proven" {
				t.Fatalf("retained=%v: %+v", tc.retained, r.Integration)
			}
			if !tc.retained {
				if applied := c.apply(context.Background(), ownedApplyOptions(w.Options, *planned.Plan)); applied.Status != "ok" {
					t.Fatalf("apply: %+v", applied.Diagnostics)
				}
			}
		})
	}
}

// An attached HEAD at a descendant of the creation commit is valid authority.
func TestTidyV3OwnedPlanAdmitsAttachedHeadAtADescendant(t *testing.T) {
	w := ownedPositionFixture(t)
	planned := defaultTidyV3RetirementCoordinator().plan(context.Background(), w.Options)
	if planned.Status != "ok" || planned.Plan.Retirement.Target.HeadRef != "refs/heads/b" || validateV3Plan(*planned.Plan, planned.Plan.StateDir, planned.Plan.PlanID) != nil {
		t.Fatalf("attached descendant head not admitted: %+v", planned.Diagnostics)
	}
}

// commitUnintegratedChange adds a commit that changes content, so no forge-free
// proof (not even no-net-change) integrates it.
func commitUnintegratedChange(t *testing.T, dir, name string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitFixture(t, dir, "add", name)
	gitFixture(t, dir, "commit", "-qm", "not integrated: "+name)
}

// The exact lock and the backlink are part of the identity that VerifyIdentity
// keeps: each mutation of either comparison must be caught here, not by a later check.
func TestGitVerifyIdentityKeepsTheExactLockAndBacklink(t *testing.T) {
	for _, kind := range []string{"lock", "backlink"} {
		t.Run(kind, func(t *testing.T) {
			_, creation, task := gitRetirementFixture(t)
			file, content := "locked", "stw:other\n"
			if kind == "backlink" {
				file, content = "gitdir", "/elsewhere/.git\n"
			}
			if e := os.WriteFile(filepath.Join(task.Native.GitDir, file), []byte(content), 0600); e != nil {
				t.Fatal(e)
			}
			_, e := (gitAdapter{}).VerifyIdentity(context.Background(), creation)
			requireOperationCode(t, e, "git-foreign-workspace")
		})
	}
}
