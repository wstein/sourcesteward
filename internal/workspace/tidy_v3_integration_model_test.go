// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Spec 031 T012: the proof model. The reason table of the contract is the
// single definition; the closed enum in code must equal it row for row.
func TestIntegrationReasonEnumEqualsContractTable(t *testing.T) {
	raw, err := os.ReadFile("../../specs/031-tidy-integration-proofs/contracts/integration-proofs.md")
	if err != nil {
		t.Fatal(err)
	}
	section := string(raw)
	section = section[strings.Index(section, "## Review reasons"):]
	section = section[:strings.Index(section, "## Command bounds")]
	row := regexp.MustCompile("(?m)^\\| `([a-z-]+)` \\|.*\\| (yes|no)[^|]*\\|$")
	var names []string
	clearable := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		names = append(names, m[1])
		clearable[m[1]] = m[2] == "yes"
	}
	if len(names) != len(integrationReasons) {
		t.Fatalf("contract lists %d reasons, code %d: %v", len(names), len(integrationReasons), names)
	}
	for i, r := range integrationReasons {
		if names[i] != r.Name || clearable[r.Name] != r.Clearable {
			t.Fatalf("row %d: contract %s clearable=%t, code %+v", i, names[i], clearable[names[i]], r)
		}
	}
	if !integrationHardStop("history-override") || !integrationHardStop("unsupported-metadata") || integrationHardStop("conflict") || integrationHardStop("unknown") {
		t.Fatal("only history-override and unsupported-metadata are hard stops")
	}
}

func TestIntegrationClassesStrengthsAndKindOrder(t *testing.T) {
	want := map[string]string{"ancestor": "ancestry", "patch-id": "history-equivalent", "merge-noop": "content-present", "no-net-change": "nothing-unmerged"}
	if len(integrationStrength) != len(want) {
		t.Fatal("strength set differs from the contract")
	}
	for class, strength := range want {
		if integrationStrength[class] != strength {
			t.Fatalf("class %s strength %q, want %q", class, integrationStrength[class], strength)
		}
	}
	if strings.Join(integrationKindOrder, ",") != "ancestor,patch-id,merge-noop,no-net-change" {
		t.Fatalf("kind order %v", integrationKindOrder)
	}
	for _, bad := range []V3ProofPolicy{{Version: 2, Kinds: integrationKindOrder}, {Version: 1}, {Version: 1, Kinds: []string{"patch-id", "ancestor"}}, {Version: 1, Kinds: []string{"patch-id", "patch-id"}}, {Version: 1, Kinds: []string{"cherry-pick"}}} {
		if validV3ProofPolicy(bad) {
			t.Fatalf("policy admitted: %+v", bad)
		}
	}
	if !validV3ProofPolicy(defaultIntegrationPolicy()) || !validV3ProofPolicy(V3ProofPolicy{Version: 1, Kinds: []string{"patch-id", "no-net-change"}}) {
		t.Fatal("valid policy refused")
	}
}

func integrationTestPlan(candidate V3Candidate, policy V3ProofPolicy) V3Plan {
	p := v3TestPlan()
	p.ProofPolicy = &policy
	candidate.OID = strings.Repeat("f", 40)
	p.Candidates = []V3Candidate{candidate}
	p.Expected = v3SnapshotWithState(p.Expected, v3TestNative(p))
	p.Effects = v3PlanEffects(p)
	p.Digest, _ = v3PlanDigest(p)
	return p
}

func integrationEvidence(p V3Plan, class, disposition string, reasons, flags []string) *V3IntegrationEvidence {
	e := &V3IntegrationEvidence{Class: class, Strength: integrationStrength[class], Disposition: disposition, Reasons: reasons, Flags: flags, BaseSelector: p.Expected.BaseSelector, BaseOID: p.Expected.BaseCommit, TipOID: strings.Repeat("f", 40)}
	if class == integrationPatchID {
		e.Matched = 2
	}
	return e
}

func integrationCandidate(class string, e *V3IntegrationEvidence, decision, reason string) V3Candidate {
	return V3Candidate{ActionID: "action-1", Kind: "branch", Name: "refs/heads/topic", Decision: decision, Reason: reason, Proof: class, WorkspacePaths: []string{}, Evidence: &V3ProofEvidence{Integration: e}}
}

// Every class and disposition validates through the plan, and each forged
// variant is refused: dispositions, classes, strengths, reasons, hard stops.
func TestV3IntegrationEvidenceValidatesDispositionsAndRejectsForgery(t *testing.T) {
	policy := defaultIntegrationPolicy()
	base := integrationTestPlan(V3Candidate{}, policy)
	build := func(class string, mutate func(*V3IntegrationEvidence), accept bool, kinds ...string) V3Plan {
		pol := policy
		if len(kinds) > 0 {
			pol = V3ProofPolicy{Version: 1, Kinds: kinds}
		}
		pol.AcceptWhitespaceDifferences = accept
		p := integrationTestPlan(V3Candidate{}, pol)
		e := integrationEvidence(p, class, integrationProvenDisposition, []string{}, []string{})
		mutate(e)
		p.Candidates[0] = integrationCandidate(class, e, "delete", class)
		p.Candidates[0].OID = strings.Repeat("f", 40)
		p.Effects = v3PlanEffects(p)
		p.Digest, _ = v3PlanDigest(p)
		return p
	}
	same := func(*V3IntegrationEvidence) {}
	for _, class := range []string{"patch-id", "merge-noop", "no-net-change"} {
		if p := build(class, same, false); validateV3Plan(p, p.StateDir, p.PlanID) != nil {
			t.Fatalf("%s proof refused: %v", class, validateV3Plan(p, p.StateDir, p.PlanID))
		}
	}
	if p := build("patch-id", func(e *V3IntegrationEvidence) { e.Flags = []string{"accepted-whitespace-differences"} }, true); validateV3Plan(p, p.StateDir, p.PlanID) != nil {
		t.Fatal("accepted whitespace flag refused under the accepting policy")
	}
	forged := map[string]V3Plan{
		"class not in the recorded policy":      build("merge-noop", same, false, "ancestor", "patch-id"),
		"accepted flag without the policy flag": build("patch-id", func(e *V3IntegrationEvidence) { e.Flags = []string{"accepted-whitespace-differences"} }, false),
		"unaccepted whitespace flag deletes":    build("patch-id", func(e *V3IntegrationEvidence) { e.Flags = []string{"whitespace-differs"} }, true),
		"wrong strength":                        build("patch-id", func(e *V3IntegrationEvidence) { e.Strength = "content-present" }, false),
		"reasons on a proven tip":               build("patch-id", func(e *V3IntegrationEvidence) { e.Reasons = []string{"partial-match"} }, false),
		"unmatched commits":                     build("patch-id", func(e *V3IntegrationEvidence) { e.Unmatched = 1 }, false),
		"no matched commit":                     build("patch-id", func(e *V3IntegrationEvidence) { e.Matched = 0 }, false),
		"skipped merges":                        build("patch-id", func(e *V3IntegrationEvidence) { e.SkippedMerges = 1 }, false),
		"counts on merge-noop":                  build("merge-noop", func(e *V3IntegrationEvidence) { e.Matched = 1 }, false),
		"other tip":                             build("merge-noop", func(e *V3IntegrationEvidence) { e.TipOID = strings.Repeat("e", 40) }, false),
		"other base":                            build("merge-noop", func(e *V3IntegrationEvidence) { e.BaseOID = strings.Repeat("e", 40) }, false),
		"other base selector":                   build("merge-noop", func(e *V3IntegrationEvidence) { e.BaseSelector = "refs/heads/other" }, false),
		"ancestor class in the arm":             build("merge-noop", func(e *V3IntegrationEvidence) { e.Class, e.Strength = "ancestor", "ancestry" }, false),
		"nil reasons":                           build("merge-noop", func(e *V3IntegrationEvidence) { e.Reasons = nil }, false),
		"class mismatching the proof tag":       build("merge-noop", func(e *V3IntegrationEvidence) { e.Class, e.Strength = "no-net-change", "nothing-unmerged" }, false),
	}
	for name, p := range forged {
		if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
			t.Errorf("forged evidence admitted: %s", name)
		}
	}
	review := func(disposition, reason string, reasons []string, flags []string, tag string) V3Plan {
		p := integrationTestPlan(V3Candidate{}, policy)
		e := integrationEvidence(p, "", disposition, reasons, flags)
		p.Candidates[0] = integrationCandidate(tag, e, "retain", reason)
		p.Candidates[0].OID = strings.Repeat("f", 40)
		p.Effects = v3PlanEffects(p)
		p.Digest, _ = v3PlanDigest(p)
		return p
	}
	ok := map[string]V3Plan{
		"needs-review":   review("needs-review", "needs-review", []string{"partial-match", "would-add-content"}, []string{}, "none"),
		"whitespace":     review("needs-review", "needs-review", []string{"whitespace-differs"}, []string{"whitespace-differs"}, "none"),
		"graft":          review("retained", "history-override", []string{"history-override"}, []string{}, "none"),
		"alternates":     review("retained", "unsupported-metadata", []string{"unsupported-metadata"}, []string{}, "none"),
		"budget":         review("needs-review", "needs-review", []string{"proof-budget-exhausted"}, []string{}, "none"),
		"object dir":     review("needs-review", "needs-review", []string{"object-dir-unavailable"}, []string{}, "none"),
		"proof limit":    review("needs-review", "needs-review", []string{"proof-limit"}, []string{}, "none"),
		"merge in range": review("needs-review", "needs-review", []string{"merge-commits-in-range"}, []string{}, "none"),
	}
	for name, p := range ok {
		p.Candidates[0].Proof = "none"
		p.Effects = v3PlanEffects(p)
		p.Digest, _ = v3PlanDigest(p)
		if err := validateV3Plan(p, p.StateDir, p.PlanID); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
	refused := map[string]V3Plan{
		"clearable reason retained":     review("retained", "conflict", []string{"conflict"}, []string{}, "none"),
		"hard stop only needs review":   review("needs-review", "needs-review", []string{"history-override"}, []string{}, "none"),
		"empty reasons":                 review("needs-review", "needs-review", []string{}, []string{}, "none"),
		"unknown reason":                review("needs-review", "needs-review", []string{"looks-fine"}, []string{}, "none"),
		"duplicate reason":              review("needs-review", "needs-review", []string{"conflict", "conflict"}, []string{}, "none"),
		"needs-review deleting":         review("needs-review", "needs-review", []string{"conflict"}, []string{}, "none"),
		"candidate reason not the enum": review("needs-review", "integration-not-proven", []string{"conflict"}, []string{}, "none"),
		"hard stop reason mismatch":     review("retained", "unsupported-metadata", []string{"history-override"}, []string{}, "none"),
		"whitespace flag unexplained":   review("needs-review", "needs-review", []string{"conflict"}, []string{"whitespace-differs"}, "none"),
	}
	refused["needs-review deleting"] = func() V3Plan {
		p := refused["needs-review deleting"]
		p.Candidates[0].Decision = "delete"
		p.Effects = v3PlanEffects(p)
		p.Digest, _ = v3PlanDigest(p)
		return p
	}()
	for name, p := range refused {
		p.Candidates[0].Proof = "none"
		p.Effects = v3PlanEffects(p)
		p.Digest, _ = v3PlanDigest(p)
		if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
			t.Errorf("forged review evidence admitted: %s", name)
		}
	}
	// A plan without a policy keeps its exact bytes and evaluates ancestry only.
	plain := v3TestPlan()
	raw, _ := json.Marshal(plain)
	if strings.Contains(string(raw), "proof_policy") || strings.Contains(string(raw), `"integration"`) {
		t.Fatalf("pre-policy plan encoding changed: %s", raw)
	}
	noPolicy := integrationTestPlan(V3Candidate{}, policy)
	noPolicy.ProofPolicy = nil
	e := integrationEvidence(noPolicy, "merge-noop", integrationProvenDisposition, []string{}, []string{})
	noPolicy.Candidates[0] = integrationCandidate("merge-noop", e, "delete", "merge-noop")
	noPolicy.Candidates[0].OID = strings.Repeat("f", 40)
	noPolicy.Effects = v3PlanEffects(noPolicy)
	noPolicy.Digest, _ = v3PlanDigest(noPolicy)
	if validateV3Plan(noPolicy, noPolicy.StateDir, noPolicy.PlanID) == nil {
		t.Fatal("forge-free proof admitted without a recorded policy")
	}
	_ = base
	// Plan identity binds the policy, the kinds and the whitespace flag.
	a, b, c := integrationTestPlan(V3Candidate{}, policy), integrationTestPlan(V3Candidate{}, V3ProofPolicy{Version: 1, Kinds: []string{"ancestor"}}), integrationTestPlan(V3Candidate{}, V3ProofPolicy{Version: 1, Kinds: integrationKindOrder, AcceptWhitespaceDifferences: true})
	a.Candidates, b.Candidates, c.Candidates = nil, nil, nil
	for _, p := range []*V3Plan{&a, &b, &c} {
		p.Candidates = []V3Candidate{}
		p.Digest, _ = v3PlanDigest(*p)
	}
	if a.Digest == b.Digest || a.Digest == c.Digest || b.Digest == c.Digest {
		t.Fatal("plan digest does not bind the proof policy")
	}
}

// Reasons and flags are closed enums: many reviewed refs never exhaust the
// 1,024 record bound of the plan (Spec 029 aggregate, unchanged).
func TestV3BoundsDoNotCountClosedReasonEnums(t *testing.T) {
	e := V3IntegrationEvidence{Disposition: "needs-review", Reasons: []string{"conflict", "partial-match", "would-add-content"}, Flags: []string{}}
	rows := make([]V3IntegrationEvidence, 600)
	for i := range rows {
		rows[i] = e
	}
	if err := v3Bounds(struct{ Rows []V3IntegrationEvidence }{rows}); err != nil {
		t.Fatalf("closed enums counted: %v", err)
	}
	if err := v3Bounds(struct{ Rows []V3IntegrationEvidence }{append(rows, make([]V3IntegrationEvidence, 600)...)}); err == nil {
		t.Fatal("the row bound itself must still hold")
	}
}
