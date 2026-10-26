//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package workspace

import "context"

func RestoreRegistrationPruneV4(_ context.Context, _ RegistrationPruneV4RestoreOptions) RegistrationPruneV4Result {
	r := pruneV4Result()
	r.Command = "tidy-restore"
	r.Operation = "registration-restore"
	return failPruneV4(r, failure("unsupported-backend", "Administrative registration restore requires native Linux or macOS no-replace publication."))
}
