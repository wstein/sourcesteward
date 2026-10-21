// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The repository's hooks are never run by stw, whether the hook directory is
// the relative core.hooksPath of a project (.githooks) or an absolute one.
func TestCreationNeverRunsRepositoryHooks(t *testing.T) {
	for _, mode := range []string{"relative", "absolute"} {
		t.Run(mode, func(t *testing.T) {
			d := fixtureGit(t)
			marker := filepath.Join(t.TempDir(), "hook-ran")
			hooks := filepath.Join(d, ".githooks")
			if e := os.Mkdir(hooks, 0700); e != nil {
				t.Fatal(e)
			}
			script := "#!/bin/sh\ntouch '" + marker + "'\n"
			for _, name := range []string{"post-checkout", "reference-transaction"} {
				if e := os.WriteFile(filepath.Join(hooks, name), []byte(script), 0700); e != nil {
					t.Fatal(e)
				}
			}
			value := ".githooks"
			if mode == "absolute" {
				value = hooks
			}
			gitFixture(t, d, "config", "core.hooksPath", value)
			// Control: plain Git does run this hook for a worktree add.
			control := filepath.Join(gitTempDir(t), "control")
			gitFixture(t, d, "worktree", "add", "--detach", control, "HEAD")
			if _, e := os.Stat(marker); e != nil {
				t.Fatalf("fixture hook does not run under plain Git: %v", e)
			}
			if e := os.Remove(marker); e != nil {
				t.Fatal(e)
			}
			gitFixture(t, d, "worktree", "remove", "--force", control)
			if e := os.Remove(marker); e != nil && !os.IsNotExist(e) {
				t.Fatal(e)
			}
			requireCreation(t, d)
			if _, e := os.Stat(marker); !os.IsNotExist(e) {
				t.Fatalf("a repository hook ran during plan or apply: %v", e)
			}
		})
	}
}

// Submodule configuration is data and gitlinks are accepted as data: nothing
// is initialized, fetched or run, and the source gains no module directory.
func TestCreationAcceptsSubmoduleConfigurationAndGitlinksAsData(t *testing.T) {
	d := fixtureGit(t)
	base := gitFixture(t, d, "rev-parse", "HEAD")
	marker := filepath.Join(t.TempDir(), "helper-ran")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		gitFixture(t, d, "update-index", "--add", "--cacheinfo", "160000,"+base+","+name)
	}
	gitFixture(t, d, "commit", "-qm", "gitlinks")
	for k, v := range map[string]string{
		"submodule.alpha.url":    "file:///nonexistent",
		"submodule.alpha.active": "true",
		"submodule.alpha.update": "!touch '" + marker + "'",
		"submodule.recurse":      "true",
	} {
		gitFixture(t, d, "config", k, v)
	}
	dest := requireCreation(t, d)
	if _, e := os.Stat(marker); !os.IsNotExist(e) {
		t.Fatalf("a submodule helper ran: %v", e)
	}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		entries, e := os.ReadDir(filepath.Join(dest, name))
		if e != nil || len(entries) != 0 {
			t.Fatalf("gitlink %s is not an empty directory: %v %v", name, entries, e)
		}
	}
	if _, e := os.Lstat(filepath.Join(d, ".git", "modules")); !os.IsNotExist(e) {
		t.Fatalf("source gained a submodule git directory: %v", e)
	}
	if _, e := os.Lstat(filepath.Join(dest, "file")); e != nil {
		t.Fatalf("regular file missing: %v", e)
	}
}

// The other refusals of the creation list stay.
func TestCreationKeepsOtherRefusals(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"core.fsmonitor", "touch /tmp/not-executed"},
		{"hook.x.command", "true"},
		{"includeIf.onbranch:task.path", "absent"},
		{"core.attributesFile", "/dev/null"},
		{"extensions.worktreeConfig", "true"},
		{"core.sparseCheckout", "true"},
		{"remote.origin.promisor", "true"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			d := fixtureGit(t)
			gitFixture(t, d, "config", tc.key, tc.value)
			requireCreationRefused(t, d, "git-unsafe-configuration")
		})
	}
	t.Run("symlink", func(t *testing.T) {
		d := fixtureGit(t)
		if e := os.Symlink("file", filepath.Join(d, "link")); e != nil {
			t.Fatal(e)
		}
		gitFixture(t, d, "add", ".")
		gitFixture(t, d, "commit", "-qm", "link")
		requireCreationRefused(t, d, "git-tree-refused")
	})
	t.Run("executable-default-hook", func(t *testing.T) {
		d := fixtureGit(t)
		if e := os.WriteFile(filepath.Join(d, ".git", "hooks", "post-checkout"), []byte("#!/bin/sh\n"), 0700); e != nil {
			t.Fatal(e)
		}
		requireCreationRefused(t, d, "git-hooks-refused")
	})
}

// Tidy keeps its strict admission: only creation changed.
func TestTidyAdmissionStaysStrictForHooksPathAndGitlinks(t *testing.T) {
	t.Run("hooks-path", func(t *testing.T) {
		d := fixtureGit(t)
		gitFixture(t, d, "config", "core.hooksPath", "empty-hooks")
		base := gitFixture(t, d, "rev-parse", "HEAD")
		if _, e := (gitAdapter{}).Observe(context.Background(), d, base); e == nil {
			t.Fatal("strict admission accepted core.hooksPath")
		}
	})
	t.Run("gitlink", func(t *testing.T) {
		d := fixtureGit(t)
		head := gitFixture(t, d, "rev-parse", "HEAD")
		gitFixture(t, d, "update-index", "--add", "--cacheinfo", "160000,"+head+",module")
		gitFixture(t, d, "commit", "-qm", "gitlink")
		base := gitFixture(t, d, "rev-parse", "HEAD")
		if _, e := (gitAdapter{}).Observe(context.Background(), d, base); e == nil {
			t.Fatal("strict admission accepted a gitlink")
		}
	})
}
