// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWorktreeReadLimit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".stw/release.toml", []byte(strings.Repeat("x", 100)))
	w := worktreeSource{dir}
	entry := sourceEntry{Name: "release.toml", Kind: "file"}
	for limit, tooLarge := range map[int]bool{99: true, 100: false, 101: false} {
		data, large, err := w.Read(entry, limit)
		if err != nil || large != tooLarge {
			t.Errorf("limit %d: tooLarge=%v err=%v", limit, large, err)
		}
		if tooLarge && len(data) != limit {
			t.Errorf("limit %d: returned %d bytes", limit, len(data))
		}
	}
}

func TestWorktreeReadRefusesAFileThatBecameASymlink(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".stw/release.toml", []byte("a = 1\n"))
	write(t, dir, "other", []byte("b = 2\n"))
	entry := sourceEntry{Name: "release.toml", Kind: "file"}
	if err := os.Remove(filepath.Join(dir, ".stw", "release.toml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../other", filepath.Join(dir, ".stw", "release.toml")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (worktreeSource{dir}).Read(entry, 1000); !errors.Is(err, errNotStable) {
		t.Fatalf("a symlink was read: %v", err)
	}
}

func TestWorktreeReadRefusesAFileSwappedWhileOpening(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".stw/release.toml", []byte("a = 1\n"))
	write(t, dir, "other", []byte("b = 2\n"))
	path := filepath.Join(dir, ".stw", "release.toml")
	beforeOpen = func(string) {
		if err := os.Remove(path); err != nil {
			t.Error(err)
		}
		if err := os.Symlink("../other", path); err != nil {
			t.Error(err)
		}
	}
	defer func() { beforeOpen = func(string) {} }()
	if _, _, err := (worktreeSource{dir}).Read(sourceEntry{Name: "release.toml", Kind: "file"}, 1000); !errors.Is(err, errNotStable) {
		t.Fatalf("a swapped file was read: %v", err)
	}
}

func TestWorktreeReadRefusesADirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".stw", "release.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (worktreeSource{dir}).Read(sourceEntry{Name: "release.toml", Kind: "file"}, 1000); !errors.Is(err, errNotStable) {
		t.Fatalf("%v", err)
	}
}

func TestWorktreeReadDoesNotBlockOnAFifo(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".stw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, ".stw", "release.toml"), 0o644); err != nil {
		t.Skipf("no fifo: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := (worktreeSource{dir}).Read(sourceEntry{Name: "release.toml", Kind: "file"}, 1000)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errNotStable) {
			t.Fatalf("%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reading a named pipe blocks; it must be refused before it is opened")
	}
}

func TestWorktreeKinds(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "real/file.txt", []byte("x"))
	write(t, dir, "top.txt", []byte("x"))
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("top.txt", filepath.Join(dir, "filelink")); err != nil {
		t.Fatal(err)
	}
	got, err := (worktreeSource{dir}).Kinds([]string{"real", "real/file.txt", "top.txt", "top.txt/below", "link", "link/file.txt", "filelink", "missing", "real/missing"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"real": "dir", "real/file.txt": "file", "top.txt": "file", "filelink": "file"}
	// A symlink to a directory is a final symlink and counts as a file; nothing below it exists.
	want["link"] = "file"
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
}
