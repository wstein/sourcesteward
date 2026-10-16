// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// pinsIsolateHome keeps the user's own Git configuration out of the pins tests: the runner strips every
// GIT_* variable, so the global and XDG files it reads come from HOME.
func pinsIsolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
}

// pinsRealDir returns a symlink-free temporary directory, so path comparisons are exact on every host.
func pinsRealDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// pinsFixture is a superproject with one submodule, `member`, cloned from a local repository. The local
// clone uses protocol.file.allow only inside the fixture setup; the code under test never contacts it.
type pinsFixture struct {
	Root  string // temporary directory holding everything
	Sub   string // the submodule's "remote"
	Super string // the superproject worktree
	Pin   string // the commit the superproject pins for member
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(git(t, dir, args...))
}

func newPinsFixture(t *testing.T) pinsFixture {
	t.Helper()
	pinsIsolateHome(t)
	f := pinsFixture{Root: pinsRealDir(t)}
	f.Sub, f.Super = filepath.Join(f.Root, "sub"), filepath.Join(f.Root, "super")
	for _, d := range []string{f.Sub, f.Super} {
		mkdirs(t, d)
		git(t, d, "init", "-q", "-b", "main")
	}
	writeFile(t, filepath.Join(f.Sub, "file.txt"), "one\n")
	git(t, f.Sub, "add", "file.txt")
	git(t, f.Sub, "commit", "-qm", "sub one")
	f.Pin = gitIn(t, f.Sub, "rev-parse", "HEAD")
	writeFile(t, filepath.Join(f.Super, "top.txt"), "top\n")
	git(t, f.Super, "add", "top.txt")
	git(t, f.Super, "commit", "-qm", "super one")
	git(t, f.Super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", f.Sub, "member")
	git(t, f.Super, "commit", "-qm", "add member")
	return f
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// requireSubmodulePathConfig skips, before any fixture is touched, on a Git older than 2.56, which has no
// extensions.submodulePathConfig (the CI minimum lane runs Git 2.46).
func requireSubmodulePathConfig(t *testing.T) {
	t.Helper()
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		t.Skip("git is unavailable")
	}
	f := strings.Fields(string(out))
	if len(f) < 3 {
		t.Skipf("unrecognized git version %q", out)
	}
	v := strings.SplitN(f[2], ".", 3)
	major, _ := strconv.Atoi(v[0])
	minor := 0
	if len(v) > 1 {
		minor, _ = strconv.Atoi(v[1])
	}
	if major < 2 || major == 2 && minor < 56 {
		t.Skipf("Git %s has no extensions.submodulePathConfig (2.56): exercised on the current Git lane only", f[2])
	}
}

// mustOS fails the test on a fixture error instead of silently building a different fixture.
func mustOS(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// rmAll removes a path (a link is removed, never followed); a missing path is fine.
func rmAll(t *testing.T, path string) {
	t.Helper()
	mustOS(t, os.RemoveAll(path))
}
