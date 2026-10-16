// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"io/fs"
	"strings"
	"testing"
)

// cleanPair is an index entry and the stat of the file it describes with nothing different, in an index
// written one second after the file.
func cleanPair() (pinsIndexEntry, *pinsStatData, pinsCompareConfig) {
	e := pinsIndexEntry{Mode: "100644", OID: oidA, Path: "a", CtimeSec: 100, CtimeNsec: 1, MtimeSec: 100, MtimeNsec: 2, Dev: 7, Ino: 9, UID: 1000, GID: 1000, Size: 5}
	st := &pinsStatData{Mode: 0o644, Size: 5, CtimeSec: 100, CtimeNsec: 1, MtimeSec: 100, MtimeNsec: 2, Dev: 7, Ino: 9, UID: 1000, GID: 1000}
	return e, st, pinsCompareConfig{FileMode: true, Symlinks: true, IndexMtimeSec: 101, EmptyBlob: sha1Empty}
}

func TestPinsCompareEntryDecidesFromMetadataOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*pinsIndexEntry, **pinsStatData, *pinsCompareConfig)
		verdict string
		reason  string
	}{
		{"identical", func(*pinsIndexEntry, **pinsStatData, *pinsCompareConfig) {}, "absent", ""},
		{"missing file", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { *s = nil }, "present", "missing"},
		{"different size", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).Size = 6 }, "present", "size"},
		{"size modulo 2^32", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).Size = 5 + 1<<32 }, "absent", ""},
		{"executable bit gained", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).Mode = 0o755 }, "present", "mode"},
		{"executable bit ignored without core.fileMode", func(_ *pinsIndexEntry, s **pinsStatData, c *pinsCompareConfig) {
			(*s).Mode, c.FileMode = 0o755, false
		}, "absent", ""},
		{"executable index entry lost its bit", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Mode = "100755" }, "present", "mode"},
		{"regular file where a symlink is recorded", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Mode = "120000" }, "present", "mode"},
		{"symlink stored as a file without core.symlinks", func(e *pinsIndexEntry, _ **pinsStatData, c *pinsCompareConfig) { e.Mode, c.Symlinks = "120000", false }, "absent", ""},
		{"symlink where a file is recorded", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).Mode = fs.ModeSymlink | 0o777 }, "present", "mode"},
		{"directory where a file is recorded", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).Mode = fs.ModeDir | 0o755 }, "present", "mode"},
		{"modification time only", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).MtimeSec = 50 }, "unknown", "modification time"},
		{"modification time nanoseconds only", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).MtimeNsec = 3 }, "unknown", "modification time"},
		{"change time only", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).CtimeNsec = 9 }, "unknown", "change time"},
		{"inode only", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).Ino = 10 }, "unknown", "inode"},
		{"device only", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).Dev = 8 }, "unknown", "device"},
		{"owner only", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).UID = 0 }, "unknown", "owner"},
		{"inode is compared modulo 2^32", func(_ *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { (*s).Ino = 9 + 1<<32 }, "absent", ""},
		{"racily clean (same instant as the index)", func(_ *pinsIndexEntry, _ **pinsStatData, c *pinsCompareConfig) {
			c.IndexMtimeSec, c.IndexMtimeNsec = 100, 2
		}, "unknown", "racily"},
		{"racily clean (newer than the index)", func(_ *pinsIndexEntry, _ **pinsStatData, c *pinsCompareConfig) { c.IndexMtimeSec = 99 }, "unknown", "racily"},
		{"racy by nanoseconds", func(_ *pinsIndexEntry, _ **pinsStatData, c *pinsCompareConfig) {
			c.IndexMtimeSec, c.IndexMtimeNsec = 100, 1
		}, "unknown", "racily"},
		{"older than the index by nanoseconds", func(_ *pinsIndexEntry, _ **pinsStatData, c *pinsCompareConfig) {
			c.IndexMtimeSec, c.IndexMtimeNsec = 100, 3
		}, "absent", ""},
		{"a racy entry whose size differs is still dirty", func(_ *pinsIndexEntry, s **pinsStatData, c *pinsCompareConfig) { (*s).Size, c.IndexMtimeSec = 9, 99 }, "present", "size"},
		{"skip-worktree", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Flags = 0x40000000 }, "unknown", "skip-worktree"},
		{"skip-worktree and missing", func(e *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) { e.Flags, *s = 0x40000000, nil }, "unknown", "skip-worktree"},
		{"assume-unchanged", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Flags = 0x8000 }, "unknown", "assume-unchanged"},
		{"intent-to-add", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Flags = 0x20000000 }, "unknown", "intent-to-add"},
		{"conflict stage", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Stage = 2 }, "unknown", "stage"},
		{"gitlink", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Mode = "160000" }, "unknown", "gitlink"},
		{"smudged size zero", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Size = 0 }, "unknown", "smudged"},
		{"genuinely empty file", func(e *pinsIndexEntry, s **pinsStatData, _ *pinsCompareConfig) {
			e.Size, e.OID, (*s).Size = 0, sha1Empty, 0
		}, "absent", ""},
		{"empty blob but the file grew", func(e *pinsIndexEntry, _ **pinsStatData, _ *pinsCompareConfig) { e.Size, e.OID = 0, sha1Empty }, "present", "size"},
		{"sha256 empty blob", func(e *pinsIndexEntry, s **pinsStatData, c *pinsCompareConfig) {
			e.Size, (*s).Size = 0, 0
			e.OID, c.EmptyBlob = "473a0f4c3be8a93681a267e3b1e9a7dcda1185436fe141f7749120a303721813", "473a0f4c3be8a93681a267e3b1e9a7dcda1185436fe141f7749120a303721813"
		}, "absent", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, st, c := cleanPair()
			tc.mutate(&e, &st, &c)
			verdict, reason := pinsCompareEntry(e, st, c)
			if verdict != tc.verdict || !strings.Contains(reason, tc.reason) || (tc.verdict == "absent") != (reason == "") {
				t.Fatalf("got %s %q, want %s containing %q", verdict, reason, tc.verdict, tc.reason)
			}
		})
	}
}
