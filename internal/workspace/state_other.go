//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package workspace

import "os"

const noFollow = 0

func pathIdentity(string, os.FileInfo) (PathIdentity, error) {
	return PathIdentity{}, failure("unsupported-platform", "Workspace mutations require Linux or macOS.")
}
func privateOwner(os.FileInfo) bool   { return false }
func regularPrivate(os.FileInfo) bool { return false }
func lockFile(*os.File) error {
	return failure("unsupported-platform", "Native store locking is unavailable.")
}
