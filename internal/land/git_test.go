// SPDX-License-Identifier: EUPL-1.2

package land

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// gitFixture is a temporary repository with a bare remote named origin. The remote holds main and
// the pull request head refs refs/pull/N/head; nothing touches a real repository.
type gitFixture struct {
	t      *testing.T
	root   string
	work   string
	remote string
}

func (x *gitFixture) git(dir string, args ...string) string {
	x.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "commit.gpgsign=false", "-c", "protocol.file.allow=always"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(cleanEnv(), "HOME="+x.root, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME="+ownerName, "GIT_AUTHOR_EMAIL="+ownerEmail, "GIT_COMMITTER_NAME="+ownerName, "GIT_COMMITTER_EMAIL="+ownerEmail)
	out, err := cmd.CombinedOutput()
	if err != nil {
		x.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func newGitFixture(t *testing.T) *gitFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	x := &gitFixture{t: t, root: root, work: filepath.Join(root, "work"), remote: filepath.Join(root, "remote.git")}
	x.git(root, "init", "--bare", "-b", "main", x.remote)
	x.git(root, "init", "-b", "main", x.work)
	x.git(x.work, "remote", "add", "origin", x.remote)
	return x
}

// commit adds an empty commit and returns its id.
func (x *gitFixture) commit(msg string) string {
	x.t.Helper()
	x.git(x.work, "commit", "--allow-empty", "-m", msg)
	return x.git(x.work, "rev-parse", "HEAD")
}

func (x *gitFixture) push(ref, to string) { x.t.Helper(); x.git(x.work, "push", "origin", ref+":"+to) }

func (x *gitFixture) userRefs() []string {
	out := x.git(x.work, "for-each-ref", "--format=%(refname) %(objectname)")
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if l != "" && !strings.HasPrefix(l, PrivateRefPrefix) {
			keep = append(keep, l)
		}
	}
	sort.Strings(keep)
	return keep
}

// chain builds main = M and the pull requests 11, 12, 13 as commits on top of it, each head
// published as refs/pull/N/head; the local clone has none of the pull request commits.
func (x *gitFixture) chain() (m string, heads [3]string) {
	m = x.commit("base")
	x.push("HEAD", "refs/heads/main")
	for i := 0; i < 3; i++ {
		heads[i] = x.commit("part " + string(rune('1'+i)))
		x.push("HEAD", "refs/pull/"+string(rune('1'))+string(rune('1'+i))+"/head")
	}
	x.git(x.work, "reset", "--hard", m)
	x.git(x.work, "fetch", "origin", "refs/heads/main:refs/remotes/origin/main")
	os.Remove(filepath.Join(x.work, ".git", "FETCH_HEAD")) // the fixture's own fetch wrote it
	return m, heads
}

func newRunner(x *gitFixture) *GitRunner {
	g := NewGit(x.work)
	g.repoOverride = "o/r"
	return g
}

func TestGitDefaultTipAndFetchStayInThePrivateNamespace(t *testing.T) {
	x := newGitFixture(t)
	m, heads := x.chain()
	g := newRunner(x)
	ctx := context.Background()
	before := x.userRefs()

	tip, err := g.DefaultTip(ctx, "origin", "main")
	if err != nil || tip != m {
		t.Fatalf("tip %q %v want %q", tip, err, m)
	}
	if err := g.Fetch(ctx, "origin", "main", []int{11, 12, 13}); err != nil {
		t.Fatal(err)
	}
	if after := x.userRefs(); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("a user ref changed:\n%v\n%v", before, after)
	}
	if _, err := os.Stat(filepath.Join(x.work, ".git", "FETCH_HEAD")); err == nil {
		t.Fatal("FETCH_HEAD was written")
	}
	for i, n := range []int{11, 12, 13} {
		got, err := g.Resolve(ctx, n)
		if err != nil || got != heads[i] {
			t.Fatalf("resolve %d: %q %v", n, got, err)
		}
	}
	if got, err := g.Resolve(ctx, 99); err != nil || got != "" {
		t.Fatalf("absent ref: %q %v", got, err)
	}
	private := x.git(x.work, "for-each-ref", "--format=%(refname)", PrivateRefPrefix)
	if !strings.Contains(private, "refs/stw/land/base") || !strings.Contains(private, "refs/stw/land/pr-13") {
		t.Fatalf("private refs %q", private)
	}
}

func TestGitAncestryAndRange(t *testing.T) {
	x := newGitFixture(t)
	m, heads := x.chain()
	g := newRunner(x)
	ctx := context.Background()
	if err := g.Fetch(ctx, "origin", "main", []int{11, 12, 13}); err != nil {
		t.Fatal(err)
	}
	if v, _ := g.IsAncestor(ctx, m, heads[0]); v != AncestorYes {
		t.Fatalf("m ancestor of head 1: %v", v)
	}
	if v, _ := g.IsAncestor(ctx, heads[1], heads[0]); v != AncestorNo {
		t.Fatalf("head 2 is not an ancestor of head 1: %v", v)
	}
	if v, _ := g.IsAncestor(ctx, sha('f'), heads[0]); v != AncestorUnknown {
		t.Fatalf("absent object must be unknown (exit 128), got %v", v)
	}
	if v, _ := g.IsAncestor(ctx, "not-a-sha", heads[0]); v != AncestorUnknown {
		t.Fatalf("invalid id must be unknown, got %v", v)
	}
	commits, err := g.Range(ctx, m, heads[2], 10)
	if err != nil || len(commits) != 3 {
		t.Fatalf("range %v %v", commits, err)
	}
	if commits[0].AuthorName != ownerName || commits[0].CommitterEmail != ownerEmail {
		t.Fatalf("identity %+v", commits[0])
	}
	limited, err := g.Range(ctx, m, heads[2], 1)
	if err != nil || len(limited) != 2 {
		t.Fatalf("a limit of 1 returns 2 commits so that the caller sees the overflow: %d %v", len(limited), err)
	}
}

func TestGitRemoteRepoParsesGitHubURLsOnly(t *testing.T) {
	cases := map[string]string{
		"https://github.com/wstein/sourcesteward.git": "wstein/sourcesteward",
		"https://github.com/wstein/sourcesteward":     "wstein/sourcesteward",
		"git@github.com:wstein/sourcesteward.git":     "wstein/sourcesteward",
		"ssh://git@github.com/wstein/sourcesteward":   "wstein/sourcesteward",
		"https://evil.example/wstein/sourcesteward":   "",
		"https://github.com.evil.example/o/r":         "",
		"/tmp/some/remote.git":                        "",
		"https://github.com/only-owner":               "",
		"https://github.com/a/b/c":                    "",
	}
	for in, want := range cases {
		if got := parseGitHubURL(in, "github.com"); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestPlanOverRealGitAndFakeForge(t *testing.T) {
	x := newGitFixture(t)
	_, heads := x.chain()
	s := happy()
	s.opts.Chain = []Item{{11, heads[0]}, {12, heads[1]}, {13, heads[2]}}
	for i, n := range []int{11, 12, 13} {
		p := pull(n, heads[i], "feat")
		s.forge.pulls[n] = p
		s.forge.checks[heads[i]] = CheckFacts{Contexts: map[string]CheckState{"ci/all": CheckSuccess}}
		s.forge.reviews[n] = nil
	}
	s.git = nil
	before := x.userRefs()
	r := Plan(context.Background(), s.opts, humanMode, s.policy, s.forge, newRunner(x))
	if !r.Landable {
		t.Fatalf("reasons %v", reasonCodes(r))
	}
	if strings.Join(x.userRefs(), "\n") != strings.Join(before, "\n") {
		t.Fatal("the plan changed a user ref")
	}
	remoteMain := x.git(x.remote, "rev-parse", "refs/heads/main")
	if r.DefaultTip != remoteMain {
		t.Fatalf("default tip %s remote %s", r.DefaultTip, remoteMain)
	}
	// a default branch that moved is not a fast-forward
	x.git(x.work, "checkout", "-q", "main")
	x.git(x.work, "commit", "--allow-empty", "-m", "moved")
	x.push("HEAD", "refs/heads/main")
	r = Plan(context.Background(), s.opts, humanMode, s.policy, s.forge, newRunner(x))
	if r.Landable || !has(r, ReasonNotFastForward, 0) {
		t.Fatalf("moved default branch: %v", reasonCodes(r))
	}
}
