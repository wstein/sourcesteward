// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package workspace

import "context"

func ObserveRegistrationPruneV4(_ context.Context, _ RegistrationPruneV4Options) RegistrationPruneV4Observation {
	return refusedPruneV4Overview(failure("unsupported-backend", "Registration prune requires native Linux or macOS mount observations."))
}
func ObserveRegistrationPruneV4Inventory(_ context.Context, _ string) RegistrationPruneV4Inventory {
	return RegistrationPruneV4Inventory{Status: "refused", Candidates: []RegistrationPruneV4Candidate{}, Diagnostic: Diagnostic{Code: "unsupported-backend", Message: "Registration prune requires native Linux or macOS mount observations."}}
}
