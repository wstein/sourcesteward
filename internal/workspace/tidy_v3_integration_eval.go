// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"errors"
	"slices"
)

// integrationVerdict is the outcome of the forge-free classes for one tip. It is
// converted to plan evidence by the planner; BinaryPaths feeds the report.
type integrationVerdict struct {
	Disposition   string // proven, needs-review, retained, or "" when no class and no reason applies
	Class         string
	Reasons       []string
	Flags         []string
	Matched       int
	Unmatched     int
	Empty         int
	SkippedMerges int
	BinaryPaths   []string
	BaseMatches   []string
	// CommitMatches is the patch-id match of each branch commit (report only).
	CommitMatches map[string]string
	// Limit is the first bound a proof-limit reason came from (report only).
	Limit *integrationLimitError
}

func firstLimit(a, b *integrationLimitError) *integrationLimitError {
	if a != nil {
		return a
	}
	return b
}

// reasonOrder sorts reasons by the contract's table order, without duplicates.
func reasonOrder(reasons []string) []string {
	out := []string{}
	for _, r := range integrationReasons {
		if slices.Contains(reasons, r.Name) {
			out = append(out, r.Name)
		}
	}
	return out
}

// review is the verdict of a tip that no class proved, for the given reasons.
func reviewVerdict(v integrationVerdict, reasons ...string) integrationVerdict {
	v.Disposition, v.Class, v.Reasons = integrationReviewDisposition, "", reasonOrder(reasons)
	return v
}

// evaluate runs the selected classes other than ancestor for the tip against
// the base, in the order of FR-007: no-net-change, patch-id (default and
// verbatim in one pass), merge-noop. Every selected class runs; the strongest
// deletable class is reported: patch-id, merge-noop, no-net-change. A tip with
// a hard stop is retained before any command runs. The caller starts the tip
// budget with beginTip.
func (r *integrationRun) evaluate(ctx context.Context, base, tip string, policy V3ProofPolicy) (integrationVerdict, error) {
	v := integrationVerdict{Reasons: []string{}, Flags: []string{}}
	if len(r.hardStops) > 0 {
		v.Disposition, v.Reasons = integrationRetainedDisposition, reasonOrder(r.hardStops)
		return v, nil
	}
	r.ensureObjectDir()
	if r.objErr != nil {
		return reviewVerdict(v, "object-dir-unavailable"), nil
	}
	var reasons []string
	// stop maps the failures that end the evaluation into a verdict.
	stop := func(err error) (integrationVerdict, bool, error) {
		switch {
		case errors.Is(err, errIntegrationBudget):
			return reviewVerdict(v, append(reasons, "proof-budget-exhausted")...), true, nil
		case errors.Is(err, errIntegrationLimit):
			errors.As(err, &v.Limit)
			return reviewVerdict(v, append(reasons, "proof-limit")...), true, nil
		}
		return v, true, err
	}
	bases, err := r.mergeBases(ctx, base, tip)
	if err != nil {
		out, _, e := stop(err)
		return out, e
	}
	if len(bases) != 1 {
		return reviewVerdict(v, "ambiguous-merge-base"), nil
	}
	tipTree, mbTree, baseTree, err := r.trees(ctx, tip, bases[0], base)
	if err != nil {
		out, _, e := stop(err)
		return out, e
	}
	netNothing := false
	if policy.has(integrationNoNetChange) {
		r.traceClass(integrationNoNetChange)
		netNothing = tipTree == mbTree
	}
	patchProven := false
	if policy.has(integrationPatchID) {
		r.traceClass(integrationPatchID)
		pr, err := r.patchID(ctx, base, tip, bases[0], tipTree, baseTree)
		if err != nil {
			out, _, e := stop(err)
			return out, e
		}
		v.Matched, v.Unmatched, v.Empty, v.SkippedMerges, v.BinaryPaths, v.BaseMatches = pr.Matched, pr.Unmatched, pr.Empty, pr.SkippedMerges, pr.BinaryPaths, pr.BaseMatches
		v.CommitMatches = pr.CommitMatches
		reasons = append(reasons, pr.Reasons...)
		v.Limit = firstLimit(v.Limit, pr.Limit)
		switch {
		case pr.Proven && !pr.Whitespace:
			patchProven = true
		case pr.Proven && pr.Whitespace && policy.acceptsWhitespace():
			patchProven, v.Flags = true, []string{integrationFlagAcceptedWhitespace}
		case pr.Whitespace:
			// Deletable only with the policy flag; also reported when the class
			// is otherwise unproven (for instance with partial-match), so a
			// reviewer sees the formatting difference.
			reasons = append(reasons, integrationFlagWhitespace)
			v.Flags = []string{integrationFlagWhitespace}
		}
	}
	noopProven := false
	if policy.has(integrationMergeNoop) {
		r.traceClass(integrationMergeNoop)
		mr, err := r.mergeNoop(ctx, base, tip, bases[0], tipTree, mbTree, baseTree)
		if err != nil {
			out, _, e := stop(err)
			return out, e
		}
		noopProven = mr.Proven
		reasons = append(reasons, mr.Reasons...)
		v.Limit = firstLimit(v.Limit, mr.Limit)
	}
	switch {
	case patchProven:
		v.Disposition, v.Class = integrationProvenDisposition, integrationPatchID
	case noopProven:
		v.Disposition, v.Class, v.Flags = integrationProvenDisposition, integrationMergeNoop, []string{}
		v.Matched, v.Unmatched, v.Empty, v.SkippedMerges = 0, 0, 0, 0
	case netNothing:
		v.Disposition, v.Class, v.Flags = integrationProvenDisposition, integrationNoNetChange, []string{}
		v.Matched, v.Unmatched, v.Empty, v.SkippedMerges = 0, 0, 0, 0
	case len(reasons) > 0:
		return reviewVerdict(v, reasons...), nil
	}
	return v, nil
}
