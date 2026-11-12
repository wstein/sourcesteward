// SPDX-License-Identifier: EUPL-1.2
package gittest

import (
	"os"
	"path/filepath"
	"testing"
)

type recorder struct {
	testing.TB
	fatal   bool
	skipped bool
}

type stop struct{}

func (r *recorder) Helper()               {}
func (r *recorder) Fatalf(string, ...any) { r.fatal = true; panic(stop{}) }
func (r *recorder) Skip(...any)           { r.skipped = true; panic(stop{}) }

func call(f func(*recorder)) (r *recorder) {
	r = &recorder{}
	defer func() {
		if v := recover(); v != nil {
			if _, ok := v.(stop); !ok {
				panic(v)
			}
		}
	}()
	f(r)
	return r
}

func fakeGit(t *testing.T, output string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\necho '" + output + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestVersionGates(t *testing.T) {
	fakeGit(t, "git version 2.43.0")
	if r := call(func(r *recorder) { RequireAtLeast(r, 2, 45, "x") }); !r.skipped {
		t.Error("2.43.0 did not skip a test that needs 2.45")
	}
	if r := call(func(r *recorder) { RequireAtLeast(r, 2, 43, "x") }); r.skipped || r.fatal {
		t.Error("2.43.0 skipped a test that needs 2.43")
	}
	if r := call(func(r *recorder) { RequireBelow(r, 2, 45, "x") }); r.skipped || r.fatal {
		t.Error("2.43.0 skipped a test for Git before 2.45")
	}
	if r := call(func(r *recorder) { RequireBelow(r, 2, 43, "x") }); !r.skipped {
		t.Error("2.43.0 did not skip a test for Git before 2.43")
	}
}

// A version that cannot be read fails the test; it never counts as an old Git that skips it.
func TestUnreadableVersionFailsInsteadOfSkipping(t *testing.T) {
	for _, out := range []string{"git version", "git version x.y", "git version +2.50.0", "nonsense"} {
		fakeGit(t, out)
		for name, f := range map[string]func(*recorder){
			"RequireAtLeast": func(r *recorder) { RequireAtLeast(r, 2, 45, "x") },
			"RequireBelow":   func(r *recorder) { RequireBelow(r, 2, 45, "x") },
		} {
			if r := call(f); !r.fatal || r.skipped {
				t.Errorf("%s with %q: fatal=%v skipped=%v", name, out, r.fatal, r.skipped)
			}
		}
	}
}
