// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
	skipIfJJSandboxUnavailable(t, err)
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
	if jjReportsRootOfMissingWorkspace(t, repo, "gone") {
		g := evidenceOf(t, o, gone)
		if !g.DirMissing || !g.Prunable || g.Incomplete != "" || g.Proof != "" {
			t.Fatalf("missing directory: %+v", g)
		}
	} else {
		// jj 0.45.1 reports no root for a workspace whose directory is gone: its path cannot be
		// determined, so it is Incomplete, never present and clean.
		unknown := 0
		for _, w := range o.Worktrees {
			if w.Path == "" {
				unknown++
				if w.Incomplete != "workspace-path-unknown" || w.DirMissing || w.Proof != "" || w.Dirty || w.IgnoredExtras || w.HeadOID == "" {
					t.Fatalf("a workspace without a path: %+v", w)
				}
			}
		}
		if unknown != 1 {
			t.Fatalf("want one workspace without a path, got %d: %+v", unknown, o.Worktrees)
		}
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
	skipIfJJSandboxUnavailable(t, err)
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
	skipIfJJSandboxUnavailable(t, err)
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

// A secondary workspace of a colocated repository may carry a .git file and .jj/.gitignore, which
// the jj retirement admission does not hold: the observer reports them as extras (retained).
func TestObserveJJColocatedSecondaryWorkspaceExtrasFollowTheLayout(t *testing.T) {
	repo := observeJJRepoWith(t, true)
	second := observeJJAdd(t, repo, "second")
	// jj 0.46 gives a secondary workspace of a colocated repository a .git file and .jj/.gitignore;
	// jj 0.45.1 does not. The observation reports extras exactly when the layout holds them.
	_, gitFile := os.Lstat(filepath.Join(second, ".git"))
	_, ignore := os.Lstat(filepath.Join(second, ".jj", ".gitignore"))
	holds := gitFile == nil || ignore == nil
	if w := evidenceOf(t, observeJJ(t, repo), second); w.Incomplete != "" || w.IgnoredExtras != holds || w.Dirty {
		t.Fatalf("extras %v for a layout that holds them: %v: %+v", w.IgnoredExtras, holds, w)
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
	observed := map[string]string{"feat": "c1", "main": "c1", "x": "c2"}
	ok := inspection.JJTidyCandidate{Name: "feat", CommitID: "c1", Proof: "native-ancestry"}
	if b := jjBranch(ok, "main", 0, hist, observed); b.Incomplete != "" || b.Proof != "ancestor" || b.Ahead != 2 || b.AuthorHint != "Ana" || b.IsBase || b.OID != "c1" {
		t.Errorf("complete: %+v", b)
	}
	if b := jjBranch(inspection.JJTidyCandidate{Name: "main", CommitID: "c1"}, "main", 0, hist, observed); !b.IsBase || b.Proof != "none" {
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
		if b := jjBranch(tc.cand, "main", tc.index, hist, observed); b.Incomplete != tc.reason || b.Proof != "" || b.Ahead != 0 || b.AuthorHint != "" {
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

// jjReportsRootOfMissingWorkspace asks the same jj binary whether the root of a workspace whose
// directory was deleted is still reported (jj 0.46 does, jj 0.45.1 reports null).
func jjReportsRootOfMissingWorkspace(t *testing.T, repo, name string) bool {
	t.Helper()
	out := jjCLI(t, repo, "workspace", "list", "--template", `json(self.name()) ++ " " ++ json(self.root()) ++ "\n"`)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, strconv.Quote(name)+" ") {
			return !strings.HasSuffix(line, " null")
		}
	}
	t.Fatalf("workspace %s is not listed: %s", name, out)
	return false
}

// skipIfJJSandboxUnavailable skips only when the native jj sandbox is genuinely unavailable and
// STW_REQUIRE_JJ is unset; every other error fails the test, so a regression of the observer
// cannot hide as a skip.
func skipIfJJSandboxUnavailable(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "read-only bubblewrap") && os.Getenv("STW_REQUIRE_JJ") != "1" {
		t.Skipf("native jj sandbox unavailable: %v", err)
	}
	t.Fatalf("observation failed: %v", err)
}

// A bookmark with a keep reason of the native jj tidy is retained with that reason and never
// proven for deletion: a bookmark tracking a remote, a recovery bookmark and an alias of the base.
func TestObserveJJBookmarksKeepTheirNativeTidyReason(t *testing.T) {
	repo := observeJJRepoWith(t, false)
	store := filepath.Join(repo, ".jj", "repo", "store", "git")
	feature := lastField(jjCLI(t, repo, "log", "-r", "feature", "--no-graph", "-T", "commit_id"))
	for _, args := range [][]string{{"remote", "add", "origin", "https://example.invalid/r.git"}, {"update-ref", "refs/remotes/origin/pushed", feature}} {
		if out, err := exec.Command("git", append([]string{"--git-dir", store}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	old := lastField(jjCLI(t, repo, "log", "-r", "main", "--no-graph", "-T", "commit_id"))
	jjCLI(t, repo, "new", "main", "-m", "next")
	jjCLI(t, repo, "bookmark", "move", "main", "--to", "@")
	jjCLI(t, repo, "bookmark", "create", "merged", "-r", old)
	jjCLI(t, repo, "bookmark", "create", "pushed", "-r", "feature")
	jjCLI(t, repo, "bookmark", "create", "stw-recovery-x", "-r", "feature")
	jjCLI(t, repo, "bookmark", "create", "trunk", "-r", "main")
	jjCLI(t, repo, "git", "import")
	o := observeJJ(t, repo)
	by := map[string]BranchEvidence{}
	for _, b := range o.Branches {
		by[b.Name] = b
	}
	for name, reason := range map[string]string{"pushed": "remote-counterpart", "stw-recovery-x": "recovery-reference", "trunk": "selected-base", "main": "selected-base"} {
		if b := by[name]; b.Reason != reason || b.Incomplete != "" {
			t.Errorf("%s: want reason %q: %+v", name, reason, b)
		}
	}
	if b := by["feature"]; b.Reason != "" || b.Proof != "none" || b.Incomplete != "" {
		t.Errorf("feature is plainly not integrated: %+v", b)
	}
	if b := by["merged"]; b.Reason != "" || b.Proof != "ancestor" || b.Incomplete != "" {
		t.Errorf("an integrated bookmark with no keep reason has no reason: %+v", b)
	}
}

func lastField(s string) string {
	f := strings.Fields(s)
	return f[len(f)-1]
}

// Every workspace of one observation is read at the observation's operation: a record from
// another operation is Incomplete, as is a different working-copy commit.
func TestObserveJJWorkspaceRefusesAnotherOperation(t *testing.T) {
	dir := gitTempDir(t)
	for _, p := range []string{".jj/working_copy"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{".jj/working_copy/tree_state", ".jj/working_copy/checkout"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := inspection.JJRepository{OperationID: "op1", RepositoryDir: "/repo", WorkspaceRoot: "/repo"}
	reg := inspection.JJWorkspacesObservation{Expected: inspection.JJWorkspaceState{Source: source}, History: map[string]inspection.JJHistory{"c1": {Ahead: 1, Author: "Ana"}}}
	ws := inspection.JJObservedWorkspace{Name: "w", Path: dir, CommitID: "c1"}
	state := func(op, commit string) func(context.Context, string, string, string, string, string) (inspection.JJRetirementState, error) {
		return func(context.Context, string, string, string, string, string) (inspection.JJRetirementState, error) {
			return inspection.JJRetirementState{Expected: inspection.JJWorkspaceState{Source: inspection.JJRepository{OperationID: op}}, Target: inspection.JJRepository{CommitID: commit},
				MetadataPaths: []string{".jj", ".jj/working_copy", ".jj/working_copy/tree_state", ".jj/working_copy/checkout"}, Proof: "native-ancestry"}, nil
		}
	}
	run := func(retire func(context.Context, string, string, string, string, string) (inspection.JJRetirementState, error), ctx context.Context) WorktreeEvidence {
		obs := defaultWorktreeObserver()
		obs.retire = retire
		w := WorktreeEvidence{Path: dir, HeadOID: "c1"}
		obs.observeJJWorkspace(ctx, ObserveOptions{Path: "/repo", Base: "main"}, reg, ws, &w)
		return w
	}
	if w := run(state("op1", "c1"), context.Background()); w.Incomplete != "" || w.Proof != "ancestor" || w.Ahead != 1 || w.AuthorHint != "Ana" {
		t.Fatalf("consistent record: %+v", w)
	}
	if w := run(state("op2", "c1"), context.Background()); w.Incomplete != "evidence-changed" || w.Proof != "" || w.Ahead != 0 {
		t.Fatalf("a record of another operation: %+v", w)
	}
	if w := run(state("op1", "c2"), context.Background()); w.Incomplete != "evidence-changed" || w.Proof != "" {
		t.Fatalf("another working-copy commit: %+v", w)
	}
	// A commit without history is Incomplete.
	reg.History = map[string]inspection.JJHistory{}
	if w := run(state("op1", "c1"), context.Background()); w.Incomplete != "history-unavailable" || w.Proof != "" {
		t.Fatalf("no history: %+v", w)
	}
	// Past the deadline a failing read is deadline-exceeded, not a vague failure.
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	failing := func(context.Context, string, string, string, string, string) (inspection.JJRetirementState, error) {
		return inspection.JJRetirementState{}, fmt.Errorf("the read failed")
	}
	if w := run(failing, expired); w.Incomplete != "deadline-exceeded" {
		t.Fatalf("deadline: %+v", w)
	}
	if w := run(failing, context.Background()); w.Incomplete != "observation-failed" {
		t.Fatalf("plain failure: %+v", w)
	}
	// A foreign repository at the registered path is a registration mismatch.
	if w := jjRegistration(inspection.JJObservedWorkspace{Name: "w", Path: dir, CommitID: "c1", Foreign: true}); w.Incomplete != "registration-mismatch" || w.Primary {
		t.Fatalf("foreign: %+v", w)
	}
}

func TestJJBranchesMustMatchTheObservedBookmarks(t *testing.T) {
	hist := map[string]inspection.JJHistory{"c1": {Ahead: 1}}
	cand := inspection.JJTidyCandidate{Name: "x", CommitID: "c1", Proof: "native-ancestry", Decision: "delete", Reason: "integrated"}
	if b := jjBranch(cand, "main", 0, hist, map[string]string{"x": "c1"}); b.Incomplete != "" || b.Proof != "ancestor" || b.Reason != "" {
		t.Fatalf("%+v", b)
	}
	for name, observed := range map[string]map[string]string{"another commit": {"x": "c9"}, "not observed": {}} {
		if b := jjBranch(cand, "main", 0, hist, observed); b.Incomplete != "evidence-changed" || b.Proof != "" {
			t.Errorf("%s: %+v", name, b)
		}
	}
}
