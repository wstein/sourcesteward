// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

type fixtureTidyV3PostProvider struct {
	tidyV3Hosting
	hook func()
}

func (s *fixtureTidyV3PostProvider) RevalidatePullRequest(ctx context.Context, pr hosting.TidyPullRequest) error {
	if s.hook != nil {
		s.hook()
	}
	return s.tidyV3Hosting.RevalidatePullRequest(ctx, pr)
}
func TestTidyV3PostProviderParentModeChangeIsPartial(t *testing.T) {
	o, _ := tidyV3CommittedLegacyFixture(t, false, 3)
	o.StateDir = privateDir(t)
	o, repo, row := fixtureTidyV3RetirementMergedHead(t, o)
	c := defaultTidyV3RetirementCoordinator()
	fixtureTidyV3RetirementHosting(&c, repo, row, nil)
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	removed, changed := false, false
	remove := c.remove
	c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
		out, code, e := remove(ctx, bin, args, cwd, wr, ro)
		if e == nil && code == 0 {
			removed = true
		}
		return out, code, e
	}
	c.refs.openHosting = func(context.Context, string, hosting.TidySessionOptions) (tidyV3Hosting, error) {
		s := &fixtureTidyV3Hosting{repo: repo, rows: map[string]hosting.TidyHeadEvidence{row.HeadOID: row}}
		return &fixtureTidyV3PostProvider{tidyV3Hosting: s, hook: func() {
			if removed && !changed {
				if e := os.Chmod(filepath.Dir(o.CompletedWorktree), 0711); e != nil {
					t.Fatal(e)
				}
				changed = true
			}
		}}, nil
	}
	got := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Online: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if !removed || !changed {
		t.Fatal("actor not injected", removed, changed, got.Diagnostics)
	}
	if got.Status != "partial" || got.Plan != nil {
		t.Fatalf("changed destination parent accepted after provider revalidation: status=%s diagnostics=%+v", got.Status, got.Diagnostics)
	}
}
func TestTidyV3NestedCanonicalGrantParentsComplete(t *testing.T) {
	source, _, common := fixtureGitLayout(t, "separate", false, true)
	gitFixture(t, source, "update-ref", "refs/stw/recovery/v3/prior", "HEAD")
	target := filepath.Join(common, "completed-workspace")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "completed", target, "HEAD")
	// Keep a distinct active registration so native removal retains worktrees/.
	active := filepath.Join(gitTempDir(t), "active")
	gitFixture(t, source, "worktree", "add", "-q", "-b", "active", active, "HEAD")
	o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: privateDir(t), CompletedWorktree: target, Owner: "operator", RemoveRegistration: true, RemoveFiles: target, ProtectWorktrees: []string{active}}
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatalf("nested canonical plan refused: %+v", planned.Diagnostics)
	}
	p := *planned.Plan
	got := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: target, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if got.Status != "ok" || got.Plan == nil {
		t.Fatalf("admitted nested parents cannot complete: status=%s diagnostics=%+v", got.Status, got.Diagnostics)
	}
}

func TestTidyV3RecoveryNamespaceMustPreserveEveryDirectoryFact(t *testing.T) {
	for _, kind := range []string{"cold-ref", "cold-reflog-sibling", "cold-reflog-direct-parent"} {
		t.Run(kind, func(t *testing.T) {
			source, _, common := fixtureGitLayout(t, "separate", false, true)
			if kind != "cold-ref" {
				gitFixture(t, source, "-c", "core.logAllRefUpdates=false", "update-ref", "refs/stw/recovery/v3/prior", "HEAD")
			}
			target := filepath.Join(common, "completed-workspace")
			if kind == "cold-reflog-sibling" {
				target = filepath.Join(common, "logs", "completed-workspace")
			}
			if kind == "cold-reflog-direct-parent" {
				target = filepath.Join(common, "logs", "refs", "stw", "recovery", "v3", "completed-workspace")
			}
			gitFixture(t, source, "worktree", "add", "-q", "-b", "completed", target, "HEAD")
			if kind != "cold-ref" {
				gitFixture(t, source, "config", "core.logAllRefUpdates", "always")
			}
			before := gitFixture(t, source, "for-each-ref", "--format=%(refname) %(objectname)")
			o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: privateDir(t), CompletedWorktree: target, Owner: "operator", RemoveRegistration: true, RemoveFiles: target, ProtectWorktrees: []string{}}
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Status == "ok" || planned.Plan != nil {
				t.Fatal("deterministic protected-container mutation got executable plan", kind, planned.Diagnostics)
			}
			if after := gitFixture(t, source, "for-each-ref", "--format=%(refname) %(objectname)"); after != before {
				t.Fatal("upfront refusal changed native refs")
			}
			id, e := gitNativeIdentity(context.Background(), target)
			if e != nil {
				t.Fatal("selected registration changed", e)
			}
			if _, e = os.Stat(id.GitDir); e != nil {
				t.Fatal("selected metadata removed")
			}
			for _, name := range []string{"tidy-v3-plans", "tidy-v3-operations"} {
				if _, e = os.Stat(filepath.Join(o.StateDir, name)); !os.IsNotExist(e) {
					t.Fatal("upfront refusal persisted executable state", name, e)
				}
			}
		})
	}
}

func TestTidyV3ColdRecoveryNamespacesOutsideProtectedContainersComplete(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	id, e := gitNativeIdentity(context.Background(), o.Path)
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{filepath.Join(id.RepositoryDir, "refs", "stw"), filepath.Join(id.RepositoryDir, "logs", "refs", "stw")} {
		if _, e = os.Stat(path); !os.IsNotExist(e) {
			t.Fatal("fixture namespace not cold", path, e)
		}
	}
	gitFixture(t, o.Path, "config", "core.logAllRefUpdates", "always")
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal("ordinary cold namespaces refused", planned.Diagnostics)
	}
	p := *planned.Plan
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if r.Status != "ok" || r.Plan == nil {
		t.Fatal("ordinary cold first-use retirement failed", r.Diagnostics)
	}
	recovery := v3RecoveryFor(p, p.Retirement.ActionID, p.Retirement.Target.CommitID)
	for _, path := range []string{filepath.Join(id.RepositoryDir, recovery.Name), filepath.Join(id.RepositoryDir, "logs", recovery.Name)} {
		if st, e := os.Stat(path); e != nil || !st.Mode().IsRegular() {
			t.Fatal("actual native ref/reflog absent", path, e)
		}
	}
}

func TestTidyV3FinalNativeControlParentModeChangeIsPartial(t *testing.T) {
	o, _ := tidyV3LegacyFixture(t, false)
	o.StateDir = privateDir(t)
	c := defaultTidyV3RetirementCoordinator()
	planned := c.plan(context.Background(), o)
	if planned.Plan == nil {
		t.Fatal(planned.Diagnostics)
	}
	p := *planned.Plan
	removed, changed := false, false
	remove := c.remove
	c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
		out, code, e := remove(ctx, bin, args, cwd, wr, ro)
		removed = e == nil && code == 0
		return out, code, e
	}
	observe := c.refs.observe
	c.refs.observe = func(ctx context.Context, path, base string) (tidyV3NativeObservation, error) {
		out, e := observe(ctx, path, base)
		if e == nil && removed && !changed {
			if e = os.Chmod(filepath.Dir(o.CompletedWorktree), 0711); e != nil {
				t.Fatal(e)
			}
			changed = true
		}
		return out, e
	}
	r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: o.CompletedWorktree, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
	if !removed || !changed || r.Status != "partial" || r.Plan != nil {
		t.Fatal("late native callback parent change became success", removed, changed, r.Diagnostics)
	}
}

func TestTidyV3NestedPostRemovalPreservesAllStableAndUnrelatedFacts(t *testing.T) {
	for _, kind := range []string{"registration-parent-mode", "unrelated-file-write", "unrelated-directory-mode"} {
		t.Run(kind, func(t *testing.T) {
			source, _, common := fixtureGitLayout(t, "separate", false, true)
			gitFixture(t, source, "update-ref", "refs/stw/recovery/v3/prior", "HEAD")
			target := filepath.Join(common, "completed-workspace")
			gitFixture(t, source, "worktree", "add", "-q", "-b", "completed", target, "HEAD")
			active := filepath.Join(gitTempDir(t), "active")
			gitFixture(t, source, "worktree", "add", "-q", "-b", "active", active, "HEAD")
			actor := filepath.Join(common, "sentinel")
			if e := os.WriteFile(actor, []byte("before"), 0600); e != nil {
				t.Fatal(e)
			}
			o := V3PlanOptions{Path: source, Base: "HEAD", StateDir: privateDir(t), CompletedWorktree: target, Owner: "operator", RemoveRegistration: true, RemoveFiles: target, ProtectWorktrees: []string{active}}
			c := defaultTidyV3RetirementCoordinator()
			planned := c.plan(context.Background(), o)
			if planned.Plan == nil {
				t.Fatal(planned.Diagnostics)
			}
			p := *planned.Plan
			remove := c.remove
			changed := false
			c.remove = func(ctx context.Context, bin string, args []string, cwd string, wr, ro []string) (string, int, error) {
				out, code, e := remove(ctx, bin, args, cwd, wr, ro)
				if e == nil && code == 0 {
					switch kind {
					case "registration-parent-mode":
						e = os.Chmod(p.Retirement.RegistrationParent.Path, 0711)
					case "unrelated-file-write":
						e = os.WriteFile(actor, []byte("actor!"), 0600)
					case "unrelated-directory-mode":
						e = os.Chmod(filepath.Join(common, "objects"), 0711)
					}
					if e != nil {
						t.Fatal(e)
					}
					changed = true
				}
				return out, code, e
			}
			r := c.apply(context.Background(), V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, CompletedWorktree: target, Owner: o.Owner, ProtectWorktrees: o.ProtectWorktrees})
			if !changed || r.Status != "partial" || r.Plan != nil {
				t.Fatal("stable/unrelated postfact relaxed", kind, r.Diagnostics)
			}
			if kind == "unrelated-file-write" {
				if b, e := os.ReadFile(actor); e != nil || string(b) != "actor!" {
					t.Fatal("actor bytes altered")
				}
			}
		})
	}
}
