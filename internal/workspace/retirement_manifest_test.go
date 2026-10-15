// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func manifestFixture(t *testing.T) (string, retirementManifestAdmission) {
	t.Helper()
	d, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(d, "dir"), 0700); e != nil {
		t.Fatal(e)
	}
	b := []byte{0, 255, 10}
	if e = os.WriteFile(filepath.Join(d, "dir", "data"), b, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(d, ".git"), []byte("gitdir: exact\n"), 0600); e != nil {
		t.Fatal(e)
	}
	return d, retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{".git"}, StoredFiles: []RetirementStoredFile{{Path: "dir/data", Digest: fmt.Sprintf("%x", sha256.Sum256(b)), Size: int64(len(b))}}}
}
func TestRetirementManifestExactBinaryAndRemoval(t *testing.T) {
	d, a := manifestFixture(t)
	m, e := observeRetirementManifest(context.Background(), d, a)
	if e != nil {
		t.Fatal(e)
	}
	if !m.Complete || len(m.Entries) != 3 {
		t.Fatalf("manifest %+v", m)
	}
	root, e := currentIdentity(d)
	if e != nil {
		t.Fatal(e)
	}
	parent, e := currentIdentity(filepath.Dir(d))
	if e != nil {
		t.Fatal(e)
	}
	if e = verifyRecordedRetirementManifest(context.Background(), root, m); e != nil {
		t.Fatal(e)
	}
	out, e := removeRecordedManifest(context.Background(), root, parent, m)
	if e != nil || !out.Attempted || !out.Complete {
		t.Fatalf("%+v %v", out, e)
	}
	if _, e = os.Lstat(d); !os.IsNotExist(e) {
		t.Fatalf("destination remains: %v", e)
	}
}
func TestRetirementManifestRefusesUnknownAndStale(t *testing.T) {
	for _, kind := range []string{"ignored", "empty-dir", "symlink", "hardlink", "changed-content", "changed-mode", "replaced-inode", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			d, a := manifestFixture(t)
			m, e := observeRetirementManifest(context.Background(), d, a)
			if e != nil {
				t.Fatal(e)
			}
			root, _ := currentIdentity(d)
			parent, _ := currentIdentity(filepath.Dir(d))
			p := filepath.Join(d, "dir", "data")
			ctx := context.Background()
			switch kind {
			case "ignored":
				e = os.WriteFile(filepath.Join(d, "extra"), nil, 0600)
			case "empty-dir":
				e = os.Mkdir(filepath.Join(d, "extra"), 0700)
			case "symlink":
				e = os.Symlink(p, filepath.Join(d, "extra"))
			case "hardlink":
				e = os.Link(p, filepath.Join(d, "extra"))
			case "changed-content":
				e = os.WriteFile(p, []byte("new"), 0600)
			case "changed-mode":
				e = os.Chmod(p, 0700)
			case "replaced-inode":
				e = os.Rename(p, p+"old")
				if e == nil {
					e = os.WriteFile(p, []byte{0, 255, 10}, 0600)
				}
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = observeRetirementManifest(ctx, d, a); e == nil && kind != "replaced-inode" {
				t.Fatal("unsafe observation accepted")
			}
			out, e := removeRecordedManifest(ctx, root, parent, m)
			if e == nil || out.Attempted || out.Complete {
				t.Fatalf("unsafe removal %+v %v", out, e)
			}
			if _, e = os.Lstat(filepath.Join(d, ".git")); e != nil {
				t.Fatal("initial refusal removed metadata")
			}
		})
	}
}
func TestRetirementManifestRejectsAdmissionTraversal(t *testing.T) {
	for _, p := range []string{"../outside", "/absolute", "dir/../data", "dir//data", ".", "x\x00y"} {
		t.Run(fmt.Sprintf("%q", p), func(t *testing.T) {
			d, a := manifestFixture(t)
			a.NativeMetadataPaths = append(a.NativeMetadataPaths, p)
			if _, e := observeRetirementManifest(context.Background(), d, a); e == nil {
				t.Fatal("invalid admission accepted")
			}
		})
	}
}

// Mutation happens after an earlier recorded unlink, independently of the
// implementation's phase counters. The remaining tree must be retained.
type retirementChangingContext struct {
	context.Context
	path, extra string
	changed     bool
}

func (c *retirementChangingContext) Err() error {
	if _, e := os.Lstat(c.path); os.IsNotExist(e) && !c.changed {
		c.changed = true
		_ = os.WriteFile(c.extra, []byte("new owner payload"), 0600)
	}
	return nil
}
func TestRetirementManifestPartialRetainsLatePayload(t *testing.T) {
	d, a := manifestFixture(t)
	m, e := observeRetirementManifest(context.Background(), d, a)
	if e != nil {
		t.Fatal(e)
	}
	root, _ := currentIdentity(d)
	parent, _ := currentIdentity(filepath.Dir(d))
	ctx := &retirementChangingContext{Context: context.Background(), path: filepath.Join(d, "dir", "data"), extra: filepath.Join(d, "dir", "late")}
	out, e := removeRecordedManifest(ctx, root, parent, m)
	if e == nil || !out.Attempted || out.Complete || len(out.Effects) != 1 {
		t.Fatalf("partial effect lost: %+v %v", out, e)
	}
	b, e := os.ReadFile(ctx.extra)
	if e != nil || string(b) != "new owner payload" {
		t.Fatal("late payload removed")
	}
	if _, e = os.Lstat(filepath.Join(d, ".git")); e != nil {
		t.Fatal("continued after late payload")
	}
}
func TestRetirementManifestRejectsBoundsAndAliases(t *testing.T) {
	for _, kind := range []string{"oversize", "casealias", "executable", "metadata-prefix", "hardlink-outside", "replaced-clean-inode", "parent-replaced"} {
		t.Run(kind, func(t *testing.T) {
			d, a := manifestFixture(t)
			m, e := observeRetirementManifest(context.Background(), d, a)
			if e != nil {
				t.Fatal(e)
			}
			root, _ := currentIdentity(d)
			parent, _ := currentIdentity(filepath.Dir(d))
			p := filepath.Join(d, "dir", "data")
			switch kind {
			case "oversize":
				e = os.WriteFile(p, make([]byte, retirementFileBytes+1), 0600)
			case "casealias":
				e = os.WriteFile(filepath.Join(d, "dir", "DATA"), nil, 0600)
			case "executable":
				e = os.Chmod(p, 0700)
			case "metadata-prefix":
				e = os.Mkdir(filepath.Join(d, ".jj"), 0700)
				if e == nil {
					e = os.WriteFile(filepath.Join(d, ".jj", "unexpected"), nil, 0600)
				}
				a.NativeMetadataPaths = append(a.NativeMetadataPaths, ".jj")
			case "hardlink-outside":
				e = os.Link(p, filepath.Join(filepath.Dir(d), "outside-link"))
				t.Cleanup(func() { _ = os.Remove(filepath.Join(filepath.Dir(d), "outside-link")) })
			case "replaced-clean-inode":
				e = os.Rename(p, p+"old")
				if e == nil {
					e = os.WriteFile(p, []byte{0, 255, 10}, 0600)
				}
				if e == nil {
					e = os.Remove(p + "old")
				}
			case "parent-replaced":
				parent.Inode++
			}
			if e != nil {
				t.Fatal(e)
			}
			if kind != "replaced-clean-inode" && kind != "parent-replaced" {
				if _, e = observeRetirementManifest(context.Background(), d, a); e == nil {
					t.Fatal("unsafe observation accepted")
				}
			}
			out, e := removeRecordedManifest(context.Background(), root, parent, m)
			if e == nil || out.Attempted {
				t.Fatalf("stale removal %+v %v", out, e)
			}
		})
	}
}

func TestRetirementManifestEntryBoundAndRootReplacement(t *testing.T) {
	t.Run("entry-bound", func(t *testing.T) {
		d, a := manifestFixture(t)
		for i := 0; i < retirementManifestEntries; i++ {
			if e := os.WriteFile(filepath.Join(d, fmt.Sprintf("extra-%04d", i)), nil, 0600); e != nil {
				t.Fatal(e)
			}
		}
		if _, e := observeRetirementManifest(context.Background(), d, a); e == nil {
			t.Fatal("entry overflow accepted")
		}
	})
	t.Run("root-replacement", func(t *testing.T) {
		d, a := manifestFixture(t)
		m, e := observeRetirementManifest(context.Background(), d, a)
		if e != nil {
			t.Fatal(e)
		}
		id, _ := currentIdentity(d)
		parent, _ := currentIdentity(filepath.Dir(d))
		old := d + "-original"
		if e = os.Rename(d, old); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = os.RemoveAll(old) })
		if e = os.Mkdir(d, 0700); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(d, "sentinel"), []byte("new owner"), 0600); e != nil {
			t.Fatal(e)
		}
		out, e := removeRecordedManifest(context.Background(), id, parent, m)
		if e == nil || out.Attempted {
			t.Fatalf("replaced root %+v %v", out, e)
		}
		for _, p := range []string{filepath.Join(old, "dir", "data"), filepath.Join(d, "sentinel")} {
			if _, e = os.Lstat(p); e != nil {
				t.Fatal("root or sibling damaged")
			}
		}
	})
}
