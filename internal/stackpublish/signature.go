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
// there is no unsigned fallback and no signer invocation. It returns every refusal found, in order.
func EvaluateSigning(p WorkspacePolicy, ev *SigningEvidence, newHead string) []Refusal {
	refuse := func(detail string) []Refusal { return []Refusal{{ReasonSignaturePolicyUnknown, detail}} }
	switch {
	case !p.Loaded:
		return refuse("policy-missing")
	case ev == nil:
		return refuse("signing-evidence-absent")
	case p.RemoteSignatures != TriYes && p.RemoteSignatures != TriNo:
		return refuse("remote-policy-unknown")
	case !ev.SourceKnown:
		return refuse("source-signatures-unknown")
	case ev.FinalHead != newHead:
		return refuse("final-head-mismatch")
	}
	switch ev.Mode {
	case SigningUnsigned:
		if len(ev.SourceSigned) > 0 {
			return refuse("signature-dropped")
		}
		if p.RemoteSignatures == TriYes {
			return refuse("required-signature-unmet")
		}
		return nil
	case SigningSigned:
		return signedChainRefusals(p, ev)
	}
	return refuse("signing-mode-unknown")
}

// signedChainRefusals verifies the bottom-up old to unsigned to signed mapping: only the host
// helper's verified signatures by allowed signers count, each child sits on the signed commit of
// the link below it, and the last signed commit is the final head the review covers.
func signedChainRefusals(p WorkspacePolicy, ev *SigningEvidence) []Refusal {
	refuse := func(detail string) []Refusal { return []Refusal{{ReasonSignaturePolicyUnknown, detail}} }
	if p.Resign == nil {
		return refuse("signing-capability-unavailable")
	}
	if len(ev.Chain) == 0 {
		return refuse("provenance-lost")
	}
	covered := map[string]bool{}
	for i, l := range ev.Chain {
		if !isLowerHex(l.Old, 40) || !isLowerHex(l.Unsigned, 40) || !isLowerHex(l.Signed, 40) || !isLowerHex(l.SignedParent, 40) ||
			l.Signed == l.Old || l.Signed == l.Unsigned || l.Old == l.Unsigned {
			return refuse("mapping-invalid")
		}
		if !l.Verified {
			return refuse("signature-unverified")
		}
		if !contains(p.Resign.Signers, l.Signer) {
			return refuse("signer-not-allowed")
		}
		if i > 0 && l.SignedParent != ev.Chain[i-1].Signed {
			return refuse("parent-mismatch")
		}
		covered[l.Old] = true
	}
	for _, old := range ev.SourceSigned {
		if !covered[old] {
			return refuse("provenance-lost")
		}
	}
	if ev.Chain[len(ev.Chain)-1].Signed != ev.FinalHead {
		return refuse("final-head-mismatch")
	}
	return nil
}

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
func ResignHandoffFor(p WorkspacePolicy) ResignHandoff {
	if !p.Loaded || p.Resign == nil || p.Resign.HelperID == "" || p.Resign.VerifierID == "" {
		return ResignHandoff{Detail: "signing-capability-unavailable"}
	}
	return ResignHandoff{Available: true, LocalFlag: "--re-sign", HelperID: p.Resign.HelperID, VerifierID: p.Resign.VerifierID}
}
