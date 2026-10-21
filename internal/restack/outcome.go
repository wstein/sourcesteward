// SPDX-License-Identifier: EUPL-1.2
package restack

// decideOutcome is the decision table of the contract, a pure function of the result: the first
// matching row wins and exactly one row matches every input. evidence-clean is the positive list
// of row 3 only; any state the table does not name falls to needs-attention. The outcome names what
// the evidence shows; it never authorizes anything.
func decideOutcome(r *Result) string {
	switch {
	case r.Status == StatusIncomplete || r.Status == StatusError: // row 1
		return OutcomeNone
	case r.Evidence != nil && r.Evidence.Replay.State == "conflict": // row 2
		return OutcomeConflicts
	case allClean(r): // row 3
		return OutcomeClean
	}
	return OutcomeAttention // row 4
}

func allClean(r *Result) bool {
	e := r.Evidence
	if e == nil || e.Replay.State != "clean" || r.Input.Moved.Count < 1 {
		return false
	}
	n := r.Input.Moved.Count
	if len(e.RangePairing.Rows) != n || len(e.OwnDiffEquality.Rows) != n {
		return false
	}
	for _, row := range e.RangePairing.Rows {
		if row.Mark != "=" {
			return false
		}
	}
	for _, row := range e.OwnDiffEquality.Rows {
		if row.State != "equal" {
			return false
		}
	}
	for _, w := range r.Effects.Worktrees {
		if w.State == "holds-branch" {
			return false
		}
	}
	return e.ParentDropped.AllDropped && len(e.EmptyAfterReplay) == 0 &&
		e.Identity.State == "pass" && e.Identity.Source == "flag" &&
		e.Overlap.Indicator == "none" && len(r.Effects.AffectedRefs) == 0
}
