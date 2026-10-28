//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2

package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func canonicalTemp(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// The physical owner is the effective user: the same directory is refused when the owner function
// stands for another user (an injected owner function, no real chown).
func TestRetirementLocationAdmissionRequiresTheOwnerOfTheDirectories(t *testing.T) {
	parent := canonicalTemp(t)
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(parent, "wt")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	if err := RetirementLocationAdmission(dest); err != nil {
		t.Fatalf("a directory of the user in a directory of the user: %v", err)
	}
	old := physicalOwnerUID
	t.Cleanup(func() { physicalOwnerUID = old })
	physicalOwnerUID = func() uint32 { return uint32(os.Geteuid()) + 1 }
	if err := RetirementLocationAdmission(dest); err == nil || !strings.Contains(err.Error(), "foreign") {
		t.Errorf("another owner must be refused, got %v", err)
	}
	if err := RetirementParentAdmission(parent); err == nil {
		t.Error("the parent of another owner must be refused")
	}
	if err := RetirementDestinationAdmission(dest); err == nil {
		t.Error("the destination of another owner must be refused")
	}
}

// A real directory of another owner (root): /tmp is one where the tests run as an ordinary user.
func TestRetirementParentAdmissionRefusesTheSharedTemporaryDirectory(t *testing.T) {
	var st syscall.Stat_t
	if err := syscall.Stat("/tmp", &st); err != nil || int(st.Uid) == os.Geteuid() {
		t.Skip("/tmp belongs to the current user here")
	}
	if err := RetirementParentAdmission("/tmp"); err == nil {
		t.Error("/tmp is owned by another user and must be refused as the parent of a worktree")
	}
}

func TestRetirementParentAdmissionRefusesASymbolicLinkSibling(t *testing.T) {
	parent := canonicalTemp(t)
	if err := os.Symlink("/nonexistent", filepath.Join(parent, "link")); err != nil {
		t.Fatal(err)
	}
	if err := RetirementParentAdmission(parent); err == nil {
		t.Error("a symbolic link next to the worktree must be refused (retirement-sibling-refused)")
	}
}
