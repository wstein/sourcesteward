// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// RegistrationPruneRisk annotates native admission outside the integration
// ladder. It never supplies proof, selects a row or prepares recovery. Caller
// establishes the displayed scope; agent selection keeps its separate own gate.
func RegistrationPruneRisk(row RegistrationPrune, caller string) risk.Assessment {
	if row.Verdict == RegistrationPruneRetain && (row.Reason == "registration-prune-ownership-unproven" || row.Reason == "registration-prune-absence-unproven") {
		return risk.Assessment{Level: risk.Kept, Reasons: []string{}}
	}
	if row.Repo != "" && row.Name != "" && row.GitDir != "" && row.Destination != "" && row.Task != "" && (row.Scope == owner.ScopeOwn || row.Scope == owner.ScopeOther) && len(SelectRegistrationPrunes([]RegistrationPrune{row}, Config{Caller: caller})) == 1 {
		return risk.Assessment{Level: risk.Orange, Reasons: []string{risk.ReasonOwnedMissingRegistration}}
	}
	return risk.Assessment{Level: risk.Red, Reasons: []string{risk.ReasonNotJudged}}
}
