// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package release

import (
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strings"
)

var errOutputExists = errors.New("output-exists")

// Hooks synchronize hostile fixtures and inject ordinary file-write/close errors.
// Production call sites supply no hooks; no native syscall function is exposed.
type recordWriteHooks struct {
	step       func(stage, path string)
	write      func(*os.File, []byte) (int, error)
	afterClose func() error
}

// writeRecordExclusive walks one component at a time relative to pinned directory
// descriptors. NOFOLLOW applies atomically at every open; no pathname check is trusted.
func writeRecordExclusive(root, path string, data []byte, testHooks ...recordWriteHooks) (written bool, retErr error) {
	var hooks recordWriteHooks
	if len(testHooks) > 0 {
		hooks = testHooks[0]
	}
	step := func(stage, path string) {
		if hooks.step != nil {
			hooks.step(stage, path)
		}
	}
	if !ValidRepoPath(path) {
		return false, errOutputExists
	}
	parts := strings.Split(path, "/")
	if len(parts) < 3 || parts[0] != "docs" || parts[1] != "releases" {
		return false, errOutputExists
	}
	dir, e := os.OpenFile(root, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return false, fmt.Errorf("%w: %v", errOutputExists, e)
	}
	defer func() {
		if e := dir.Close(); retErr == nil && e != nil {
			retErr = e
		}
	}()
	prefix := ""
	for _, part := range parts[:len(parts)-1] {
		if prefix != "" {
			prefix += "/"
		}
		prefix += part
		step("before-component", prefix)
		fd, e := unix.Openat(int(dir.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(e, os.ErrNotExist) {
			step("before-mkdir", prefix)
			e = unix.Mkdirat(int(dir.Fd()), part, 0755)
			if e != nil && !errors.Is(e, os.ErrExist) {
				return false, fmt.Errorf("%w: %v", errOutputExists, e)
			}
			fd, e = unix.Openat(int(dir.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if e != nil {
			return false, fmt.Errorf("%w: %v", errOutputExists, e)
		}
		next := os.NewFile(uintptr(fd), part)
		if e := dir.Close(); e != nil {
			next.Close()
			return false, e
		}
		dir = next
		step("after-component", prefix)
	}
	name := parts[len(parts)-1]
	step("before-final", path)
	fd, e := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0644)
	if errors.Is(e, os.ErrExist) {
		fd, e = unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if e != nil {
			return false, errOutputExists
		}
		f := os.NewFile(uintptr(fd), name)
		defer func() {
			if e := f.Close(); retErr == nil && e != nil {
				retErr = e
			}
		}()
		st, e := f.Stat()
		if e != nil || !st.Mode().IsRegular() || st.Size() != int64(len(data)) {
			return false, errOutputExists
		}
		existing, e := io.ReadAll(io.LimitReader(f, int64(len(data))+1))
		step("after-existing-read", path)
		after, statErr := f.Stat()
		if e != nil || statErr != nil || after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) || after.Mode() != st.Mode() || !bytes.Equal(existing, data) {
			return false, errOutputExists
		}
		return false, nil
	}
	if e != nil {
		return false, fmt.Errorf("%w: %v", errOutputExists, e)
	}
	f := os.NewFile(uintptr(fd), name)
	if e = f.Chmod(0644); e != nil {
		f.Close()
		return true, e
	}
	var n int
	if hooks.write != nil {
		n, e = hooks.write(f, data)
	} else {
		n, e = f.Write(data)
	}
	if e == nil && n != len(data) {
		e = io.ErrShortWrite
	}
	closeErr := f.Close()
	if closeErr == nil && hooks.afterClose != nil {
		closeErr = hooks.afterClose()
	}
	if e != nil {
		return true, e
	}
	return true, closeErr
}
