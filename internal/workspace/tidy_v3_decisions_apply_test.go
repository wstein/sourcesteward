// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"strings"
	"testing"
)

// Only revalidateReviewedDecision can catch a change that leaves the snapshot
// digests as they were: it is called with an observation that differs in the
// tip or in a protection while the expected snapshot is equal.
func TestRevalidateReviewedDecisionChecksTheTipAndProtectionsItself(t *testing.T) {
	f := classFix(t)
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		return []map[string]any{validDecision("refs/heads/fresh", tip("fresh"), base)}
	}, V3PlanOptions{})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	p := *r.Plan
	target := candidateByName(t, &p, "fresh")
	ctx, _, closeSession, err := withPlanningSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closeSession()
	real, err := observeTidyV3Git(ctx, f.dir, p.Expected.BaseSelector)
	if err != nil {
		t.Fatal(err)
	}
	check := func(mutate func(*tidyV3NativeObservation)) error {
		o := real
		o.Native = cloneV3Native(real.Native)
		if mutate != nil {
			mutate(&o)
		}
		c := defaultTidyV3RefsApplyCoordinator()
		c.refs.observe = func(context.Context, string, string) (tidyV3NativeObservation, error) { return o, nil }
		return c.revalidateReviewedDecision(context.Background(), p, real.Expected, target)
	}
	if err := check(nil); err != nil {
		t.Fatalf("unchanged state refused: %v", err)
	}
	index := func(o *tidyV3NativeObservation) int {
		for i, ref := range o.Native.References {
			if ref.Name == target.Name {
				return i
			}
		}
		t.Fatal("reference missing")
		return -1
	}
	cases := map[string]func(*tidyV3NativeObservation){
		"the tip moved":             func(o *tidyV3NativeObservation) { o.Native.References[index(o)].OID = strings.Repeat("c", 40) },
		"the reference is symbolic": func(o *tidyV3NativeObservation) { o.Native.References[index(o)].Symbolic = "refs/heads/other" },
		"the reference vanished": func(o *tidyV3NativeObservation) {
			i := index(o)
			o.Native.References = append(o.Native.References[:i], o.Native.References[i+1:]...)
		},
		"another reference points at it symbolically": func(o *tidyV3NativeObservation) {
			o.Native.References = append(o.Native.References, V3Reference{Name: "refs/heads/alias", Symbolic: target.Name})
		},
		"a workspace uses the branch": func(o *tidyV3NativeObservation) {
			o.Native.Registrations = append(o.Native.Registrations, V3Registration{Path: "/somewhere", HeadRef: target.Name, HeadOID: target.OID})
		},
		"a workspace is at the tip": func(o *tidyV3NativeObservation) {
			o.Native.Registrations = append(o.Native.Registrations, V3Registration{Path: "/somewhere", HeadOID: target.OID})
		},
		"the snapshot changed": func(o *tidyV3NativeObservation) { o.Expected.ReferenceDigest = strings.Repeat("d", 64) },
	}
	for name, mutate := range cases {
		if err := check(mutate); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The record must bind the exact tip and base.
	bad := target
	ev := *bad.Evidence.Integration
	d := *ev.Decision
	d.TipOID = strings.Repeat("c", 40)
	ev.Decision = &d
	bad.Evidence = &V3ProofEvidence{Integration: &ev}
	c := defaultTidyV3RefsApplyCoordinator()
	c.refs.observe = func(context.Context, string, string) (tidyV3NativeObservation, error) { return real, nil }
	if err := c.revalidateReviewedDecision(context.Background(), p, real.Expected, bad); err == nil {
		t.Error("a decision for another tip was accepted")
	}
}

func TestJournalRefusesAChangedStoredDecision(t *testing.T) {
	f := classFix(t)
	r := f.decide(t, func(tip func(string) string, base string) []map[string]any {
		return []map[string]any{validDecision("refs/heads/fresh", tip("fresh"), base)}
	}, V3PlanOptions{})
	if r.Status != "ok" {
		t.Fatalf("%+v", r.Diagnostics)
	}
	p := *r.Plan
	_, native, _, err := readTidyV3ApplyRecords(V3ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, ProtectWorktrees: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	j := newV3Journal(p, native)
	if err := validateV3Journal(j, p); err != nil {
		t.Fatalf("fresh journal: %v", err)
	}
	at := func(j *V3Journal) *V3JournalEntry {
		for i := range j.Entries {
			if j.Entries[i].Decision != nil {
				return &j.Entries[i]
			}
		}
		t.Fatal("no journal entry records the decision")
		return nil
	}
	for name, mutate := range map[string]func(*V3Journal){
		"reason changed": func(j *V3Journal) { e := at(j); d := *e.Decision; d.Reason = "changed"; e.Decision = &d },
		"kind changed":   func(j *V3Journal) { e := at(j); d := *e.Decision; d.Kind = "ai"; e.Decision = &d },
		"dropped":        func(j *V3Journal) { at(j).Decision = nil },
	} {
		t.Run(name, func(t *testing.T) {
			c := newV3Journal(p, native)
			mutate(&c)
			if validateV3Journal(c, p) == nil {
				t.Fatal("a journal with another decision was accepted")
			}
		})
	}
}
