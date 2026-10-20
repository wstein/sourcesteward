// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"path/filepath"
)

// The review report inside a local-reference plan (FR-031, FR-032): which
// references need review, the report built for them and the summary every plan
// result carries.

func defaultWriteReport(dir string, markdown, jsonData []byte) error {
	return writeReportFiles(dir, markdown, jsonData, defaultReportFileOps())
}

func validV3ReviewBinding(b V3ReviewBinding) bool {
	r, err := resolveReportBounds(reportBounds{MaxEntries: b.MaxEntries, MaxBytes: b.MaxBytes, MaxDiffBytes: b.MaxDiffBytes})
	return err == nil && b.MaxEntries > 0 && b.MaxBytes > 0 && b.MaxDiffBytes > 0 && r == reportBounds{MaxEntries: b.MaxEntries, MaxBytes: b.MaxBytes, MaxDiffBytes: b.MaxDiffBytes}
}

// reviewBindingFor is the binding of the plan options, nil when no report
// option was given.
func reviewBindingFor(o V3PlanOptions) (*V3ReviewBinding, reportBounds, error) {
	bounds, err := resolveReportBounds(o.reportBounds())
	if err != nil || !o.reviewRequested() {
		return nil, bounds, err
	}
	return &V3ReviewBinding{MaxEntries: bounds.MaxEntries, MaxBytes: bounds.MaxBytes, MaxDiffBytes: bounds.MaxDiffBytes}, bounds, nil
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
			if c.Evidence != nil && c.Evidence.Integration != nil && len(c.Evidence.Integration.Reasons) > 0 {
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
			entry, err = run.collectReportEntry(ctx, reportRequest{Base: p.Expected.BaseCommit, Tip: cand.OID, Verdict: verdicts[cand.OID], Policy: *p.ProofPolicy, Bounds: bounds})
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
