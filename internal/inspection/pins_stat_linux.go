// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package inspection

import "syscall"

func pinsStatTimes(st *syscall.Stat_t) (cs, cn, ms, mn int64) {
	return int64(st.Ctim.Sec), int64(st.Ctim.Nsec), int64(st.Mtim.Sec), int64(st.Mtim.Nsec)
}
