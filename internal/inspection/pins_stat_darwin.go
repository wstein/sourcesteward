// SPDX-License-Identifier: EUPL-1.2
//go:build darwin

package inspection

import "syscall"

func pinsStatTimes(st *syscall.Stat_t) (cs, cn, ms, mn int64) {
	return int64(st.Ctimespec.Sec), int64(st.Ctimespec.Nsec), int64(st.Mtimespec.Sec), int64(st.Mtimespec.Nsec)
}
