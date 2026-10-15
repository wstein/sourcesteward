// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestJJMachineRecordsAndVersions(t *testing.T) {
	for _, s := range []string{"jj", "\"a\"", "\"a\"\nextra\n", "null\n", "\"a\"\n\"b\"\n"} {
		if _, e := scalarRecord(s, 1); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, v := range []string{"0.44.0", "0.45.0", "0.46.0-rc1", "1.0.0", "0.46", "x.46.0"} {
		if supportedJJVersion(v) {
			t.Fatalf("unsupported version %q", v)
		}
	}
	for _, v := range []string{"0.45.1", "0.46.0", "0.46.0-7d382314f9420b4b8d932245b49c99492c812b9f"} {
		if !supportedJJVersion(v) {
			t.Fatal(v)
		}
	}
}
func TestJJEnvironmentIsolation(t *testing.T) {
	for _, key := range []string{"JJ_CONFIG", "GIT_TRACE", "RUST_LOG", "LD_PRELOAD", "DYLD_INSERT_LIBRARIES", "BASH_ENV", "ENV", "TMPDIR"} {
		t.Setenv(key, "sensitive-injection")
	}
	for _, entry := range jjEnvironment("/private/config") {
		if strings.Contains(entry, "sensitive-injection") {
			t.Fatal("inherited injection", entry)
		}
	}
}
func TestJJMetadataBounds(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "file")
	os.WriteFile(p, make([]byte, outputLimit+1), 0600)
	if _, e := metadataFile(p); e == nil {
		t.Fatal("oversized metadata accepted")
	}
	os.Remove(p)
	os.Symlink("missing", p)
	if _, e := metadataFile(p); e == nil {
		t.Fatal("symlink accepted")
	}
	if !within(d, filepath.Join(d, "child")) || within(d, d+"-other") {
		t.Fatal("path containment")
	}
}
func TestJJCancellation(t *testing.T) {
	d := t.TempDir()
	os.Mkdir(filepath.Join(d, ".jj"), 0700)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := Inspect(ctx, d)
	if r.Diagnostics[0].Code != "canceled" {
		t.Fatal(r)
	}
}

func requireJJSandbox(t *testing.T, d string) {
	t.Helper()
	r := Inspect(context.Background(), d)
	if len(r.Diagnostics) > 0 && r.Diagnostics[0].Code == "jj-sandbox-unavailable" && os.Getenv("STW_REQUIRE_JJ") != "1" {
		t.Skip("enforced native jj sandbox unavailable")
	}
	if r.Status != "ok" {
		t.Fatalf("sandbox baseline: %+v", r)
	}
}

func TestJJReadSideRepairsAreDenied(t *testing.T) {
	requireJJ(t)
	for _, kind := range []string{"missing-index", "corrupt-index", "missing-type", "corrupt-type"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			d := filepath.Join(root, "repo")
			jjFixture(t, root, "git", "init", d)
			requireJJSandbox(t, d)
			switch kind {
			case "missing-index":
				entries, e := os.ReadDir(filepath.Join(d, ".jj/repo/index/op_links"))
				if e != nil {
					t.Fatal(e)
				}
				for _, v := range entries {
					if e := os.Remove(filepath.Join(d, ".jj/repo/index/op_links", v.Name())); e != nil {
						t.Fatal(e)
					}
				}
			case "corrupt-index":
				entries, e := os.ReadDir(filepath.Join(d, ".jj/repo/index/segments"))
				if e != nil {
					t.Fatal(e)
				}
				for _, v := range entries {
					os.WriteFile(filepath.Join(d, ".jj/repo/index/segments", v.Name()), []byte("corrupt"), 0600)
				}
			case "missing-type":
				os.Remove(filepath.Join(d, ".jj/repo/store/type"))
			case "corrupt-type":
				os.WriteFile(filepath.Join(d, ".jj/repo/store/type"), []byte("unknown"), 0600)
			}
			before := snapshot(t, root)
			r := Inspect(context.Background(), d)
			if r.Status != "error" || r.SchemaVersion != 2 {
				t.Fatalf("repair accepted: %+v", r)
			}
			if !reflect.DeepEqual(before, snapshot(t, root)) {
				t.Fatal("read repaired original metadata")
			}
		})
	}
}

func TestJJStaleAndDivergentOperations(t *testing.T) {
	requireJJ(t)
	for _, kind := range []string{"stale", "divergent"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			d := filepath.Join(root, "repo")
			jjFixture(t, root, "git", "init", d)
			requireJJSandbox(t, d)
			if kind == "stale" {
				second := filepath.Join(root, "second")
				jjFixture(t, d, "workspace", "add", second, "--name", "second")
				jjFixture(t, d, "describe", "-r", "second@", "-m", "changed elsewhere")
				d = second
			} else {
				op := strings.TrimSpace(jjFixture(t, d, "--ignore-working-copy", "--at-op=@", "operation", "log", "-n", "1", "--no-graph", "-T", "self.id()"))
				jjFixture(t, d, "--at-op="+op, "describe", "-m", "first")
				jjFixture(t, d, "--at-op="+op, "describe", "-m", "second")
			}
			before := snapshot(t, root)
			r := Inspect(context.Background(), d)
			if kind == "divergent" && r.Status != "error" {
				t.Fatalf("divergent heads accepted %+v", r)
			}
			if r.Status == "ok" && (r.JJRepository == nil || r.JJRepository.Observation.WorkingCopy != "stored-unsnapshotted") {
				t.Fatal(r)
			}
			if !reflect.DeepEqual(before, snapshot(t, root)) {
				t.Fatal("stale/divergent inspection changed bytes")
			}
		})
	}
}

func TestJJHostileConfigurationAndEnvironment(t *testing.T) {
	requireJJ(t)
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	config := filepath.Join(root, "config")
	os.Mkdir(config, 0700)
	t.Setenv("XDG_CONFIG_HOME", config)
	d := filepath.Join(root, "repo")
	jjFixture(t, root, "git", "init", d)
	requireJJSandbox(t, d)
	// Secure repo/workspace configuration must be excluded, including aliases of builtin template functions.
	jjFixture(t, d, "config", "set", "--repo", `template-aliases."json(x)"`, `"evil"`)
	jjFixture(t, d, "config", "set", "--workspace", `template-aliases."stringify(x)"`, `"evil"`)
	sentinel := filepath.Join(root, "helper-ran")
	helper := filepath.Join(root, "helper")
	os.WriteFile(helper, []byte("#!/bin/sh\nprintf ran > '"+sentinel+"'\n"), 0700)
	jjFixture(t, d, "config", "set", "--repo", "ui.pager", helper)
	jjFixture(t, d, "config", "set", "--repo", "core.fsmonitor", "watchman")
	for _, key := range []string{"JJ_CONFIG", "JJ_TRACE", "JJ_LOG", "GIT_TRACE", "GIT_TRACE_SETUP", "BASH_ENV", "ENV"} {
		t.Setenv(key, helper)
	}
	t.Setenv("TMPDIR", d)
	before := snapshot(t, root)
	r := Inspect(context.Background(), d)
	if r.Status != "ok" {
		t.Fatalf("isolated identity %+v", r)
	}
	if !reflect.DeepEqual(before, snapshot(t, root)) {
		t.Fatal("configuration or environment created effects")
	}
	if _, e := os.Stat(sentinel); !os.IsNotExist(e) {
		t.Fatal("helper ran")
	}
}

func TestJJSessionBoundedProcess(t *testing.T) {
	if os.Getenv("STW_REQUIRE_JJ") == "1" {
		requireJJ(t)
	}
	// A runner unit seam isolates process cancellation/output limits from native repository semantics.
	root := t.TempDir()
	fake := filepath.Join(root, "sandbox")
	os.WriteFile(fake, []byte("#!/bin/sh\nwhile :; do printf xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx; done\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	s := jjSession{ctx: ctx, sandbox: fake, env: jjEnvironment(root)}
	_, _, e := s.execute(root, "unused", nil)
	if e == nil {
		t.Fatal("unbounded process accepted")
	}
}

func TestJJSelectedFailureHasNativeContract(t *testing.T) {
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, ".jj"), 0700); err != nil {
		t.Fatal(err)
	}
	r := Inspect(context.Background(), d)
	if r.SchemaVersion != 2 || r.Backend != "jj" || r.Status != "error" || r.Repository != nil {
		t.Fatalf("%+v", r)
	}
}

func jjFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("jj", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "JJ_CONFIG=", "JJ_USER=Test", "JJ_EMAIL=test@example.invalid")
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("jj %v: %s %v", args, b, e)
	}
	return string(b)
}

func requireJJ(t *testing.T) {
	t.Helper()
	if _, e := exec.LookPath("jj"); e != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal("mandatory jj executable unavailable")
		}
		t.Skip("jj executable unavailable")
	}
}

func TestJJConformance(t *testing.T) {
	requireJJ(t)
	for _, shape := range []string{"standalone", "colocated", "secondary", "edited", "nested", "unusual"} {
		t.Run(shape, func(t *testing.T) {
			root := t.TempDir()
			d := filepath.Join(root, "repo")
			if shape == "unusual" {
				d = filepath.Join(root, "<>& space\n日本語")
			}
			args := []string{"git", "init", d}
			if shape == "colocated" {
				args = append(args, "--colocate")
			}
			jjFixture(t, root, args...)
			if shape == "secondary" {
				d = filepath.Join(root, "second")
				jjFixture(t, filepath.Join(root, "repo"), "workspace", "add", d, "--name", "second")
			}
			if shape == "edited" {
				os.WriteFile(filepath.Join(d, "untracked"), []byte("unsnapshotted"), 0600)
			}
			if shape == "nested" {
				d = filepath.Join(d, "nested")
				os.Mkdir(d, 0700)
			}
			before := snapshot(t, root)
			r := Inspect(context.Background(), d)
			if r.Diagnostics != nil && len(r.Diagnostics) > 0 && r.Diagnostics[0].Code == "jj-sandbox-unavailable" && os.Getenv("STW_REQUIRE_JJ") != "1" {
				t.Skip("enforced jj sandbox unavailable")
			}
			if r.Status != "ok" || r.Backend != "jj" || r.SchemaVersion != 2 || r.Repository != nil {
				t.Fatalf("%+v", r)
			}
			if !reflect.DeepEqual(before, snapshot(t, root)) {
				t.Fatal("native inspection changed repository bytes")
			}
		})
	}
}

func TestJJDoesNotRequireGit(t *testing.T) {
	requireJJ(t)
	root := t.TempDir()
	jjFixture(t, root, "git", "init", filepath.Join(root, "repo"))
	jjPath, _ := exec.LookPath("jj")
	bindir := t.TempDir()
	if err := os.Symlink(jjPath, filepath.Join(bindir, "jj")); err != nil {
		t.Fatal(err)
	}
	// The backend must be selected before attempting the injected unavailable Git runner.
	r := inspect(context.Background(), filepath.Join(root, "repo"), func(context.Context, string, ...string) (string, int, error) {
		t.Fatal("Git invoked for jj ownership")
		return "", -1, exec.ErrNotFound
	})
	if r.Backend != "jj" || r.SchemaVersion != 2 || strings.Contains(r.Error(), "Git executable") {
		t.Fatalf("%+v", r)
	}
}
