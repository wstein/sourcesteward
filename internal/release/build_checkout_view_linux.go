// SPDX-License-Identifier: EUPL-1.2
//go:build linux

package release

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ctime is observed only; source access metadata is never restored or written.
func buildViewChangeStamp(info os.FileInfo) time.Time {
	s := info.Sys().(*syscall.Stat_t)
	return time.Unix(s.Ctim.Sec, s.Ctim.Nsec)
}

func observeBuildViewGroup(leader int) (buildViewGroup, error) {
	var result buildViewGroup
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || !entry.IsDir() {
			continue
		}
		groupID, e := syscall.Getpgid(pid)
		if errors.Is(e, syscall.ESRCH) && pid != leader {
			continue
		}
		if e != nil {
			return result, e
		}
		if groupID != leader {
			continue
		}
		raw, e := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if (os.IsNotExist(e) || errors.Is(e, syscall.ESRCH)) && pid != leader {
			continue
		}
		if e != nil {
			return result, e
		}
		end := strings.LastIndexByte(string(raw), ')')
		if end < 0 {
			return result, errors.New("invalid native process metadata")
		}
		fields := strings.Fields(string(raw[end+1:]))
		if len(fields) < 3 {
			return result, errors.New("incomplete native process metadata")
		}
		group, e := strconv.Atoi(fields[2])
		if e != nil {
			return result, e
		}
		if group != leader {
			return result, errors.New("owned native process group changed during observation")
		}
		result.members++
		if pid == leader {
			result.leaderTerminal = fields[0] == "Z"
		}
	}
	if result.members == 0 {
		return result, errors.New("owned native group identity missing")
	}
	return result, nil
}
