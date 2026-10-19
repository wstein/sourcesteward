// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The final metadata open must never yield a symlink swapped in after the Lstat and must
// never block on a FIFO. os.Root resolves in-root symlinks itself and handles non-blocking
// opens, so the O_NOFOLLOW and O_NONBLOCK flags are defense in depth that this Go version
// makes unobservable; the symlink refusal is enforced by re-binding the opened descriptor
// to its name in openBuildViewFile, and that check is what this test pins.
func TestBuildViewOpenFileRefusesSymlinkAndNeverBlocksOnFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0600); err != nil {
		t.Skipf("no FIFO support: %v", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if f, err := openBuildViewFile(root, "link"); err == nil {
		f.Close()
		t.Fatal("openBuildViewFile followed a final symlink (O_NOFOLLOW missing)")
	}
	if f, err := openBuildViewFile(root, "real"); err != nil {
		t.Fatalf("regular file refused: %v", err)
	} else {
		f.Close()
	}
	type result struct {
		file *os.File
		err  error
	}
	done := make(chan result, 1)
	go func() {
		f, e := openBuildViewFile(root, "fifo")
		done <- result{f, e}
	}()
	select {
	case r := <-done:
		if r.file != nil {
			r.file.Close()
		}
	case <-time.After(2 * time.Second):
		// Release the blocked reader so the test process does not leak it.
		if w, e := os.OpenFile(filepath.Join(dir, "fifo"), os.O_WRONLY|syscall.O_NONBLOCK, 0); e == nil {
			w.Close()
		}
		t.Fatal("openBuildViewFile blocked on a FIFO (O_NONBLOCK missing)")
	}
}

// A regular file replaced by a symlink to its own relocated bytes between the
// Lstat and the open is refused during capture.
func TestBuildViewCaptureRefusesFinalComponentSwappedForSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index")
	if err := os.WriteFile(path, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	swapped := false
	binding, err := captureBuildViewPath(context.Background(), path, func(stage string) {
		if stage != "before-file" {
			return
		}
		if e := os.Rename(path, filepath.Join(dir, "moved")); e != nil {
			t.Fatal(e)
		}
		if e := os.Symlink(filepath.Join(dir, "moved"), path); e != nil {
			t.Fatal(e)
		}
		swapped = true
	})
	if binding != nil {
		binding.Close()
	}
	if !swapped {
		t.Fatal("the swap hook never ran")
	}
	if err == nil {
		t.Fatal("capture accepted a final component swapped for a symlink")
	}
}
