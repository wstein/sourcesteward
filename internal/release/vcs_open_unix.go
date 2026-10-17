// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package release

import (
	"errors"
	"os"
	"syscall"
)

// openRegular opens a file without following a symlink and without blocking on a named pipe.
// A symlink is reported as errNotStable; the caller checks the opened file is regular.
func openRegular(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, errNotStable
	}
	return f, err
}
