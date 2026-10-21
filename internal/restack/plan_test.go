// SPDX-License-Identifier: EUPL-1.2
package restack

import (
	"context"
	"strings"
	"testing"
)

func TestTheMotivatingScenarioIsEvidenceClean(t *testing.T) {
	s := rebaseMerged(t)
	res := s.plan(t)
	if res.Status != StatusOK || res.Outcome != OutcomeClean || len(res.Diagnostics) != 0 {
		t.Fatalf("%s %s %+v", res.Status, res.Outcome, res.Diagnostics)
	}
	if res.Kind != "restack-plan" || res.SchemaVersion != 1 || res.Backend != "git" || res.Notice != Notice || res.Boundary.Reached != nil || res.ObservedAt == "" {
		t.Fatalf("envelope %+v", res)
	}
	e := res.Evidence
	if !e.ParentDropped.AllDropped || !e.RangePairing.AllEqual || !e.OwnDiffEquality.AllEqual || e.Identity.State != "pass" || e.Overlap.Indicator != "none" {
		t.Fatalf("%+v", e)
	}
	found := map[string]bool{}
	for _, l := range res.Limitations {
		found[l] = true
	}
	if !found["semantic-breaks-not-detected"] || !found["prediction-is-not-apply"] {
		t.Fatalf("limitations %v", res.Limitations)
	}
}

func TestWithoutAnIdentityFlagTheOutcomeNeedsAttention(t *testing.T) {
	s := rebaseMerged(t)
	o := s.options()
	o.Identities = nil
	if res := Plan(context.Background(), o); res.Outcome != OutcomeAttention || res.Status != StatusOK {
		t.Fatalf("%s %s", res.Status, res.Outcome)
	}
}

func TestMoreThanMaxPathsIsIncompleteNeverTruncated(t *testing.T) {
	s := rebaseMergedWith(t, func(r *repo) { r.commit("B", map[string]string{"b": "b\n"}) }, func(r *repo) {
		files := map[string]string{}
		for i := 0; i < MaxPaths+1; i++ {
			files["many/f"+itoa(i)] = "x\n"
		}
		r.commit("C1", files)
	}, func(r *repo) {})
	res := s.plan(t)
	if res.Status != StatusIncomplete || firstCode(res) != "bound-exceeded" || res.Boundary.Reached == nil || *res.Boundary.Reached != "max_paths" || res.Evidence != nil || res.Outcome != OutcomeNone {
		t.Fatalf("%s %+v %v", res.Status, res.Diagnostics, res.Boundary.Reached)
	}
}

func TestAResultOverTheByteBoundIsIncomplete(t *testing.T) {
	old := resultLimit
	resultLimit = 1000
	defer func() { resultLimit = old }()
	res := rebaseMerged(t).plan(t)
	if res.Status != StatusIncomplete || res.Evidence != nil || res.Boundary.Reached == nil || *res.Boundary.Reached != "max_result_bytes" || res.Outcome != OutcomeNone {
		t.Fatalf("%s %+v", res.Status, res.Diagnostics)
	}
}

func TestSubjectsAreBoundedAndInert(t *testing.T) {
	long := strings.Repeat("x", 500)
	s := rebaseMergedWith(t, func(r *repo) { r.commit("B", map[string]string{"b": "b\n"}) },
		func(r *repo) { r.commit(long, map[string]string{"c": "c\n"}) }, func(r *repo) {})
	got := s.plan(t).Input.Moved.Commits[0]
	if len(got.Subject) != 200 || len(got.SubjectSHA256) != 64 {
		t.Fatalf("%d %q", len(got.Subject), got.SubjectSHA256)
	}
	if subject([]byte("a\x1b[31mred\x00\xff")) != `a\u001b[31mred\u0000\xff` {
		t.Fatalf("%q", subject([]byte("a\x1b[31mred\x00\xff")))
	}
}
