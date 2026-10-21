// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// Spec 031 T035: the review report and decision flags.

func TestTidyV3ReviewFlagsReachThePlanner(t *testing.T) {
	base := []string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main"}
	called, got, err := runProofCLI(base)
	if err != nil || !called || got.ReportDir != "" || got.ReportMaxEntries != 0 || got.DecisionsFile != "" || len(got.ConsolidationRanges) != 0 {
		t.Fatalf("defaults: err=%v %+v", err, got)
	}
	args := append(append([]string{}, base...), "--report-dir", "/tmp/report", "--report-max-entries", "4096", "--report-max-bytes", "16777216", "--report-max-diff-bytes=1048576",
		"--consolidation-range", "a..b", "--consolidation-range", "c..d", "--decisions", "/tmp/decisions.json")
	called, got, err = runProofCLI(args)
	if err != nil || !called || got.ReportDir != "/tmp/report" || got.ReportMaxEntries != 4096 || got.ReportMaxBytes != 16<<20 || got.ReportMaxDiffBytes != 1<<20 ||
		!reflect.DeepEqual(got.ConsolidationRanges, []string{"a..b", "c..d"}) || got.DecisionsFile != "/tmp/decisions.json" {
		t.Fatalf("flags: err=%v %+v", err, got)
	}
}

func TestTidyV3ReviewFlagMisuseExitsBeforeNativeWork(t *testing.T) {
	base := []string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main"}
	with := func(extra ...string) []string { return append(append([]string{}, base...), extra...) }
	retire := []string{"plan", "--contract", "3", "--base", "HEAD", "--task", "t", "--owner", "o", "--state-dir", "/tmp/state", "--remove-registration", "--remove-files", "/tmp/task"}
	cases := map[string][]string{
		"relative report dir":      with("--report-dir", "reports"),
		"non-canonical report dir": with("--report-dir", "/tmp/a/../b"),
		"empty report dir":         with("--report-dir", ""),
		"report dir repeated":      with("--report-dir", "/tmp/a", "--report-dir", "/tmp/b"),
		"entries zero":             with("--report-max-entries", "0"),
		"entries negative":         with("--report-max-entries", "-1"),
		"entries over the ceiling": with("--report-max-entries", "4097"),
		"bytes over the ceiling":   with("--report-max-bytes", "16777217"),
		"diff bytes over":          with("--report-max-diff-bytes", "1048577"),
		"entries not a number":     with("--report-max-entries", "many"),
		"more than four ranges":    with("--consolidation-range", "a..b", "--consolidation-range", "a..b", "--consolidation-range", "a..b", "--consolidation-range", "a..b", "--consolidation-range", "a..b"),
		"empty range":              with("--consolidation-range", ""),
		"relative decisions":       with("--decisions", "decisions.json"),
		"decisions repeated":       with("--decisions", "/tmp/a.json", "--decisions", "/tmp/b.json"),
		"report dir without 3":     {"plan", "--base", "refs/heads/main", "--report-dir", "/tmp/report"},
		"range without 3":          {"plan", "--base", "refs/heads/main", "--consolidation-range", "a..b"},
		"decisions without 3":      {"plan", "--base", "refs/heads/main", "--decisions", "/tmp/d.json"},
		"entries without 3":        {"plan", "--base", "refs/heads/main", "--report-max-entries", "3"},
		"report with retirement":   append(append([]string{}, retire...), "--report-dir", "/tmp/report"),
		"decisions with tracking":  with("--online", "--tracking-remote", "origin", "--decisions", "/tmp/d.json"),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			called, _, err := runProofCLI(args)
			var runtime runtimeFailure
			if err == nil || errors.As(err, &runtime) || called {
				t.Fatalf("misuse reached the planner: err=%v called=%t", err, called)
			}
		})
	}
}

func TestTidyV3HumanOutputAgreesWithJSONForTheReview(t *testing.T) {
	oid := strings.Repeat("b", 40)
	rec := &workspace.V3DecisionRecord{Branch: "refs/heads/topic", TipOID: oid, BaseOID: strings.Repeat("a", 40), Decision: "integrated", Reason: "on the base", Reviewer: "claude", Kind: "ai"}
	ev := &workspace.V3IntegrationEvidence{Class: "reviewed-integrated", Strength: "reviewer-decision", Disposition: "proven", Reasons: []string{}, Flags: []string{}, BaseSelector: "refs/heads/main", BaseOID: strings.Repeat("a", 40), TipOID: oid, Decision: rec}
	r := workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git",
		ReviewSummary: &workspace.V3ReviewSummary{Proven: 1, NeedsReview: 2, Retained: 3, Reasons: map[string]int{"conflict": 2, "workspace-used": 1}, ReportWritten: true, Entries: 2, Omitted: 1},
		Plan: &workspace.V3Plan{
			Review: &workspace.V3ReviewBinding{MaxEntries: 512, MaxBytes: 4 << 20, MaxDiffBytes: 64 << 10, Ranges: []workspace.V3ReviewRange{{Selector: "a..b", From: oid, To: oid, Commits: 3}}, DecisionsDigest: strings.Repeat("c", 64),
				Notes: []workspace.V3ReviewNote{{Code: "stale-decision", Branch: "refs/heads/old", Detail: "decided tip x"}}},
			Candidates: []workspace.V3Candidate{{Name: "refs/heads/topic", Kind: "branch", OID: oid, Decision: "delete", Reason: "reviewed-integrated", Proof: "reviewed-integrated", WorkspacePaths: []string{}, Evidence: &workspace.V3ProofEvidence{Integration: ev}}}}}
	var human bytes.Buffer
	if err := renderTidyV3Text(&human, r); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{
		"Review summary: proven=1 needs_review=2 retained=3 report_written=true entries=2 omitted=1",
		`Review reason: "conflict" count=2`, `Review reason: "workspace-used" count=1`,
		"Review bounds: max_entries=512 max_bytes=4194304 max_diff_bytes=65536",
		`Review range: "a..b"`, "commits=3", `Review decisions digest: "` + strings.Repeat("c", 64) + `"`,
		`Review note: "stale-decision" branch="refs/heads/old" detail="decided tip x"`,
		`Decision: branch="refs/heads/topic" decision="integrated" kind="ai" reviewer="claude" reason="on the base"`,
	} {
		if !strings.Contains(human.String(), fact) {
			t.Errorf("fact %s missing from the human output:\n%s", fact, human.String())
		}
	}
}
