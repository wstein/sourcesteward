// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"errors"
	"fmt"
	"github.com/wstein/sourcesteward/internal/inspection"
	"sort"
	"strings"
)

var errJJCommittedSymlinkInput = errors.New("native jj committed symlink input resolution unavailable")

type jjSource struct{ reader *inspection.JJReleaseReader }

func (j jjSource) StwEntries() ([]sourceEntry, bool, error) {
	all, err := j.reader.Entries()
	if err != nil {
		return nil, false, err
	}
	byName := map[string]sourceEntry{}
	for path, kind := range all {
		if !strings.HasPrefix(path, ".stw/") {
			continue
		}
		name := strings.TrimPrefix(path, ".stw/")
		if i := strings.IndexByte(name, '/'); i >= 0 {
			name = name[:i]
			kind = "dir"
		}
		byName[name] = sourceEntry{Name: name, Kind: kind, id: path}
	}
	names := []string{}
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := []sourceEntry{}
	for _, name := range names {
		out = append(out, byName[name])
	}
	if len(out) > maxStwEntries {
		return out[:maxStwEntries], true, nil
	}
	return out, false, nil
}
func (j jjSource) Read(e sourceEntry, limit int) ([]byte, bool, error) {
	data, err := j.reader.Read(e.id)
	if errors.Is(err, inspection.ErrJJReleaseBlobTooLarge) {
		return nil, true, nil
	}
	if len(data) > limit {
		return data[:limit], true, err
	}
	return data, false, err
}
func (j jjSource) Entries(paths []string) (map[string]TreeEntry, error) {
	all, err := j.reader.Entries()
	if err != nil {
		return nil, err
	}
	out := map[string]TreeEntry{}
	for _, p := range paths {
		kind := all[p]
		switch kind {
		case "file":
			out[p] = TreeEntry{Kind: "file"}
		case "symlink":
			// Supported native file-show emits success with empty stdout for symlinks.
			// Unknown targets cannot prove separation from any build input.
			return nil, fmt.Errorf("%w: %q", errJJCommittedSymlinkInput, p)
		case "tree", "git-submodule":
			out[p] = TreeEntry{Kind: "dir"}
		default:
			for name := range all {
				if strings.HasPrefix(name, p+"/") {
					out[p] = TreeEntry{Kind: "dir"}
					break
				}
			}
		}
	}
	return out, nil
}
func (j jjSource) pathEntry(path string) (sourceEntry, int64, error) {
	all, err := j.reader.Entries()
	if err != nil {
		return sourceEntry{}, 0, err
	}
	n := int64(0)
	if all[path] == "file" {
		nonempty, err := j.reader.Nonempty(path)
		if err != nil {
			return sourceEntry{}, 0, err
		}
		if nonempty {
			n = 1
		}
	}
	return sourceEntry{Name: path, id: path, Kind: all[path]}, n, nil
}
func (j jjSource) commitTime() (string, error) { return j.reader.CommitTime() }
func (j jjSource) mainAncestry(remote string) (MainRef, Gate, error) {
	id, reason, ancestor, err := j.reader.Main(remote)
	main := MainRef{Remote: remote}
	if id != "" {
		main.Commit = &id
	}
	outcome := "fail"
	if ancestor {
		outcome = "pass"
	}
	if reason != "" {
		outcome = "not-run"
	}
	return main, gate("commit-not-on-main", outcome, reason, "recorded locally; no fetch was made; jj stored state was not snapshotted"), err
}

func (j jjSource) allowlistMatches(patterns []string) ([]AllowlistMatch, error) {
	entries, err := j.reader.Entries()
	if err != nil {
		return nil, err
	}
	rows := newAllowlistMatches(patterns)
	for path, kind := range entries {
		if kind != "tree" {
			countAllowlistPath(rows, path)
		}
	}
	return rows, nil
}
