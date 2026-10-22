//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// dirLock is the advisory flock of a state directory.
type dirLock struct{ f *os.File }

// acquireLock takes the exclusive lock file and records the holder's pid in it.
func acquireLock(dir string) (*dirLock, error) {
	f, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
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
