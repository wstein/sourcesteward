//go:build darwin || linux

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func gitMetadata(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 65536 {
		return nil, failure("git-metadata-refused", "native Git registration metadata must be a bounded regular file")
	}
	if raw, ok := st.Sys().(*syscall.Stat_t); !ok || raw.Nlink != 1 {
		return nil, failure("git-metadata-refused", "native Git registration metadata must have a single filesystem link")
	}
	b, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(b) > 65536 {
		return nil, failure("git-metadata-refused", "native Git registration metadata exceeds the read bound")
	}
	return b, nil
}

// gitLayoutFile pins its canonical parent and records the identity of the open
// no-follow file, binding literal bytes and physical identity in one observation.
func gitLayoutFile(path string) ([]byte, PathIdentity, os.FileMode, error) {
	root, _, err := openCanonical(filepath.Dir(path))
	if err != nil {
		return nil, PathIdentity{}, 0, err
	}
	defer root.Close()
	f, err := root.OpenFile(filepath.Base(path), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, PathIdentity{}, 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > 65536 || !privateOwner(st) {
		return nil, PathIdentity{}, 0, failure("git-layout-refused", "Git layout metadata is not a bounded owned regular file")
	}
	if raw, ok := st.Sys().(*syscall.Stat_t); !ok || raw.Nlink != 1 {
		return nil, PathIdentity{}, 0, failure("git-layout-refused", "Git layout metadata is hardlinked")
	}
	data, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(data) > 65536 {
		return nil, PathIdentity{}, 0, failure("git-layout-refused", "Git layout metadata exceeds the read bound")
	}
	id, err := pathIdentity(path, st)
	return data, id, st.Mode(), err
}
