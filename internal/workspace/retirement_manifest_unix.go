//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"syscall"
)

func retirementSingleLink(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Nlink == 1
}
