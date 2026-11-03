// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// appleLauncher is the one path production refuses (workspace.gitExecutable): the
// Apple launcher cannot run inside the required executable sandbox.
const appleLauncher = "/usr/bin/git"

// nativeGitFallbacks are the usual places of a native Git on macOS.
var nativeGitFallbacks = []string{
	"/opt/homebrew/bin/git",
	"/usr/local/bin/git",
	"/Applications/Xcode.app/Contents/Developer/usr/bin/git",
	"/Library/Developer/CommandLineTools/usr/bin/git",
}

func isNativeGit(path string) bool {
	resolved, err := filepath.EvalSymlinks(path)
	return err == nil && resolved != appleLauncher
}

func regularExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// selectNativeGit decides which directory must lead PATH so that "git" resolves to a
// native executable. It returns "" and true when nothing has to change (not macOS,
// or the first Git on PATH is native), a directory and true when a later PATH entry
// or a fallback is native, "" and true when PATH holds no Git at all (the caller reports
// a missing Git as before), and "" and false when only the Apple launcher exists.
func selectNativeGit(goos string, path, fallbacks []string, native func(string) bool) (string, bool) {
	if goos != "darwin" {
		return "", true
	}
	first, found := true, false
	for _, dir := range path {
		candidate := filepath.Join(dir, "git")
		if !regularExecutable(candidate) {
			continue
		}
		found = true
		if native(candidate) {
			if first {
				return "", true
			}
			return dir, true
		}
		first = false
	}
	if !found {
		// No Git on PATH at all: nothing to substitute; the caller reports a missing Git.
		return "", true
	}
	for _, candidate := range fallbacks {
		if regularExecutable(candidate) && native(candidate) {
			return filepath.Dir(candidate), true
		}
	}
	return "", false
}

// requireNativeGit makes "git" resolve to a native executable for the test, or skips
// it with the reason "native Git required" on macOS when only the Apple launcher
// exists. STW_REQUIRE_NATIVE_SANDBOX=1 or STW_REQUIRE_JJ=1 turn the skip into a failure.
func requireNativeGit(t *testing.T) {
	t.Helper()
	dir, ok := selectNativeGit(runtime.GOOS, filepath.SplitList(os.Getenv("PATH")), nativeGitFallbacks, isNativeGit)
	if !ok {
		msg := "native Git required: only the Apple /usr/bin/git launcher was found; install one (for example `brew install git`) and put it first on PATH"
		if os.Getenv("STW_REQUIRE_NATIVE_SANDBOX") == "1" || os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	if dir != "" {
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
}
