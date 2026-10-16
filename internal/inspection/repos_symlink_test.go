// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A repository reached through a symlinked parent or working directory is the same repository Git reports
// by its real path. Comparing the unresolved path with Git's made every such path not-repository-root.
func TestReposPathThroughSymlinkedParent(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	real := reposNewRepo(t, filepath.Join(base, "real", "r"))
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "real"), link); err != nil {
		t.Fatal(err)
	}
	given := filepath.Join(link, "r")
	res := Repos(context.Background(), ReposOptions{Paths: []string{given}})
	e := res.Entries[0]
	if e.Status != "ok" || e.Error != nil {
		t.Fatalf("symlinked parent refused: %+v", e)
	}
	if e.Path != given || e.ResolvedPath == nil || *e.ResolvedPath != real {
		t.Fatalf("both paths must be reported, got path %q resolved %v", e.Path, e.ResolvedPath)
	}
}

func TestReposPathThroughSymlinkedWorkingDirectory(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	real := reposNewRepo(t, filepath.Join(base, "real", "r"))
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "real"), link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link) // sets PWD to the symlinked name, which os.Getwd honors
	for _, given := range []string{"r", "./r", filepath.Join("..", "link", "r")} {
		res := Repos(context.Background(), ReposOptions{Paths: []string{given}})
		e := res.Entries[0]
		if e.Status != "ok" || e.ResolvedPath == nil || *e.ResolvedPath != real {
			t.Fatalf("%q: relative path through a symlinked cwd refused: %+v", given, e)
		}
	}
}

// Paths are data: a directory whose name starts with a dash is an ordinary path.
func TestReposLeadingDashDirectoryIsAccepted(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	real := reposNewRepo(t, filepath.Join(base, "-dash"))
	t.Chdir(base)
	res := Repos(context.Background(), ReposOptions{Paths: []string{"-dash"}})
	e := res.Entries[0]
	if e.Status != "ok" || e.ResolvedPath == nil || *e.ResolvedPath != real {
		t.Fatalf("leading-dash directory refused: %+v", e)
	}
}

// One resolved path that is not valid UTF-8 must be that entry's invalid-path, not an output-limit that
// nulls every other inventory.
func TestReposNonUTF8ResolvedPathIsInvalidPathOnly(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	bad := filepath.Join(base, "bad\xff")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Skipf("file system rejects non-UTF-8 names: %v", err)
	}
	reposNewRepo(t, bad)
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(bad, alias); err != nil {
		t.Fatal(err)
	}
	good := reposNewRepo(t, filepath.Join(base, "good"))
	res := Repos(context.Background(), ReposOptions{Paths: []string{alias, good}})
	if e := res.Entries[0]; e.Status != "error" || e.Error == nil || e.Error.Code != "invalid-path" {
		t.Fatalf("non-UTF-8 resolved path: %+v", e)
	}
	if e := res.Entries[1]; e.Status != "ok" || e.Inventory == nil {
		t.Fatalf("healthy member damaged: %+v", e)
	}
	if res.Summary.InventoriesOmitted != 0 {
		t.Fatalf("inventories omitted: %d", res.Summary.InventoriesOmitted)
	}
	if err := ReposCheckJSONBudget(res); err != nil {
		t.Fatalf("result does not encode: %v", err)
	}
}
