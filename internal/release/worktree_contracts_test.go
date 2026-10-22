// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStw(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".stw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".stw", name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadWorktreeContracts(t *testing.T) {
	example, err := os.ReadFile("../../specs/026-release-process/examples/go-cli.release.toml")
	if err != nil {
		t.Fatal(err)
	}
	t.Run("no .stw directory declares nothing", func(t *testing.T) {
		got, err := ReadWorktreeContracts(t.TempDir())
		if err != nil || len(got.Products) != 0 || !got.Valid() {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("a valid contract", func(t *testing.T) {
		root := t.TempDir()
		writeStw(t, root, "release.toml", string(example))
		got, err := ReadWorktreeContracts(root)
		if err != nil || !got.Valid() || len(got.Products) != 1 || got.Products[0].Contract == nil || got.Products[0].File != ".stw/release.toml" || got.Products[0].Product != "" {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("an invalid contract is present but not valid", func(t *testing.T) {
		root := t.TempDir()
		writeStw(t, root, "release.toml", strings.Replace(string(example), "schema_version = 1", "schema_version = 7", 1))
		got, err := ReadWorktreeContracts(root)
		if err != nil || got.Valid() || len(got.Products) != 1 || got.Products[0].Contract != nil {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("a linked contract is never followed", func(t *testing.T) {
		root := t.TempDir()
		writeStw(t, root, "real.txt", string(example))
		if err := os.Symlink("real.txt", filepath.Join(root, ".stw", "release.toml")); err != nil {
			t.Fatal(err)
		}
		got, err := ReadWorktreeContracts(root)
		if err != nil || got.Valid() {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("a default file mixed with a named one is not valid", func(t *testing.T) {
		root := t.TempDir()
		writeStw(t, root, "release.toml", string(example))
		writeStw(t, root, "release.other.toml", string(example))
		if got, err := ReadWorktreeContracts(root); err != nil || got.Valid() {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run(".stw that is a file is an error", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".stw"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadWorktreeContracts(root); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("a .stw listing past its bound is refused", func(t *testing.T) {
		root := t.TempDir()
		writeStw(t, root, "release.toml", string(example))
		for i := 0; i <= maxStwEntries; i++ {
			if err := os.WriteFile(filepath.Join(root, ".stw", fmt.Sprintf("n%05d.txt", i)), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ReadWorktreeContracts(root); !errors.Is(err, errReleaseLimit) {
			t.Fatalf("%v", err)
		}
	})
}
