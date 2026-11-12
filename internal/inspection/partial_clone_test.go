// SPDX-License-Identifier: EUPL-1.2
package inspection_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/gittest"
	"github.com/wstein/sourcesteward/internal/inspection"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.invalid", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.invalid")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func missingObjects(t *testing.T, dir string) string {
	t.Helper()
	var m []string
	for _, l := range strings.Split(gitCmd(t, dir, "rev-list", "--objects", "--missing=print", "--all"), "\n") {
		if strings.HasPrefix(l, "?") {
			m = append(m, l)
		}
	}
	return strings.Join(m, "\n")
}

// Git before 2.46 fetches missing objects of a partial clone although GIT_NO_LAZY_FETCH=1 is set. The
// inventory promises no network access, so on such Git it does not read the clone (it reports an
// incomplete result) and fetches nothing. On Git 2.46 and later the variable is honored.
func TestInventoryDoesNotFetchInAPartialCloneOnGitBefore246(t *testing.T) {
	gittest.RequireBelow(t, 2, 46, "GIT_NO_LAZY_FETCH is ignored or rejected")
	origin := t.TempDir()
	gitCmd(t, origin, "init", "-q", "-b", "main")
	gitCmd(t, origin, "config", "uploadpack.allowFilter", "true")
	gitCmd(t, origin, "config", "uploadpack.allowAnySHA1InWant", "true")
	if err := os.WriteFile(filepath.Join(origin, "f"), []byte(strings.Repeat("line\n", 200)), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, origin, "add", "f")
	gitCmd(t, origin, "commit", "-qm", "one")
	// The last commit renames and changes the file: rename detection (diff -M) reads the old blob, which the
	// checkout of the clone does not fetch.
	gitCmd(t, origin, "mv", "f", "g")
	if err := os.WriteFile(filepath.Join(origin, "g"), []byte(strings.Repeat("line\n", 200)+"more\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, origin, "add", "g")
	gitCmd(t, origin, "commit", "-qm", "rename")
	clone := filepath.Join(t.TempDir(), "clone")
	gitCmd(t, filepath.Dir(clone), "-c", "protocol.file.allow=always", "clone", "-q", "--filter=blob:none", "file://"+origin, clone)
	before := missingObjects(t, clone)
	if before == "" {
		t.Fatal("the fixture is not a partial clone with missing objects")
	}
	r := inspection.Inventory(context.Background(), clone, inspection.InventoryOptions{Scopes: []string{"stored"}, Revision: "HEAD"})
	if r.Status == "ok" {
		t.Fatalf("the inventory read a partial clone on Git before 2.46: %+v", r)
	}
	if after := missingObjects(t, clone); after != before {
		t.Fatalf("missing objects changed: %q -> %q", before, after)
	}
}
