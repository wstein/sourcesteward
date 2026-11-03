// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSelectNativeGit(t *testing.T) {
	fake := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	apple, native, other := fake(t), fake(t), fake(t)
	isNative := func(p string) bool { return filepath.Dir(p) != apple }
	cases := []struct {
		name      string
		goos      string
		path      []string
		fallbacks []string
		dir       string
		ok        bool
	}{
		{"non-darwin keeps PATH", "linux", []string{apple}, nil, "", true},
		{"native first on PATH", "darwin", []string{native, apple}, nil, "", true},
		{"apple first, native later on PATH", "darwin", []string{apple, native}, nil, native, true},
		{"apple only, native fallback", "darwin", []string{apple}, []string{filepath.Join(other, "missing"), native}, native, true},
		{"apple only", "darwin", []string{apple}, []string{filepath.Join(other, "missing")}, "", false},
		{"no git on PATH is left to the caller", "darwin", nil, []string{native}, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fallbacks := make([]string, len(c.fallbacks))
			for i, f := range c.fallbacks {
				fallbacks[i] = f
				if filepath.Base(f) != "git" {
					fallbacks[i] = filepath.Join(f, "git")
				}
			}
			dir, ok := selectNativeGit(c.goos, c.path, fallbacks, isNative)
			if dir != c.dir || ok != c.ok {
				t.Fatalf("selectNativeGit = %q, %v; want %q, %v", dir, ok, c.dir, c.ok)
			}
		})
	}
}

func TestRequireNativeGitLeavesAUsableGitAlone(t *testing.T) {
	if runtime.GOOS != "darwin" {
		before := os.Getenv("PATH")
		requireNativeGit(t)
		if os.Getenv("PATH") != before {
			t.Fatal("requireNativeGit changed PATH where no native Git selection is needed")
		}
		return
	}
	requireNativeGit(t)
	p, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(p); err != nil || resolved == "/usr/bin/git" {
		t.Fatalf("git on PATH after requireNativeGit is %q (%v)", resolved, err)
	}
}

func TestNativeGitMissingIsFatal(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"neither set", nil, false},
		{"sandbox empty", map[string]string{"STW_REQUIRE_NATIVE_SANDBOX": ""}, false},
		{"jj empty", map[string]string{"STW_REQUIRE_JJ": ""}, false},
		{"sandbox 0", map[string]string{"STW_REQUIRE_NATIVE_SANDBOX": "0"}, false},
		{"sandbox 1", map[string]string{"STW_REQUIRE_NATIVE_SANDBOX": "1"}, true},
		{"jj 1", map[string]string{"STW_REQUIRE_JJ": "1"}, true},
		{"both 1", map[string]string{"STW_REQUIRE_NATIVE_SANDBOX": "1", "STW_REQUIRE_JJ": "1"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nativeGitMissingIsFatal(func(k string) string { return c.env[k] }); got != c.want {
				t.Fatalf("nativeGitMissingIsFatal = %v, want %v", got, c.want)
			}
		})
	}
}
