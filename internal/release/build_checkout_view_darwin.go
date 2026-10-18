// SPDX-License-Identifier: EUPL-1.2
//go:build darwin

package release

import (
	"os"
	"syscall"
	"time"
)

// ctime is observed only; source access metadata is never restored or written.
func buildViewChangeStamp(info os.FileInfo) time.Time {
	s := info.Sys().(*syscall.Stat_t)
	return time.Unix(s.Ctimespec.Sec, s.Ctimespec.Nsec)
}
