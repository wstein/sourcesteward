// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
