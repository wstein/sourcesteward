// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"fmt"
	"path/filepath"
)

// The review report inside a local-reference plan (FR-031, FR-032): which
// references need review, the report built for them and the summary every plan
// result carries.

func defaultWriteReport(dir string, markdown, jsonData []byte) error {
	return writeReportFiles(dir, markdown, jsonData, defaultReportFileOps())
}

func validV3ReviewBinding(b V3ReviewBinding, p V3Plan) bool {
	r, err := resolveReportBounds(reportBounds{MaxEntries: b.MaxEntries, MaxBytes: b.MaxBytes, MaxDiffBytes: b.MaxDiffBytes})
	if err != nil || b.MaxEntries <= 0 || b.MaxBytes <= 0 || b.MaxDiffBytes <= 0 || r != (reportBounds{MaxEntries: b.MaxEntries, MaxBytes: b.MaxBytes, MaxDiffBytes: b.MaxDiffBytes}) {
		return false
	}
	if b.Ranges == nil || len(b.Ranges) > reportMaxRanges || !validV3ReviewNotes(b.Notes) || b.DecisionsDigest != "" && !v3Hash(b.DecisionsDigest) {
		return false
	}
	for _, rg := range b.Ranges {
		if _, _, err := parseConsolidationRange(rg.Selector); err != nil || !v3OID(rg.From, p.Expected.Identity.ObjectFormat) || !v3OID(rg.To, p.Expected.Identity.ObjectFormat) || rg.Commits < 0 || rg.Commits > reportMaxRangeCommits {
			return false
		}
	}
	return true
}

// reviewBindingFor is the binding of the plan options, nil when no report
// option was given.
func reviewBindingFor(o V3PlanOptions, ranges []V3ReviewRange) (*V3ReviewBinding, reportBounds, error) {
	bounds, err := resolveReportBounds(o.reportBounds())
	if err != nil || !o.reviewRequested() {
		return nil, bounds, err
	}
	if ranges == nil {
		ranges = []V3ReviewRange{}
	}
	return &V3ReviewBinding{MaxEntries: bounds.MaxEntries, MaxBytes: bounds.MaxBytes, MaxDiffBytes: bounds.MaxDiffBytes, Ranges: ranges, Notes: []V3ReviewNote{}}, bounds, nil
}

// reportRoots are the places a report must not be written into.
func reportRoots(source, state string, id NativeIdentity, registrations []string) []string {
	roots := []string{source, state, id.WorkspaceRoot, id.RepositoryDir, id.GitDir}
	roots = append(roots, registrations...)
	for i, r := range roots {
		if r != "" {
			roots[i] = filepath.Clean(r)
		}
	}
	return roots
}

func needsReview(c V3Candidate) bool {
	return c.Decision == "retain" && c.Reason == integrationReviewDisposition && c.Evidence != nil && c.Evidence.Integration != nil
}

// reviewCounts counts the plan's references per bucket and reason and lists the
// retained ones (FR-031).
func reviewCounts(p V3Plan) V3ReportSummary {
	s := V3ReportSummary{Reasons: map[string]int{}, RetainedRefs: []V3ReportRetained{}}
	for _, c := range p.Candidates {
		switch {
		case c.Decision == "delete":
			s.Proven++
		case needsReview(c):
			s.NeedsReview++
			for _, reason := range c.Evidence.Integration.Reasons {
				s.Reasons[reason]++
			}
		default:
			s.Retained++
			reason := c.Reason
			if c.Reason != reviewedKeep && c.Evidence != nil && c.Evidence.Integration != nil && len(c.Evidence.Integration.Reasons) > 0 {
				reason = c.Evidence.Integration.Reasons[0]
				for _, r := range c.Evidence.Integration.Reasons {
					s.Reasons[r]++
				}
			} else {
				s.Reasons[reason]++
			}
			s.RetainedRefs = append(s.RetainedRefs, V3ReportRetained{Branch: untrusted(c.Name), Reason: reason})
		}
	}
	return s
}

// buildReviewReport collects the entries of the first MaxEntries needs-review
// references and renders both files. Tips shared by several references are
// collected once.
func (c tidyV3RefsCoordinator) buildReviewReport(ctx context.Context, run *integrationRun, p V3Plan, verdicts map[string]integrationVerdict, bounds reportBounds) (renderedReport, error) {
	doc := reportDocument{Base: V3ReportBase{Selector: p.Expected.BaseSelector, OID: p.Expected.BaseCommit}, Bounds: bounds, Counts: reviewCounts(p), Entries: []V3ReportEntry{}}
	collected := map[string]V3ReportEntry{}
	seen := 0
	for _, cand := range p.Candidates {
		if !needsReview(cand) {
			continue
		}
		seen++
		if seen > bounds.MaxEntries {
			doc.Omitted = append(doc.Omitted, V3ReportOmitted{Branch: untrusted(cand.Name), Reasons: append([]string{}, cand.Evidence.Integration.Reasons...)})
			continue
		}
		entry, ok := collected[cand.OID]
		if !ok {
			var err error
			entry, err = run.collectReportEntry(ctx, reportRequest{Base: p.Expected.BaseCommit, Tip: cand.OID, Verdict: verdicts[cand.OID], Policy: *p.ProofPolicy, Bounds: bounds, Ranges: p.Review.Ranges})
			if err != nil {
				return renderedReport{}, err
			}
			collected[cand.OID] = entry
		}
		entry.Branch = untrusted(cand.Name)
		doc.Entries = append(doc.Entries, entry)
	}
	return renderReport(doc)
}

// reviewSummary is the summary of the plan result.
func reviewSummary(p V3Plan, rendered *renderedReport) *V3ReviewSummary {
	counts := reviewCounts(p)
	s := &V3ReviewSummary{Proven: counts.Proven, NeedsReview: counts.NeedsReview, Retained: counts.Retained, Reasons: counts.Reasons}
	if rendered != nil {
		s.ReportWritten, s.Entries, s.Omitted = true, len(rendered.Shown), len(rendered.Omitted)
	}
	return s
}

// applyDecisions applies the decisions to the needs-review references of the
// plan, in file order (FR-040). A decision that does not apply is ignored with a
// note; it is never an error. The report the reviewer saw decides whether an
// entry was fully reviewed.
func applyDecisions(p *V3Plan, file V3DecisionFile, rendered renderedReport) {
	byName := map[string]int{}
	for i, c := range p.Candidates {
		byName[c.Name] = i
	}
	shown := map[string]bool{}
	for _, name := range rendered.Shown {
		shown[name] = true
	}
	note := func(code, branch, detail string) {
		p.Review.Notes = append(p.Review.Notes, V3ReviewNote{Code: code, Branch: branch, Detail: detail})
	}
	for _, d := range file.Decisions {
		i, ok := byName[d.Branch]
		if !ok {
			note(noteDecisionNotApplied, d.Branch, "decision-not-applicable: the reference is absent from the plan")
			continue
		}
		c := p.Candidates[i]
		switch {
		case d.TipOID != c.OID || d.BaseOID != p.Expected.BaseCommit:
			note(noteStaleDecision, d.Branch, fmt.Sprintf("decided tip %s and base %s; current tip %s and base %s", d.TipOID, d.BaseOID, c.OID, p.Expected.BaseCommit))
		case c.Decision == "delete":
			note(noteDecisionNotApplied, d.Branch, "decision-not-applicable: the reference is already proven")
		case !needsReview(c):
			note(noteDecisionNotApplied, d.Branch, "decision-not-applicable: the reference is protected or a hard stop ("+c.Reason+")")
		case d.Decision == decisionNeedsHuman:
			// The reviewer could not decide: the reference stays under review and
			// the note carries the reason for the owner.
			note(noteNeedsHuman, d.Branch, "needs-human ("+d.Kind+" reviewer "+d.Reviewer+"): "+clipText(d.Reason, decisionReasonLimit))
		case !shown[d.Branch] || rendered.Truncated[d.Branch]:
			note(noteDecisionNotApplied, d.Branch, "decision-not-applicable: not-fully-reviewed")
		default:
			p.Candidates[i] = decided(*p, c, d)
		}
	}
}

// decided is the candidate after an applied decision.
func decided(p V3Plan, c V3Candidate, d V3DecisionRecord) V3Candidate {
	rec := d
	prior := *c.Evidence.Integration
	if d.Decision == "keep" {
		prior.Decision, prior.Disposition = &rec, integrationRetainedDisposition
		c.Reason, c.Evidence = reviewedKeep, &V3ProofEvidence{Integration: &prior}
		return c
	}
	class := reviewedIntegrated
	if d.Decision == "drop" {
		class = reviewedDrop
	}
	c.Decision, c.Reason, c.Proof = "delete", class, class
	c.Evidence = &V3ProofEvidence{Integration: &V3IntegrationEvidence{Class: class, Strength: "reviewer-decision", Disposition: integrationProvenDisposition, Reasons: []string{}, Flags: []string{},
		BaseSelector: p.Expected.BaseSelector, BaseOID: p.Expected.BaseCommit, TipOID: c.OID, Decision: &rec}}
	return c
}
