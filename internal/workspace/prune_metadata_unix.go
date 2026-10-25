// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"golang.org/x/sys/unix"
)

const pruneMetadataFileLimit int64 = 64 << 20
const pruneMetadataTotalLimit int64 = 256 << 20
const pruneMetadataEntryLimit = 1024

type pruneMetadataLimits struct {
	File, Total int64
	Entries     int
}

func defaultPruneMetadataLimits() pruneMetadataLimits {
	return pruneMetadataLimits{pruneMetadataFileLimit, pruneMetadataTotalLimit, pruneMetadataEntryLimit}
}

type pruneContextReader struct {
	context.Context
	io.Reader
}

func (r pruneContextReader) Read(p []byte) (int, error) {
	if err := r.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

// This is an inventory/digest reader, not a recovery payload. It writes no
// sidecars and cannot establish recovery-confirmed or authorize removal.
func observePruneMetadata(ctx context.Context, path string, limits pruneMetadataLimits) ([]pruneMetadataEntry, int64, error) {
	root, _, err := openCanonical(path)
	if err != nil {
		return nil, 0, failure("prune-metadata-refused", "Registration metadata root is unavailable or aliased.")
	}
	defer root.Close()
	allowed := map[string]bool{}
	for _, p := range gitRetirementMetadataPaths() {
		allowed[p] = true
	}
	allowed["config.worktree"] = true
	entries := []pruneMetadataEntry{}
	seen := map[string]bool{}
	var total int64
	refuse := func() error {
		return failure("prune-metadata-refused", "Registration has unknown, changed, linked or unsupported metadata.")
	}
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil || name != "." && !allowed[name] {
			return refuse()
		}
		if len(entries) >= limits.Entries {
			return failure("prune-metadata-limit", "Registration metadata entry limit exceeded.")
		}
		before, err := root.Lstat(name)
		if err != nil {
			return refuse()
		}
		wantDirectory := name == "." || name == "logs" || name == "refs"
		if before.IsDir() != wantDirectory {
			return refuse()
		}
		seen[name] = true
		physical, err := retirementV3Physical(filepath.Join(path, name), before)
		if err != nil {
			return refuse()
		}
		entry := pruneMetadataEntry{Path: name, Kind: physical.Kind, Identity: PathIdentity{physical.Path, physical.Device, physical.Inode}, Mode: physical.Mode, Size: before.Size(), Physical: physical}
		if before.IsDir() {
			entry.Size = 0
			entries = append(entries, entry)
			return nil
		}
		if before.Size() > limits.File || before.Size() > limits.Total-total {
			return failure("prune-metadata-limit", "Registration metadata byte limit exceeded.")
		}
		pruneObservationStep(ctx, "metadata-before-open", filepath.Join(path, name), nil)
		file, err := root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return refuse()
		}
		opened, err := file.Stat()
		if err != nil || !os.SameFile(before, opened) {
			file.Close()
			return failure("prune-metadata-refused", "Registration metadata identity changed before reading.")
		}
		pruneObservationStep(ctx, "metadata-before-read", filepath.Join(path, name), nil)
		hash := sha256.New()
		buffer := make([]byte, 64<<10)
		n, readErr := io.CopyBuffer(hash, io.LimitReader(pruneContextReader{ctx, file}, limits.File+1), buffer)
		pruneObservationStep(ctx, "metadata-after-read", filepath.Join(path, name), nil)
		after, statErr := file.Stat()
		file.Close()
		if readErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return refuse()
		}
		if statErr != nil || n != before.Size() {
			return failure("prune-metadata-refused", "Registration metadata size changed while reading.")
		}
		fresh, err := retirementV3Physical(physical.Path, after)
		if err != nil || !reflect.DeepEqual(fresh, physical) {
			return failure("prune-metadata-refused", "Registration metadata physical facts changed while reading.")
		}
		if n > limits.File || n > limits.Total-total {
			return failure("prune-metadata-limit", "Registration metadata byte limit exceeded.")
		}
		entry.Digest = hex.EncodeToString(hash.Sum(nil))
		total += n
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	for name := range allowed {
		if !seen[name] {
			entries = append(entries, pruneMetadataEntry{Path: name, Kind: "absent"})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, total, nil
}
