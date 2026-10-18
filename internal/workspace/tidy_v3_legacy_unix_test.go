//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestTidyV3LegacyPhysicalOwnerCapabilityRefusesForeignUID(t *testing.T) {
	p := gitTempDir(t)
	st, e := os.Stat(p)
	if e != nil {
		t.Fatal(e)
	}
	native, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("native ownership capability unavailable")
	}
	// Alter only this captured stat fixture; no real ownership is changed.
	native.Uid++
	if _, e = retirementV3Physical(p, st); e == nil {
		t.Fatal("foreign directory owner admitted")
	}
}

func TestTidyV3LegacyRegularSiblingForeignOwnerRefused(t *testing.T) {
	p := filepath.Join(gitTempDir(t), "sibling")
	if e := os.WriteFile(p, []byte("actor"), 0600); e != nil {
		t.Fatal(e)
	}
	st, e := os.Lstat(p)
	if e != nil {
		t.Fatal(e)
	}
	native, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("native ownership capability unavailable")
	}
	native.Uid++
	if _, e = retirementV3Physical(p, st); e == nil {
		t.Fatal("foreign regular sibling owner admitted")
	}
}
