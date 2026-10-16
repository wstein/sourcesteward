// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReposPathValidationS1(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Missing path
	missingPath := filepath.Join(tempDir, "missing")

	// 2. Regular file
	filePath := filepath.Join(tempDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Dangling symlink
	danglingSymlink := filepath.Join(tempDir, "dangling")
	if err := os.Symlink(filepath.Join(tempDir, "does-not-exist"), danglingSymlink); err != nil {
		t.Fatal(err)
	}

	// 4. Leading dash name
	leadingDash := "-custom-name"

	inspectCalled := false
	fakeInspect := func(ctx context.Context, path string) Result {
		inspectCalled = true
		return Result{}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{missingPath, filePath, danglingSymlink, leadingDash},
	}, fakeInspect, fakeInventory)

	if inspectCalled {
		t.Fatalf("Inspect must not be called for invalid paths")
	}

	if len(res.Entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(res.Entries))
	}

	for i, tc := range []struct {
		path string
	}{
		{missingPath},
		{filePath},
		{danglingSymlink},
		{leadingDash},
	} {
		entry := res.Entries[i]
		if entry.Path != tc.path {
			t.Errorf("[%d] expected path %q, got %q", i, tc.path, entry.Path)
		}
		if entry.Status != "error" {
			t.Errorf("[%d] expected status error, got %s", i, entry.Status)
		}
		if entry.Error == nil || entry.Error.Code != "invalid-path" {
			t.Errorf("[%d] expected error code invalid-path, got %v", i, entry.Error)
		}
		if entry.Inventory != nil {
			t.Errorf("[%d] expected nil inventory, got %v", i, entry.Inventory)
		}
	}
}

func TestReposSymlinkResolution(t *testing.T) {
	tempDir := t.TempDir()
	targetDir := filepath.Join(tempDir, "real-repo")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkPath := filepath.Join(tempDir, "symlink-repo")
	if err := os.Symlink(targetDir, symlinkPath); err != nil {
		t.Fatal(err)
	}

	resolvedTarget, err := filepath.EvalSymlinks(targetDir)
	if err != nil {
		t.Fatal(err)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		if path != resolvedTarget {
			t.Fatalf("Inspect called with %q, want %q", path, resolvedTarget)
		}
		wt := resolvedTarget
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(resolvedTarget, ".git"),
				CommonDir: filepath.Join(resolvedTarget, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{
			Status:  "ok",
			Backend: "git",
		}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{symlinkPath},
	}, fakeInspect, fakeInventory)

	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res.Entries))
	}
	e := res.Entries[0]
	if e.Path != symlinkPath {
		t.Errorf("expected path %q, got %q", symlinkPath, e.Path)
	}
	if e.ResolvedPath == nil || *e.ResolvedPath != resolvedTarget {
		t.Errorf("expected resolved_path %q, got %v", resolvedTarget, e.ResolvedPath)
	}
	if e.Status != "ok" {
		t.Errorf("expected status ok, got %s", e.Status)
	}
}

func TestReposNotRepositoryRoot(t *testing.T) {
	tempDir := t.TempDir()
	repoDir := filepath.Join(tempDir, "repo")
	subDir := filepath.Join(repoDir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	resolvedSub, err := filepath.EvalSymlinks(subDir)
	if err != nil {
		t.Fatal(err)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := resolvedRepo
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(resolvedRepo, ".git"),
				CommonDir: filepath.Join(resolvedRepo, ".git"),
				Worktree:  &wt, // toplevel worktree is repoDir, not subdir
			},
		}
	}
	inventoryCalled := false
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		inventoryCalled = true
		return InventoryResult{}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{subDir},
	}, fakeInspect, fakeInventory)

	if inventoryCalled {
		t.Fatalf("Inventory must not be called when path is not repository root")
	}

	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(res.Entries))
	}
	e := res.Entries[0]
	if e.Status != "error" {
		t.Errorf("expected status error, got %s", e.Status)
	}
	if e.Error == nil || e.Error.Code != ReposCodeNotRepositoryRoot {
		t.Errorf("expected error code %s, got %v", ReposCodeNotRepositoryRoot, e.Error)
	}
	if e.ResolvedPath == nil || *e.ResolvedPath != resolvedSub {
		t.Errorf("expected resolved_path %q, got %v", resolvedSub, e.ResolvedPath)
	}
}

func TestReposUnsupportedAndAmbiguousBackends(t *testing.T) {
	tempDir := t.TempDir()
	gbDir := filepath.Join(tempDir, "gb-repo")
	ambDir := filepath.Join(tempDir, "amb-repo")
	if err := os.Mkdir(gbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ambDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		if strings.Contains(path, "gb-repo") {
			return Result{
				Status:      "error",
				Backend:     "gitbutler",
				Diagnostics: []Diagnostic{{Code: "unsupported-backend", Message: "GitButler backend is unsupported"}},
			}
		}
		return Result{
			Status:      "error",
			Backend:     "ambiguous",
			Diagnostics: []Diagnostic{{Code: "ambiguous-backend", Message: "multiple backend markers found"}},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{gbDir, ambDir},
	}, fakeInspect, fakeInventory)

	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res.Entries))
	}

	e0 := res.Entries[0]
	if e0.Backend != "gitbutler" || e0.Status != "error" || e0.Error == nil || e0.Error.Code != "unsupported-backend" {
		t.Errorf("entry 0: unexpected result %+v", e0)
	}

	e1 := res.Entries[1]
	if e1.Backend != "ambiguous" || e1.Status != "error" || e1.Error == nil || e1.Error.Code != "ambiguous-backend" {
		t.Errorf("entry 1: unexpected result %+v", e1)
	}
}

func TestReposBareRepositoryAndMissingGit(t *testing.T) {
	tempDir := t.TempDir()
	bareDir := filepath.Join(tempDir, "bare.git")
	missingGitDir := filepath.Join(tempDir, "repo-no-git")
	if err := os.Mkdir(bareDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(missingGitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedBare, err := filepath.EvalSymlinks(bareDir)
	if err != nil {
		t.Fatal(err)
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		if strings.Contains(path, "bare.git") {
			return Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    resolvedBare,
					CommonDir: resolvedBare,
					Bare:      true,
				},
			}
		}
		return Result{
			Status:      "error",
			Backend:     "unknown",
			Diagnostics: []Diagnostic{{Code: "git-unavailable", Message: "Git executable is unavailable"}},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{
			Status:  "partial",
			Backend: "git",
			Boundary: InventoryBoundary{
				Limitations: []string{"bare repository has no disk or index checkout scopes"},
			},
		}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{bareDir, missingGitDir},
	}, fakeInspect, fakeInventory)

	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res.Entries))
	}

	// Bare repository is partial
	e0 := res.Entries[0]
	if e0.Status != "partial" || e0.Backend != "git" || e0.Inventory == nil {
		t.Errorf("bare entry: expected partial with inventory, got %+v", e0)
	}

	// Missing Git is error git-unavailable
	e1 := res.Entries[1]
	if e1.Status != "error" || e1.Error == nil || e1.Error.Code != "git-unavailable" {
		t.Errorf("missing git entry: expected error git-unavailable, got %+v", e1)
	}
}

func TestReposMixedGitAndJJScopes(t *testing.T) {
	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, "git-repo")
	jjDir := filepath.Join(tempDir, "jj-repo")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(jjDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedGit, _ := filepath.EvalSymlinks(gitDir)
	resolvedJJ, _ := filepath.EvalSymlinks(jjDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		if strings.Contains(path, "git-repo") {
			wt := resolvedGit
			return Result{
				Status:  "ok",
				Backend: "git",
				Repository: &Repository{
					GitDir:    filepath.Join(resolvedGit, ".git"),
					CommonDir: filepath.Join(resolvedGit, ".git"),
					Worktree:  &wt,
				},
			}
		}
		return Result{
			Status:  "ok",
			Backend: "jj",
			JJRepository: &JJRepository{
				WorkspaceRoot: resolvedJJ,
				RepositoryDir: filepath.Join(resolvedJJ, ".jj", "repo"),
			},
		}
	}

	var gitScopes, jjScopes []string
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		if strings.Contains(path, "git-repo") {
			gitScopes = o.Scopes
			return InventoryResult{Status: "ok", Backend: "git"}
		}
		jjScopes = o.Scopes
		return InventoryResult{Status: "ok", Backend: "jj"}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{gitDir, jjDir},
	}, fakeInspect, fakeInventory)

	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res.Entries))
	}

	// Git uses default scopes (empty / nil)
	if len(gitScopes) != 0 {
		t.Errorf("expected Git to receive default scopes, got %v", gitScopes)
	}

	// jj gets stored scope
	if len(jjScopes) != 1 || jjScopes[0] != "stored" {
		t.Errorf("expected jj to receive ['stored'] scope, got %v", jjScopes)
	}
}

func TestReposDeduplication(t *testing.T) {
	tempDir := t.TempDir()
	dir1 := filepath.Join(tempDir, "repo1")
	dir2 := filepath.Join(tempDir, "repo2") // linked worktree of repo1
	if err := os.Mkdir(dir1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir2, 0o755); err != nil {
		t.Fatal(err)
	}

	resolved1, _ := filepath.EvalSymlinks(dir1)
	_, _ = filepath.EvalSymlinks(dir2)

	commonDir := filepath.Join(resolved1, ".git")

	var inspectCount atomic.Int32 // workers call the fakes concurrently
	fakeInspect := func(ctx context.Context, path string) Result {
		inspectCount.Add(1)
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(path, ".git"),
				CommonDir: commonDir, // both share the same CommonDir!
				Worktree:  &wt,
			},
		}
	}
	var inventoryCount atomic.Int32
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		inventoryCount.Add(1)
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	// Test 1: Identical explicit path without a process
	// Pass dir1 twice: dir1, dir1, dir2
	res := repos(context.Background(), ReposOptions{
		Paths: []string{dir1, dir1, dir2},
	}, fakeInspect, fakeInventory)

	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(res.Entries))
	}

	// Entry 0: ok
	if res.Entries[0].Status != "ok" {
		t.Errorf("entry 0: expected ok, got %s", res.Entries[0].Status)
	}

	// Entry 1: identical path -> duplicate-repository, duplicate_of 0
	e1 := res.Entries[1]
	if e1.Status != "error" || e1.Error == nil || e1.Error.Code != ReposCodeDuplicateRepository {
		t.Errorf("entry 1: expected duplicate-repository, got %+v", e1)
	}
	if e1.DuplicateOf == nil || *e1.DuplicateOf != 0 {
		t.Errorf("entry 1: expected duplicate_of 0, got %v", e1.DuplicateOf)
	}

	// Entry 2: linked worktree -> duplicate-repository by common_dir, duplicate_of 0
	e2 := res.Entries[2]
	if e2.Status != "error" || e2.Error == nil || e2.Error.Code != ReposCodeDuplicateRepository {
		t.Errorf("entry 2: expected duplicate-repository, got %+v", e2)
	}
	if e2.DuplicateOf == nil || *e2.DuplicateOf != 0 {
		t.Errorf("entry 2: expected duplicate_of 0, got %v", e2.DuplicateOf)
	}

	// Inventory was only run for entry 0!
	if inventoryCount.Load() != 1 {
		t.Errorf("expected exactly 1 inventory run, got %d", inventoryCount.Load())
	}

	// Inspect was NOT run for identical explicit path (only for dir1 and dir2)
	if inspectCount.Load() != 2 {
		t.Errorf("expected 2 inspect calls (not for identical path), got %d", inspectCount.Load())
	}
}

func TestReposJJDeduplication(t *testing.T) {
	tempDir := t.TempDir()
	ws1 := filepath.Join(tempDir, "ws1")
	ws2 := filepath.Join(tempDir, "ws2")
	if err := os.Mkdir(ws1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ws2, 0o755); err != nil {
		t.Fatal(err)
	}

	resolved1, _ := filepath.EvalSymlinks(ws1)
	resolved2, _ := filepath.EvalSymlinks(ws2)

	sharedRepoDir := filepath.Join(tempDir, ".jj-store", "repo")

	fakeInspect := func(ctx context.Context, path string) Result {
		wsRoot := resolved1
		if path == resolved2 {
			wsRoot = resolved2
		}
		return Result{
			Status:  "ok",
			Backend: "jj",
			JJRepository: &JJRepository{
				WorkspaceRoot: wsRoot,
				RepositoryDir: sharedRepoDir, // both workspaces share the same repo dir!
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "jj"}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{ws1, ws2},
	}, fakeInspect, fakeInventory)

	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res.Entries))
	}

	// ws1: ok
	if res.Entries[0].Status != "ok" {
		t.Errorf("entry 0: expected ok, got %s", res.Entries[0].Status)
	}

	// ws2: duplicate-repository with duplicate_of 0
	e1 := res.Entries[1]
	if e1.Status != "error" || e1.Error == nil || e1.Error.Code != ReposCodeDuplicateRepository {
		t.Errorf("entry 1: expected duplicate-repository, got %+v", e1)
	}
	if e1.DuplicateOf == nil || *e1.DuplicateOf != 0 {
		t.Errorf("entry 1: expected duplicate_of 0, got %v", e1.DuplicateOf)
	}
}

func TestReposSubmoduleInLinkedWorktreeNotDuplicate(t *testing.T) {
	tempDir := t.TempDir()
	subMain := filepath.Join(tempDir, "main-wt", "sub")
	subLinked := filepath.Join(tempDir, "linked-wt", "sub")
	if err := os.MkdirAll(subMain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(subLinked, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedMain, _ := filepath.EvalSymlinks(subMain)
	resolvedLinked, _ := filepath.EvalSymlinks(subLinked)

	// In Git, a submodule in main worktree and one in linked worktree have distinct CommonDir!
	commonDirMain := filepath.Join(tempDir, "super", ".git", "modules", "sub")
	commonDirLinked := filepath.Join(tempDir, "super", ".git", "worktrees", "linked-wt", "modules", "sub")

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := resolvedMain
		cDir := commonDirMain
		if path == resolvedLinked {
			wt = resolvedLinked
			cDir = commonDirLinked
		}
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(cDir, "gitdir"),
				CommonDir: cDir,
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{subMain, subLinked},
	}, fakeInspect, fakeInventory)

	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res.Entries))
	}

	// Neither is duplicate!
	if res.Entries[0].Status != "ok" {
		t.Errorf("entry 0: expected ok, got %s", res.Entries[0].Status)
	}
	if res.Entries[1].Status != "ok" {
		t.Errorf("entry 1: expected ok (not duplicate), got %s", res.Entries[1].Status)
	}
	if res.Entries[1].DuplicateOf != nil {
		t.Errorf("entry 1: expected duplicate_of nil, got %v", res.Entries[1].DuplicateOf)
	}
}

func TestReposContainment(t *testing.T) {
	tempDir := t.TempDir()
	parentDir := filepath.Join(tempDir, "workspace")
	nestedDir := filepath.Join(parentDir, "inner")
	siblingDir := filepath.Join(parentDir + "-other") // sibling prefix, NOT inside

	for _, d := range []string{parentDir, nestedDir, siblingDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	fakeInspect := func(ctx context.Context, path string) Result {
		resolved, _ := filepath.EvalSymlinks(path)
		wt := resolved
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(resolved, ".git"),
				CommonDir: filepath.Join(resolved, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	res := repos(context.Background(), ReposOptions{
		Paths: []string{parentDir, nestedDir, siblingDir},
	}, fakeInspect, fakeInventory)

	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(res.Entries))
	}

	// parentDir is not contained in anything
	if res.Entries[0].ContainedIn != nil {
		t.Errorf("entry 0: expected contained_in nil, got %v", res.Entries[0].ContainedIn)
	}

	// nestedDir is contained in parentDir (index 0)
	if res.Entries[1].ContainedIn == nil || *res.Entries[1].ContainedIn != 0 {
		t.Errorf("entry 1: expected contained_in 0, got %v", res.Entries[1].ContainedIn)
	}

	// siblingDir is NOT contained in parentDir
	if res.Entries[2].ContainedIn != nil {
		t.Errorf("entry 2: expected contained_in nil, got %v", res.Entries[2].ContainedIn)
	}
}
