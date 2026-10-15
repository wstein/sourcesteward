// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package inspection

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestGitInventoryConfinedDiskOpen(t *testing.T) {
	d := t.TempDir()
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "private"), []byte("private"), 0600)
	os.Symlink(outside, filepath.Join(d, "escape"))
	os.Symlink(filepath.Join(outside, "private"), filepath.Join(d, "symlink"))
	syscall.Mkfifo(filepath.Join(d, "fifo"), 0600)
	root, e := os.OpenRoot(d)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	for _, path := range []string{"escape/private", "symlink", "fifo"} {
		f, e := openGitInventoryDisk(root, path)
		if e == nil {
			f.Close()
			t.Fatalf("unsafe path accepted: %s", path)
		}
	}
	os.WriteFile(filepath.Join(d, "ordinary"), []byte("ordinary"), 0600)
	f, e := openGitInventoryDisk(root, "ordinary")
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
}

func TestGitInventoryLiteralQueryDoesNotReadOtherDiskPaths(t *testing.T) {
	d := repo(t)
	for _, name := range []string{"safe", "other"} {
		os.WriteFile(filepath.Join(d, name), []byte("clean"), 0600)
	}
	git(t, d, "add", ".")
	git(t, d, "commit", "-qm", "initial")
	os.Remove(filepath.Join(d, "other"))
	syscall.Mkfifo(filepath.Join(d, "other"), 0600)
	r := gitInventory(t, d, InventoryOptions{Query: "clean", Scopes: []string{"disk"}, Paths: []string{"safe"}})
	if r.Status != "ok" || r.Query.Value != "true" {
		t.Fatalf("%+v", r)
	}
	if r.Workspaces[0].Tracked.Value != "unknown" {
		t.Fatal("unobserved broader workspace claimed clean")
	}
}
