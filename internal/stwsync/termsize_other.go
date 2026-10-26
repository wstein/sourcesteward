// SPDX-License-Identifier: EUPL-1.2

//go:build !unix

package stwsync

import "io"

// TerminalSize returns 0, 0 where the size of a terminal cannot be read; the caller picks a default.
func TerminalSize(io.Writer) (cols, rows int) { return 0, 0 }
