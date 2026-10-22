// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"fmt"
	"strings"
	"testing"
)

// The per-entry report bounds (16 files, 1,000 diff lines, 50 commits) used to
// be fixed, so a reviewer could never clear a branch that is larger than that:
// its entry was always "not fully reviewed". Each is now an option with a
// ceiling, and a decision applies once the entry is complete under the bounds.
func TestDecisionsClearBranchesLargerThanTheDefaultEntryBounds(t *testing.T) {
	f := reportRepo(t)
	big := func(name string, files map[string]string, commits int) {
		f.branch(name, f.main)
		for i := 0; i < commits; i++ {
			f.commit(fmt.Sprintf("%s step %d", name, i), map[string]string{name + "-steps.txt": fmt.Sprintf("step %d\n", i)})
		}
		if files != nil {
			f.commit(name+" files", files)
		}
		f.checkout(f.main)
	}
	wide := map[string]string{}
	for i := 0; i < 20; i++ {
		wide[fmt.Sprintf("wide/file-%02d.txt", i)] = "line\n"
	}
	big("wide", wide, 0)
	big("long", map[string]string{"long.txt": strings.Repeat("a line of text\n", 1500)}, 0)
	big("history", nil, 60)
	f.commit("move the base", map[string]string{"base-moved.txt": "m\n"})
	for _, tc := range []struct {
		name string
		opt  V3PlanOptions
	}{
		{"wide", V3PlanOptions{ReportMaxFiles: 64}},
		{"long", V3PlanOptions{ReportMaxLines: 5000, ReportMaxDiffBytes: 1 << 20}},
		{"history", V3PlanOptions{ReportMaxCommits: 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			build := func(tip func(string) string, base string) []map[string]any {
				return []map[string]any{validDecision("refs/heads/"+tc.name, tip(tc.name), base)}
			}
			before := f.decide(t, build, V3PlanOptions{ReportMaxDiffBytes: 1 << 20})
			if before.Status != "ok" || candidateByName(t, before.Plan, tc.name).Decision != "retain" || !strings.Contains(strings.Join(noteCodes(before), " "), "decision-not-applicable") {
				t.Fatalf("the default bounds should leave %s not fully reviewed: %+v %v", tc.name, before.Diagnostics, noteCodes(before))
			}
			after := f.decide(t, build, tc.opt)
			if after.Status != "ok" {
				t.Fatalf("%+v", after.Diagnostics)
			}
			if c := candidateByName(t, after.Plan, tc.name); c.Decision != "delete" || c.Reason != "reviewed-integrated" {
				t.Fatalf("the raised bound did not let the decision apply: %+v notes %+v", c, after.Plan.Review.Notes)
			}
		})
	}
}

func TestReportEntryBoundsAreResolvedAndBoundIntoThePlan(t *testing.T) {
	b, err := resolveReportBounds(reportBounds{})
	if err != nil || b.MaxFiles != 16 || b.MaxLines != 1000 || b.MaxCommits != 50 {
		t.Fatalf("defaults: %+v %v", b, err)
	}
	for _, bad := range []reportBounds{{MaxFiles: -1}, {MaxFiles: reportFilesCeiling + 1}, {MaxLines: reportLinesCeiling + 1}, {MaxCommits: reportCommitsCeiling + 1}} {
		if _, err := resolveReportBounds(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	f := reportRepo(t)
	r := f.planReport(V3PlanOptions{ReportDir: gitTempDir(t), ReportMaxFiles: 64})
	if r.Status != "ok" || r.Plan.Review == nil || r.Plan.Review.MaxFiles != 64 || r.Plan.Review.MaxLines != 0 {
		t.Fatalf("binding: %+v", r.Plan.Review)
	}
	if !validV3ReviewBinding(*r.Plan.Review, *r.Plan) {
		t.Fatal("a binding with a raised bound is invalid")
	}
	forged := *r.Plan.Review
	forged.MaxFiles = reportFilesCeiling + 1
	if validV3ReviewBinding(forged, *r.Plan) {
		t.Fatal("a binding above the ceiling is valid")
	}
}
