// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistrationPruneV4HistoryRechecksSameMetadataFileIdentity(t *testing.T) {
	for _, kind := range []string{"plans", "journals"} {
		t.Run(kind, func(t *testing.T) {
			p := pruneV4HistoryFixture(t)
			path := filepath.Join(p.StateDir, pruneV4Namespace, kind, p.PlanID+".json")
			// Hold the original inode through replacement to exclude inode reuse.
			held, e := os.Open(path)
			pruneMust(t, e)
			defer held.Close()
			before, e := held.Stat()
			pruneMust(t, e)
			data, e := os.ReadFile(path)
			pruneMust(t, e)
			fired := false
			c := pruneV4HistoryCoordinator{scanLimit: 4096, scanBytes: 4 << 20, outputBytes: 4 << 20, afterRead: func() {
				fired = true
				replacement := path + ".replacement"
				pruneMust(t, os.WriteFile(replacement, data, 0600))
				pruneMust(t, os.Chtimes(replacement, before.ModTime(), before.ModTime()))
				pruneMust(t, os.Rename(replacement, path))
				after, e := os.Stat(path)
				pruneMust(t, e)
				actual, e := os.ReadFile(path)
				pruneMust(t, e)
				if os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || before.Mode() != after.Mode() || !bytes.Equal(data, actual) {
					t.Fatal("fixture must change only record identity, preserving bytes, size, mode and mtime")
				}
			}}
			r := c.history(context.Background(), RegistrationPruneV4HistoryOptions{Path: p.SourcePath, StateDir: p.StateDir})
			if !fired {
				t.Fatal("after-read history hook was not reached")
			}
			if r.Status != "refused" || len(r.Entries) != 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "changed-state" || r.Diagnostics[0].Message != "Stored contract-4 history record changed during listing." {
				t.Fatalf("want refused/changed-state for replaced identity, got %+v", r)
			}
		})
	}
}
