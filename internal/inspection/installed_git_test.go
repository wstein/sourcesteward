// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The tests of this package cannot use internal/gittest (it imports this package), so the two helpers
// that depend on the installed Git version are here.

// installedGitAtLeast reports whether the Git on PATH is at least major.minor; an unreadable version
// fails the test.
func installedGitAtLeast(t *testing.T, major, minor int) bool {
	t.Helper()
	out, err := exec.Command("git", "version").Output()
	fields := strings.Fields(string(out))
	if err != nil || len(fields) < 3 {
		t.Fatalf("git version: %q %v", out, err)
	}
	ma, mi, err := ParseVersion(fields[2])
	if err != nil {
		t.Fatalf("git version %q: %v", out, err)
	}
	return ma > major || (ma == major && mi >= minor)
}

// limitRunBelow246 restricts the run to the tests that match run when the Git on PATH is older than
// 2.46 (the floor of every inspection except Inspect); an explicit -test.run wins.
func limitRunBelow246(run string) {
	out, err := exec.Command("git", "version").Output()
	fields := strings.Fields(string(out))
	if err != nil || len(fields) < 3 || VersionAtLeast(fields[2], 2, 46) {
		return
	}
	if _, _, err := ParseVersion(fields[2]); err != nil {
		return
	}
	if !flag.Parsed() {
		flag.Parse()
	}
	if f := flag.Lookup("test.run"); f != nil && f.Value.String() == "" && flag.Set("test.run", run) == nil {
		fmt.Fprintf(os.Stderr, "Git %s is older than 2.46: the run is limited to the tests of the older behavior\n", fields[2])
	}
}

func TestLimitRunBelow246(t *testing.T) {
	f := flag.Lookup("test.run")
	old := f.Value.String()
	t.Cleanup(func() { f.Value.Set(old) })
	for _, tc := range []struct {
		out     string
		limited bool
	}{
		{"git version 2.45.9", true}, {"git version 2.43.0", true},
		{"git version 2.46.0", false}, {"git version 2.47.3", false}, {"git version 3.0.0", false},
		{"git version x.y", false}, {"git version", false}, {"nonsense", false},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(dir+"/git", []byte("#!/bin/sh\necho '"+tc.out+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", dir)
		f.Value.Set("")
		limitRunBelow246("^RUN$")
		if got := f.Value.String() == "^RUN$"; got != tc.limited {
			t.Errorf("%q: test.run=%q, want limited=%v", tc.out, f.Value.String(), tc.limited)
		}
	}
}
