// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"golang.org/x/sys/unix"
)

type pruneV4PayloadEntry struct {
	Path          string  `json:"path"`
	Kind          string  `json:"kind"`
	Mode          *uint32 `json:"mode"`
	Size          int64   `json:"size"`
	ContentSHA256 *string `json:"content_sha256"`
}
type pruneV4Payload struct {
	SchemaVersion int                   `json:"schema_version"`
	Operation     string                `json:"operation"`
	PlanID        string                `json:"plan_id"`
	PlanDigest    string                `json:"plan_digest"`
	Entries       []pruneV4PayloadEntry `json:"entries"`
}

func pruneV4PayloadManifest(p RegistrationPruneV4Plan) pruneV4Payload {
	m := pruneV4Payload{4, "registration-prune", p.PlanID, p.Digest, []pruneV4PayloadEntry{}}
	for _, f := range p.Expected.Metadata {
		entry := pruneV4PayloadEntry{Path: f.Path, Kind: f.Kind, Size: f.Size}
		if f.Kind != "absent" {
			mode := f.Mode
			entry.Mode = &mode
		}
		if f.Kind == "file" {
			digest := f.Digest
			entry.ContentSHA256 = &digest
		}
		m.Entries = append(m.Entries, entry)
	}
	return m
}
func pruneV4PayloadError() error {
	return failure("prune-recovery-refused", "Complete private registration recovery could not be verified.")
}

// preparePruneV4Payload is called only under the shared state-store lock. It
// never removes partial evidence or makes a destructive effect eligible alone.
func preparePruneV4Payload(ctx context.Context, p RegistrationPruneV4Plan, s *store) (string, error) {
	return preparePruneV4PayloadWithLimits(ctx, p, s, defaultPruneMetadataLimits())
}

// The private limits parameter lets boundary tests exercise N and N+1 without
// allocating maximum-size native indexes. Production always uses the contract limits.
func preparePruneV4PayloadWithLimits(ctx context.Context, p RegistrationPruneV4Plan, s *store, limits pruneMetadataLimits) (string, error) {
	if e := s.validate(); e != nil {
		return "", e
	}
	parent := s.dirs["payloads"]
	if e := parent.Mkdir(p.PlanID, 0700); e != nil {
		return "", pruneV4PayloadError()
	}
	if e := parent.Chmod(p.PlanID, 0700); e != nil {
		return "", pruneV4PayloadError()
	}
	if e := syncDirectory(parent); e != nil {
		return "", pruneV4PayloadError()
	}
	dst, e := parent.OpenRoot(p.PlanID)
	if e != nil {
		return "", pruneV4PayloadError()
	}
	defer dst.Close()
	if e = dst.Mkdir("files", 0700); e != nil {
		return "", pruneV4PayloadError()
	}
	if e = dst.Chmod("files", 0700); e != nil {
		return "", pruneV4PayloadError()
	}
	files, e := dst.OpenRoot("files")
	if e != nil {
		return "", pruneV4PayloadError()
	}
	defer files.Close()
	src, id, e := openCanonical(p.Registration.GitDir)
	if e != nil || id != p.Expected.Registration {
		if src != nil {
			src.Close()
		}
		return "", pruneV4PayloadError()
	}
	defer src.Close()
	if len(p.Expected.Metadata) > limits.Entries {
		return "", failure("prune-metadata-limit", "Registration metadata entry limit exceeded.")
	}
	var total int64
	for _, m := range p.Expected.Metadata {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		switch m.Kind {
		case "absent":
			if _, e = src.Lstat(m.Path); !errors.Is(e, os.ErrNotExist) {
				return "", pruneV4PayloadError()
			}
		case "directory":
			if m.Path != "." {
				if e = files.Mkdir(m.Path, 0700); e != nil {
					return "", pruneV4PayloadError()
				}
				if e = files.Chmod(m.Path, 0700); e != nil {
					return "", pruneV4PayloadError()
				}
			}
		case "file":
			if m.Size < 0 || m.Size > limits.File || m.Size > limits.Total-total {
				return "", failure("prune-metadata-limit", "Registration metadata byte limit exceeded.")
			}
			before, e := src.Lstat(m.Path)
			if e != nil {
				return "", pruneV4PayloadError()
			}
			physical, e := retirementV3Physical(filepath.Join(p.Registration.GitDir, m.Path), before)
			if e != nil || !reflect.DeepEqual(physical, m.Physical) {
				return "", pruneV4PayloadError()
			}
			pruneObservationStep(ctx, "v4-payload-before-file", m.Path, nil)
			in, e := src.OpenFile(m.Path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if e != nil {
				return "", pruneV4PayloadError()
			}
			st, e := in.Stat()
			if e != nil || !os.SameFile(before, st) {
				in.Close()
				return "", pruneV4PayloadError()
			}
			out, e := files.OpenFile(m.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
			if e != nil {
				in.Close()
				return "", pruneV4PayloadError()
			}
			e = out.Chmod(0600)
			h := sha256.New()
			var n int64
			if e == nil {
				n, e = io.CopyBuffer(io.MultiWriter(out, h), io.LimitReader(pruneContextReader{ctx, in}, m.Size+1), make([]byte, 64<<10))
			}
			after, statErr := in.Stat()
			in.Close()
			if e == nil {
				e = out.Sync()
			}
			closeErr := out.Close()
			if e != nil || statErr != nil || closeErr != nil || n != m.Size || hex.EncodeToString(h.Sum(nil)) != m.Digest {
				return "", pruneV4PayloadError()
			}
			physical, e = retirementV3Physical(filepath.Join(p.Registration.GitDir, m.Path), after)
			pathInfo, pathErr := src.Lstat(m.Path)
			if e != nil || pathErr != nil || !os.SameFile(before, pathInfo) || !reflect.DeepEqual(physical, m.Physical) {
				return "", pruneV4PayloadError()
			}
			total += n
		default:
			return "", pruneV4PayloadError()
		}
	}
	if total != p.Expected.MetadataBytes {
		return "", pruneV4PayloadError()
	}
	// Flush every nested sidecar directory before publishing the manifest.
	for i := len(p.Expected.Metadata) - 1; i >= 0; i-- {
		m := p.Expected.Metadata[i]
		if m.Kind == "directory" {
			r, e := files.OpenRoot(m.Path)
			if e != nil {
				return "", pruneV4PayloadError()
			}
			e = syncDirectory(r)
			r.Close()
			if e != nil {
				return "", pruneV4PayloadError()
			}
		}
	}
	if e = syncDirectory(files); e != nil {
		return "", pruneV4PayloadError()
	}
	manifest := pruneV4PayloadManifest(p)
	if e = writePruneV4Record(dst, "manifest.json", manifest, true); e != nil {
		return "", e
	}
	if e = s.validate(); e != nil {
		return "", e
	}
	_, digest, e := readPruneV4Payload(ctx, p)
	return digest, e
}

func readPruneV4Payload(ctx context.Context, p RegistrationPruneV4Plan) (pruneV4Payload, string, error) {
	var zero pruneV4Payload
	dst, _, e := openCanonical(p.PayloadPath)
	if e != nil {
		return zero, "", pruneV4PayloadError()
	}
	defer dst.Close()
	info, e := dst.Stat(".")
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 || !privateOwner(info) {
		return zero, "", pruneV4PayloadError()
	}
	raw, e := readPruneV4PrivateFile(dst, "manifest.json", int64(v3RecordLimit))
	if e != nil {
		return zero, "", e
	}
	want := pruneV4PayloadManifest(p)
	encoded, e := pruneV4Canonical(want, "")
	if e != nil || !bytes.Equal(raw, append(encoded, '\n')) {
		return zero, "", pruneV4PayloadError()
	}
	files, e := dst.OpenRoot("files")
	if e != nil {
		return zero, "", pruneV4PayloadError()
	}
	defer files.Close()
	// Reject extra files, directories and aliases, including a substituted
	// files root. Recovery is the exact logical manifest, not a matching subset.
	listing, e := dst.Open(".")
	if e != nil {
		return zero, "", pruneV4PayloadError()
	}
	names, e := listing.ReadDir(4)
	listing.Close()
	if e != nil || len(names) != 2 {
		return zero, "", pruneV4PayloadError()
	}
	for _, name := range names {
		if name.Name() != "files" && name.Name() != "manifest.json" {
			return zero, "", pruneV4PayloadError()
		}
	}
	before, e := dst.Lstat("files")
	opened, se := files.Stat(".")
	if e != nil || se != nil || !before.IsDir() || !os.SameFile(before, opened) {
		return zero, "", pruneV4PayloadError()
	}
	allowed := map[string]bool{}
	for _, m := range want.Entries {
		if m.Kind != "absent" {
			allowed[m.Path] = true
		}
	}
	count := 0
	e = fs.WalkDir(files.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		count++
		if walkErr != nil || count > pruneMetadataEntryLimit || !allowed[name] || entry.Type()&os.ModeSymlink != 0 {
			return pruneV4PayloadError()
		}
		return nil
	})
	if e != nil {
		return zero, "", e
	}
	for _, m := range want.Entries {
		if e = ctx.Err(); e != nil {
			return zero, "", e
		}
		st, e := files.Lstat(m.Path)
		switch m.Kind {
		case "absent":
			if !errors.Is(e, os.ErrNotExist) {
				return zero, "", pruneV4PayloadError()
			}
		case "directory":
			if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 || !privateOwner(st) {
				return zero, "", pruneV4PayloadError()
			}
		case "file":
			if e != nil || !regularPrivate(st) || st.Size() != m.Size {
				return zero, "", pruneV4PayloadError()
			}
			f, e := files.OpenFile(m.Path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if e != nil {
				return zero, "", pruneV4PayloadError()
			}
			opened, e := f.Stat()
			if e != nil || !os.SameFile(st, opened) {
				f.Close()
				return zero, "", pruneV4PayloadError()
			}
			h := sha256.New()
			n, e := io.CopyBuffer(h, io.LimitReader(pruneContextReader{ctx, f}, m.Size+1), make([]byte, 64<<10))
			after, se := f.Stat()
			f.Close()
			current, ce := files.Lstat(m.Path)
			if e != nil || se != nil || ce != nil || n != m.Size || !os.SameFile(st, after) || !os.SameFile(st, current) || hex.EncodeToString(h.Sum(nil)) != *m.ContentSHA256 {
				return zero, "", pruneV4PayloadError()
			}
		}
	}
	digest, e := pruneV4Digest("stw-registration-payload-v4", want, "")
	return want, digest, e
}
func readPruneV4PrivateFile(root *os.Root, name string, limit int64) ([]byte, error) {
	before, e := root.Lstat(name)
	if e != nil || !regularPrivate(before) || before.Size() > limit {
		return nil, pruneV4PayloadError()
	}
	f, e := root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, pruneV4PayloadError()
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(before, opened) {
		return nil, pruneV4PayloadError()
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	after, se := f.Stat()
	current, ce := root.Lstat(name)
	if e != nil || se != nil || ce != nil || len(b) > int(limit) || int64(len(b)) != before.Size() || !os.SameFile(before, after) || !os.SameFile(before, current) {
		return nil, pruneV4PayloadError()
	}
	return b, nil
}
func writePruneV4Record(root *os.Root, name string, value any, exclusive bool) error {
	b, e := pruneV4Canonical(value, "")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	if len(b) > v3RecordLimit {
		return failure("record-limit", "Contract-4 record exceeds its encoded bound.")
	}
	target := name
	if !exclusive {
		id, e := randomID()
		if e != nil {
			return e
		}
		target = "tmp-" + id + ".json"
	}
	f, e := root.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return pruneV4PayloadError()
	}
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil || ce != nil {
		return pruneV4PayloadError()
	}
	if !exclusive {
		st, e := root.Lstat(name)
		if e != nil || !regularPrivate(st) {
			return pruneV4PayloadError()
		}
		if e = root.Rename(target, name); e != nil {
			return pruneV4PayloadError()
		}
	}
	if e = syncDirectory(root); e != nil {
		return pruneV4PayloadError()
	}
	return nil
}
