// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Consolidation ranges and `git range-diff` pairing (FR-037). The pairing is
// evidence for the reviewer and for the suggested verdict; it never proves
// integration and never changes a disposition.

const (
	reportMaxRanges       = 4
	reportMaxRangeCommits = 256
	reportPairingSkipped  = "range pairing skipped: textconv or diff attribute"
	// The branch side holds at most 1,024 commits and the range 256, so no
	// range-diff listing has more rows than this.
	reportPairingRows = 1280
)

var reportRangeRow = regexp.MustCompile(`^\s*(?:-|[0-9]+):\s+(?:[0-9a-f]+|-+) ([=!<>]) (?:-|[0-9]+):\s+(?:[0-9a-f]+|-+)(?: .*)?$`)

var specReportRangeDiff = integrationSpec{"range-diff", reportPairingRows, bothWidths(reportPairingRows * reportRecordBytes), '\n', func(r []byte, _ int) bool {
	return len(r)+1 <= reportRecordBytes && reportRangeRow.Match(r)
}}

func rangeUsageError(selector, why string) error {
	if len(selector) > 4096 {
		selector = selector[:4096]
	}
	return failure("invalid-options", fmt.Sprintf("--consolidation-range %q %s.", selector, why))
}

// parseConsolidationRange splits `A..B`: exactly one `..`, no `...`, both sides
// non-empty and not option-like.
func parseConsolidationRange(selector string) (from, to string, err error) {
	if !validText(selector) || len(selector) > 4096 {
		return "", "", rangeUsageError(selector, "is not valid text")
	}
	from, to, ok := strings.Cut(selector, "..")
	if !ok || strings.Contains(to, "..") || strings.HasPrefix(to, ".") || strings.HasSuffix(from, ".") || from == "" || to == "" || strings.HasPrefix(from, "-") || strings.HasPrefix(to, "-") {
		return "", "", rangeUsageError(selector, "must be A..B with two revisions")
	}
	return from, to, nil
}

// resolveConsolidationRanges resolves each side to a full object id and counts
// the commits; a malformed, unresolvable or oversized range is a usage error
// naming the selector.
func (r *integrationRun) resolveConsolidationRanges(ctx context.Context, selectors []string) ([]V3ReviewRange, error) {
	if len(selectors) > reportMaxRanges {
		return nil, failure("invalid-options", "--consolidation-range may be given at most four times.")
	}
	r.ensureObjectDir()
	if r.objErr != nil {
		return nil, failure("object-dir-unavailable", "A private object directory is required to resolve consolidation ranges.")
	}
	var out []V3ReviewRange
	for _, selector := range selectors {
		from, to, err := parseConsolidationRange(selector)
		if err != nil {
			return nil, err
		}
		var ids [2]string
		for i, side := range []string{from, to} {
			lines, code, rerr := r.records(ctx, integrationCommand{args: []string{"rev-parse", "--verify", "--quiet", "--end-of-options", side + "^{commit}"}, tolerate: true}, specMergeBase)
			if rerr != nil {
				if errors.Is(rerr, errIntegrationLimit) {
					return nil, rangeUsageError(selector, "could not be resolved to a commit")
				}
				return nil, rerr
			}
			if code != 0 || len(lines) != 1 {
				return nil, rangeUsageError(selector, "could not be resolved to a commit")
			}
			ids[i] = lines[0]
		}
		n, err := r.count(ctx, "rev-list", "--count", ids[0]+".."+ids[1], "--")
		if err != nil {
			if errors.Is(err, errIntegrationLimit) {
				return nil, rangeUsageError(selector, "could not be counted")
			}
			return nil, err
		}
		if n > reportMaxRangeCommits {
			return nil, rangeUsageError(selector, fmt.Sprintf("holds %d commits, more than %d", n, reportMaxRangeCommits))
		}
		out = append(out, V3ReviewRange{Selector: selector, From: ids[0], To: ids[1], Commits: n})
	}
	return out, nil
}

// reportPairing pairs the branch's commits with each named range and keeps the
// range that pairs most (the first on a tie). range-diff runs textconv helpers
// even with --no-textconv, so it is skipped when one is configured or a diff
// attribute applies; with the patch-id class not selected the attribute check
// did not run and the pairing is skipped too.
func (r *integrationRun) reportPairing(ctx context.Context, e *V3ReportEntry, q reportRequest, reasons []string) error {
	if tidyV3HasConfigClass(r.notes, "diff.*.textconv") || slices.Contains(reasons, "diff-attribute") || !q.Policy.has(integrationPatchID) {
		e.RangePairing = &V3ReportPairing{Range: q.Ranges[0].Selector, Evidence: "superseded-candidate", Note: reportPairingSkipped, Pairs: []V3ReportPair{}}
		return nil
	}
	var best *V3ReportPairing
	bestScore := -1
	for _, rg := range q.Ranges {
		rows, err := r.lines(ctx, integrationCommand{args: []string{"range-diff", "--no-color", "--no-patch", "--abbrev=12", q.Base + ".." + q.Tip, rg.From + ".." + rg.To}}, specReportRangeDiff)
		if err != nil {
			return err
		}
		p := &V3ReportPairing{Range: rg.Selector, Evidence: "superseded-candidate", Pairs: []V3ReportPair{}}
		score := 0
		for _, row := range rows {
			pair := parseRangeDiffRow(row)
			if pair.Status == "=" || pair.Status == "!" {
				score++
			}
			if len(p.Pairs) < reportMaxPairs {
				p.Pairs = append(p.Pairs, pair)
			}
		}
		if len(rows) > reportMaxPairs {
			p.Truncated = &V3ReportTruncation{Unit: "rows", Shown: reportMaxPairs, Total: len(rows)}
		}
		if score > bestScore {
			best, bestScore = p, score
		}
	}
	e.RangePairing = best
	return nil
}

// parseRangeDiffRow reads `N: ABBREV S N: ABBREV subject`; a side that is `-`
// has no commit.
func parseRangeDiffRow(row string) V3ReportPair {
	fields := strings.Fields(row)
	// The columns are: old number, old commit, status, new number, new commit.
	pair := V3ReportPair{Status: fields[2]}
	if strings.Trim(fields[1], "-") != "" {
		pair.BranchCommit = fields[1]
	}
	if strings.Trim(fields[4], "-") != "" {
		pair.RangeCommit = fields[4]
	}
	return pair
}
