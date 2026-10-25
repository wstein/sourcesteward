// SPDX-License-Identifier: EUPL-1.2

package stackpublish

// ChangeClass is the review class of a change: docs-only (specification or documentation prose
// only) or code (code, tests, scripts, generated files, or anything not proved docs-only).
type ChangeClass string

// Change classes.
const (
	ClassDocsOnly ChangeClass = "docs-only"
	ClassCode     ChangeClass = "code"
)

// ClassifyPaths classifies changed paths. Docs-only needs a known, non-empty set where every
// path is on the prose allow list; anything else (unsafe path, unknown evidence, empty set) is code.
func ClassifyPaths(paths []string, known bool) ChangeClass { return "" }

// Commit is one commit of the publication range with its actual identities and changed paths.
type Commit struct {
	ID         string
	Author     Identity
	Committer  Identity
	Paths      []string
	PathsKnown bool
}

// RangeEvidence is `<observed remote default>..<actual new head>`, oldest first, including every
// still-open parent commit.
type RangeEvidence struct {
	Default  string
	Complete bool
	Commits  []Commit
}

// RangeDiff is the range-diff pairing of the rewrite: "=" per unchanged pair.
type RangeDiff struct {
	Complete bool
	OldCount int
	NewCount int
	Rows     []string
}

// MovedScopeSource is the part of the 015 apply receipt the adapter reads.
type MovedScopeSource struct {
	Spec           ReceiptSpec
	Backend        string
	Repository     string
	Task           string
	ReviewGateHead string
	TargetNewHead  string
	ChangeClass    string // review_gate.change_class
	Complete       bool
}

// ConfigOrigin is one `git config --show-origin` user.* entry.
type ConfigOrigin struct {
	Scope string
	Key   string
	Value string
}

// ConfigEvidence is the bounded origin evidence of user.name and user.email.
type ConfigEvidence struct {
	Complete bool
	Origins  []ConfigOrigin
}

// LocalChecks is the result of the applicable local checks on the actual candidate.
type LocalChecks struct {
	Known bool
	Green bool
}

// PublicationInput is all evidence of one before-push evaluation, fetched in this run.
type PublicationInput struct {
	Policy     WorkspacePolicy
	Backend    string
	Repository string
	Task       string
	Rewrite    bool
	NewHead    string
	AuthorID   string
	Range      *RangeEvidence
	Moved      *MovedScopeSource
	RangeDiff  *RangeDiff
	Checks     *LocalChecks
	Review     *ReviewEvidence
	Config     *ConfigEvidence
	Signing    *SigningEvidence
}

// PublicationReceipt is the before-push verdict.
type PublicationReceipt struct {
	NewHead         string
	MovedScopeClass ChangeClass // empty when no moved scope applies (a new, non-rewritten topic)
	RangeClass      ChangeClass
	Class           ChangeClass // effective: code unless both are proved docs-only
	ClassUnknown    bool        // some evidence of the class was missing or unrecognized, so it counts as code
	PureRebase      bool
	ReviewRequired  bool
	MinTier         ReviewTier
	Blockers        []Refusal
}

// AdaptMovedScope validates the 015 receipt and returns the 016-only moved-scope class.
func AdaptMovedScope(p WorkspacePolicy, backend, repository, task, newHead string, src *MovedScopeSource) (ChangeClass, *Refusal) {
	return "", nil
}

// EvaluatePublication evaluates the before-push gates as a pure function of the evidence.
func EvaluatePublication(in PublicationInput) PublicationReceipt { return PublicationReceipt{} }

// Allowed reports whether no blocker remains.
func (r PublicationReceipt) Allowed() bool { return false }

// Status is complete when allowed and refused otherwise (exit 0 or 1).
func (r PublicationReceipt) Status() Status { return StatusComplete }
