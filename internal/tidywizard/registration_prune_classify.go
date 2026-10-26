// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// RegistrationPruneID uses the approved full identity and domain. Admission
// supplies exact native spellings; this function never normalizes them.
func RegistrationPruneID(repo, name, gitdir string) string {
	b, _ := json.Marshal([]string{"registration-prune", repo, name, gitdir})
	sum := sha256.Sum256(append([]byte("stw-registration-row-v4\n"), b...))
	return hex.EncodeToString(sum[:])
}

// ClassifyRegistrationPrune consumes complete read-only admission, never native
// prunable markers, branch integration, path hints or a guessed owner label.
func ClassifyRegistrationPrune(repo string, o workspace.RegistrationPruneV4Observation, cfg Config) RegistrationPrune {
	reg := o.Registration
	row := RegistrationPrune{ID: RegistrationPruneID(repo, reg.Name, reg.GitDir), Kind: "registration-prune", Repo: repo, Name: reg.Name, GitDir: reg.GitDir, Destination: reg.Destination, OwnerSource: owner.SourceUnknown, Scope: owner.ScopeUnknown, Verdict: RegistrationPruneRetain, Reason: "registration-prune-absence-unproven", Proof: "none", Effects: []string{}, Limitations: []string{}}
	if !o.Complete() {
		row.Reason, row.Verdict = registrationPruneRefusal(o.Diagnostic.Code)
		return row
	}
	own := owner.Resolve(owner.Item{TaskOwner: reg.Owner}, cfg.Caller, cfg.Agent)
	row.Owner, row.OwnerSource, row.Scope, row.Task = own.Label, own.Source, own.Scope, reg.Task
	row.Verdict, row.Reason, row.Proof = RegistrationPruneSafe, "registration-prune-ready", "owned-missing-registration"
	row.Effects = []string{"remove-registration"}
	row.EvidenceDigest = o.EvidenceDigest
	row.Limitations = append([]string{}, o.Limitations...)
	return row
}

func registrationPruneRefusal(code string) (string, RegistrationPruneVerdict) {
	switch code {
	case "prune-lock-refused", "prune-ownership-refused", "prune-ownership-unavailable", "prune-selection-refused", "prune-scope-refused", "task-not-found", "task-incomplete", "invalid-task", "owner-mismatch":
		return "registration-prune-ownership-unproven", RegistrationPruneRetain
	case "prune-parent-unavailable", "prune-absence-unavailable", "prune-mount-unavailable", "prune-destination-present", "prune-path-refused":
		return "registration-prune-absence-unproven", RegistrationPruneRetain
	case "prune-evidence-changed", "prune-absence-changed", "prune-mount-changed", "prune-parent-changed", "prune-registration-refused", "prune-pointer-refused", "prune-metadata-refused", "prune-repository-refused", "prune-repository-changed", "changed-ownership", "changed-state", "evidence-changed":
		return "registration-prune-changed", RegistrationPruneBlocked
	case "prune-sibling-refused":
		return "registration-prune-inventory-blocked", RegistrationPruneBlocked
	case "prune-recovery-refused", "prune-metadata-limit", "record-limit", "state-overlap", "invalid-state", "prune-store-refused":
		return "registration-prune-recovery-unavailable", RegistrationPruneBlocked
	case "unsupported-backend", "git-ownership-refused", "git-unavailable", "git-sandbox-unavailable", "git-layout-refused", "git-metadata-refused":
		return "registration-prune-unsupported", RegistrationPruneUnknown
	default:
		return "registration-prune-absence-unproven", RegistrationPruneRetain
	}
}

// SelectRegistrationPrunes is the safe selection shared by future --save and
// all-selection callers. It does not store plans or grant typed other-owner
// authorization. Scope is re-derived so an edited row label cannot grant own.
func SelectRegistrationPrunes(rows []RegistrationPrune, cfg Config) []RegistrationPrune {
	out := []RegistrationPrune{}
	seen := map[string]bool{}
	for _, row := range rows {
		scope := owner.Resolve(owner.Item{TaskOwner: row.Owner}, cfg.Caller, cfg.Agent)
		if row.Kind != "registration-prune" || row.Verdict != RegistrationPruneSafe || row.Reason != "registration-prune-ready" || row.Proof != "owned-missing-registration" || row.OwnerSource != owner.SourceTaskRecord || row.Owner == "" || row.Scope == owner.ScopeUnknown || row.Scope != scope.Scope || len(row.Effects) != 1 || row.Effects[0] != "remove-registration" || !registrationPruneDigest(row.EvidenceDigest) || cfg.Agent && (row.Scope != owner.ScopeOwn || cfg.Caller == "") {
			continue
		}
		if seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func registrationPruneDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func registrationPruneText(s string) bool { return len(s) <= 4096 && utf8.ValidString(s) }
