//go:build !darwin && !linux

// SPDX-License-Identifier: EUPL-1.2
package workspace

import "os"

func gitMetadata(path string) ([]byte, error) {
	return nil, failure("git-metadata-refused", "safe Git registration metadata reads require Linux or macOS")
}

func gitLayoutFile(path string) ([]byte, PathIdentity, os.FileMode, error) {
	return nil, PathIdentity{}, 0, failure("git-layout-refused", "safe Git layout metadata reads require Linux or macOS")
}
