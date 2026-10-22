//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// dirLock is the advisory flock of a state directory.
type dirLock struct{ f *os.File }

// lockAttempts and lockPause bound the retry of acquireLock: a --status probe holds a shared lock for
// an instant and must not make a starting watcher report a second watcher.
const (
	lockAttempts = 5
	lockPause    = 250 * time.Millisecond
)

// acquireLock takes the exclusive lock file and records the holder's pid in it. A busy lock is retried
// briefly (about one second) before it is reported.
func acquireLock(dir string) (*dirLock, error) {
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	var lockErr error
	for i := 0; i < lockAttempts; i++ {
		if lockErr = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); lockErr == nil {
			break
		}
		if i < lockAttempts-1 {
			time.Sleep(lockPause)
		}
	}
	if lockErr != nil {
		raw := make([]byte, 32)
		n, _ := f.ReadAt(raw, 0)
		f.Close()
		pid, _ := strconv.Atoi(strings.TrimSpace(string(raw[:n])))
		return nil, &LockedError{PID: pid}
	}
	f.Truncate(0)
	f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	return &dirLock{f}, nil
}

func (l *dirLock) Close() error {
	syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}

// lockHolder reports whether a watcher holds the lock of dir and its recorded pid. It takes a shared
// non-blocking lock only to probe, writes nothing and never creates the lock file.
func lockHolder(dir string) (bool, int) {
	f, err := os.Open(filepath.Join(dir, "lock"))
	if err != nil {
		return false, 0
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return false, 0
	}
	raw := make([]byte, 32)
	n, _ := f.ReadAt(raw, 0)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw[:n])))
	return true, pid
}
