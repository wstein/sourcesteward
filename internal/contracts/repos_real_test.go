// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/inspection"
)

// reposRealFixture is a hermetic set of real Git repositories exercising every entry shape that does
// not need jj: ok, partial, error without a backend, invalid path, skipped and path-unsafe submodules.
type reposRealFixture struct {
	dir                         string
	clean, super, uninit, plain string
}

func reposRealGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "protocol.file.allow=always", "-c", "user.name=Tester", "-c", "user.email=test@example.org"}, args...)...)
	if raw, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s %v", args, raw, err)
	}
}

func newReposRealFixture(t *testing.T) reposRealFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := reposRealFixture{dir: base}
	init := func(name string) string {
		p := filepath.Join(base, name)
		if err := os.Mkdir(p, 0o755); err != nil {
			t.Fatal(err)
		}
		reposRealGit(t, p, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(p, "f"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		reposRealGit(t, p, "add", ".")
		reposRealGit(t, p, "commit", "-qm", "initial")
		return p
	}
	f.clean = init("clean")
	sub := init("sub")
	f.super = init("super")
	reposRealGit(t, f.super, "submodule", "add", "-q", sub, "s1")
	reposRealGit(t, f.super, "submodule", "add", "-q", sub, "s2")
	reposRealGit(t, f.super, "commit", "-qm", "submodules")
	// s2 becomes a symlink to the checked out s1 (path-unsafe); a clone without --recurse leaves s1 uninitialized.
	f.uninit = filepath.Join(base, "uninit")
	reposRealGit(t, base, "clone", "-q", f.super, f.uninit)
	if err := os.RemoveAll(filepath.Join(f.super, "s2")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("s1", filepath.Join(f.super, "s2")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(f.clean, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.plain = filepath.Join(base, "plain")
	if err := os.Mkdir(f.plain, 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestReposRealOutputValidatesAgainstSchemas runs the real command and validates what it prints, the
// shapes hand-written fixtures cannot prove: null diagnostics, empty backends and message formats.
func TestReposRealOutputValidatesAgainstSchemas(t *testing.T) {
	f := newReposRealFixture(t)
	repos, inventory := schema(t, "repos"), schema(t, "inventory")
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"healthy", []string{"repos", f.clean, "--json"}, 0},
		{"invalid-and-plain", []string{"repos", filepath.Join(f.dir, "absent"), f.plain, f.clean, f.clean, "--json"}, 1},
		{"submodule-states", []string{"repos", f.super, f.uninit, "--submodules", "--json"}, 1},
		{"subdirectory", []string{"repos", filepath.Join(f.clean, "subdir"), "--json"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diag bytes.Buffer
			if code := cli.Run(context.Background(), "test", tc.args, &out, &diag); code != tc.want {
				t.Fatalf("exit %d want %d: %s %s", code, tc.want, out.String(), diag.String())
			}
			valid(t, repos, out.Bytes())
			var result struct {
				Entries []struct {
					Inventory json.RawMessage `json:"inventory"`
				} `json:"entries"`
			}
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			for _, e := range result.Entries {
				if len(e.Inventory) > 0 && string(e.Inventory) != "null" {
					valid(t, inventory, e.Inventory)
				}
			}
		})
	}
}

// TestReposUsageErrorResultValidates covers the library-level usage error result, which the command
// never prints (exit 2 emits no JSON) but API callers marshal.
func TestReposUsageErrorResultValidates(t *testing.T) {
	result := inspection.Repos(context.Background(), inspection.ReposOptions{})
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	valid(t, schema(t, "repos"), raw)
}
