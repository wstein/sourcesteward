// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// repo is a temporary Git repository. The identity is passed per command with -c, never stored.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &repo{t: t, dir: dir}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	full := append([]string{"-C", r.dir, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// commit writes the file, commits it and returns the commit id.
func (r *repo) commit(name, content, msg string) string {
	r.t.Helper()
	r.write(name, content)
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

func (r *repo) head() string { return r.git("rev-parse", "HEAD") }

// c1File changes four lines, so that range-diff pairs a whitespace-edited c1 with the original (!).
const c1File = "one\nTWO\nthree\nFOUR\nfive\nSIX\nseven\nEIGHT\n"

const aFile = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n"

// stack is the usual fixture: main has m1; feature has c1 (a.txt) and c2 (b.txt); main then gets m2.
type stack struct {
	r                  *repo
	m1, c1, c2, m2, f2 string
}

func newStack(t *testing.T) *stack {
	t.Helper()
	r := newRepo(t)
	r.write("a.txt", aFile)
	r.write("b.txt", "x = 1\ny = 2\n")
	r.write("c.txt", "p\nq\n")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "m1")
	s := &stack{r: r, m1: r.head()}
	r.git("checkout", "-q", "-b", "feature")
	s.c1 = r.commit("a.txt", c1File, "c1")
	s.c2 = r.commit("b.txt", "x = 1\ny = 3\n", "c2")
	r.git("checkout", "-q", "main")
	s.m2 = r.commit("c.txt", "p\nq\nr\n", "m2")
	return s
}

// rebased returns feature rebased onto main as a new branch and its head.
func (s *stack) rebased(branch string) string {
	s.r.git("checkout", "-q", "-b", branch, "feature")
	s.r.git("rebase", "-q", "main")
	return s.r.head()
}

func (s *stack) check(t *testing.T, newBase, newHead string) Result {
	t.Helper()
	return Check(t.Context(), NewRunner(s.r.dir), Request{PR: 12, OldBase: s.m1, OldHead: s.c2, NewBase: newBase, NewHead: newHead})
}
