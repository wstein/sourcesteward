// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Repos executes the multi-repository inventory operation across explicit and discovered members.
func Repos(ctx context.Context, o ReposOptions) ReposResult {
	return reposWithRunner(ctx, o, Inspect, Inventory, runGit)
}

func repos(ctx context.Context, o ReposOptions, inspectFn inspectFunc, inventoryFn inventoryFunc) ReposResult {
	return reposWithRunner(ctx, o, inspectFn, inventoryFn, runGit)
}

func reposWithRunner(ctx context.Context, o ReposOptions, inspectFn inspectFunc, inventoryFn inventoryFunc, run runner) ReposResult {
	if err := ValidateReposOptions(o); err != nil {
		return ReposResult{
			SchemaVersion: 1,
			Command:       "repos",
			Status:        "error",
			Boundary: ReposBoundary{
				Freshness:   "sampled-nontransactional",
				Effects:     ReposDefaultEffects(),
				Limits:      ReposDefaultLimits(),
				Limitations: []string{},
			},
			InputPaths:  []string{},
			Diagnostics: []Diagnostic{{Code: "invalid-options", Message: err.Error()}},
			Entries:     []ReposEntry{},
		}
	}

	totalCtx, totalCancel := context.WithTimeout(ctx, ReposTotalSeconds*time.Second)
	defer totalCancel()

	// Stage S1: Structural validation of explicit paths
	entries := prepareExplicitEntries(o.Paths, o.Submodules)

	// Stage S2: Identity inspection (Inspect) of explicit entries, parallel (max 4 workers)
	inspections := inspectExplicitParallel(ctx, totalCtx, entries, inspectFn)

	// Stage S3: Deterministic validation of explicit entries (root equality, duplicates)
	validateExplicitEntries(entries, inspections)
	computeContainment(entries)

	// Stage S4: Submodule discovery for surviving Git parents
	if o.Submodules {
		discoverSubmodules(totalCtx, &entries, &inspections, inspectFn, run)
		computeContainment(entries)
	}

	// Stage S5: Inventory of surviving entries, parallel (max 4 workers)
	inventoryEntriesParallel(ctx, totalCtx, entries, inspections, inventoryFn)

	// Stage S6: Derive facts & summary
	for i := range entries {
		if entries[i].Inventory != nil && (entries[i].Status == "ok" || entries[i].Status == "partial") {
			entries[i].Facts = deriveFacts(&entries[i])
		}
	}

	summary := deriveSummary(entries)
	overallStatus := ReposOverallStatus(entries)

	res := ReposResult{
		SchemaVersion:       1,
		Command:             "repos",
		Status:              overallStatus,
		InputPaths:          o.Paths,
		SubmodulesRequested: o.Submodules,
		Summary:             summary,
		Boundary: ReposBoundary{
			Complete:  overallStatus == "ok",
			Freshness: "sampled-nontransactional",
			Effects:   ReposDefaultEffects(),
			Limits:    ReposDefaultLimits(),
			Limitations: []string{
				"No ahead/behind, publication or task-ownership facts: single-repository inventory does not observe them.",
				"Submodule checkouts: dirty state is unknown (Git reports a submodule's Git directory as its worktree).",
				"Discovered submodule paths are checked before Git runs, but a path swapped for a symlink in between can be read before it is rejected.",
			},
		},
		Entries:     entries,
		Diagnostics: []Diagnostic{},
	}

	// Apply output budget (drops oversized inventories to output-limit)
	applyOutputBudget(&res)

	return res
}

func runWorkerPool(indices []int, limit int, fn func(idx int)) {
	if len(indices) == 0 {
		return
	}
	workerCount := limit
	if workerCount > len(indices) {
		workerCount = len(indices)
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	indicesChan := make(chan int, len(indices))
	for _, idx := range indices {
		indicesChan <- idx
	}
	close(indicesChan)

	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range indicesChan {
				fn(idx)
			}
		}()
	}
	wg.Wait()
}

func mapTimeoutError(callerCtx context.Context, totalCtx context.Context, callCtx context.Context, diag Diagnostic) (code string, message string) {
	if diag.Code != "canceled" && diag.Code != "jj-timeout" && diag.Code != ReposCodeInventoryTimeout {
		return diag.Code, diag.Message
	}
	// Caller explicitly canceled
	if callerCtx.Err() != nil && callerCtx.Err() == context.Canceled {
		return ReposCodeCanceled, "caller canceled operation"
	}
	// Total deadline fired
	if totalCtx.Err() != nil {
		return ReposCodeInventoryTimeout, fmt.Sprintf("total deadline of %d seconds exceeded", ReposTotalSeconds)
	}
	// Per-call deadline fired
	if callCtx.Err() != nil && callCtx.Err() == context.DeadlineExceeded {
		return ReposCodeInventoryTimeout, fmt.Sprintf("per-call deadline of %d seconds exceeded", ReposPerRepositorySeconds)
	}
	return ReposCodeInventoryTimeout, fmt.Sprintf("per-call deadline of %d seconds exceeded", ReposPerRepositorySeconds)
}

func inspectExplicitParallel(callerCtx context.Context, totalCtx context.Context, entries []ReposEntry, inspectFn inspectFunc) []Result {
	inspections := make([]Result, len(entries))
	var validIndices []int
	for i := range entries {
		if entries[i].Status != "error" && entries[i].ResolvedPath != nil {
			validIndices = append(validIndices, i)
		}
	}

	runWorkerPool(validIndices, ReposParallelism, func(i int) {
		entry := &entries[i]

		if callerCtx.Err() != nil && callerCtx.Err() == context.Canceled {
			entry.Status = "error"
			entry.Error = &ReposError{Code: ReposCodeCanceled, Message: "operation canceled before start"}
			return
		}
		if totalCtx.Err() != nil {
			entry.Status = "error"
			entry.Error = &ReposError{Code: ReposCodeInventoryNotStarted, Message: "deadline exceeded before identity inspection started"}
			return
		}

		defer func() {
			if r := recover(); r != nil {
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    ReposCodeMemberFailed,
					Message: fmt.Sprintf("worker panic during identity inspection: %v", r),
				}
			}
		}()

		callCtx, callCancel := context.WithTimeout(totalCtx, ReposPerRepositorySeconds*time.Second)
		defer callCancel()

		insp := inspectFn(callCtx, *entry.ResolvedPath)
		inspections[i] = insp
		if insp.Backend != "" {
			entry.Backend = insp.Backend
		}

		if insp.Status == "error" {
			entry.Status = "error"
			code := "inspection-failed"
			msg := "identity inspection failed"
			if len(insp.Diagnostics) > 0 {
				code, msg = mapTimeoutError(callerCtx, totalCtx, callCtx, insp.Diagnostics[0])
			}
			entry.Error = &ReposError{Code: code, Message: msg}
		}
	})

	return inspections
}

func validateExplicitEntries(entries []ReposEntry, inspections []Result) {
	seenNative := make(map[string]int)

	for i := range entries {
		entry := &entries[i]
		if entry.Status == "error" || entry.ResolvedPath == nil {
			continue
		}
		insp := inspections[i]
		if insp.Status == "error" {
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

func verifyIdentityUnchanged(prior Result, inv InventoryResult) bool {
	if inv.Identity.Status != "ok" {
		return true
	}
	if prior.Backend != inv.Identity.Backend {
		return false
	}
	switch prior.Backend {
	case "git":
		if prior.Repository == nil || inv.Identity.Repository == nil {
			return false
		}
		if filepath.Clean(prior.Repository.GitDir) != filepath.Clean(inv.Identity.Repository.GitDir) {
			return false
		}
		if filepath.Clean(prior.Repository.CommonDir) != filepath.Clean(inv.Identity.Repository.CommonDir) {
			return false
		}
		if (prior.Repository.Worktree == nil) != (inv.Identity.Repository.Worktree == nil) {
			return false
		}
		if prior.Repository.Worktree != nil && filepath.Clean(*prior.Repository.Worktree) != filepath.Clean(*inv.Identity.Repository.Worktree) {
			return false
		}
		// Compare HEAD OID
		if (prior.Repository.Head.OID == nil) != (inv.Identity.Repository.Head.OID == nil) {
			return false
		}
		if prior.Repository.Head.OID != nil && *prior.Repository.Head.OID != *inv.Identity.Repository.Head.OID {
			return false
		}
		// Compare HEAD Ref
		if (prior.Repository.Head.Ref == nil) != (inv.Identity.Repository.Head.Ref == nil) {
			return false
		}
		if prior.Repository.Head.Ref != nil && *prior.Repository.Head.Ref != *inv.Identity.Repository.Head.Ref {
			return false
		}
		return true
	case "jj":
		if prior.JJRepository == nil || inv.Identity.JJRepository == nil {
			return false
		}
		if filepath.Clean(prior.JJRepository.WorkspaceRoot) != filepath.Clean(inv.Identity.JJRepository.WorkspaceRoot) {
			return false
		}
		if filepath.Clean(prior.JJRepository.RepositoryDir) != filepath.Clean(inv.Identity.JJRepository.RepositoryDir) {
			return false
		}
		if prior.JJRepository.OperationID != inv.Identity.JJRepository.OperationID {
			return false
		}
		return true
	default:
		return true
	}
}

func inventoryEntriesParallel(callerCtx context.Context, totalCtx context.Context, entries []ReposEntry, inspections []Result, inventoryFn inventoryFunc) {
	var validIndices []int
	for i := range entries {
		if entries[i].Status != "error" && entries[i].Status != "skipped" && entries[i].ResolvedPath != nil {
			validIndices = append(validIndices, i)
		}
	}

	runWorkerPool(validIndices, ReposParallelism, func(i int) {
		entry := &entries[i]

		if callerCtx.Err() != nil && callerCtx.Err() == context.Canceled {
			entry.Status = "error"
			entry.Error = &ReposError{Code: ReposCodeCanceled, Message: "operation canceled before start"}
			return
		}
		if totalCtx.Err() != nil {
			entry.Status = "error"
			entry.Error = &ReposError{Code: ReposCodeInventoryNotStarted, Message: "deadline exceeded before inventory started"}
			return
		}

		defer func() {
			if r := recover(); r != nil {
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    ReposCodeMemberFailed,
					Message: fmt.Sprintf("worker panic during inventory: %v", r),
				}
			}
		}()

		var opts InventoryOptions
		switch entry.Backend {
		case "git":
			opts = InventoryOptions{}
		case "jj":
			opts = InventoryOptions{Scopes: []string{"stored"}}
		default:
			return
		}

		callCtx, callCancel := context.WithTimeout(totalCtx, ReposPerRepositorySeconds*time.Second)
		defer callCancel()

		inv := inventoryFn(callCtx, *entry.ResolvedPath, opts)

		// Compare identity if both identities have status "ok"
		if i < len(inspections) && inspections[i].Status == "ok" && inv.Identity.Status == "ok" {
			if !verifyIdentityUnchanged(inspections[i], inv) {
				entry.Status = "error"
				entry.Error = &ReposError{
					Code:    ReposCodeChangedRepository,
					Message: "repository identity changed during inventory",
				}
				entry.Inventory = nil
				entry.Facts = nil
				return
			}
		}

		entry.Inventory = &inv

		switch inv.Status {
		case "ok":
			if entry.Error == nil {
				entry.Status = "ok"
			}
		case "partial":
			msg := "inventory status partial"
			if len(inv.Boundary.Limitations) > 0 {
				for _, lim := range inv.Boundary.Limitations {
					if !strings.HasPrefix(lim, "multiple base candidates") {
						msg = lim
						break
					}
				}
			}
			invErr := ReposError{
				Code:    ReposCodeInventoryPartial,
				Message: msg,
			}
			for _, lim := range inv.Boundary.Limitations {
				entry.Notes = append(entry.Notes, "inventory-partial: "+lim)
			}
			if entry.Error != nil {
				primary, notes := ReposResolveCauses([]ReposError{*entry.Error, invErr})
				entry.Error = &primary
				entry.Notes = append(entry.Notes, notes...)
			} else {
				entry.Status = "partial"
				entry.Error = &invErr
			}
		default: // "error"
			code := "inventory-failed"
			msg := "inventory failed"
			if len(inv.Diagnostics) > 0 {
				code, msg = mapTimeoutError(callerCtx, totalCtx, callCtx, inv.Diagnostics[0])
			}
			invErr := ReposError{
				Code:    code,
				Message: msg,
			}
			entry.Status = "error"
			if entry.Error != nil {
				primary, notes := ReposResolveCauses([]ReposError{*entry.Error, invErr})
				entry.Error = &primary
				entry.Notes = append(entry.Notes, notes...)
			} else {
				entry.Error = &invErr
			}
		}
	})
}
