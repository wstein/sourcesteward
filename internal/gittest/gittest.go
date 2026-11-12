// SPDX-License-Identifier: EUPL-1.2

// Package gittest holds helpers for tests that depend on the version of the installed Git.
package gittest

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/inspection"
)

// AtLeast reports whether the Git on PATH is at least major.minor, from `git version`. A version that
// cannot be read fails the test; it never counts as old.
func AtLeast(t testing.TB, major, minor int) bool {
	t.Helper()
	out, err := exec.Command("git", "version").Output()
	if err != nil {
		t.Fatalf("git version: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		t.Fatalf("unexpected git version output %q", out)
	}
	ma, mi, err := inspection.ParseVersion(fields[2])
	if err != nil {
		t.Fatalf("git version %q: %v", out, err)
	}
	return ma > major || (ma == major && mi >= minor)
}

// RequireAtLeast skips the test unless the Git on PATH is at least major.minor.
func RequireAtLeast(t testing.TB, major, minor int, why string) {
	t.Helper()
	if !AtLeast(t, major, minor) {
		t.Skip("needs Git " + strconv.Itoa(major) + "." + strconv.Itoa(minor) + " (" + why + ")")
	}
}

// RequireBelow skips the test when the Git on PATH is at least major.minor; it is the counterpart of
// RequireAtLeast for tests of the behavior of older Git.
func RequireBelow(t testing.TB, major, minor int, why string) {
	t.Helper()
	if AtLeast(t, major, minor) {
		t.Skip("needs Git older than " + strconv.Itoa(major) + "." + strconv.Itoa(minor) + " (" + why + ")")
	}
}
