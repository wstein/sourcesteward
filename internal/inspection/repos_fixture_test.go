// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// reposHermetic keeps a test independent of the developer's HOME and global Git configuration. System
// configuration cannot be neutralized for Inspect and Inventory (their runner drops every GIT_ variable);
// the fixtures themselves are created with GIT_CONFIG_NOSYSTEM by git().
func reposHermetic(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
}

// reposTempDir returns a symlink-free temporary directory, so expectations do not depend on a symlinked
// TMPDIR (macOS /var, /tmp).
func reposTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// reposNewRepo creates a Git repository with one commit at dir (created when absent).
func reposNewRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-qm", "initial")
	return dir
}

// reposSuper creates a superproject at dir with the named submodules (each cloned from a private source
// repository) and commits the registration.
func reposSuper(t *testing.T, dir string, names ...string) string {
	t.Helper()
	reposNewRepo(t, dir)
	src := reposNewRepo(t, filepath.Join(reposTempDir(t), "src"))
	for _, n := range names {
		git(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, n)
	}
	git(t, dir, "commit", "-qm", "submodules")
	return dir
}

// reposRequireGit skips before any fixture is touched when Git is older than the given minor version.
func reposRequireGit(t *testing.T, minor int) {
	t.Helper()
	out, err := exec.Command("git", "version").Output()
	if err != nil {
		t.Skip("git unavailable")
	}
	var major, mnr int
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		t.Skip("unrecognized git version")
	}
	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		t.Skip("unrecognized git version")
	}
	major, mnr = atoiPrefix(parts[0]), atoiPrefix(parts[1])
	if major < 2 || (major == 2 && mnr < minor) {
		t.Skipf("needs Git 2.%d, have %s", minor, fields[2])
	}
}

func atoiPrefix(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
