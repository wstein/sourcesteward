//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"syscall"
)

const noFollow = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

func pathIdentity(path string, st os.FileInfo) (PathIdentity, error) {
	v, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return PathIdentity{}, failure("unsupported-platform", "Native file identity is unavailable.")
	}
	return PathIdentity{path, uint64(v.Dev), uint64(v.Ino)}, nil
}
func privateOwner(st os.FileInfo) bool {
	v, ok := st.Sys().(*syscall.Stat_t)
	return ok && v.Uid == uint32(os.Geteuid())
}
func regularPrivate(st os.FileInfo) bool {
	v, ok := st.Sys().(*syscall.Stat_t)
	return ok && st.Mode().IsRegular() && st.Mode().Perm() == 0600 && privateOwner(st) && v.Nlink == 1
}
func lockFile(f *os.File) error {
	if e := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		return failure("state-busy", "State store is locked by another invocation; retry after it exits.")
	}
	return nil
}
