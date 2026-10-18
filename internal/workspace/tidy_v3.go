// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"context"
	"time"
)

// PlanTidyV3 is the explicit version-three operation boundary. Existing tidy
// entry points and their stored-plan meanings remain separate.
func PlanTidyV3(ctx context.Context, o V3PlanOptions) V3Result {
	timeout := 10 * time.Second
	if o.Online {
		timeout = 30 * time.Second
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
	p, _, _, err := readTidyV3ApplyRecords(o)
	if err != nil {
		return failTidyV3(newV3Result("tidy-apply"), err)
	}
	if p.Mode == "tracking-refs" {
		return defaultTidyV3TrackingCoordinator().apply(ctx, o)
	}
	return defaultTidyV3RefsApplyCoordinator().apply(ctx, o)
}
