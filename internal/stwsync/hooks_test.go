// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hooksDir creates a directory of hooks for core.hooksPath. Every hook appends "NAME ARGS" to marker.
func hooksDir(t *testing.T, root, name, marker string, failing map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{"post-merge", "post-rewrite", "reference-transaction"} {
		body := "echo \"" + hook + " $*\" >> '" + marker + "'\n" + failing[hook]
		write(t, filepath.Join(dir, hook), "#!/bin/sh\n"+body+"exit 0\n")
		if err := os.Chmod(filepath.Join(dir, hook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// treeDigest lists every file under dirs with its content hash: what "stw installs and removes no hook" compares.
func treeDigest(t *testing.T, dirs ...string) string {
	t.Helper()
	var lines []string
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			raw, _ := os.ReadFile(path)
			sum := sha256.Sum256(raw)
			mode := d.Type().String()
			if info, e := d.Info(); e == nil {
				mode = info.Mode().String()
			}
			lines = append(lines, path+" "+mode+" "+hex.EncodeToString(sum[:]))
			return nil
		})
	}
	sortStrings(lines)
	return strings.Join(lines, "\n")
}

func readMarker(t *testing.T, marker string) string {
	t.Helper()
	raw, _ := os.ReadFile(marker)
	return string(raw)
}

// A repository with post-merge, post-rewrite and reference-transaction hooks in a core.hooksPath directory is not
// refused: the hooks of the underlying Git operations run in a real run, and stw adds and removes no hook.
func TestHooksViaCoreHooksPathRunInARealRun(t *testing.T) {
	w := newWorkspace(t)
	marker := filepath.Join(w.root, "markers")
	dir := hooksDir(t, w.root, "hooks", marker, nil)
	for _, repo := range []string{".", "b"} {
		runGit(t, w.dir(repo), "config", "core.hooksPath", dir)
	}
	// the root fast-forwards (post-merge); b is rebased (post-rewrite)
	w.advance(t, ".", "main")
	w.advance(t, "b", "main")
	commit(t, w.dir("b"), "local.txt", "l\n")
	runGit(t, w.dir("b"), "config", "pull.rebase", "true")
	gitHooks := []string{filepath.Join(w.ws, ".git", "hooks"), filepath.Join(w.ws, ".git", "modules", "b", "hooks")}
	before := treeDigest(t, append([]string{dir}, gitHooks...)...)
	_ = os.Remove(marker) // hooks of the fixture setup (commits) are not the run's
	r := run(t, w, Options{})
	got := repoByName(t, r)
	if got["."].Result != ResultFastForwarded || got["b"].Result != ResultRebased || r.Error != nil {
		t.Fatalf("%+v %+v", got["."], got["b"])
	}
	m := readMarker(t, marker)
	for _, want := range []string{"post-merge", "post-rewrite", "reference-transaction prepared", "reference-transaction committed"} {
		if !strings.Contains(m, want) {
			t.Errorf("hook %q did not run:\n%s", want, m)
		}
	}
	if after := treeDigest(t, append([]string{dir}, gitHooks...)...); after != before {
		t.Errorf("hooks were installed, removed or changed:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// The fetch of a plan runs the reference-transaction hook (it updates remote-tracking refs) and nothing else of the
// hooks that change branches: a plan makes no merge or rewrite.
func TestHooksRunInThePlansFetch(t *testing.T) {
	w := newWorkspace(t)
	marker := filepath.Join(w.root, "markers")
	dir := hooksDir(t, w.root, "hooks", marker, nil)
	runGit(t, w.ws, "config", "core.hooksPath", dir)
	w.advance(t, ".", "main")
	before := treeDigest(t, dir)
	_ = os.Remove(marker)
	r := run(t, w, Options{Plan: true})
	if repoByName(t, r)["."].Result != ResultFastForward {
		t.Fatalf("%+v", repoByName(t, r)["."])
	}
	m := readMarker(t, marker)
	if !strings.Contains(m, "reference-transaction prepared") || strings.Contains(m, "post-merge") || strings.Contains(m, "post-rewrite") {
		t.Fatalf("hooks of the plan:\n%s", m)
	}
	if treeDigest(t, dir) != before {
		t.Error("a plan changed the hooks")
	}
}

// A hook that makes Git fail fails only that item: the repository of the failing hook is failed, the other proceeds.
func TestFailingHookViaCoreHooksPathFailsOnlyThatItem(t *testing.T) {
	w := newWorkspace(t)
	marker := filepath.Join(w.root, "markers")
	bad := hooksDir(t, w.root, "bad", marker, map[string]string{"reference-transaction": "if [ \"$1\" = prepared ]; then while read o n ref; do [ \"$ref\" = refs/heads/main ] && exit 1; done; fi\n"})
	good := hooksDir(t, w.root, "good", marker, nil)
	runGit(t, w.dir("a"), "config", "core.hooksPath", bad)
	runGit(t, w.ws, "config", "core.hooksPath", good)
	runGit(t, w.dir("b"), "config", "core.hooksPath", good)
	w.advance(t, "a", "main")
	w.advance(t, "b", "main")
	oldA := w.oid(t, "a", "HEAD")
	r := run(t, w, Options{})
	got := repoByName(t, r)
	if got["a"].Result != ResultFailed || w.oid(t, "a", "HEAD") != oldA || got["b"].Result != ResultFastForwarded || r.ExitCode() != 1 {
		t.Fatalf("%+v %+v", got["a"], got["b"])
	}
}

// A hook file that Git cannot run is not a stw problem: a non-executable hook and a hook that is a directory are ignored
// by Git, the item is still processed, and a symlink to an executable hook runs.
func TestUnrunnableAndLinkedHooksViaCoreHooksPath(t *testing.T) {
	w := newWorkspace(t)
	marker := filepath.Join(w.root, "markers")
	dir := hooksDir(t, w.root, "hooks", marker, nil)
	// post-merge: not executable; post-rewrite: a directory; reference-transaction: a symlink to the real script
	if err := os.Chmod(filepath.Join(dir, "post-merge"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "post-rewrite")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "post-rewrite"), 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(w.root, "real-reference-transaction")
	if err := os.Rename(filepath.Join(dir, "reference-transaction"), real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "reference-transaction")); err != nil {
		t.Fatal(err)
	}
	for _, repo := range []string{".", "b"} {
		runGit(t, w.dir(repo), "config", "core.hooksPath", dir)
	}
	runGit(t, w.dir("b"), "config", "pull.rebase", "true")
	w.advance(t, ".", "main")
	w.advance(t, "b", "main")
	commit(t, w.dir("b"), "local.txt", "l\n")
	_ = os.Remove(marker)
	r := run(t, w, Options{})
	got := repoByName(t, r)
	if got["."].Result != ResultFastForwarded || got["b"].Result != ResultRebased || r.Error != nil {
		t.Fatalf("%+v %+v", got["."], got["b"])
	}
	m := readMarker(t, marker)
	if !strings.Contains(m, "reference-transaction prepared") {
		t.Errorf("the symlinked hook did not run:\n%s", m)
	}
	if strings.Contains(m, "post-merge") || strings.Contains(m, "post-rewrite") {
		t.Errorf("a hook Git cannot run was run:\n%s", m)
	}
}
