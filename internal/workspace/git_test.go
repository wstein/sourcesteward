// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gitTempDir(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func fixtureGit(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "darwin" {
		if p, e := exec.LookPath("git"); e == nil && p == "/usr/bin/git" {
			for _, native := range []string{"/Applications/Xcode.app/Contents/Developer/usr/bin/git", "/Library/Developer/CommandLineTools/usr/bin/git"} {
				if st, e := os.Stat(native); e == nil && st.Mode().IsRegular() {
					t.Setenv("PATH", filepath.Dir(native)+":"+os.Getenv("PATH"))
					break
				}
			}
		}
	}
	t.Setenv("HOME", gitTempDir(t))
	t.Setenv("XDG_CONFIG_HOME", gitTempDir(t))
	d := gitTempDir(t)
	gitFixture(t, d, "init", "-q")
	gitFixture(t, d, "config", "user.name", "Fixture")
	gitFixture(t, d, "config", "user.email", "fixture@example.invalid")
	if err := os.WriteFile(filepath.Join(d, "file"), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, d, "add", ".")
	gitFixture(t, d, "commit", "-qm", "fixture")
	return d
}

func TestGitWorkspaceLiteralBasesAndStaleSafety(t *testing.T) {
	d := fixtureGit(t)
	for _, base := range []string{"HEAD", gitFixture(t, d, "symbolic-ref", "HEAD")} {
		if _, err := (gitAdapter{}).Observe(context.Background(), d, base); err != nil {
			t.Fatalf("literal base %q: %v", base, err)
		}
	}
	s, err := (gitAdapter{}).Observe(context.Background(), d, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	gitFixture(t, d, "config", "safe.test", "changed")
	dest := gitTempDir(t)
	if _, err := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "stale", SourcePath: d, Destination: dest, Expected: s}); err == nil {
		t.Fatal("stale safety admitted")
	}
	entries, err := os.ReadDir(dest)
	if err != nil || len(entries) != 0 {
		t.Fatal("stale plan changed reserved destination")
	}
}

func TestGitWorkspaceLinkedSourceMetadataPreserved(t *testing.T) {
	d := fixtureGit(t)
	linked := filepath.Join(gitTempDir(t), "linked")
	gitFixture(t, d, "worktree", "add", "--detach", linked, "HEAD")
	id, err := gitNativeIdentity(context.Background(), linked)
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(id.GitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, "file"), []byte("linked dirty\n"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := (gitAdapter{}).Observe(context.Background(), linked, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	dest := gitTempDir(t)
	n, err := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "linked-source", SourcePath: linked, Destination: dest, Expected: s})
	if err != nil || !n.SourceUnchanged {
		t.Fatalf("create from linked: %v %+v", err, n)
	}
	got, err := os.ReadFile(filepath.Join(id.GitDir, "index"))
	if err != nil || string(got) != string(index) {
		t.Fatal("linked source index changed")
	}
	dirty, err := os.ReadFile(filepath.Join(linked, "file"))
	if err != nil || string(dirty) != "linked dirty\n" {
		t.Fatal("linked source content changed")
	}
}

func TestGitTreeAdmissionBounds(t *testing.T) {
	for _, raw := range []string{"160000 commit abc -\tmodule\x00", "120000 blob abc 5\tlink\x00", "100644 blob abc 16777217\tlarge\x00", "100644 blob abc 5\t../outside\x00", "100644 blob abc 5\t.GIT/config\x00"} {
		if err := validateGitTree(raw); err == nil {
			t.Fatalf("unsafe tree admitted: %q", raw)
		}
	}
	if err := validateGitTree("100644 blob abc 5\tspaces and\nnewlines\x00"); err != nil {
		t.Fatal(err)
	}
}
func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func TestGitWorkspaceNativeCreatePreservesSource(t *testing.T) {
	d := fixtureGit(t)
	base := gitFixture(t, d, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(d, "file"), []byte("dirty\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "untracked"), []byte("keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	index, _ := os.ReadFile(filepath.Join(d, ".git", "index"))
	config, _ := os.ReadFile(filepath.Join(d, ".git", "config"))
	s, err := (gitAdapter{}).Observe(context.Background(), d, base)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(gitTempDir(t), "task")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	p := MutationPlan{TaskID: "test-task", SourcePath: d, Destination: dest, Expected: s}
	n, err := (gitAdapter{}).Create(context.Background(), p)
	if err != nil {
		t.Fatalf("create: %v %+v", err, n)
	}
	if !n.Complete || !n.SourceUnchanged || n.Identity.CommitID != base || n.Identity.HeadRef != "" {
		t.Fatalf("native result: %+v", n)
	}
	for name, want := range map[string]string{"file": "dirty\n", "untracked": "keep\n"} {
		got, e := os.ReadFile(filepath.Join(d, name))
		if e != nil || string(got) != want {
			t.Fatalf("source %s changed", name)
		}
	}
	for name, want := range map[string][]byte{"index": index, "config": config} {
		got, e := os.ReadFile(filepath.Join(d, ".git", name))
		if e != nil || string(got) != string(want) {
			t.Fatalf("source %s changed", name)
		}
	}
	got, _ := os.ReadFile(filepath.Join(dest, "file"))
	if string(got) != "stored\n" {
		t.Fatalf("checkout: %q", got)
	}
	if _, err := (gitAdapter{}).Verify(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dest, "config", "user.name", "Fixture") // same shared setting, no content change
	if err := os.WriteFile(filepath.Join(dest, "file"), []byte("progress\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dest, "add", "file")
	gitFixture(t, dest, "commit", "-qm", "progress")
	if _, err := (gitAdapter{}).Verify(context.Background(), p); err != nil {
		t.Fatalf("normal task progress rejected: %v", err)
	}
}

func TestGitWorkspaceRefusesUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"core.hooksPath", "empty-hooks"}, {"filter.hostile.smudge", "touch /tmp/not-executed"}, {"core.fsmonitor", "touch /tmp/not-executed"}, {"includeIf.onbranch:task.path", "absent"}, {"core.sparseCheckout", "true"}, {"extensions.worktreeConfig", "true"}, {"remote.origin.promisor", "true"}} {
		t.Run(tc.key, func(t *testing.T) {
			d := fixtureGit(t)
			base := gitFixture(t, d, "rev-parse", "HEAD")
			gitFixture(t, d, "config", tc.key, tc.value)
			if _, err := (gitAdapter{}).Observe(context.Background(), d, base); err == nil {
				t.Fatal("unsafe configuration admitted")
			}
		})
	}
}

func TestGitWorkspaceRefusesExecutableHooksAndGitlinks(t *testing.T) {
	for _, hook := range []string{"post-checkout", "reference-transaction"} {
		t.Run(hook, func(t *testing.T) {
			d := fixtureGit(t)
			base := gitFixture(t, d, "rev-parse", "HEAD")
			sentinel := filepath.Join(t.TempDir(), "called")
			h := filepath.Join(d, ".git", "hooks", hook)
			if err := os.WriteFile(h, []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err := (gitAdapter{}).Observe(context.Background(), d, base); err == nil {
				t.Fatal("hook admitted")
			}
			if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
				t.Fatal("hook executed")
			}
		})
	}
	t.Run("gitlink", func(t *testing.T) {
		d := fixtureGit(t)
		base := gitFixture(t, d, "rev-parse", "HEAD")
		gitFixture(t, d, "update-index", "--add", "--cacheinfo", "160000,"+base+",module")
		gitFixture(t, d, "commit", "-qm", "gitlink")
		base = gitFixture(t, d, "rev-parse", "HEAD")
		if _, err := (gitAdapter{}).Observe(context.Background(), d, base); err == nil {
			t.Fatal("gitlink admitted")
		}
	})
}

func TestGitWorkspaceRefusesSelectorsAndOccupiedDestination(t *testing.T) {
	d := fixtureGit(t)
	for _, base := range []string{"HEAD~0", "main", "--help", "HEAD^{commit}"} {
		if _, err := (gitAdapter{}).Observe(context.Background(), d, base); err == nil {
			t.Fatalf("selector %q admitted", base)
		}
	}
	base := gitFixture(t, d, "rev-parse", "HEAD")
	s, err := (gitAdapter{}).Observe(context.Background(), d, base)
	if err != nil {
		t.Fatal(err)
	}
	dest := gitTempDir(t)
	if err := os.WriteFile(filepath.Join(dest, "foreign"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (gitAdapter{}).Create(context.Background(), MutationPlan{TaskID: "test", SourcePath: d, Destination: dest, Expected: s}); err == nil {
		t.Fatal("occupied destination admitted")
	}
	got, _ := os.ReadFile(filepath.Join(dest, "foreign"))
	if string(got) != "keep" {
		t.Fatal("foreign state changed")
	}
}
