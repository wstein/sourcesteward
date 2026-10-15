//go:build !darwin && !linux

// SPDX-License-Identifier: EUPL-1.2
package workspace

func gitMetadata(path string) ([]byte, error) {
	return nil, failure("git-metadata-refused", "safe Git registration metadata reads require Linux or macOS")
}
