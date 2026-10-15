// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const retirementManifestEntries = 1024
const retirementFileBytes = 64 << 10

func retirementLiteralPath(p string) bool {
	return p != "" && p != "." && validText(p) && !filepath.IsAbs(p) && filepath.Clean(p) == p && !strings.Contains(p, "\\") && p != ".." && !strings.HasPrefix(p, "../")
}
func retirementManifestEntriesDigest(entries []RetirementEntry) (string, error) {
	b, e := json.Marshal(entries)
	if e != nil {
		return "", e
	}
	if len(b) > 1<<20 {
		return "", errors.New("retirement manifest exceeds encoded limit")
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}
func observeRetirementManifest(ctx context.Context, path string, a retirementManifestAdmission) (RetirementManifest, error) {
	if a.Backend != "git" && a.Backend != "jj" {
		return RetirementManifest{}, errors.New("unsupported manifest backend")
	}
	if len(a.NativeMetadataPaths)+len(a.StoredFiles) > retirementManifestEntries {
		return RetirementManifest{}, errors.New("manifest admission exceeds limit")
	}
	known := map[string]bool{}
	metadata := map[string]bool{}
	stored := map[string]RetirementStoredFile{}
	admit := func(p string) error {
		if !retirementLiteralPath(p) {
			return errors.New("unsafe literal manifest path")
		}
		known[p] = true
		for d := filepath.Dir(p); d != "."; d = filepath.Dir(d) {
			known[d] = true
		}
		return nil
	}
	for _, p := range a.NativeMetadataPaths {
		if metadata[p] {
			return RetirementManifest{}, errors.New("duplicate metadata admission")
		}
		if e := admit(p); e != nil {
			return RetirementManifest{}, e
		}
		metadata[p] = true
	}
	for _, f := range a.StoredFiles {
		if _, ok := stored[f.Path]; ok || metadata[f.Path] {
			return RetirementManifest{}, errors.New("duplicate stored admission")
		}
		if e := admit(f.Path); e != nil {
			return RetirementManifest{}, e
		}
		if len(f.Digest) != 64 || f.Size < 0 || f.Size > retirementFileBytes {
			return RetirementManifest{}, errors.New("invalid stored file fact")
		}
		stored[f.Path] = f
	}
	r, _, e := openCanonical(path)
	if e != nil {
		return RetirementManifest{}, e
	}
	defer r.Close()
	m, e := walkRetirementManifest(ctx, r)
	if e != nil {
		return RetirementManifest{}, e
	}
	seen := map[string]bool{}
	for _, entry := range m.Entries {
		if !known[entry.Path] {
			return RetirementManifest{}, errors.New("unknown physical retirement payload")
		}
		seen[entry.Path] = true
		if f, ok := stored[entry.Path]; ok {
			if entry.Kind != "regular" || entry.Digest != f.Digest || entry.Size != f.Size || entry.Executable != f.Executable {
				return RetirementManifest{}, errors.New("stored content differs from physical disk")
			}
		} else if !metadata[entry.Path] && entry.Kind != "directory" {
			return RetirementManifest{}, errors.New("unexpected physical file")
		}
	}
	for p := range metadata {
		if !seen[p] {
			return RetirementManifest{}, errors.New("admitted metadata is missing")
		}
	}
	for p := range stored {
		if !seen[p] {
			return RetirementManifest{}, errors.New("stored file is missing")
		}
	}
	return m, nil
}

// Each directory is opened separately after a no-follow identity check. All
// traversal stays beneath the pinned root, including during concurrent renames.
func walkRetirementManifest(ctx context.Context, root *os.Root) (RetirementManifest, error) {
	m := RetirementManifest{Entries: []RetirementEntry{}}
	aliases := map[string]string{}
	var walk func(*os.Root, string) error
	walk = func(dir *os.Root, prefix string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		f, e := dir.Open(".")
		if e != nil {
			return e
		}
		names, e := f.Readdirnames(retirementManifestEntries + 1)
		f.Close()
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}
		sort.Strings(names)
		for _, name := range names {
			if e := ctx.Err(); e != nil {
				return e
			}
			p := name
			if prefix != "" {
				p = prefix + "/" + name
			}
			if !retirementLiteralPath(p) {
				return errors.New("unsafe physical manifest path")
			}
			if len(m.Entries) >= retirementManifestEntries {
				return errors.New("physical manifest entry limit exceeded")
			}
			folded := strings.ToLower(norm.NFC.String(p))
			if old, ok := aliases[folded]; ok && old != p {
				return errors.New("aliased physical manifest names")
			}
			aliases[folded] = p
			st, e := dir.Lstat(name)
			if e != nil {
				return e
			}
			id, e := pathIdentity(p, st)
			if e != nil {
				return e
			}
			entry := RetirementEntry{Path: p, Device: id.Device, Inode: id.Inode, Mode: uint32(st.Mode())}
			if st.IsDir() {
				entry.Kind = "directory"
				child, e := dir.OpenRoot(name)
				if e != nil {
					return e
				}
				opened, e := child.Stat(".")
				if e != nil || !os.SameFile(st, opened) {
					child.Close()
					return errors.New("physical directory changed")
				}
				m.Entries = append(m.Entries, entry)
				e = walk(child, p)
				child.Close()
				if e != nil {
					return e
				}
			} else if st.Mode().IsRegular() {
				entry.Kind = "regular"
				entry.Size = st.Size()
				entry.Executable = st.Mode().Perm()&0111 != 0
				if !retirementSingleLink(st) || st.Size() > retirementFileBytes {
					return errors.New("hardlinked or oversized physical file")
				}
				f, e := dir.OpenFile(name, os.O_RDONLY|noFollow, 0)
				if e != nil {
					return e
				}
				opened, e := f.Stat()
				if e != nil || !os.SameFile(st, opened) || opened.Mode() != st.Mode() || opened.Size() != st.Size() {
					f.Close()
					return errors.New("physical file changed while opening")
				}
				b, e := io.ReadAll(io.LimitReader(f, retirementFileBytes+1))
				after, ae := f.Stat()
				f.Close()
				if e != nil {
					return e
				}
				if ae != nil || len(b) > retirementFileBytes || int64(len(b)) != st.Size() || !os.SameFile(st, after) || after.Mode() != st.Mode() || after.Size() != st.Size() {
					return errors.New("physical file changed while reading")
				}
				entry.Digest = fmt.Sprintf("%x", sha256.Sum256(b))
				m.Entries = append(m.Entries, entry)
			} else {
				return errors.New("symlink or special physical entry refused")
			}
		}
		return nil
	}
	if e := walk(root, ""); e != nil {
		return RetirementManifest{}, e
	}
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Path < m.Entries[j].Path })
	var e error
	m.Digest, e = retirementManifestEntriesDigest(m.Entries)
	if e != nil {
		return RetirementManifest{}, e
	}
	m.Complete = true
	return m, nil
}
func verifyRecordedRetirementManifest(ctx context.Context, id PathIdentity, expected RetirementManifest) error {
	if e := validateRetirementManifest(expected); e != nil {
		return e
	}
	digest, e := retirementManifestEntriesDigest(expected.Entries)
	if e != nil || digest != expected.Digest {
		return errors.New("invalid physical manifest digest")
	}
	root, got, e := openCanonical(id.Path)
	if e != nil {
		return e
	}
	defer root.Close()
	if got != id {
		return errors.New("physical root identity changed")
	}
	actual, e := walkRetirementManifest(ctx, root)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("physical retirement manifest changed")
	}
	return nil
}

func removeRecordedManifest(ctx context.Context, id, parentID PathIdentity, m RetirementManifest) (out TidyNativeOutcome, err error) {
	out.Effects = []string{}
	out.Limitations = []string{"Filesystem identities and bytes are sampled; external same-user races and ABA cannot be excluded."}
	if filepath.Dir(id.Path) != parentID.Path {
		return out, errors.New("retirement parent does not bind root")
	}
	parent, pid, e := openCanonical(parentID.Path)
	if e != nil {
		return out, e
	}
	defer parent.Close()
	if pid != parentID {
		return out, errors.New("retirement parent identity changed")
	}
	if e = verifyRecordedRetirementManifest(ctx, id, m); e != nil {
		return out, e
	}
	root, rid, e := openCanonical(id.Path)
	if e != nil {
		return out, e
	}
	defer root.Close()
	if rid != id {
		return out, errors.New("retirement root identity changed")
	}
	remaining := map[string]RetirementEntry{}
	for _, entry := range m.Entries {
		remaining[entry.Path] = entry
	}
	entries := append([]RetirementEntry(nil), m.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		di, dj := strings.Count(entries[i].Path, "/"), strings.Count(entries[j].Path, "/")
		if di != dj {
			return di > dj
		}
		return entries[i].Path > entries[j].Path
	})
	checkAnchor := func() error {
		if e := ctx.Err(); e != nil {
			return e
		}
		r, e := currentIdentity(id.Path)
		if e != nil || r != id {
			return errors.New("retirement root changed")
		}
		p, e := currentIdentity(parentID.Path)
		if e != nil || p != parentID {
			return errors.New("retirement parent changed")
		}
		return nil
	}
	for _, entry := range entries {
		if e = checkAnchor(); e != nil {
			return out, e
		}
		// Rewalk the remaining complete tree before every unlink. New entries and
		// replacements therefore stop further removal rather than broadening scope.
		actual, e := walkRetirementManifest(ctx, root)
		if e != nil {
			return out, e
		}
		if len(actual.Entries) != len(remaining) {
			return out, errors.New("retirement entries changed during removal")
		}
		for _, got := range actual.Entries {
			want, ok := remaining[got.Path]
			if !ok || got != want {
				return out, errors.New("retirement entry changed during removal")
			}
		}
		dir := root
		var opened []*os.Root
		parts := strings.Split(entry.Path, "/")
		for i, part := range parts[:len(parts)-1] {
			prefix := strings.Join(parts[:i+1], "/")
			want, ok := remaining[prefix]
			if !ok || want.Kind != "directory" {
				for _, r := range opened {
					r.Close()
				}
				return out, errors.New("unrecorded parent directory")
			}
			st, se := dir.Lstat(part)
			if se != nil {
				for _, r := range opened {
					r.Close()
				}
				return out, se
			}
			pi, pe := pathIdentity(prefix, st)
			if pe != nil || pi.Device != want.Device || pi.Inode != want.Inode || uint32(st.Mode()) != want.Mode {
				for _, r := range opened {
					r.Close()
				}
				return out, errors.New("parent directory changed")
			}
			next, ne := dir.OpenRoot(part)
			if ne != nil {
				for _, r := range opened {
					r.Close()
				}
				return out, ne
			}
			ns, ne := next.Stat(".")
			if ne != nil || !os.SameFile(st, ns) {
				next.Close()
				for _, r := range opened {
					r.Close()
				}
				return out, errors.New("parent directory replaced")
			}
			opened = append(opened, next)
			dir = next
		}
		if e = verifyRetirementRemovalLeaf(dir, parts[len(parts)-1], entry); e != nil {
			for _, r := range opened {
				r.Close()
			}
			return out, e
		}
		if e = checkAnchor(); e != nil {
			for _, r := range opened {
				r.Close()
			}
			return out, e
		}
		out.Attempted = true
		e = dir.Remove(parts[len(parts)-1])
		for _, r := range opened {
			r.Close()
		}
		if e != nil {
			return out, e
		}
		out.Effects = append(out.Effects, "Removed recorded "+entry.Kind+" "+entry.Path)
		delete(remaining, entry.Path)
	}
	if e = checkAnchor(); e != nil {
		return out, e
	}
	empty, e := walkRetirementManifest(ctx, root)
	if e != nil || len(empty.Entries) != 0 {
		return out, errors.New("retirement root is no longer empty")
	}
	st, e := parent.Lstat(filepath.Base(id.Path))
	if e != nil {
		return out, e
	}
	last, e := pathIdentity(id.Path, st)
	if e != nil || !st.IsDir() || last != id {
		return out, errors.New("retirement root replaced before removal")
	}
	if e = ctx.Err(); e != nil {
		return out, e
	}
	out.Attempted = true
	if e = parent.Remove(filepath.Base(id.Path)); e != nil {
		return out, e
	}
	out.Effects = append(out.Effects, "Removed exact owned workspace directory")
	out.Complete = true
	return out, nil
}

// Verification is immediately adjacent to unlink through the pinned parent.
// It does not make unlink conditional atomically; that sampling limit is exposed.
func verifyRetirementRemovalLeaf(dir *os.Root, name string, want RetirementEntry) error {
	st, e := dir.Lstat(name)
	if e != nil {
		return e
	}
	id, e := pathIdentity(want.Path, st)
	if e != nil || id.Device != want.Device || id.Inode != want.Inode || uint32(st.Mode()) != want.Mode {
		return errors.New("retirement unlink identity changed")
	}
	if want.Kind == "directory" {
		if !st.IsDir() {
			return errors.New("retirement directory type changed")
		}
		d, e := dir.OpenRoot(name)
		if e != nil {
			return e
		}
		defer d.Close()
		opened, e := d.Stat(".")
		if e != nil || !os.SameFile(st, opened) {
			return errors.New("retirement directory replaced")
		}
		f, e := d.Open(".")
		if e != nil {
			return e
		}
		defer f.Close()
		names, e := f.Readdirnames(1)
		if len(names) != 0 || (e != nil && !errors.Is(e, io.EOF)) {
			return errors.New("retirement directory contains unrecorded entries")
		}
		return nil
	}
	if want.Kind != "regular" || !st.Mode().IsRegular() || !retirementSingleLink(st) || st.Size() != want.Size {
		return errors.New("retirement file type or size changed")
	}
	f, e := dir.OpenFile(name, os.O_RDONLY|noFollow, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(st, opened) || opened.Mode() != st.Mode() || opened.Size() != st.Size() || !retirementSingleLink(opened) {
		return errors.New("retirement unlink file replaced")
	}
	b, e := io.ReadAll(io.LimitReader(f, retirementFileBytes+1))
	if e != nil {
		return e
	}
	after, e := f.Stat()
	if e != nil || !os.SameFile(st, after) || after.Mode() != st.Mode() || after.Size() != st.Size() || !retirementSingleLink(after) || int64(len(b)) != want.Size || fmt.Sprintf("%x", sha256.Sum256(b)) != want.Digest {
		return errors.New("retirement unlink content changed")
	}
	return nil
}

func retirementManifestDigest(m RetirementManifest) string {
	d, e := retirementManifestEntriesDigest(m.Entries)
	if e != nil {
		return ""
	}
	return d
}

func validateRetirementManifest(m RetirementManifest) error {
	if !m.Complete || len(m.Entries) > retirementManifestEntries || retirementManifestDigest(m) != m.Digest {
		return errors.New("invalid complete retirement manifest")
	}
	prior := ""
	dirs := map[string]bool{}
	aliases := map[string]bool{}
	for _, v := range m.Entries {
		if !retirementLiteralPath(v.Path) || v.Path <= prior || v.Inode == 0 {
			return errors.New("invalid retirement entry identity/order")
		}
		prior = v.Path
		folded := strings.ToLower(norm.NFC.String(v.Path))
		if aliases[folded] {
			return errors.New("aliased retirement manifest path")
		}
		aliases[folded] = true
		if p := filepath.Dir(v.Path); p != "." && !dirs[p] {
			return errors.New("missing recorded retirement parent")
		}
		mode := os.FileMode(v.Mode)
		switch v.Kind {
		case "directory":
			if !mode.IsDir() || mode&os.ModeSymlink != 0 || v.Size != 0 || v.Digest != "" || v.Executable {
				return errors.New("invalid directory retirement entry")
			}
			dirs[v.Path] = true
		case "regular":
			b, e := hex.DecodeString(v.Digest)
			if e != nil || len(b) != sha256.Size || strings.ToLower(v.Digest) != v.Digest || !mode.IsRegular() || v.Size < 0 || v.Size > retirementFileBytes || v.Executable != (mode.Perm()&0111 != 0) {
				return errors.New("invalid regular retirement entry")
			}
		default:
			return errors.New("unsupported retirement entry kind")
		}
	}
	return nil
}
