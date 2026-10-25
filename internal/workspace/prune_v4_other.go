// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package workspace

import "context"

func PlanRegistrationPruneV4(_ context.Context, _ RegistrationPruneV4Options) RegistrationPruneV4Result {
	return failPruneV4(pruneV4Result(), failure("unsupported-backend", "Registration prune requires native Linux or macOS mount observations."))
}
