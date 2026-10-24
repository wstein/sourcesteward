// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func jjCLI(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("jj", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "JJ_CONFIG=", "JJ_USER=Test", "JJ_EMAIL=test@example.invalid")
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("jj %v: %s %v", args, b, err)
	}
	return string(b)
}

// observeJJRepo is a repository with bookmark main one commit behind the working copy
// and a bookmark feature that is one commit ahead of main.
func observeJJRepo(t *testing.T) string { return observeJJRepoWith(t, false) }

// observeJJRepoWith makes the repository colocated (a Git directory next to .jj) or not.
func observeJJRepoWith(t *testing.T, colocate bool) string {
	t.Helper()
	if _, err := exec.LookPath("jj"); err != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal(err)
		}
		t.Skip("jj unavailable")
	}
	root := gitTempDir(t)
	repo := filepath.Join(root, "repo")
	init := []string{"git", "init", repo}
	if colocate {
		init = append(init, "--colocate")
	} else {
		init = append(init, "--no-colocate")
	}
	jjCLI(t, root, init...)
	if err := os.WriteFile(filepath.Join(repo, "file"), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jjCLI(t, repo, "commit", "-m", "base")
	jjCLI(t, repo, "bookmark", "create", "main", "-r", "@-")
	if err := os.WriteFile(filepath.Join(repo, "feature"), []byte("work\n"), 0600); err != nil {
		t.Fatal(err)
	}
	jjCLI(t, repo, "commit", "-m", "feature work")
	jjCLI(t, repo, "bookmark", "create", "feature", "-r", "@-")
	return repo
}

// observeJJAdd adds a workspace whose working copy is an empty change on main.
func observeJJAdd(t *testing.T, repo, name string) string {
	t.Helper()
	p := filepath.Join(gitTempDir(t), name)
	jjCLI(t, repo, "workspace", "add", "--name", name, "-r", "main", p)
	return p
}

func observeJJ(t *testing.T, repo string) WorktreeObservation {
	t.Helper()
	o, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: repo, Base: "main"})
	if err != nil {
		t.Skipf("native jj observation unavailable: %v", err)
	}
	return o
}

func TestObserveJJRegistrationAndContentFacts(t *testing.T) {
	repo := observeJJRepo(t)
	clean := observeJJAdd(t, repo, "clean")
	dirty := observeJJAdd(t, repo, "dirty")
	extras := observeJJAdd(t, repo, "extras")
	deleted := observeJJAdd(t, repo, "deleted")
	gone := observeJJAdd(t, repo, "gone")
	if err := os.WriteFile(filepath.Join(dirty, "file"), []byte("edited\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extras, "scratch"), []byte("untracked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(deleted, "file")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Minute)
	o := observeJJ(t, repo)
	if o.Backend != "jj" || o.Base != "main" || o.BaseOID == "" || o.TrackingSkipped != "tracking-unsupported-for-jj" || len(o.TrackingRefs) != 0 {
		t.Fatalf("header: %+v", o)
	}
	p := evidenceOf(t, o, repo)
	if !p.Primary || p.Incomplete != "" || p.HeadRef != "" || p.Locked || p.Proof != "" || !p.LastActivity.IsZero() {
		t.Fatalf("primary: %+v", p)
	}
	c := evidenceOf(t, o, clean)
	if c.Primary || c.Incomplete != "" || c.Dirty || c.IgnoredExtras || c.HeadRef != "" || c.Locked || c.Proof != "empty-working-copy-integrated-parent" || c.AuthorHint != "Test" || c.HasOwnCommit || c.LastActivity.Before(before) {
		t.Fatalf("clean: %+v", c)
	}
	if d := evidenceOf(t, o, dirty); d.Incomplete != "" || !d.Dirty || d.LastActivity.IsZero() || d.Proof != "empty-working-copy-integrated-parent" {
		t.Fatalf("dirty: %+v", d)
	}
	if e := evidenceOf(t, o, extras); e.Incomplete != "" || !e.IgnoredExtras || e.Dirty {
		t.Fatalf("extras: %+v", e)
	}
	if d := evidenceOf(t, o, deleted); d.Incomplete != "" || !d.Dirty {
		t.Fatalf("deleted: %+v", d)
	}
	g := evidenceOf(t, o, gone)
	if !g.DirMissing || !g.Prunable || g.Incomplete != "" || g.Proof != "" {
		t.Fatalf("missing directory: %+v", g)
	}
}

func TestObserveJJBookmarks(t *testing.T) {
	repo := observeJJRepo(t)
	o := observeJJ(t, repo)
	by := map[string]BranchEvidence{}
	for _, b := range o.Branches {
		by[b.Name] = b
	}
	if len(by) != 2 {
		t.Fatalf("bookmarks: %+v", o.Branches)
	}
	if b := by["main"]; !b.IsBase || b.Proof != "ancestor" || b.Ahead != 0 || b.AuthorHint != "Test" || b.Incomplete != "" || b.OID != o.BaseOID {
		t.Fatalf("main: %+v", b)
	}
	if b := by["feature"]; b.IsBase || b.Proof != "none" || b.Ahead != 1 || b.Incomplete != "" {
		t.Fatalf("feature: %+v", b)
	}
	jjCLI(t, repo, "bookmark", "create", "merged", "-r", "main")
	for _, b := range observeJJ(t, repo).Branches {
		if b.Name == "merged" && (b.Proof != "ancestor" || b.Ahead != 0) {
			t.Fatalf("merged: %+v", b)
		}
	}
}

// The workspace the observation starts from cannot be compared with itself: it is Incomplete, not clean.
func TestObserveJJSecondaryWorkspaceAsSource(t *testing.T) {
	repo := observeJJRepo(t)
	second := observeJJAdd(t, repo, "second")
	o, err := ObserveWorktrees(context.Background(), ObserveOptions{Path: second, Base: "main"})
	if err != nil {
		t.Skipf("native jj observation unavailable: %v", err)
	}
	if w := evidenceOf(t, o, second); w.Incomplete != "source-workspace" || w.Primary || w.Proof != "" {
		t.Fatalf("%+v", w)
	}
	if w := evidenceOf(t, o, repo); !w.Primary || w.Incomplete != "" {
		t.Fatalf("%+v", w)
	}
}

func TestObserveJJOnlineHasNoTracking(t *testing.T) {
	repo := observeJJRepo(t)
	obs := defaultWorktreeObserver()
	obs.candidates = func(context.Context, V3PlanOptions, string, tidyV3NativeObservation) (V3Plan, tidyV3TrackingHosting, hosting.TidyBranchAdvertisement, error) {
		t.Fatal("jj reached the Git tracking evidence")
		return V3Plan{}, nil, hosting.TidyBranchAdvertisement{}, nil
	}
	o, err := obs.observe(context.Background(), ObserveOptions{Path: repo, Base: "main", Online: true, Remote: "origin"})
	if err != nil {
		t.Skipf("native jj observation unavailable: %v", err)
	}
	if o.TrackingSkipped != "tracking-unsupported-for-jj" || len(o.TrackingRefs) != 0 {
		t.Fatalf("%+v", o)
	}
}

func TestObserveJJWorkspaceLimits(t *testing.T) {
	repo := observeJJRepo(t)
	a := observeJJAdd(t, repo, "a")
	big := observeJJAdd(t, repo, "big")
	for i := 0; i < retirementV2ManifestEntries+1; i++ {
		if err := os.WriteFile(filepath.Join(big, fmt.Sprintf("f%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	o := observeJJ(t, repo)
	if w := evidenceOf(t, o, big); w.Incomplete != "retirement-file-limit" || w.Dirty || w.IgnoredExtras || w.Proof != "" {
		t.Fatalf("a workspace above the manifest bound must be Incomplete: %+v", w)
	}
	if w := evidenceOf(t, o, a); w.Incomplete != "" {
		t.Fatalf("one workspace's limit hid another: %+v", w)
	}
}

func TestObserveJJContentOutcomeTable(t *testing.T) {
	other := fmt.Errorf("x")
	for name, tc := range map[string]struct {
		err    error
		dirty  bool
		extras bool
		reason string
	}{
		"clean":                  {nil, false, false, ""},
		"unknown payload":        {errRetirementV2UnknownPayload, false, true, ""},
		"stored content differs": {errRetirementStoredDiffers, true, false, ""},
		"stored file missing":    {errRetirementV2StoredMissing, true, false, ""},
		"file limit":             {failure("retirement-file-limit", "m"), false, false, "retirement-file-limit"},
		"unknown error":          {other, false, false, "observation-failed"},
		"deadline":               {context.DeadlineExceeded, false, false, "deadline-exceeded"},
	} {
		if dirty, extras, reason := manifestOutcome(tc.err); dirty != tc.dirty || extras != tc.extras || reason != tc.reason {
			t.Errorf("%s: got %v/%v/%q", name, dirty, extras, reason)
		}
	}
}

func TestJJProofOutcome(t *testing.T) {
	for in, want := range map[string]string{"native-ancestry": "ancestor", "empty-working-copy-integrated-parent": "empty-working-copy-integrated-parent", "": "none"} {
		if got := jjProof(in); got != want {
			t.Errorf("jjProof(%q) = %q, want %q", in, got, want)
		}
	}
}

// A secondary workspace of a colocated repository carries a .git file and .jj/.gitignore, which
// the jj retirement admission does not hold: the observer reports them as extras (retained).
func TestObserveJJColocatedSecondaryWorkspaceHasExtras(t *testing.T) {
	repo := observeJJRepoWith(t, true)
	second := observeJJAdd(t, repo, "second")
	if w := evidenceOf(t, observeJJ(t, repo), second); w.Incomplete != "" || !w.IgnoredExtras {
		t.Fatalf("%+v", w)
	}
}

// TestObserveJJManyWorkspaces measures the jj observation of a primary workspace and twenty
// secondary ones of ten tracked files each; the time is logged.
func TestObserveJJManyWorkspaces(t *testing.T) {
	if testing.Short() {
		t.Skip("builds 21 workspaces")
	}
	repo := observeJJRepo(t)
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(repo, fmt.Sprintf("tracked%d", i)), []byte("x\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	jjCLI(t, repo, "commit", "-m", "tracked files")
	jjCLI(t, repo, "bookmark", "move", "main", "--to", "@-")
	var paths []string
	for i := 0; i < 20; i++ {
		paths = append(paths, observeJJAdd(t, repo, fmt.Sprintf("w%02d", i)))
	}
	start := time.Now()
	o := observeJJ(t, repo)
	t.Logf("jj: 21 workspaces, ten tracked files each: %v", time.Since(start))
	for _, p := range paths {
		if w := evidenceOf(t, o, p); w.Incomplete != "" {
			t.Fatalf("%+v", w)
		}
	}
}

// The jj observation writes nothing: not the repository (operation log, operation heads, Git refs
// of the colocated store), not a workspace's files or working-copy state. jj itself runs only in
// the session's read-only sandbox, which refuses to start without proving that writes fail, so the
// run cannot be repeated without it; the snapshot shows the result. The harness can fail: a plain
// jj status snapshots the working copy and records an operation.
func TestObserveJJWritesNothing(t *testing.T) {
	repo := observeJJRepoWith(t, true)
	dirty := observeJJAdd(t, repo, "dirty")
	extras := observeJJAdd(t, repo, "extras")
	if err := os.WriteFile(filepath.Join(dirty, "file"), []byte("edited, not snapshotted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extras, "scratch"), []byte("untracked, not snapshotted\n"), 0600); err != nil {
		t.Fatal(err)
	}
	roots := []string{repo, dirty, extras}
	before := treeSnapshot(t, roots...)
	observeJJ(t, repo)
	if !sameSnapshot(t, before, treeSnapshot(t, roots...)) {
		t.Fatal("the jj observation changed the repository, the operation log, a working copy or a ref")
	}
	jjCLI(t, dirty, "status")
	if sameSnapshot(t, before, treeSnapshot(t, roots...)) {
		t.Fatal("the snapshot comparison cannot see a jj working-copy snapshot")
	}
}

func TestJJBranchMapping(t *testing.T) {
	hist := map[string]inspection.JJHistory{"c1": {Ahead: 2, Author: "Ana"}}
	ok := inspection.JJTidyCandidate{Name: "feat", CommitID: "c1", Proof: "native-ancestry"}
	if b := jjBranch(ok, "main", 0, hist); b.Incomplete != "" || b.Proof != "ancestor" || b.Ahead != 2 || b.AuthorHint != "Ana" || b.IsBase || b.OID != "c1" {
		t.Errorf("complete: %+v", b)
	}
	if b := jjBranch(inspection.JJTidyCandidate{Name: "main", CommitID: "c1"}, "main", 0, hist); !b.IsBase || b.Proof != "none" {
		t.Errorf("base: %+v", b)
	}
	for name, tc := range map[string]struct {
		cand   inspection.JJTidyCandidate
		index  int
		reason string
	}{
		"limit":            {ok, observerBranchLimit, "branch-limit"},
		"ambiguous target": {inspection.JJTidyCandidate{Name: "x", Reason: "ambiguous-target"}, 0, "ambiguous-target"},
		"no history":       {inspection.JJTidyCandidate{Name: "x", CommitID: "c2", Proof: "native-ancestry"}, 0, "history-unavailable"},
	} {
		if b := jjBranch(tc.cand, "main", tc.index, hist); b.Incomplete != tc.reason || b.Proof != "" || b.Ahead != 0 || b.AuthorHint != "" {
			t.Errorf("%s: %+v", name, b)
		}
	}
}

func TestJJRegistrationMapping(t *testing.T) {
	w := jjRegistration(inspection.JJObservedWorkspace{Name: "a", Path: "/ws/a", CommitID: "c1"})
	if w.Path != "/ws/a" || w.HeadOID != "c1" || w.HeadRef != "" || w.Locked || w.Primary || w.DirMissing || w.Prunable || w.Incomplete != "" {
		t.Errorf("%+v", w)
	}
	if w := jjRegistration(inspection.JJObservedWorkspace{Name: "gone", Path: "/ws/g", CommitID: "c1", Missing: true}); !w.DirMissing || !w.Prunable || w.Incomplete != "" {
		t.Errorf("missing: %+v", w)
	}
	if w := jjRegistration(inspection.JJObservedWorkspace{Name: "main", Path: "/ws/m", CommitID: "c1", Primary: true}); !w.Primary {
		t.Errorf("primary: %+v", w)
	}
	if w := jjRegistration(inspection.JJObservedWorkspace{Name: "nopath", CommitID: "c1"}); w.Incomplete != "workspace-path-unknown" || w.HeadOID != "c1" {
		t.Errorf("no path: %+v", w)
	}
}
