// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// These tests run discovery against real Git repositories: registration is read by the isolation runner and
// every submodule location is confined by the Spec 021 safety layer, so a fake runner would prove little.

func TestReposDiscoverRegisteredSubmodulesInByteOrder(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "b", "a-b", "a/z", "my.sub dir")
	res := reposRun([]string{super}, true)
	var got []string
	for _, e := range res.Entries[1:] {
		got = append(got, e.Path)
		if e.Origin != "submodule" || e.Parent == nil || *e.Parent != 0 || e.Status != "ok" || e.Error != nil || e.Backend != "git" {
			t.Fatalf("submodule entry %+v", e)
		}
	}
	want := []string{"a-b", "a/z", "b", "my.sub dir"} // sorted by path bytes: '-' < '/' < letters
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("order %q, want %q", got, want)
	}
	parent := res.Entries[0]
	if parent.Discovery.Status != "ok" || parent.Discovery.Submodules != 4 || parent.Discovery.Ignored != 0 {
		t.Fatalf("parent discovery %+v", parent.Discovery)
	}
	// The embedded inventory of a discovered entry equals the single-repository inventory (SC-001).
	sub := res.Entries[1]
	again := Inventory(context.Background(), *sub.ResolvedPath, InventoryOptions{})
	if sub.Inventory == nil || sub.Inventory.Status != again.Status || sub.Inventory.Identity.Repository.GitDir != again.Identity.Repository.GitDir {
		t.Fatalf("inventory differs from Inventory: %+v", sub.Inventory)
	}
}

func TestReposDiscoverSubmoduleNotesCarryDirtyCaveat(t *testing.T) {
	reposHermetic(t)
	super := reposSuper(t, filepath.Join(reposTempDir(t), "super"), "s1")
	e := reposRun([]string{super}, true).Entries[1]
	found := false
	for _, n := range e.Notes {
		found = found || strings.Contains(n, "dirty") && strings.Contains(n, "unknown")
	}
	if !found || e.Facts == nil || e.Facts.Dirty.Value != "unknown" {
		t.Fatalf("US3-6: dirty must be unknown with the caveat in notes: %+v / %+v", e.Facts, e.Notes)
	}
}

func TestReposDiscoverUninitializedSubmoduleIsSkippedWithoutProcess(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "s1")
	clone := filepath.Join(base, "clone")
	git(t, base, "clone", "-q", super, clone) // submodules are not checked out: s1 is an empty directory
	var seen []string
	var mu sync.Mutex
	res := reposRunWith([]string{clone}, reposRecordingInspect(&seen, &mu), nil)
	e := reposEntry(t, res, "s1")
	if e.Status != "skipped" || e.Error == nil || e.Error.Code != "submodule-uninitialized" || e.Inventory != nil || e.Facts != nil {
		t.Fatalf("uninitialized submodule: %+v", e)
	}
	if len(seen) != 1 || seen[0] != clone {
		t.Fatalf("a process-level inspection ran inside the skipped member: %v", seen)
	}
	if res.Summary.SkippedUninitialized != 1 {
		t.Fatalf("summary %+v", res.Summary)
	}
}

func TestReposDiscoverRegistrationParsing(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	repo := reposNewRepo(t, filepath.Join(base, "super"))
	reposGitlinks(t, repo,
		[2]string{"my.dotted name", "dir with space"},
		[2]string{"nl", "line\nbreak"},
		[2]string{"glob", "x[ab]*"},
		[2]string{"shared-a", "shared"},
		[2]string{"shared-b", "shared"},
		[2]string{"relink", "old"},
	)
	// The last path of a name wins (Git uses the last value); a value-less key is ignored.
	git(t, repo, "config", "-f", ".gitmodules", "--add", "submodule.relink.path", "new")
	git(t, repo, "config", "-f", ".gitmodules", "submodule.nopath.url", "x")
	oid := gitIn(t, repo, "rev-parse", "HEAD")
	git(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+oid+",new")
	git(t, repo, "add", ".gitmodules")
	git(t, repo, "commit", "-qm", "relink")
	res := reposRun([]string{repo}, true)
	var got []string
	for _, e := range res.Entries[1:] {
		got = append(got, e.Path)
		if e.Status != "skipped" {
			t.Fatalf("%q: %+v", e.Path, e)
		}
	}
	want := []string{"dir with space", "line\nbreak", "new", "shared", "x[ab]*"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("paths %q, want %q", got, want)
	}
	parent := res.Entries[0]
	// "old" has no gitlink at all: ignored, and named in the notes.
	if parent.Discovery.Ignored != 0 {
		t.Fatalf("ignored %d notes %q", parent.Discovery.Ignored, parent.Notes)
	}
}

func TestReposDiscoverIgnoresInvalidAndUnconfirmedPaths(t *testing.T) {
	reposHermetic(t)
	repo := reposNewRepo(t, filepath.Join(reposTempDir(t), "super"))
	reposRegister(t, repo, false,
		[2]string{"parent", "../escape"},
		[2]string{"abs", "/abs/path"},
		[2]string{"dash", "-opt"},
		[2]string{"dots", "a/../b"},
		[2]string{"empty", "a//b"},
		[2]string{"ghost-entry", "ghost"}, // registered without a gitlink
	)
	reposGitlinks(t, repo, [2]string{"ok", "fine"})
	res := reposRun([]string{repo}, true)
	parent := res.Entries[0]
	if len(res.Entries) != 2 || res.Entries[1].Path != "fine" {
		t.Fatalf("entries %v", reposPaths(res))
	}
	if parent.Discovery.Ignored != 6 {
		t.Fatalf("ignored %d, want 6; notes %q", parent.Discovery.Ignored, parent.Notes)
	}
	for _, want := range []string{"discovery-ignored: ../escape", "discovery-ignored: /abs/path", "discovery-ignored: -opt", "discovery-ignored: a/../b", "discovery-ignored: a//b", "discovery-ignored: ghost"} {
		ok := false
		for _, n := range parent.Notes {
			ok = ok || n == want
		}
		if !ok {
			t.Errorf("missing note %q in %q", want, parent.Notes)
		}
	}
}

func TestReposDiscoverNoteEscapesHostilePath(t *testing.T) {
	reposHermetic(t)
	repo := reposNewRepo(t, filepath.Join(reposTempDir(t), "super"))
	long := "/" + strings.Repeat("é", 400)
	reposRegister(t, repo, false, [2]string{"ctl", "\x1b[31m/red"}, [2]string{"long", long})
	parent := reposRun([]string{repo}, true).Entries[0]
	for _, n := range parent.Notes {
		if !strings.HasPrefix(n, "discovery-ignored: ") || strings.ContainsAny(n, "\x1b\n") || len(n) > 300 {
			t.Fatalf("unescaped or unbounded note %q", n)
		}
	}
	if parent.Discovery.Ignored != 2 || len(parent.Notes) != 2 {
		t.Fatalf("ignored %d notes %q", parent.Discovery.Ignored, parent.Notes)
	}
}

// C2: a [submodule] section without a subsection yields the key submodule.path, which is no registration
// and must not panic.
func TestReposDiscoverSectionWithoutSubsection(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "s1")
	f, err := os.OpenFile(filepath.Join(super, ".gitmodules"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(f, "[submodule]\n\tpath = suba\n[submodule \"\"]\n\tpath = subb\n")
	f.Close()
	git(t, super, "add", ".gitmodules")
	git(t, super, "commit", "-qm", "odd sections")
	res := reposRun([]string{super}, true)
	if len(res.Entries) != 2 || res.Entries[1].Path != "s1" || res.Entries[0].Discovery.Status != "ok" {
		t.Fatalf("entries %v discovery %+v", reposPaths(res), res.Entries[0].Discovery)
	}
}

func TestReposDiscoverRefusesUnreadableOrIrregularGitmodules(t *testing.T) {
	reposHermetic(t)
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, repo string) bool
	}{
		{"symlink blob (mode 120000)", func(t *testing.T, repo string) bool {
			// update-index refuses to add it, so build the tree by hand with git mktree. Git's own
			// `config --blob` would parse such a blob, which is why the mode check is mandatory.
			blob := strings.TrimSpace(reposHashObject(t, repo, "[submodule \"x\"]\n\tpath = x\n"))
			f := gitIn(t, repo, "rev-parse", "HEAD:f")
			tree := strings.TrimSpace(reposGitStdin(t, repo, "120000 blob "+blob+"\t.gitmodules\n100644 blob "+f+"\tf\n", "mktree"))
			commit := strings.TrimSpace(git(t, repo, "-c", "user.name=T", "-c", "user.email=t@example.org", "commit-tree", tree, "-p", "HEAD", "-m", "symlink .gitmodules"))
			git(t, repo, "update-ref", "HEAD", commit)
			return true
		}},
		{"tree", func(t *testing.T, repo string) bool {
			writeFile(t, filepath.Join(repo, ".gitmodules", "inner"), "x\n")
			git(t, repo, "add", ".gitmodules/inner")
			return false
		}},
		{"oversized blob", func(t *testing.T, repo string) bool {
			writeFile(t, filepath.Join(repo, ".gitmodules"), strings.Repeat("# padding\n", 7000))
			git(t, repo, "add", ".gitmodules")
			return false
		}},
		{"gitlink", func(t *testing.T, repo string) bool {
			git(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+gitIn(t, repo, "rev-parse", "HEAD")+",.gitmodules")
			return false
		}},
		{"malformed configuration", func(t *testing.T, repo string) bool {
			writeFile(t, filepath.Join(repo, ".gitmodules"), "[submodule \"x\n")
			git(t, repo, "add", ".gitmodules")
			return false
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := reposNewRepo(t, filepath.Join(reposTempDir(t), "super"))
			if !tc.setup(t, repo) {
				git(t, repo, "commit", "-qm", "odd .gitmodules")
			}
			res := reposRun([]string{repo}, true)
			parent := res.Entries[0]
			if len(res.Entries) != 1 || parent.Discovery.Status != "failed" || parent.Discovery.Submodules != 0 {
				t.Fatalf("entries %v discovery %+v", reposPaths(res), parent.Discovery)
			}
			if parent.Status != "partial" || parent.Error == nil || parent.Error.Code != "discovery-failed" {
				t.Fatalf("parent %+v %+v", parent.Status, parent.Error)
			}
			if parent.Inventory == nil {
				t.Fatal("a failed discovery must not remove the parent's own inventory")
			}
		})
	}
}

func reposHashObject(t *testing.T, repo, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "blob")
	writeFile(t, p, content)
	return git(t, repo, "hash-object", "-w", p)
}

// H5: a failing ls-tree is a failed discovery, not an empty registration.
func TestReposDiscoverFailedReadIsNotEmpty(t *testing.T) {
	reposHermetic(t)
	super := reposSuper(t, filepath.Join(reposTempDir(t), "super"), "s1")
	tree := gitIn(t, super, "rev-parse", "HEAD^{tree}") // ls-tree HEAD cannot read the root tree
	if err := os.Remove(filepath.Join(super, ".git", "objects", tree[:2], tree[2:])); err != nil {
		t.Fatal(err)
	}
	parent := reposRun([]string{super}, true).Entries[0]
	if parent.Discovery.Status != "failed" || parent.Discovery.Reason == "" {
		t.Fatalf("discovery %+v", parent.Discovery)
	}
	causes := parent.Notes
	if parent.Error != nil {
		causes = append(causes, parent.Error.Code+": ")
	}
	if !strings.Contains(strings.Join(causes, "\n"), "discovery-failed") {
		t.Fatalf("discovery-failed not reported: %+v %q", parent.Error, parent.Notes)
	}
}

func TestReposDiscoverNothingToDiscoverStartsNoProcess(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	unborn := filepath.Join(base, "unborn")
	git(t, base, "init", "-q", "-b", "main", unborn)
	bareSrc := reposNewRepo(t, filepath.Join(base, "bare-src"))
	bare := filepath.Join(base, "bare.git")
	git(t, base, "clone", "-q", "--bare", bareSrc, bare)
	plain := reposNewRepo(t, filepath.Join(base, "plain"))
	for name, path := range map[string]string{"unborn HEAD": unborn, "bare repository": bare, "no .gitmodules": plain} {
		var n atomic.Int32
		res := reposRunWith([]string{path}, nil, reposCountingRunner(&n))
		d := res.Entries[0].Discovery
		if d.Status != "ok" || d.Submodules != 0 || d.Ignored != 0 {
			t.Errorf("%s: discovery %+v", name, d)
		}
		if name != "no .gitmodules" && n.Load() != 0 {
			t.Errorf("%s: %d discovery processes started", name, n.Load())
		}
	}
}

func TestReposDiscoverSkipsParentsThatFailedEntryChecks(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "s1")
	if err := os.Mkdir(filepath.Join(super, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	res := reposRunWith([]string{filepath.Join(super, "dir"), super, super}, nil, reposCountingRunner(&n))
	if res.Entries[0].Error == nil || res.Entries[0].Error.Code != "not-repository-root" || res.Entries[0].Discovery.Status != "not-applicable" {
		t.Fatalf("subdirectory: %+v", res.Entries[0])
	}
	if res.Entries[2].DuplicateOf == nil || res.Entries[2].Discovery.Status != "not-applicable" {
		t.Fatalf("duplicate: %+v", res.Entries[2])
	}
	// Only the surviving parent runs discovery: layout (2 calls), ls-tree, config, ls-files, and the
	// safety layer's own configuration read of the one submodule.
	if got := reposPaths(res); len(got) != 4 || got[3] != "s1" {
		t.Fatalf("entries %v", got)
	}
	if n.Load() == 0 {
		t.Fatal("the surviving parent must run discovery")
	}
}

func TestReposDiscoverNotApplicableToNonGitParentsRunsNoProcess(t *testing.T) {
	jjDir := reposTempDir(t)
	fakeInspect := func(ctx context.Context, path string) Result {
		return Result{Status: "ok", Backend: "jj", JJRepository: &JJRepository{WorkspaceRoot: path, RepositoryDir: filepath.Join(path, ".jj", "repo")}}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "jj"}
	}
	var n atomic.Int32
	res := reposWithRunner(context.Background(), ReposOptions{Paths: []string{jjDir}, Submodules: true}, fakeInspect, fakeInventory, reposCountingRunner(&n))
	if n.Load() != 0 || res.Entries[0].Discovery.Status != "not-applicable" || res.Summary.DiscoveryNotApplicable != 1 {
		t.Fatalf("processes %d discovery %+v", n.Load(), res.Entries[0].Discovery)
	}
}

func TestReposDiscoverIsNotRecursive(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "s1")
	res := reposRun([]string{super}, true)
	sub := res.Entries[1]
	if sub.Discovery.Requested || sub.Discovery.Status != "not-applicable" || sub.Discovery.Reason != "discovery is not recursive" {
		t.Fatalf("discovery of a submodule: %+v", sub.Discovery)
	}
}

// Registration is read from HEAD and confirmed in the index: a staged-only submodule is not discovered, and
// a staged removal is ignored (counted), not discovered.
func TestReposDiscoverStagedOnlyAndStagedRemoved(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "s1")
	src := reposNewRepo(t, filepath.Join(base, "src2"))
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "staged-only") // not committed
	git(t, super, "rm", "-qf", "s1")                                                                // staged removal
	res := reposRun([]string{super}, true)
	if len(res.Entries) != 1 {
		t.Fatalf("entries %v", reposPaths(res))
	}
	if d := res.Entries[0].Discovery; d.Status != "ok" || d.Submodules != 0 || d.Ignored != 1 {
		t.Fatalf("discovery %+v notes %q", d, res.Entries[0].Notes)
	}
}

func TestReposDiscoverBoundsOnCandidatesAndEntries(t *testing.T) {
	reposHermetic(t)
	mk := func(n int) string {
		repo := reposNewRepo(t, filepath.Join(reposTempDir(t), "super"))
		var np [][2]string
		for i := 0; i < n; i++ {
			np = append(np, [2]string{fmt.Sprintf("n%02d", i), fmt.Sprintf("p%02d", i)})
		}
		reposGitlinks(t, repo, np...)
		return repo
	}
	for _, tc := range []struct {
		n          int
		wantStatus string
		wantEntry  int
		wantCode   string
	}{
		{15, "ok", 16, ""},                  // one parent plus fifteen: exactly the cap
		{16, "limit", 1, "discovery-limit"}, // seventeen entries: none of the submodules is added
		{64, "limit", 1, "discovery-limit"}, // still within 64 candidates, over the entry cap
		{65, "limit", 1, "discovery-limit"}, // more than 64 distinct candidates
	} {
		repo := mk(tc.n)
		res := reposRun([]string{repo}, true)
		d := res.Entries[0].Discovery
		if d.Status != tc.wantStatus || len(res.Entries) != tc.wantEntry || d.Submodules != map[bool]int{true: 15, false: 0}[tc.n == 15] {
			t.Errorf("%d candidates: discovery %+v entries %d", tc.n, d, len(res.Entries))
		}
		if tc.wantCode != "" && (res.Entries[0].Error == nil || res.Entries[0].Error.Code != tc.wantCode || res.Entries[0].Status != "partial") {
			t.Errorf("%d candidates: %+v", tc.n, res.Entries[0].Error)
		}
	}
}

// A submodule path equal to an explicit path is not added again (deduplicated); a submodule that is the same
// repository as an earlier entry under another path is a duplicate-repository (FR-005, M1).
func TestReposDiscoverDeduplicatesByPathAndIdentity(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "s1", "s2")
	byPath := reposRun([]string{super, filepath.Join(super, "s1")}, true)
	if got := reposPaths(byPath); len(got) != 3 || got[2] != "s2" {
		t.Fatalf("entries %v", got)
	}
	if d := byPath.Entries[0].Discovery; d.Deduplicated != 1 || d.Submodules != 1 {
		t.Fatalf("discovery %+v", d)
	}
	// A linked worktree of s2 is the same repository as the discovered s2 (same common directory).
	wt := filepath.Join(base, "s2-linked")
	git(t, filepath.Join(super, "s2"), "worktree", "add", "-q", "-b", "extra", wt)
	res := reposRun([]string{wt, super}, true)
	e := reposEntry(t, res, "s2")
	if e.Status != "error" || e.Error == nil || e.Error.Code != "duplicate-repository" || e.DuplicateOf == nil || *e.DuplicateOf != 0 {
		t.Fatalf("discovered submodule duplicate of an earlier worktree: %+v %+v", e, e.Error)
	}
	if e.Inventory != nil {
		t.Fatal("a duplicate runs no inventory")
	}
}

// H2: the submodules of a linked worktree live under <git-dir>/worktrees/<id>/modules.
func TestReposDiscoverFromLinkedWorktreeSuperproject(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "super"), "s1")
	linked := filepath.Join(base, "linked")
	git(t, super, "worktree", "add", "-q", "-b", "wt", linked)
	git(t, linked, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "-q")
	res := reposRun([]string{linked}, true)
	e := reposEntry(t, res, "s1")
	if e.Status != "ok" || e.Error != nil || e.Inventory == nil {
		t.Fatalf("submodule of a linked worktree: %+v %+v", e, e.Error)
	}
	if !strings.Contains(e.Inventory.Identity.Repository.GitDir, filepath.Join("worktrees", "linked", "modules", "s1")) {
		t.Fatalf("unexpected Git directory %q", e.Inventory.Identity.Repository.GitDir)
	}
	// The same submodule of the main worktree is a different repository, not a duplicate (US6).
	both := reposRun([]string{super, linked}, true)
	for _, e := range both.Entries {
		if e.Error != nil && e.Error.Code == "duplicate-repository" && e.Origin == "submodule" {
			t.Fatalf("submodules of two worktrees must not be duplicates: %+v", e)
		}
	}
}

func TestReposDiscoverFromGitfileSuperproject(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposSuper(t, filepath.Join(base, "origin"), "s1")
	moved := filepath.Join(base, "moved")
	git(t, base, "clone", "-q", "--separate-git-dir", filepath.Join(base, "gitdir"), super, moved)
	git(t, moved, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "-q")
	e := reposEntry(t, reposRun([]string{moved}, true), "s1")
	if e.Status != "ok" || e.Error != nil {
		t.Fatalf("submodule of a gitfile superproject: %+v %+v", e, e.Error)
	}
}

// An old-style submodule keeps its Git directory in place.
func TestReposDiscoverInPlaceGitDirectoryIsAccepted(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	src := reposNewRepo(t, filepath.Join(base, "src"))
	super := reposNewRepo(t, filepath.Join(base, "super"))
	git(t, base, "clone", "-q", src, filepath.Join(super, "inplace"))
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "inplace")
	git(t, super, "commit", "-qm", "adopt")
	if fi, err := os.Stat(filepath.Join(super, "inplace", ".git")); err != nil || !fi.IsDir() {
		t.Skip("this Git version absorbs the Git directory")
	}
	e := reposEntry(t, reposRun([]string{super}, true), "inplace")
	if e.Status != "ok" || e.Error != nil {
		t.Fatalf("in-place Git directory: %+v %+v", e, e.Error)
	}
}
