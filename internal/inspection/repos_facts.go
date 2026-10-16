// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"path/filepath"
	"strings"
)

// deriveFacts derives ReposFacts from the embedded inventory of an entry.
// Only called when an entry has an inventory and its status is ok or partial.
func deriveFacts(entry *ReposEntry) *ReposFacts {
	if entry.Inventory == nil || entry.Status == "error" || entry.Status == "skipped" {
		return nil
	}
	inv := entry.Inventory
	facts := &ReposFacts{
		Dirty: ReposDirty{
			Value: "unknown",
		},
	}

	// 1. Head state & refs
	switch entry.Backend {
	case "git":
		if inv.Identity.Repository != nil {
			head := inv.Identity.Repository.Head
			facts.HeadState = head.State
			facts.HeadRef = head.Ref
			facts.HeadCommitID = head.OID
		}
	case "jj":
		facts.HeadState = "jj-workspace"
		if inv.Identity.JJRepository != nil {
			if inv.Identity.JJRepository.CommitID != "" {
				cid := inv.Identity.JJRepository.CommitID
				facts.HeadCommitID = &cid
			}
			if inv.Identity.JJRepository.ChangeID != "" {
				chg := inv.Identity.JJRepository.ChangeID
				facts.ChangeID = &chg
			}
		}
	}

	// 2. Root workspace & dirty state
	var rootWs *InventoryWorkspace
	if entry.ResolvedPath != nil {
		cleanResolved := filepath.Clean(*entry.ResolvedPath)
		for i := range inv.Workspaces {
			ws := &inv.Workspaces[i]
			if ws.Path != nil && filepath.Clean(*ws.Path) == cleanResolved {
				rootWs = ws
				break
			}
		}
	}

	if rootWs == nil {
		facts.Dirty.Value = "unknown"
		if entry.Origin == "submodule" {
			facts.Dirty.Reason = "submodule checkout has no matching workspace record"
		} else if inv.Identity.Repository != nil && inv.Identity.Repository.Bare {
			facts.Dirty.Reason = "bare repository has no root workspace"
		} else {
			facts.Dirty.Reason = "no workspace matched the repository root"
		}
	} else {
		if entry.Backend == "jj" {
			facts.Dirty.Value = "unknown"
			reason := "disk freshness needs a snapshot"
			if len(rootWs.Limitations) > 0 {
				reason = rootWs.Limitations[0]
			}
			facts.Dirty.Reason = reason
		} else { // Git
			if rootWs.Tracked.Value == "true" || rootWs.Untracked.Value == "true" {
				facts.Dirty.Value = "true"
				facts.Dirty.Reason = ""
			} else if rootWs.Tracked.Value == "false" && rootWs.Untracked.Value == "false" {
				facts.Dirty.Value = "false"
				facts.Dirty.Reason = ""
			} else {
				facts.Dirty.Value = "unknown"
				if rootWs.Tracked.Reason != "" {
					facts.Dirty.Reason = rootWs.Tracked.Reason
				} else {
					facts.Dirty.Reason = rootWs.Untracked.Reason
				}
			}
		}
	}

	// 3. Conflicted references
	for _, ref := range inv.References {
		if len(ref.AddedTargets) >= 2 || len(ref.RemovedTargets) > 0 {
			facts.ConflictedReferences++
		}
	}

	// 4. Workspaces counts
	facts.Workspaces.Total = len(inv.Workspaces)
	for _, ws := range inv.Workspaces {
		switch ReposWorkspaceKind(ws, entry.Backend) {
		case "stale":
			facts.Workspaces.Stale++
		case "foreign":
			facts.Workspaces.Foreign++
		case "unverified":
			facts.Workspaces.Unverified++
		}
	}

	return facts
}

// ReposWorkspaceKind classifies one workspace record of an entry with the given backend for the facts and
// for the text rendering, so both count the same: "stale" (registration missing), "foreign" (a manager
// other than the entry's backend; a record without manager is never foreign), "unverified" (registration
// unknown and not foreign) or "" (nothing to report).
func ReposWorkspaceKind(ws InventoryWorkspace, backend string) string {
	switch {
	case ws.Registration == "missing":
		return "stale"
	case ws.Manager != "" && ws.Manager != backend:
		return "foreign"
	case ws.Registration == "unknown":
		return "unverified"
	}
	return ""
}

// deriveSummary computes the aggregated ReposSummary across all entries.
func deriveSummary(entries []ReposEntry) ReposSummary {
	summary := ReposSummary{
		Entries: len(entries),
	}
	for _, e := range entries {
		switch e.Status {
		case "ok":
			summary.ByStatus.Ok++
		case "partial":
			summary.ByStatus.Partial++
		case "error":
			summary.ByStatus.Error++
		case "skipped":
			summary.ByStatus.Skipped++
		}

		switch e.Backend {
		case "git":
			summary.ByBackend.Git++
		case "jj":
			summary.ByBackend.JJ++
		default:
			summary.ByBackend.Other++
		}

		if e.Origin == "submodule" && e.Status == "skipped" {
			summary.SkippedUninitialized++
		}
		if e.Origin == "explicit" && e.Discovery.Status == "not-applicable" {
			summary.DiscoveryNotApplicable++
		}

		if e.Facts != nil {
			switch e.Facts.Dirty.Value {
			case "true":
				summary.Dirty.True++
			case "false":
				summary.Dirty.False++
			case "unknown":
				summary.Dirty.Unknown++
			}
			summary.ConflictedReferences += e.Facts.ConflictedReferences
			summary.StaleWorkspaces += e.Facts.Workspaces.Stale
			summary.ForeignWorkspaces += e.Facts.Workspaces.Foreign
			summary.UnverifiedWorkspaces += e.Facts.Workspaces.Unverified
		}
	}
	return summary
}

// scrubReposStrings makes every string this operation composes valid UTF-8 and at most ReposMessageLimit
// bytes. Paths are validated earlier; messages and notes may carry repository text, and one of them must
// never make the whole result unencodable. Embedded inventories are not touched.
func scrubReposStrings(res *ReposResult) {
	for i := range res.Diagnostics {
		res.Diagnostics[i].Message = reposClean(res.Diagnostics[i].Message)
	}
	for i := range res.Boundary.Limitations {
		res.Boundary.Limitations[i] = reposClean(res.Boundary.Limitations[i])
	}
	for i := range res.Entries {
		e := &res.Entries[i]
		e.Discovery.Reason = reposClean(e.Discovery.Reason)
		if e.Error != nil {
			e.Error.Message = reposClean(e.Error.Message)
		}
		for j := range e.Notes {
			e.Notes[j] = reposClean(e.Notes[j])
		}
	}
}

// applyOutputBudget ensures the result fits within ReposMaxOutputBytes (8 MiB). Only the size bound omits
// an embedded inventory: later inventories are dropped once the bound is reached, and if the content around
// the inventories alone exceeds it, every embedded inventory is dropped.
func applyOutputBudget(res *ReposResult) {
	scrubReposStrings(res)
	if err := ReposCheckJSONBudget(*res); err == nil {
		return
	}

	// Stash original inventories
	originalInventories := make([]*InventoryResult, len(res.Entries))
	for i := range res.Entries {
		originalInventories[i] = res.Entries[i].Inventory
		res.Entries[i].Inventory = nil
	}

	// Add inventories back in order while budget holds
	omitting := false
	for i := range res.Entries {
		entry := &res.Entries[i]
		orig := originalInventories[i]
		if orig == nil {
			continue
		}
		if omitting {
			omitEntryInventory(entry, "embedded inventory omitted to respect result size limit")
			continue
		}
		entry.Inventory = orig
		if err := ReposCheckJSONBudget(*res); err != nil {
			entry.Inventory = nil
			if isReposEncodingError(err) {
				omitEntryInventory(entry, "embedded inventory omitted because it cannot be encoded as JSON")
				continue
			}
			omitting = true
			omitEntryInventory(entry, "embedded inventory omitted to respect result size limit")
		}
	}
	reposRefreshAfterOmission(res)

	// Final re-check: the content around the inventories alone exceeds the bound, so no inventory stays.
	if err := ReposCheckJSONBudget(*res); err != nil {
		for i := range res.Entries {
			entry := &res.Entries[i]
			if entry.Inventory != nil {
				omitEntryInventory(entry, "embedded inventory omitted to respect result size limit")
			}
		}
		reposRefreshAfterOmission(res)
		res.Status = "error"
		res.Boundary.Complete = false
	}
}

func reposRefreshAfterOmission(res *ReposResult) {
	res.Summary = deriveSummary(res.Entries)
	res.Summary.InventoriesOmitted = countOmittedInventories(res.Entries)
	res.Status = ReposOverallStatus(res.Entries)
	res.Boundary.Complete = res.Status == "ok"
}

// omitEntryInventory drops an embedded inventory. The entry has no usable inventory any more, so it is an
// error entry; the output-limit cause follows the precedence rule, and the loser becomes a note formatted
// "<code>: <message>". Facts stay (they were derived before the omission).
func omitEntryInventory(entry *ReposEntry, message string) {
	entry.Inventory = nil
	limit := ReposError{Code: ReposCodeOutputLimit, Message: message}
	entry.Status = "error"
	if entry.Error == nil {
		entry.Error = &limit
		return
	}
	primary, notes := ReposResolveCauses([]ReposError{*entry.Error, limit})
	entry.Error = &primary
	entry.Notes = append(entry.Notes, notes...)
}

func countOmittedInventories(entries []ReposEntry) int {
	count := 0
	for _, e := range entries {
		if e.Error != nil && e.Error.Code == ReposCodeOutputLimit {
			count++
			continue
		}
		for _, n := range e.Notes {
			if strings.HasPrefix(n, ReposCodeOutputLimit+": ") {
				count++
				break
			}
		}
	}
	return count
}
