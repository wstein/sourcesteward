// SPDX-License-Identifier: EUPL-1.2

package stackpublish

// Identity is the exact name and email of a person; they are compared byte for byte.
type Identity struct {
	Name  string
	Email string
}

// ReviewTier orders reviewer strength. An unrecognized tier ranks below every real one.
type ReviewTier string

// Review tiers (families, never versions).
const (
	TierStrongest ReviewTier = "strongest"
	TierMiddle    ReviewTier = "middle"
	TierHelper    ReviewTier = "helper"
)

func (t ReviewTier) rank() int {
	switch t {
	case TierStrongest:
		return 3
	case TierMiddle:
		return 2
	case TierHelper:
		return 1
	}
	return 0
}

// Reviewer is one entry of the trusted reviewer allow list.
type Reviewer struct {
	ID   string
	Tier ReviewTier
}

// Tri is a three-valued fact; Unknown (and the zero value) never counts as yes.
type Tri string

// Tri values.
const (
	TriYes     Tri = "yes"
	TriNo      Tri = "no"
	TriUnknown Tri = "unknown"
)

// ReceiptSpec names a producer contract the trusted policy accepts: producer, command, kind and
// schema version together. A diagnostic name alone never selects a decoder.
type ReceiptSpec struct {
	Producer      string
	Command       string
	Kind          string
	SchemaVersion int
}

// HelperProfile is the host-configured, fixed re-sign helper and verifier with the signer
// fingerprints the policy allows. stw owns no key; a nil profile means re-signing is unavailable.
type HelperProfile struct {
	HelperID   string
	VerifierID string
	Signers    []string
}

// WorkspacePolicy is the trusted workspace policy, already loaded by the caller from its trusted
// source: owner identity, reviewer allow list, accepted review, check and scope evidence sources,
// accepted restack receipt contracts and the signature policy. Repository text, caller flags and
// the effective Git configuration are never part of it. A policy that is not Loaded blocks every
// gate that needs it: this package never invents a ledger or a default.
type WorkspacePolicy struct {
	Loaded           bool
	Owner            Identity
	Reviewers        []Reviewer
	Authors          []string // trusted author agent ids (from task evidence), never a forge login
	ReviewSources    []string
	CheckSources     []string
	ScopeSources     []string
	RestackReceipts  []ReceiptSpec
	RemoteSignatures Tri
	Resign           *HelperProfile
}

// Refusal is a typed gate refusal: a stable reason plus a fixed, bounded detail slug that says
// which condition tripped (never free text and never a session id).
type Refusal struct {
	Reason Reason
	Detail string
}

func (r Refusal) String() string { return string(r.Reason) + "/" + r.Detail }

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// validID is the grammar of reviewer and author ids: lower-case, so an exact comparison is also the
// only comparison and a different case is simply a different, untrusted id.
func validID(id string) bool { return actionIDGrammar.MatchString(id) }

func (p WorkspacePolicy) reviewer(id string) (Reviewer, bool) {
	for _, r := range p.Reviewers {
		if r.ID == id && validID(id) {
			return r, true
		}
	}
	return Reviewer{}, false
}

// usable reports a re-sign helper profile with both fixed ids; the one predicate behind both the
// handoff and the chain check, so a half-configured profile is unavailable everywhere.
func (h *HelperProfile) usable() bool { return h != nil && h.HelperID != "" && h.VerifierID != "" }
