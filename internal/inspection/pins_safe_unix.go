// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"io/fs"
	"syscall"
)

// pinsOpenFlags makes a metadata read refuse symlinks and never block on a FIFO.
const pinsOpenFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW

// pinsFileID reports the device and inode of an lstat result.
func pinsFileID(info fs.FileInfo) (pinsDirID, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return pinsDirID{}, false
	}
	return pinsDirID{Dev: uint64(st.Dev), Ino: uint64(st.Ino)}, true
}
