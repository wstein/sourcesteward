// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJJTidyPrivateExecutionContext(t *testing.T) {
	for _, kind := range []string{"valid", "changed", "symlink", "hardlink", "oversized", "missing"} {
		t.Run(kind, func(t *testing.T) {
			root, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			metadata := filepath.Join(root, "source", ".jj")
			wc := filepath.Join(metadata, "working_copy")
			os.MkdirAll(wc, 0700)
			os.Mkdir(filepath.Join(metadata, "repo"), 0700)
			for _, name := range []string{"checkout", "tree_state", "type"} {
				if e := os.WriteFile(filepath.Join(wc, name), []byte("native"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			p := filepath.Join(wc, "checkout")
			switch kind {
			case "symlink":
				os.Remove(p)
				os.Symlink(filepath.Join(wc, "type"), p)
			case "hardlink":
				os.Link(p, filepath.Join(root, "alias"))
			case "oversized":
				os.WriteFile(p, make([]byte, outputLimit+1), 0600)
			case "missing":
				os.Remove(p)
			}
			private := filepath.Join(root, "private")
			os.Mkdir(private, 0700)
			path, verify, e := prepareJJTidyContext(private, metadata, filepath.Join(metadata, "repo"))
			if kind == "symlink" || kind == "hardlink" || kind == "oversized" || kind == "missing" {
				if e == nil {
					t.Fatal("unsafe metadata copied")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e := os.Lstat(filepath.Join(path, ".git")); !os.IsNotExist(e) {
				t.Fatal("execution context is colocated")
			}
			b, e := os.ReadFile(filepath.Join(path, ".jj", "repo"))
			if e != nil || string(b) != filepath.Join(metadata, "repo") {
				t.Fatalf("invalid native store pointer %q %v", b, e)
			}
			if kind == "changed" {
				os.WriteFile(p, []byte("changed"), 0600)
				if verify() == nil {
					t.Fatal("changed source metadata accepted")
				}
			} else if e := verify(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
