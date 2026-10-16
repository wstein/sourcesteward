// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"io/fs"
	"syscall"
)

// pinsStatOf extracts the stat fields an index entry records from an lstat result.
func pinsStatOf(info fs.FileInfo) (pinsStatData, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return pinsStatData{}, false
	}
	cs, cn, ms, mn := pinsStatTimes(st)
	return pinsStatData{
		Mode: info.Mode(), Size: info.Size(),
		CtimeSec: cs, CtimeNsec: cn, MtimeSec: ms, MtimeNsec: mn,
		Dev: uint64(st.Dev), Ino: uint64(st.Ino), UID: uint64(st.Uid), GID: uint64(st.Gid),
	}, true
}
