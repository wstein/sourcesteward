// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceID(t *testing.T) {
	if got := WorkspaceID("github.com", "", []string{"C/D", "a/b", "A/B"}); got != "54c7f4a089e18dae" {
		t.Fatalf("repos id %s", got)
	}
	if got := WorkspaceID("github.com", "/w/root", []string{"a/b"}); got != "ed569e7596a66e60" {
		t.Fatalf("workspace id %s", got)
	}
	if WorkspaceID("github.com", "", []string{"a/b"}) == WorkspaceID("ghe.example", "", []string{"a/b"}) {
		t.Fatal("host must be part of the id")
	}
}

func TestDefaultStateDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/x/state")
	if got := DefaultStateDir("abc"); got != "/x/state/stw/watch/abc" {
		t.Fatal(got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/h")
	if got := DefaultStateDir("abc"); got != "/h/.local/state/stw/watch/abc" {
		t.Fatal(got)
	}
}

func TestEnsureStateDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a", "state")
	if err := EnsureStateDir(dir); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dir)
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v", st.Mode())
	}
	if err := EnsureStateDir(dir); err != nil {
		t.Fatalf("existing 0700: %v", err)
	}
	os.Chmod(dir, 0o755)
	if err := EnsureStateDir(dir); !errors.Is(err, ErrStateDirMode) {
		t.Fatalf("wide mode: %v", err)
	}
	if err := EnsureStateDir("relative/dir"); !errors.Is(err, ErrStateDirUsage) {
		t.Fatalf("relative: %v", err)
	}
}

func TestEnsureStateDirRefusesWorkTreesAndStwConfigDirs(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".git"), 0o700) // a work tree root: .git as a directory
	if err := EnsureStateDir(filepath.Join(root, "sub", "state")); !errors.Is(err, ErrInsideWorkTree) {
		t.Fatalf("git dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sub")); err == nil {
		t.Fatal("a refused directory must not be created")
	}
	linked := t.TempDir()
	os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: /elsewhere\n"), 0o600) // linked worktree or submodule
	if err := EnsureStateDir(filepath.Join(linked, "state")); !errors.Is(err, ErrInsideWorkTree) {
		t.Fatalf("git file: %v", err)
	}
	if err := EnsureStateDir(filepath.Join(t.TempDir(), ".stw", "state")); !errors.Is(err, ErrInsideWorkTree) {
		t.Fatalf(".stw: %v", err)
	}
	// a symlink into a work tree is resolved
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(root, link)
	if err := EnsureStateDir(filepath.Join(link, "state2")); !errors.Is(err, ErrInsideWorkTree) {
		t.Fatalf("symlink: %v", err)
	}
}

func TestLockExcludesASecondWatcherAndNamesTheHolder(t *testing.T) {
	dir := t.TempDir()
	l1, err := acquireLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = acquireLock(dir)
	var held *LockedError
	if !errors.As(err, &held) || held.PID != os.Getpid() {
		t.Fatalf("second lock: %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(os.Getpid())) {
		t.Fatal(err)
	}
	l1.Close()
	l2, err := acquireLock(dir)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	l2.Close()
	st, _ := os.Stat(filepath.Join(dir, "lock"))
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
}

func TestHeartbeatAndAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	if err := writeHeartbeat(dir, now, 42, "stream"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "heartbeat"))
	if string(raw) != "2026-10-22T09:00:00Z 42 stream\n" {
		t.Fatalf("%q", raw)
	}
	st, _ := os.Stat(filepath.Join(dir, "heartbeat"))
	if st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "tmp") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
}
