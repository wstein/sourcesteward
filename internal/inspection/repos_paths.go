// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

type inspectFunc func(ctx context.Context, path string) Result
type inventoryFunc func(ctx context.Context, path string, o InventoryOptions) InventoryResult

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

		// Check for leading dash (e.g. passed after --)
		if strings.HasPrefix(p, "-") {
			entry.Status = "error"
			entry.Error = &ReposError{
				Code:    "invalid-path",
				Message: "repository path must not start with '-'",
			}
			entries[i] = entry
			continue
		}

		st, err := os.Lstat(p)
		if err != nil {
			entry.Status = "error"
			entry.Error = &ReposError{
				Code:    "invalid-path",
				Message: "repository path does not exist or is inaccessible",
			}
			entries[i] = entry
			continue
		}

		if st.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    "invalid-path",
					Message: "cannot resolve repository symlink target",
				}
				entries[i] = entry
				continue
			}
			resolvedSt, err := os.Stat(resolved)
			if err != nil || !resolvedSt.IsDir() {
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    "invalid-path",
					Message: "repository symlink target must be an accessible directory",
				}
				entries[i] = entry
				continue
			}
			abs, err := filepath.Abs(resolved)
			if err != nil {
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    "invalid-path",
					Message: "cannot determine absolute path for symlink target",
				}
				entries[i] = entry
				continue
			}
			entry.ResolvedPath = &abs
		} else {
			if !st.IsDir() {
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    "invalid-path",
					Message: "repository path is a file, not a directory",
				}
				entries[i] = entry
				continue
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    "invalid-path",
					Message: "cannot determine absolute path for repository",
				}
				entries[i] = entry
				continue
			}
			entry.ResolvedPath = &abs
		}

		entries[i] = entry
	}
	return entries
}

// inspectExplicitEntries implements Stage S2 & S3: identity inspection, root validation, and deduplication.
func inspectExplicitEntries(ctx context.Context, entries []ReposEntry, inspectFn inspectFunc) {
	seenNative := make(map[string]int)

	for i := range entries {
		entry := &entries[i]
		if entry.Status == "error" || entry.ResolvedPath == nil {
			continue
		}

		insp := inspectFn(ctx, *entry.ResolvedPath)
		entry.Backend = insp.Backend

		if insp.Status == "error" {
			entry.Status = "error"
			code := "inspection-failed"
			msg := "identity inspection failed"
			if len(insp.Diagnostics) > 0 {
				code = insp.Diagnostics[0].Code
				msg = insp.Diagnostics[0].Message
			}
			entry.Error = &ReposError{Code: code, Message: msg}
			continue
		}

		// Root equality check (FR-004)
		var repoRoot string
		switch insp.Backend {
		case "git":
			if insp.Repository != nil {
				if insp.Repository.Bare {
					repoRoot = insp.Repository.GitDir
				} else if insp.Repository.Worktree != nil {
					repoRoot = *insp.Repository.Worktree
				} else {
					repoRoot = insp.Repository.GitDir
				}
			}
		case "jj":
			if insp.JJRepository != nil {
				repoRoot = insp.JJRepository.WorkspaceRoot
			} else {
				repoRoot = *entry.ResolvedPath
			}
		default:
			repoRoot = *entry.ResolvedPath
		}

		if filepath.Clean(*entry.ResolvedPath) != filepath.Clean(repoRoot) {
			entry.Status = "error"
			entry.Error = &ReposError{
				Code:    ReposCodeNotRepositoryRoot,
				Message: "path is inside a repository but not its root",
			}
			continue
		}

		// Native repository identity deduplication (FR-005)
		var nativeID string
		switch insp.Backend {
		case "git":
			if insp.Repository != nil {
				nativeID = "git:" + filepath.Clean(insp.Repository.CommonDir)
			}
		case "jj":
			if insp.JJRepository != nil {
				nativeID = "jj:" + filepath.Clean(insp.JJRepository.RepositoryDir)
			}
		}

		if nativeID != "" {
			if prevIdx, exists := seenNative[nativeID]; exists {
				dupIdx := prevIdx
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    ReposCodeDuplicateRepository,
					Message: "same repository as an earlier entry",
				}
				entry.DuplicateOf = &dupIdx
				continue
			}
			seenNative[nativeID] = entry.Index
		}
	}
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

// inventoryEntries runs the inventory operation on surviving entries.
func inventoryEntries(ctx context.Context, entries []ReposEntry, inventoryFn inventoryFunc) {
	for i := range entries {
		entry := &entries[i]
		if entry.Status == "error" || entry.Status == "skipped" || entry.ResolvedPath == nil {
			continue
		}

		var opts InventoryOptions
		switch entry.Backend {
		case "git":
			opts = InventoryOptions{}
		case "jj":
			opts = InventoryOptions{Scopes: []string{"stored"}}
		default:
			continue
		}

		inv := inventoryFn(ctx, *entry.ResolvedPath, opts)
		entry.Inventory = &inv

		switch inv.Status {
		case "ok":
			entry.Status = "ok"
			entry.Error = nil
		case "partial":
			entry.Status = "partial"
			msg := "inventory status partial"
			if len(inv.Boundary.Limitations) > 0 {
				msg = inv.Boundary.Limitations[0]
			}
			entry.Error = &ReposError{
				Code:    ReposCodeInventoryPartial,
				Message: msg,
			}
		default: // "error"
			entry.Status = "error"
			code := "inventory-failed"
			msg := "inventory failed"
			if len(inv.Diagnostics) > 0 {
				code = inv.Diagnostics[0].Code
				msg = inv.Diagnostics[0].Message
			}
			entry.Error = &ReposError{
				Code:    code,
				Message: msg,
			}
		}
	}
}
