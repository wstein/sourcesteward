// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func statusOf(t *testing.T, path string, o StatusOptions) StatusResult {
	t.Helper()
	o.Path = path
	r := Status(context.Background(), o)
	if r.Error != nil {
		t.Fatalf("status failed: %+v", r.Error)
	}
	return r
}

func statusRepos(r StatusResult) map[string]RepoStatus {
	m := map[string]RepoStatus{}
	for _, rp := range r.Repos {
		m[rp.Name] = rp
	}
	return m
}

func statusNames(r StatusResult) string {
	var n []string
	for _, rp := range r.Repos {
		n = append(n, rp.Name)
	}
	return strings.Join(n, " ")
}

func TestStatusCleanWorkspaceNeedsNoAttention(t *testing.T) {
	w := newWorkspace(t)
	r := statusOf(t, w.ws, StatusOptions{})
	if got := statusNames(r); got != ". a a/n b c dev" {
		t.Fatalf("names %q", got)
	}
	if r.Summary.Total != 6 || r.Summary.Attention != 0 || r.SchemaVersion != 1 || r.Command != "status" {
		t.Fatalf("%+v", r.Summary)
	}
	rp := statusRepos(r)
	root := rp["."]
	if root.Kind != "root" || root.Branch == nil || *root.Branch != "main" || root.Upstream == nil || *root.Upstream != "origin/main" ||
		root.Ahead == nil || *root.Ahead != 0 || root.Behind == nil || *root.Behind != 0 || root.Gitlink != nil || len(root.Attention) != 0 {
		t.Fatalf("%+v", root)
	}
	if c := rp["c"]; c.Branch != nil || !c.Detached || c.Gitlink == nil || c.Gitlink.State != "ok" || len(c.Attention) != 0 {
		t.Fatalf("detached submodule: %+v", c)
	}
	if rp["a/n"].Kind != "submodule" || rp["dev"].Branch == nil || *rp["dev"].Branch != "develop" {
		t.Fatalf("%+v", rp["dev"])
	}
}

func TestStatusFindsTheWorkspaceFromAnywhereInsideIt(t *testing.T) {
	w := newWorkspace(t)
	if err := os.MkdirAll(filepath.Join(w.ws, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(w.root, "linked")
	runGit(t, w.ws, "worktree", "add", "-q", "-b", "topic", linked)
	for _, start := range []string{w.ws, filepath.Join(w.ws, "docs"), w.dir("a"), w.dir("a/n"), w.dir("dev"), linked} {
		r := statusOf(t, start, StatusOptions{})
		if got := statusNames(r); got != ". a a/n b c dev" || r.Root == nil || *r.Root != w.ws {
			t.Errorf("%s: %q root %v", start, got, r.Root)
		}
	}
}

func TestStatusChangesConflictsOperationAndStash(t *testing.T) {
	w := newWorkspace(t)
	write(t, filepath.Join(w.ws, "staged.txt"), "s\n")
	runGit(t, w.ws, "add", "staged.txt")
	write(t, filepath.Join(w.ws, "untracked.txt"), "x")  // untracked
	write(t, filepath.Join(w.ws, "ws.txt"), "changed\n") // unstaged (tracked seed file)
	write(t, filepath.Join(w.ws, "u1.txt"), "u\n")

	// a stash in b
	write(t, filepath.Join(w.dir("b"), "b.txt"), "stash me\n")
	runGit(t, w.dir("b"), "stash", "-q")

	// a merge conflict in a
	a := w.dir("a")
	runGit(t, a, "checkout", "-q", "-b", "side")
	commit(t, a, "a.txt", "side\n")
	runGit(t, a, "checkout", "-q", "main")
	commit(t, a, "a.txt", "main\n")
	if err := gitCmd(a, "merge", "side").Run(); err == nil {
		t.Fatal("expected a conflict")
	}

	r := statusOf(t, w.ws, StatusOptions{})
	rp := statusRepos(r)
	root := rp["."]
	if root.Changes.Staged != 1 || root.Changes.Unstaged < 1 || root.Changes.Untracked < 1 || root.Changes.Conflicts != 0 || root.Operation != nil {
		t.Fatalf("root %+v", root.Changes)
	}
	if rp["b"].Stash != 1 {
		t.Fatalf("stash %d", rp["b"].Stash)
	}
	ar := rp["a"]
	if ar.Changes.Conflicts != 1 || ar.Operation == nil || *ar.Operation != "merge" {
		t.Fatalf("a %+v op %v", ar.Changes, ar.Operation)
	}
	if !has(ar.Attention, "conflicts") || !has(ar.Attention, "operation") || !has(root.Attention, "staged") || !has(root.Attention, "unstaged") || !has(root.Attention, "untracked") {
		t.Fatalf("attention %v %v", ar.Attention, root.Attention)
	}
	if r.Summary.Attention < 2 {
		t.Fatalf("summary %+v", r.Summary)
	}
}

func has(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func TestStatusAheadBehindAndFetchIsOptIn(t *testing.T) {
	w := newWorkspace(t)
	commit(t, w.dir("b"), "local.txt", "l\n")
	w.advance(t, "b", "main")
	before := runGit(t, w.dir("b"), "for-each-ref")
	r := statusOf(t, w.ws, StatusOptions{})
	b := statusRepos(r)["b"]
	if *b.Ahead != 1 || *b.Behind != 0 || !has(b.Attention, "ahead") {
		t.Fatalf("without fetch: %d %d", *b.Ahead, *b.Behind)
	}
	if after := runGit(t, w.dir("b"), "for-each-ref"); after != before {
		t.Fatal("status changed refs without --fetch")
	}
	r = statusOf(t, w.ws, StatusOptions{Fetch: true})
	b = statusRepos(r)["b"]
	if *b.Ahead != 1 || *b.Behind != 1 || !has(b.Attention, "behind") || b.FetchError != nil {
		t.Fatalf("with fetch: %d %d", *b.Ahead, *b.Behind)
	}
}

func TestStatusNoUpstreamAndGone(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.dir("b"), "checkout", "-q", "-b", "local-only")
	runGit(t, w.dir("dev"), "checkout", "-q", "-b", "gone")
	runGit(t, w.dir("dev"), "push", "-q", "-u", "origin", "gone")
	runGit(t, w.dir("dev"), "push", "-q", "origin", "--delete", "gone")
	runGit(t, w.dir("dev"), "fetch", "-q", "--prune", "origin")
	rp := statusRepos(statusOf(t, w.ws, StatusOptions{}))
	if b := rp["b"]; b.Upstream != nil || b.Ahead != nil || !has(b.Attention, "no-upstream") {
		t.Fatalf("%+v", b)
	}
	if d := rp["dev"]; d.Upstream == nil || !d.UpstreamGone || d.Ahead != nil || !has(d.Attention, "upstream-gone") {
		t.Fatalf("%+v", d)
	}
}

func TestStatusBranchIntegration(t *testing.T) {
	w := newWorkspace(t)
	b := w.dir("b")
	runGit(t, b, "branch", "merged")
	runGit(t, b, "checkout", "-q", "-b", "wip")
	commit(t, b, "wip.txt", "w\n")
	runGit(t, b, "checkout", "-q", "main")
	br := statusRepos(statusOf(t, w.ws, StatusOptions{}))["b"].Branches
	if br.Local != 3 || br.Base == nil || *br.Base != "origin/main" || br.Integrated == nil || *br.Integrated != 1 || br.NotIntegrated == nil || *br.NotIntegrated != 1 {
		t.Fatalf("%+v", br)
	}
}

func TestStatusLinkedWorktrees(t *testing.T) {
	w := newWorkspace(t)
	clean := filepath.Join(w.root, "wt-clean")
	dirty := filepath.Join(w.root, "wt-dirty")
	locked := filepath.Join(w.root, "wt-locked")
	gone := filepath.Join(w.root, "wt-gone")
	runGit(t, w.ws, "worktree", "add", "-q", "-b", "clean", clean)
	runGit(t, w.ws, "worktree", "add", "-q", "-b", "dirty", dirty)
	runGit(t, w.ws, "worktree", "add", "-q", "--detach", locked)
	runGit(t, w.ws, "worktree", "add", "-q", "-b", "gone", gone)
	write(t, filepath.Join(dirty, "x.txt"), "x\n")
	runGit(t, w.ws, "worktree", "lock", "--reason", "on a usb stick", locked)
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	r := statusOf(t, w.ws, StatusOptions{})
	wt := statusRepos(r)["."].Worktrees
	if wt.Linked != 4 || wt.Dirty != 1 || wt.Locked != 1 || wt.Prunable != 1 || len(wt.List) != 0 {
		t.Fatalf("%+v", wt)
	}
	for _, reason := range []string{"worktree-dirty", "worktree-locked", "worktree-prunable"} {
		if !has(statusRepos(r)["."].Attention, reason) {
			t.Errorf("missing %s", reason)
		}
	}
	r = statusOf(t, w.ws, StatusOptions{Worktrees: true})
	list := statusRepos(r)["."].Worktrees.List
	if len(list) != 4 {
		t.Fatalf("%+v", list)
	}
	byPath := map[string]WorktreeInfo{}
	for _, it := range list {
		byPath[it.Path] = it
	}
	if d := byPath[dirty]; d.Branch == nil || *d.Branch != "dirty" || !d.Dirty {
		t.Fatalf("%+v", d)
	}
	if l := byPath[locked]; l.Branch != nil || !l.Detached || l.Locked == nil || *l.Locked != "on a usb stick" {
		t.Fatalf("%+v", l)
	}
	if g := byPath[gone]; !g.Prunable {
		t.Fatalf("%+v", g)
	}
}

func TestStatusHandlesManyWorktrees(t *testing.T) {
	w := newWorkspace(t)
	const n = 120
	for i := 0; i < n; i++ {
		runGit(t, w.ws, "worktree", "add", "-q", "--detach", filepath.Join(w.root, fmt.Sprintf("many-%03d", i)))
	}
	r := statusOf(t, w.ws, StatusOptions{Worktrees: true})
	if wt := statusRepos(r)["."].Worktrees; wt.Linked != n || len(wt.List) != n || wt.Dirty != 0 {
		t.Fatalf("%d %d %d", wt.Linked, len(wt.List), wt.Dirty)
	}
}

func TestStatusGitlinkDrift(t *testing.T) {
	w := newWorkspace(t)
	commit(t, w.dir("b"), "drift.txt", "d\n")
	runGit(t, w.dir("dev"), "checkout", "-q", "--detach", "HEAD~1")
	w.advance(t, "c", "main")
	runGit(t, w.dir("c"), "fetch", "-q", "origin")
	runGit(t, w.dir("c"), "checkout", "-q", "--detach", "origin/main")
	rp := statusRepos(statusOf(t, w.ws, StatusOptions{}))
	b := rp["b"]
	if b.Gitlink == nil || b.Gitlink.State != "drift" || b.Gitlink.Relation == nil || *b.Gitlink.Relation != "ahead" ||
		b.Gitlink.Recorded == nil || b.Gitlink.CheckedOut == nil || *b.Gitlink.Recorded == *b.Gitlink.CheckedOut || !has(b.Attention, "gitlink-drift") {
		t.Fatalf("%+v", b.Gitlink)
	}
	if d := rp["dev"]; d.Gitlink.State != "drift" || *d.Gitlink.Relation != "behind" {
		t.Fatalf("%+v", d.Gitlink)
	}
	if c := rp["c"]; c.Gitlink.State != "drift" || *c.Gitlink.Relation != "ahead" {
		t.Fatalf("%+v", c.Gitlink)
	}
}

func TestStatusUninitializedSubmodule(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.ws, "submodule", "deinit", "-q", "-f", "b")
	rp := statusRepos(statusOf(t, w.ws, StatusOptions{}))
	b := rp["b"]
	if b.State != "uninitialized" || b.Gitlink == nil || b.Gitlink.State != "uninitialized" || b.Gitlink.Recorded == nil || b.Gitlink.CheckedOut != nil || len(b.Attention) != 0 {
		t.Fatalf("%+v", b)
	}
}

func TestStatusIsReadOnly(t *testing.T) {
	w := newWorkspace(t)
	runGit(t, w.ws, "worktree", "add", "-q", "-b", "wt", filepath.Join(w.root, "wt"))
	write(t, filepath.Join(w.ws, "untracked.txt"), "u\n")
	write(t, filepath.Join(w.dir("a"), "a.txt"), "changed\n")
	snap := func() string {
		var b strings.Builder
		for _, n := range []string{".", "a", "a/n", "b", "c", "dev"} {
			b.WriteString(runGit(t, w.dir(n), "for-each-ref") + runGit(t, w.dir(n), "status", "--porcelain=v2", "--branch", "--show-stash") + runGit(t, w.dir(n), "stash", "list"))
		}
		files := snapshotTree(t, w.ws)
		keys := make([]string, 0, len(files))
		for k := range files {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(k + "=" + files[k] + "\n")
		}
		return b.String()
	}
	before := snap()
	idx := runGit(t, w.ws, "ls-files", "--stage")
	statusOf(t, w.ws, StatusOptions{Worktrees: true})
	statusOf(t, w.dir("a/n"), StatusOptions{})
	if after := snap(); after != before || runGit(t, w.ws, "ls-files", "--stage") != idx {
		t.Fatalf("status changed the workspace:\n%s\n----\n%s", before, after)
	}
}

func TestStatusRefusesAPathOutsideAnyRepository(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := Status(context.Background(), StatusOptions{Path: dir})
	if r.Error == nil || r.Error.Code != "status-not-a-repository" || r.Repos == nil || len(r.Repos) != 0 {
		t.Fatalf("%+v", r)
	}
}
