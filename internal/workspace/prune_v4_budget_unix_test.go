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
