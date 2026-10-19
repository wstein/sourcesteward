// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"errors"
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

// Mode is part of the identity. A directory edge is compared by identity alone
// (no ctime), so a chmod-only change of an ancestor is refused only through Mode.
func TestBuildViewIdentityIncludesModeBits(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ancestor")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("fixture changed the inode")
	}
	if sameBuildViewIdentity(before, after) {
		t.Fatal("identity ignores a chmod-only change")
	}
	if !sameBuildViewIdentity(before, before) {
		t.Fatal("identity differs from itself")
	}
}

func TestBuildViewValidateRefusesChmodOnlyAncestorChange(t *testing.T) {
	base := t.TempDir()
	ancestor := filepath.Join(base, "ancestor")
	if err := os.Mkdir(ancestor, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(ancestor, "index")
	if err := os.WriteFile(file, []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	binding, err := captureBuildViewPath(context.Background(), file, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	if err := binding.validate(); err != nil {
		t.Fatalf("unchanged binding refused: %v", err)
	}
	if err := os.Chmod(ancestor, 0750); err != nil {
		t.Fatal(err)
	}
	if err := binding.validate(); err == nil {
		t.Fatal("chmod-only change of a bound ancestor accepted")
	}
}

// Cancellation must SIGKILL the whole owned group and return promptly. The
// helper sleeps for an hour, so only the kill can end it: a missing kill fails
// here with a message instead of hanging until the package timeout.
func TestBuildViewSupervisorKillsOwnedGroupWithinDeadline(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pidCh := make(chan int, 1)
	type outcome struct {
		err error
	}
	done := make(chan outcome, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go func() {
		_, _, e := runBuildViewNative(ctx, buildViewCommand{Executable: binary, Dir: t.TempDir(), Env: []string{"STW_BUILD_VIEW_NATIVE_TEST=wait", "GORACE=atexit_sleep_ms=0"}}, &bytes.Buffer{}, buildViewNativeHooks{started: func(pid int) { pidCh <- pid }})
		done <- outcome{e}
	}()
	pid := <-pidCh
	defer func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }() // never leak the sleeper if the assertion fails
	select {
	case o := <-done:
		if !errors.Is(o.err, context.DeadlineExceeded) {
			t.Fatalf("canceled group returned %v, want the deadline error", o.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the supervisor did not SIGKILL the owned process group within 5s of cancellation")
	}
	if err := syscall.Kill(-pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("owned process group %d still exists after return: %v", pid, err)
	}
}
