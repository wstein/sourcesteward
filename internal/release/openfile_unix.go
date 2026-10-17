// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package release

import (
	"os"
	"syscall"
)

// openRegular opens a file without following a final symlink and without blocking on a named
// pipe. This is the only use of syscall in the file readers: the O_ flags below (the
// architecture check refuses anything else from syscall in this file).
func openRegular(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		if info, lerr := os.Lstat(path); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, errNotStable
		}
		return nil, err
	}
	return f, nil
}
