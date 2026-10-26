// SPDX-License-Identifier: EUPL-1.2

package tidywizard

import (
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// RiskFacts maps a classifier candidate to the facts of the Spec 044 risk ladder. It is the one
// place where the two vocabularies meet, and it only reads: it decides nothing and never changes
// the candidate. The classifier keeps no separate dirt, extras, primary or incomplete flag, so those
// facts are read from the reason it already gave; a fact the candidate does not carry stays false
// (not known), which the ladder treats conservatively.
//
// mergedPR is the number of a pull request merged into the base whose head equals the tip (the
// online evidence of Spec 044 C2); 0 means none or not looked up. The upstream-gone fact is not
// carried on the candidate yet and stays false.
func RiskFacts(c Candidate, mergedPR int) risk.Facts {
	return risk.Facts{
		Kind: string(c.Kind), Verdict: string(c.Verdict), Proof: string(c.Proof), Reason: c.Reason,
		Scope: c.Scope, OwnerSource: c.OwnerSource,
		// owner_source task-record is exactly "a creation record was found".
		HasCreationRecord: c.OwnerSource == owner.SourceTaskRecord,
		Primary:           c.Reason == "primary-checkout" || c.Reason == "current-worktree",
		DirectoryMissing:  c.Kind == KindStaleWorktree,
		Dirty:             c.Reason == "dirty",
		IgnoredExtras:     c.Reason == "ignored-extras",
		Incomplete:        c.Reason == "evidence-incomplete",
		NoWork:            c.Evidence == "no-work",
		MergedHead:        mergedPR > 0,
		LastActivity:      c.LastActivity,
	}
}
