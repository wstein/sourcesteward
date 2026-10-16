// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// T010: a repository replaced between the identity inspection and the inventory is changed-repository, its
// inventory is discarded, and the other members are untouched. The swap happens on the real file system.
func TestReposRealRepositoryReplacedBetweenIdentityAndInventory(t *testing.T) {
	reposHermetic(t)
	base := reposTempDir(t)
	victim := reposNewRepo(t, filepath.Join(base, "victim"))
	other := reposNewRepo(t, filepath.Join(base, "other"))
	impostor := reposNewRepo(t, filepath.Join(base, "impostor"))
	if err := os.WriteFile(filepath.Join(impostor, "f"), []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, impostor, "commit", "-qam", "different history")
	var once sync.Once
	inventory := func(ctx context.Context, path string, o InventoryOptions) InventoryResult {
		if path == victim {
			once.Do(func() {
				if err := os.Rename(victim, victim+".moved"); err != nil {
					t.Error(err)
				}
				if err := os.Rename(impostor, victim); err != nil {
					t.Error(err)
				}
			})
		}
		return Inventory(ctx, path, o)
	}
	res := reposWithRunner(context.Background(), ReposOptions{Paths: []string{victim, other}}, Inspect, inventory, newReposRunner)
	e := res.Entries[0]
	if e.Status != "error" || e.Error == nil || e.Error.Code != ReposCodeChangedRepository || e.Inventory != nil || e.Facts != nil {
		t.Fatalf("swapped repository: %+v %+v", e, e.Error)
	}
	if o := res.Entries[1]; o.Status != "ok" || o.Inventory == nil {
		t.Fatalf("sibling damaged: %+v", o)
	}
}

// extensions.submodulePathConfig (Git 2.56) stores a submodule's Git directory under the configured name,
// encoded when the name contains a slash. Skipped before any fixture is created on older Git.
func TestReposDiscoverSubmodulePathConfig(t *testing.T) {
	reposRequireGit(t, 56)
	reposHermetic(t)
	base := reposTempDir(t)
	super := reposNewRepo(t, filepath.Join(base, "super"))
	src := reposNewRepo(t, filepath.Join(base, "src"))
	git(t, super, "config", "core.repositoryformatversion", "1")
	git(t, super, "config", "extensions.submodulePathConfig", "true")
	git(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", "--name", "n/x", src, "s1")
	git(t, super, "commit", "-qm", "path config")
	e := reposEntry(t, reposRun([]string{super}, true), "s1")
	if e.Status != "ok" || e.Error != nil {
		t.Fatalf("submodule with a configured Git directory name: %+v %+v", e, e.Error)
	}
	// A configured gitdir that leaves the modules directory is refused.
	git(t, super, "config", "submodule.n/x.gitdir", "../../outside")
	e = reposEntry(t, reposRun([]string{super}, true), "s1")
	if e.Status != "error" || e.Error == nil || e.Error.Code != "path-unsafe" {
		t.Fatalf("escaping configured gitdir: %+v %+v", e, e.Error)
	}
}
