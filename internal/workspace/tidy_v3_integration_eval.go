// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
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

// evaluate runs the selected classes other than ancestor for the tip against
// the base. A tip with a hard stop is retained before any command runs.
func (r *integrationRun) evaluate(ctx context.Context, base, tip string, policy V3ProofPolicy) (integrationVerdict, error) {
	v := integrationVerdict{Reasons: []string{}, Flags: []string{}}
	if len(r.hardStops) > 0 {
		v.Disposition, v.Reasons = integrationRetainedDisposition, reasonOrder(r.hardStops)
		return v, nil
	}
	if r.objErr != nil {
		v.Disposition, v.Reasons = integrationReviewDisposition, []string{"object-dir-unavailable"}
		return v, nil
	}
	bases, err := r.mergeBases(ctx, base, tip)
	if err != nil {
		if err == errIntegrationLimit {
			v.Disposition, v.Reasons = integrationReviewDisposition, []string{"proof-limit"}
			return v, nil
		}
		return v, err
	}
	if len(bases) != 1 {
		v.Disposition, v.Reasons = integrationReviewDisposition, []string{"ambiguous-merge-base"}
		return v, nil
	}
	tipTree, mbTree, baseTree, err := r.trees(ctx, tip, bases[0], base)
	if err != nil {
		if err == errIntegrationLimit {
			v.Disposition, v.Reasons = integrationReviewDisposition, []string{"proof-limit"}
			return v, nil
		}
		return v, err
	}
	var reasons []string
	// Order of FR-007: no-net-change, patch-id (default and verbatim in one
	// pass), merge-noop. Every selected class runs; the strongest deletable
	// class is reported: patch-id, then merge-noop, then no-net-change.
	netNothing := policy.has(integrationNoNetChange) && tipTree == mbTree
	patchProven := false
	if policy.has(integrationPatchID) {
		pr, err := r.patchID(ctx, base, tip, bases[0], tipTree, baseTree)
		if err != nil {
			return v, err
		}
		v.Matched, v.Unmatched, v.Empty, v.SkippedMerges, v.BinaryPaths = pr.Matched, pr.Unmatched, pr.Empty, pr.SkippedMerges, pr.BinaryPaths
		reasons = append(reasons, pr.Reasons...)
		switch {
		case pr.Proven && !pr.Whitespace:
			patchProven = true
		case pr.Proven && pr.Whitespace && policy.acceptsWhitespace():
			patchProven, v.Flags = true, []string{integrationFlagAcceptedWhitespace}
		case pr.Proven && pr.Whitespace:
			reasons = append(reasons, integrationFlagWhitespace)
			v.Flags = []string{integrationFlagWhitespace}
		}
	}
	noopProven := false
	if policy.has(integrationMergeNoop) {
		mr, err := r.mergeNoop(ctx, base, tip, bases[0], tipTree, mbTree, baseTree)
		if err != nil {
			return v, err
		}
		noopProven = mr.Proven
		reasons = append(reasons, mr.Reasons...)
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
	}
	if v.Disposition != integrationProvenDisposition && len(reasons) > 0 {
		v.Disposition, v.Reasons = integrationReviewDisposition, reasonOrder(reasons)
	}
	return v, nil
}
