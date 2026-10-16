// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
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
	dir                                 string
	clean, linked, super, uninit, plain string
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
	f.linked = filepath.Join(base, "linked")
	reposRealGit(t, f.clean, "worktree", "add", "-q", "-b", "wt", f.linked)
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
	// Submodule validation refuses directories writable by others, and Git creates them that way under
	// umask 000. Fix the modes instead of the process umask, so the fixture is independent of it.
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink != 0 {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.Chmod(p, info.Mode().Perm()&^0o022)
	})
	if err != nil {
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

// TestReposFixtureEntriesMatchRealOutput keeps the hand-written fixtures honest: the error, discovery,
// backend and notes of entries without an inventory equal what the command prints for the same case.
func TestReposFixtureEntriesMatchRealOutput(t *testing.T) {
	f := newReposRealFixture(t)
	run := func(args ...string) []map[string]any {
		var out, diag bytes.Buffer
		cli.Run(context.Background(), "test", append([]string{"repos"}, append(args, "--json")...), &out, &diag)
		var doc struct {
			Entries []map[string]any `json:"entries"`
		}
		if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
			t.Fatalf("%v: %s %s", err, out.String(), diag.String())
		}
		return doc.Entries
	}
	find := func(entries []map[string]any, status string) map[string]any {
		for _, e := range entries {
			if e["status"] == status && e["inventory"] == nil {
				return e
			}
		}
		t.Fatalf("no %s entry without inventory in %v", status, entries)
		return nil
	}
	same := func(name string, real, fixture map[string]any) {
		t.Helper()
		for _, k := range []string{"backend", "error", "discovery", "notes", "origin", "facts", "inventory"} {
			r, _ := json.Marshal(real[k])
			x, _ := json.Marshal(fixture[k])
			if !bytes.Equal(r, x) {
				t.Errorf("%s: field %s differs\n real:    %v\n fixture: %v", name, k, real[k], fixture[k])
			}
		}
	}
	same("skipped", find(run("--submodules", f.uninit), "skipped"), reposObject(t, "skipped")["entries"].([]any)[1].(map[string]any))
	same("duplicate", find(run(f.clean, f.linked), "error"), reposObject(t, "duplicate")["entries"].([]any)[1].(map[string]any))
	same("not-repository-root", find(run(filepath.Join(f.clean, "subdir")), "error"), reposObject(t, "error")["entries"].([]any)[0].(map[string]any))
}

// Every error code `repos` itself raises must produce a schema-valid result. The base is a real result
// (so the embedded inventory and facts are real); each case sets the entry to the shape the data model
// prescribes for that code.
func TestReposEveryCodeShapeValidatesAgainstTheSchema(t *testing.T) {
	f := newReposRealFixture(t)
	var out, diag bytes.Buffer
	cli.Run(context.Background(), "test", []string{"repos", f.clean, "--json"}, &out, &diag)
	var base inspection.ReposResult
	if err := json.Unmarshal(out.Bytes(), &base); err != nil {
		t.Fatal(err)
	}
	repos := schema(t, "repos")
	for _, tc := range []struct {
		code, status       string
		keepInv, keepFacts bool
	}{
		{"output-limit", "error", false, true},
		{"discovery-failed", "partial", true, true},
		{"discovery-limit", "partial", true, true},
		{"inventory-timeout", "error", false, false},
		{"inventory-not-started", "error", false, false},
		{"canceled", "error", false, false},
		{"member-failed", "error", false, false},
		{"changed-repository", "error", false, false},
		{"path-unsafe", "error", false, false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			res := base
			res.Entries = append([]inspection.ReposEntry(nil), base.Entries...)
			e := res.Entries[0]
			e.Status, e.Error = tc.status, &inspection.ReposError{Code: tc.code, Message: tc.code + " message"}
			if !tc.keepInv {
				e.Inventory = nil
			}
			if !tc.keepFacts {
				e.Facts = nil
			}
			if tc.code == "output-limit" {
				res.Summary.InventoriesOmitted = 1
			}
			res.Entries[0] = e
			res.Status = "error"
			if tc.status == "partial" {
				res.Status = "partial"
			}
			res.Boundary.Complete = false
			raw, err := json.Marshal(res)
			if err != nil {
				t.Fatal(err)
			}
			valid(t, repos, raw)
		})
	}
}
