// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistrationPruneV4RemovalBudgetBeforeFirstUnlink(t *testing.T) {
	_, p := fixturePruneV4(t)
	base, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	measured := false
	ctx := context.WithValue(base, pruneObservationHookKey{}, pruneObservationHook(func(stage, path string, value any) {
		if stage == "v4-removal-budget" {
			measured = true
			// Model a slow full verification pass without sleeping or timing a disk.
			*value.(*time.Duration) = 10 * time.Second
		}
	}))
	r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if !measured || r.Status != "partial" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "prune-metadata-limit" {
		t.Fatalf("deadline admission: measured=%v result=%+v", measured, r)
	}
	for _, m := range p.Expected.Metadata {
		if m.Kind == "file" {
			b, e := os.ReadFile(filepath.Join(p.Registration.GitDir, m.Path))
			if e != nil || gitDigest(string(b)) != m.Digest {
				t.Fatalf("unlink before budget admission: %s %v", m.Path, e)
			}
		}
	}
}

// Exercise the actual byte quotas, not reduced test-only limits. The measured
// cost seam models a slow verification pass without timing the host's disk.
func TestRegistrationPruneV4RemovalBudgetAtAcceptedByteLimits(t *testing.T) {
	opts, p := fixturePruneV4(t)
	large := []string{"index", "logs/HEAD", "ORIG_HEAD", "config.worktree"}
	otherBytes := p.Expected.MetadataBytes
	for _, m := range p.Expected.Metadata {
		for _, name := range large {
			if m.Path == name {
				otherBytes -= m.Size
			}
		}
	}
	for i, name := range large {
		size := pruneMetadataFileLimit
		if i == len(large)-1 {
			size -= otherBytes
		}
		if size <= 0 {
			t.Fatal("fixture metadata exceeds the exact-limit layout")
		}
		path := filepath.Join(p.Registration.GitDir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			t.Fatal(err)
		}
		err = f.Truncate(size)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("large metadata: truncate=%v close=%v", err, closeErr)
		}
	}
	planned := PlanRegistrationPruneV4(context.Background(), opts)
	if planned.Status != "ok" || planned.Plan == nil {
		t.Fatalf("exact byte-limit plan: %+v", planned)
	}
	p = *planned.Plan
	if p.Expected.MetadataBytes != pruneMetadataTotalLimit {
		t.Fatalf("fixture total %d, want %d", p.Expected.MetadataBytes, pruneMetadataTotalLimit)
	}
	maxFile := int64(0)
	for _, m := range p.Expected.Metadata {
		if m.Size > maxFile {
			maxFile = m.Size
		}
	}
	if maxFile != pruneMetadataFileLimit {
		t.Fatalf("fixture max file %d, want %d", maxFile, pruneMetadataFileLimit)
	}
	measured := false
	ctx := pruneHookContext(func(stage, path string, value any) {
		if stage == "v4-removal-budget" {
			measured = true
			*value.(*time.Duration) = 10 * time.Second
		}
	})
	r := ApplyRegistrationPruneV4(ctx, RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode})
	if !measured || r.Status != "partial" || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "prune-metadata-limit" {
		t.Fatalf("size-limit budget: measured=%v result=%+v", measured, r)
	}
	actual, total, err := observePruneMetadata(context.Background(), p.Registration.GitDir, defaultPruneMetadataLimits())
	if err != nil || total != pruneMetadataTotalLimit {
		t.Fatalf("registration changed: total=%d error=%v", total, err)
	}
	for i, m := range actual {
		if m.Kind == "regular" {
			m.Kind = "file"
		}
		want := p.Expected.Metadata[i]
		if m.Path != want.Path || m.Kind != want.Kind || m.Digest != want.Digest || m.Physical != want.Physical {
			t.Fatalf("metadata changed at %s: %+v want %+v", m.Path, m, want)
		}
	}
	manifest, digest, err := readPruneV4Payload(context.Background(), p)
	if err != nil || digest == "" || manifest.PlanDigest != p.Digest {
		t.Fatalf("exact-limit recovery lost: %v", err)
	}
}

func TestRegistrationPruneV4RemovalBudgetAtEntryLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := pruneV4RemovalBudget(ctx, time.Millisecond, pruneMetadataEntryLimit); err != nil {
		t.Fatalf("entry-limit budget should fit: %v", err)
	}
	for _, tc := range []struct {
		cost    time.Duration
		entries int
	}{
		{10 * time.Millisecond, pruneMetadataEntryLimit},
		{0, pruneMetadataEntryLimit + 1},
	} {
		err := pruneV4RemovalBudget(ctx, tc.cost, tc.entries)
		assertPruneReason(t, err, "prune-metadata-limit", "Remaining deadline cannot cover measured full registration verification before removal; no metadata was unlinked.")
	}
}
