// SPDX-License-Identifier: EUPL-1.2
//go:build darwin

package inspection

import (
	"os"
	"syscall"
)

// osChangeStamp is the inode change time in nanoseconds; the kernel sets it, a tool cannot restore it.
func osChangeStamp(info os.FileInfo) (int64, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int64(st.Ctimespec.Sec)*1e9 + int64(st.Ctimespec.Nsec), true
}
