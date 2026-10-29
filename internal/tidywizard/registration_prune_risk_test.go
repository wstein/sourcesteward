// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

func TestRegistrationPruneRiskKeepsSelectionIndependent(t *testing.T) {
	own := selectionPruneRow("own")
	other := selectionPruneRow("other")
	other.Owner = "claude"
	other.Scope = owner.ScopeOther
	for _, row := range []RegistrationPrune{own, other} {
		if got := RegistrationPruneRisk(row, "codex"); got.Level != risk.Orange || len(got.Reasons) != 1 || got.Reasons[0] != risk.ReasonOwnedMissingRegistration {
			t.Fatalf("native assessment=%+v", got)
		}
		// Display is the same in agent mode; the strict selector still excludes other.
		got := SelectRegistrationPrunes([]RegistrationPrune{row}, Config{Caller: "codex", Agent: true})
		if (len(got) == 1) != (row.Scope == owner.ScopeOwn) {
			t.Fatalf("agent selection=%+v", got)
		}
		row.Risk = "green"
		row.RiskReasons = []string{"proof-ancestor"}
		row.Proof = "ancestor"
		if got := RegistrationPruneRisk(row, "codex"); got.Level != risk.Red {
			t.Fatalf("display annotation supplied proof: %+v", got)
		}
		if len(SelectRegistrationPrunes([]RegistrationPrune{row}, Config{Caller: "codex"})) != 0 {
			t.Fatal("annotation authorized selection")
		}
	}
}
