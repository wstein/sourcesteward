// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	ownerIdentity = "Fixture Owner <owner@example.org>"
	placeholder   = "t <t@t>"
)

// repo is a real temporary Git repository. The identity of fixture commits is passed through the
// environment of each command; nothing is ever written to a Git configuration by the fixture.
type repo struct {
	t    *testing.T
	dir  string
	tick int
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := &repo{t: t, dir: filepath.Join(root, "repo"), tick: 1_700_000_000}
	if err := os.Mkdir(r.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *repo) env() []string {
	r.tick += 60
	date := "@" + itoa(r.tick) + " +0000"
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + r.dir, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Fixture Owner", "GIT_AUTHOR_EMAIL=owner@example.org", "GIT_COMMITTER_NAME=Fixture Owner", "GIT_COMMITTER_EMAIL=owner@example.org",
		"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date, "GIT_EDITOR=:", "LC_ALL=C",
		// The fixture itself never starts background maintenance that would race a snapshot.
		"GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=gc.auto", "GIT_CONFIG_VALUE_0=0", "GIT_CONFIG_KEY_1=maintenance.auto", "GIT_CONFIG_VALUE_1=false",
	}
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for ; n > 0; n /= 10 {
		i--
		b[i] = byte('0' + n%10)
	}
	return string(b[i:])
}

func (r *repo) gitIn(dir string, extra []string, stdin string, args ...string) (string, int) {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(r.env(), extra...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			r.t.Fatal(err)
		}
	}
	return strings.TrimRight(out.String(), "\n"), code
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	out, code := r.gitIn(r.dir, nil, "", args...)
	if code != 0 {
		r.t.Fatalf("git %v: exit %d\n%s", args, code, out)
	}
	return out
}

func (r *repo) gitCode(args ...string) (string, int) { return r.gitIn(r.dir, nil, "", args...) }

func (r *repo) write(path, content string) {
	r.t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// commit writes the files, commits everything and returns the new commit id.
func (r *repo) commit(message string, files map[string]string) string {
	r.t.Helper()
	for path, content := range files {
		r.write(path, content)
	}
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", message)
	return r.oid("HEAD")
}

func (r *repo) oid(rev string) string { return r.git("rev-parse", "--verify", rev+"^{commit}") }

func (r *repo) checkout(args ...string) { r.git(append([]string{"checkout", "-q"}, args...)...) }

// scenario is the motivating case: the owner rebase-merged the parent branch into main, so main
// carries B1' and B2' and the child branch still carries B1 and B2 under old identities.
type scenario struct {
	*repo
	base, oldParent, oldHead, newParent string
	parentCommits, childCommits         []string
}

func rebaseMerged(t *testing.T) *scenario {
	return rebaseMergedWith(t, func(r *repo) {
		r.commit("B1", map[string]string{"parent1.txt": "p1\n"})
		r.commit("B2", map[string]string{"parent2.txt": "p2\n"})
	}, func(r *repo) {
		r.commit("C1", map[string]string{"child1.txt": "c1\n"})
		r.commit("C2", map[string]string{"child2.txt": "c2\n"})
	}, func(r *repo) { r.commit("M", map[string]string{"main.txt": "m\n"}) })
}

// rebaseMergedWith builds main: A; parent: A + parent(); child: parent + child(); then
// cherry-picks the parent commits onto main (the rebase-merge) and adds after() on main.
func rebaseMergedWith(t *testing.T, parent, child, after func(*repo)) *scenario {
	r := newRepo(t)
	s := &scenario{repo: r}
	r.commit("A", map[string]string{"README": "a\n", "shared.txt": "line1\nline2\nline3\n"})
	s.base = r.oid("HEAD")
	r.checkout("-b", "parent")
	parent(r)
	s.oldParent = r.oid("HEAD")
	s.parentCommits = strings.Fields(r.git("rev-list", "--reverse", s.base+".."+s.oldParent))
	r.checkout("-b", "child")
	child(r)
	s.oldHead = r.oid("HEAD")
	s.childCommits = strings.Fields(r.git("rev-list", "--reverse", s.oldParent+".."+s.oldHead))
	r.checkout("main")
	r.git(append([]string{"cherry-pick"}, s.parentCommits...)...)
	after(r)
	s.newParent = r.oid("HEAD")
	return s
}

func (s *scenario) options() Options {
	return Options{Path: s.dir, Branch: "refs/heads/child", OldParent: s.oldParent, NewParent: s.newParent, Remote: "origin", Identities: []string{ownerIdentity}}
}

func (s *scenario) plan(t *testing.T) Result {
	t.Helper()
	return Plan(context.Background(), s.options())
}

func firstCode(r Result) string {
	if len(r.Diagnostics) == 0 {
		return ""
	}
	return r.Diagnostics[0].Code
}

// oversizeRange is a branch with MaxCommits+1 commits above the old parent, written with one
// fast-import stream to keep the fixture quick.
func oversizeRange(t *testing.T) *scenario {
	return rebaseMergedWith(t, func(r *repo) { r.commit("B", map[string]string{"b": "b\n"}) }, func(r *repo) {
		var b strings.Builder
		b.WriteString("reset refs/heads/child\nfrom refs/heads/parent\n")
		for i := 0; i < MaxCommits+1; i++ {
			b.WriteString("commit refs/heads/child\ncommitter F <f@x> " + itoa(1_800_000_000+i) + " +0000\ndata 1\nc\nM 100644 inline f.txt\ndata " + itoa(len(itoa(i))+1) + "\n" + itoa(i) + "\n\n")
		}
		if out, code := r.gitIn(r.dir, nil, b.String(), "fast-import", "--quiet"); code != 0 {
			t.Fatal(out)
		}
		r.checkout("-f", "child")
	}, func(r *repo) { r.commit("M", map[string]string{"m": "m\n"}) })
}
