// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetirementV3LargeBinaryContent(t *testing.T) {
	d, a := manifestFixture(t)
	b := bytes.Repeat([]byte{0, 255, '\n', 13}, 75280)[:301117]
	if e := os.WriteFile(filepath.Join(d, "dir", "data"), b, 0600); e != nil {
		t.Fatal(e)
	}
	a.StoredFiles[0].Size = int64(len(b))
	a.StoredFiles[0].Digest = fmt.Sprintf("%x", sha256.Sum256(b))
	budget := newTidyV3ProofBudget(false)
	if e := budget.beginObservation(); e != nil {
		t.Fatal(e)
	}
	if _, e := observeRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget); e != nil {
		t.Fatalf("Git v3 complete 301117-byte stored content must be admitted: %v", e)
	}
}

func TestRetirementContentPolicySelection(t *testing.T) {
	for _, backend := range []string{"git", "jj", "unknown", ""} {
		for _, version := range []int{0, 1, 2, 3, 4} {
			p, e := retirementPolicy(backend, version)
			valid := (backend == "git" || backend == "jj") && version == 2 || backend == "git" && version == 3
			if (e == nil) != valid {
				t.Fatalf("%q/%d policy=%d error=%v", backend, version, p, e)
			}
		}
	}
	if _, e := retirementContentPolicy(0).fileLimit(false); e == nil {
		t.Fatal("zero policy accepted")
	}
	for _, backend := range []string{"git", "jj"} {
		d, a := manifestFixture(t)
		a.Backend = backend
		b := bytes.Repeat([]byte("x"), (64<<10)+1)
		os.WriteFile(filepath.Join(d, "dir", "data"), b, 0600)
		a.StoredFiles[0].Size = int64(len(b))
		a.StoredFiles[0].Digest = fmt.Sprintf("%x", sha256.Sum256(b))
		if _, e := observeRetirementManifest(context.Background(), d, a); e == nil {
			t.Fatalf("old %s manifest accepted >64KiB", backend)
		}
	}
}

type retirementV3ActorContext struct {
	context.Context
	budget *tidyV3ProofBudget
	after  int64
	mutate func()
	done   bool
}

func (c *retirementV3ActorContext) Err() error {
	if !c.done && c.budget.snapshot().UserBytes >= c.after {
		c.done = true
		c.mutate()
	}
	return c.Context.Err()
}

func TestRetirementV3FilePostReadActors(t *testing.T) {
	for _, actor := range []string{"same-size-restored-mtime", "same-bytes-restored-mtime", "chmod", "hardlink", "symlink", "replacement", "growth", "truncation"} {
		t.Run(actor, func(t *testing.T) {
			d, _ := manifestFixture(t)
			p := filepath.Join(d, "dir", "data")
			st, e := os.Lstat(p)
			if e != nil {
				t.Fatal(e)
			}
			want, e := retirementV3Physical(p, st)
			if e != nil {
				t.Fatal(e)
			}
			root, e := os.OpenRoot(filepath.Dir(p))
			if e != nil {
				t.Fatal(e)
			}
			defer root.Close()
			budget := newTidyV3ProofBudget(false)
			budget.beginObservation()
			budget.beginUserPass()
			ctx := &retirementV3ActorContext{Context: context.Background(), budget: budget, after: st.Size(), mutate: func() {
				var e error
				switch actor {
				case "same-size-restored-mtime":
					e = os.WriteFile(p, []byte{1, 2, 3}, 0600)
					if e == nil {
						e = os.Chtimes(p, st.ModTime(), st.ModTime())
					}
				case "same-bytes-restored-mtime":
					e = os.WriteFile(p, []byte{0, 255, 10}, 0600)
					if e == nil {
						e = os.Chtimes(p, st.ModTime(), st.ModTime())
					}
				case "growth":
					e = os.WriteFile(p, []byte{0, 255, 10, 1}, 0600)
				case "truncation":
					e = os.Truncate(p, 2)
				case "chmod":
					e = os.Chmod(p, 0700)
				case "hardlink":
					e = os.Link(p, filepath.Join(d, "actor-link"))
				case "symlink":
					e = os.Rename(p, p+"-actor")
					if e == nil {
						e = os.Symlink(p+"-actor", p)
					}
				case "replacement":
					e = os.Rename(p, p+"-actor")
					if e == nil {
						e = os.WriteFile(p, []byte{0, 255, 10}, 0600)
					}
				}
				if e != nil {
					t.Fatal(e)
				}
			}}
			if _, e = readRetirementV3File(ctx, root, "data", want, false, budget); e == nil {
				t.Fatal("late actor accepted")
			}
			if !ctx.done {
				t.Fatal("actor did not execute after all content bytes")
			}
			if _, e = os.Lstat(p); e != nil {
				t.Fatal("actor payload removed")
			}
			if _, e = os.Stat(filepath.Join(d, ".git")); e != nil {
				t.Fatal("metadata removed")
			}
		})
	}
}

func TestRetirementV3Bounds(t *testing.T) {
	for _, size := range []int64{16 << 20, (16 << 20) + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			d, a := manifestFixture(t)
			p := filepath.Join(d, "dir", "data")
			f, e := os.OpenFile(p, os.O_TRUNC|os.O_WRONLY, 0600)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.Truncate(size); e != nil {
				t.Fatal(e)
			}
			f.Close()
			h := sha256.New()
			zeros := make([]byte, 64<<10)
			for n := size; n > 0; {
				k := min(n, int64(len(zeros)))
				h.Write(zeros[:k])
				n -= k
			}
			a.StoredFiles[0].Size = size
			a.StoredFiles[0].Digest = fmt.Sprintf("%x", h.Sum(nil))
			budget := newTidyV3ProofBudget(false)
			budget.beginObservation()
			_, e = observeRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget)
			if (e == nil) != (size == 16<<20) {
				t.Fatalf("size=%d error=%v", size, e)
			}
		})
	}
	for _, size := range []int{64 << 10, (64 << 10) + 1} {
		t.Run(fmt.Sprintf("metadata-%d", size), func(t *testing.T) {
			d, a := manifestFixture(t)
			if e := os.WriteFile(filepath.Join(d, ".git"), bytes.Repeat([]byte("m"), size), 0600); e != nil {
				t.Fatal(e)
			}
			budget := newTidyV3ProofBudget(false)
			budget.beginObservation()
			_, e := observeRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget)
			if (e == nil) != (size == 64<<10) {
				t.Fatalf("metadata size=%d error=%v", size, e)
			}
		})
	}
}

func TestRetirementV3AggregateEntriesAndUserPassOverrun(t *testing.T) {
	d := gitTempDir(t)
	a := retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{".git"}}
	if e := os.WriteFile(filepath.Join(d, ".git"), nil, 0600); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 1023; i++ {
		name := fmt.Sprintf("entry-%04d", i)
		if e := os.WriteFile(filepath.Join(d, name), nil, 0600); e != nil {
			t.Fatal(e)
		}
		a.StoredFiles = append(a.StoredFiles, RetirementStoredFile{Path: name, Digest: fmt.Sprintf("%x", sha256.Sum256(nil))})
	}
	budget := newTidyV3ProofBudget(false)
	budget.beginObservation()
	m, e := observeRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget)
	if e != nil || len(m.Entries) != 1024 {
		t.Fatalf("exact1024 aggregate entries refused: %v", e)
	}
	a.StoredFiles = append(a.StoredFiles, RetirementStoredFile{Path: "one-too-many", Digest: fmt.Sprintf("%x", sha256.Sum256(nil))})
	if _, e = observeRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget); e == nil {
		t.Fatal("aggregate1025 admitted")
	}
	a = retirementManifestAdmission{Backend: "git", NativeMetadataPaths: []string{".git"}}
	for i := 0; i < 4; i++ {
		a.StoredFiles = append(a.StoredFiles, RetirementStoredFile{Path: fmt.Sprint(i), Size: 16 << 20, Digest: fmt.Sprintf("%064x", 0)})
	}
	a.StoredFiles = append(a.StoredFiles, RetirementStoredFile{Path: "over", Size: 1, Digest: fmt.Sprintf("%064x", 0)})
	if _, e = observeRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget); e == nil {
		t.Fatal("64MiB plus one user byte admitted")
	}
}

func TestRetirementV3EncodedManifestExactBoundary(t *testing.T) {
	smallV3RecordLimit(t)
	root := "/fixture"
	m := V3Manifest{Complete: true, Entries: []V3ManifestEntry{}}
	for i := 0; i < 1024; i++ {
		m.Entries = append(m.Entries, V3ManifestEntry{Identity: V3PhysicalIdentity{Path: fmt.Sprintf("%s/file-%04d", root, i), Device: 1, Inode: uint64(i + 1), UID: uint64(os.Geteuid()), OwnerKnown: true, Kind: "regular", Mode: 0600, Nlink: 1, TimestampPrecision: "nanosecond"}, Digest: fmt.Sprintf("%064x", 0)})
	}
	var e error
	m.Digest, e = v3RecordDigest(m.Entries)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	padding := (1 << 20) - len(b)
	for i := range m.Entries {
		n := min(padding, 3000)
		m.Entries[i].Identity.Path += strings.Repeat("x", n)
		padding -= n
	}
	if padding != 0 {
		t.Fatal("boundary fixture padding did not fit")
	}
	m.Digest, e = v3RecordDigest(m.Entries)
	if e != nil {
		t.Fatal(e)
	}
	b, _ = json.Marshal(m)
	if len(b) != 1<<20 {
		t.Fatalf("encoded boundary %d", len(b))
	}
	if e = validateRetirementV3Manifest(m, root, false, retirementGitV3Content); e != nil {
		t.Fatalf("exact1MiB record refused: %v", e)
	}
	m.Entries[len(m.Entries)-1].Identity.Path += "x"
	m.Digest, e = v3RecordDigest(m.Entries)
	if e != nil {
		t.Fatal(e)
	}
	if e = validateRetirementV3Manifest(m, root, false, retirementGitV3Content); e == nil {
		t.Fatal("encoded1MiB plus one admitted")
	}
}

func TestRetirementV3RecordedManifestRefusesActorChanges(t *testing.T) {
	for _, actor := range []string{"directory-replacement", "metadata-replacement", "unknown-empty-directory", "unknown-ignored-file", "metadata-directory", "extra-metadata-admission"} {
		t.Run(actor, func(t *testing.T) {
			d, a := manifestFixture(t)
			budget := newTidyV3ProofBudget(false)
			budget.beginObservation()
			m, e := observeRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget)
			if e != nil {
				t.Fatal(e)
			}
			switch actor {
			case "directory-replacement":
				e = os.Rename(filepath.Join(d, "dir"), filepath.Join(d, "actor-dir"))
				if e == nil {
					e = os.Mkdir(filepath.Join(d, "dir"), 0700)
				}
			case "metadata-replacement":
				e = os.Rename(filepath.Join(d, ".git"), filepath.Join(d, "actor-pointer"))
				if e == nil {
					e = os.WriteFile(filepath.Join(d, ".git"), []byte("gitdir: exact\n"), 0600)
				}
			case "unknown-empty-directory":
				e = os.Mkdir(filepath.Join(d, "extra"), 0700)
			case "unknown-ignored-file":
				e = os.WriteFile(filepath.Join(d, "ignored-cache"), []byte("actor cache"), 0600)
			case "metadata-directory":
				e = os.Remove(filepath.Join(d, ".git"))
				if e == nil {
					e = os.Mkdir(filepath.Join(d, ".git"), 0700)
				}
			case "extra-metadata-admission":
				a.NativeMetadataPaths = append(a.NativeMetadataPaths, "ignored-cache")
				e = os.WriteFile(filepath.Join(d, "ignored-cache"), []byte("actor cache"), 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = verifyRecordedRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget, m); e == nil {
				t.Fatal("recorded manifest accepted changed actor state")
			}
			if _, e = os.Lstat(d); e != nil {
				t.Fatal("target root removed")
			}
		})
	}
}

func TestRetirementV3PhysicalReadBudgetAndCancellation(t *testing.T) {
	for _, kind := range []string{"budget", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			d, a := manifestFixture(t)
			budget := newTidyV3ProofBudget(false)
			budget.beginObservation()
			ctx := context.Background()
			if kind == "budget" {
				budget.limits[proofUser] = 2
			} else {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, e := observeRetirementV3Manifest(ctx, d, a, false, retirementGitV3Content, budget); e == nil {
				t.Fatal("exhausted proof accepted")
			}
			c := budget.snapshot()
			if c.UserBytes > 3 || c.MaxReadBuffer > 64<<10 {
				t.Fatalf("unbounded failed read %+v", c)
			}
			got, e := os.ReadFile(filepath.Join(d, "dir", "data"))
			if e != nil || !bytes.Equal(got, []byte{0, 255, 10}) {
				t.Fatal("refusal changed actor bytes")
			}
		})
	}
}
