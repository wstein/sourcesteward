// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"reflect"
	"testing"
)

// Spec 031 T031: the deterministic suggested verdict (contracts/review-report.md
// "Suggested verdict") and the fixed questions.

func verdictEntry(mutate func(*V3ReportEntry)) V3ReportEntry {
	e := syntheticEntry("v", 5)
	e.Diff.Source = "merge-result"
	if mutate != nil {
		mutate(&e)
	}
	return e
}

func pairing(statuses ...string) *V3ReportPairing {
	p := &V3ReportPairing{Range: "A..B", Evidence: "superseded-candidate", Pairs: []V3ReportPair{}}
	for _, s := range statuses {
		p.Pairs = append(p.Pairs, V3ReportPair{Status: s, BranchCommit: "aaaaaaaaaaaa", RangeCommit: "bbbbbbbbbbbb"})
	}
	return p
}

func TestSuggestedVerdictTable(t *testing.T) {
	empty := func(e *V3ReportEntry) { e.Diff = emptyReportDiff("merge-result") }
	cases := []struct {
		name       string
		mutate     func(*V3ReportEntry)
		verdict    string
		confidence string
	}{
		{"empty diff", empty, "obviously-integrated", "high"},
		{"whitespace-only diff", func(e *V3ReportEntry) { e.Diff.WhitespaceOnly = true }, "obviously-integrated", "medium"},
		{"every commit paired equal", func(e *V3ReportEntry) { e.RangePairing = pairing("=", "=") }, "looks-superseded", "medium"},
		{"paired with a changed patch", func(e *V3ReportEntry) { e.RangePairing = pairing("=", "!") }, "looks-superseded", "low"},
		{"range-only commits do not block", func(e *V3ReportEntry) { e.RangePairing = pairing("=", ">") }, "looks-superseded", "medium"},
		{"a branch commit without a pair", func(e *V3ReportEntry) { e.RangePairing = pairing("=", "<") }, "has-real-unmerged-content", "medium"},
		{"a range that pairs nothing", func(e *V3ReportEntry) { e.RangePairing = pairing(">") }, "has-real-unmerged-content", "medium"},
		{"real content without a range", nil, "has-real-unmerged-content", "high"},
		{"conflict", func(e *V3ReportEntry) { e.Reasons = []string{"conflict"} }, "unclear", "low"},
		{"merge commits in range", func(e *V3ReportEntry) { e.Reasons = []string{"merge-commits-in-range"} }, "unclear", "low"},
		{"diff attribute", func(e *V3ReportEntry) { e.Reasons = []string{"diff-attribute"} }, "unclear", "low"},
		{"proof limit", func(e *V3ReportEntry) { e.Reasons = []string{"proof-limit"} }, "unclear", "low"},
		{"truncated diff", func(e *V3ReportEntry) { e.Diff.Truncated = &V3ReportTruncation{Unit: "lines", Shown: 1, Total: 2} }, "unclear", "low"},
		{"truncated commits", func(e *V3ReportEntry) { e.CommitsTruncated = &V3ReportTruncation{Unit: "commits", Shown: 1, Total: 2} }, "unclear", "low"},
		{"truncated pairing", func(e *V3ReportEntry) {
			e.RangePairing = pairing("=")
			e.RangePairing.Truncated = &V3ReportTruncation{Unit: "rows", Shown: 1, Total: 2}
		}, "unclear", "low"},
		{"no diff available", func(e *V3ReportEntry) { e.Diff = emptyReportDiff("unavailable") }, "unclear", "low"},
		{"whitespace-only wins over a later rule", func(e *V3ReportEntry) { e.Diff.WhitespaceOnly = true; e.Reasons = []string{"merge-commits-in-range"} }, "obviously-integrated", "medium"},
		{"paired wins over a later rule", func(e *V3ReportEntry) { e.RangePairing = pairing("="); e.Reasons = []string{"conflict"} }, "looks-superseded", "medium"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := verdictEntry(c.mutate)
			got := suggestVerdict(e)
			if got.Verdict != c.verdict || got.Confidence != c.confidence || len(got.Basis) == 0 {
				t.Fatalf("%+v, want %s/%s", got, c.verdict, c.confidence)
			}
		})
	}
}

func TestSuggestedVerdictMergeGateComesFirst(t *testing.T) {
	for _, reason := range []string{"merge-driver", "gitlink-change", "merge-attribute", "attribute-source"} {
		for name, mutate := range map[string]func(*V3ReportEntry){
			"empty diff":  func(e *V3ReportEntry) { e.Diff = emptyReportDiff("merge-result") },
			"whitespace":  func(e *V3ReportEntry) { e.Diff.WhitespaceOnly = true },
			"paired":      func(e *V3ReportEntry) { e.RangePairing = pairing("=") },
			"real change": nil,
		} {
			e := verdictEntry(mutate)
			e.Reasons = []string{reason}
			e.Diff.Source = "branch-against-merge-base"
			got := suggestVerdict(e)
			if got.Verdict != "unclear" || got.Confidence != "low" || len(got.Basis) == 0 || got.Basis[0] != "merge-gate:"+reason {
				t.Errorf("%s/%s: %+v", reason, name, got)
			}
		}
	}
}

func TestSuggestedVerdictIsEvidenceNotADisposition(t *testing.T) {
	e := verdictEntry(func(e *V3ReportEntry) { e.RangePairing = pairing("=") })
	for _, v := range []string{"obviously-integrated", "looks-superseded", "has-real-unmerged-content", "unclear"} {
		if !reportVerdictKnown(v) {
			t.Errorf("verdict %q unknown", v)
		}
	}
	if got := suggestVerdict(e); got.Verdict == "" || reportVerdictKnown("integrated") {
		t.Errorf("a verdict must not be a decision: %+v", got)
	}
}

func TestReportQuestionsAreTheFixedTemplates(t *testing.T) {
	q := reportQuestions()
	ids := []string{}
	for _, x := range q {
		ids = append(ids, x.ID)
		if x.Text == "" {
			t.Errorf("%s has no text", x.ID)
		}
	}
	if !reflect.DeepEqual(ids, []string{"Q1", "Q2", "Q3", "Q4", "guidance"}) || q[4].Text != reviewerGuidance {
		t.Fatalf("questions %+v", q)
	}
	if q[0].Text != "Is every change in unintegrated_diff already present elsewhere on the base in another form?" || q[3].Text != "Is any change in the diff something the owner would want to keep?" {
		t.Errorf("template text changed: %+v", q)
	}
	if !reflect.DeepEqual(reportAllowedDecisions(), []string{"integrated", "keep", "drop"}) {
		t.Errorf("allowed decisions %v", reportAllowedDecisions())
	}
}
