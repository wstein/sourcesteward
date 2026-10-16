// SPDX-License-Identifier: EUPL-1.2
package inspection

import "context"

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
				Freshness: "sampled-nontransactional",
				Effects:   ReposDefaultEffects(),
				Limits:    ReposDefaultLimits(),
			},
			Diagnostics: []Diagnostic{{Code: "invalid-options", Message: err.Error()}},
			Entries:     []ReposEntry{},
		}
	}

	entries := prepareExplicitEntries(o.Paths, o.Submodules)
	inspectExplicitEntries(ctx, entries, inspectFn)
	computeContainment(entries)
	inventoryEntries(ctx, entries, inventoryFn)

	if o.Submodules {
		discoverSubmodules(ctx, &entries, inspectFn, inventoryFn, run)
		computeContainment(entries)
	}

	overallStatus := ReposOverallStatus(entries)

	return ReposResult{
		SchemaVersion:       1,
		Command:             "repos",
		Status:              overallStatus,
		InputPaths:          o.Paths,
		SubmodulesRequested: o.Submodules,
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
		Entries: entries,
	}
}
