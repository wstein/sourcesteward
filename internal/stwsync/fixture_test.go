// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, dir, file, content string) {
	t.Helper()
	write(t, filepath.Join(dir, file), content)
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-q", "-m", "change "+file)
}

// fixture is a repository with a bare remote, a second clone that moves the remote, and linked worktrees.
type fixture struct {
	root, bare, repo, other string
}

// newFixture builds, in a temporary directory: a bare origin, a primary clone "repo" (main checked
// out) and a second clone "other" used to move the remote.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{root: root, bare: filepath.Join(root, "origin.git"), repo: filepath.Join(root, "repo"), other: filepath.Join(root, "other")}
	runGit(t, root, "init", "-q", "--bare", "-b", "main", f.bare)
	runGit(t, root, "init", "-q", "-b", "main", f.repo)
	runGit(t, f.repo, "remote", "add", "origin", f.bare)
	commit(t, f.repo, "a.txt", "one\n")
	runGit(t, f.repo, "push", "-q", "-u", "origin", "main")
	runGit(t, root, "clone", "-q", f.bare, f.other)
	return f
}

// branchOnRemote creates branch NAME at the current main on the remote and as a tracking local branch.
func (f *fixture) branchOnRemote(t *testing.T, name string) {
	t.Helper()
	runGit(t, f.repo, "branch", name, "main")
	runGit(t, f.repo, "push", "-q", "-u", "origin", name)
}

// advanceRemote adds a commit to branch NAME of the remote through the second clone.
func (f *fixture) advanceRemote(t *testing.T, name string) {
	t.Helper()
	runGit(t, f.other, "fetch", "-q", "origin")
	runGit(t, f.other, "checkout", "-q", "-B", name, "origin/"+name)
	commit(t, f.other, name+"-remote.txt", name+"\n")
	runGit(t, f.other, "push", "-q", "origin", name)
}

// advanceLocal adds a commit to local branch NAME without leaving the primary worktree on it.
func (f *fixture) advanceLocal(t *testing.T, name string) {
	t.Helper()
	tree := filepath.Join(f.root, "tmp-"+name)
	runGit(t, f.repo, "worktree", "add", "-q", tree, name)
	commit(t, tree, name+"-local.txt", name+"\n")
	runGit(t, f.repo, "worktree", "remove", "--force", tree)
}

func (f *fixture) worktree(t *testing.T, branch string) string {
	t.Helper()
	tree := filepath.Join(f.root, "wt-"+branch)
	runGit(t, f.repo, "worktree", "add", "-q", tree, branch)
	return tree
}

func (f *fixture) oid(t *testing.T, ref string) string {
	t.Helper()
	return runGit(t, f.repo, "rev-parse", "--verify", ref)
}

func itemsByBranch(t *testing.T, r Result) map[string]Item {
	t.Helper()
	m := map[string]Item{}
	for _, it := range r.Items {
		if _, dup := m[it.Branch]; dup {
			t.Fatalf("duplicate item %s", it.Branch)
		}
		m[it.Branch] = it
	}
	return m
}

func itoa(i int) string { return strconv.Itoa(i) }

func gitCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}

func newTestJournal(t *testing.T, e env) *journal {
	t.Helper()
	j, err := newJournal(context.Background(), e, "origin")
	if err != nil {
		t.Fatal(err)
	}
	return j
}
