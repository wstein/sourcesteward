// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

type fakeNativeGitT struct {
	fatal, skip string
	setenv      map[string]string
}

func (f *fakeNativeGitT) Helper()                  {}
func (f *fakeNativeGitT) Fatal(args ...any)        { f.fatal = fmt.Sprint(args...) }
func (f *fakeNativeGitT) Skip(args ...any)         { f.skip = fmt.Sprint(args...) }
func (f *fakeNativeGitT) Setenv(key, value string) { f.setenv = map[string]string{key: value} }

// requireNativeGit itself, with its dependencies replaced: a missing native Git skips with
// "native Git required", fails when a CI variable is 1, and prepends a found directory.
func TestRequireNativeGitDecisions(t *testing.T) {
	gitDir := func() string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	apple, native := gitDir(), gitDir()
	oldGOOS, oldGetenv, oldNative, oldFallbacks := nativeGitGOOS, nativeGitGetenv, nativeGitIsNative, nativeGitFallbacks
	t.Cleanup(func() {
		nativeGitGOOS, nativeGitGetenv, nativeGitIsNative, nativeGitFallbacks = oldGOOS, oldGetenv, oldNative, oldFallbacks
	})
	nativeGitGOOS, nativeGitFallbacks = "darwin", nil
	nativeGitIsNative = func(p string) bool { return filepath.Dir(p) != apple }
	cases := []struct {
		name    string
		env     map[string]string
		wantErr bool
		wantMsg string
		wantDir string
	}{
		{"missing, unset skips", map[string]string{"PATH": apple}, false, "native Git required", ""},
		{"missing, sandbox 1 fails", map[string]string{"PATH": apple, "STW_REQUIRE_NATIVE_SANDBOX": "1"}, true, "native Git required", ""},
		{"missing, jj 1 fails", map[string]string{"PATH": apple, "STW_REQUIRE_JJ": "1"}, true, "native Git required", ""},
		{"missing, 0 skips", map[string]string{"PATH": apple, "STW_REQUIRE_NATIVE_SANDBOX": "0", "STW_REQUIRE_JJ": ""}, false, "native Git required", ""},
		{"native later on PATH is prepended", map[string]string{"PATH": apple + ":" + native, "STW_REQUIRE_JJ": "1"}, false, "", native},
		{"native first changes nothing", map[string]string{"PATH": native + ":" + apple}, false, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nativeGitGetenv = func(k string) string { return c.env[k] }
			f := &fakeNativeGitT{}
			requireNativeGit(f)
			gotMsg, other := f.skip, f.fatal
			if c.wantErr {
				gotMsg, other = f.fatal, f.skip
			}
			if other != "" || !strings.Contains(gotMsg, c.wantMsg) || (c.wantMsg == "" && gotMsg != "") {
				t.Fatalf("fatal=%q skip=%q, want message %q (fatal=%v)", f.fatal, f.skip, c.wantMsg, c.wantErr)
			}
			if c.wantDir == "" && f.setenv != nil {
				t.Fatalf("unexpected PATH change %v", f.setenv)
			}
			if c.wantDir != "" && f.setenv["PATH"] != c.wantDir+string(os.PathListSeparator)+c.env["PATH"] {
				t.Fatalf("PATH change %v, want %q first", f.setenv, c.wantDir)
			}
		})
	}
}
