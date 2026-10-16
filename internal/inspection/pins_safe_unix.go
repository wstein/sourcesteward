// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"io/fs"
	"os"
	"syscall"
)

// pinsOpenFlags makes a metadata read refuse symlinks and never block on a FIFO.
const pinsOpenFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW

// pinsDirFlags makes an open fail unless the path is a directory.
const pinsDirFlags = syscall.O_DIRECTORY

func pinsEffectiveUID() uint32 { return uint32(os.Geteuid()) }

// pinsFileOwner reports the owning user of an lstat result.
func pinsFileOwner(info fs.FileInfo) (uint32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

// pinsFileID reports the device and inode of an lstat result.
func pinsFileID(info fs.FileInfo) (pinsDirID, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return pinsDirID{}, false
	}
	return pinsDirID{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}, true
}

func pinsEffectiveGID() uint32 { return uint32(os.Getegid()) }

// pinsFileGroup reports the owning group of an lstat result.
func pinsFileGroup(info fs.FileInfo) (uint32, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Gid, true
}
