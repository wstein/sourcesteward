// SPDX-License-Identifier: EUPL-1.2
package workspace

import "slices"

// The suggested verdict and the fixed questions of the review report (FR-038).
// The verdict is a suggestion for the reviewer, never a decision.

var reportVerdicts = []string{"obviously-integrated", "looks-superseded", "has-real-unmerged-content", "unclear"}

func reportVerdictKnown(v string) bool { return slices.Contains(reportVerdicts, v) }

// reportMergeGate is the set of reasons that forbid running merge-tree.
var reportMergeGate = []string{"merge-driver", "gitlink-change", "merge-attribute", "attribute-source"}

// reportUnclearReasons are the reasons of rule 3 that make a verdict unclear;
// the last three leave no diff to judge.
var reportUnclearReasons = []string{"conflict", "merge-commits-in-range", "diff-attribute", "proof-limit", "ambiguous-merge-base", "proof-budget-exhausted", "object-dir-unavailable"}

// suggestVerdict applies the deterministic table in order: (0) the merge gate,
// (1) an empty or whitespace-only unintegrated diff, (2) a named range that
// pairs every branch commit, (3) conflicts, merge commits, a diff attribute, a
// proof limit, a missing diff or any truncation, (4) real unmerged content.
func suggestVerdict(e V3ReportEntry) V3ReportVerdict {
	for _, reason := range reportMergeGate {
		if slices.Contains(e.Reasons, reason) {
			return V3ReportVerdict{Verdict: "unclear", Confidence: "low", Basis: []string{"merge-gate:" + reason}}
		}
	}
	d := e.Diff
	if d.Source == "merge-result" {
		if d.Stat.Files == 0 && len(d.Files) == 0 {
			return V3ReportVerdict{Verdict: "obviously-integrated", Confidence: "high", Basis: []string{"unintegrated-diff-empty"}}
		}
		if d.WhitespaceOnly {
			return V3ReportVerdict{Verdict: "obviously-integrated", Confidence: "medium", Basis: []string{"unintegrated-diff-whitespace-only"}}
		}
	}
	if p := e.RangePairing; p != nil && p.Note == "" && p.Truncated == nil {
		paired, equal, unpaired := 0, 0, 0
		for _, pair := range p.Pairs {
			switch pair.Status {
			case "=":
				paired, equal = paired+1, equal+1
			case "!":
				paired++
			case "<":
				unpaired++
			}
		}
		if paired > 0 && unpaired == 0 {
			confidence := "low"
			if paired == equal {
				confidence = "medium"
			}
			return V3ReportVerdict{Verdict: "looks-superseded", Confidence: confidence, Basis: []string{"range-pairs-every-branch-commit"}}
		}
	}
	var basis []string
	for _, reason := range reportUnclearReasons {
		if slices.Contains(e.Reasons, reason) {
			basis = append(basis, "reason:"+reason)
		}
	}
	if d.Source == "unavailable" {
		basis = append(basis, "unintegrated-diff-unavailable")
	}
	if d.Truncated != nil {
		basis = append(basis, "truncated:diff")
	}
	if e.CommitsTruncated != nil {
		basis = append(basis, "truncated:commits")
	}
	if e.RangePairing != nil && e.RangePairing.Truncated != nil {
		basis = append(basis, "truncated:range-pairing")
	}
	if len(basis) > 0 {
		return V3ReportVerdict{Verdict: "unclear", Confidence: "low", Basis: basis}
	}
	if e.RangePairing == nil {
		return V3ReportVerdict{Verdict: "has-real-unmerged-content", Confidence: "high", Basis: []string{"unintegrated-diff-has-content", "no-range-named"}}
	}
	return V3ReportVerdict{Verdict: "has-real-unmerged-content", Confidence: "medium", Basis: []string{"unintegrated-diff-has-content", "range-does-not-pair-every-branch-commit"}}
}

// reportGuidance is the reviewer guidance of the report header: the questions,
// the evidence each decision needs and the rules for checking and for untrusted
// content. It is written once per report, never per entry.
func reportGuidance() V3ReportGuidance {
	return V3ReportGuidance{
		Untrusted: reviewerGuidance,
		Questions: []V3ReportQuestion{
			{ID: "Q1", Text: "Is every change in unintegrated_diff already present elsewhere on the base in another form?"},
			{ID: "Q2", Text: "Is the unintegrated diff only whitespace or formatting?"},
			{ID: "Q3", Text: "Does the range pairing show the branch's commits were consolidated?"},
			{ID: "Q4", Text: "Is any change in the diff something the owner would want to keep?"},
		},
		Decisions: []V3ReportDecisionGuide{
			{Decision: "integrated", Evidence: "Every change of the branch is already on the base in another form. Cite the base commit OIDs (or the path and line) that carry it. A similar subject alone is not evidence."},
			{Decision: "keep", Evidence: "The branch holds content the owner may want, or you cannot show that the base carries it. When in doubt, keep."},
			{Decision: "drop", Evidence: "The content is deliberately abandoned, for example stale or replaced by a different design. Cite why."},
		},
		ReadOnlyGit: "You may check with read-only git in a clone of the repository: git log, git diff, git cherry, git range-diff, git merge-base --is-ancestor, git show, git grep, git ls-tree. Run nothing that writes; cite the command and the base commit or path that settles the point. Where an entry says unavailable or truncated, run the check yourself or keep the branch.",
	}
}
