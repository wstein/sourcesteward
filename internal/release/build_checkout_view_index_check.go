// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The source index is compared semantically, never by bytes or inode: a project step
// such as `git status --porcelain` or `git describe --dirty` legitimately rewrites it
// (stat refresh) without changing any tracked state. What is bound is each entry's
// mode, object, stage and path, plus the flags that can hide a difference from the
// predicate: assume-unchanged, skip-worktree and intent-to-add.
const buildViewScratch = "scratch"

const buildViewIndexFlagMask = 0x8000 | 0x3000 | 1<<29 | 1<<30

// indexListing digests the semantic content of the index that root's private
// environment selects. Stat data (ctime, mtime, dev, ino, uid, gid, size) is read
// and dropped; only the entry header, path and masked flags are retained.
func (g *buildViewGraph) indexListing(ctx context.Context, root buildViewRoot) ([32]byte, error) {
	var sum [32]byte
	listing, status, err := g.read(ctx, string(root.Worktree), g.privateEnv(root), buildViewListingLimit, "ls-files", "--stage", "-v", "--debug", "-z", "--full-name", "--")
	if err != nil {
		return sum, err
	}
	defer listing.Close()
	if status != 0 {
		return sum, errors.New("native index listing failed")
	}
	r := bufio.NewReader(io.NewSectionReader(listing, 0, 1<<63-1))
	hash := sha256.New()
	for {
		header, err := r.ReadString('\t')
		if err == io.EOF && header == "" {
			copy(sum[:], hash.Sum(nil))
			return sum, nil
		}
		if err != nil {
			return sum, errors.New("invalid native index listing framing")
		}
		name, err := r.ReadString(0)
		if err != nil {
			return sum, errors.New("invalid native index listing path")
		}
		var flags uint64
		for i := 0; i < 5; i++ {
			line, e := r.ReadString('\n')
			if e != nil {
				return sum, errors.New("invalid native index listing statistics")
			}
			if i == 4 {
				_, text, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "flags: ")
				if !ok {
					return sum, errors.New("invalid native index listing flags")
				}
				if flags, e = strconv.ParseUint(text, 16, 64); e != nil {
					return sum, errors.New("invalid native index listing flags")
				}
			}
		}
		fmt.Fprintf(hash, "%d:%s%d:%s%x\n", len(header), header, len(name), name, flags&buildViewIndexFlagMask)
	}
}

// currentIndex copies the live source index (and its split-index base) through
// descriptor-relative bindings into a scratch git directory whose other entries link
// to the sealed private ones, and returns a root whose private environment selects it.
func (g *buildViewGraph) currentIndex(ctx context.Context, root buildViewRoot) (buildViewRoot, func(), error) {
	gd, err := os.MkdirTemp(g.scratch, "index-")
	if err != nil {
		return root, nil, err
	}
	done := func() { _ = os.RemoveAll(gd) }
	fail := func(e error) (buildViewRoot, func(), error) { done(); return root, nil, e }
	if err = os.Chmod(gd, 0700); err != nil {
		return fail(err)
	}
	entries, err := os.ReadDir(string(root.PrivateGitdir))
	if err != nil {
		return fail(err)
	}
	for _, entry := range entries {
		if entry.Name() == "index" || strings.HasPrefix(entry.Name(), "sharedindex.") {
			continue
		}
		if entry.Name() == "HEAD" {
			// Git validates HEAD of a git directory without following an absolute link.
			if err = copyBuildViewHead(filepath.Join(string(root.PrivateGitdir), "HEAD"), filepath.Join(gd, "HEAD")); err != nil {
				return fail(err)
			}
			continue
		}
		if err = os.Symlink(filepath.Join(string(root.PrivateGitdir), entry.Name()), filepath.Join(gd, entry.Name())); err != nil {
			return fail(err)
		}
	}
	if err = g.copyUnbound(ctx, filepath.Join(string(root.Gitdir), "index"), filepath.Join(gd, "index"), true); err != nil {
		return fail(err)
	}
	if index, e := os.Open(filepath.Join(gd, "index")); e == nil {
		dependency, e := buildViewIndexDependency(index, root.hashBytes)
		index.Close()
		if e != nil {
			return fail(e)
		}
		if dependency != "" {
			if err = g.copyUnbound(ctx, filepath.Join(string(root.Gitdir), dependency), filepath.Join(gd, dependency), false); err != nil {
				return fail(err)
			}
		}
		// Git trusts an entry whose stat data matches the file, and the stat data of the
		// live index is what a project step controls. In the scratch copy every entry
		// loses it, so native Git re-hashes the content of each tracked file whatever
		// its mtime is. A split index is first folded into one file, because the shared
		// base is named by its own checksum and cannot be edited in place.
		if dependency != "" {
			current := root
			current.PrivateGitdir = buildViewPath(gd)
			result, status, e := g.read(ctx, string(root.Worktree), g.privateEnv(current), buildViewQueryLimit, "update-index", "--no-split-index")
			if result != nil {
				result.Close()
			}
			if e != nil || status != 0 {
				return fail(errors.New("cannot fold the split index of the verified copy"))
			}
		}
		if err = smudgeBuildViewIndexStat(filepath.Join(gd, "index"), root.hashBytes); err != nil {
			return fail(err)
		}
		// A refresh then re-hashes every entry whose stat data no longer matches and
		// records the real stat data of those whose content equals the index. An entry
		// whose content differs keeps empty stat data, which the tracked-tree
		// comparison reports. The exit status only says that some entry needs an
		// update, which the comparison reports as well.
		current := root
		current.PrivateGitdir = buildViewPath(gd)
		result, _, e := g.read(ctx, string(root.Worktree), g.privateEnv(current), buildViewQueryLimit, "update-index", "-q", "--refresh")
		if result != nil {
			result.Close()
		}
		if e != nil {
			return fail(e)
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return fail(e)
	}
	root.PrivateGitdir = buildViewPath(gd)
	return root, done, nil
}

// checkIndexes refuses when any root's live index differs semantically from the
// captured one. With diff, the first root's tracked tree is also compared with the
// requested commit using the copy that was just verified, so stat data refreshed by a
// project step is judged by native Git exactly as the project would see it.
func (g *buildViewGraph) checkIndexes(ctx context.Context, diff bool) error {
	for i, root := range g.roots {
		current, done, err := g.currentIndex(ctx, root)
		if err != nil {
			return err
		}
		digest, err := g.indexListing(ctx, current)
		if err == nil && digest != g.indexSeal[i] {
			err = errors.New("checkout index changed")
		}
		if err == nil && diff && i == 0 {
			var result *os.File
			var status int
			result, status, err = g.read(ctx, string(current.Worktree), g.privateEnv(current), buildViewQueryLimit, "diff-index", "--quiet", "--no-ext-diff", "--no-textconv", g.options.Commit, "--")
			if result != nil {
				result.Close()
			}
			if err == nil && status != 0 {
				err = errors.New("tracked checkout differs from requested commit")
			}
		}
		done()
		if err != nil {
			return err
		}
	}
	return nil
}

func copyBuildViewHead(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(target, destination)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, raw, 0600)
}

// smudgeBuildViewIndexStat zeroes the ctime, mtime, dev, ino and size of every entry of
// the index file in place and rewrites the trailing checksum. Entry lengths, flags,
// modes, object ids, paths and extensions are untouched.
func smudgeBuildViewIndexStat(path string, hashBytes int) error {
	invalid := errors.New("verified index copy framing is invalid")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if hashBytes != 20 && hashBytes != 32 {
		return invalid
	}
	end := len(raw) - hashBytes
	if end < 12 || string(raw[:4]) != "DIRC" {
		return invalid
	}
	version := binary.BigEndian.Uint32(raw[4:8])
	if version < 2 || version > 4 {
		return invalid
	}
	count := binary.BigEndian.Uint32(raw[8:12])
	position := 12
	for i := uint32(0); i < count; i++ {
		start := position
		if end-position < 42+hashBytes {
			return invalid
		}
		clear(raw[position : position+24])    // ctime, mtime, dev, ino
		clear(raw[position+36 : position+40]) // size
		flags := binary.BigEndian.Uint16(raw[position+40+hashBytes:])
		position += 42 + hashBytes
		if flags&0x4000 != 0 {
			position += 2
		}
		if position > end {
			return invalid
		}
		if version == 4 {
			for {
				if position >= end {
					return invalid
				}
				position++
				if raw[position-1]&0x80 == 0 {
					break
				}
			}
		}
		nul := bytes.IndexByte(raw[position:end], 0)
		if nul < 0 {
			return invalid
		}
		position += nul + 1
		if version != 4 {
			position = start + (position-start+7)/8*8
		}
		if position > end {
			return invalid
		}
	}
	var sum []byte
	if hashBytes == 20 {
		digest := sha1.Sum(raw[:end])
		sum = digest[:]
	} else {
		digest := sha256.Sum256(raw[:end])
		sum = digest[:]
	}
	copy(raw[end:], sum)
	return os.WriteFile(path, raw, 0600)
}
