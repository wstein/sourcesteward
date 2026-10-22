// SPDX-License-Identifier: EUPL-1.2
package stwsync

import (
	"io"
	"os"
)

// IsTerminal reports whether w is a terminal (a character device).
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// ColorEnabled reports whether w is a terminal and NO_COLOR is unset or empty.
func ColorEnabled(w io.Writer) bool {
	return os.Getenv("NO_COLOR") == "" && IsTerminal(w)
}
