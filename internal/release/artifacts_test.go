// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChecksumProfile(t *testing.T) {
	h := strings.Repeat("a", 64)
	for _, s := range []string{h + "  one\n", h + " *one\n"} {
		m, e := parseChecksums([]byte(s))
		if e != nil || m["one"] != h {
			t.Fatalf("%v %v", m, e)
		}
	}
	for _, s := range []string{h + " one\n", h + "  ../one\n", h + "  a/b\n", h + "  one\n" + h + "  one\n", strings.Repeat("x", 65537), h + "  one", h + "  \xff\n"} {
		if _, e := parseChecksums([]byte(s)); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
func TestArtifactDirectoryRefusals(t *testing.T) {
	dir := t.TempDir()
	data := []byte("asset")
	h := fmt.Sprintf("%x", sha256.Sum256(data))
	want := []FileRef{{Name: "one", Size: int64(len(data)), SHA256: h}}
	for _, tc := range []string{"missing", "mismatch", "extra", "symlink", "directory", "valid"} {
		t.Run(tc, func(t *testing.T) {
			p := filepath.Join(dir, "one")
			os.RemoveAll(p)
			os.Remove(filepath.Join(dir, "extra"))
			switch tc {
			case "missing":
			case "symlink":
				os.Symlink("absent", p)
			case "directory":
				os.Mkdir(p, 0755)
			default:
				os.WriteFile(p, data, 0644)
			}
			if tc == "mismatch" {
				os.WriteFile(p, []byte("other"), 0644)
			}
			if tc == "extra" {
				os.WriteFile(filepath.Join(dir, "extra"), nil, 0644)
			}
			_, g, e := inspectArtifacts(dir, want)
			if e != nil {
				t.Fatal(e)
			}
			failed := false
			for _, x := range g {
				failed = failed || x.Outcome == "fail"
			}
			if failed == (tc == "valid") {
				t.Fatalf("%+v", g)
			}
		})
	}
}

func TestArtifactSizeAndCancellationBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "oversize")
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(maxArtifactBytes + 1); e != nil {
		f.Close()
		t.Fatal(e)
	}
	f.Close()
	if _, e = hashArtifact(context.Background(), path); !errors.Is(e, errReleaseLimit) {
		t.Fatalf("%v", e)
	}
	os.WriteFile(path, []byte("small"), 0644)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = hashArtifact(ctx, path); !errors.Is(e, context.Canceled) {
		t.Fatalf("%v", e)
	}
}
