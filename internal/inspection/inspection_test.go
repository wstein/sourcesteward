// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGitFixtureDoesNotLaunchAutomaticMaintenance(t *testing.T) {
	trace := filepath.Join(t.TempDir(), "fixture-trace.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	b, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid Git trace event: %v", err)
		}
		if event.Event != "child_start" {
			continue
		}
		for i, arg := range event.Argv {
			if arg == "maintenance" && i+1 < len(event.Argv) && event.Argv[i+1] == "run" {
				t.Fatal("fixture commit spawned automatic maintenance, which can outlive setup and invalidate no-write snapshots")
			}
		}
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	// Fixture writes must finish before no-write snapshots start. Automatic
	// maintenance can leave a detached child changing repository lock files.
	c := exec.Command("git", append([]string{"-c", "maintenance.auto=false"}, args...)...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %s %v", args, b, e)
	}
	return string(b)
}
func repo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	git(t, d, "init", "-q", "-b", "main")
	return d
}
func TestIdentity(t *testing.T) {
	d := repo(t)
	r := Inspect(context.Background(), d)
	if r.Status != "ok" || r.Repository.Head.State != "unborn" {
		t.Fatalf("%+v", r)
	}
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	r = Inspect(context.Background(), d)
	if r.Status != "ok" || r.Repository.Head.State != "attached" || *r.Repository.Head.Ref != "refs/heads/main" {
		t.Fatalf("%+v", r)
	}
	git(t, d, "checkout", "--detach", "-q")
	r = Inspect(context.Background(), d)
	if r.Status != "ok" || r.Repository.Head.State != "detached" || r.Repository.Head.Ref != nil {
		t.Fatalf("%+v", r)
	}
}
func TestManagedAndMalformed(t *testing.T) {
	d := repo(t)
	if e := os.Mkdir(filepath.Join(d, ".jj"), 0700); e != nil {
		t.Fatal(e)
	}
	r := Inspect(context.Background(), d)
	if r.Status != "error" || r.Backend != "jj" || r.SchemaVersion != 2 {
		t.Fatalf("%+v", r)
	}
	os.Mkdir(filepath.Join(d, ".git", "gitbutler"), 0700)
	r = Inspect(context.Background(), d)
	if r.Backend != "ambiguous" {
		t.Fatalf("%+v", r)
	}
	d = repo(t)
	os.WriteFile(filepath.Join(d, ".git", "refs", "heads", "main"), []byte("corrupt\n"), 0600)
	r = Inspect(context.Background(), d)
	if r.Status != "error" {
		t.Fatalf("corrupt branch accepted: %+v", r)
	}
}

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() {
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			out[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestShapesAndNoWrites(t *testing.T) {
	for _, shape := range []string{"normal", "bare", "linked", "sha256", "newline"} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			d := filepath.Join(root, "repository")
			if shape == "newline" {
				d = filepath.Join(root, "space \n日本語")
			}
			os.Mkdir(d, 0700)
			args := []string{"init", "-q", "-b", "main"}
			if shape == "bare" {
				args = append(args, "--bare")
			}
			if shape == "sha256" {
				args = append(args, "--object-format=sha256")
			}
			git(t, d, args...)
			if shape != "bare" {
				git(t, d, "commit", "--allow-empty", "-qm", "initial")
			}
			if shape == "linked" {
				linked := filepath.Join(root, "linked")
				git(t, d, "worktree", "add", "-qb", "topic", linked)
				d = linked
			}
			before := snapshot(t, root)
			r := Inspect(context.Background(), d)
			after := snapshot(t, root)
			if r.Status != "ok" {
				t.Fatalf("%+v", r)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("inspection changed bytes at relative paths: %q", changedSnapshotPaths(root, before, after))
			}
			if r.Repository.Bare != (shape == "bare") || r.Repository.LinkedWorktree != (shape == "linked") {
				t.Fatalf("%+v", r.Repository)
			}
			if shape == "sha256" && len(*r.Repository.Head.OID) != 64 {
				t.Fatal("sha256")
			}
			if shape == "newline" && *r.Repository.Worktree != func() string { v, _ := filepath.EvalSymlinks(d); return v }() {
				t.Fatalf("path lost:%q", *r.Repository.Worktree)
			}
		})
	}
}

func changedSnapshotPaths(root string, before, after map[string]string) []string {
	changed := map[string]bool{}
	for path, b := range before {
		if a, ok := after[path]; !ok || a != b {
			changed[path] = true
		}
	}
	for path, a := range after {
		if b, ok := before[path]; !ok || a != b {
			changed[path] = true
		}
	}
	paths := make([]string, 0, len(changed))
	for path := range changed {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			relative = path
		}
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	return paths
}
func TestDetectionBoundaries(t *testing.T) {
	t.Run("nested", func(t *testing.T) {
		outer := repo(t)
		os.Mkdir(filepath.Join(outer, ".jj"), 0700)
		inner := filepath.Join(outer, "inner")
		os.Mkdir(inner, 0700)
		git(t, inner, "init", "-q", "-b", "main")
		r := Inspect(context.Background(), inner)
		if r.Status != "ok" || r.Backend != "git" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("standalone jj", func(t *testing.T) {
		d := t.TempDir()
		os.Mkdir(filepath.Join(d, ".jj"), 0700)
		r := Inspect(context.Background(), d)
		if r.Backend != "jj" || r.Status != "error" || r.SchemaVersion != 2 {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("marker symlink", func(t *testing.T) {
		d := repo(t)
		os.Symlink(t.TempDir(), filepath.Join(d, ".jj"))
		r := Inspect(context.Background(), d)
		if r.Diagnostics[0].Code != "invalid-metadata" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("packed ref", func(t *testing.T) {
		d := repo(t)
		git(t, d, "commit", "--allow-empty", "-qm", "initial")
		git(t, d, "update-ref", "refs/gitbutler/test", "HEAD")
		git(t, d, "pack-refs", "--all")
		r := Inspect(context.Background(), d)
		if r.Backend != "gitbutler" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("prefix collision", func(t *testing.T) {
		d := repo(t)
		git(t, d, "commit", "--allow-empty", "-qm", "initial")
		git(t, d, "branch", "gitbutler/workspace-other")
		r := Inspect(context.Background(), d)
		if r.Status != "ok" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("common dir", func(t *testing.T) {
		d := repo(t)
		git(t, d, "commit", "--allow-empty", "-qm", "initial")
		linked := filepath.Join(t.TempDir(), "linked")
		git(t, d, "worktree", "add", "-qb", "topic", linked)
		os.Mkdir(filepath.Join(d, ".git", "gitbutler"), 0700)
		r := Inspect(context.Background(), linked)
		if r.Backend != "gitbutler" {
			t.Fatalf("%+v", r)
		}
	})
}
func TestCorruptHead(t *testing.T) {
	for _, kind := range []string{"missing object", "malformed branch ref", "blob", "invalid head", "nonbranch"} {
		t.Run(kind, func(t *testing.T) {
			d := repo(t)
			switch kind {
			case "malformed branch ref":
				os.WriteFile(filepath.Join(d, ".git", "refs", "heads", "main"), []byte("not-an-object-id\n"), 0600)
			case "missing object":
				os.WriteFile(filepath.Join(d, ".git", "refs", "heads", "main"), []byte(strings.Repeat("1", 40)+"\n"), 0600)
			case "blob":
				oid := strings.TrimSpace(git(t, d, "hash-object", "-w", "--stdin"))
				os.WriteFile(filepath.Join(d, ".git", "refs", "heads", "main"), []byte(oid+"\n"), 0600)
			case "invalid head":
				os.WriteFile(filepath.Join(d, ".git", "HEAD"), []byte("bad\n"), 0600)
			case "nonbranch":
				os.WriteFile(filepath.Join(d, ".git", "HEAD"), []byte("ref: refs/tags/nope\n"), 0600)
			}
			r := Inspect(context.Background(), d)
			if r.Status != "error" {
				t.Fatalf("accepted:%+v", r)
			}
		})
	}
}
func TestEnvironmentAndHelpers(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	sentinel := filepath.Join(t.TempDir(), "sentinel")
	helper := filepath.Join(t.TempDir(), "helper")
	os.WriteFile(helper, []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0700)
	for _, key := range []string{"core.fsmonitor", "core.pager", "core.editor", "core.sshCommand", "alias.rev-parse"} {
		git(t, d, "config", key, helper)
	}
	os.WriteFile(filepath.Join(d, ".git", "hooks", "post-index-change"), []byte("#!/bin/sh\ntouch '"+sentinel+"'\n"), 0700)
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("GIT_DIR", t.TempDir())
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.worktree")
	t.Setenv("GIT_CONFIG_VALUE_0", t.TempDir())
	t.Setenv("GIT_TRACE", trace)
	t.Setenv("GIT_TRACE_SETUP", trace)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "missing"))
	before := snapshot(t, d)
	r := Inspect(context.Background(), d)
	if r.Status != "ok" {
		t.Fatalf("%+v", r)
	}
	if !reflect.DeepEqual(before, snapshot(t, d)) {
		t.Fatal("repository changed")
	}
	for _, p := range []string{sentinel, trace} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("effect:%s", p)
		}
	}
}
func TestInvalidUTF8SymlinkTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, string([]byte{255}))
	if err := os.Mkdir(target, 0700); err != nil {
		t.Skip(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Skip(err)
	}
	r := Inspect(context.Background(), alias)
	if r.Status != "error" || r.Diagnostics[0].Code != "invalid-path" || !utf8.ValidString(r.InputPath) {
		t.Fatalf("%+v", r)
	}
}
func TestReplaceObjectCannotHideNonCommitHead(t *testing.T) {
	d := repo(t)
	git(t, d, "commit", "--allow-empty", "-qm", "initial")
	commit := strings.TrimSpace(git(t, d, "rev-parse", "HEAD"))
	blob := strings.TrimSpace(git(t, d, "hash-object", "-w", "--stdin"))
	git(t, d, "replace", "-f", blob, commit)
	os.WriteFile(filepath.Join(d, ".git", "refs", "heads", "main"), []byte(blob+"\n"), 0600)
	before := snapshot(t, d)
	r := Inspect(context.Background(), d)
	if r.Status != "error" || r.Diagnostics[0].Code != "invalid-metadata" || r.Repository != nil {
		t.Fatalf("replacement hid raw noncommit HEAD:%+v", r)
	}
	if !reflect.DeepEqual(before, snapshot(t, d)) {
		t.Fatal("replacement inspection changed bytes")
	}
}
