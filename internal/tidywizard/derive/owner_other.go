//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package derive

import "os"

// The ownership of a directory cannot be established on this platform, so no store is accepted.
func ownedByCurrentUser(os.FileInfo) bool { return false }
