// SPDX-License-Identifier: EUPL-1.2
package workspace

import "slices"

// Spec 031 integration proof model (FR-001, FR-003w, FR-006, data model
// ProofEvidence). The reason table of contracts/integration-proofs.md is the
// single definition of every reason; nothing else in the code names one.

const (
	integrationAncestor    = "ancestor"
	integrationPatchID     = "patch-id"
	integrationMergeNoop   = "merge-noop"
	integrationNoNetChange = "no-net-change"

	integrationProvenDisposition   = "proven"
	integrationReviewDisposition   = "needs-review"
	integrationRetainedDisposition = "retained"

	integrationFlagWhitespace         = "whitespace-differs"
	integrationFlagAcceptedWhitespace = "accepted-whitespace-differences"

	// integrationPolicyVersion is bound into the plan identity (FR-011).
	integrationPolicyVersion = 1
)

// integrationKindOrder is the order of FR-006 and the canonical order of the
// kinds stored in a plan.
var integrationKindOrder = []string{integrationAncestor, integrationPatchID, integrationMergeNoop, integrationNoNetChange}

// integrationStrength labels each deletable class (FR-001).
var integrationStrength = map[string]string{
	integrationAncestor:    "ancestry",
	integrationPatchID:     "history-equivalent",
	integrationMergeNoop:   "content-present",
	integrationNoNetChange: "nothing-unmerged",
}

// integrationReason describes one row of the reason table.
type integrationReason struct {
	Name      string
	Clearable bool
}

// integrationReasons is the closed reason enum in the order of the contract table.
var integrationReasons = []integrationReason{
	{"whitespace-differs", true}, {"partial-match", true}, {"multiset-mismatch", true}, {"merge-commits-in-range", true},
	{"ambiguous-merge-base", true}, {"gitlink-change", true}, {"would-add-content", true}, {"conflict", true},
	{"attribute-source", true}, {"merge-attribute", true}, {"diff-attribute", true}, {"merge-driver", true},
	{"proof-limit", true}, {"proof-budget-exhausted", true}, {"object-dir-unavailable", true},
	{"history-override", false}, {"unsupported-metadata", false},
}

func integrationReasonKnown(name string) bool {
	for _, r := range integrationReasons {
		if r.Name == name {
			return true
		}
	}
	return false
}

// integrationHardStop reports the two reasons no decision can clear.
func integrationHardStop(name string) bool {
	for _, r := range integrationReasons {
		if r.Name == name {
			return !r.Clearable
		}
	}
	return false
}

// V3ProofPolicy is the proof configuration bound into the plan identity.
type V3ProofPolicy struct {
	Version                     int      `json:"version"`
	Kinds                       []string `json:"kinds"`
	AcceptWhitespaceDifferences bool     `json:"accept_whitespace_differences"`
}

// V3IntegrationEvidence is the ProofEvidence of one distinct tip. Class and
// strength are present only for a proven tip; reasons are non-empty exactly for
// needs-review and retained.
type V3IntegrationEvidence struct {
	Class         string   `json:"class,omitempty"`
	Strength      string   `json:"strength,omitempty"`
	Disposition   string   `json:"disposition"`
	Reasons       []string `json:"reasons"`
	Flags         []string `json:"flags"`
	BaseSelector  string   `json:"base_selector"`
	BaseOID       string   `json:"base_oid"`
	TipOID        string   `json:"tip_oid"`
	Matched       int      `json:"matched"`
	Unmatched     int      `json:"unmatched"`
	Empty         int      `json:"empty"`
	SkippedMerges int      `json:"skipped_merges"`
}

func defaultIntegrationPolicy() V3ProofPolicy {
	return V3ProofPolicy{Version: integrationPolicyVersion, Kinds: slices.Clone(integrationKindOrder)}
}

// validV3ProofPolicy accepts a nonempty subset of the four kinds in canonical order.
func validV3ProofPolicy(p V3ProofPolicy) bool {
	if p.Version != integrationPolicyVersion || len(p.Kinds) == 0 || p.Kinds == nil {
		return false
	}
	last := -1
	for _, k := range p.Kinds {
		i := slices.Index(integrationKindOrder, k)
		if i <= last {
			return false
		}
		last = i
	}
	return true
}

func (p *V3ProofPolicy) has(kind string) bool    { return p != nil && slices.Contains(p.Kinds, kind) }
func (p *V3ProofPolicy) acceptsWhitespace() bool { return p != nil && p.AcceptWhitespaceDifferences }

// validV3IntegrationEvidence checks the invariants of the data model for the
// tip c.OID against the plan's selected base.
func validV3IntegrationEvidence(e V3IntegrationEvidence, tip string, p V3Plan) bool {
	if e.TipOID != tip || e.BaseOID != p.Expected.BaseCommit || e.BaseSelector != p.Expected.BaseSelector || e.Reasons == nil || e.Flags == nil {
		return false
	}
	if e.Matched < 0 || e.Unmatched < 0 || e.Empty < 0 || e.SkippedMerges < 0 || e.Matched > observationLimit || e.Unmatched > observationLimit || e.Empty > observationLimit || e.SkippedMerges > 9999999 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range e.Reasons {
		if !integrationReasonKnown(r) || seen[r] {
			return false
		}
		seen[r] = true
	}
	for _, f := range e.Flags {
		if f != integrationFlagWhitespace && f != integrationFlagAcceptedWhitespace {
			return false
		}
	}
	switch e.Disposition {
	case integrationProvenDisposition:
		if e.Class == "" || e.Class == integrationAncestor || e.Strength != integrationStrength[e.Class] || len(e.Reasons) != 0 || !p.ProofPolicy.has(e.Class) {
			return false
		}
		if e.Class != integrationPatchID {
			return len(e.Flags) == 0 && e.Matched == 0 && e.Unmatched == 0 && e.Empty == 0 && e.SkippedMerges == 0
		}
		if e.Matched < 1 || e.Unmatched != 0 || e.SkippedMerges != 0 || len(e.Flags) > 1 {
			return false
		}
		// An unaccepted whitespace flag makes the tip needs-review, never proven.
		return len(e.Flags) == 0 || e.Flags[0] == integrationFlagAcceptedWhitespace && p.ProofPolicy.acceptsWhitespace()
	case integrationReviewDisposition, integrationRetainedDisposition:
		if e.Class != "" || e.Strength != "" || len(e.Reasons) == 0 || len(e.Flags) > 1 {
			return false
		}
		hard, soft := false, false
		for _, r := range e.Reasons {
			hard, soft = hard || integrationHardStop(r), soft || !integrationHardStop(r)
		}
		// A hard stop retains the tip (and only hard stops do); every other
		// reason leaves it for review.
		return hard == (e.Disposition == integrationRetainedDisposition) && !(hard && soft) && (len(e.Flags) == 0 || e.Flags[0] == integrationFlagWhitespace && seen[integrationFlagWhitespace])
	}
	return false
}
