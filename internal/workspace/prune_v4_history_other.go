// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package workspace

import "context"

func HistoryRegistrationPruneV4(_ context.Context, _ RegistrationPruneV4HistoryOptions) RegistrationPruneV4HistoryResult {
	return failPruneV4History(newPruneV4HistoryResult(), failure("unsupported-backend", "Registration prune history requires the supported Linux or macOS private-store reader."))
}
