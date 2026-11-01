// SPDX-License-Identifier: EUPL-1.2
package carryover

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuardAllowsOnlyReadOnlyCommandsAndTheOneFetch(t *testing.T) {
	for _, args := range [][]string{
		{"cat-file", "-e", "x"}, {"diff", "a...b"}, {"patch-id", "--stable"}, {"rev-list", "--count", "a..b"}, {"range-diff", "a..b", "c..d"},
		{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", "origin", "+refs/pull/7/head:refs/stw/carry-over/pr-7"},
	} {
		if err := guard(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		nil, {"push", "origin", "main"}, {"update-ref", "refs/heads/main", "x"}, {"reset", "--hard"}, {"checkout", "main"}, {"gc"}, {"config", "user.name", "x"},
		{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", "origin", "+refs/pull/7/head:refs/heads/main"},
		{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", "origin", "+refs/heads/main:refs/stw/carry-over/pr-7"},
		{"fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--", "--upload-pack=evil", "+refs/pull/7/head:refs/stw/carry-over/pr-7"},
		{"fetch", "origin"},
	} {
		if err := guard(args); err == nil {
			t.Errorf("%v: allowed", args)
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
		if _, err := r.Count(ctx, bad, sha("1")); err == nil {
			t.Errorf("Count %q", bad)
		}
		if _, err := r.RangeDiff(ctx, sha("1"), sha("2"), sha("3"), bad); err == nil {
			t.Errorf("RangeDiff %q", bad)
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
