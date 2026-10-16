// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package inspection

import "io/fs"

// Without stat data every tracked entry is unknown, like the ownership check (pins_safe_other.go).
func pinsStatOf(fs.FileInfo) (pinsStatData, bool) { return pinsStatData{}, false }
