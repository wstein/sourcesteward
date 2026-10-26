// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
	"context"
	"sort"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// RegistrationPruneResult is the explicit opt-in source result. Incomplete
// means no returned row may authorize selection; notices contain fixed codes.
type RegistrationPruneResult struct {
	Rows       []RegistrationPrune
	Incomplete bool
	Notices    []string
}

type registrationPruneAdapter struct {
	inventory func(context.Context, string) workspace.RegistrationPruneV4Inventory
	observe   func(context.Context, workspace.RegistrationPruneV4Options) workspace.RegistrationPruneV4Observation
	readTask  func(string, string) (workspace.TaskRecord, bool, error)
}

// RegistrationPrunes enumerates bounded native missing registrations, joins a
// single original private store, then reuses complete Spec 011 admission. It
// performs no online integration proof, plan persistence or native mutation.
// cfg must agree with mode; viewing other owners does not authorize selection.
func RegistrationPrunes(ctx context.Context, repos []RepoSpec, stores []derive.Store, mode owner.Evidence, cfg Config) RegistrationPruneResult {
	return (registrationPruneAdapter{workspace.ObserveRegistrationPruneV4Inventory, workspace.ObserveRegistrationPruneV4, workspace.ReadTask}).run(ctx, repos, stores, mode, cfg)
}

func (a registrationPruneAdapter) run(ctx context.Context, repos []RepoSpec, stores []derive.Store, mode owner.Evidence, cfg Config) RegistrationPruneResult {
	out := RegistrationPruneResult{Rows: []RegistrationPrune{}, Notices: []string{}}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	nativeMode := workspace.V3RunMode{Mode: string(mode.Mode), StdinTTY: mode.StdinTTY, StdoutTTY: mode.StdoutTTY, Markers: mode.Markers, Mine: mode.Mine, ByOwner: mode.ByOwner, Caller: mode.Caller, CallerSource: mode.CallerSource}
	incomplete := func() {
		out.Incomplete = true
		if len(out.Notices) == 0 {
			out.Notices = append(out.Notices, "registration-prune-inventory-incomplete")
		}
	}
	if workspace.ValidateV3RunMode(&nativeMode) != nil || cfg.Caller != mode.Caller || cfg.Agent != (mode.Mode == owner.ModeAgent) || len(repos) > MaxCandidates || len(stores) > MaxCandidates {
		incomplete()
		return out
	}
	ordered := append([]RepoSpec{}, repos...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
	seenRepos := map[string]bool{}
	for _, repo := range ordered {
		if ctx.Err() != nil {
			incomplete()
			break
		}
		if !registrationPruneText(repo.Name) || repo.Name == "" || seenRepos[repo.Name] {
			incomplete()
			continue
		}
		seenRepos[repo.Name] = true
		if repo.Skip != "" || repo.Error != "" {
			incomplete()
			continue
		}
		inventory := a.inventory(ctx, repo.Path)
		if inventory.Status != "ok" {
			incomplete()
			continue
		}
		for _, candidate := range inventory.Candidates {
			if ctx.Err() != nil {
				incomplete()
				break
			}
			if len(out.Rows) >= MaxCandidates {
				incomplete()
				break
			}
			if !registrationPruneText(candidate.Name) || !registrationPruneText(candidate.GitDir) || !registrationPruneText(candidate.Destination) {
				incomplete()
				continue
			}
			obs := workspace.RegistrationPruneV4Observation{Status: "refused", Registration: workspace.PruneV4Registration{Name: candidate.Name, GitDir: candidate.GitDir, Destination: candidate.Destination}, Diagnostic: workspace.Diagnostic{Code: "prune-ownership-refused"}}
			if state, ok := a.uniqueStore(ctx, stores, candidate); ok {
				obs = a.observe(ctx, workspace.RegistrationPruneV4Options{Path: repo.Path, StateDir: state, Name: candidate.Name, GitDir: candidate.GitDir, Mode: nativeMode})
				// Refusals may have no registration. Preserve only exact inventory identity;
				// the classifier grants owner/task fields only after complete admission.
				if !obs.Complete() {
					obs.Registration = workspace.PruneV4Registration{Name: candidate.Name, GitDir: candidate.GitDir, Destination: candidate.Destination}
				}
			}
			row := ClassifyRegistrationPrune(repo.Name, obs, cfg)
			out.Rows = append(out.Rows, row)
		}
	}
	if ctx.Err() != nil {
		incomplete()
	}
	if out.Incomplete {
		for i := range out.Rows {
			if out.Rows[i].Verdict == RegistrationPruneSafe {
				out.Rows[i].Verdict = RegistrationPruneBlocked
				out.Rows[i].Reason = "registration-prune-inventory-blocked"
				out.Rows[i].Proof = "none"
				out.Rows[i].Effects = []string{}
				out.Rows[i].EvidenceDigest = ""
			}
		}
	}
	sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].ID < out.Rows[j].ID })
	return out
}

func (a registrationPruneAdapter) uniqueStore(ctx context.Context, stores []derive.Store, candidate workspace.RegistrationPruneV4Candidate) (string, bool) {
	if !validTask(candidate.Task) {
		return "", false
	}
	found := ""
	seen := map[string]bool{}
	for _, store := range stores {
		if ctx.Err() != nil || store.Issue != nil {
			return "", false
		}
		if seen[store.Path] {
			continue
		}
		seen[store.Path] = true
		rec, ok, err := a.readTask(store.Path, candidate.Task)
		if err != nil {
			return "", false
		}
		if !ok {
			continue
		}
		// Equal copies in different stores remain ambiguous for this new capability.
		if found != "" || rec.TaskID != candidate.Task || rec.Owner == "" || rec.Destination != candidate.Destination || rec.Native.GitDir != candidate.GitDir {
			return "", false
		}
		found = store.Path
	}
	return found, found != ""
}
