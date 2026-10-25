// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

const observeBase = "refs/heads/main"

// observeRepo is a repository with branch main and one commit.
func observeRepo(t *testing.T) string {
	t.Helper()
	d := fixtureGit(t)
	gitFixture(t, d, "branch", "-M", "main")
	return d
}

// observeAdd adds a linked worktree on a new branch of the same name and returns its path.
func observeAdd(t *testing.T, repo, name string) string {
	t.Helper()
	p := filepath.Join(gitTempDir(t), name)
	gitFixture(t, repo, "worktree", "add", "-q", "-b", name, p, "main")
	return p
}

func observe(t *testing.T, repo string) WorktreeObservation {
	t.Helper()
	o, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: repo, Base: observeBase})
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	return o
}

func evidenceOf(t *testing.T, o WorktreeObservation, path string) WorktreeEvidence {
	t.Helper()
	for _, w := range o.Worktrees {
		if w.Path == path {
			return w
		}
	}
	t.Fatalf("no worktree %s in %+v", path, o.Worktrees)
	return WorktreeEvidence{}
}

func commitIn(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, dir, "add", file)
	gitFixture(t, dir, "commit", "-qm", "change "+file)
}

func TestObserveWorktreeRegistrationFacts(t *testing.T) {
	repo := observeRepo(t)
	linked := observeAdd(t, repo, "task-a")
	gitFixture(t, repo, "worktree", "lock", "--reason", "stw:task-a", linked)
	gone := observeAdd(t, repo, "gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	o := observe(t, repo)
	if o.Backend != "git" || o.Base != observeBase || o.BaseOID != gitFixture(t, repo, "rev-parse", "main") {
		t.Fatalf("header: %+v", o)
	}
	main := evidenceOf(t, o, repo)
	if !main.Primary || main.Locked || main.HeadRef != "refs/heads/main" || main.Incomplete != "" || main.Proof != "" || !main.LastActivity.IsZero() {
		t.Fatalf("primary: %+v", main)
	}
	w := evidenceOf(t, o, linked)
	if w.Primary || !w.Locked || w.LockReason != "stw:task-a" || w.HeadRef != "refs/heads/task-a" || w.HeadOID != gitFixture(t, repo, "rev-parse", "task-a") {
		t.Fatalf("linked: %+v", w)
	}
	g := evidenceOf(t, o, gone)
	if !g.DirMissing || !g.Prunable || g.Incomplete != "" || g.Proof != "" || g.HeadOID == "" {
		t.Fatalf("missing directory: %+v", g)
	}
	if o.TrackingSkipped != "tracking-requires-online" || len(o.TrackingRefs) != 0 {
		t.Fatalf("tracking: %+v", o)
	}
}

func TestObserveWorktreeContentFacts(t *testing.T) {
	repo := observeRepo(t)
	clean := observeAdd(t, repo, "clean")
	work := observeAdd(t, repo, "work")
	commitIn(t, work, "mine", "own work\n")
	staged := observeAdd(t, repo, "staged")
	if err := os.WriteFile(filepath.Join(staged, "file"), []byte("staged change\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, staged, "add", "file")
	extras := observeAdd(t, repo, "extras")
	if err := os.WriteFile(filepath.Join(extras, "scratch"), []byte("untracked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	edited := observeAdd(t, repo, "edited")
	if err := os.WriteFile(filepath.Join(edited, "file"), []byte("edited\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Minute)
	o := observe(t, repo)

	c := evidenceOf(t, o, clean)
	if c.Incomplete != "" || c.Dirty || c.IgnoredExtras || c.Proof != "ancestor" || c.Ahead != 0 || c.HasOwnCommit || c.AuthorHint != "Fixture" || c.LastActivity.Before(before) {
		t.Fatalf("clean: %+v", c)
	}
	w := evidenceOf(t, o, work)
	if w.Incomplete != "" || w.Dirty || w.Proof != "none" || w.Ahead != 1 || !w.HasOwnCommit {
		t.Fatalf("work: %+v", w)
	}
	if s := evidenceOf(t, o, staged); s.Incomplete != "" || !s.Dirty || s.LastActivity.IsZero() || s.AuthorHint != "Fixture" {
		t.Fatalf("staged: %+v", s)
	}
	if e := evidenceOf(t, o, extras); e.Incomplete != "" || !e.IgnoredExtras || e.Dirty {
		t.Fatalf("extras: %+v", e)
	}
	// A modified or deleted tracked file is Dirty; the other content facts are kept.
	e := evidenceOf(t, o, edited)
	if e.Incomplete != "" || !e.Dirty || e.IgnoredExtras || e.LastActivity.IsZero() || e.Proof != "ancestor" || e.AuthorHint != "Fixture" || e.HeadRef != "refs/heads/edited" {
		t.Fatalf("edited: %+v", e)
	}
	if err := os.Remove(filepath.Join(edited, "file")); err != nil {
		t.Fatal(err)
	}
	if d := evidenceOf(t, observe(t, repo), edited); d.Incomplete != "" || !d.Dirty || d.Proof != "ancestor" {
		t.Fatalf("deleted: %+v", d)
	}
}

func TestObserveBranches(t *testing.T) {
	repo := observeRepo(t)
	gitFixture(t, repo, "branch", "merged")
	gitFixture(t, repo, "branch", "ahead")
	gitFixture(t, repo, "switch", "-q", "ahead")
	commitIn(t, repo, "x", "x\n")
	gitFixture(t, repo, "switch", "-q", "main")
	o := observe(t, repo)
	by := map[string]BranchEvidence{}
	for _, b := range o.Branches {
		by[b.Name] = b
	}
	if len(by) != 3 {
		t.Fatalf("branches: %+v", o.Branches)
	}
	if b := by["refs/heads/main"]; !b.IsBase || b.Incomplete != "" {
		t.Fatalf("main: %+v", b)
	}
	if b := by["refs/heads/merged"]; b.IsBase || b.Proof != "ancestor" || b.Ahead != 0 || b.AuthorHint != "Fixture" || b.OID != gitFixture(t, repo, "rev-parse", "merged") {
		t.Fatalf("merged: %+v", b)
	}
	if b := by["refs/heads/ahead"]; b.Proof != "none" || b.Ahead != 1 {
		t.Fatalf("ahead: %+v", b)
	}
	for i := 1; i < len(o.Branches); i++ {
		if o.Branches[i-1].Name >= o.Branches[i].Name {
			t.Fatalf("branches are not sorted: %+v", o.Branches)
		}
	}
}

// A branch whose commits were rebase-merged into the base has different SHAs but the same patch.
func TestObservePatchIDProof(t *testing.T) {
	repo := observeRepo(t)
	topic := observeAdd(t, repo, "topic")
	commitIn(t, topic, "feature", "feature\n")
	commitIn(t, repo, "other", "other\n")
	gitFixture(t, repo, "cherry-pick", "topic")
	o := observe(t, repo)
	if w := evidenceOf(t, o, topic); w.Incomplete != "" || w.Proof != "patch-id" || w.Ahead != 1 {
		t.Fatalf("patch-id worktree: %+v", w)
	}
	for _, b := range o.Branches {
		if b.Name == "refs/heads/topic" && b.Proof != "patch-id" {
			t.Fatalf("patch-id branch: %+v", b)
		}
	}
}

func TestObserveOptionsAreValidated(t *testing.T) {
	repo := observeRepo(t)
	for name, o := range map[string]ObserveOptions{
		"relative path":         {Path: "repo", Base: observeBase},
		"no base":               {Path: repo},
		"remote when offline":   {Path: repo, Base: observeBase, Remote: "origin"},
		"online without remote": {Path: repo, Base: observeBase, Online: true},
	} {
		if _, err := ObserveWorktrees(context.Background(), o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: gitTempDir(t), Base: observeBase}); err == nil {
		t.Error("not a repository: accepted")
	}
	if _, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: repo, Base: "refs/heads/absent"}); err == nil {
		t.Error("missing base: accepted")
	}
}

func TestObserveWorktreeLimitMarksTheSurplusIncomplete(t *testing.T) {
	repo := observeRepo(t)
	observeAdd(t, repo, "a")
	observeAdd(t, repo, "b")
	observeAdd(t, repo, "c")
	defer func(n int) { observerWorktreeLimit = n }(observerWorktreeLimit)
	observerWorktreeLimit = 2
	o := observe(t, repo)
	if len(o.Worktrees) != 4 {
		t.Fatalf("registrations must all be listed: %+v", o.Worktrees)
	}
	incomplete := 0
	for _, w := range o.Worktrees {
		if w.Incomplete == "worktree-limit" {
			incomplete++
			if w.Path == "" || w.HeadOID == "" {
				t.Fatalf("registration facts lost: %+v", w)
			}
		}
	}
	if incomplete != 2 {
		t.Fatalf("surplus incomplete = %d: %+v", incomplete, o.Worktrees)
	}
}

func TestObserveProofPhaseExhaustionIsIncompleteNeverNone(t *testing.T) {
	repo := observeRepo(t)
	a := observeAdd(t, repo, "a")
	commitIn(t, a, "x", "x\n")
	b := observeAdd(t, repo, "b")
	commitIn(t, b, "y", "y\n")
	clock := time.Now()
	obs := defaultWorktreeObserver()
	obs.refs.integration.now = func() time.Time { clock = clock.Add(61 * time.Second); return clock }
	o, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{a, b} {
		if w := evidenceOf(t, o, p); w.Incomplete != "proof-budget-exhausted" || w.Proof != "" || w.Ahead != 0 || w.HasOwnCommit || w.Dirty || w.AuthorHint != "" || !w.LastActivity.IsZero() {
			t.Fatalf("%s: a proof-Incomplete worktree keeps content facts: %+v", p, w)
		}
	}
	for _, br := range o.Branches {
		if br.Name == "refs/heads/a" && (br.Incomplete != "proof-budget-exhausted" || br.Proof != "") {
			t.Fatalf("branch: %+v", br)
		}
	}
}

func TestObserveEveryWorktreeGetsItsOwnContentBudget(t *testing.T) {
	repo := observeRepo(t)
	var paths []string
	for i := 0; i < 8; i++ {
		paths = append(paths, observeAdd(t, repo, fmt.Sprintf("w%d", i)))
	}
	o := observe(t, repo)
	for _, p := range paths {
		if w := evidenceOf(t, o, p); w.Incomplete != "" || w.Proof != "ancestor" {
			t.Fatalf("a shared content budget ran out: %+v", w)
		}
	}
}

func TestObserveLastActivityIsTheNewestOfHeadIndexAndTrackedFiles(t *testing.T) {
	repo := observeRepo(t)
	w := observeAdd(t, repo, "act")
	want := time.Now().Add(-time.Hour).Truncate(time.Second)
	gitdir := strings.TrimSpace(gitFixture(t, w, "rev-parse", "--absolute-git-dir"))
	old := want.Add(-48 * time.Hour)
	for _, p := range []string{filepath.Join(gitdir, "HEAD"), filepath.Join(gitdir, "index"), filepath.Join(w, "file")} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(w, "file"), want, want); err != nil {
		t.Fatal(err)
	}
	got := evidenceOf(t, observe(t, repo), w).LastActivity
	if !got.Equal(want) {
		t.Fatalf("last activity %v, want %v", got, want)
	}
}

// treeSnapshot records every file under roots (mode, size, modification time, content digest and
// symlink targets), including every .git directory and each linked worktree's own index.
func treeSnapshot(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	for _, root := range roots {
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			entry := fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				target, _ := os.Readlink(p)
				entry += " -> " + target
			case info.Mode().IsRegular():
				data, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				entry += " " + gitDigest(string(data))
			}
			snap[p] = entry
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return snap
}

func sameSnapshot(t *testing.T, before, after map[string]string) bool {
	t.Helper()
	if len(before) != len(after) {
		return false
	}
	for p, v := range before {
		if after[p] != v {
			t.Logf("changed: %s\n  before %s\n  after  %s", p, v, after[p])
			return false
		}
	}
	return true
}

// readOnlyFixture is stat-dirty and content-clean in the primary checkout and in every linked
// worktree (each has its own index). A plain git status would rewrite an index, run the
// configured fsmonitor hook and run the clean filter attributed to the tracked file; the planning
// policy neutralizes the first two, and no observer command applies a filter (with a filter the
// repository is refused, see TestObserveRefusesAFilterAttribute). The markers are
// written into the returned state directory, which belongs to the snapshot.
func readOnlyFixture(t *testing.T, withFilter bool) (repo string, trees []string, state string) {
	t.Helper()
	repo = observeRepo(t)
	state = gitTempDir(t)
	script := func(name string) string {
		p := filepath.Join(gitTempDir(t), name)
		body := "#!/bin/sh\necho ran >> " + filepath.Join(state, name+"-ran") + "\n"
		if name == "filter" {
			body += "cat\n"
		}
		if err := os.WriteFile(p, []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// The scripts are named so that the markers are fsmonitor-ran and filter-ran.
	gitFixture(t, repo, "config", "core.untrackedCache", "true")
	gitFixture(t, repo, "config", "core.fsmonitor", script("fsmonitor"))
	if withFilter {
		// The existing admission refuses a repository with an active filter attribute as a whole.
		gitFixture(t, repo, "config", "filter.noop.clean", script("filter"))
		if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("file filter=noop\n"), 0600); err != nil {
			t.Fatal(err)
		}
		gitFixture(t, repo, "add", ".gitattributes")
		gitFixture(t, repo, "commit", "-qm", "attributes")
	}
	for _, n := range []string{"one", "two", "three"} {
		trees = append(trees, observeAdd(t, repo, n))
	}
	trees = append(trees, repo)
	touchTrees(t, trees)
	return repo, trees, state
}

func touchTrees(t *testing.T, trees []string) {
	t.Helper()
	later := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	for _, tree := range trees {
		if err := os.Chtimes(filepath.Join(tree, "file"), later, later); err != nil {
			t.Fatal(err)
		}
	}
}

// The harness can fail: a plain git status over the fixture, with its default environment,
// rewrites a worktree's own index and runs the configured fsmonitor hook and the clean filter, and
// the snapshot shows all three.
func TestObserveSnapshotDetectsAPlainGitStatus(t *testing.T) {
	repo, trees, state := readOnlyFixture(t, true)
	roots := append(append([]string{}, trees...), state, filepath.Dir(trees[0]))
	before := treeSnapshot(t, roots...)
	if out, err := exec.Command("git", "-C", trees[0], "status", "--porcelain").CombinedOutput(); err != nil {
		t.Fatalf("probe: %v %s", err, out)
	}
	after := treeSnapshot(t, roots...)
	index := filepath.Join(repo, ".git", "worktrees", "one", "index")
	if after[index] == before[index] {
		t.Error("the stat-dirty fixture exposes no index refresh")
	}
	for _, marker := range []string{"fsmonitor-ran", "filter-ran"} {
		if _, ok := after[filepath.Join(state, marker)]; !ok {
			t.Errorf("the fixture does not expose %s", marker)
		}
	}
}

func TestObserveRefusesAFilterAttribute(t *testing.T) {
	repo, trees, state := readOnlyFixture(t, true)
	roots := append(append([]string{}, trees...), state)
	before := treeSnapshot(t, roots...)
	if _, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: repo, Base: observeBase}); err == nil {
		t.Fatal("a repository with an active filter attribute was observed")
	}
	if !sameSnapshot(t, before, treeSnapshot(t, roots...)) {
		t.Fatal("the refusal changed the repository")
	}
}

func TestObserveWritesNothing(t *testing.T) {
	repo, trees, state := readOnlyFixture(t, false)
	roots := append(append([]string{}, trees...), state, filepath.Dir(trees[0]))
	before := treeSnapshot(t, roots...)
	t.Run("sandboxed", func(t *testing.T) {
		observe(t, repo)
		if !sameSnapshot(t, before, treeSnapshot(t, roots...)) {
			t.Fatal("an observer changed the repository, a worktree, its index, config or the store")
		}
	})
	// Every native command of the observers, the reused retirement checks included, runs without
	// the sandbox, which mounts the file system read-only and would hide a write by itself. The
	// identity inspection runs unsandboxed in any case (read-only rev-parse queries).
	t.Run("without the sandbox", func(t *testing.T) {
		defer func(v bool) { nativeUnsandboxed = v }(nativeUnsandboxed)
		nativeUnsandboxed = true
		observe(t, repo)
		if !sameSnapshot(t, before, treeSnapshot(t, roots...)) {
			t.Fatal("the observers' native commands write when the sandbox does not stop them")
		}
	})
}

// TestObserveSixtyOneRealisticWorktrees is the budget fixture of Spec 041 FR-052: a primary checkout
// and sixty linked worktrees of 2,000 tracked files each, thirty with a patch-id tip, thirty with unmerged
// work, with a shared object store. Nothing may be Incomplete and the run must fit the 60 s
// base observation window plus the 60 s minimum of the proof phase; the measured time is logged.
func TestObserveSixtyOneRealisticWorktrees(t *testing.T) {
	if testing.Short() {
		t.Skip("builds 61 checkouts")
	}
	repo := observeRepo(t)
	for d := 0; d < 20; d++ {
		dir := filepath.Join(repo, fmt.Sprintf("pkg%02d", d))
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < 100; f++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%03d.txt", f)), []byte(strings.Repeat(fmt.Sprintf("line %d %d\n", d, f), 40)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	gitFixture(t, repo, "add", ".")
	gitFixture(t, repo, "commit", "-qm", "tracked tree")
	var trees []string
	for i := 0; i < 60; i++ {
		trees = append(trees, observeAdd(t, repo, fmt.Sprintf("w%02d", i)))
	}
	// Every worktree has its own commit; thirty are cherry-picked into the base afterwards (a
	// different SHA with the same patch), thirty stay unmerged. The base has moved on since they
	// branched, so every tip needs the patch-id and merge evaluations of the proof phase.
	for i := 0; i < 60; i++ {
		commitIn(t, trees[i], fmt.Sprintf("own-%02d", i), fmt.Sprintf("own work %d\n", i))
	}
	commitIn(t, repo, "moved-on", "base moved\n")
	for i := 0; i < 30; i++ {
		gitFixture(t, repo, "cherry-pick", fmt.Sprintf("w%02d", i))
	}
	start := time.Now()
	o := observe(t, repo)
	elapsed := time.Since(start)
	t.Logf("61 worktrees, 2,000 tracked files each: %v", elapsed)
	if len(o.Worktrees) != 61 {
		t.Fatalf("worktrees: %d", len(o.Worktrees))
	}
	proofs := map[string]int{}
	for _, w := range o.Worktrees {
		if w.Incomplete != "" {
			t.Fatalf("incomplete within the stated budget: %+v", w)
		}
		if !w.Primary {
			proofs[w.Proof]++
		}
	}
	t.Logf("proofs: %v", proofs)
	if proofs["patch-id"] != 30 || proofs["none"] != 30 {
		t.Fatalf("proof mix: %v", proofs)
	}
	if elapsed > planObservationTimeout+integrationPhaseTimeout {
		t.Fatalf("%v exceeds the observation plus proof phase", elapsed)
	}
}

func TestObserveTrackingOnline(t *testing.T) {
	o, tc, s, opens := fixtureTidyV3TrackingPlanner(t)
	dir := o.Expected.Identity.WorkspaceRoot
	gitFixture(t, dir, "update-ref", "refs/remotes/origin/live", gitFixture(t, dir, "rev-parse", "refs/remotes/origin/stale"))
	obs := defaultWorktreeObserver()
	obs.tracking = tc
	before := treeSnapshot(t, dir)
	got, err := obs.observe(context.Background(), ObserveOptions{Path: dir, Base: "HEAD", Online: true, Remote: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	if *opens != 1 || !s.closed || got.TrackingSkipped != "" || len(got.TrackingRefs) != 2 {
		t.Fatalf("opens=%d closed=%v: %+v", *opens, s.closed, got)
	}
	want := map[string]string{"refs/remotes/origin/stale": "github-branch-absent", "refs/remotes/origin/live": "none"}
	reason := map[string]string{"refs/remotes/origin/stale": "github-branch-absent", "refs/remotes/origin/live": "remote-source-present"}
	for _, r := range got.TrackingRefs {
		if r.Incomplete != "" || r.Proof != want[r.Name] || r.Reason != reason[r.Name] {
			t.Fatalf("tracking ref: %+v", r)
		}
	}
	if !sameSnapshot(t, before, treeSnapshot(t, dir)) {
		t.Fatal("online observation changed the repository")
	}

	// A failing advertisement makes every tracking reference of the remote Incomplete.
	o, tc, s, _ = fixtureTidyV3TrackingPlanner(t)
	dir = o.Expected.Identity.WorkspaceRoot
	s.errorAt = map[int]error{1: failure("hosting-evidence-unavailable", "fixture failure")}
	obs.tracking = tc
	got, err = obs.observe(context.Background(), ObserveOptions{Path: dir, Base: "HEAD", Online: true, Remote: "origin"})
	if err != nil || len(got.TrackingRefs) != 1 || got.TrackingRefs[0].Incomplete == "" || got.TrackingRefs[0].Proof != "" {
		t.Fatalf("failed advertisement: %v %+v", err, got.TrackingRefs)
	}
}

func TestObserveHelpers(t *testing.T) {
	for _, tc := range []struct {
		name, base string
		want       bool
	}{
		{"refs/heads/main", "refs/heads/main", true},
		{"refs/heads/main", "refs/remotes/origin/main", true},
		{"refs/heads/feature/x", "refs/remotes/origin/feature/x", true},
		{"refs/heads/other", "refs/remotes/origin/main", false},
		{"refs/heads/main", "HEAD", false},
		{"refs/heads/main", "refs/heads/develop", false},
	} {
		if got := isBaseBranch(tc.name, tc.base); got != tc.want {
			t.Errorf("isBaseBranch(%q, %q) = %v", tc.name, tc.base, got)
		}
	}
	long := strings.Repeat("é", 100)
	for in, want := range map[string]string{
		"Ana\x1b[31m Lee\n": "Ana[31m Lee",
		"  plain  ":         "plain",
		"bad\xffbyte":       "badbyte",
		long:                strings.Repeat("é", 64),
	} {
		if got := authorHint(in); got != want {
			t.Errorf("authorHint(%q) = %q, want %q", in, got, want)
		}
	}
	if got := authorHint(strings.Repeat("x", 200)); len(got) != 128 {
		t.Errorf("author bound: %d", len(got))
	}
}

func TestObserveLastActivityCountsTheIndex(t *testing.T) {
	repo := observeRepo(t)
	w := observeAdd(t, repo, "idx")
	gitdir := strings.TrimSpace(gitFixture(t, w, "rev-parse", "--absolute-git-dir"))
	old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	want := old.Add(24 * time.Hour)
	for _, p := range []string{filepath.Join(gitdir, "HEAD"), filepath.Join(w, "file")} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(gitdir, "index"), want, want); err != nil {
		t.Fatal(err)
	}
	if got := evidenceOf(t, observe(t, repo), w).LastActivity; !got.Equal(want) {
		t.Fatalf("last activity %v, want the index time %v", got, want)
	}
}

func TestObserveHistoryReadFailureIsIncompleteNotZero(t *testing.T) {
	repo := observeRepo(t)
	w := observeAdd(t, repo, "hist")
	commitIn(t, w, "x", "x\n")
	obs := defaultWorktreeObserver()
	obs.read = func(context.Context, string, string, ...string) (string, error) {
		return "", failure("git-query-refused", "fixture failure")
	}
	o, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase})
	if err != nil {
		t.Fatal(err)
	}
	if e := evidenceOf(t, o, w); e.Incomplete != "git-query-refused" || e.Ahead != 0 || e.HasOwnCommit || e.Proof != "" || e.Path != w {
		t.Fatalf("worktree: %+v", e)
	}
	for _, b := range o.Branches {
		if b.Incomplete != "git-query-refused" || b.Ahead != 0 || b.Proof != "" || b.OID == "" {
			t.Fatalf("branch: %+v", b)
		}
	}
}

func TestObserveRefusesARepositoryWithABareRegistration(t *testing.T) {
	src := observeRepo(t)
	bare := filepath.Join(gitTempDir(t), "bare.git")
	gitFixture(t, src, "clone", "-q", "--bare", src, bare)
	linked := filepath.Join(gitTempDir(t), "linked")
	gitFixture(t, bare, "worktree", "add", "-q", linked, "main")
	if _, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: linked, Base: observeBase}); err == nil {
		t.Fatal("a bare registration was observed")
	}
}

func TestObservePrimaryIsDecidedByTheGitDirectory(t *testing.T) {
	t.Run("separate git dir", func(t *testing.T) {
		repo := filepath.Join(gitTempDir(t), "sep")
		gitDir := filepath.Join(gitTempDir(t), "admin.git")
		if err := os.Mkdir(repo, 0700); err != nil {
			t.Fatal(err)
		}
		gitFixture(t, repo, "init", "-q", "--separate-git-dir", gitDir)
		gitFixture(t, repo, "config", "user.name", "Fixture")
		gitFixture(t, repo, "config", "user.email", "fixture@example.invalid")
		commitIn(t, repo, "file", "stored\n")
		gitFixture(t, repo, "branch", "-M", "main")
		linked := observeAdd(t, repo, "task")
		o := observe(t, repo)
		p := evidenceOf(t, o, repo)
		if !p.Primary || p.Incomplete != "" || p.Proof != "" || !p.LastActivity.IsZero() || p.Dirty {
			t.Fatalf("separate-git-dir primary: %+v", p)
		}
		if l := evidenceOf(t, o, linked); l.Primary || l.Incomplete != "" || l.Proof != "ancestor" {
			t.Fatalf("linked worktree: %+v", l)
		}
	})
	t.Run("submodule checkout", func(t *testing.T) {
		sub := observeRepo(t)
		super := observeRepo(t)
		gitFixture(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "mod")
		checkout := filepath.Join(super, "mod")
		if fi, err := os.Lstat(filepath.Join(checkout, ".git")); err != nil || fi.IsDir() {
			t.Fatalf("fixture: the submodule .git must be a file")
		}
		o, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: checkout, Base: observeBase})
		if err != nil {
			t.Fatal(err)
		}
		if len(o.Worktrees) != 1 || !o.Worktrees[0].Primary || o.Worktrees[0].Incomplete != "" || o.Worktrees[0].Proof != "" {
			t.Fatalf("submodule primary: %+v", o.Worktrees)
		}
	})
}

// A worktree directory must still be the registered one: a foreign repository swapped in at the
// registered path, or a different HEAD, is Incomplete and never clean and proven.
func TestObserveBindsContentToTheRegistration(t *testing.T) {
	repo := observeRepo(t)
	swapped := observeAdd(t, repo, "swapped")
	moved := observeAdd(t, repo, "moved")
	if err := os.RemoveAll(swapped); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(swapped, 0700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, swapped, "init", "-q")
	gitFixture(t, swapped, "config", "user.name", "Foreign")
	gitFixture(t, swapped, "config", "user.email", "foreign@example.invalid")
	commitIn(t, swapped, "file", "foreign\n")
	// Same repository, different position than registered: the worktree moves on after listing.
	// Simulated by committing in it before the observer reads the registrations.
	commitIn(t, moved, "extra", "extra\n")
	o := observe(t, repo)
	if w := evidenceOf(t, o, swapped); w.Incomplete != "registration-mismatch" || w.Proof != "" || w.Dirty || w.AuthorHint != "" || w.HeadOID == "" {
		t.Fatalf("foreign repository: %+v", w)
	}
	if w := evidenceOf(t, o, moved); w.Incomplete != "" {
		t.Fatalf("a worktree at its registered HEAD must be observed: %+v", w)
	}
}

func TestObserveHardStopsAndProofLimitsAreIncompleteNotNone(t *testing.T) {
	repo := observeRepo(t)
	w := observeAdd(t, repo, "open")
	commitIn(t, w, "x", "x\n")
	t.Run("object directory unavailable", func(t *testing.T) {
		obs := defaultWorktreeObserver()
		obs.refs.integration.makeObjectDir = func() (string, error) { return "", fmt.Errorf("fixture") }
		o, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase})
		if err != nil {
			t.Fatal(err)
		}
		if e := evidenceOf(t, o, w); e.Incomplete != "object-dir-unavailable" || e.Proof != "" {
			t.Fatalf("%+v", e)
		}
	})
	t.Run("grafts", func(t *testing.T) {
		grafts := filepath.Join(repo, ".git", "info", "grafts")
		if err := os.MkdirAll(filepath.Dir(grafts), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(grafts, nil, 0600); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(grafts)
		o := observe(t, repo)
		if e := evidenceOf(t, o, w); e.Incomplete != "history-override" || e.Proof != "" {
			t.Fatalf("%+v", e)
		}
	})
}

func TestProofOutcomeMapping(t *testing.T) {
	review := func(reasons ...string) V3CommitProof {
		return V3CommitProof{Decision: "retain", Reason: integrationReviewDisposition, Proof: "none", Evidence: &V3ProofEvidence{Integration: &V3IntegrationEvidence{Reasons: reasons}}}
	}
	for name, tc := range map[string]struct {
		p      V3CommitProof
		err    error
		proof  string
		reason string
	}{
		"ancestry":             {V3CommitProof{Decision: "delete", Proof: "native-ancestry"}, nil, "ancestor", ""},
		"patch-id":             {V3CommitProof{Decision: "delete", Proof: "patch-id"}, nil, "patch-id", ""},
		"not proven":           {V3CommitProof{Decision: "retain", Proof: "none", Reason: "integration-not-proven"}, nil, "none", ""},
		"needs review":         {review("conflict"), nil, "none", ""},
		"proof limit":          {review("proof-limit"), nil, "", "proof-limit"},
		"budget":               {review("proof-budget-exhausted"), nil, "", "proof-budget-exhausted"},
		"object dir":           {review("object-dir-unavailable"), nil, "", "object-dir-unavailable"},
		"ambiguous merge base": {review("ambiguous-merge-base"), nil, "", "ambiguous-merge-base"},
		"history override":     {V3CommitProof{Decision: "retain", Reason: "history-override", Proof: "none", Evidence: &V3ProofEvidence{Integration: &V3IntegrationEvidence{Reasons: []string{"history-override"}}}}, nil, "", "history-override"},
		"unsupported metadata": {V3CommitProof{Decision: "retain", Reason: "unsupported-metadata", Proof: "none"}, nil, "", "unsupported-metadata"},
		"limit error":          {V3CommitProof{}, errIntegrationLimit, "", "proof-limit"},
		"budget error":         {V3CommitProof{}, errIntegrationBudget, "", "proof-budget-exhausted"},
		"other error":          {V3CommitProof{}, fmt.Errorf("x"), "", "observation-failed"},
	} {
		if proof, reason := proofOutcome(tc.p, tc.err); proof != tc.proof || reason != tc.reason {
			t.Errorf("%s: got %q/%q, want %q/%q", name, proof, reason, tc.proof, tc.reason)
		}
	}
}

// The refusal table of the content checks: every outcome of the retirement checks becomes a fact
// or an Incomplete reason; none becomes a clean worktree.
func TestObserveContentOutcomeTable(t *testing.T) {
	type row struct {
		err    error
		dirty  bool
		extras bool
		reason string
	}
	other := fmt.Errorf("x")
	stored := map[string]row{
		"clean":         {nil, false, false, ""},
		"index differs": {failure("retirement-index-not-clean", "m"), true, false, ""},
		"file limit":    {failure("retirement-file-limit", "m"), false, false, "retirement-file-limit"},
		"gitlink":       {failure("git-tree-refused", "m"), false, false, "git-tree-refused"},
		"unknown error": {other, false, false, "observation-failed"},
		"deadline":      {context.DeadlineExceeded, false, false, "deadline-exceeded"},
	}
	for name, tc := range stored {
		if dirty, reason := storedOutcome(tc.err); dirty != tc.dirty || reason != tc.reason {
			t.Errorf("stored %s: got %v/%q", name, dirty, reason)
		}
	}
	manifest := map[string]row{
		"clean":                  {nil, false, false, ""},
		"unrecorded entry":       {failure("retirement-unrecorded-entry", "m"), false, true, ""},
		"stored content differs": {errRetirementStoredDiffers, true, false, ""},
		"stored file missing":    {errRetirementStoredMissing, true, false, ""},
		"file limit":             {failure("retirement-file-limit", "m"), false, false, "retirement-file-limit"},
		"unknown error":          {other, false, false, "observation-failed"},
		"deadline":               {context.DeadlineExceeded, false, false, "deadline-exceeded"},
	}
	for name, tc := range manifest {
		if dirty, extras, reason := manifestOutcome(tc.err); dirty != tc.dirty || extras != tc.extras || reason != tc.reason {
			t.Errorf("manifest %s: got %v/%v/%q", name, dirty, extras, reason)
		}
	}
}

func TestObserveBranchLimitMarksTheSurplusIncomplete(t *testing.T) {
	repo := observeRepo(t)
	for _, n := range []string{"a", "b", "c"} {
		gitFixture(t, repo, "branch", n)
	}
	defer func(n int) { observerBranchLimit = n }(observerBranchLimit)
	observerBranchLimit = 2
	o := observe(t, repo)
	incomplete := 0
	for _, b := range o.Branches {
		if b.Incomplete == "branch-limit" {
			incomplete++
			if b.OID == "" || b.Proof != "" {
				t.Fatalf("%+v", b)
			}
		}
	}
	if len(o.Branches) != 4 || incomplete != 2 {
		t.Fatalf("branches: %+v", o.Branches)
	}
}

// Another linked worktree of the same repository in place of the registered one: its
// administrative directory points back at its own path, not the registered one.
func TestObserveRefusesAnotherLinkedWorktreeAtTheRegisteredPath(t *testing.T) {
	repo := observeRepo(t)
	a := observeAdd(t, repo, "a")
	b := observeAdd(t, repo, "b")
	if err := os.RemoveAll(a); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a, 0700); err != nil {
		t.Fatal(err)
	}
	pointer, err := os.ReadFile(filepath.Join(b, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, ".git"), pointer, 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, b, "switch", "-q", "--ignore-other-worktrees", "a")
	if w := evidenceOf(t, observe(t, repo), a); w.Incomplete == "" || w.Proof != "" {
		t.Fatalf("another worktree's administrative directory was accepted: %+v", w)
	}
}

// A clone of the same history swapped in at the registered path has the registered HEAD and ref
// but is another repository.
func TestObserveRefusesAClonedRepositoryAtTheRegisteredPath(t *testing.T) {
	repo := observeRepo(t)
	w := observeAdd(t, repo, "cloned")
	if err := os.RemoveAll(w); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, filepath.Dir(w), "clone", "-q", repo, "cloned")
	gitFixture(t, w, "checkout", "-q", "-b", "cloned")
	if e := evidenceOf(t, observe(t, repo), w); e.Incomplete != "registration-mismatch" || e.Primary || e.Proof != "" {
		t.Fatalf("%+v", e)
	}
}

// The directory must be at the registered HEAD and ref.
func TestObserveContentRequiresTheRegisteredHeadAndRef(t *testing.T) {
	repo := observeRepo(t)
	w := observeAdd(t, repo, "pos")
	commitIn(t, w, "x", "x\n")
	ctx, _, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSession()
	bin, err := gitExecutable()
	if err != nil {
		t.Fatal(err)
	}
	id, err := gitNativeIdentity(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	head := gitFixture(t, w, "rev-parse", "HEAD")
	for name, tc := range map[string]struct {
		mod  func(*WorktreeEvidence)
		want string
	}{
		"registered":     {func(*WorktreeEvidence) {}, ""},
		"another head":   {func(e *WorktreeEvidence) { e.HeadOID = id.CommitID }, "registration-mismatch"},
		"another ref":    {func(e *WorktreeEvidence) { e.HeadRef = "refs/heads/other" }, "registration-mismatch"},
		"detached state": {func(e *WorktreeEvidence) { e.HeadRef = "" }, "registration-mismatch"},
	} {
		e := WorktreeEvidence{Path: w, HeadOID: head, HeadRef: "refs/heads/pos"}
		tc.mod(&e)
		defaultWorktreeObserver().observeWorktreeContent(ctx, bin, newCommitFacts(gitRead, bin, repo, id.CommitID), id.RepositoryDir, &e)
		if e.Incomplete != tc.want {
			t.Errorf("%s: incomplete %q, want %q", name, e.Incomplete, tc.want)
		}
	}
}

func TestObserveOfflineNeverReachesTheTrackingCoordinator(t *testing.T) {
	repo := observeRepo(t)
	obs := defaultWorktreeObserver()
	obs.candidates = func(context.Context, V3PlanOptions, string, tidyV3NativeObservation) (V3Plan, tidyV3TrackingHosting, hosting.TidyBranchAdvertisement, error) {
		t.Fatal("the offline path reached the tracking evidence")
		return V3Plan{}, nil, hosting.TidyBranchAdvertisement{}, nil
	}
	o, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase})
	if err != nil || o.TrackingSkipped != "tracking-requires-online" || len(o.TrackingRefs) != 0 {
		t.Fatalf("%v %+v", err, o)
	}
}

func TestObserveUnreadableActivityIsIncomplete(t *testing.T) {
	repo := observeRepo(t)
	w := observeAdd(t, repo, "act")
	obs := defaultWorktreeObserver()
	obs.activity = func(string, string, []RetirementStoredFile) (time.Time, error) {
		return time.Time{}, os.ErrPermission
	}
	o, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: observeBase})
	if err != nil {
		t.Fatal(err)
	}
	if e := evidenceOf(t, o, w); e.Incomplete != "activity-unreadable" || !e.LastActivity.IsZero() || e.Proof != "" || e.HeadOID == "" {
		t.Fatalf("%+v", e)
	}
}

func TestLastActivityErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := lastActivity(dir, dir, nil); err == nil {
		t.Error("a missing HEAD file was ignored")
	}
	for _, n := range []string{"HEAD", "index"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "plain"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// A path below a regular file is an error that is not "does not exist".
	if _, err := lastActivity(dir, dir, []RetirementStoredFile{{Path: "plain/below"}}); err == nil {
		t.Error("an unreadable tracked file was ignored")
	}
	if got, err := lastActivity(dir, dir, []RetirementStoredFile{{Path: "deleted"}}); err != nil || got.IsZero() {
		t.Errorf("a deleted tracked file must contribute nothing: %v %v", got, err)
	}
}

// An expired observation deadline must be reported as deadline-exceeded for every remaining
// worktree, never as git-ownership-refused (issue 252); a real ownership refusal keeps its code.
// A cancelled analysis is no verdict (Spec 041); its worktrees read as the jj path reads them,
// observation-failed, and also not as an ownership refusal.
func TestObserveDoneContextIsNotAnOwnershipRefusal(t *testing.T) {
	repo := observeRepo(t)
	var paths []string
	for _, n := range []string{"d1", "d2", "d3"} {
		w := observeAdd(t, repo, n)
		commitIn(t, w, "x", "x\n")
		paths = append(paths, w)
	}
	bin, err := gitExecutable()
	if err != nil {
		t.Fatal(err)
	}
	live, _, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSession()
	id, err := gitNativeIdentity(live, repo)
	if err != nil {
		t.Fatal(err)
	}
	expired, cancelExpired := context.WithDeadline(live, time.Unix(0, 1))
	defer cancelExpired()
	cancelled, cancelNow := context.WithCancel(live)
	cancelNow()
	for name, tc := range map[string]struct {
		ctx  context.Context
		want string
	}{"deadline": {expired, "deadline-exceeded"}, "cancelled": {cancelled, "observation-failed"}} {
		for _, p := range paths {
			// Content that was read before the context ended must not survive the incomplete mark.
			e := WorktreeEvidence{Path: p, HeadOID: gitFixture(t, p, "rev-parse", "HEAD"), HeadRef: "refs/heads/" + filepath.Base(p),
				Dirty: true, IgnoredExtras: true, Ahead: 3, HasOwnCommit: true, AuthorHint: "a", Proof: "ancestor", LastActivity: time.Unix(5, 0)}
			defaultWorktreeObserver().observeWorktreeContent(tc.ctx, bin, newCommitFacts(gitRead, bin, repo, id.CommitID), id.RepositoryDir, &e)
			if e.Incomplete != tc.want {
				t.Errorf("%s %s: incomplete %q, want %s", name, filepath.Base(p), e.Incomplete, tc.want)
			}
			if e.Dirty || e.IgnoredExtras || e.HasOwnCommit || e.Ahead != 0 || e.AuthorHint != "" || e.Proof != "" || !e.LastActivity.IsZero() {
				t.Errorf("%s %s: content kept after the incomplete observation: %+v", name, filepath.Base(p), e)
			}
		}
	}
}

func TestObserveRealOwnershipRefusalKeepsItsReason(t *testing.T) {
	repo := observeRepo(t)
	plain := t.TempDir() // registered path that is no Git checkout
	live, _, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSession()
	bin, err := gitExecutable()
	if err != nil {
		t.Fatal(err)
	}
	id, err := gitNativeIdentity(live, repo)
	if err != nil {
		t.Fatal(err)
	}
	e := WorktreeEvidence{Path: plain, HeadOID: id.CommitID, HeadRef: "refs/heads/x"}
	defaultWorktreeObserver().observeWorktreeContent(live, bin, newCommitFacts(gitRead, bin, repo, id.CommitID), id.RepositoryDir, &e)
	if e.Incomplete != "git-ownership-refused" {
		t.Errorf("incomplete %q, want git-ownership-refused", e.Incomplete)
	}
}
