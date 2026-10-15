//go:build darwin || linux

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"syscall"
)

func gitTidyMutableFile(st os.FileInfo) bool {
	raw, ok := st.Sys().(*syscall.Stat_t)
	return ok && st.Mode().IsRegular() && raw.Nlink == 1 && privateOwner(st) && st.Size() <= recordLimit
}
