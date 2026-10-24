//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package derive

import (
	"os"
	"syscall"
)

func ownedByCurrentUser(st os.FileInfo) bool {
	v, ok := st.Sys().(*syscall.Stat_t)
	return ok && v.Uid == uint32(os.Geteuid())
}
