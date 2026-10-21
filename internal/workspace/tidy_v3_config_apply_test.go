// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// A stored plan whose configuration notes were forged never validates.
func TestTidyV3PlanValidationRejectsForgedConfigNotes(t *testing.T) {
	f := newPolicyFix(t)
	f.cfg("core.hooksPath", f.hooksDir)
	f.cfg("submodule.member.url", "https://example.invalid/member.git")
	r, err := f.planNoted(neutral("core.hooksPath", applyNone), neutral("submodule.*", applyNone))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateV3Plan(*r.Plan, r.Plan.StateDir, r.Plan.PlanID); err != nil {
		t.Fatalf("control plan invalid: %v", err)
	}
	for name, mutate := range map[string]func(*V3Plan){
		"unsorted":      func(p *V3Plan) { p.ConfigNotes[0], p.ConfigNotes[1] = p.ConfigNotes[1], p.ConfigNotes[0] },
		"duplicate":     func(p *V3Plan) { p.ConfigNotes = append(p.ConfigNotes, p.ConfigNotes[len(p.ConfigNotes)-1]) },
		"unknown-class": func(p *V3Plan) { p.ConfigNotes[0].KeyClass = "core.other" },
		"disposition":   func(p *V3Plan) { p.ConfigNotes[0].Disposition = "refused" },
		"apply-effect":  func(p *V3Plan) { p.ConfigNotes[0].ApplyEffect = applyRefuses },
		"digest":        func(p *V3Plan) { p.ConfigNotes[0].Digest = "abc" },
	} {
		t.Run(name, func(t *testing.T) {
			p := *r.Plan
			p.ConfigNotes = append([]V3ConfigNote{}, p.ConfigNotes...)
			mutate(&p)
			digest, err := v3PlanDigest(p)
			if err != nil {
				t.Fatal(err)
			}
			p.Digest = digest
			if validateV3Plan(p, p.StateDir, p.PlanID) == nil {
				t.Fatal("forged configuration notes admitted")
			}
		})
	}
}

// Plans without notes keep their exact bytes (no config_notes key) and plans
// stored before this field existed still decode; notes round-trip exactly.
func TestTidyV3PlanConfigNotesAreOptionalOnTheWire(t *testing.T) {
	f := newPolicyFix(t)
	f.cfg("core.hooksPath", f.hooksDir)
	r, err := f.planNoted(neutral("core.hooksPath", applyNone))
	if err != nil {
		t.Fatal(err)
	}
	with, err := json.Marshal(r.Plan)
	if err != nil {
		t.Fatal(err)
	}
	var got V3Plan
	if err = decodeV3Record(with, &got); err != nil || !reflect.DeepEqual(got.ConfigNotes, r.Plan.ConfigNotes) {
		t.Fatalf("notes did not round-trip: %v %+v", err, got.ConfigNotes)
	}
	bare := *r.Plan
	bare.ConfigNotes = nil
	without, err := json.Marshal(bare)
	if err != nil || strings.Contains(string(without), "config_notes") {
		t.Fatalf("plan without notes carries the key: %v %s", err, without)
	}
	var old V3Plan
	if err = decodeV3Record(without, &old); err != nil || old.ConfigNotes != nil {
		t.Fatalf("plan stored before the field existed no longer decodes: %v", err)
	}
	if err = v3Bounds(bare); err != nil {
		t.Fatalf("absent notes are not a null array: %v", err)
	}
}
