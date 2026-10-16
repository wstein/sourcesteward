// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReposDiscoverGitmodulesBlobValidation(t *testing.T) {
	tempDir := reposTempDir(t)
	repoDir := filepath.Join(tempDir, "super")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)

	for _, tc := range []struct {
		name       string
		lsTreeOut  string
		wantStatus string // "ok" or "failed"
		wantCode   string
	}{
		{
			name:       "valid 100644 blob",
			lsTreeOut:  "100644 blob abcdef1234567890 50\t.gitmodules\x00",
			wantStatus: "ok",
		},
		{
			name:       "symlink 120000 blob",
			lsTreeOut:  "120000 blob abcdef1234567890 50\t.gitmodules\x00",
			wantStatus: "failed",
			wantCode:   ReposCodeDiscoveryFailed,
		},
		{
			name:       "tree mode 040000",
			lsTreeOut:  "040000 tree abcdef1234567890 -\t.gitmodules\x00",
			wantStatus: "failed",
			wantCode:   ReposCodeDiscoveryFailed,
		},
		{
			name:       "oversized blob > 64 KiB",
			lsTreeOut:  "100644 blob abcdef1234567890 65537\t.gitmodules\x00",
			wantStatus: "failed",
			wantCode:   ReposCodeDiscoveryFailed,
		},
		{
			name:       "missing .gitmodules in HEAD",
			lsTreeOut:  "",
			wantStatus: "ok",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeInspect := func(ctx context.Context, path string) Result {
				wt := resolvedRepo
				return Result{
					Status:  "ok",
					Backend: "git",
					Repository: &Repository{
						GitDir:    filepath.Join(resolvedRepo, ".git"),
						CommonDir: filepath.Join(resolvedRepo, ".git"),
						Worktree:  &wt,
					},
				}
			}
			fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
				return InventoryResult{Status: "ok", Backend: "git"}
			}

			fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
				if len(args) >= 3 && args[0] == "ls-tree" {
					return tc.lsTreeOut, 0, nil
				}
				if len(args) >= 2 && args[0] == "config" {
					return "", 0, nil
				}
				return "", 0, nil
			}

			res := reposWithRunner(context.Background(), ReposOptions{
				Paths:      []string{repoDir},
				Submodules: true,
			}, fakeInspect, fakeInventory, fakeRunner)

			if len(res.Entries) == 0 {
				t.Fatalf("expected at least 1 entry")
			}
			parent := res.Entries[0]
			if parent.Discovery.Status != tc.wantStatus {
				t.Errorf("discovery status = %s, want %s", parent.Discovery.Status, tc.wantStatus)
			}
			if tc.wantCode != "" {
				if parent.Status != "partial" {
					t.Errorf("parent status = %s, want partial", parent.Status)
				}
				if parent.Error == nil || parent.Error.Code != tc.wantCode {
					t.Errorf("parent error = %v, want code %s", parent.Error, tc.wantCode)
				}
			}
		})
	}
}

func TestReposDiscoverConfigParsing(t *testing.T) {
	tempDir := reposTempDir(t)
	repoDir := filepath.Join(tempDir, "super")
	sub1Dir := filepath.Join(repoDir, "sub 1")
	sub2Dir := filepath.Join(repoDir, "sub.two")
	for _, d := range []string{repoDir, sub1Dir, sub2Dir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	_, _ = filepath.EvalSymlinks(repoDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := path
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(path, ".git"),
				CommonDir: filepath.Join(path, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	// Config output with:
	// - name with dots: submodule.name.with.dots.path
	// - path with spaces: sub 1
	// - overriding previous value: submodule.a.path = old, then submodule.a.path = sub 1 (last wins)
	// - two names sharing the same path: submodule.b.path = sub 1 (shares path with a)
	// - valueless key (skipped)
	configOut := "submodule.a.path\nold\x00" +
		"submodule.a.path\nsub 1\x00" +
		"submodule.b.path\nsub 1\x00" +
		"submodule.name.with.dots.path\nsub.two\x00" +
		"submodule.novalue.path\n\x00"

	fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if len(args) >= 3 && args[0] == "ls-tree" {
			return "100644 blob 1234567890abcdef 100\t.gitmodules\x00", 0, nil
		}
		if len(args) >= 2 && args[0] == "config" {
			return configOut, 0, nil
		}
		if len(args) >= 3 && args[0] == "ls-files" {
			path := args[len(args)-1]
			path = strings.TrimPrefix(path, ":(literal)")
			return fmt.Sprintf("160000 1111111111111111111111111111111111111111 0\t%s\x00", path), 0, nil
		}
		return "", 0, nil
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths:      []string{repoDir},
		Submodules: true,
	}, fakeInspect, fakeInventory, fakeRunner)

	if len(res.Entries) < 1 {
		t.Fatalf("expected entries")
	}
	parent := res.Entries[0]
	if parent.Discovery.Status != "ok" {
		t.Errorf("parent discovery status = %s, want ok", parent.Discovery.Status)
	}

	// 2 submodules discovered: "sub 1" and "sub.two"
	if parent.Discovery.Submodules != 2 {
		t.Errorf("discovered submodules count = %d, want 2", parent.Discovery.Submodules)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("total entries = %d, want 3 (parent + 2 submodules)", len(res.Entries))
	}

	// Discovered entries must have Origin == "submodule", Parent == 0
	e1, e2 := res.Entries[1], res.Entries[2]
	if e1.Origin != "submodule" || e1.Parent == nil || *e1.Parent != 0 {
		t.Errorf("e1 origin/parent mismatch: %+v", e1)
	}
	if e2.Origin != "submodule" || e2.Parent == nil || *e2.Parent != 0 {
		t.Errorf("e2 origin/parent mismatch: %+v", e2)
	}
}

func TestReposDiscoverHostileAndSafety(t *testing.T) {
	tempDir := reposTempDir(t)
	repoDir := filepath.Join(tempDir, "super")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := resolvedRepo
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(resolvedRepo, ".git"),
				CommonDir: filepath.Join(resolvedRepo, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	// Hostile paths:
	// - ".."
	// - "/absolute"
	// - "-leading-dash"
	// - "a/../b"
	configOut := "submodule.dotdot.path\n..\x00" +
		"submodule.abs.path\n/etc/passwd\x00" +
		"submodule.dash.path\n-flag\x00" +
		"submodule.traversal.path\na/../b\x00"

	fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if len(args) >= 3 && args[0] == "ls-tree" {
			return "100644 blob 1234567890abcdef 100\t.gitmodules\x00", 0, nil
		}
		if len(args) >= 2 && args[0] == "config" {
			return configOut, 0, nil
		}
		return "", 0, nil
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths:      []string{repoDir},
		Submodules: true,
	}, fakeInspect, fakeInventory, fakeRunner)

	parent := res.Entries[0]
	// All hostile paths must be ignored, none added as entries
	if parent.Discovery.Ignored != 4 {
		t.Errorf("ignored count = %d, want 4", parent.Discovery.Ignored)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("total entries = %d, want 1 (parent only)", len(res.Entries))
	}
}

func TestReposDiscoverCandidateLimit(t *testing.T) {
	tempDir := reposTempDir(t)
	repoDir := filepath.Join(tempDir, "super")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := resolvedRepo
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(resolvedRepo, ".git"),
				CommonDir: filepath.Join(resolvedRepo, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	// 65 distinct candidates (> 64)
	var sb strings.Builder
	for i := 0; i < 65; i++ {
		sb.WriteString(fmt.Sprintf("submodule.sub%d.path\nsub%d\x00", i, i))
	}

	fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if len(args) >= 3 && args[0] == "ls-tree" {
			return "100644 blob 1234567890abcdef 100\t.gitmodules\x00", 0, nil
		}
		if len(args) >= 2 && args[0] == "config" {
			return sb.String(), 0, nil
		}
		return "", 0, nil
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths:      []string{repoDir},
		Submodules: true,
	}, fakeInspect, fakeInventory, fakeRunner)

	parent := res.Entries[0]
	if parent.Discovery.Status != "limit" {
		t.Errorf("parent discovery status = %s, want limit", parent.Discovery.Status)
	}
	if parent.Status != "partial" {
		t.Errorf("parent status = %s, want partial", parent.Status)
	}
	if parent.Error == nil || parent.Error.Code != ReposCodeDiscoveryLimit {
		t.Errorf("parent error = %v, want code %s", parent.Error, ReposCodeDiscoveryLimit)
	}
	if len(res.Entries) != 1 {
		t.Fatalf("total entries = %d, want 1 (none added on discovery-limit)", len(res.Entries))
	}
}

func TestReposDiscoverNonGitParentNotApplicable(t *testing.T) {
	tempDir := reposTempDir(t)
	jjDir := filepath.Join(tempDir, "jj-repo")
	if err := os.Mkdir(jjDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedJJ, _ := filepath.EvalSymlinks(jjDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		return Result{
			Status:  "ok",
			Backend: "jj",
			JJRepository: &JJRepository{
				WorkspaceRoot: resolvedJJ,
				RepositoryDir: filepath.Join(resolvedJJ, ".jj", "repo"),
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "jj"}
	}

	runnerCalled := false
	fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		runnerCalled = true
		return "", 0, nil
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths:      []string{jjDir},
		Submodules: true,
	}, fakeInspect, fakeInventory, fakeRunner)

	if runnerCalled {
		t.Fatalf("Git runner must not be called for non-Git parent discovery")
	}

	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry")
	}
	parent := res.Entries[0]
	if parent.Discovery.Status != "not-applicable" {
		t.Errorf("parent discovery status = %s, want not-applicable", parent.Discovery.Status)
	}
}

func TestReposDiscoverPointerOutsideModules(t *testing.T) {
	tempDir := reposTempDir(t)
	repoDir := filepath.Join(tempDir, "super")
	subDir := filepath.Join(repoDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create pointer file pointing OUTSIDE superproject modules dir
	outsideDir := filepath.Join(tempDir, "outside", "modules", "sub")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, ".git"), []byte("gitdir: "+outsideDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := resolvedRepo
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(resolvedRepo, ".git"),
				CommonDir: filepath.Join(resolvedRepo, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if len(args) >= 3 && args[0] == "ls-tree" {
			return "100644 blob 1234567890abcdef 100\t.gitmodules\x00", 0, nil
		}
		if len(args) >= 2 && args[0] == "config" {
			return "submodule.sub.path\nsub\x00", 0, nil
		}
		if len(args) >= 3 && args[0] == "ls-files" {
			return "160000 1111111111111111111111111111111111111111 0\tsub\x00", 0, nil
		}
		return "", 0, nil
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths:      []string{repoDir},
		Submodules: true,
	}, fakeInspect, fakeInventory, fakeRunner)

	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries (parent + submodule), got %d", len(res.Entries))
	}

	sub := res.Entries[1]
	if sub.Status != "error" || sub.Error == nil || sub.Error.Code != ReposCodePathUnsafe {
		t.Errorf("submodule expected error path-unsafe, got %+v", sub)
	}
}

func TestReposDiscoverInplaceGitDirAccepted(t *testing.T) {
	tempDir := reposTempDir(t)
	repoDir := filepath.Join(tempDir, "super")
	subDir := filepath.Join(repoDir, "sub")
	subGitDir := filepath.Join(subDir, ".git")
	if err := os.MkdirAll(subGitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)
	resolvedSub, _ := filepath.EvalSymlinks(subDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := resolvedRepo
		if path == resolvedSub {
			wt = resolvedSub
		}
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if len(args) >= 3 && args[0] == "ls-tree" {
			return "100644 blob 1234567890abcdef 100\t.gitmodules\x00", 0, nil
		}
		if len(args) >= 2 && args[0] == "config" {
			return "submodule.sub.path\nsub\x00", 0, nil
		}
		if len(args) >= 3 && args[0] == "ls-files" {
			return "160000 1111111111111111111111111111111111111111 0\tsub\x00", 0, nil
		}
		return "", 0, nil
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths:      []string{repoDir},
		Submodules: true,
	}, fakeInspect, fakeInventory, fakeRunner)

	if len(res.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(res.Entries))
	}

	sub := res.Entries[1]
	if sub.Status != "ok" {
		t.Errorf("in-place gitdir expected ok, got %s", sub.Status)
	}
}

func TestReposDiscoverDeduplicatedSubmodule(t *testing.T) {
	tempDir := reposTempDir(t)
	repoDir := filepath.Join(tempDir, "super")
	subDir := filepath.Join(repoDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)
	resolvedSub, _ := filepath.EvalSymlinks(subDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := resolvedRepo
		if path == resolvedSub {
			wt = resolvedSub
		}
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(wt, ".git"),
				CommonDir: filepath.Join(wt, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if dir != resolvedRepo && dir != repoDir {
			return "", 0, nil
		}
		if len(args) >= 3 && args[0] == "ls-tree" {
			return "100644 blob 1234567890abcdef 100\t.gitmodules\x00", 0, nil
		}
		if len(args) >= 2 && args[0] == "config" {
			return "submodule.sub.path\nsub\x00", 0, nil
		}
		if len(args) >= 3 && args[0] == "ls-files" {
			return "160000 1111111111111111111111111111111111111111 0\tsub\x00", 0, nil
		}
		return "", 0, nil
	}

	// Both repoDir and subDir are passed explicitly!
	res := reposWithRunner(context.Background(), ReposOptions{
		Paths:      []string{repoDir, subDir},
		Submodules: true,
	}, fakeInspect, fakeInventory, fakeRunner)

	if len(res.Entries) != 2 {
		t.Fatalf("expected exactly 2 entries (subDir not duplicated), got %d", len(res.Entries))
	}

	parent := res.Entries[0]
	if parent.Discovery.Deduplicated != 1 {
		t.Errorf("expected deduplicated 1, got %d", parent.Discovery.Deduplicated)
	}
}

func TestReposDiscoverStagedOnlyAndStagedRemoved(t *testing.T) {
	tempDir := reposTempDir(t)
	repoDir := filepath.Join(tempDir, "super")
	if err := os.Mkdir(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedRepo, _ := filepath.EvalSymlinks(repoDir)

	fakeInspect := func(ctx context.Context, path string) Result {
		wt := resolvedRepo
		return Result{
			Status:  "ok",
			Backend: "git",
			Repository: &Repository{
				GitDir:    filepath.Join(resolvedRepo, ".git"),
				CommonDir: filepath.Join(resolvedRepo, ".git"),
				Worktree:  &wt,
			},
		}
	}
	fakeInventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		return InventoryResult{Status: "ok", Backend: "git"}
	}

	// Case 1: staged-removed:
	// in HEAD .gitmodules, but ls-files returns empty (staged removal)
	fakeRunner := func(ctx context.Context, dir string, args ...string) (string, int, error) {
		if len(args) >= 3 && args[0] == "ls-tree" {
			return "100644 blob 1234567890abcdef 100\t.gitmodules\x00", 0, nil
		}
		if len(args) >= 2 && args[0] == "config" {
			return "submodule.removed.path\nremoved\x00", 0, nil
		}
		if len(args) >= 3 && args[0] == "ls-files" {
			// Removed from index!
			return "", 0, nil
		}
		return "", 0, nil
	}

	res := reposWithRunner(context.Background(), ReposOptions{
		Paths:      []string{repoDir},
		Submodules: true,
	}, fakeInspect, fakeInventory, fakeRunner)

	parent := res.Entries[0]
	if parent.Discovery.Ignored != 1 {
		t.Errorf("staged-removed expected ignored 1, got %d", parent.Discovery.Ignored)
	}
	if len(res.Entries) != 1 {
		t.Errorf("total entries expected 1, got %d", len(res.Entries))
	}
}
