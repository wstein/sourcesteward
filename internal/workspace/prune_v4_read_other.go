// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package workspace

func pruneV4ReadPlanBytes(_ *store, _ string) ([]byte, error) {
	return nil, failure("unsupported-backend", "Contract-4 private-store reading requires Linux or macOS.")
}
