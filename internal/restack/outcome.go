// SPDX-License-Identifier: EUPL-1.2
package restack

func decideOutcome(r *Result) string {
	if r.Evidence != nil && r.Evidence.Replay.State == "conflict" {
		return OutcomeConflicts
	}
	return OutcomeAttention
}
