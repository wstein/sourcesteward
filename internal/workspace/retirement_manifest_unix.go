//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
	"reflect"
	"syscall"
)

// physicalOwnerUID is the user id the physical identities of a retirement must carry; a variable
// only so a test can stand for another user.
var physicalOwnerUID = func() uint32 { return uint32(os.Geteuid()) }

func retirementSingleLink(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Nlink == 1
}

// Linux and Darwin expose native nanosecond ctime under different field names.
// Refuse unsupported Stat_t layouts rather than synthesizing timestamp facts.
func retirementV3Physical(path string, st os.FileInfo) (V3PhysicalIdentity, error) {
	s, ok := st.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != physicalOwnerUID() {
		return V3PhysicalIdentity{}, errors.New("v3 physical owner capability unavailable or foreign")
	}
	kind := "regular"
	if st.IsDir() {
		kind = "directory"
	} else if !st.Mode().IsRegular() || s.Nlink != 1 {
		return V3PhysicalIdentity{}, errors.New("v3 requires single-link regular files or directories")
	}
	raw := reflect.ValueOf(s).Elem()
	ct := raw.FieldByName("Ctim")
	if !ct.IsValid() {
		ct = raw.FieldByName("Ctimespec")
	}
	if !ct.IsValid() {
		return V3PhysicalIdentity{}, errors.New("native ctime capability unavailable")
	}
	sec, ns := ct.FieldByName("Sec"), ct.FieldByName("Nsec")
	if !sec.IsValid() || !ns.IsValid() {
		return V3PhysicalIdentity{}, errors.New("native ctime precision unavailable")
	}
	return V3PhysicalIdentity{Path: path, Device: uint64(s.Dev), Inode: uint64(s.Ino), UID: uint64(s.Uid), OwnerKnown: true, Kind: kind, Mode: uint32(s.Mode) & 07777, Nlink: uint64(s.Nlink), Size: st.Size(), MtimeNS: st.ModTime().UnixNano(), CtimeNS: sec.Int()*1e9 + ns.Int(), TimestampPrecision: "nanosecond"}, nil
}
