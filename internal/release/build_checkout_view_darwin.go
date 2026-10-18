// SPDX-License-Identifier: EUPL-1.2
//go:build darwin

package release

import (
	"encoding/binary"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"reflect"
	"syscall"
	"time"
)

// ctime is observed only; source access metadata is never restored or written.
func buildViewChangeStamp(info os.FileInfo) time.Time {
	s := info.Sys().(*syscall.Stat_t)
	return time.Unix(s.Ctimespec.Sec, s.Ctimespec.Nsec)
}

// Decode only the three required fields using generated ABI field offsets.
// Pointer-bearing KinfoProc cannot be binary.Read directly; no unsafe overlay or
// hidden ENOMEM-retry helper is used. Compile success is not Darwin acceptance.
func observeBuildViewGroup(leader int) (buildViewGroup, error) {
	var result buildViewGroup
	raw, err := unix.SysctlRaw("kern.proc.pgrp", leader)
	if err != nil {
		return result, err
	}
	t := reflect.TypeOf(unix.KinfoProc{})
	if t.Size() != uintptr(unix.SizeofKinfoProc) || len(raw)%unix.SizeofKinfoProc != 0 {
		return result, errors.New("invalid native group record size")
	}
	field := func(parent, child string, size uintptr) (uintptr, error) {
		p, ok := t.FieldByName(parent)
		if !ok {
			return 0, errors.New("missing native group layout")
		}
		c, ok := p.Type.FieldByName(child)
		if !ok || c.Type.Size() != size {
			return 0, errors.New("invalid native group field")
		}
		return p.Offset + c.Offset, nil
	}
	pidOffset, err := field("Proc", "P_pid", 4)
	if err != nil {
		return result, err
	}
	statOffset, err := field("Proc", "P_stat", 1)
	if err != nil {
		return result, err
	}
	groupOffset, err := field("Eproc", "Pgid", 4)
	if err != nil {
		return result, err
	}
	for offset := 0; offset < len(raw); offset += unix.SizeofKinfoProc {
		record := raw[offset : offset+unix.SizeofKinfoProc]
		pid := int(int32(binary.NativeEndian.Uint32(record[pidOffset : pidOffset+4])))
		group := int(int32(binary.NativeEndian.Uint32(record[groupOffset : groupOffset+4])))
		if pid <= 0 || group != leader {
			return result, errors.New("native group identity mismatch")
		}
		result.members++
		// XNU bsd/sys/proc.h: SZOMB=5, awaiting collection by parent.
		if pid == leader {
			result.leaderTerminal = record[statOffset] == 5
		}
	}
	if result.members == 0 {
		return result, errors.New("owned native group identity missing")
	}
	return result, nil
}
