// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/release"
)

func TestReleaseCheckNativeContractLimitExitThree(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	home := t.TempDir()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("jj", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home, "JJ_CONFIG=", "JJ_USER=Test", "JJ_EMAIL=t@example.org")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("jj %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("git", "init", "--no-colocate", dir)
	if err := os.MkdirAll(filepath.Join(dir, ".stw"), 0755); err != nil {
		t.Fatal(err)
	}
	data := append(bytes.Repeat([]byte("\n"), release.MaxContractBytes), releaseExample(t, "go-cli")...)
	if err := os.WriteFile(filepath.Join(dir, ".stw", "release.toml"), data, 0644); err != nil {
		t.Fatal(err)
	}
	run("commit", "-m", "oversized contract")
	commit := run("--ignore-working-copy", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
	status, out, _ := runRelease(t, "check", dir, "--commit", commit, "--json")
	if status != 3 || !strings.Contains(out, `"status":"findings"`) || !strings.Contains(out, `"code":"limit-exceeded"`) {
		t.Fatalf("status=%d output=%s", status, out)
	}
}

func TestReleaseNativeSymlinkCapabilityExitOne(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not installed")
	}
	home := t.TempDir()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("jj", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home, "JJ_CONFIG=", "JJ_USER=Test", "JJ_EMAIL=t@example.org")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("jj %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("git", "init", "--no-colocate", dir)
	for _, path := range []string{".stw", "scripts", "docs"} {
		if err := os.MkdirAll(filepath.Join(dir, path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, ".stw/release.toml"), releaseExample(t, "go-cli"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs/run.sh"), []byte("build input"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../docs/run.sh", filepath.Join(dir, "scripts/release-package.sh")); err != nil {
		t.Fatal(err)
	}
	run("commit", "-m", "symlink input")
	commit := run("--ignore-working-copy", "log", "--no-graph", "-r", "@-", "-T", "commit_id")
	run("git", "remote", "add", "origin", "https://github.com/example/test.git")
	for _, args := range [][]string{{"check", dir, "--commit", commit, "--json"}, {"plan", commit, dir, "--version", "v1.2.3", "--json"}} {
		status, out, _ := runRelease(t, args...)
		if status != 1 || !strings.Contains(out, `"status":"refused"`) || !strings.Contains(out, `"code":"unsupported-backend"`) || !strings.Contains(out, `"complete":false`) || !strings.Contains(out, "native jj committed symlink input resolution unavailable") {
			t.Fatalf("status=%d output=%s", status, out)
		}
	}
}
