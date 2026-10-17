// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package inspection

import (
	"os"
	"syscall"
)

// changeStamp is the inode change time in nanoseconds; the kernel sets it, a tool cannot restore it.
func changeStamp(info os.FileInfo) (int64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int64(st.Ctim.Sec)*1e9 + int64(st.Ctim.Nsec), true
}
