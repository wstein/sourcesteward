//go:build darwin || linux

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"io"
	"os"
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
