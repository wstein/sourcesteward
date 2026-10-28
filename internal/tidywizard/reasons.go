// SPDX-License-Identifier: EUPL-1.2

package tidywizard

// AllReasons lists every reason code a candidate can carry, in a stable order: the classifier's own
// keep reasons, the protections of the derivation, the closed list of tracking reasons and the
// native reasons of jj branches, plus `unregistered` and `locked-stw`, which are information on a
// safe row and the reason of the one decision row. The renderer needs a sentence and an action for
// each (Spec 044 SC-010). Each call owns its slice.
func AllReasons() []string {
	return []string{
		"unmerged", "dirty", "ignored-extras", "evidence-incomplete",
		"registration-record-missing", "registration-record-ambiguous", "locked-foreign", "locked-stw", "lock-mismatch",
		"unregistered", "primary-checkout", "current-worktree", "recently-active", "branch-attached-elsewhere",
		"jj-retirement-unsupported", "stale-registration-no-plan-kind",
		"retirement-parent-foreign", "legacy-owner-unknown", "unknown-workspace-path", "state-overlap",
		"remote-source-present", "symbolic-reference", "selected-base", "workspace-used",
		"remote-counterpart", "recovery-reference",
	}
}
