// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type inspectFunc func(ctx context.Context, path string) Result
type inventoryFunc func(ctx context.Context, path string, o InventoryOptions) InventoryResult

// resolveReposPath returns the absolute, symlink-free path Git reports for p. The working directory is
// resolved too (os.Getwd honors a symlinked $PWD), so a relative path and a path through a symlinked parent
// compare equal to the root the identity inspection returns.
func resolveReposPath(p string) (string, error) {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if r, err = filepath.Abs(r); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(r)
}

// prepareExplicitEntries implements Stage S1: process-free structural validation of explicit paths.
func prepareExplicitEntries(paths []string, submodulesRequested bool) []ReposEntry {
	entries := make([]ReposEntry, len(paths))
	seenExplicit := make(map[string]int)

	for i, p := range paths {
		entry := ReposEntry{
			Index:   i,
			Path:    p,
			Origin:  "explicit",
			Backend: "unknown",
			Discovery: ReposDiscovery{
				Requested: submodulesRequested,
				Status:    "not-requested",
				Reason:    "",
			},
			Notes: []string{},
		}
		if submodulesRequested {
			entry.Discovery.Status = "not-applicable"
		}

		// Check for identical explicit paths without starting a process (FR-005, US6)
		if prevIdx, exists := seenExplicit[p]; exists {
			dupIdx := prevIdx
			entry.Status = "error"
			entry.Error = &ReposError{
				Code:    ReposCodeDuplicateRepository,
				Message: "duplicate explicit repository path",
			}
			entry.DuplicateOf = &dupIdx
			entries[i] = entry
			continue
		}
		seenExplicit[p] = i

		resolved, err := resolveReposPath(p)
		if err != nil {
			entry.Status = "error"
			entry.Error = &ReposError{
				Code:    "invalid-path",
				Message: "repository path does not exist or is inaccessible",
			}
			entries[i] = entry
			continue
		}
		if st, err := os.Stat(resolved); err != nil || !st.IsDir() {
			entry.Status = "error"
			entry.Error = &ReposError{
				Code:    "invalid-path",
				Message: "repository path is not an accessible directory",
			}
			entries[i] = entry
			continue
		}
		if !utf8.ValidString(resolved) {
			entry.Status = "error"
			entry.Error = &ReposError{
				Code:    "invalid-path",
				Message: "resolved repository path is not valid UTF-8",
			}
			entries[i] = entry
			continue
		}
		entry.ResolvedPath = &resolved

		entries[i] = entry
	}
	return entries
}

// computeContainment detects containment of listed repositories (FR-005).
func computeContainment(entries []ReposEntry) {
	for i := range entries {
		if entries[i].ResolvedPath == nil {
			continue
		}
		pathI := filepath.Clean(*entries[i].ResolvedPath)
		var nearestContainer *int
		longestPrefixLen := -1

		for j := range entries {
			if i == j || entries[j].ResolvedPath == nil {
				continue
			}
			pathJ := filepath.Clean(*entries[j].ResolvedPath)
			prefix := pathJ + string(filepath.Separator)
			if strings.HasPrefix(pathI, prefix) {
				if len(pathJ) > longestPrefixLen {
					longestPrefixLen = len(pathJ)
					idx := j
					nearestContainer = &idx
				}
			}
		}
		entries[i].ContainedIn = nearestContainer
	}
}
