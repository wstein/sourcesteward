// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
)

func pruneMountIdentity(fd int) (string, error) {
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		return "", err
	}
	mount := unix.ByteSliceToString(st.Mntonname[:])
	if mount == "" {
		return "", errors.New("mount identity unavailable")
	}
	return fmt.Sprintf("%x:%d:%s", st.Fsid.Val, st.Type, mount), nil
}
