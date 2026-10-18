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

type retirementContentPolicy uint8

const (
	retirementLegacyContent retirementContentPolicy = iota + 1
	retirementGitV3Content
)

// Policies are selected from a command/record version and backend, never from
// caller-supplied numeric limits. Old manifest and unlink entry points stay v2.
func retirementPolicy(backend string, version int) (retirementContentPolicy, error) {
	switch {
	case (backend == "git" || backend == "jj") && version == 2:
		return retirementLegacyContent, nil
	case backend == "git" && version == 3:
		return retirementGitV3Content, nil
	default:
		return 0, errors.New("unsupported retirement backend/version content policy")
	}
}
func (p retirementContentPolicy) fileLimit(metadata bool) (int64, error) {
	if p != retirementLegacyContent && p != retirementGitV3Content {
		return 0, errors.New("unknown retirement content policy")
	}
	if metadata || p == retirementLegacyContent {
		return retirementFileBytes, nil
	}
	return 16 << 20, nil
}

// Only Git v3 admits the inert native commit-message file. The original
// metadata vocabulary and every Git/jj v2 caller keep their reviewed meaning.
func gitRetirementV3MetadataPaths(policy retirementContentPolicy) ([]string, error) {
	if policy != retirementGitV3Content {
		return nil, errors.New("Git v3 metadata policy required")
	}
	return append(gitRetirementMetadataPaths(), "COMMIT_EDITMSG"), nil
}

func validateRetirementV3Manifest(m V3Manifest, root string, metadata bool, policy retirementContentPolicy) error {
	if policy != retirementGitV3Content || !validV3Manifest(m, root, metadata) {
		return errors.New("invalid v3 physical retirement manifest")
	}
	b, e := json.Marshal(m)
	if e != nil || len(b) > 1<<20 {
		return errors.New("v3 retirement manifest exceeds encoded limit")
	}
	dirs := map[string]bool{}
	aliases := map[string]bool{}
	var userBytes int64
	for _, v := range m.Entries {
		rel, e := filepath.Rel(root, v.Identity.Path)
		if e != nil || !retirementLiteralPath(rel) || len(rel) > 4096 {
			return errors.New("unsafe v3 physical path")
		}
		folded := strings.ToLower(norm.NFC.String(rel))
		if aliases[folded] {
			return errors.New("aliased v3 physical path")
		}
		aliases[folded] = true
		if parent := filepath.Dir(rel); parent != "." && !dirs[parent] {
			return errors.New("missing v3 manifest parent")
		}
		if v.Identity.Kind == "directory" {
			dirs[rel] = true
		} else if !metadata && rel != ".git" {
			userBytes += v.Identity.Size
		}
	}
	if userBytes > 64<<20 {
		return errors.New("v3 user pass exceeds 64 MiB")
	}
	return nil
}

// The caller starts one full observation on its operation-wide ledger. A disk
// observation is a fresh user pass plus a physical metadata pass; a registration
// observation is one metadata pass. No prior digest is reused as read evidence.
func observeRetirementV3Manifest(ctx context.Context, path string, a retirementManifestAdmission, metadataOnly bool, policy retirementContentPolicy, budget *tidyV3ProofBudget) (V3Manifest, error) {
	empty := V3Manifest{}
	if policy != retirementGitV3Content || a.Backend != "git" {
		return empty, errors.New("Git v3 content policy required")
	}
	if metadataOnly && len(a.StoredFiles) > 0 {
		return empty, errors.New("user files in metadata admission")
	}
	known := map[string]bool{}
	metadata := map[string]bool{}
	stored := map[string]RetirementStoredFile{}
	admit := func(p string) error {
		if !retirementLiteralPath(p) || len(p) > 4096 {
			return errors.New("unsafe v3 admission path")
		}
		known[p] = true
		for d := filepath.Dir(p); d != "."; d = filepath.Dir(d) {
			known[d] = true
		}
		if len(known) > retirementManifestEntries {
			return errors.New("aggregate manifest admission exceeds 1024 entries")
		}
		return nil
	}
	allowedMetadata := map[string]bool{".git": true}
	if metadataOnly {
		allowedMetadata = map[string]bool{}
		paths, e := gitRetirementV3MetadataPaths(policy)
		if e != nil {
			return empty, e
		}
		for _, p := range paths {
			allowedMetadata[p] = true
		}
	}
	for _, p := range a.NativeMetadataPaths {
		if !allowedMetadata[p] {
			return empty, errors.New("unsupported native metadata admission")
		}
		if metadata[p] {
			return empty, errors.New("duplicate metadata admission")
		}
		if e := admit(p); e != nil {
			return empty, e
		}
		metadata[p] = true
	}
	var total int64
	for _, f := range a.StoredFiles {
		limit, _ := policy.fileLimit(false)
		digest, e := hex.DecodeString(f.Digest)
		if e != nil || len(digest) != sha256.Size || strings.ToLower(f.Digest) != f.Digest || f.Size < 0 || f.Size > limit {
			return empty, errors.New("invalid v3 stored file fact")
		}
		if _, ok := stored[f.Path]; ok || metadata[f.Path] {
			return empty, errors.New("duplicate stored admission")
		}
		if e := admit(f.Path); e != nil {
			return empty, e
		}
		stored[f.Path] = f
		total += f.Size
		if total > 64<<20 {
			return empty, errors.New("v3 user pass exceeds 64 MiB")
		}
	}
	if !metadataOnly && (len(a.NativeMetadataPaths) != 1 || !metadata[".git"]) {
		return empty, errors.New("Git physical metadata must be the exact .git pointer")
	}
	if !metadataOnly {
		if e := budget.beginUserPass(); e != nil {
			return empty, e
		}
	}
	if e := budget.beginMetadataPass(); e != nil {
		return empty, e
	}
	root, _, e := openCanonical(path)
	if e != nil {
		return empty, e
	}
	defer root.Close()
	rootBefore, e := root.Stat(".")
	if e != nil {
		return empty, e
	}
	rootID, e := retirementV3Physical(path, rootBefore)
	if e != nil {
		return empty, e
	}
	m := V3Manifest{Entries: []V3ManifestEntry{}}
	seen := map[string]bool{}
	aliases := map[string]bool{}
	var walkedUserBytes int64
	var walk func(*os.Root, string) error
	walk = func(dir *os.Root, prefix string) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		listing, e := dir.Open(".")
		if e != nil {
			return e
		}
		names, e := listing.Readdirnames(retirementManifestEntries + 1)
		listing.Close()
		if e != nil && e != io.EOF {
			return e
		}
		sort.Strings(names)
		for _, name := range names {
			if e := ctx.Err(); e != nil {
				return e
			}
			rel := name
			if prefix != "" {
				rel = prefix + "/" + name
			}
			if !known[rel] || len(m.Entries) >= retirementManifestEntries {
				return errors.New("unknown or excessive v3 physical payload")
			}
			folded := strings.ToLower(norm.NFC.String(rel))
			if aliases[folded] {
				return errors.New("aliased v3 physical path")
			}
			aliases[folded] = true
			full := filepath.Join(path, rel)
			st, e := dir.Lstat(name)
			if e != nil {
				return e
			}
			id, e := retirementV3Physical(full, st)
			if e != nil {
				return e
			}
			entry := V3ManifestEntry{Identity: id}
			seen[rel] = true
			if id.Kind == "directory" {
				if metadata[rel] && (!metadataOnly || (rel != "logs" && rel != "refs")) {
					return errors.New("native metadata file became directory")
				}
				if _, ok := stored[rel]; ok {
					return errors.New("stored file became directory")
				}
				child, e := dir.OpenRoot(name)
				if e != nil {
					return e
				}
				opened, e := child.Stat(".")
				if e != nil {
					child.Close()
					return e
				}
				openedID, e := retirementV3Physical(full, opened)
				if e != nil || openedID != id {
					child.Close()
					return errors.New("physical directory changed on open")
				}
				m.Entries = append(m.Entries, entry)
				e = walk(child, rel)
				after, ae := child.Stat(".")
				child.Close()
				final, fe := dir.Lstat(name)
				if e != nil {
					return e
				}
				if ae != nil || fe != nil {
					return errors.New("physical directory unavailable after walk")
				}
				ai, ae := retirementV3Physical(full, after)
				fi, fe := retirementV3Physical(full, final)
				if ae != nil || fe != nil || ai != id || fi != id {
					return errors.New("physical directory changed during walk")
				}
			} else {
				isMetadata := metadataOnly || metadata[rel]
				if metadataOnly && (rel == "logs" || rel == "refs") {
					return errors.New("native metadata directory became file")
				}
				fact, ok := stored[rel]
				if !ok && !metadata[rel] {
					return errors.New("unexpected physical file")
				}
				limit, _ := policy.fileLimit(isMetadata)
				if id.Size > limit {
					return errors.New("oversized v3 physical file")
				}
				if !isMetadata {
					walkedUserBytes += id.Size
					if walkedUserBytes > 64<<20 {
						return errors.New("v3 user pass exceeds 64 MiB")
					}
				}
				digest, e := readRetirementV3File(ctx, dir, name, id, isMetadata, budget)
				if e != nil {
					return e
				}
				entry.Digest = digest
				entry.Executable = id.Mode&0111 != 0
				if ok && (fact.Digest != digest || fact.Size != id.Size || fact.Executable != entry.Executable) {
					return errors.New("stored content differs from physical disk")
				}
				m.Entries = append(m.Entries, entry)
			}
		}
		return nil
	}
	if e = walk(root, ""); e != nil {
		return empty, e
	}
	for p := range metadata {
		if !seen[p] {
			return empty, errors.New("admitted metadata missing")
		}
	}
	for p := range stored {
		if !seen[p] {
			return empty, errors.New("stored file missing")
		}
	}
	after, e := root.Stat(".")
	if e != nil {
		return empty, e
	}
	ai, e := retirementV3Physical(path, after)
	final, e2 := os.Lstat(path)
	if e2 != nil {
		return empty, e2
	}
	fi, e2 := retirementV3Physical(path, final)
	if e != nil || e2 != nil || ai != rootID || fi != rootID {
		return empty, errors.New("physical manifest root changed")
	}
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Identity.Path < m.Entries[j].Identity.Path })
	m.Digest, e = v3RecordDigest(m.Entries)
	if e != nil {
		return empty, e
	}
	m.Complete = true
	if e = validateRetirementV3Manifest(m, path, metadataOnly, policy); e != nil {
		return empty, e
	}
	return m, nil
}

func readRetirementV3File(ctx context.Context, dir *os.Root, name string, want V3PhysicalIdentity, metadata bool, budget *tidyV3ProofBudget) (string, error) {
	f, e := dir.OpenFile(name, os.O_RDONLY|noFollow, 0)
	if e != nil {
		return "", e
	}
	defer f.Close()
	check := func(st os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		id, e := retirementV3Physical(want.Path, st)
		if e != nil || id != want {
			return errors.New("physical file identity/time changed")
		}
		return nil
	}
	if e = check(f.Stat()); e != nil {
		return "", e
	}
	channel := proofUser
	if metadata {
		channel = proofMetadata
	}
	r := budget.reader(ctx, channel, f)
	h := sha256.New()
	var buffer [64 << 10]byte
	remaining := want.Size
	for remaining > 0 {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		n, re := r.Read(buffer[:min(remaining, int64(len(buffer)))])
		if n > 0 {
			h.Write(buffer[:n])
			remaining -= int64(n)
		}
		if re != nil && !(re == io.EOF && remaining == 0) {
			return "", re
		}
		if n == 0 {
			return "", io.ErrNoProgress
		}
	}
	var one [1]byte
	n, re := r.Read(one[:])
	if n != 0 || re != io.EOF {
		return "", errors.New("physical file lacks exact EOF")
	}
	if e = check(f.Stat()); e != nil {
		return "", e
	}
	if e = check(dir.Lstat(name)); e != nil {
		return "", e
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func verifyRecordedRetirementV3Manifest(ctx context.Context, path string, a retirementManifestAdmission, metadata bool, policy retirementContentPolicy, budget *tidyV3ProofBudget, want V3Manifest) error {
	if e := validateRetirementV3Manifest(want, path, metadata, policy); e != nil {
		return e
	}
	got, e := observeRetirementV3Manifest(ctx, path, a, metadata, policy, budget)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(got, want) {
		return errors.New("recorded v3 manifest changed")
	}
	return nil
}
