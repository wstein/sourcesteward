// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// discoverSubmodules handles Stage S4 discovery for explicit Git parents.
func discoverSubmodules(ctx context.Context, entries *[]ReposEntry, inspections *[]Result, inspectFn inspectFunc, run runner) {
	parentCount := len(*entries)
	seenPaths := make(map[string]bool)
	for _, e := range *entries {
		if e.ResolvedPath != nil {
			seenPaths[filepath.Clean(*e.ResolvedPath)] = true
		}
	}

	for i := 0; i < parentCount; i++ {
		entry := &(*entries)[i]
		if !entry.Discovery.Requested {
			continue
		}

		// Discovery runs only for explicit parents that passed root and duplicate checks and are Git-owned
		if entry.Origin != "explicit" || entry.Status == "error" || entry.DuplicateOf != nil || entry.Backend != "git" || entry.ResolvedPath == nil {
			entry.Discovery.Status = "not-applicable"
			continue
		}

		parentRoot := *entry.ResolvedPath
		parentGitDir := filepath.Join(parentRoot, ".git")
		if entry.Inventory != nil && entry.Inventory.Identity.Repository != nil {
			parentGitDir = entry.Inventory.Identity.Repository.GitDir
		}

		// 1. Check HEAD .gitmodules blob via ls-tree -z -l HEAD -- .gitmodules
		lsOut, code, err := run(ctx, parentRoot, "ls-tree", "-z", "-l", "HEAD", "--", ".gitmodules")
		if err != nil || code != 0 {
			// Unborn HEAD or command failure
			entry.Discovery.Status = "ok"
			continue
		}

		lsOut = strings.TrimRight(lsOut, "\x00")
		if lsOut == "" {
			entry.Discovery.Status = "ok"
			continue
		}

		// Parse ls-tree output: <mode> <type> <object> <size>\t<file>
		tabIdx := strings.IndexByte(lsOut, '\t')
		if tabIdx == -1 {
			entry.Discovery.Status = "failed"
			entry.Discovery.Reason = "malformed ls-tree output"
			entry.Status = "partial"
			entry.Error = &ReposError{Code: ReposCodeDiscoveryFailed, Message: "malformed ls-tree output for .gitmodules"}
			continue
		}

		metaFields := strings.Fields(lsOut[:tabIdx])
		if len(metaFields) < 4 {
			entry.Discovery.Status = "failed"
			entry.Discovery.Reason = "malformed ls-tree metadata"
			entry.Status = "partial"
			entry.Error = &ReposError{Code: ReposCodeDiscoveryFailed, Message: "malformed ls-tree metadata for .gitmodules"}
			continue
		}

		mode := metaFields[0]
		blobOID := metaFields[2]
		sizeStr := metaFields[3]

		// Mode check: regular file only (100644 or 100755)
		if mode != "100644" && mode != "100755" {
			entry.Discovery.Status = "failed"
			entry.Discovery.Reason = fmt.Sprintf("invalid .gitmodules mode: %s", mode)
			entry.Status = "partial"
			entry.Error = &ReposError{Code: ReposCodeDiscoveryFailed, Message: fmt.Sprintf(".gitmodules is not a regular file (mode %s)", mode)}
			continue
		}

		// Size check: at most 64 KiB
		size, err := strconv.Atoi(sizeStr)
		if err != nil || size > ReposGitmodulesBytes {
			entry.Discovery.Status = "failed"
			entry.Discovery.Reason = "oversized .gitmodules blob"
			entry.Status = "partial"
			entry.Error = &ReposError{Code: ReposCodeDiscoveryFailed, Message: ".gitmodules exceeds maximum size of 64 KiB"}
			continue
		}

		// 2. Parse config --blob <oid> --null --list --no-includes
		cfgOut, code, err := run(ctx, parentRoot, "config", "--blob", blobOID, "--null", "--list", "--no-includes")
		if err != nil || code != 0 {
			entry.Discovery.Status = "failed"
			entry.Discovery.Reason = "cannot parse .gitmodules blob"
			entry.Status = "partial"
			entry.Error = &ReposError{Code: ReposCodeDiscoveryFailed, Message: "cannot parse .gitmodules config blob"}
			continue
		}

		// Collect last path per submodule name
		nameToPath := make(map[string]string)
		items := strings.Split(cfgOut, "\x00")
		for _, item := range items {
			if item == "" {
				continue
			}
			newlineIdx := strings.IndexByte(item, '\n')
			if newlineIdx == -1 {
				continue
			}
			k := item[:newlineIdx]
			v := item[newlineIdx+1:]
			lowerK := strings.ToLower(k)
			if strings.HasPrefix(lowerK, "submodule.") && strings.HasSuffix(lowerK, ".path") {
				subName := k[len("submodule.") : len(k)-len(".path")]
				if v != "" {
					nameToPath[subName] = v
				}
			}
		}

		// Deduplicate candidate paths across names
		distinctPathsMap := make(map[string]bool)
		for _, p := range nameToPath {
			distinctPathsMap[p] = true
		}

		// Candidate limit check (64)
		if len(distinctPathsMap) > ReposMaxDiscoveryCandidates {
			entry.Discovery.Status = "limit"
			entry.Discovery.Reason = fmt.Sprintf("candidate paths exceed limit of %d", ReposMaxDiscoveryCandidates)
			entry.Status = "partial"
			entry.Error = &ReposError{Code: ReposCodeDiscoveryLimit, Message: fmt.Sprintf("submodule candidates exceed limit of %d", ReposMaxDiscoveryCandidates)}
			continue
		}

		var candidatePaths []string
		for p := range distinctPathsMap {
			candidatePaths = append(candidatePaths, p)
		}
		sort.Strings(candidatePaths)

		// 3. Confirm each candidate path and check safety
		type candidateEntry struct {
			relPath   string
			absPath   string
			isSkipped bool
			isError   bool
			errCode   string
			errMsg    string
		}

		var validCandidates []candidateEntry

		for _, p := range candidatePaths {
			// Lexical check
			if !isLexicallyValidSubmodulePath(p) {
				entry.Discovery.Ignored++
				entry.Notes = append(entry.Notes, fmt.Sprintf("discovery-ignored: lexically invalid path %q", p))
				continue
			}

			// Index confirmation with ls-files --stage -z -- ':(literal)<path>'
			filesOut, code, err := run(ctx, parentRoot, "ls-files", "--stage", "-z", "--", ":(literal)"+p)
			if err != nil || code != 0 || filesOut == "" {
				entry.Discovery.Ignored++
				continue
			}
			filesOut = strings.TrimRight(filesOut, "\x00")
			fTabIdx := strings.IndexByte(filesOut, '\t')
			if fTabIdx == -1 {
				entry.Discovery.Ignored++
				continue
			}
			fMeta := strings.Fields(filesOut[:fTabIdx])
			if len(fMeta) < 3 || fMeta[0] != "160000" || filesOut[fTabIdx+1:] != p {
				entry.Discovery.Ignored++
				continue
			}

			// Submodule path resolution and containment check
			fullSubPath := filepath.Join(parentRoot, filepath.FromSlash(p))
			absSubPath, err := filepath.Abs(fullSubPath)
			if err != nil {
				entry.Discovery.Ignored++
				continue
			}

			// Check if already listed (deduplicated)
			cleanSubPath := filepath.Clean(absSubPath)
			resolvedCandidate, err := filepath.EvalSymlinks(cleanSubPath)
			if seenPaths[cleanSubPath] || (err == nil && seenPaths[filepath.Clean(resolvedCandidate)]) {
				entry.Discovery.Deduplicated++
				continue
			}

			// Symlink safety check on path components below parentRoot
			relClean := filepath.Clean(filepath.FromSlash(p))
			parts := strings.Split(relClean, string(filepath.Separator))
			curr := parentRoot
			isSafe := true
			for _, part := range parts {
				curr = filepath.Join(curr, part)
				st, err := os.Lstat(curr)
				if err != nil {
					break
				}
				if st.Mode()&os.ModeSymlink != 0 {
					isSafe = false
					break
				}
			}

			if !isSafe {
				validCandidates = append(validCandidates, candidateEntry{
					relPath: p,
					absPath: absSubPath,
					isError: true,
					errCode: ReposCodePathUnsafe,
					errMsg:  "symlink in submodule path",
				})
				continue
			}

			// Check filesystem state of .git
			gitEntry := filepath.Join(absSubPath, ".git")
			st, err := os.Lstat(gitEntry)
			if err != nil {
				// Uninitialized submodule directory
				validCandidates = append(validCandidates, candidateEntry{
					relPath:   p,
					absPath:   absSubPath,
					isSkipped: true,
					errCode:   ReposCodeSubmoduleUninitialized,
					errMsg:    "submodule is uninitialized",
				})
				continue
			}

			if st.Mode()&os.ModeSymlink != 0 || st.Mode()&os.ModeNamedPipe != 0 || st.Mode()&os.ModeSocket != 0 {
				validCandidates = append(validCandidates, candidateEntry{
					relPath: p,
					absPath: absSubPath,
					isError: true,
					errCode: ReposCodePathUnsafe,
					errMsg:  ".git is a symlink or special file",
				})
				continue
			}

			if st.Mode().IsRegular() {
				// Pointer file check
				ptrData, err := os.ReadFile(gitEntry)
				if err != nil || len(ptrData) > ReposMaxPathBytes {
					validCandidates = append(validCandidates, candidateEntry{
						relPath: p,
						absPath: absSubPath,
						isError: true,
						errCode: ReposCodePathUnsafe,
						errMsg:  "cannot read .git pointer file",
					})
					continue
				}
				ptrContent := strings.TrimSpace(string(ptrData))
				if !strings.HasPrefix(ptrContent, "gitdir:") {
					validCandidates = append(validCandidates, candidateEntry{
						relPath: p,
						absPath: absSubPath,
						isError: true,
						errCode: ReposCodePathUnsafe,
						errMsg:  "invalid .git pointer format",
					})
					continue
				}
				ptrTarget := strings.TrimSpace(ptrContent[len("gitdir:"):])
				var absTarget string
				if filepath.IsAbs(ptrTarget) {
					absTarget = filepath.Clean(ptrTarget)
				} else {
					absTarget = filepath.Clean(filepath.Join(filepath.Dir(gitEntry), ptrTarget))
				}
				modulesDir := filepath.Clean(filepath.Join(parentGitDir, "modules"))
				if !strings.HasPrefix(absTarget, modulesDir+string(filepath.Separator)) && absTarget != modulesDir {
					validCandidates = append(validCandidates, candidateEntry{
						relPath: p,
						absPath: absSubPath,
						isError: true,
						errCode: ReposCodePathUnsafe,
						errMsg:  ".git pointer targets location outside superproject modules directory",
					})
					continue
				}
			}

			validCandidates = append(validCandidates, candidateEntry{
				relPath: p,
				absPath: absSubPath,
			})
		}

		// Check 16-entry cap across total entries
		if len(*entries)+len(validCandidates) > ReposMaxEntries {
			entry.Discovery.Status = "limit"
			entry.Discovery.Reason = fmt.Sprintf("total entries would exceed limit of %d", ReposMaxEntries)
			entry.Status = "partial"
			entry.Error = &ReposError{Code: ReposCodeDiscoveryLimit, Message: fmt.Sprintf("total entries would exceed limit of %d", ReposMaxEntries)}
			continue
		}

		entry.Discovery.Status = "ok"
		entry.Discovery.Submodules = len(validCandidates)

		// Append new submodule entries
		parentIdx := entry.Index
		for _, cand := range validCandidates {
			subIdx := len(*entries)
			absP := cand.absPath
			subEntry := ReposEntry{
				Index:        subIdx,
				Path:         cand.relPath,
				ResolvedPath: &absP,
				Origin:       "submodule",
				Parent:       &parentIdx,
				Discovery: ReposDiscovery{
					Requested: false,
					Status:    "not-applicable",
					Reason:    "discovery is not recursive",
				},
				Notes: []string{},
			}
			seenPaths[filepath.Clean(absP)] = true

			if cand.isSkipped {
				subEntry.Status = "skipped"
				subEntry.Error = &ReposError{
					Code:    cand.errCode,
					Message: cand.errMsg,
				}
				*inspections = append(*inspections, Result{Status: "error"})
			} else if cand.isError {
				subEntry.Status = "error"
				subEntry.Error = &ReposError{
					Code:    cand.errCode,
					Message: cand.errMsg,
				}
				*inspections = append(*inspections, Result{Status: "error"})
			} else {
				// Stage S4: Identity inspection of discovered submodule candidate
				insp := inspectFn(ctx, absP)
				*inspections = append(*inspections, insp)
				subEntry.Backend = insp.Backend
				if insp.Status == "error" {
					subEntry.Status = "error"
					code := "inspection-failed"
					msg := "identity inspection failed"
					if len(insp.Diagnostics) > 0 {
						code = insp.Diagnostics[0].Code
						msg = insp.Diagnostics[0].Message
					}
					subEntry.Error = &ReposError{Code: code, Message: msg}
				} else {
					// FR-008: After the identity inspection, its worktree must equal the expected path
					wtMatches := false
					if insp.Repository != nil && insp.Repository.Worktree != nil {
						if filepath.Clean(*insp.Repository.Worktree) == filepath.Clean(absP) {
							wtMatches = true
						}
					}
					if !wtMatches {
						subEntry.Status = "error"
						subEntry.Error = &ReposError{
							Code:    ReposCodePathUnsafe,
							Message: "submodule worktree does not match expected path",
						}
					} else {
						subEntry.Status = "ok"
					}
				}
			}

			*entries = append(*entries, subEntry)
		}
	}
}

func isLexicallyValidSubmodulePath(p string) bool {
	if !utf8.ValidString(p) || len(p) == 0 || len(p) > ReposMaxPathBytes {
		return false
	}
	if strings.HasPrefix(p, "-") || strings.HasPrefix(p, "/") {
		return false
	}
	if strings.HasSuffix(p, "/") || strings.ContainsRune(p, 0) {
		return false
	}
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
