// SPDX-License-Identifier: EUPL-1.2

package stackpublish

// SigningMode says whether the actual candidate is unsigned or carries re-signed commits.
type SigningMode string

// Signing modes.
const (
	SigningUnsigned SigningMode = "unsigned"
	SigningSigned   SigningMode = "signed"
)

// SignedLink maps one moved commit through old, unsigned candidate and re-signed commit, bottom
// up: SignedParent is the re-signed commit its signed child sits on. Verified is the verifier's
// result for the detached signature, reported by the host helper; this package runs no signer.
type SignedLink struct {
	Old          string
	Unsigned     string
	Signed       string
	SignedParent string
	Signer       string // public signer fingerprint
	Verified     bool
}

// SigningEvidence is the signing part of a local 015 apply receipt for the actual candidate.
type SigningEvidence struct {
	Mode         SigningMode
	SourceKnown  bool     // source signature presence was read completely
	SourceSigned []string // old commits that carried a signature in the source
	Chain        []SignedLink
	FinalHead    string
}

// EvaluateSigning applies the signature policy to the actual candidate. It refuses a dropped source
// signature, an unreadable or unmet policy, and an unavailable, unverified or inconsistent re-sign;
// there is no unsigned fallback and no signer invocation.
func EvaluateSigning(p WorkspacePolicy, ev *SigningEvidence, newHead string) []Refusal { return nil }

// ResignHandoff is inert data for the explicit local 015 plan: whether the host-configured helper
// capability exists and which fixed helper and verifier it names. It runs nothing.
type ResignHandoff struct {
	Available  bool
	Detail     string
	LocalFlag  string
	HelperID   string
	VerifierID string
}

// ResignHandoffFor describes the optional local re-sign step; without a configured helper it is
// unavailable (signing-capability-unavailable), never an unsigned fallback.
func ResignHandoffFor(p WorkspacePolicy) ResignHandoff { return ResignHandoff{} }
