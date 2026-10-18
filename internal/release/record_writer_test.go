// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package release

import (
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordExclusiveNoFollowWriter(t *testing.T) {
	for _, tc := range []string{"new", "identical", "different", "docs-symlink", "releases-symlink", "file-symlink", "directory"} {
		t.Run(tc, func(t *testing.T) {
			d := t.TempDir()
			out := t.TempDir()
			path := "docs/releases/v1.json"
			data := []byte("canonical\n")
			switch tc {
			case "docs-symlink":
				os.Symlink(out, filepath.Join(d, "docs"))
			case "releases-symlink":
				os.Mkdir(filepath.Join(d, "docs"), 0755)
				os.Symlink(out, filepath.Join(d, "docs/releases"))
			default:
				os.MkdirAll(filepath.Join(d, "docs/releases"), 0755)
			}
			switch tc {
			case "identical":
				os.WriteFile(filepath.Join(d, path), data, 0644)
			case "different":
				os.WriteFile(filepath.Join(d, path), []byte("existing"), 0644)
			case "file-symlink":
				os.WriteFile(filepath.Join(out, "file"), data, 0644)
				os.Symlink(filepath.Join(out, "file"), filepath.Join(d, path))
			case "directory":
				os.Mkdir(filepath.Join(d, path), 0755)
			}
			written, e := writeRecordExclusive(d, path, data)
			valid := tc == "new" || tc == "identical"
			if (e == nil) != valid || written != (tc == "new") {
				t.Fatalf("written=%v error=%v", written, e)
			}
			if tc == "different" {
				raw, _ := os.ReadFile(filepath.Join(d, path))
				if string(raw) != "existing" {
					t.Fatal("overwrote file")
				}
			}
			entries, _ := os.ReadDir(out)
			if len(entries) > 0 && tc != "file-symlink" {
				t.Fatal("wrote through symlink")
			}
		})
	}
}

func TestRecordWriterModeAndInjectedFailures(t *testing.T) {
	old := unix.Umask(0077)
	defer unix.Umask(old)
	d := t.TempDir()
	path := "docs/releases/v1.json"
	if written, e := writeRecordExclusive(d, path, []byte("canonical")); e != nil || !written {
		t.Fatalf("%t %v", written, e)
	}
	st, e := os.Stat(filepath.Join(d, path))
	if e != nil || st.Mode().Perm() != 0644 {
		t.Fatalf("mode %v error %v", st.Mode(), e)
	}
	for _, kind := range []string{"short", "interrupted", "close"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			hooks := recordWriteHooks{}
			if kind == "short" {
				hooks.write = func(f *os.File, data []byte) (int, error) { return f.Write(data[:1]) }
			}
			if kind == "interrupted" {
				hooks.write = func(f *os.File, data []byte) (int, error) { n, _ := f.Write(data[:1]); return n, io.ErrUnexpectedEOF }
			}
			if kind == "close" {
				hooks.afterClose = func() error { return io.ErrClosedPipe }
			}
			written, e := writeRecordExclusive(root, path, []byte("canonical"), hooks)
			if !written || e == nil {
				t.Fatalf("%t %v", written, e)
			}
			if _, e := os.Stat(filepath.Join(root, path)); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestRecordWriterDeterministicPathSwaps(t *testing.T) {
	for _, component := range []string{"docs", "docs/releases", "docs/releases/toolbox"} {
		t.Run(component, func(t *testing.T) {
			root := t.TempDir()
			outside := t.TempDir()
			if e := os.MkdirAll(filepath.Join(root, component), 0755); e != nil {
				t.Fatal(e)
			}
			fired := false
			hooks := recordWriteHooks{step: func(stage, path string) {
				if stage == "before-component" && path == component && !fired {
					fired = true
					os.Rename(filepath.Join(root, path), filepath.Join(root, path)+".saved")
					os.Symlink(outside, filepath.Join(root, path))
				}
			}}
			written, e := writeRecordExclusive(root, "docs/releases/toolbox/v1.json", []byte("canonical"), hooks)
			if !fired || written || e == nil {
				t.Fatalf("fired=%t written=%t error=%v", fired, written, e)
			}
			entries, _ := os.ReadDir(outside)
			if len(entries) != 0 {
				t.Fatal("followed swapped symlink")
			}
		})
	}
}
func TestRecordWriterPinsRenamedAncestorInode(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs/releases"), 0755)
	fired := false
	hooks := recordWriteHooks{step: func(stage, path string) {
		if stage == "after-component" && path == "docs" && !fired {
			fired = true
			os.Rename(filepath.Join(root, "docs"), filepath.Join(root, "owned-docs"))
			os.Symlink(outside, filepath.Join(root, "docs"))
		}
	}}
	written, e := writeRecordExclusive(root, "docs/releases/v1.json", []byte("canonical"), hooks)
	if e != nil || !written || !fired {
		t.Fatalf("%t %v", written, e)
	}
	if raw, e := os.ReadFile(filepath.Join(root, "owned-docs/releases/v1.json")); e != nil || string(raw) != "canonical" {
		t.Fatalf("%q %v", raw, e)
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) > 0 {
		t.Fatal("replacement redirected descriptor-relative write")
	}
}
func TestRecordWriterConcurrentCreationAndExistingMutation(t *testing.T) {
	for _, kind := range []string{"directory-create", "identical-create", "different-create", "existing-mutation"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := "docs/releases/v1.json"
			data := []byte("canonical")
			if kind == "existing-mutation" {
				os.MkdirAll(filepath.Join(root, "docs/releases"), 0755)
				os.WriteFile(filepath.Join(root, path), data, 0644)
			}
			fired := false
			hooks := recordWriteHooks{step: func(stage, name string) {
				if fired {
					return
				}
				switch {
				case kind == "directory-create" && stage == "before-mkdir" && name == "docs":
					fired = true
					os.Mkdir(filepath.Join(root, "docs"), 0755)
				case strings.HasSuffix(kind, "-create") && kind != "directory-create" && stage == "before-final":
					fired = true
					bytes := data
					if kind == "different-create" {
						bytes = []byte("different")
					}
					os.WriteFile(filepath.Join(root, path), bytes, 0644)
				case kind == "existing-mutation" && stage == "after-existing-read":
					fired = true
					os.WriteFile(filepath.Join(root, path), []byte("different"), 0644)
				}
			}}
			written, e := writeRecordExclusive(root, path, data, hooks)
			refuse := kind == "different-create" || kind == "existing-mutation"
			if !fired || (e != nil) != refuse || written != (kind == "directory-create") {
				t.Fatalf("fired=%t written=%t error=%v", fired, written, e)
			}
		})
	}
}

func TestRecordWriterExistingHardlinksSpecialAndOversizedFiles(t *testing.T) {
	for _, kind := range []string{"hardlink-identical", "hardlink-different", "fifo", "oversized", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			os.MkdirAll(filepath.Join(root, "docs/releases"), 0755)
			path := "docs/releases/v1.json"
			full := filepath.Join(root, path)
			original := filepath.Join(root, "original")
			data := []byte("canonical")
			switch kind {
			case "hardlink-identical", "hardlink-different":
				raw := data
				if kind == "hardlink-different" {
					raw = []byte("different")
				}
				os.WriteFile(original, raw, 0600)
				if e := os.Link(original, full); e != nil {
					t.Fatal(e)
				}
			case "fifo":
				if e := unix.Mkfifo(full, 0600); e != nil {
					t.Fatal(e)
				}
			case "oversized":
				f, e := os.Create(full)
				if e != nil {
					t.Fatal(e)
				}
				f.Truncate(MaxReleaseDocumentBytes + 1)
				f.Close()
			case "directory":
				os.Mkdir(full, 0755)
			}
			before := snapshot(t, root)
			done := make(chan error, 1)
			var written bool
			go func() { var e error; written, e = writeRecordExclusive(root, path, data); done <- e }()
			select {
			case e := <-done:
				if written || (e == nil) != (kind == "hardlink-identical") {
					t.Fatalf("%t %v", written, e)
				}
			case <-time.After(time.Second):
				t.Fatal("blocked on existing special file")
			}
			if diff := differences(before, snapshot(t, root)); len(diff) > 0 {
				t.Fatal(diff)
			}
		})
	}
}
func TestRecordWriterRejectsDeviceDestination(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs/releases"), 0755)
	full := filepath.Join(root, "docs/releases/v1.json")
	if e := unix.Mknod(full, unix.S_IFCHR|0600, int(unix.Mkdev(1, 3))); e != nil {
		t.Skipf("device node fixture unavailable: %v", e)
	}
	done := make(chan error, 1)
	go func() { _, e := writeRecordExclusive(root, "docs/releases/v1.json", []byte("canonical")); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("accepted device destination")
		}
	case <-time.After(time.Second):
		t.Fatal("blocked on device destination")
	}
}
