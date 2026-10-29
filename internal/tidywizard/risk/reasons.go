// SPDX-License-Identifier: EUPL-1.2
package risk

// Stable phase-1 and native-prune risk words cover every emitted reason.
const (
	ReasonProofAncestor            = "proof-ancestor"
	ReasonProofNoNetChange         = "proof-no-net-change"
	ReasonProofGitHubBranchAbsent  = "proof-github-branch-absent"
	ReasonNoOwnCommit              = "no-own-commit"
	ReasonPRMergedHead             = "pr-merged-head"
	ReasonProofEquivalent          = "proof-equivalent"
	ReasonRecentActivity           = "recent-activity"
	ReasonNobodyVouches            = "nobody-vouches"
	ReasonDecisionRow              = "decision-row"
	ReasonUpstreamGone             = "upstream-gone"
	ReasonNotIntegrated            = "not-integrated"
	ReasonUncommittedFiles         = "uncommitted-files"
	ReasonExtraFiles               = "extra-files"
	ReasonNotJudged                = "not-judged"
	ReasonOwnedMissingRegistration = "owned-missing-registration"
)

// AllReasons returns the complete phase-1 vocabulary in stable order. Each call
// owns its slice; callers cannot modify the catalog used by another renderer.
// The native-prune word is emitted by its separate adapter, never the ladder.
// Phase-2 words remain absent.
func AllReasons() []string {
	return []string{ReasonProofAncestor, ReasonProofNoNetChange, ReasonProofGitHubBranchAbsent, ReasonNoOwnCommit, ReasonPRMergedHead, ReasonProofEquivalent, ReasonRecentActivity, ReasonNobodyVouches, ReasonDecisionRow, ReasonUpstreamGone, ReasonNotIntegrated, ReasonUncommittedFiles, ReasonExtraFiles, ReasonNotJudged, ReasonOwnedMissingRegistration}
}
