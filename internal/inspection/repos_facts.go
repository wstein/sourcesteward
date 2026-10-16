// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"path/filepath"
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
		if ws.Registration == "missing" {
			facts.Workspaces.Stale++
		} else if ws.Manager != "" && ws.Manager != entry.Backend {
			facts.Workspaces.Foreign++
		} else if ws.Registration == "unknown" {
			facts.Workspaces.Unverified++
		}
	}

	return facts
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

// applyOutputBudget ensures the result fits within ReposMaxOutputBytes (8 MiB).
func applyOutputBudget(res *ReposResult) {
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
			omitEntryInventory(entry)
			continue
		}

		// Try including this inventory
		entry.Inventory = orig
		if err := ReposCheckJSONBudget(*res); err != nil {
			omitting = true
			omitEntryInventory(entry)
		}
	}

	// Recompute summary
	res.Summary = deriveSummary(res.Entries)
	res.Summary.InventoriesOmitted = countOmittedInventories(res.Entries)
	res.Status = ReposOverallStatus(res.Entries)
	res.Boundary.Complete = (res.Status == "ok")

	// Final re-check: if still failing budget, omit EVERY entry's inventory
	if err := ReposCheckJSONBudget(*res); err != nil {
		for i := range res.Entries {
			entry := &res.Entries[i]
			omitEntryInventory(entry)
		}
		res.Summary = deriveSummary(res.Entries)
		res.Summary.InventoriesOmitted = countOmittedInventories(res.Entries)
		res.Status = "error"
		res.Boundary.Complete = false
	}
}

func omitEntryInventory(entry *ReposEntry) {
	entry.Inventory = nil
	if entry.Status == "error" {
		entry.Notes = append(entry.Notes, ReposCodeOutputLimit)
	} else {
		entry.Status = "error"
		entry.Error = &ReposError{
			Code:    ReposCodeOutputLimit,
			Message: "embedded inventory omitted to respect result size limit",
		}
	}
}

func countOmittedInventories(entries []ReposEntry) int {
	count := 0
	for _, e := range entries {
		if e.Error != nil && e.Error.Code == ReposCodeOutputLimit {
			count++
		} else {
			for _, n := range e.Notes {
				if n == ReposCodeOutputLimit {
					count++
					break
				}
			}
		}
	}
	return count
}
