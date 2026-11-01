// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardAllowsOnlyTheReviewedVectors(t *testing.T) {
	fetch := append(append([]string{}, fetchFlags...), "origin", "+refs/pull/7/head:refs/stw/carry-over/pr-7")
	logv := []string{"log", "--reverse", "--no-merges", "--no-color", "--no-show-signature", "--no-ext-diff", "--no-textconv", "--no-renames", "-p", "--format=%x00%B%x00", "--end-of-options", sha("1") + ".." + sha("2")}
	for _, args := range [][]string{
		{"rev-parse", "--verify", "--quiet", sha("1") + "^{commit}"},
		{"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", sha("1") + "..." + sha("2"), "--"},
		{"patch-id", "--stable"},
		{"rev-list", "--merges", "--end-of-options", sha("1") + ".." + sha("2")},
		{"rev-list", "--no-walk", "--end-of-options", PullRef(7)},
		logv, fetch,
	} {
		if err := guard(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	without := func(v []string, drop string) []string {
		var out []string
		for _, a := range v {
			if a != drop {
				out = append(out, a)
			}
		}
		return out
	}
	replace := func(v []string, from, to string) []string {
		out := append([]string{}, v...)
		for i, a := range out {
			if a == from {
				out[i] = to
			}
		}
		return out
	}
	for name, args := range map[string][]string{
		"empty": nil, "push": {"push", "origin", "main"}, "update-ref": {"update-ref", "refs/heads/main", "x"}, "reset": {"reset", "--hard"},
		"checkout": {"checkout", "main"}, "gc": {"gc"}, "config": {"config", "user.name", "x"}, "range-diff": {"range-diff", sha("1") + ".." + sha("2"), sha("3") + ".." + sha("4")},
		"fetch with tags":       replace(fetch, "--no-tags", "--tags"),
		"fetch without refmap":  without(fetch, "--refmap="),
		"fetch without no-tags": without(fetch, "--no-tags"),
		"fetch into a branch":   replace(fetch, "+refs/pull/7/head:refs/stw/carry-over/pr-7", "+refs/pull/7/head:refs/heads/main"),
		"fetch of a branch":     replace(fetch, "+refs/pull/7/head:refs/stw/carry-over/pr-7", "+refs/heads/main:refs/stw/carry-over/pr-7"),
		"fetch option remote":   replace(fetch, "origin", "--upload-pack=evil"),
		"fetch short":           {"fetch", "origin"},
		"log without textconv":  without(logv, "--no-textconv"),
		"log without ext diff":  without(logv, "--no-ext-diff"),
		"log without signature": without(logv, "--no-show-signature"),
		"log with a ref":        replace(logv, sha("1")+".."+sha("2"), "main..feature"),
		"diff without textconv": {"diff", "--no-color", "--no-ext-diff", "--no-renames", sha("1") + "..." + sha("2"), "--"},
		"diff without ext":      {"diff", "--no-color", "--no-textconv", "--no-renames", sha("1") + "..." + sha("2"), "--"},
		"diff with a ref":       {"diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "main...feature", "--"},
		"patch-id unstable":     {"patch-id"},
		"rev-list outside":      {"rev-list", "--no-walk", "--end-of-options", "refs/heads/main"},
	} {
		if err := guard(args); err == nil {
			t.Errorf("%s: allowed %v", name, args)
		}
	}
}

func TestRunnerRefusesInvalidIDsBeforeRunningGit(t *testing.T) {
	r := &Runner{Bin: "/nonexistent/git-must-not-run", Dir: t.TempDir()}
	ctx := t.Context()
	for _, bad := range []string{"-x", "a..b", "a b", "main", ""} {
		if _, err := r.HasCommit(ctx, bad); err == nil {
			t.Errorf("HasCommit %q", bad)
		}
		if _, err := r.PatchID(ctx, bad, sha("1")); err == nil {
			t.Errorf("PatchID %q", bad)
		}
		if _, err := r.Merges(ctx, sha("1"), bad); err == nil {
			t.Errorf("Merges %q", bad)
		}
		if _, err := r.Commits(ctx, bad, sha("1")); err == nil {
			t.Errorf("Commits %q", bad)
		}
	}
	for _, remote := range []string{"-u", "a b", "x..y", ""} {
		if _, err := r.FetchPull(ctx, remote, 1); err == nil {
			t.Errorf("FetchPull %q", remote)
		}
	}
	if _, err := r.FetchPull(ctx, "origin", 0); err == nil {
		t.Error("FetchPull 0")
	}
}

// remoteWithPull creates a bare remote whose refs/pull/7/head is tip, and wires it as "origin".
func remoteWithPull(t *testing.T, s *stack, tip string) {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "remote.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s.r.git("push", "-q", bare, tip+":refs/pull/7/head")
	s.r.git("tag", "-f", "v-remote", tip)
	s.r.git("push", "-q", "-f", bare, "refs/tags/v-remote")
	s.r.git("tag", "-d", "v-remote")
	s.r.git("remote", "add", "origin", bare)
}

func TestFetchWritesOnlyThePrivateRef(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	remoteWithPull(t, s, head)
	// Detach the work from the local branch so that the new head exists only on the remote.
	s.r.git("checkout", "-q", "main")
	s.r.git("branch", "-q", "-D", "new")
	s.r.git("gc", "-q", "--prune=now")
	before := s.r.git("for-each-ref", "--format=%(refname)")
	res := Check(t.Context(), NewRunner(s.r.dir), Request{PR: 7, OldBase: s.m1, OldHead: s.c2, NewBase: s.m2, NewHead: head, Remote: "origin"})
	wantReasons(t, res)
	if res.PRHead.State != StateMatch {
		t.Fatalf("%+v", res.PRHead)
	}
	after := s.r.git("for-each-ref", "--format=%(refname)")
	if after != before+"\n"+PullRef(7) && !(before == "" && after == PullRef(7)) {
		t.Fatalf("refs before:\n%s\nafter:\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(s.r.dir, ".git", "FETCH_HEAD")); err == nil {
		t.Fatal("FETCH_HEAD was written")
	}
	if tags := s.r.git("tag", "--list"); tags != "" {
		t.Fatalf("tags: %s", tags)
	}
}

func TestMovedPullHeadRefuses(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	remoteWithPull(t, s, s.c2) // the pull request head is still the old head
	res := Check(t.Context(), NewRunner(s.r.dir), Request{PR: 7, OldBase: s.m1, OldHead: s.c2, NewBase: s.m2, NewHead: head, Remote: "origin"})
	wantReasons(t, res, ReasonHeadMismatch)
}

func TestUnreachableRemoteIsUnknown(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	res := Check(t.Context(), NewRunner(s.r.dir), Request{PR: 7, OldBase: s.m1, OldHead: s.c2, NewBase: s.m2, NewHead: head, Remote: "nowhere"})
	if res.CarryOver || res.PRHead.State != StateUnknown || !strings.Contains(strings.Join(res.Reasons, ","), ReasonReadFailed) {
		t.Fatalf("%+v", res)
	}
}

// pinnedPatchID is the patch-id of the diff as the rule defines it, computed by the test with the same flags.
func pinnedPatchID(t *testing.T, r *repo, base, head string) string {
	t.Helper()
	diff := exec.Command("git", "-C", r.dir, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", base+"..."+head, "--")
	raw, err := diff.Output()
	if err != nil {
		t.Fatal(err)
	}
	pid := exec.Command("git", "-C", r.dir, "patch-id", "--stable")
	pid.Stdin = strings.NewReader(string(raw))
	out, err := pid.Output()
	if err != nil {
		t.Fatal(err)
	}
	id, _, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	return id
}

func TestPatchIDIgnoresRenameDetection(t *testing.T) {
	s := newStack(t)
	s.r.git("checkout", "-q", "-b", "renamed", "main")
	s.r.git("mv", "b.txt", "d.txt")
	s.r.git("commit", "-q", "-m", "rename")
	head := s.r.head()
	got, err := NewRunner(s.r.dir).PatchID(t.Context(), s.m2, head)
	if err != nil {
		t.Fatal(err)
	}
	if want := pinnedPatchID(t, s.r, s.m2, head); got != want {
		t.Fatalf("patch-id %s, want %s (the diff is taken without rename detection)", got, want)
	}
}

func TestPatchIDSeesBinaryContent(t *testing.T) {
	r := newRepo(t)
	r.commit("a.txt", "a\n", "base")
	base := r.head()
	r.git("checkout", "-q", "-b", "one")
	r.write("blob.bin", "\x00\x01\x02")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "bin one")
	one := r.head()
	r.git("checkout", "-q", "-b", "two", base)
	r.write("blob.bin", "\x00\x01\x03")
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "bin two")
	res := Check(t.Context(), NewRunner(r.dir), Request{PR: 1, OldBase: base, OldHead: one, NewBase: base, NewHead: r.head()})
	if res.PatchID.State != StateDiffers {
		t.Fatalf("different binary content must change the patch-id: %+v", res)
	}
}

// marker returns an executable script that appends a line to a marker file, plus the file's path.
func marker(t *testing.T, body string) (script, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "ran")
	script = filepath.Join(dir, "driver.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ran >> '"+log+"'\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, log
}

func ran(t *testing.T, log string) int {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "ran")
}

func TestNoDiffDriverRunsAndAWhitespaceChangeStaysNo(t *testing.T) {
	s := newStack(t)
	script, log := marker(t, `tr -d ' ' < "$1"`)
	s.r.git("config", "diff.strip.textconv", script)
	s.r.git("config", "diff.external", script)
	// info/attributes is not covered by an attribute source; the flags must keep the driver away.
	if err := os.WriteFile(filepath.Join(s.r.dir, ".git", "info", "attributes"), []byte("*.txt diff=strip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.r.git("checkout", "-q", "-b", "new", "main")
	s.r.git("cherry-pick", s.c1)
	s.r.write("a.txt", strings.Replace(c1File, "TWO", "TWO ", 1))
	s.r.git("commit", "-q", "-a", "--amend", "--no-edit")
	s.r.git("cherry-pick", s.c2)
	res := s.check(t, s.m2, s.r.head())
	wantReasons(t, res, ReasonRangeDiff)
	if res.PatchID.State != StateEqual {
		t.Fatalf("the patch-id ignores whitespace: %+v", res.PatchID)
	}
	if n := ran(t, log); n != 0 {
		t.Fatalf("a diff driver ran %d times", n)
	}
	// Sanity: the driver is wired, so a plain git diff does start it.
	s.r.git("diff", s.m1, s.c2)
	if ran(t, log) == 0 {
		t.Fatal("the marker driver is not wired; the test proves nothing")
	}
}

func TestFetchWritesNoConfiguredRefmapRef(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	remoteWithPull(t, s, head)
	s.r.git("config", "remote.origin.fetch", "+refs/pull/*/head:refs/remotes/origin/pr/*")
	s.r.git("config", "--add", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	if _, err := NewRunner(s.r.dir).FetchPull(t.Context(), "origin", 7); err != nil {
		t.Fatal(err)
	}
	if refs := s.r.git("for-each-ref", "--format=%(refname)", "refs/remotes"); refs != "" {
		t.Fatalf("the configured refmap wrote refs:\n%s", refs)
	}
	if got := s.r.git("rev-parse", PullRef(7)); got != head {
		t.Fatalf("private ref %s, want %s", got, head)
	}
}

func TestNoHookRunsDuringTheFetch(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	remoteWithPull(t, s, head)
	script, log := marker(t, "exit 0")
	hook := filepath.Join(s.r.dir, ".git", "hooks", "reference-transaction")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(script, hook); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRunner(s.r.dir).FetchPull(t.Context(), "origin", 7); err != nil {
		t.Fatal(err)
	}
	if n := ran(t, log); n != 0 {
		t.Fatalf("the reference-transaction hook ran %d times", n)
	}
	// Sanity: the hook fires for an ordinary ref update.
	s.r.git("update-ref", "refs/heads/probe", head)
	if ran(t, log) == 0 {
		t.Fatal("the hook is not wired; the test proves nothing")
	}
}

func TestInheritedGitDirDoesNotRedirectTheRunner(t *testing.T) {
	s := newStack(t)
	head := s.rebased("new")
	other := newRepo(t)
	other.commit("x.txt", "x\n", "other")
	t.Setenv("GIT_DIR", filepath.Join(other.dir, ".git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other.dir, "index-elsewhere"))
	wantReasons(t, s.check(t, s.m2, head))
}

func TestCheckoutOutsideARepositoryIsAReadFailure(t *testing.T) {
	res := Check(t.Context(), NewRunner(t.TempDir()), req)
	wantReasons(t, res, ReasonReadFailed)
	if res.CarryOver {
		t.Fatal("carry-over without a repository")
	}
}
