// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"golang.org/x/sys/unix"
	"strconv"
)

func pruneMountIdentity(fd int) (string, error) {
	var st unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW|unix.AT_NO_AUTOMOUNT, unix.STATX_MNT_ID, &st); err != nil {
		return "", err
	}
	if st.Mask&unix.STATX_MNT_ID == 0 || st.Mnt_id == 0 {
		return "", errors.New("mount ID unavailable")
	}
	return strconv.FormatUint(st.Mnt_id, 10), nil
}
