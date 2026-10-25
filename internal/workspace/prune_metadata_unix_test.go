// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
)

func TestPruneMetadataLimitsAndExactBoundaries(t *testing.T) {
	if got := defaultPruneMetadataLimits(); got != (pruneMetadataLimits{64 << 20, 256 << 20, 1024}) {
		t.Fatalf("production bounds changed: %+v", got)
	}
	for _, kind := range []string{"file", "total", "entries"} {
		for _, over := range []bool{false, true} {
			t.Run(kind+map[bool]string{false: "_N", true: "_N_plus_one"}[over], func(t *testing.T) {
				root := gitTempDir(t)
				payload := bytes.Repeat([]byte{0, 255, 128, 7}, 8)
				if err := os.WriteFile(filepath.Join(root, "index"), payload, 0600); err != nil {
					t.Fatal(err)
				}
				limits := pruneMetadataLimits{File: 32, Total: 32, Entries: 2}
				if over {
					switch kind {
					case "file":
						limits.File--
					case "total":
						limits.Total--
					case "entries":
						limits.Entries--
					}
				}
				entries, total, err := observePruneMetadata(context.Background(), root, limits)
				if over {
					assertPruneFailure(t, err, "prune-metadata-limit")
					return
				}
				if err != nil || total != 32 {
					t.Fatalf("N refused: %d %v", total, err)
				}
				found, absent := false, false
				for _, entry := range entries {
					if entry.Path == "index" && entry.Digest == gitDigest(string(payload)) && entry.Size == 32 {
						found = true
					}
					if entry.Path == "locked" && entry.Kind == "absent" {
						absent = true
					}
				}
				if !found || !absent {
					t.Fatal("binary bytes or optional absence omitted")
				}
			})
		}
	}
}
func TestPruneMetadataRefusesFIFOsAndWrongKinds(t *testing.T) {
	for _, kind := range []string{"fifo", "directory_as_index", "file_as_logs"} {
		t.Run(kind, func(t *testing.T) {
			root := gitTempDir(t)
			var err error
			switch kind {
			case "fifo":
				err = unix.Mkfifo(filepath.Join(root, "index"), 0600)
			case "directory_as_index":
				err = os.Mkdir(filepath.Join(root, "index"), 0700)
			case "file_as_logs":
				err = os.WriteFile(filepath.Join(root, "logs"), []byte("retain"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = observePruneMetadata(context.Background(), root, defaultPruneMetadataLimits())
			assertPruneFailure(t, err, "prune-metadata-refused")
		})
	}
}
func TestPruneMetadataReadsIndexBeyondLegacyTextBound(t *testing.T) {
	root := gitTempDir(t)
	payload := bytes.Repeat([]byte{0, 255, 128, 7}, 32769)
	if err := os.WriteFile(filepath.Join(root, "index"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	entries, total, err := observePruneMetadata(context.Background(), root, defaultPruneMetadataLimits())
	if err != nil || total != int64(len(payload)) {
		t.Fatalf("binary index rejected by legacy text quota: %d %v", total, err)
	}
	for _, entry := range entries {
		if entry.Path == "index" {
			if entry.Digest != gitDigest(string(payload)) {
				t.Fatal("large binary index digest changed")
			}
			return
		}
	}
	t.Fatal("index omitted")
}
