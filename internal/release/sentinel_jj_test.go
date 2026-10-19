// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// jjLanes are the jj binaries the environment can exercise: the stable one on PATH and the
// documented minimum, 0.45.1, when it is installed next to it.
func jjLanes(t *testing.T) map[string]string {
	t.Helper()
	lanes := map[string]string{}
	if path, err := exec.LookPath("jj"); err == nil {
		lanes["jj"] = path
	}
	home, _ := os.UserHomeDir()
	for _, candidate := range []string{filepath.Join(home, ".local", "bin", "jj-0.45.1"), "/usr/local/bin/jj-0.45.1"} {
		if _, err := os.Stat(candidate); err == nil {
			lanes["jj-0.45.1"] = candidate
		}
	}
	if len(lanes) == 0 {
		t.Skip("jj is not installed; the jj effect sentinels cannot run here")
	}
	return lanes
}

func jjEnvironment(home string) []string {
	return append(withoutInheritedGit(os.Environ()), "HOME="+home, "XDG_CONFIG_HOME="+home, "JJ_CONFIG=", "JJ_USER=Test", "JJ_EMAIL=t@example.org", "JJ_TIMESTAMP=2026-01-01T00:00:00Z", "JJ_OP_TIMESTAMP=2026-01-01T00:00:00Z")
}

func jjRun(t *testing.T, dir, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("jj", append([]string{"--no-pager", "--color=never"}, args...)...)
	cmd.Dir, cmd.Env = dir, jjEnvironment(home)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// useJJ makes `jj` resolve to the lane binary for the inspection under test.
func useJJ(t *testing.T, binary string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.Symlink(binary, filepath.Join(bin, "jj")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

type jjLayout struct {
	name  string
	setup func(t *testing.T, home string) (workspace string, ok bool)
}

func jjLayouts() []jjLayout {
	initRepo := func(flag string) func(*testing.T, string) (string, bool) {
		return func(t *testing.T, home string) (string, bool) {
			dir, _ := filepath.EvalSymlinks(t.TempDir())
			if out, err := jjRun(t, dir, home, "git", "init", flag, dir); err != nil {
				t.Logf("jj git init %s is not available: %s", flag, out)
				return "", false
			}
			return dir, true
		}
	}
	return []jjLayout{
		{"colocated", initRepo("--colocate")},
		{"standalone", initRepo("--no-colocate")},
		{"secondary workspace", func(t *testing.T, home string) (string, bool) {
			main, ok := initRepo("--no-colocate")(t, home)
			if !ok {
				return "", false
			}
			second := filepath.Join(filepath.Dir(main), filepath.Base(main)+"-second")
			t.Cleanup(func() { os.RemoveAll(second) })
			if out, err := jjRun(t, main, home, "workspace", "add", second); err != nil {
				t.Logf("jj workspace add failed: %s", out)
				return "", false
			}
			resolved, _ := filepath.EvalSymlinks(second)
			return resolved, true
		}},
	}
}

func jjState(t *testing.T, dir, home string) string {
	t.Helper()
	ops, err := jjRun(t, dir, home, "--ignore-working-copy", "op", "log", "--no-graph", "-T", `id.short(16) ++ " " ++ description ++ "\n"`)
	if err != nil {
		t.Fatalf("op log: %s", ops)
	}
	wc, err := jjRun(t, dir, home, "--ignore-working-copy", "log", "--no-graph", "-r", "@", "-T", `commit_id ++ " " ++ change_id ++ "\n"`)
	if err != nil {
		t.Fatalf("log: %s", wc)
	}
	return ops + "--\n" + wc
}

func TestCheckHasNoEffectOnAJJWorkspace(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		for _, layout := range jjLayouts() {
			t.Run(lane+"/"+layout.name, func(t *testing.T) {
				useJJ(t, binary)
				home := t.TempDir()
				dir, ok := layout.setup(t, home)
				if !ok {
					t.Skipf("%s: layout %s cannot be created with this jj", lane, layout.name)
				}
				// The contract is an unsnapshotted working-copy file: any jj command that snapshots would record it.
				version, _ := jjRun(t, dir, home, "--version")
				t.Logf("lane %s runs %s", lane, strings.TrimSpace(version))
				write(t, dir, ".stw/release.toml", exampleBytes(t, "go-cli"))
				write(t, dir, "scripts/release-package.sh", []byte("#!/bin/sh\n"))
				stateBefore, filesBefore := jjState(t, dir, home), snapshot(t, dir)
				result := run(t, CheckOptions{Path: dir})
				if result.Status != "ok" || result.Backend != "jj" || result.Contract == nil {
					t.Fatalf("working-tree form: %+v %q", result, codes(result))
				}
				refused := run(t, CheckOptions{Path: dir, Commit: strings.Repeat("a", 40)})
				if refused.Status != "refused" || refused.Error.Code != "commit-unresolved" {
					t.Fatalf("--commit: %+v", refused)
				}
				if after := jjState(t, dir, home); after != stateBefore {
					t.Fatalf("the operation log or the working-copy commit changed:\nbefore:\n%s\nafter:\n%s", stateBefore, after)
				}
				if d := differences(filesBefore, snapshot(t, dir)); len(d) != 0 {
					t.Fatalf("check changed the workspace:\n%s", strings.Join(d, "\n"))
				}
			})
		}
	}
}

// A sentinel that cannot see a snapshot proves nothing, so show that a normal jj command is seen.
func TestJJSentinelsSeeASnapshot(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			home := t.TempDir()
			dir, ok := jjLayouts()[0].setup(t, home)
			if !ok {
				t.Skip("no colocated jj repository")
			}
			write(t, dir, "new.txt", []byte("x"))
			before := jjState(t, dir, home)
			if out, err := jjRun(t, dir, home, "status"); err != nil {
				t.Fatalf("jj status: %s", out)
			}
			if jjState(t, dir, home) == before {
				t.Fatal("jj status did not snapshot the working copy, so the sentinel cannot detect one")
			}
		})
	}
}

func TestCheckRefusesAmbiguousOwnership(t *testing.T) {
	for lane, binary := range jjLanes(t) {
		t.Run(lane, func(t *testing.T) {
			useJJ(t, binary)
			home := t.TempDir()
			dir, ok := jjLayouts()[0].setup(t, home)
			if !ok {
				t.Skip("no colocated jj repository")
			}
			write(t, dir, ".stw/release.toml", exampleBytes(t, "go-cli"))
			if err := os.MkdirAll(filepath.Join(dir, ".git", "gitbutler"), 0o755); err != nil {
				t.Fatal(err)
			}
			filesBefore := snapshot(t, dir)
			r := Check(context.Background(), CheckOptions{Path: dir})
			if r.Status != "refused" || r.Error == nil || r.Error.Code != "ambiguous-backend" {
				t.Fatalf("%+v", r)
			}
			if d := differences(filesBefore, snapshot(t, dir)); len(d) != 0 {
				t.Fatalf("%v", d)
			}
		})
	}
}
