// SPDX-License-Identifier: EUPL-1.2

// Package gittest holds helpers for tests that depend on the version of the installed Git.
package gittest

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/gitversion"
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
	ma, mi, err := gitversion.Parse(fields[2])
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

// LimitBelow is for TestMain of a package whose code needs Git major.minor or later. On an older Git
// it restricts the run to the tests that match run (the tests of the older behavior) and skips those
// that match skip; empty patterns do not restrict. An explicit -test.run or -test.skip of the caller
// wins. It prints one line to standard error when it limits the run.
func LimitBelow(major, minor int, run, skip string) {
	out, err := exec.Command("git", "version").Output()
	fields := strings.Fields(string(out))
	if err != nil || len(fields) < 3 {
		return // the tests report the missing or unreadable Git themselves
	}
	ma, mi, err := gitversion.Parse(fields[2])
	if err != nil || ma > major || (ma == major && mi >= minor) {
		return
	}
	flag.Parse()
	limited := false
	for name, value := range map[string]string{"test.run": run, "test.skip": skip} {
		if f := flag.Lookup(name); f != nil && value != "" && f.Value.String() == "" {
			if flag.Set(name, value) == nil {
				limited = true
			}
		}
	}
	if limited {
		fmt.Fprintf(os.Stderr, "Git %s is older than %d.%d: the run is limited to the tests of the older behavior (needs Git %d.%d)\n", fields[2], major, minor, major, minor)
	}
}
