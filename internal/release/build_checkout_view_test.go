// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These are native config LIST frames, not a project command runner. Complete
// semantic streams are private files; values must never enter diagnostics.
func TestBuildViewRenderConfig(t *testing.T) {
	type entry struct {
		scope, origin, key, value string
		has                       bool
	}
	entries := []entry{
		{"local", "file:/original/config", "core.repositoryformatversion", "0", true},
		{"local", "file:/original/config", "include.path", "/never/render/include", true},
		{"local", "file:/original/included", "filter.driver.clean", "", true},
		{"local", "file:/original/included", "fixture.sub\tpart.name", "line1\nline2\t\"\\", true},
		{"local", "file:/original/config", "fixture.boolean", "", false},
		{"local", "file:/original/config", "fixture.empty", "", true},
		{"worktree", "file:/original/config.worktree", "core.worktree", "/original/worktree", true},
		{"command", "command line:", "core.fsmonitor", "false", true},
	}
	var names, values strings.Builder
	for _, e := range entries {
		names.WriteString(e.scope + "\x00" + e.origin + "\x00" + e.key + "\x00")
		values.WriteString(e.scope + "\x00" + e.origin + "\x00" + e.key)
		if e.has {
			values.WriteString("\n" + e.value)
		}
		values.WriteByte(0)
	}
	spool := func(name, data string) *os.File {
		t.Helper()
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return f
	}
	var local, worktree bytes.Buffer
	if err := renderBuildViewConfig(spool("names", names.String()), spool("values", values.String()), &local, &worktree); err != nil {
		t.Fatal(err)
	}
	want := "[core]\nrepositoryformatversion = \"0\"\n[filter \"driver\"]\nclean = \"\"\n[fixture \"sub\tpart\"]\nname = \"line1\\nline2\\t\\\"\\\\\"\n[fixture]\nboolean\n[fixture]\nempty = \"\"\n"
	if local.String() != want {
		t.Fatalf("local rendering mismatch: got %q want %q", local.String(), want)
	}
	if worktree.String() != "[core]\nworktree = \"/original/worktree\"\n" {
		t.Fatal("worktree scope lost")
	}
	for _, change := range []struct{ name, value string }{
		{"key mismatch", strings.Replace(values.String(), "fixture.empty", "fixture.other", 1)},
		{"scope mismatch", strings.Replace(values.String(), "worktree\x00", "local\x00", 1)},
		{"origin mismatch", strings.Replace(values.String(), "file:/original/included", "file:/other", 1)},
		{"truncated", strings.TrimSuffix(values.String(), "\x00")},
	} {
		t.Run(change.name, func(t *testing.T) {
			if err := renderBuildViewConfig(spool("names", names.String()), spool("values", change.value), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
				t.Fatal("mismatched capture accepted")
			}
		})
	}
}

func TestBuildViewRenderNativeRoundTrip(t *testing.T) {
	dir := newRepo(t)
	for _, pair := range [][2]string{{"fixture.line\tbreak.value", "newline\nand tab\t"}, {"fixture.tab\tpart.empty", ""}, {"fixture.duplicate", "first"}, {"fixture.duplicate", "last"}} {
		git(t, dir, "config", "--add", pair[0], pair[1])
	}
	var output [2]*os.File
	for i, nameOnly := range []bool{true, false} {
		f, err := os.CreateTemp(t.TempDir(), "capture")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		args := []string{"--no-pager", "config", "--includes", "--null", "--show-scope", "--show-origin", "--list"}
		if nameOnly {
			args = append(args, "--name-only")
		}
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = gitEnv()
		cmd.Stdout = f
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			t.Fatal("native config capture failed", err)
		}
		output[i] = f
	}
	private := filepath.Join(t.TempDir(), "config")
	f, err := os.Create(private)
	if err != nil {
		t.Fatal(err)
	}
	var worktree bytes.Buffer
	if err := renderBuildViewConfig(output[0], output[1], f, &worktree); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	// The same trusted native parser must recover literal key/value boundaries.
	cmd := exec.Command("git", "--no-pager", "config", "--file", private, "--null", "--list")
	cmd.Dir = dir
	cmd.Env = gitEnv()
	actual, err := cmd.Output()
	if err != nil {
		t.Fatal("rendered config rejected by native parser", err)
	}
	for _, expected := range []string{"fixture.line\tbreak.value\nnewline\nand tab\t\x00", "fixture.tab\tpart.empty\n\x00", "fixture.duplicate\nfirst\x00fixture.duplicate\nlast\x00"} {
		if !bytes.Contains(actual, []byte(expected)) {
			t.Fatal("native roundtrip lost literal key, value or order")
		}
	}
}

func TestBuildViewCaptureBindsParentAndActualFile(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "source")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(parent, "index")
	if err := os.WriteFile(original, []byte("captured bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(base, "moved")
	actor := filepath.Join(base, "actor")
	if err := os.Mkdir(actor, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(actor, "index"), []byte("wrong bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := false
	binding, err := captureBuildViewPath(context.Background(), original, func(stage string) {
		if stage == "opened-directory:"+parent && !changed {
			if _, e := os.Stat(filepath.Join(parent, "index")); e == nil {
				if e = os.Rename(parent, moved); e != nil {
					t.Fatal(e)
				}
				if e = os.Symlink(actor, parent); e != nil {
					t.Fatal(e)
				}
				changed = true
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	snapshot, err := os.CreateTemp(t.TempDir(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	if err := binding.copyTo(snapshot); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.NewSectionReader(snapshot, 0, 1<<20))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "captured bytes" {
		t.Fatal("recursive acquisition reopened replaced parent")
	}
	if err := binding.validate(); err == nil {
		t.Fatal("replaced parent binding accepted")
	}
}
func TestBuildViewCaptureReadAndRevalidation(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "metadata")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	binding, err := captureBuildViewPath(context.Background(), link, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer binding.Close()
	snapshot, err := os.CreateTemp(t.TempDir(), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	if err := binding.copyTo(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := binding.validate(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("mutated!"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(target, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := binding.validate(); err == nil {
		t.Fatal("content mutation with restored mtime accepted")
	}
}

func TestBuildViewCaptureSymlinkResolution(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(".", filepath.Join(dir, "repeat")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	path := dir + "/repeat/repeat/index"
	b, err := captureBuildViewPath(context.Background(), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.Close()
	if err := os.Symlink("cycle/again", filepath.Join(dir, "cycle")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = captureBuildViewPath(ctx, filepath.Join(dir, "cycle/index"), nil)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("symlink cycle was not resolved as native path failure: %v", err)
	}
}

func TestBuildViewEnvironmentIsNativeOnly(t *testing.T) {
	inherited := []string{"HOME=/semantic/home", "XDG_CONFIG_HOME=/semantic/xdg", "LANG=en_US.UTF-8", "LC_CTYPE=C.UTF-8", "TZ=UTC", "SUDO_UID=42", "GITHUB_TOKEN=synthetic", "AWS_SECRET_ACCESS_KEY=synthetic", "LD_PRELOAD=/project/loader", "DYLD_INSERT_LIBRARIES=/project/loader", "BASH_ENV=/project/startup", "PATH=/project/bin", "GIT_DIR=/project/source", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=filter.fixture.clean", "STW_PRIVATE_MANIFEST=/project/manifest"}
	env := buildViewEnvironment(inherited, "/trusted/bin/git", "/private/job")
	joined := strings.Join(env, "\n")
	for _, bad := range []string{"synthetic", "/project/", "GIT_CONFIG_COUNT", "STW_PRIVATE"} {
		if strings.Contains(joined, bad) {
			t.Fatalf("native environment retained forbidden field %q", bad)
		}
	}
	for _, wanted := range []string{"HOME=/semantic/home", "XDG_CONFIG_HOME=/semantic/xdg", "LANG=en_US.UTF-8", "LC_CTYPE=C.UTF-8", "TZ=UTC", "SUDO_UID=42", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "TMPDIR=/private/job"} {
		found := false
		for _, v := range env {
			found = found || v == wanted
		}
		if !found {
			t.Fatalf("missing semantic/native environment field %q", wanted)
		}
	}
}

func TestBuildViewCaptureRawHeadSymlinkDoesNotRequirePhysicalTarget(t *testing.T) {
	dir := t.TempDir()
	head := filepath.Join(dir, "HEAD")
	// Native Git treats refs/... symlink HEAD as a symbolic ref in its ref store;
	// a linked worktree need not have that physical path under its own gitdir.
	if err := os.Symlink("refs/heads/main", head); err != nil {
		t.Fatal(err)
	}
	b, err := captureBuildViewMetadata(context.Background(), head, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !b.rawSymlink || b.linkTarget != "refs/heads/main" {
		t.Fatal("raw metadata symlink not preserved")
	}
	if err := b.validate(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(head); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("refs/heads/other", head); err != nil {
		t.Fatal(err)
	}
	if err := b.validate(); err == nil {
		t.Fatal("changed symbolic HEAD accepted")
	}
}
