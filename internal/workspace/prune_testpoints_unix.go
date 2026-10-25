// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import "context"

// Per-observation injection points are private and absent from normal callers.
// They introduce no global mutable state or CLI/stored-plan surface.
type pruneObservationHookKey struct{}
type pruneObservationHook func(stage, path string, value any)

func pruneObservationStep(ctx context.Context, stage, path string, value any) {
	if hook, ok := ctx.Value(pruneObservationHookKey{}).(pruneObservationHook); ok {
		hook(stage, path, value)
	}
}
