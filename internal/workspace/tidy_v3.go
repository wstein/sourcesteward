// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"time"
)

// PlanTidyV3 is the explicit version-three operation boundary. Existing tidy
// entry points and their stored-plan meanings remain separate.
func PlanTidyV3(ctx context.Context, o V3PlanOptions) V3Result {
	timeout := planObservationTimeout
	if o.Online {
		timeout += planOnlineTimeout
	}
	if o.Task == "" && o.CompletedWorktree == "" && o.TrackingRemote == "" {
		timeout = v3ProofPlanTimeout(o) // local-reference plans include the proof phase
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return failTidyV3(newV3Result("tidy-plan"), err)
	}
	if o.Path == "." {
		path, err := canonicalSource(o.Path)
		if err != nil {
			return failTidyV3(newV3Result("tidy-plan"), err)
		}
		o.Path = path
	}
	if o.Task != "" || o.CompletedWorktree != "" {
		return defaultTidyV3RetirementCoordinator().plan(ctx, o)
	}
	if o.TrackingRemote != "" {
		return defaultTidyV3TrackingCoordinator().plan(ctx, o)
	}
	return defaultTidyV3RefsCoordinator().plan(ctx, o)
}

// ApplyTidyV3 selects only an immutable, validated v3 record. Authorization and
// online acknowledgment are checked before the coordinator can open hosting.
func ApplyTidyV3(ctx context.Context, o V3ApplyOptions) V3Result {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return failTidyV3(newV3Result("tidy-apply"), err)
	}
	if !o.Authorize {
		return failTidyV3(newV3Result("tidy-apply"), failure("authorization-required", "Apply requires explicit acknowledgment of exact stored local effects."))
	}
	if !validID(o.PlanID) || !v3CanonicalPath(o.StateDir) {
		return failTidyV3(newV3Result("tidy-apply"), failure("invalid-options", "An exact stored plan identity and canonical private state directory are required."))
	}
	p, err := readTidyV3DispatchPlan(o)
	if err != nil {
		return failTidyV3(newV3Result("tidy-apply"), err)
	}
	if p.Mode == "owned-retirement" || p.Mode == "legacy-retirement" {
		return defaultTidyV3RetirementCoordinator().apply(ctx, o)
	}
	if p.Mode == "tracking-refs" {
		return defaultTidyV3TrackingCoordinator().apply(ctx, o)
	}
	return defaultTidyV3RefsApplyCoordinator().apply(ctx, o)
}

// Read only the immutable union to select a coordinator. The original caller
// protections and acknowledgments are validated before any session or writer;
// each coordinator then independently reopens and revalidates its own records.
func readTidyV3DispatchPlan(o V3ApplyOptions) (V3Plan, error) {
	raw, err := openStoreKinds(o.StateDir, false, []string{"tidy-v3-plans", "tidy-v3-operations"})
	if err != nil {
		return V3Plan{}, err
	}
	defer raw.Close()
	s := &v3Store{store: raw, protected: append([]string{}, o.ProtectWorktrees...)}
	p, _, err := s.readPlan(o.PlanID)
	if err != nil {
		return p, err
	}
	if err = validateV3ApplyOptions(o, p); err != nil {
		return p, err
	}
	return p, nil
}
