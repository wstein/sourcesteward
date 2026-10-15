// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"errors"
	"os"
	"syscall"
)

// os.Root prevents parent-component substitution from escaping the workspace.
// Nonblocking/no-follow open rejects FIFOs and final-component symlinks.
func openGitInventoryDisk(root *os.Root, path string) (*os.File, error) {
	before, e := root.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("tracked disk content is not a regular file")
	}
	f, e := root.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return nil, e
	}
	after, e := f.Stat()
	if e != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		f.Close()
		return nil, errors.New("tracked file changed while opening")
	}
	return f, nil
}
