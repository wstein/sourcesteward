// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunFastForwardsEveryWorktreeAndNonCheckedOutBranch(t *testing.T) {
	f := classificationFixture(t)
	wt := f.worktree(t, "behind") // linked worktree, clean
	before := map[string]string{}
	for _, b := range []string{"main", "behind", "equal", "ahead", "diverged", "dirty", "untracked-files", "gone", "local-only"} {
		before[b] = f.oid(t, b)
	}
	upstream := func(b string) string { return runGit(t, f.bare, "rev-parse", "refs/heads/"+b) }

	r := Run(context.Background(), Options{Path: f.repo, Prune: true})
	if r.Error != nil || r.Mode != "sync" || r.RunID == nil || r.Command != "sync" {
		t.Fatalf("%+v", r)
	}
	got := itemsByBranch(t, r)
	for _, b := range []string{"main", "behind", "dirty", "untracked-files"} {
		it := got[b]
		if it.Result != ResultFastForwarded || *it.OldOID != before[b] || it.NewOID == nil || *it.NewOID != upstream(b) || f.oid(t, b) != upstream(b) {
			t.Errorf("%s: %+v (ref %s)", b, it, f.oid(t, b))
		}
	}
	if got["main"].Worktree == nil || *got["main"].Worktree != f.repo || got["behind"].Worktree == nil || *got["behind"].Worktree != wt {
		t.Errorf("worktrees: %+v %+v", got["main"], got["behind"])
	}
	// the checked-out branches moved their worktrees as well
	if _, err := os.Stat(filepath.Join(wt, "behind-remote.txt")); err != nil {
		t.Errorf("the linked worktree was not fast-forwarded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.repo, "main-remote.txt")); err != nil {
		t.Errorf("the primary worktree was not fast-forwarded: %v", err)
	}
	for b, want := range map[string]string{"equal": ResultUpToDate, "ahead": ResultAhead, "diverged": ResultDiverged, "gone": ResultUpstreamGone} {
		if got[b].Result != want || f.oid(t, b) != before[b] || got[b].NewOID != nil {
			t.Errorf("%s: %+v", b, got[b])
		}
	}
	if f.oid(t, "local-only") != before["local-only"] || r.Summary.Untracked != 1 {
		t.Error("a branch without an upstream was touched or listed")
	}
	if r.Summary.ByResult[ResultFastForwarded] != 4 || r.Summary.Total != len(r.Items) {
		t.Errorf("summary: %+v", r.Summary)
	}
	if r.ExitCode() != 1 { // diverged needs the owner
		t.Errorf("exit %d", r.ExitCode())
	}
}

func TestRunSecondRunChangesNothing(t *testing.T) {
	f := newFixture(t)
	f.advanceRemote(t, "main")
	Run(context.Background(), Options{Path: f.repo})
	r := Run(context.Background(), Options{Path: f.repo})
	if it := itemsByBranch(t, r)["main"]; it.Result != ResultUpToDate || r.ExitCode() != 0 {
		t.Fatalf("%+v", r)
	}
}

// TestRunItemFailuresAreIsolated: a hook that makes Git exit non-zero for one branch fails only that item.
func TestRunItemFailureDoesNotStopTheOthers(t *testing.T) {
	f := newFixture(t)
	f.branchOnRemote(t, "a")
	f.branchOnRemote(t, "b")
	f.advanceRemote(t, "a")
	f.advanceRemote(t, "b")
	oldA, oldB := f.oid(t, "a"), f.oid(t, "b")
	hook := "#!/bin/sh\n[ \"$1\" = prepared ] || exit 0\nwhile read old new ref; do [ \"$ref\" = refs/heads/a ] && exit 1; done\nexit 0\n"
	write(t, filepath.Join(f.repo, ".git", "hooks", "reference-transaction"), hook)
	if err := os.Chmod(filepath.Join(f.repo, ".git", "hooks", "reference-transaction"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := Run(context.Background(), Options{Path: f.repo})
	got := itemsByBranch(t, r)
	if got["a"].Result != ResultFailed || got["a"].Reason == nil || f.oid(t, "a") != oldA {
		t.Errorf("a: %+v", got["a"])
	}
	if got["b"].Result != ResultFastForwarded || f.oid(t, "b") == oldB {
		t.Errorf("b: %+v", got["b"])
	}
	if r.ExitCode() != 1 {
		t.Errorf("a failed item must exit 1")
	}
}

// TestRunLostRaceIsFailedAndNeverRepaired moves the branch between classification and apply.
func TestRunLostRaceIsFailedAndNeverRepaired(t *testing.T) {
	f := newFixture(t)
	f.branchOnRemote(t, "side")
	f.branchOnRemote(t, "other")
	f.advanceRemote(t, "side")
	f.advanceRemote(t, "other")
	f.advanceRemote(t, "main")
	oldOther := f.oid(t, "other")
	var moved string
	applyHook = func(ref string) {
		switch ref {
		case "refs/heads/side": // not checked out: a concurrent writer moves the ref
			runGit(t, f.repo, "update-ref", ref, f.oid(t, "main"))
			moved = f.oid(t, "side")
		case "refs/heads/main": // checked out: a concurrent commit moves HEAD
			commit(t, f.repo, "racer.txt", "x\n")
		}
	}
	t.Cleanup(func() { applyHook = nil })
	racerBase := f.oid(t, "main")
	r := Run(context.Background(), Options{Path: f.repo})
	got := itemsByBranch(t, r)
	if got["side"].Result != ResultFailed || got["side"].Reason == nil || *got["side"].Reason != "raced" || f.oid(t, "side") != moved {
		t.Errorf("side: %+v", got["side"])
	}
	if got["main"].Result != ResultFailed || got["main"].Reason == nil || *got["main"].Reason != "head-moved" {
		t.Errorf("main: %+v", got["main"])
	}
	if runGit(t, f.repo, "rev-parse", "main~1") != racerBase {
		t.Errorf("the racing commit was repaired away")
	}
	if got["other"].Result != ResultFastForwarded || f.oid(t, "other") == oldOther {
		t.Errorf("other: %+v", got["other"])
	}
}

// TestApplyNeverLosesOrMergesCommits drives the apply step with forged classifications, as a mutant
// of the classifier would produce them: the apply step must refuse anything but a pure fast-forward.
func TestApplyNeverLosesOrMergesCommits(t *testing.T) {
	f := classificationFixture(t)
	ctx := context.Background()
	r, e := plan(ctx, Options{Path: f.repo})
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	upstream := runGit(t, f.bare, "rev-parse", "refs/heads/diverged")
	for _, tc := range []struct{ branch, tree string }{{"diverged", ""}, {"ahead", ""}} {
		old := f.oid(t, tc.branch)
		up := upstream
		if tc.branch == "ahead" {
			up = runGit(t, f.bare, "rev-parse", "refs/heads/ahead")
		}
		it := Item{Branch: tc.branch, Result: ResultFastForward, OldOID: ptr(old), NewOID: ptr(up)}
		out := e.applyItem(ctx, newTestJournal(t, e), it)
		if out.Result != ResultFailed || f.oid(t, tc.branch) != old {
			t.Errorf("%s (not checked out): %+v ref %s", tc.branch, out, f.oid(t, tc.branch))
		}
	}
	// checked out: the same forged item in a worktree
	tree := f.worktree(t, "diverged")
	old := f.oid(t, "diverged")
	it := Item{Branch: "diverged", Worktree: &tree, Result: ResultFastForward, OldOID: ptr(old), NewOID: ptr(upstream)}
	out := e.applyItem(ctx, newTestJournal(t, e), it)
	if out.Result != ResultFailed || f.oid(t, "diverged") != old {
		t.Errorf("checked out: %+v", out)
	}
	for _, b := range []string{"main", "behind", "diverged", "ahead"} {
		if n := runGit(t, f.repo, "rev-list", "--merges", "--count", b); n != "0" {
			t.Errorf("%s has %s merge commits", b, n)
		}
	}
	_ = r
}

// TestRunKeepsEveryLocalCommit: all commits reachable from any local branch before the run are
// reachable from the same branch afterwards, and no branch gained a merge commit.
func TestRunKeepsEveryLocalCommitAndCreatesNoMerge(t *testing.T) {
	f := classificationFixture(t)
	f.worktree(t, "behind")
	tips := map[string]string{}
	for _, b := range []string{"main", "behind", "ahead", "diverged", "equal"} {
		tips[b] = f.oid(t, b)
	}
	Run(context.Background(), Options{Path: f.repo, Prune: true})
	for b, tip := range tips {
		if out := runGit(t, f.repo, "rev-list", "--count", tip+".."+b); out != "" && false {
			t.Fatal(out)
		}
		if err := gitCmd(f.repo, "merge-base", "--is-ancestor", tip, b).Run(); err != nil {
			t.Errorf("%s lost commits: old tip %s is not an ancestor", b, tip)
		}
		if n := runGit(t, f.repo, "rev-list", "--merges", "--count", b); n != "0" {
			t.Errorf("%s has %s merge commits", b, n)
		}
	}
}

func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, err := os.ReadFile(p)
		out[strings.TrimPrefix(p, dir)] = string(raw) + "|" + info.Mode().String()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRunLeavesADirtyWorktreeByteIdentical(t *testing.T) {
	f := classificationFixture(t)
	dirty := f.worktree(t, "dirty")
	write(t, filepath.Join(dirty, "a.txt"), "edited tracked\n")
	runGit(t, dirty, "add", "a.txt") // staged
	write(t, filepath.Join(dirty, "u.txt"), "untracked\n")
	write(t, filepath.Join(dirty, "b.txt"), "second\n")
	runGit(t, dirty, "add", "b.txt")
	write(t, filepath.Join(dirty, "b.txt"), "second, unstaged on top\n")
	before := snapshotTree(t, dirty)
	index := runGit(t, dirty, "ls-files", "-s")
	head, stashes := runGit(t, dirty, "rev-parse", "HEAD"), runGit(t, f.repo, "stash", "list")

	r := Run(context.Background(), Options{Path: f.repo})
	it := itemsByBranch(t, r)["dirty"]
	if it.Result != ResultSkippedDirty || it.Reason == nil || f.oid(t, "dirty") != head {
		t.Fatalf("%+v", it)
	}
	after := snapshotTree(t, dirty)
	for k, v := range before {
		if k != "/.git" && after[k] != v {
			t.Errorf("%s changed", k)
		}
	}
	if len(after) != len(before) || runGit(t, dirty, "ls-files", "-s") != index || runGit(t, f.repo, "stash", "list") != stashes {
		t.Error("index, file set or stash changed")
	}
	if r.ExitCode() != 1 {
		t.Error("skipped-dirty exits 1")
	}
}

func TestRunPruneReportsRemovedRefsAndNeverDeletesLocalBranches(t *testing.T) {
	f := classificationFixture(t)
	r := Run(context.Background(), Options{Path: f.repo, Prune: true})
	if r.Fetch.PrunedTotal != 1 || len(r.Fetch.PrunedRefs) != 1 || r.Fetch.PrunedRefs[0] != "refs/remotes/origin/gone" {
		t.Fatalf("%+v", r.Fetch)
	}
	if runGit(t, f.repo, "rev-parse", "--verify", "refs/heads/gone") == "" || itemsByBranch(t, r)["gone"].Result != ResultUpstreamGone {
		t.Fatal("the local branch of a pruned upstream must be kept and reported")
	}
}

func TestRunWithoutPruneRemovesNoRemoteTrackingRef(t *testing.T) {
	f := classificationFixture(t)
	runGit(t, f.repo, "config", "fetch.prune", "true") // must not take effect
	r := Run(context.Background(), Options{Path: f.repo})
	if r.Fetch.PrunedTotal != 0 || len(r.Fetch.PrunedRefs) != 0 {
		t.Fatalf("%+v", r.Fetch)
	}
	f.oid(t, "refs/remotes/origin/gone")
	if it := itemsByBranch(t, r)["gone"]; it.Result != ResultUpToDate {
		t.Errorf("%+v", it)
	}
}

func TestRunRefusalsMatchThePlan(t *testing.T) {
	dir := t.TempDir()
	r := Run(context.Background(), Options{Path: dir})
	if r.Error == nil || r.Error.Code != "sync-unsupported-backend" || r.RunID != nil || len(r.Items) != 0 || r.Mode != "sync" {
		t.Fatalf("%+v", r)
	}
	f := newFixture(t)
	if r := Run(context.Background(), Options{Path: f.repo, Remote: "nope"}); r.Error == nil || r.Error.Code != "sync-remote-unknown" {
		t.Fatalf("%+v", r)
	}
}
