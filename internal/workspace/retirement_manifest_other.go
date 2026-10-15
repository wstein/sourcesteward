//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package workspace

import "os"

func retirementSingleLink(os.FileInfo) bool { return false }
