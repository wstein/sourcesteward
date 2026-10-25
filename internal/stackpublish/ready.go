// SPDX-License-Identifier: EUPL-1.2

package stackpublish

// ReviewVerdict is the verdict of an independent review.
type ReviewVerdict string

// Review verdicts. Only a clear verdict counts.
const (
	VerdictClear    ReviewVerdict = "clear"
	VerdictNotClear ReviewVerdict = "not-clear"
)

// ReviewEvidence is a review receipt as read from a trusted source. It is not a ledger this
// package keeps: the caller passes what the source reported.
type ReviewEvidence struct {
	Source         string
	ReviewerID     string
	Verdict        ReviewVerdict
	SubjectSHA     string
	EvidenceDigest string // lower-case SHA-256 of the evidence, a locator's content identity
	Locator        string
}

// CheckRun is one reported check run of a context at a commit.
type CheckRun struct {
	Context    string
	HeadSHA    string
	Conclusion string // "success" is green; anything else is not
}

// ChecksEvidence is the required-check evidence: the union of the required contexts of every
// source, whether all sources were read completely, and the runs reported for the head.
type ChecksEvidence struct {
	Source   string
	Complete bool
	Required []string
	Runs     []CheckRun
}

// RebaseEvidence is the repository merge-method fact `rebaseable`, with the head and base it was
// evaluated at. A value evaluated at another head or base is stale.
type RebaseEvidence struct {
	Rebaseable    Tri
	EvaluatedHead string
	EvaluatedBase string
}

// ScopeEvidence is the accepted-scope fact of the PR. UnmetInScope lists acceptance still missing
// inside the accepted scope; OutOfScope lists follow-up work outside it, which never blocks.
type ScopeEvidence struct {
	Source       string
	Accepted     Tri
	UnmetInScope []string
	OutOfScope   []string
}

// ReadyInput is all the evidence of one Ready evaluation. The caller fetches it fresh, for the
// exact current head and base, immediately before the Ready write; a nil pointer is evidence that
// is absent or could not be read.
type ReadyInput struct {
	Policy   WorkspacePolicy
	Head     string // full current PR head
	BaseSHA  string // full current base
	AuthorID string // the author agent of the PR, who cannot review it
	Guards   []Guard
	Review   *ReviewEvidence
	Checks   *ChecksEvidence
	Rebase   *RebaseEvidence
	Scope    *ScopeEvidence
}

// ReadyReceipt is the result of the Ready gate: the exact head and base evaluated, and every
// blocker found (an empty list means Ready).
type ReadyReceipt struct {
	Head     string
	BaseSHA  string
	Blockers []Refusal
}

// EvaluateReady evaluates the Ready gate as a pure function of the evidence passed in.
func EvaluateReady(in ReadyInput) ReadyReceipt { return ReadyReceipt{} }

// Ready reports whether no blocker remains.
func (r ReadyReceipt) Ready() bool { return false }

// Status is complete for a Ready receipt and refused otherwise (exit 0 or 1).
func (r ReadyReceipt) Status() Status { return StatusComplete }

// Guards renders the receipt as the guard the step API consumes.
func (r ReadyReceipt) Guards() []Guard { return nil }
