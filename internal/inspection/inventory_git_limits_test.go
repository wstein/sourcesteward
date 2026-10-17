// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func smallBounds() inventoryBounds {
	b := defaultInventoryBounds()
	return b
}

// fakeGitFor delegates to the real Git except for invocations whose argument list matches the shell
// pattern, which print endless records (to exceed a scan allowance) or block.
func fakeGitFor(t *testing.T, pattern, action string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	fakeGit(t, "case \" $* \" in\n"+pattern+")\n"+action+"\n;;\nesac\nexec "+real+" \"$@\"\n")
}

const endlessNUL = "while :; do printf 'M\\0x\\0'; done"

func TestGitInventoryIndexLimitDegradesTheWorkspace(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 60, "")
	git(t, d, "worktree", "add", "-qb", "side", filepath.Join(t.TempDir(), "side"))
	os.WriteFile(filepath.Join(d, "d000", "file00001.txt"), []byte("staged\n"), 0o644)
	git(t, d, "add", "d000/file00001.txt")
	b := smallBounds()
	b.IndexEntries = 50
	r := gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"disk", "tracked", "staged", "untracked"}}, b)
	w := r.Workspaces[0]
	for _, ws := range r.Workspaces {
		if ws.Path != nil && *ws.Path == d {
			w = ws
		}
	}
	if r.Status != "partial" || w.Tracked.Value != "unknown" || w.Tracked.Reason != txtIndexLimit || w.Conflicts.Value != "unknown" || w.Conflicts.Reason != txtIndexLimit {
		t.Fatalf("%s %+v", r.Status, w)
	}
	if !hasLimitation(r, txtIndexLimit) || len(r.Workspaces) != 2 {
		t.Fatalf("%q ws=%d", r.Boundary.Limitations, len(r.Workspaces))
	}
	// Staged evidence is independent of the index read.
	staged := false
	for _, f := range r.Files {
		if f.Scope == "staged" && f.Path == "d000/file00001.txt" {
			staged = true
		}
		if f.Scope == "disk" {
			t.Fatalf("disk record despite the aborted index read: %+v", f)
		}
	}
	if !staged {
		t.Fatalf("no staged record: %+v", r.Files)
	}
	// The path byte limit trips the same way.
	b = smallBounds()
	b.IndexPathBytes = 100
	r = gitInventoryWith(t, d, InventoryOptions{}, b)
	if r.Status != "partial" || r.Workspaces[0].Tracked.Reason != txtIndexLimit {
		t.Fatalf("%s %+v", r.Status, r.Workspaces[0])
	}
}

func TestGitInventoryScanAndRecordBoundsOnDiskFeeds(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 60, "")
	// FEED-3: check-attr prints about 12 KB for 60 files; the paths and index stay below 3 KB.
	b := smallBounds()
	b.Scan = 3000
	r := gitInventoryWith(t, d, InventoryOptions{}, b)
	if r.Status != "partial" || r.Workspaces[0].Tracked.Value != "unknown" || r.Workspaces[0].Tracked.Reason != txtScan("disk comparison") {
		t.Fatalf("FEED-3 %s %+v", r.Status, r.Workspaces[0])
	}
	// FEED-4: with normalization the attribute check returns early, the index read exceeds the allowance.
	git(t, d, "config", "core.autocrlf", "true")
	b.Scan = 1500
	r = gitInventoryWith(t, d, InventoryOptions{}, b)
	if r.Status != "partial" || len(r.Workspaces) == 0 || r.Workspaces[0].Conflicts.Reason != txtScan("disk comparison") {
		t.Fatalf("FEED-4 %s %+v %+v", r.Status, r.Workspaces, r.Diagnostics)
	}
	// LIM-4: an index record (69 bytes plus the path) longer than the limit stops that feed with its own text.
	git(t, d, "config", "--unset", "core.autocrlf")
	long := manyFilesRepo(t, 20, "longprefix-longprefix-longprefix-")
	b = smallBounds()
	b.Record = 100
	r = gitInventoryWith(t, long, InventoryOptions{}, b)
	if r.Status != "partial" || r.Workspaces[0].Tracked.Reason != txtRecord("disk comparison") {
		t.Fatalf("record %s %+v", r.Status, r.Workspaces)
	}
	// The query is unknown, never clean, when the comparison was not made.
	q := gitInventoryWith(t, long, InventoryOptions{Query: "clean"}, b)
	if q.Status != "partial" || q.Query.Value != "unknown" {
		t.Fatalf("%+v", q)
	}
}

func TestGitInventoryContentBudgetIsPerRunAndChargesTheInspectedWorkspaceFirst(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 30, "")
	side := filepath.Join(canonical(t, t.TempDir()), "side")
	git(t, d, "worktree", "add", "-qb", "side", side)
	b := smallBounds()
	b.ContentBudget = 30*12 + 40 // enough for the inspected workspace and a few files of the other
	r := gitInventoryWith(t, side, InventoryOptions{}, b)
	var inspected, other InventoryWorkspace
	for _, w := range r.Workspaces {
		if w.Path != nil && *w.Path == side {
			inspected = w
		} else {
			other = w
		}
	}
	if r.Status != "ok" || inspected.Tracked.Value != "false" {
		t.Fatalf("the inspected workspace must not be starved: %s %+v", r.Status, inspected)
	}
	if other.Tracked.Value != "unknown" || other.Tracked.Reason != txtBudget {
		t.Fatalf("other %+v", other)
	}
	// The same run from the main checkout compares the main checkout first.
	r = gitInventoryWith(t, d, InventoryOptions{}, b)
	if r.Status != "ok" {
		t.Fatalf("%s %+v", r.Status, r.Workspaces)
	}
	// A single workspace whose own content exceeds the budget is partial, and both passes agree (no changed-repository).
	b.ContentBudget = 100
	r = gitInventoryWith(t, d, InventoryOptions{}, b)
	for _, w := range r.Workspaces {
		if w.Path != nil && *w.Path == d && (w.Tracked.Value != "unknown" || w.Tracked.Reason != txtBudget) {
			t.Fatalf("%+v", w)
		}
	}
	if r.Status != "partial" || len(r.Diagnostics) != 0 {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
}

func TestGitInventorySkipWorktreeAndSymlinkEntries(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 10, "")
	git(t, d, "update-index", "--skip-worktree", "d000/file00001.txt", "d000/file00002.txt")
	os.Remove(filepath.Join(d, "d000", "file00001.txt"))
	r := gitInventory(t, d, InventoryOptions{})
	w := r.Workspaces[0]
	if r.Status != "ok" || w.Tracked.Value != "false" || len(r.Files) != 0 || !hasString(w.Limitations, txtSkipWorktree) {
		t.Fatalf("absent skip-worktree entry: %s %+v %+v", r.Status, w, r.Files)
	}
	// A skip-worktree file that is present and modified is a real edit.
	os.WriteFile(filepath.Join(d, "d000", "file00002.txt"), []byte("edited\n"), 0o644)
	r = gitInventory(t, d, InventoryOptions{})
	if r.Workspaces[0].Tracked.Value != "true" || len(r.Files) != 2 {
		t.Fatalf("present skip-worktree file: %+v %+v", r.Workspaces[0], r.Files)
	}
	q := gitInventory(t, d, InventoryOptions{Query: "clean"})
	if q.Query.Value != "false" {
		t.Fatalf("%+v", q)
	}
	// core.symlinks=false: symlink entries are not compared.
	d2 := repo(t)
	os.Symlink("target", filepath.Join(d2, "link"))
	git(t, d2, "add", "-A")
	git(t, d2, "commit", "-qm", "link")
	git(t, d2, "config", "core.symlinks", "false")
	r = gitInventory(t, d2, InventoryOptions{})
	if r.Status != "partial" || r.Workspaces[0].Tracked.Value != "unknown" || r.Workspaces[0].Tracked.Reason != txtSymlinks {
		t.Fatalf("%s %+v", r.Status, r.Workspaces[0])
	}
}

func TestGitInventoryConflictedIndexUsesTheUnmergedTag(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	os.WriteFile(filepath.Join(d, "f"), []byte("base\n"), 0o644)
	git(t, d, "add", "-A")
	git(t, d, "commit", "-qm", "base")
	git(t, d, "checkout", "-qb", "other")
	os.WriteFile(filepath.Join(d, "f"), []byte("other\n"), 0o644)
	git(t, d, "commit", "-qam", "other")
	git(t, d, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(d, "f"), []byte("main\n"), 0o644)
	git(t, d, "commit", "-qam", "main")
	merge := exec.Command("git", "merge", "other")
	merge.Dir = d
	merge.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	merge.Run() // a conflict is the expected outcome
	r := gitInventory(t, d, InventoryOptions{})
	w := r.Workspaces[0]
	if w.Conflicts.Value != "true" || w.Tracked.Value != "true" {
		t.Fatalf("%+v", w)
	}
	conflicts := 0
	for _, f := range r.Files {
		if f.Change == "conflict" {
			conflicts++
		}
	}
	if conflicts != 2 {
		t.Fatalf("%+v", r.Files)
	}
}

func TestGitInventoryNonUTF8IndexPathIsInvalidMetadata(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	if err := os.WriteFile(filepath.Join(d, "bad\xffname"), []byte("x"), 0o644); err != nil {
		t.Skip("file system rejects non-UTF-8 names")
	}
	git(t, d, "add", "-A")
	git(t, d, "commit", "-qm", "bad")
	r := gitInventory(t, d, InventoryOptions{})
	if r.Status != "error" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "invalid-metadata" {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
}

func TestGitInventoryBufferedCommandKeepsItsCap(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	f, _ := os.OpenFile(filepath.Join(d, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o644)
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(f, "[remote \"%s%04d\"]\n\turl = /nonexistent\n", strings.Repeat("r", 40), i)
	}
	f.Close()
	r := gitInventory(t, d, InventoryOptions{})
	if r.Status != "error" || r.Diagnostics[0].Code != "git-inventory-failed" || r.Diagnostics[0].Message != "cannot observe native remote names" {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
}

func TestInventoryRecordBudgetErrorsWhenWorkspacesAndBasesAloneOverflow(t *testing.T) {
	if _, ok := inventoryRecordBudget(1000, 24); !ok {
		t.Fatal("1024 records fit")
	}
	if _, ok := inventoryRecordBudget(1000, 25); ok {
		t.Fatal("1025 records must be DEG-10")
	}
	if b, _ := inventoryRecordBudget(3, 0); b != 1021 {
		t.Fatal(b)
	}
}

func TestInventoryDeadlineMapsToTimeoutAndLeavesNoProcess(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 5, "")
	pidFile := filepath.Join(t.TempDir(), "pid")
	// Only the attribute-path listing is blocked; every other Git call passes through to the real Git.
	fakeGitFor(t, "*\" ls-files --cached \"*", "echo $$ > "+pidFile+"; exec sleep 30")
	ctx := newTriggeredDeadline()
	go func() { readPIDQuiet(pidFile); ctx.expire() }() // the deadline fires only after the shim is running
	r := Inventory(ctx, d, InventoryOptions{})
	if r.Status != "error" || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "inventory-timeout" {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
	if !processGone(readPID(t, pidFile)) {
		t.Fatal("blocked process survived the deadline")
	}
}

func TestGitInventoryScanBoundOnStoredStagedAndUntrackedFeeds(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 5, "")
	os.WriteFile(filepath.Join(d, "untracked.txt"), []byte("u"), 0o644)
	os.WriteFile(filepath.Join(d, "d000", "file00001.txt"), []byte("staged\n"), 0o644)
	git(t, d, "add", "d000/file00001.txt")
	b := smallBounds()
	b.Scan = 1 << 16
	t.Run("stored", func(t *testing.T) {
		fakeGitFor(t, "*\" diff-tree \"*", endlessNUL)
		r := gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"stored"}}, b)
		if r.Status != "partial" || !hasLimitation(r, txtScan("stored")) || len(r.Files) != 0 {
			t.Fatalf("%s %q %+v", r.Status, r.Boundary.Limitations, r.Files)
		}
	})
	t.Run("staged", func(t *testing.T) {
		fakeGitFor(t, "*\" diff --cached \"*", "while :; do echo noise 1>&2; done")
		r := gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"staged"}}, b)
		w := r.Workspaces[0]
		if r.Status != "partial" || w.Tracked.Value != "unknown" || w.Tracked.Reason != txtScan("staged") {
			t.Fatalf("%s %+v", r.Status, w)
		}
		q := gitInventoryWith(t, d, InventoryOptions{Query: "clean", Scopes: []string{"staged"}}, b)
		if q.Query.Value != "unknown" || q.Status != "partial" {
			t.Fatalf("%+v", q)
		}
		// A non-unknown candidate from another feed still proves dirty.
		q = gitInventoryWith(t, d, InventoryOptions{Query: "dirty", Scopes: []string{"staged", "untracked"}}, b)
		if q.Query.Value != "true" || q.Status != "ok" || !q.Boundary.Complete {
			t.Fatalf("%+v", q)
		}
	})
	t.Run("untracked", func(t *testing.T) {
		fakeGitFor(t, "*\" ls-files --others \"*", "while :; do printf 'x\\0'; done")
		b.Scan = 4096
		r := gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"untracked"}}, b)
		w := r.Workspaces[0]
		if r.Status != "partial" || w.Untracked.Value != "unknown" || w.Untracked.Reason != txtScan("untracked") {
			t.Fatalf("%s %+v", r.Status, w)
		}
	})
}

func TestGitInventoryUntrackedListingStopsAtTheCandidateBound(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	for i := 0; i < 60; i++ {
		os.WriteFile(filepath.Join(d, fmt.Sprintf("u%03d", i)), []byte("x"), 0o644)
	}
	b := smallBounds()
	b.Candidates = 20
	r := gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"untracked"}}, b)
	if r.Status != "partial" || r.Workspaces[0].Untracked.Value != "true" || len(r.Files) != 20 || !hasLimitation(r, txtFilesTruncated(1, true)) {
		t.Fatalf("%s %+v %d %q", r.Status, r.Workspaces[0].Untracked, len(r.Files), r.Boundary.Limitations)
	}
	q := gitInventoryWith(t, d, InventoryOptions{Query: "dirty", Scopes: []string{"untracked"}}, b)
	if q.Status != "ok" || q.Query.Value != "true" || !hasLimitation(q, txtFilesTruncated(1, true)) {
		t.Fatalf("%+v", q)
	}
	// Retained path bytes are capped as well (the same rule, DEG-2).
	b = smallBounds()
	b.CandidatePathBytes = 40
	r = gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"untracked"}}, b)
	if r.Status != "partial" || len(r.Files) == 0 || len(r.Files) > 10 {
		t.Fatalf("%s %d", r.Status, len(r.Files))
	}
}

func TestGitInventoryFileShareFollowsFairShareAndQueryRule(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 3000, "")
	st := readAll(t, d)
	for _, p := range st {
		os.WriteFile(filepath.Join(d, p), []byte("changed\n"), 0o644)
	}
	r := gitInventory(t, d, InventoryOptions{})
	if r.Status != "partial" || r.Boundary.Complete || len(r.Files) != 1021 || !hasLimitation(r, truncatedText(6000-1021)) {
		t.Fatalf("%s files=%d %q", r.Status, len(r.Files), r.Boundary.Limitations)
	}
	for i := 1; i < len(r.Files); i++ {
		a, b := r.Files[i-1], r.Files[i]
		if a.Path > b.Path || a.Path == b.Path && a.Scope >= b.Scope {
			t.Fatalf("not path ordered at %d", i)
		}
	}
	q := gitInventory(t, d, InventoryOptions{Query: "dirty"})
	if q.Status != "ok" || !q.Boundary.Complete || q.Query.Value != "true" || !hasLimitation(q, truncatedText(6000-1021)) {
		t.Fatalf("%s %v %+v", q.Status, q.Boundary.Complete, q.Query)
	}
	q = gitInventory(t, d, InventoryOptions{Query: "clean"})
	if q.Status != "ok" || q.Query.Value != "false" {
		t.Fatalf("%+v", q.Query)
	}
}

func readAll(t *testing.T, d string) []string {
	t.Helper()
	out := []string{}
	_, _, err := newStreamGit(defaultInventoryBounds())(context.Background(), d, nil, "\x00", []string{"ls-files", "-z"}, func(r []byte) error { out = append(out, string(r)); return nil })
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGitInventoryResultBytesAreTrimmedNotFatal(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	long := strings.Repeat("x", 240)
	dir := d
	for i := 0; i < 6; i++ {
		dir = filepath.Join(dir, fmt.Sprintf("%s%d", long, i))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skip("path too long for this file system")
	}
	for i := 0; i < 900; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d", i)), []byte("x"), 0o644); err != nil {
			t.Skip("path too long for this file system")
		}
	}
	r := gitInventory(t, d, InventoryOptions{Scopes: []string{"untracked"}})
	raw, _ := json.Marshal(r)
	if r.Status != "partial" || len(raw) > inventoryResultLimit || !hasLimitation(r, txtResultTrimmed) || len(r.Files) == 0 {
		t.Fatalf("%s %d %d %q", r.Status, len(raw), len(r.Files), r.Boundary.Limitations)
	}
	q := gitInventory(t, d, InventoryOptions{Query: "dirty", Scopes: []string{"untracked"}})
	if q.Status != "ok" || q.Query.Value != "true" || !hasLimitation(q, txtResultTrimmed) {
		t.Fatalf("%s %+v %q", q.Status, q.Query, q.Boundary.Limitations)
	}
}

func TestGitInventoryReferenceScanTruncatesTagsFirstAndPredicatesSeeAll(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	var in strings.Builder
	for i := 0; i < 700; i++ {
		fmt.Fprintf(&in, "create refs/tags/release-tag-number-%04d HEAD\n", i)
	}
	execGit(t, d, in.String(), "update-ref", "--stdin")
	r := gitInventory(t, d, InventoryOptions{})
	if r.Status != "partial" || len(r.References) >= 701 || len(r.References) < 500 || r.References[0].Kind != "local" {
		t.Fatalf("%s refs=%d", r.Status, len(r.References))
	}
	omitted := 701 - len(r.References)
	if !hasLimitation(r, txtRefsTruncated(omitted)) {
		t.Fatalf("%q", r.Boundary.Limitations)
	}
	cost := len(r.Workspaces)
	for _, ref := range r.References {
		cost += referenceCost(ref)
	}
	if cost > inventoryRecordLimit {
		t.Fatalf("cost %d", cost)
	}
	// An omitted tag still exists for ref-exists, and ancestry sees both omitted tags.
	last := "refs/tags/release-tag-number-0699"
	q := gitInventory(t, d, InventoryOptions{Query: "ref-exists", Ref: last})
	truncatedNote := false
	for _, l := range q.Boundary.Limitations {
		truncatedNote = truncatedNote || strings.HasPrefix(l, "reference listing truncated: ")
	}
	if q.Status != "ok" || q.Query.Value != "true" || !truncatedNote || !q.Boundary.Complete {
		t.Fatalf("%s %+v %q", q.Status, q.Query, q.Boundary.Limitations)
	}
	q = gitInventory(t, d, InventoryOptions{Query: "ancestor", From: last, To: "refs/tags/release-tag-number-0698"})
	if q.Status != "ok" || q.Query.Value != "true" {
		t.Fatalf("%s %+v", q.Status, q.Query)
	}
	// A clean/dirty query keeps its status while the reference listing is truncated.
	c := gitInventory(t, d, InventoryOptions{Query: "clean"})
	if c.Status != "ok" || c.Query.Value != "true" || !hasLimitation(c, txtRefsTruncated(omitted)) {
		t.Fatalf("%s %+v %q", c.Status, c.Query, c.Boundary.Limitations)
	}
}

func TestGitInventoryReferenceBoundsAndNamespaces(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	var in strings.Builder
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&in, "create refs/tags/t%03d HEAD\ncreate refs/pull/%d/head HEAD\n", i, i)
	}
	execGit(t, d, in.String(), "update-ref", "--stdin")
	b := smallBounds()
	b.References = 100
	r := gitInventoryWith(t, d, InventoryOptions{}, b)
	if r.Status != "error" || r.Diagnostics[0].Code != "inventory-limit" || r.Diagnostics[0].Message != txtRefScan {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
	// Other namespaces are never scanned, however many exist.
	b.References = 200
	r = gitInventoryWith(t, d, InventoryOptions{}, b)
	if r.Status != "ok" || len(r.References) != 151 {
		t.Fatalf("%s refs=%d", r.Status, len(r.References))
	}
	for _, ref := range r.References {
		if strings.HasPrefix(ref.Name, "refs/pull/") {
			t.Fatal("pull namespace scanned")
		}
	}
	// A scan allowance overrun is the same error, never a partial listing.
	b = smallBounds()
	b.Scan = 500
	r = gitInventoryWith(t, d, InventoryOptions{}, b)
	if r.Status != "error" || r.Diagnostics[0].Message != txtRefScan {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
	q := gitInventoryWith(t, d, InventoryOptions{Query: "ref-exists", Ref: "t001"}, b)
	if q.Status != "error" || q.Diagnostics[0].Code != "inventory-limit" {
		t.Fatalf("%s %+v", q.Status, q.Diagnostics)
	}
}

func TestGitInventoryKindOrderWholeReferencesAndFactsOnlyForReturnedReferences(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	var in strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&in, "create refs/heads/topic-%03d HEAD\n", i)
	}
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&in, "create refs/remotes/origin/r%03d HEAD\n", i)
	}
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&in, "create refs/tags/v%03d HEAD\n", i)
	}
	execGit(t, d, in.String(), "update-ref", "--stdin")
	calls := 0
	var mu sync.Mutex
	run := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		for _, a := range args {
			if a == "merge-base" {
				mu.Lock()
				calls++
				mu.Unlock()
			}
		}
		return runGit(ctx, dir, args...)
	}
	id := Inspect(context.Background(), d)
	b := defaultInventoryBounds()
	r := inventoryGitBounded(context.Background(), id, InventoryOptions{Scopes: []string{"stored"}, Base: "HEAD"}, run, newStreamGit(b), b)
	if r.Status != "partial" {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
	seenRemote, seenTag := false, false
	for _, ref := range r.References {
		switch ref.Kind {
		case "remote":
			seenRemote = true
		case "tag":
			seenTag = true
		case "local":
			if seenRemote || seenTag {
				t.Fatal("local after remote or tag")
			}
		}
		if ref.Kind == "remote" && seenTag {
			t.Fatal("remote after tag")
		}
		if ref.CommitID != nil && ref.MergedIntoBase.Value == "unknown" {
			t.Fatalf("returned reference without facts: %+v", ref)
		}
	}
	if calls != len(r.References) {
		t.Fatalf("ancestry computed for %d references, returned %d", calls, len(r.References))
	}
}

func TestGitInventoryReferenceRecheckDetectsAChangeThroughTheStream(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	b := defaultInventoryBounds()
	real := newStreamGit(b)
	n := 0
	wrapped := func(ctx context.Context, dir string, in io.Reader, delim string, args []string, each func([]byte) error) (int, bool, error) {
		for _, a := range args {
			if a == "for-each-ref" {
				n++
				if n == 2 {
					git(t, d, "branch", "racing")
				}
			}
		}
		return real(ctx, dir, in, delim, args, each)
	}
	id := Inspect(context.Background(), d)
	r := inventoryGitBounded(context.Background(), id, InventoryOptions{Scopes: []string{"stored"}}, runGit, wrapped, b)
	if r.Status != "error" || r.Diagnostics[0].Code != "changed-repository" {
		t.Fatalf("%s %+v", r.Status, r.Diagnostics)
	}
}

func TestGitInventoryIndexBoundsCountEntriesAndPathBytesOnly(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 60, "") // 60 entries with 18-byte paths: 1,080 path bytes
	at := func(entries, pathBytes int) InventoryResult {
		b := smallBounds()
		b.IndexEntries, b.IndexPathBytes = entries, pathBytes
		return gitInventoryWith(t, d, InventoryOptions{}, b)
	}
	if r := at(60, 1080); r.Status != "ok" || r.Workspaces[0].Tracked.Value != "false" {
		t.Fatalf("at the limits: %s %+v", r.Status, r.Workspaces[0])
	}
	if r := at(59, 1080); r.Workspaces[0].Tracked.Reason != txtIndexLimit {
		t.Fatalf("one entry over: %+v", r.Workspaces[0])
	}
	if r := at(60, 1079); r.Workspaces[0].Tracked.Reason != txtIndexLimit {
		t.Fatalf("one path byte over: %+v", r.Workspaces[0])
	}
}

func TestGitInventoryReferenceAndCandidateBoundariesAreExact(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	var in strings.Builder
	for i := 0; i < 49; i++ {
		fmt.Fprintf(&in, "create refs/tags/t%02d HEAD\n", i)
	}
	execGit(t, d, in.String(), "update-ref", "--stdin") // 50 references with main
	b := smallBounds()
	b.References = 50
	if r := gitInventoryWith(t, d, InventoryOptions{}, b); r.Status == "error" {
		t.Fatalf("50 references at the bound: %+v", r.Diagnostics)
	}
	b.References = 49
	if r := gitInventoryWith(t, d, InventoryOptions{}, b); r.Status != "error" || r.Diagnostics[0].Message != txtRefScan {
		t.Fatalf("one over: %s %+v", r.Status, r.Diagnostics)
	}
	u := repo(t)
	for i := 0; i < 60; i++ {
		os.WriteFile(filepath.Join(u, fmt.Sprintf("u%03d", i)), []byte("x"), 0o644)
	}
	b = smallBounds()
	b.Candidates = 60
	if r := gitInventoryWith(t, u, InventoryOptions{Scopes: []string{"untracked"}}, b); r.Status != "ok" || len(r.Files) != 60 {
		t.Fatalf("candidates at the bound: %s %d %q", r.Status, len(r.Files), r.Boundary.Limitations)
	}
	b.Candidates = 59
	if r := gitInventoryWith(t, u, InventoryOptions{Scopes: []string{"untracked"}}, b); r.Status != "partial" || len(r.Files) != 59 || !hasLimitation(r, txtFilesTruncated(1, true)) {
		t.Fatalf("one over: %s %d %q", r.Status, len(r.Files), r.Boundary.Limitations)
	}
	// Even a listing that cannot retain a single record has seen an untracked path.
	b.Candidates = 0
	r := gitInventoryWith(t, u, InventoryOptions{Scopes: []string{"untracked"}}, b)
	if r.Workspaces[0].Untracked.Value != "true" || !hasLimitation(r, txtFilesTruncated(1, true)) {
		t.Fatalf("%+v %q", r.Workspaces[0].Untracked, r.Boundary.Limitations)
	}
}

func TestStoredTruncationCountIsExactAndTrimmingIsCounted(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 60, "")
	b := smallBounds()
	b.Candidates = 20
	r := gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"stored"}}, b)
	if r.Status != "partial" || len(r.Files) != 20 || !hasLimitation(r, txtFilesTruncated(40, false)) {
		t.Fatalf("stored count is exact: %s %d %q", r.Status, len(r.Files), r.Boundary.Limitations)
	}
	// Records dropped to fit the 1 MiB bound are part of the same count, and TXT-6 follows.
	long := repo(t)
	dir := long
	for i := 0; i < 6; i++ {
		dir = filepath.Join(dir, fmt.Sprintf("%s%d", strings.Repeat("x", 240), i))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skip("path too long")
	}
	for i := 0; i < 900; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d", i)), []byte("x"), 0o644); err != nil {
			t.Skip("path too long")
		}
	}
	r = gitInventory(t, long, InventoryOptions{Scopes: []string{"untracked"}})
	if !hasLimitation(r, txtResultTrimmed) || !hasLimitation(r, txtFilesTruncated(900-len(r.Files), false)) {
		t.Fatalf("returned %d of 900: %q", len(r.Files), r.Boundary.Limitations)
	}
}

func TestResultTrimmingNeverDropsBranchesOrRemoteReferences(t *testing.T) {
	isolatedHome(t)
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	prefix := ""
	for i := 0; i < 11; i++ {
		prefix += strings.Repeat("p", 250) + "/"
	}
	var in strings.Builder
	for i := 0; i < 450; i++ {
		fmt.Fprintf(&in, "create refs/heads/%sb%03d HEAD\n", prefix, i)
	}
	if out, err := execGitErr(d, in.String(), "update-ref", "--stdin"); err != nil {
		t.Skipf("file system rejects the long reference names: %s", out)
	}
	r := gitInventory(t, d, InventoryOptions{})
	if r.Status != "error" || r.Diagnostics[0].Code != "inventory-limit" {
		t.Fatalf("local references that cannot fit must be an error, not silently dropped: %s refs=%d %+v", r.Status, len(r.References), r.Diagnostics)
	}
}

func execGitErr(dir, stdin string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-c", "maintenance.auto=false"}, args...)...)
	c.Dir = dir
	c.Stdin = strings.NewReader(stdin)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	b, err := c.CombinedOutput()
	return string(b), err
}

func TestFairShareBoundaries(t *testing.T) {
	for _, tc := range []struct{ files, refs, budget, wantFiles, wantRefs int }{
		{500, 500, 1000, 500, 500}, // exactly the budget
		{501, 500, 1000, 500, 500}, // files above half, references fit in the other half exactly
		{499, 501, 1000, 499, 501}, // files within half
		{500, 600, 1000, 500, 500}, // files == half, references overflow
		{600, 500, 1000, 500, 500}, // references == the other half
		{600, 501, 1001, 500, 501}, // both overflow, odd budget: files get the floor
		{501, 501, 1001, 500, 501}, // just above the equal split
		{0, 2000, 1000, 0, 1000},   // no files: references take the whole budget
		{2000, 0, 1000, 1000, 0},   // no references: files take the whole budget
		{1000, 1, 1000, 999, 1},    // references fit in the other half
		{1, 1000, 1000, 1, 999},
	} {
		f, r := fairShare(tc.files, tc.refs, tc.budget)
		if f != tc.wantFiles || r != tc.wantRefs {
			t.Fatalf("%+v got %d %d", tc, f, r)
		}
	}
}

func TestInventoryCancellationStopsABlockedStreamAndLeavesNoProcess(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 5, "")
	pidFile := filepath.Join(t.TempDir(), "pid")
	fakeGitFor(t, "*\" ls-files --cached \"*", "echo $$ > "+pidFile+"; exec sleep 30")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan InventoryResult, 1)
	go func() { done <- Inventory(ctx, d, InventoryOptions{}) }()
	pid := readPID(t, pidFile) // the shim signals readiness before the clock matters
	cancel()
	select {
	case r := <-done:
		if r.Status != "error" || r.Diagnostics[0].Code != "canceled" {
			t.Fatalf("%s %+v", r.Status, r.Diagnostics)
		}
	case <-time.After(120 * time.Second):
		t.Fatal("cancellation did not stop the blocked stream")
	}
	if !processGone(pid) {
		t.Fatal("blocked process survived")
	}
}

func TestBareRepositoryRegistrationsAreNotCheckoutEntries(t *testing.T) {
	isolatedHome(t)
	src := manyFilesRepo(t, 3, "")
	root := canonical(t, t.TempDir())
	bare := filepath.Join(root, "bare.git")
	git(t, src, "clone", "-q", "--bare", src, bare)
	wt := filepath.Join(root, "wt")
	git(t, bare, "worktree", "add", "-q", wt, "-b", "side")
	r, calls := recordingInventory(t, wt, InventoryOptions{})
	var bareEntry InventoryWorkspace
	for _, w := range r.Workspaces {
		if w.Path != nil && *w.Path == bare {
			bareEntry = w
		}
	}
	if r.Status != "ok" || len(r.Workspaces) != 2 || bareEntry.Path == nil || bareEntry.Registration != "present" || !hasString(bareEntry.Limitations, "bare repository has no disk checkout") || hasString(bareEntry.Limitations, txtCheckoutMissing) {
		t.Fatalf("%s %+v %q diag=%+v", r.Status, r.Workspaces, r.Boundary.Limitations, r.Diagnostics)
	}
	if bad := callsIn(calls, bare, "ls-files", "diff", "check-attr"); len(bad) != 0 {
		t.Fatalf("content commands ran against the bare repository: %+v", bad)
	}
	if q := gitInventory(t, wt, InventoryOptions{Query: "clean"}); q.Status != "ok" || q.Query.Value != "true" {
		t.Fatalf("%s %+v", q.Status, q.Query)
	}
}

func TestCheckoutFallbackNeverAnswersAQuery(t *testing.T) {
	isolatedHome(t)
	work, _ := separateGitDirRepo(t)
	id := Inspect(context.Background(), work)
	b := defaultInventoryBounds()
	real := newStreamGit(b)
	// The inspected checkout fails the candidate checks (its Git processes fail after the identity).
	failing := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if dir == work && len(args) > 0 && args[len(args)-1] != "" && strings.Contains(strings.Join(args, " "), "rev-parse") {
			return "", -1, fmt.Errorf("planted failure")
		}
		return runGit(ctx, dir, args...)
	}
	for _, query := range []string{"clean", "dirty"} {
		r := inventoryGitBounded(context.Background(), id, InventoryOptions{Query: query, Scopes: []string{"disk", "tracked", "untracked"}}, failing, real, b)
		if r.Query == nil || r.Query.Value != "unknown" || r.Status != "partial" {
			t.Fatalf("%s must not be answered without observing the checkout: %s %+v %+v", query, r.Status, r.Query, r.Diagnostics)
		}
	}
	r := inventoryGitBounded(context.Background(), id, InventoryOptions{Scopes: []string{"disk", "tracked", "untracked"}}, failing, real, b)
	if r.Status != "partial" || !hasLimitation(r, txtCheckoutMissing) {
		t.Fatalf("%s %q %+v", r.Status, r.Boundary.Limitations, r.Diagnostics)
	}
}

func TestCheckoutCandidateRequiresTheSameHead(t *testing.T) {
	isolatedHome(t)
	work, gitDir := separateGitDirRepo(t)
	id := Inspect(context.Background(), work)
	b := defaultInventoryBounds()
	marker := "worktree " + gitDir + "\x00HEAD "
	// The registration lists another commit for the Git directory entry than the checkout has.
	lying := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		out, code, err := runGit(ctx, dir, args...)
		if i := strings.Index(out, marker); i >= 0 && err == nil {
			start := i + len(marker)
			out = out[:start] + strings.Repeat("a", 40) + out[start+40:]
		}
		return out, code, err
	}
	r := inventoryGitBounded(context.Background(), id, InventoryOptions{}, lying, newStreamGit(b), b)
	if r.Status != "partial" || !hasLimitation(r, txtCheckoutMissing) {
		t.Fatalf("a candidate with another HEAD than its registration must fall back to CK-2: %s %q %+v", r.Status, r.Boundary.Limitations, r.Diagnostics)
	}
	for _, w := range r.Workspaces {
		if w.Path != nil && *w.Path == work {
			t.Fatalf("described a checkout whose HEAD differs from the registration: %+v", w)
		}
	}
}

func TestHashDiskRequiresExactlyTheStatSize(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f")
	os.WriteFile(f, []byte("0123456789"), 0o644)
	for _, tc := range []struct {
		size int64
		ok   bool
	}{{10, true}, {11, false}, {9, false}, {0, false}} {
		h, _ := os.Open(f)
		err := hashDisk(context.Background(), h, tc.size, sha1.New())
		h.Close()
		var changed *changedDuringError
		if (err == nil) != tc.ok || (!tc.ok && !errors.As(err, &changed)) {
			t.Fatalf("size %d: %v", tc.size, err)
		}
	}
}

func TestTruncateReferencesKeepsWholeReferencesThatFitExactly(t *testing.T) {
	ref := func() InventoryReference { return InventoryReference{AddedTargets: make([]InventoryTarget, 1)} } // cost 2
	refs := []InventoryReference{ref(), ref(), ref()}
	for budget, want := range map[int]int{0: 0, 1: 0, 2: 1, 3: 1, 4: 2, 5: 2, 6: 3, 7: 3} {
		if got := len(truncateReferences(refs, budget)); got != want {
			t.Fatalf("budget %d keeps %d, want %d", budget, got, want)
		}
	}
}

func TestDeletedTrackedFileIsReportedDeletedNotSkipped(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 5, "")
	os.Remove(filepath.Join(d, "d000", "file00002.txt"))
	r := gitInventory(t, d, InventoryOptions{})
	found := false
	for _, f := range r.Files {
		found = found || f.Path == "d000/file00002.txt" && f.Scope == "disk" && f.Change == "deleted"
	}
	if !found || r.Workspaces[0].Tracked.Value != "true" || hasString(r.Workspaces[0].Limitations, txtSkipWorktree) {
		t.Fatalf("%+v %+v", r.Files, r.Workspaces[0])
	}
}

func TestGitInventoryIndexReadAbortsOnItsOwnWhenNoComparisonIsRequested(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 60, "")
	b := smallBounds()
	b.IndexEntries = 50
	// Without disk or tracked scopes the attribute check does not run; the index read is for conflicts only.
	r := gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"staged"}}, b)
	w := r.Workspaces[0]
	if r.Status != "ok" || w.Conflicts.Value != "unknown" || w.Conflicts.Reason != txtIndexLimit || !hasString(w.Limitations, txtIndexLimit) {
		t.Fatalf("%s %+v", r.Status, w)
	}
	b = smallBounds()
	b.IndexPathBytes = 100
	if r := gitInventoryWith(t, d, InventoryOptions{Scopes: []string{"staged"}}, b); r.Workspaces[0].Conflicts.Reason != txtIndexLimit {
		t.Fatalf("%+v", r.Workspaces[0])
	}
}

func TestContentBudgetEqualToTheFileSizeIsEnough(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 1, "")
	size := len("content 0\n")
	b := smallBounds()
	b.ContentBudget = size
	if r := gitInventoryWith(t, d, InventoryOptions{}, b); r.Status != "ok" || r.Workspaces[0].Tracked.Value != "false" {
		t.Fatalf("budget equal to the size: %s %+v", r.Status, r.Workspaces[0])
	}
	b.ContentBudget = size - 1
	if r := gitInventoryWith(t, d, InventoryOptions{}, b); r.Workspaces[0].Tracked.Reason != txtBudget {
		t.Fatalf("one byte short: %+v", r.Workspaces[0])
	}
}

func TestDiffRecordsAcceptUnmergedOnlyOutsideTheStoredScope(t *testing.T) {
	if _, err := parseGitInventoryDiff("U\x00f\x00", "staged"); err != nil {
		t.Fatalf("staged accepts an unmerged record: %v", err)
	}
	if _, err := parseGitInventoryDiff("U\x00f\x00", "stored"); err == nil {
		t.Fatal("a stored delta never contains an unmerged record")
	}
}

// A filter driver whose name is not valid UTF-8 can neither be neutralized nor counted as a transformation
// by a locale-dependent regular expression: the inventory refuses it as invalid metadata (DEG-14).
func TestGitInventoryRefusesNonUTF8FilterConfigurationAndAttributes(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 3, "")
	f, err := os.OpenFile(filepath.Join(d, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("[filter \"\xff\x80\"]\n\tclean = cat\n")
	f.Close()
	for _, locale := range []string{"C", "C.UTF-8", "en_US.UTF-8"} {
		t.Setenv("LC_ALL", locale)
		r := gitInventory(t, d, InventoryOptions{})
		if r.Status != "error" || r.Diagnostics[0].Code != "invalid-metadata" {
			t.Fatalf("LC_ALL=%s: a non-UTF-8 filter name must be refused, not treated as authoritative: %s %+v tracked=%+v", locale, r.Status, r.Diagnostics, r.Workspaces)
		}
	}
	// A non-ASCII but valid name is neutralized and counted as a transformation.
	d2 := manyFilesRepo(t, 3, "")
	git(t, d2, "config", "filter.日本語.clean", "cat")
	os.WriteFile(filepath.Join(d2, ".gitattributes"), []byte("d000/file00001.txt filter=日本語\n"), 0o644)
	r := gitInventory(t, d2, InventoryOptions{})
	if r.Status != "partial" || r.Workspaces[0].Tracked.Value != "unknown" || !strings.Contains(r.Workspaces[0].Tracked.Reason, "transformations") {
		t.Fatalf("%s %+v", r.Status, r.Workspaces[0])
	}
	// A non-UTF-8 attribute value is refused as invalid metadata, not read as a harmless value.
	d3 := manyFilesRepo(t, 3, "")
	os.WriteFile(filepath.Join(d3, ".gitattributes"), []byte("d000/file00001.txt filter=\xff\xfe\n"), 0o644)
	r = gitInventory(t, d3, InventoryOptions{})
	if r.Status != "error" || r.Diagnostics[0].Code != "invalid-metadata" {
		t.Fatalf("non-UTF-8 attribute value: %s %+v", r.Status, r.Diagnostics)
	}
}

// An attribute naming a filter driver that is not configured does not change how files are compared: the
// driver cannot run, so comparisons stay authoritative.
func TestGitInventoryUnconfiguredFilterAttributeKeepsComparisonsAuthoritative(t *testing.T) {
	isolatedHome(t)
	d := manyFilesRepo(t, 3, "")
	os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("d000/file00001.txt filter=nobody\n"), 0o644)
	os.WriteFile(filepath.Join(d, "d000", "file00001.txt"), []byte("edited\n"), 0o644)
	r := gitInventory(t, d, InventoryOptions{})
	w := r.Workspaces[0]
	if r.Status != "ok" || w.Tracked.Value != "true" || strings.Contains(w.Tracked.Reason, "transformations") {
		t.Fatalf("%s %+v", r.Status, w)
	}
}

// Generic scan errors name the operating system condition of a failed Git process, never a path.
func TestScanErrorMessagesCarryOnlyTheProcessCondition(t *testing.T) {
	process := &processError{"git process failed: resource temporarily unavailable"}
	if got := withCause("cannot observe Git index", process).Error(); got != "cannot observe Git index: git process failed: resource temporarily unavailable" {
		t.Fatal(got)
	}
	pathError := &os.PathError{Op: "open", Path: "/home/user/secret-repo/.git/index", Err: os.ErrPermission}
	if got := withCause("cannot observe Git index", pathError).Error(); got != "cannot observe Git index" {
		t.Fatalf("an arbitrary error must not reach the message: %q", got)
	}
	// The reference scan uses the same wrapper.
	s := gitInventorySession{ctx: context.Background(), format: "sha1", bounds: defaultInventoryBounds(),
		stream: func(context.Context, string, io.Reader, string, []string, func([]byte) error) (int, bool, error) {
			return -1, false, process
		}}
	_, _, code, msg := s.scanReferences("/ignored", true)
	if code != "git-inventory-failed" || msg != "cannot enumerate Git references: git process failed: resource temporarily unavailable" {
		t.Fatalf("%s %q", code, msg)
	}
	s.stream = func(context.Context, string, io.Reader, string, []string, func([]byte) error) (int, bool, error) {
		return -1, false, pathError
	}
	if _, _, _, msg := s.scanReferences("/ignored", true); msg != "cannot enumerate Git references" {
		t.Fatalf("%q", msg)
	}
}

func TestStreamStartFailureNamesNoPath(t *testing.T) {
	dir := t.TempDir()
	// A shim whose interpreter does not exist: the start error from the operating system names the file.
	bad := filepath.Join(dir, "git")
	os.WriteFile(bad, []byte("#!/nonexistent/interpreter\n"), 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := newStreamGit(defaultInventoryBounds())
	_, _, err := s(context.Background(), t.TempDir(), nil, "\x00", []string{"ls-files"}, func([]byte) error { return nil })
	var process *processError
	if !errors.As(err, &process) || process.msg != "git process failed: no such file or directory" || strings.Contains(err.Error(), dir) {
		t.Fatalf("%v", err)
	}
}

// A tracked symlink that is swapped after the Lstat is a change during the inventory, without the path.
func TestReadTrackedLinkFailureIsAChangeDuringTheInventory(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "regular"), []byte("x"), 0o644)
	root, err := os.OpenRoot(d)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, err = readTrackedLink(root, "regular") // not a symlink: the entry changed between Lstat and Readlink
	var changed *changedDuringError
	if !errors.As(err, &changed) || strings.Contains(err.Error(), d) || changed.msg != "tracked content changed during inventory" {
		t.Fatalf("%v", err)
	}
	os.Symlink("target", filepath.Join(d, "link"))
	if link, err := readTrackedLink(root, "link"); err != nil || link != "target" {
		t.Fatalf("%q %v", link, err)
	}
}

// A same-size rewrite while a file is hashed is a change even when the modification time is restored: the
// change time (set by the kernel) and the file identity are compared as well (LIM-10).
func TestRewriteDuringHashingIsAChange(t *testing.T) {
	isolatedHome(t)
	for _, mode := range []string{"in-place", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			d := manyFilesRepo(t, 3, "")
			target := "d000/file00001.txt"
			full := filepath.Join(d, target)
			original, _ := os.ReadFile(full)
			info, _ := os.Stat(full)
			inventoryBeforeHash = func(path string) {
				if path != target {
					return
				}
				inventoryBeforeHash = nil
				time.Sleep(30 * time.Millisecond) // let the coarse filesystem clock advance past the Lstat
				rewritten := []byte(strings.Repeat("z", len(original)))
				if mode == "in-place" {
					os.WriteFile(full, rewritten, 0o644)
				} else {
					os.WriteFile(full+".new", rewritten, 0o644)
					os.Rename(full+".new", full)
				}
				os.Chtimes(full, info.ModTime(), info.ModTime()) // the modification time is restored: a tie
			}
			t.Cleanup(func() { inventoryBeforeHash = nil })
			r := gitInventory(t, d, InventoryOptions{})
			if r.Status != "error" || r.Diagnostics[0].Code != "changed-repository" || r.Diagnostics[0].Message != "tracked content changed during inventory" {
				t.Fatalf("%s %+v", r.Status, r.Diagnostics)
			}
		})
	}
}

// Every configured driver key counts: a filter with only a process or required setting is a transformation.
func TestGitInventoryProcessOnlyAndRequiredOnlyFiltersAreTransformations(t *testing.T) {
	isolatedHome(t)
	for _, key := range []string{"process", "required"} {
		d := manyFilesRepo(t, 3, "")
		git(t, d, "config", "filter.solo."+key, map[string]string{"process": "cat", "required": "true"}[key])
		os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("d000/file00001.txt filter=solo\n"), 0o644)
		r := gitInventory(t, d, InventoryOptions{})
		if r.Status != "partial" || r.Workspaces[0].Tracked.Value != "unknown" || !strings.Contains(r.Workspaces[0].Tracked.Reason, "transformations") {
			t.Fatalf("%s: %s %+v", key, r.Status, r.Workspaces[0])
		}
	}
}

// Git runs a driver named "unset" or "unspecified" for filter=unset: check-attr prints the same word for
// the real states, so a configured driver of that name must count as a transformation.
func TestGitInventoryFilterDriversNamedLikeAttributeStatesAreTransformations(t *testing.T) {
	isolatedHome(t)
	for _, name := range []string{"unset", "unspecified"} {
		d := manyFilesRepo(t, 3, "")
		git(t, d, "config", "filter."+name+".clean", "cat")
		os.WriteFile(filepath.Join(d, ".gitattributes"), []byte("d000/file00001.txt filter="+name+"\n"), 0o644)
		os.WriteFile(filepath.Join(d, "d000", "file00001.txt"), []byte("edited\n"), 0o644)
		r := gitInventory(t, d, InventoryOptions{})
		w := r.Workspaces[0]
		if r.Status != "partial" || w.Tracked.Value != "unknown" || !strings.Contains(w.Tracked.Reason, "transformations") {
			t.Fatalf("filter=%s: %s %+v", name, r.Status, w)
		}
	}
}
