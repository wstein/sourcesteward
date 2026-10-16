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

// reposBounds are the normative deadlines as durations. Production uses defaultReposBounds; tests pass
// shorter ones through reposWithBounds so that an expired deadline needs no minute of waiting.
type reposBounds struct{ total, call time.Duration }

var defaultReposBounds = reposBounds{total: ReposTotalSeconds * time.Second, call: ReposPerRepositorySeconds * time.Second}

// reposSubmoduleDirtyNote qualifies the embedded inventory of a submodule (US3-6): Git reports a
// submodule's Git directory, not its checkout, as its worktree.
const (
	reposSamplingPrefix  = "Sampled observations are not transactional"
	reposMaxPartialNotes = 16
)

const reposSubmoduleDirtyNote = "submodule: dirty is unknown, because Git reports the submodule's Git directory, not its checkout, as its worktree"

// Repos executes the multi-repository inventory operation across explicit and discovered members.
func Repos(ctx context.Context, o ReposOptions) ReposResult {
	return reposWithRunner(ctx, o, Inspect, Inventory, newReposRunner)
}

func repos(ctx context.Context, o ReposOptions, inspectFn inspectFunc, inventoryFn inventoryFunc) ReposResult {
	return reposWithRunner(ctx, o, inspectFn, inventoryFn, newReposRunner)
}

func reposWithRunner(ctx context.Context, o ReposOptions, inspectFn inspectFunc, inventoryFn inventoryFunc, newRunner reposNewRunner) ReposResult {
	return reposWithBounds(ctx, o, inspectFn, inventoryFn, newRunner, defaultReposBounds)
}

func reposWithBounds(ctx context.Context, o ReposOptions, inspectFn inspectFunc, inventoryFn inventoryFunc, newRunner reposNewRunner, b reposBounds) ReposResult {
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

	totalCtx, totalCancel := context.WithTimeout(ctx, b.total)
	defer totalCancel()

	// Stage S1: Structural validation of explicit paths
	entries := prepareExplicitEntries(o.Paths, o.Submodules)

	// Stage S2: Identity inspection (Inspect) of explicit entries, parallel (max 4 workers)
	inspections := inspectExplicitParallel(ctx, totalCtx, entries, inspectFn, b)

	// Stage S3: Deterministic validation of explicit entries (root equality, duplicates)
	ids := validateExplicitEntries(entries, inspections)
	computeContainment(entries)

	// Stage S4: Submodule discovery for surviving Git parents, then identity inspection of the added
	// entries through the same pool and per-call deadlines as the explicit ones.
	if o.Submodules {
		pending := discoverSubmodules(ctx, totalCtx, &entries, &inspections, newRunner, b)
		indices := make([]int, len(pending))
		for i, p := range pending {
			indices[i] = p.index
		}
		inspectIndices(ctx, totalCtx, entries, inspections, indices, inspectFn, b)
		verifyDiscoveredSubmodules(entries, inspections, pending, ids)
		computeContainment(entries)
	}

	// Stage S5: Inventory of surviving entries, parallel (max 4 workers)
	inventoryEntriesParallel(ctx, totalCtx, entries, inspections, inventoryFn, b)

	// Stage S6: Derive facts & summary
	for i := range entries {
		if entries[i].Inventory != nil && (entries[i].Status == "ok" || entries[i].Status == "partial") {
			entries[i].Facts = deriveFacts(&entries[i])
			if entries[i].Origin == "submodule" {
				entries[i].Notes = append(entries[i].Notes, reposSubmoduleDirtyNote)
			}
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
				"Submodule locations are confined by the submodule pin audit's checks: a submodule whose Git directory has alternates or a commondir entry, or that is not owned by the effective user or is writable by others, is refused with path-unsafe.",
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

// mapTimeoutError maps an inherited cancellation or timeout diagnostic to the repos codes (FR-012): the
// caller's own cancellation or deadline stays `canceled`, a fired total deadline and a fired per-call
// deadline are `inventory-timeout` with the bound that fired. Any other code is inherited unchanged.
func mapTimeoutError(callerCtx context.Context, totalCtx context.Context, callCtx context.Context, diag Diagnostic) (code string, message string) {
	if diag.Code != "canceled" && diag.Code != "jj-timeout" && diag.Code != ReposCodeInventoryTimeout {
		return diag.Code, diag.Message
	}
	if callerCtx.Err() != nil || totalCtx.Err() != nil {
		code, message = reposMapError(callerCtx, totalCtx, nil)
		return code, message
	}
	return ReposCodeInventoryTimeout, fmt.Sprintf("per-call deadline of %d seconds exceeded", ReposPerRepositorySeconds)
}

func inspectExplicitParallel(callerCtx context.Context, totalCtx context.Context, entries []ReposEntry, inspectFn inspectFunc, b reposBounds) []Result {
	inspections := make([]Result, len(entries))
	var validIndices []int
	for i := range entries {
		if entries[i].Status != "error" && entries[i].ResolvedPath != nil {
			validIndices = append(validIndices, i)
		}
	}
	inspectIndices(callerCtx, totalCtx, entries, inspections, validIndices, inspectFn, b)
	return inspections
}

// inspectIndices runs the identity inspection of the given entries on the bounded pool, each under its own
// per-call deadline inside the total, with panic recovery and timeout mapping.
func inspectIndices(callerCtx context.Context, totalCtx context.Context, entries []ReposEntry, inspections []Result, validIndices []int, inspectFn inspectFunc, b reposBounds) {
	runWorkerPool(validIndices, ReposParallelism, func(i int) {
		entry := &entries[i]

		if callerCtx.Err() != nil {
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

		callCtx, callCancel := context.WithTimeout(totalCtx, b.call)
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
}

// reposIdentityIndex remembers the native repository identity of every admitted entry in expansion order,
// so a later entry for the same repository (Git common directory; jj repository directory) is a duplicate.
type reposIdentityIndex struct{ seen map[string]int }

func nativeIdentity(insp Result) string {
	switch insp.Backend {
	case "git":
		if insp.Repository != nil {
			return "git:" + filepath.Clean(insp.Repository.CommonDir)
		}
	case "jj":
		if insp.JJRepository != nil {
			return "jj:" + filepath.Clean(insp.JJRepository.RepositoryDir)
		}
	}
	return ""
}

// admit records entry's identity, or marks it a duplicate of the earlier entry with the same identity.
func (x *reposIdentityIndex) admit(entry *ReposEntry, insp Result) {
	id := nativeIdentity(insp)
	if id == "" {
		return
	}
	if prev, exists := x.seen[id]; exists {
		entry.Status = "error"
		entry.Error = &ReposError{Code: ReposCodeDuplicateRepository, Message: "same repository as an earlier entry"}
		entry.DuplicateOf = &prev
		return
	}
	x.seen[id] = entry.Index
}

func validateExplicitEntries(entries []ReposEntry, inspections []Result) *reposIdentityIndex {
	ids := &reposIdentityIndex{seen: make(map[string]int)}

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
		ids.admit(entry, insp)
	}
	return ids
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

// reposRaise records a cause that makes the entry an error. An earlier cause (a failed discovery, say) is not
// overwritten: the causes go through the precedence rule and the loser becomes a note (FR-012).
func reposRaise(entry *ReposEntry, e ReposError) {
	entry.Status = "error"
	if entry.Error == nil {
		entry.Error = &e
		return
	}
	primary, notes := ReposResolveCauses([]ReposError{*entry.Error, e})
	entry.Error = &primary
	entry.Notes = append(entry.Notes, notes...)
}

func inventoryEntriesParallel(callerCtx context.Context, totalCtx context.Context, entries []ReposEntry, inspections []Result, inventoryFn inventoryFunc, b reposBounds) {
	var validIndices []int
	for i := range entries {
		if entries[i].Status != "error" && entries[i].Status != "skipped" && entries[i].ResolvedPath != nil {
			validIndices = append(validIndices, i)
		}
	}

	runWorkerPool(validIndices, ReposParallelism, func(i int) {
		entry := &entries[i]

		if callerCtx.Err() != nil {
			reposRaise(entry, ReposError{Code: ReposCodeCanceled, Message: "operation canceled before start"})
			return
		}
		if totalCtx.Err() != nil {
			reposRaise(entry, ReposError{Code: ReposCodeInventoryNotStarted, Message: "deadline exceeded before inventory started"})
			return
		}

		defer func() {
			if r := recover(); r != nil {
				reposRaise(entry, ReposError{Code: ReposCodeMemberFailed, Message: fmt.Sprintf("worker panic during inventory: %v", r)})
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

		callCtx, callCancel := context.WithTimeout(totalCtx, b.call)
		defer callCancel()

		inv := inventoryFn(callCtx, *entry.ResolvedPath, opts)

		// Compare identity if both identities have status "ok"
		if i < len(inspections) && inspections[i].Status == "ok" && inv.Identity.Status == "ok" {
			if !verifyIdentityUnchanged(inspections[i], inv) {
				reposRaise(entry, ReposError{Code: ReposCodeChangedRepository, Message: "repository identity changed during inventory"})
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
			// Limitation 0 is the fixed sampling statement every inventory carries; it says nothing
			// about why this one is partial. The message is the first remaining limitation that is not
			// a base-candidate remark (informational on ordinary clones).
			lims := inv.Boundary.Limitations
			if len(lims) > 0 && strings.HasPrefix(lims[0], reposSamplingPrefix) {
				lims = lims[1:]
			}
			msg := "inventory status partial"
			for _, lim := range lims {
				if !strings.HasPrefix(lim, "multiple base candidates") {
					msg = lim
					break
				}
			}
			msg = reposClean(msg)
			invErr := ReposError{
				Code:    ReposCodeInventoryPartial,
				Message: msg,
			}
			for i, lim := range lims {
				if i == reposMaxPartialNotes {
					entry.Notes = append(entry.Notes, fmt.Sprintf("inventory-partial: %d more limitations omitted", len(lims)-i))
					break
				}
				entry.Notes = append(entry.Notes, reposClean("inventory-partial: "+lim))
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
