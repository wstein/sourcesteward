// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// sourceEntry is one entry of the .stw directory of the evaluated source.
type sourceEntry struct {
	Name string
	Kind string // "file" (regular), "symlink", "dir" or "other"
	id   string // blob id for a commit source
}

// contractSource reads the contract files and answers tree questions for one evaluated source:
// the working tree or a commit.
type contractSource interface {
	Tree
	// Entries lists .stw; truncated reports a listing cut at its bound.
	Entries() (entries []sourceEntry, truncated bool, err error)
	// Read returns at most limit bytes of the entry and whether it is longer.
	Read(e sourceEntry, limit int) (data []byte, tooLarge bool, err error)
}

// errStwNotDirectory is returned when .stw exists but is a symlink or not a directory.
var errStwNotDirectory = errors.New(".stw is not a directory")

// errNotStable is returned when a listed contract file is not, when read, the same regular file.
var errNotStable = errors.New("the contract file is not a stable regular file")

// beforeOpen is a test seam called between checking a file and opening it, so a test can swap the
// file the way a concurrent writer could.
var beforeOpen = func(path string) {}

// worktreeSource reads files of the working tree without following symlinks.
type worktreeSource struct{ root string }

func (w worktreeSource) Entries() ([]sourceEntry, bool, error) {
	info, err := os.Lstat(filepath.Join(w.root, ".stw"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.IsDir() {
		return nil, false, errStwNotDirectory
	}
	items, err := os.ReadDir(filepath.Join(w.root, ".stw"))
	if err != nil {
		return nil, false, err
	}
	entries := make([]sourceEntry, 0, len(items))
	for _, item := range items {
		kind := "other"
		switch mode := item.Type(); {
		case mode.IsRegular():
			kind = "file"
		case mode&os.ModeSymlink != 0:
			kind = "symlink"
		case mode.IsDir():
			kind = "dir"
		}
		entries = append(entries, sourceEntry{Name: item.Name(), Kind: kind})
	}
	return entries, false, nil
}

// Read opens a regular file, rechecks after opening that it is the same file that was listed
// (no symlink swap), and reads at most limit bytes.
func (w worktreeSource) Read(e sourceEntry, limit int) ([]byte, bool, error) {
	path := filepath.Join(w.root, ".stw", e.Name)
	before, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	if !before.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%w: it is not a regular file", errNotStable)
	}
	beforeOpen(path)
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(before, after) || !after.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%w: it changed while it was opened", errNotStable)
	}
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}

// Kinds reports which paths exist in the working tree; a path below a symlink counts as absent
// and a final symlink counts as a file.
func (w worktreeSource) Kinds(paths []string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range paths {
		segments := strings.Split(p, "/")
		current, kind := w.root, ""
		for i, segment := range segments {
			current = filepath.Join(current, segment)
			info, err := os.Lstat(current)
			if err != nil {
				kind = ""
				break
			}
			kind = "file"
			if info.IsDir() {
				kind = "dir"
			}
			// Only the last segment may be a file; a symlink on the way is a file here, so a path
			// below it is not probed through it.
			if kind == "file" && i < len(segments)-1 {
				kind = ""
				break
			}
		}
		if kind != "" {
			out[p] = kind
		}
	}
	return out, nil
}
