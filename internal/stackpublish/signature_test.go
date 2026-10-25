// SPDX-License-Identifier: EUPL-1.2

package stackpublish

import (
	"reflect"
	"testing"
)

func helperPolicy() WorkspacePolicy {
	p := goodPolicy()
	p.Resign = &HelperProfile{HelperID: "host-signer-1", VerifierID: "host-verifier-1", Signers: []string{"SHA256:allowed"}}
	return p
}

func signedEvidence() *SigningEvidence {
	return &SigningEvidence{Mode: SigningSigned, SourceKnown: true, SourceSigned: []string{sha('1'), sha('2')}, FinalHead: sha('f'),
		Chain: []SignedLink{
			{Old: sha('1'), Unsigned: sha('3'), Signed: sha('e'), SignedParent: sha('0'), Signer: "SHA256:allowed", Verified: true},
			{Old: sha('2'), Unsigned: sha('4'), Signed: sha('f'), SignedParent: sha('e'), Signer: "SHA256:allowed", Verified: true}}}
}

func unsignedEvidence() *SigningEvidence {
	return &SigningEvidence{Mode: SigningUnsigned, SourceKnown: true, FinalHead: sha('f')}
}

func refusals(rs []Refusal) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func TestUnsignedCandidateOfUnsignedSourceIsFineWhenNoSignatureIsRequired(t *testing.T) {
	if got := EvaluateSigning(goodPolicy(), unsignedEvidence(), sha('f')); len(got) != 0 {
		t.Fatalf("%v", refusals(got))
	}
}

func TestSigningRefusals(t *testing.T) {
	type tc struct {
		policy func() WorkspacePolicy
		ev     func() *SigningEvidence
		want   []string
	}
	cases := map[string]tc{
		"policy not loaded":                  {func() WorkspacePolicy { return WorkspacePolicy{} }, unsignedEvidence, []string{"signature-policy-unknown/policy-missing"}},
		"evidence absent":                    {goodPolicy, func() *SigningEvidence { return nil }, []string{"signature-policy-unknown/signing-evidence-absent"}},
		"remote policy unknown":              {func() WorkspacePolicy { p := goodPolicy(); p.RemoteSignatures = TriUnknown; return p }, unsignedEvidence, []string{"signature-policy-unknown/remote-policy-unknown"}},
		"remote policy unset":                {func() WorkspacePolicy { p := goodPolicy(); p.RemoteSignatures = ""; return p }, unsignedEvidence, []string{"signature-policy-unknown/remote-policy-unknown"}},
		"source signatures unreadable":       {goodPolicy, func() *SigningEvidence { e := unsignedEvidence(); e.SourceKnown = false; return e }, []string{"signature-policy-unknown/source-signatures-unknown"}},
		"final head is not the candidate":    {goodPolicy, func() *SigningEvidence { e := unsignedEvidence(); e.FinalHead = sha('9'); return e }, []string{"signature-policy-unknown/final-head-mismatch"}},
		"unknown mode":                       {goodPolicy, func() *SigningEvidence { e := unsignedEvidence(); e.Mode = "partly"; return e }, []string{"signature-policy-unknown/signing-mode-unknown"}},
		"dropped source signature":           {goodPolicy, func() *SigningEvidence { e := unsignedEvidence(); e.SourceSigned = []string{sha('1')}; return e }, []string{"signature-policy-unknown/signature-dropped"}},
		"required signature unmet":           {func() WorkspacePolicy { p := goodPolicy(); p.RemoteSignatures = TriYes; return p }, unsignedEvidence, []string{"signature-policy-unknown/required-signature-unmet"}},
		"signed without a helper capability": {goodPolicy, signedEvidence, []string{"signature-policy-unknown/signing-capability-unavailable"}},
		"signed chain missing":               {helperPolicy, func() *SigningEvidence { e := signedEvidence(); e.Chain = nil; return e }, []string{"signature-policy-unknown/provenance-lost"}},
		"a signed source commit has no link": {helperPolicy, func() *SigningEvidence {
			e := signedEvidence()
			e.SourceSigned = append(e.SourceSigned, sha('7'))
			return e
		}, []string{"signature-policy-unknown/provenance-lost"}},
		"signature unverified":           {helperPolicy, func() *SigningEvidence { e := signedEvidence(); e.Chain[1].Verified = false; return e }, []string{"signature-policy-unknown/signature-unverified"}},
		"signer not allowed":             {helperPolicy, func() *SigningEvidence { e := signedEvidence(); e.Chain[0].Signer = "SHA256:other"; return e }, []string{"signature-policy-unknown/signer-not-allowed"}},
		"child not on the signed parent": {helperPolicy, func() *SigningEvidence { e := signedEvidence(); e.Chain[1].SignedParent = sha('3'); return e }, []string{"signature-policy-unknown/parent-mismatch"}},
		"parent still the unsigned one": {helperPolicy, func() *SigningEvidence {
			e := signedEvidence()
			e.Chain[1].SignedParent = e.Chain[0].Unsigned
			return e
		}, []string{"signature-policy-unknown/parent-mismatch"}},
		"mapping reuses old as signed": {helperPolicy, func() *SigningEvidence {
			e := signedEvidence()
			e.Chain[0].Signed = e.Chain[0].Old
			e.Chain[1].SignedParent = e.Chain[0].Old
			return e
		}, []string{"signature-policy-unknown/mapping-invalid"}},
		"short sha in the mapping":          {helperPolicy, func() *SigningEvidence { e := signedEvidence(); e.Chain[0].Unsigned = "abc"; return e }, []string{"signature-policy-unknown/mapping-invalid"}},
		"last signed is not the final head": {helperPolicy, func() *SigningEvidence { e := signedEvidence(); e.Chain[1].Signed = sha('8'); return e }, []string{"signature-policy-unknown/final-head-mismatch"}},
	}
	for name, c := range cases {
		got := refusals(EvaluateSigning(c.policy(), c.ev(), sha('f')))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v want %v", name, got, c.want)
		}
	}
}

func TestAValidFinalSignedChainPasses(t *testing.T) {
	if got := EvaluateSigning(helperPolicy(), signedEvidence(), sha('f')); len(got) != 0 {
		t.Fatalf("%v", refusals(got))
	}
	// A required remote signature is satisfied by the verified re-signed chain.
	p := helperPolicy()
	p.RemoteSignatures = TriYes
	if got := EvaluateSigning(p, signedEvidence(), sha('f')); len(got) != 0 {
		t.Fatalf("%v", refusals(got))
	}
	// Signed commits without any signed source commit (a new signed candidate) also verify.
	e := signedEvidence()
	e.SourceSigned = nil
	if got := EvaluateSigning(helperPolicy(), e, sha('f')); len(got) != 0 {
		t.Fatalf("%v", refusals(got))
	}
}

func TestResignHandoffIsInertDataAndUnavailableWithoutAHelper(t *testing.T) {
	h := ResignHandoffFor(goodPolicy())
	if h.Available || h.Detail != "signing-capability-unavailable" || h.HelperID != "" || h.LocalFlag != "" {
		t.Fatalf("%+v", h)
	}
	h = ResignHandoffFor(WorkspacePolicy{})
	if h.Available || h.Detail != "signing-capability-unavailable" {
		t.Fatalf("%+v", h)
	}
	h = ResignHandoffFor(helperPolicy())
	if !h.Available || h.LocalFlag != "--re-sign" || h.HelperID != "host-signer-1" || h.VerifierID != "host-verifier-1" || h.Detail != "" {
		t.Fatalf("%+v", h)
	}
}

// A remote authorization has no way to reach a signer: the step API takes no signing input and
// the signing evaluation takes no authorization.
func TestRemoteAuthorizationNeverSelectsASigningStep(t *testing.T) {
	typ := reflect.TypeOf(ApplyRequest{})
	for i := 0; i < typ.NumField(); i++ {
		if n := typ.Field(i).Name; n == "Resign" || n == "Signer" || n == "ReSign" {
			t.Fatalf("ApplyRequest has a signing field %s", n)
		}
	}
	ev := reflect.TypeOf(EvaluateSigning)
	for i := 0; i < ev.NumIn(); i++ {
		if ev.In(i) == typ {
			t.Fatal("EvaluateSigning takes an apply request")
		}
	}
}
