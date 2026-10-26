// SPDX-License-Identifier: EUPL-1.2

//go:build unix

package stwsync

import (
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// TerminalSize returns the columns and rows of the terminal w writes to, or 0, 0 when w is not a
// terminal or the size cannot be read. The caller picks a default.
func TerminalSize(w io.Writer) (cols, rows int) {
	f, ok := w.(*os.File)
	if !ok {
		return 0, 0
	}
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0
	}
	return int(ws.Col), int(ws.Row)
}
