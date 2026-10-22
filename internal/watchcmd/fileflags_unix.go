//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import "syscall"

// A config file is opened without blocking on a FIFO and, for the implicit file, without following a symlink.
const (
	openNonBlock = syscall.O_NONBLOCK
	openNoFollow = syscall.O_NOFOLLOW
)
