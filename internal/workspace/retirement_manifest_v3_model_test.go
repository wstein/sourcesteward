// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestRetirementV3ExactUserPassExcludesPointerMetadata(t *testing.T) {
	d, a := manifestFixture(t)
	a.StoredFiles = nil
	h := sha256.New()
	zeros := make([]byte, 64<<10)
	for i := 0; i < 256; i++ {
		h.Write(zeros)
	}
	digest := fmt.Sprintf("%x", h.Sum(nil))
	for i := 0; i < 4; i++ {
		name := fmt.Sprintf("payload-%d", i)
		p := filepath.Join(d, name)
		f, e := os.Create(p)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.Truncate(16 << 20); e != nil {
			t.Fatal(e)
		}
		f.Close()
		a.StoredFiles = append(a.StoredFiles, RetirementStoredFile{Path: name, Size: 16 << 20, Digest: digest})
	}
	if e := os.RemoveAll(filepath.Join(d, "dir")); e != nil {
		t.Fatal(e)
	}
	budget := newTidyV3ProofBudget(false)
	budget.beginObservation()
	if _, e := observeRetirementV3Manifest(context.Background(), d, a, false, retirementGitV3Content, budget); e != nil {
		t.Fatalf("exact64MiB user content plus separately bounded pointer metadata must pass: %v", e)
	}
}

func TestRetirementV3CentralPointerAccounting(t *testing.T) {
	root := "/fixture"
	newManifest := func(extra string, size int64) V3Manifest {
		m := V3Manifest{Complete: true, Entries: []V3ManifestEntry{}}
		for i := 0; i < 4; i++ {
			m.Entries = append(m.Entries, V3ManifestEntry{Identity: V3PhysicalIdentity{Path: fmt.Sprintf("%s/user-%d", root, i), Inode: uint64(i + 1), UID: uint64(os.Geteuid()), OwnerKnown: true, Kind: "regular", Mode: 0600, Nlink: 1, Size: 16 << 20, TimestampPrecision: "nanosecond"}, Digest: fmt.Sprintf("%064x", 0)})
		}
		m.Entries = append(m.Entries, V3ManifestEntry{Identity: V3PhysicalIdentity{Path: filepath.Join(root, extra), Inode: 10, UID: uint64(os.Geteuid()), OwnerKnown: true, Kind: "regular", Mode: 0600, Nlink: 1, Size: size, TimestampPrecision: "nanosecond"}, Digest: fmt.Sprintf("%064x", 0)})
		sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Identity.Path < m.Entries[j].Identity.Path })
		m.Digest, _ = v3RecordDigest(m.Entries)
		return m
	}
	for _, tc := range []struct {
		name, path     string
		size           int64
		metadata, want bool
	}{
		{"exact-user-plus-pointer", ".git", 64 << 10, false, true},
		{"oversized-pointer", ".git", (64 << 10) + 1, false, false},
		{"extra-user-byte", "ordinary", 1, false, false},
		{"lookalike-pointer", ".git-other", 1, false, false},
		{"nested-pointer", "nested/.git", 1, false, false},
		{"registration-total-unchanged", ".git", 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validV3Manifest(newManifest(tc.path, tc.size), root, tc.metadata); got != tc.want {
				t.Fatalf("valid=%t want=%t", got, tc.want)
			}
		})
	}
}
