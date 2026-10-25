// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func snapshot(t *testing.T, w *workspace, name string) string {
	t.Helper()
	dir := w.dir(name)
	files := snapshotTree(t, dir)
	var keys []string
	for k, v := range files {
		if !strings.Contains(k, "/.git") && !strings.HasPrefix(k, "/.git") {
			keys = append(keys, k+"="+v)
		}
	}
	sortStrings(keys)
	return worktreeState(t, dir) + "\n" + strings.Join(keys, "\n") + "\n" + runGit(t, dir, "for-each-ref", "refs/heads") + runGit(t, dir, "stash", "list")
}

func TestSyncCoversTheRootAndEverySubmoduleOnABranch(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, ".", "main")
	w.advance(t, "a", "main")
	w.advance(t, "a", "main")
	w.advance(t, "c", "main")
	// other branches and a linked worktree are outside the scope and stay untouched
	runGit(t, w.ws, "push", "-q", "origin", "main:topic")
	runGit(t, w.ws, "branch", "--track", "topic", "origin/topic")
	w.advance(t, ".", "topic")
	linked := filepath.Join(w.root, "linked")
	runGit(t, w.ws, "worktree", "add", "-q", linked, "topic")
	topic, linkedState := w.oid(t, ".", "topic"), runGit(t, linked, "rev-parse", "HEAD")
	oldRoot, oldA, oldB, oldC := w.oid(t, ".", "HEAD"), w.oid(t, "a", "HEAD"), w.oid(t, "b", "HEAD"), w.oid(t, "c", "HEAD")

	r := Sync(context.Background(), Options{Path: w.ws})
	if r.Error != nil || r.Mode != "sync" || r.Command != "sync" || r.RunID == nil {
		t.Fatalf("%+v", r)
	}
	if len(r.Repos) != 6 {
		t.Fatalf("one entry per repository, no branches or worktrees: %+v", r.Repos)
	}
	got := repoByName(t, r)
	if g := got["."]; g.Result != ResultFastForwarded || g.Kind != "root" || *g.Branch != "main" || *g.OldOID != oldRoot || *g.NewOID != w.oid(t, ".", "HEAD") || g.Behind != 1 {
		t.Errorf("root: %+v", g)
	}
	if g := got["a"]; g.Result != ResultFastForwarded || g.Kind != "submodule" || *g.OldOID != oldA || g.Behind != 2 || w.oid(t, "a", "HEAD") == oldA || w.oid(t, "a", "main") != *g.NewOID {
		t.Errorf("a: %+v", g)
	}
	if g := got["b"]; g.Result != ResultUpToDate || w.oid(t, "b", "HEAD") != oldB {
		t.Errorf("b: %+v", g)
	}
	if g := got["c"]; g.Result != ResultSkippedDetached || g.Branch != nil || w.oid(t, "c", "HEAD") != oldC {
		t.Errorf("c is detached and only reported: %+v", g)
	}
	if w.oid(t, ".", "topic") != topic || runGit(t, linked, "rev-parse", "HEAD") != linkedState {
		t.Error("another branch or a linked worktree was touched")
	}
	if r.ExitCode() != 0 {
		t.Errorf("a successful run exits 0, got %d", r.ExitCode())
	}
	if r.Summary.Total != 6 || r.Summary.ByResult[ResultFastForwarded] != 2 {
		t.Errorf("%+v", r.Summary)
	}
	// a second run has nothing to do
	r = Sync(context.Background(), Options{Path: w.ws})
	if r.RunID != nil || repoByName(t, r)["."].Result != ResultUpToDate || repoByName(t, r)["a"].Result != ResultUpToDate {
		t.Errorf("%+v", r)
	}
}

func TestSyncPlanChangesOnlyRemoteTrackingRefs(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, ".", "main")
	w.advance(t, "a", "main")
	before := map[string]string{}
	for _, n := range []string{".", "a", "b", "c"} {
		before[n] = snapshot(t, w, n)
	}
	r := Sync(context.Background(), Options{Path: w.ws, Plan: true})
	if r.Error != nil || r.Mode != "plan" || r.RunID != nil {
		t.Fatalf("%+v", r)
	}
	got := repoByName(t, r)
	if got["."].Result != ResultFastForward || got["a"].Result != ResultFastForward || got["."].NewOID == nil {
		t.Fatalf("%+v", got)
	}
	for n, s := range before {
		if snapshot(t, w, n) != s {
			t.Errorf("%s changed by a plan", n)
		}
	}
	if _, err := os.Stat(filepath.Join(w.ws, ".git", "stw")); !os.IsNotExist(err) {
		t.Error("a plan must not journal")
	}
	runGit(t, w.ws, "rev-parse", "--verify", "refs/remotes/origin/main")
	if runGit(t, w.ws, "rev-parse", "origin/main") == w.oid(t, ".", "HEAD") {
		t.Error("the plan must fetch the remote-tracking ref")
	}
}

func TestSyncSkipsDirtyDivergedAheadAndUnconfiguredRepos(t *testing.T) {
	w := newWorkspace(t)
	// root: dirty in a file that the update also changes
	w.advanceFile(t, ".", "main", "ws.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "edited\n")
	runGit(t, w.ws, "add", "ws.txt")
	write(t, filepath.Join(w.ws, "ws.txt"), "edited again on top of the staged edit\n")
	write(t, filepath.Join(w.ws, "untracked.txt"), "u\n")
	// a: diverged 1 ahead 2 behind
	commit(t, w.dir("a"), "local.txt", "l\n")
	w.advance(t, "a", "main")
	w.advance(t, "a", "main")
	// b: only ahead
	commit(t, w.dir("b"), "local.txt", "l\n")
	before := map[string]string{}
	for _, n := range []string{".", "a", "b", "c"} {
		before[n] = snapshot(t, w, n)
	}
	r := Sync(context.Background(), Options{Path: w.ws})
	got := repoByName(t, r)
	if g := got["."]; g.Result != ResultSkippedDirty || g.Reason == nil {
		t.Errorf("root: %+v", g)
	}
	if g := got["a"]; g.Result != ResultDiverged || g.Ahead != 1 || g.Behind != 2 || g.Reason == nil {
		t.Errorf("a: %+v", g)
	}
	if g := got["b"]; g.Result != ResultAhead || g.Ahead != 1 {
		t.Errorf("b: %+v", g)
	}
	for n, s := range before {
		if n != "c" && snapshot(t, w, n) != s {
			t.Errorf("%s changed", n)
		}
	}
	if r.ExitCode() != 0 {
		t.Errorf("skips are not failures: exit %d", r.ExitCode())
	}
	if n := runGit(t, w.dir("a"), "rev-list", "--merges", "--count", "HEAD"); n != "0" {
		t.Errorf("merge commit created: %s", n)
	}
}

func TestSyncSkipsNoUpstreamAndUninitializedSubmodules(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.dir("b"), "branch", "--unset-upstream")
	runGit(t, w.ws, "submodule", "deinit", "-q", "-f", "a")
	got := repoByName(t, Sync(context.Background(), Options{Path: w.ws}))
	if got["b"].Result != ResultSkippedNoUpstream || got["a"].Result != ResultSkippedUninitialized {
		t.Fatalf("%+v %+v", got["b"], got["a"])
	}
}

func TestSyncSkipsBusyRepos(t *testing.T) {
	w := newWorkspace(t)
	w.advance(t, "b", "main")
	w.advance(t, ".", "main")
	// b: a merge is in progress
	commit(t, w.dir("b"), "b.txt", "local\n")
	runGit(t, w.dir("b"), "checkout", "-q", "-b", "other", "HEAD~1")
	commit(t, w.dir("b"), "b.txt", "other\n")
	runGit(t, w.dir("b"), "checkout", "-q", "main")
	_ = gitCmd(w.dir("b"), "merge", "other").Run()
	// root: a bisect is in progress
	runGit(t, w.ws, "bisect", "start")
	runGit(t, w.ws, "bisect", "bad", "HEAD")
	runGit(t, w.ws, "bisect", "good", runGit(t, w.ws, "rev-list", "--max-parents=0", "HEAD"))
	before := map[string]string{}
	for _, n := range []string{".", "b"} {
		before[n] = snapshot(t, w, n)
	}
	got := repoByName(t, Sync(context.Background(), Options{Path: w.ws}))
	for _, n := range []string{".", "b"} {
		if got[n].Result != ResultSkippedBusy {
			t.Errorf("%s: %+v", n, got[n])
		}
		if snapshot(t, w, n) != before[n] {
			t.Errorf("%s changed", n)
		}
	}
}

func TestSyncRefusesNonGitAndJJPaths(t *testing.T) {
	r := Sync(context.Background(), Options{Path: t.TempDir()})
	if r.Error == nil || r.Error.Code != "sync-unsupported-backend" || len(r.Repos) != 0 || r.ExitCode() != 1 {
		t.Fatalf("%+v", r)
	}
}

// busyStates starts a real Git operation that stops in the repository at dir and leaves it in progress.
var busyStates = map[string]func(t *testing.T, dir string){
	"merge": func(t *testing.T, dir string) {
		conflictBranch(t, dir)
		_ = gitCmd(dir, "merge", "other").Run()
	},
	"cherry-pick": func(t *testing.T, dir string) {
		conflictBranch(t, dir)
		_ = gitCmd(dir, "cherry-pick", "other").Run()
	},
	"revert": func(t *testing.T, dir string) {
		commit(t, dir, "busy.txt", "first\n")
		commit(t, dir, "busy.txt", "second\n")
		_ = gitCmd(dir, "revert", "--no-edit", "HEAD~1").Run()
	},
	"rebase-merge": func(t *testing.T, dir string) {
		conflictBranch(t, dir)
		_ = gitCmd(dir, "rebase", "other").Run()
	},
	"rebase-apply": func(t *testing.T, dir string) {
		conflictBranch(t, dir)
		patch := filepath.Join(dir, "..", filepath.Base(dir)+".patch")
		out := runGit(t, dir, "format-patch", "-1", "other", "--stdout")
		write(t, patch, out+"\n")
		_ = gitCmd(dir, "am", patch).Run()
	},
	"bisect": func(t *testing.T, dir string) {
		for i := 0; i < 4; i++ {
			commit(t, dir, "bisect"+itoa(i)+".txt", "x\n")
		}
		runGit(t, dir, "bisect", "start")
		runGit(t, dir, "bisect", "bad", "HEAD")
		runGit(t, dir, "bisect", "good", runGit(t, dir, "rev-list", "--max-parents=0", "HEAD"))
	},
}

// conflictBranch leaves a branch "other" that conflicts with the tip of the current branch on one file.
func conflictBranch(t *testing.T, dir string) {
	t.Helper()
	commit(t, dir, "busy.txt", "main side\n")
	runGit(t, dir, "checkout", "-q", "-b", "other", "HEAD~1")
	commit(t, dir, "busy.txt", "other side\n")
	runGit(t, dir, "checkout", "-q", "main")
}

// An operation in progress makes the repository skipped-busy, in a plan and in a run, in the root and in a submodule:
// the branch, the worktree and the Git directory are byte-identical afterwards and the exit status is 0.
func TestSkippedBusyForEveryOperationInPlanAndRun(t *testing.T) {
	for op, start := range busyStates {
		for _, repo := range []string{".", "b"} {
			for _, plan := range []bool{true, false} {
				name := op + "/" + repo + "/" + map[bool]string{true: "plan", false: "run"}[plan]
				t.Run(name, func(t *testing.T) {
					w := newWorkspace(t)
					w.advance(t, repo, "main")
					start(t, w.dir(repo))
					before := snapshot(t, w, repo)
					head := w.oid(t, repo, "HEAD")
					r := Sync(context.Background(), Options{Path: w.ws, Plan: plan})
					if r.Error != nil {
						t.Fatal(r.Error)
					}
					g := repoByName(t, r)[repo]
					if g.Result != ResultSkippedBusy || g.Reason == nil || r.ExitCode() != 0 {
						t.Fatalf("%+v exit %d", g, r.ExitCode())
					}
					if snapshot(t, w, repo) != before || w.oid(t, repo, "HEAD") != head {
						t.Error("the repository changed")
					}
					if r.RunID != nil {
						t.Errorf("a skipped repository was journaled: %v", *r.RunID)
					}
				})
			}
		}
	}
}
