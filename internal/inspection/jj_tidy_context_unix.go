// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"os"
	"syscall"
)

func jjTidySingleLink(st os.FileInfo) bool {
	if st == nil || !st.Mode().IsRegular() {
		return false
	}
	raw, ok := st.Sys().(*syscall.Stat_t)
	return ok && raw.Nlink == 1
}
