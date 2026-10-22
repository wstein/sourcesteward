//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAcquireLockOutlastsAStatusProbe(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := EnsureStateDir(dir); err != nil {
		t.Fatal(err)
	}
	probe, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(probe.Fd()), syscall.LOCK_SH); err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(400 * time.Millisecond); probe.Close() }()
	l, err := acquireLock(dir)
	if err != nil {
		t.Fatalf("a shared probe must not fail a starting watcher: %v", err)
	}
	// a real holder is still reported, after the bounded retry
	start := time.Now()
	_, err = acquireLock(dir)
	var locked *LockedError
	if !errors.As(err, &locked) || locked.PID != os.Getpid() {
		t.Fatalf("%v", err)
	}
	if d := time.Since(start); d < 800*time.Millisecond || d > 3*time.Second {
		t.Fatalf("retry took %s", d)
	}
	l.Close()
}
