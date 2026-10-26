// SPDX-License-Identifier: EUPL-1.2
package risk

// Stable phase-1 risk words let renderers cover every emitted reason.
const (
	ReasonProofAncestor           = "proof-ancestor"
	ReasonProofNoNetChange        = "proof-no-net-change"
	ReasonProofGitHubBranchAbsent = "proof-github-branch-absent"
	ReasonNoOwnCommit             = "no-own-commit"
	ReasonPRMergedHead            = "pr-merged-head"
	ReasonProofEquivalent         = "proof-equivalent"
	ReasonRecentActivity          = "recent-activity"
	ReasonNobodyVouches           = "nobody-vouches"
	ReasonDecisionRow             = "decision-row"
	ReasonUpstreamGone            = "upstream-gone"
	ReasonNotIntegrated           = "not-integrated"
	ReasonUncommittedFiles        = "uncommitted-files"
	ReasonExtraFiles              = "extra-files"
	ReasonNotJudged               = "not-judged"
)

// AllReasons returns the complete phase-1 vocabulary in stable order. Each call
// owns its slice; callers cannot modify the catalog used by another renderer.
// Phase-2 words are absent because the phase-1 ladder never emits them.
func AllReasons() []string {
	return []string{ReasonProofAncestor, ReasonProofNoNetChange, ReasonProofGitHubBranchAbsent, ReasonNoOwnCommit, ReasonPRMergedHead, ReasonProofEquivalent, ReasonRecentActivity, ReasonNobodyVouches, ReasonDecisionRow, ReasonUpstreamGone, ReasonNotIntegrated, ReasonUncommittedFiles, ReasonExtraFiles, ReasonNotJudged}
}
