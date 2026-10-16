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

func TestReposIntegrationRealRepositories(t *testing.T) {
	tempRoot := t.TempDir()

	// 1. Clean repo with linked worktree
	r1 := filepath.Join(tempRoot, "r1")
	os.Mkdir(r1, 0o755)
	git(t, r1, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(r1, "file1"), []byte("content1\n"), 0o644)
	git(t, r1, "add", ".")
	git(t, r1, "commit", "-qm", "initial")
	r1Linked := filepath.Join(tempRoot, "r1-linked")
	git(t, r1, "worktree", "add", "-qb", "wt-branch", r1Linked)

	// 2. Repo with stale linked worktree
	r2 := filepath.Join(tempRoot, "r2")
	os.Mkdir(r2, 0o755)
	git(t, r2, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(r2, "file2"), []byte("content2\n"), 0o644)
	git(t, r2, "add", ".")
	git(t, r2, "commit", "-qm", "initial")
	r2Stale := filepath.Join(tempRoot, "r2-stale")
	git(t, r2, "worktree", "add", "-qb", "stale-branch", r2Stale)
	if err := os.RemoveAll(r2Stale); err != nil {
		t.Fatal(err)
	}

	// 3. Dirty repository
	r3 := filepath.Join(tempRoot, "r3")
	os.Mkdir(r3, 0o755)
	git(t, r3, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(r3, "file3"), []byte("content3\n"), 0o644)
	git(t, r3, "add", ".")
	git(t, r3, "commit", "-qm", "initial")
	// Make dirty by writing uncommitted changes
	os.WriteFile(filepath.Join(r3, "file3"), []byte("modified\n"), 0o644)
	os.WriteFile(filepath.Join(r3, "untracked"), []byte("untracked\n"), 0o644)

	// 4. Nested repositories (r4 contains r4Nested as an independent repo)
	r4 := filepath.Join(tempRoot, "r4")
	os.Mkdir(r4, 0o755)
	git(t, r4, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(r4, "file4"), []byte("content4\n"), 0o644)
	git(t, r4, "add", ".")
	git(t, r4, "commit", "-qm", "initial")
	r4Nested := filepath.Join(r4, "nested-repo")
	os.Mkdir(r4Nested, 0o755)
	git(t, r4Nested, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(r4Nested, "nested-file"), []byte("nested\n"), 0o644)
	git(t, r4Nested, "add", ".")
	git(t, r4Nested, "commit", "-qm", "initial")

	// 5. Superproject with a submodule
	subSource := filepath.Join(tempRoot, "sub-src")
	os.Mkdir(subSource, 0o755)
	git(t, subSource, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(subSource, "subfile"), []byte("sub\n"), 0o644)
	git(t, subSource, "add", ".")
	git(t, subSource, "commit", "-qm", "initial")

	r5Super := filepath.Join(tempRoot, "r5-super")
	os.Mkdir(r5Super, 0o755)
	git(t, r5Super, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(r5Super, "superfile"), []byte("super\n"), 0o644)
	git(t, r5Super, "add", ".")
	git(t, r5Super, "commit", "-qm", "initial")
	// Add submodule using file protocol
	git(t, r5Super, "-c", "protocol.file.allow=always", "submodule", "add", subSource, "sub")
	git(t, r5Super, "commit", "-qm", "add submodule")

	// 6. Member whose check-attr exceeds 64 KiB
	r6Attr := filepath.Join(tempRoot, "r6-attr")
	os.Mkdir(r6Attr, 0o755)
	git(t, r6Attr, "init", "-q", "-b", "main")
	// 400 files with 160-byte paths to exceed 64 KiB check-attr output
	for i := 0; i < 400; i++ {
		longName := fmt.Sprintf("path_long_name_component_padding_to_exceed_sixty_four_kib_check_attr_limit_%04d.txt", i)
		os.WriteFile(filepath.Join(r6Attr, longName), []byte("x\n"), 0o644)
	}
	git(t, r6Attr, "add", ".")
	git(t, r6Attr, "commit", "-qm", "many long paths")

	// 7. Member with more than 1,024 tracked files
	r7Limit := filepath.Join(tempRoot, "r7-limit")
	os.Mkdir(r7Limit, 0o755)
	git(t, r7Limit, "init", "-q", "-b", "main")
	for i := 0; i < 1030; i++ {
		os.WriteFile(filepath.Join(r7Limit, fmt.Sprintf("f%04d", i)), []byte("1\n"), 0o644)
	}
	git(t, r7Limit, "add", ".")
	git(t, r7Limit, "commit", "-qm", "over 1024 files")

	// Execute Repos on all members with --submodules
	paths := []string{r1, r2, r3, r4, r4Nested, r5Super, r6Attr, r7Limit}
	res := Repos(context.Background(), ReposOptions{
		Paths:      paths,
		Submodules: true,
	})

	if len(res.Entries) < len(paths) {
		t.Fatalf("expected at least %d entries, got %d", len(paths), len(res.Entries))
	}

	// Verify each embedded inventory matches standalone Inventory call
	for _, entry := range res.Entries {
		if entry.Inventory == nil {
			continue
		}
		expectedInv := Inventory(context.Background(), *entry.ResolvedPath, InventoryOptions{})
		if entry.Inventory.Status != expectedInv.Status {
			t.Errorf("entry %q inventory status mismatch: embedded=%s, standalone=%s", entry.Path, entry.Inventory.Status, expectedInv.Status)
		}
		if entry.Inventory.Backend != expectedInv.Backend {
			t.Errorf("entry %q backend mismatch: %s vs %s", entry.Path, entry.Inventory.Backend, expectedInv.Backend)
		}
	}

	// Specific member assertions:
	// r1 (clean with linked worktree) -> ok
	if res.Entries[0].Status != "ok" {
		t.Errorf("r1 expected ok, got %s", res.Entries[0].Status)
	}

	// r2 (stale linked worktree) -> partial
	if res.Entries[1].Status != "partial" {
		t.Errorf("r2 (stale worktree) expected partial, got %s", res.Entries[1].Status)
	}

	// r3 (dirty) -> ok (dirty state is a fact, does not make status partial)
	if res.Entries[2].Status != "ok" {
		t.Errorf("r3 expected ok, got %s", res.Entries[2].Status)
	}
	if res.Entries[2].Facts == nil || res.Entries[2].Facts.Dirty.Value != "true" {
		t.Errorf("r3 expected dirty.value true, got %+v", res.Entries[2].Facts)
	}

	// r4Nested is contained in r4
	nestedIdx := 4
	if res.Entries[nestedIdx].ContainedIn == nil || *res.Entries[nestedIdx].ContainedIn != 3 {
		t.Errorf("r4Nested expected contained_in 3, got %+v", res.Entries[nestedIdx].ContainedIn)
	}

	// r5Super (gitlink-bearing) -> partial ("submodule disk state excluded")
	if res.Entries[5].Status != "partial" {
		t.Errorf("r5Super expected partial, got %s", res.Entries[5].Status)
	}

	// r6Attr (check-attr > 64 KiB) -> error (git-inventory-failed)
	if res.Entries[6].Status != "error" {
		t.Errorf("r6Attr expected error, got %s", res.Entries[6].Status)
	}
	if res.Entries[6].Error == nil || !strings.Contains(res.Entries[6].Error.Code, "git-inventory-failed") {
		t.Errorf("r6Attr expected git-inventory-failed, got %+v", res.Entries[6].Error)
	}

	// r7Limit (> 1024 files) -> error (git-inventory-failed or inventory-limit)
	if res.Entries[7].Status != "error" {
		t.Errorf("r7Limit expected error, got %s", res.Entries[7].Status)
	}
	if res.Entries[7].Error == nil || (!strings.Contains(res.Entries[7].Error.Code, "git-inventory-failed") && !strings.Contains(res.Entries[7].Error.Code, "inventory-limit")) {
		t.Errorf("r7Limit expected git-inventory-failed or inventory-limit, got %+v", res.Entries[7].Error)
	}

	// Overall status must be partial (qualified observations, some ok, some partial, some error)
	if res.Status != "partial" {
		t.Errorf("expected overall status partial, got %s", res.Status)
	}

	// Boundary complete must be false
	if res.Boundary.Complete {
		t.Errorf("expected boundary.complete false")
	}
}
