//go:build !darwin && !linux

// SPDX-License-Identifier: EUPL-1.2
package workspace

import "os"

func gitTidyMutableFile(os.FileInfo) bool { return false }
