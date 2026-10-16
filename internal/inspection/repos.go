// SPDX-License-Identifier: EUPL-1.2
package inspection

import "context"

// Repos executes the multi-repository inventory operation across explicit and discovered members.
func Repos(ctx context.Context, o ReposOptions) ReposResult {
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
		}
	}
	return ReposResult{
		SchemaVersion:       1,
		Command:             "repos",
		Status:              "ok",
		InputPaths:          o.Paths,
		SubmodulesRequested: o.Submodules,
		Boundary: ReposBoundary{
			Complete:  true,
			Freshness: "sampled-nontransactional",
			Effects:   ReposDefaultEffects(),
			Limits:    ReposDefaultLimits(),
		},
		Entries: []ReposEntry{},
	}
}
