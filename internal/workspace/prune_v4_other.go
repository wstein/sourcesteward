// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package workspace

import "context"

func PlanRegistrationPruneV4(_ context.Context, _ RegistrationPruneV4Options) RegistrationPruneV4Result {
	return failPruneV4(pruneV4Result(), failure("unsupported-backend", "Registration prune requires native Linux or macOS mount observations."))
}

func ApplyRegistrationPruneV4(_ context.Context, _ RegistrationPruneV4ApplyOptions) RegistrationPruneV4Result {
	r := pruneV4Result()
	r.Command = "tidy-apply"
	return failPruneV4(r, failure("unsupported-backend", "Registration prune requires native Linux or macOS mount observations."))
}
