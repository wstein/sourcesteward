// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package inspection

import "os"

// changeStamp is unavailable here: only size, modification time and identity are compared.
func changeStamp(info os.FileInfo) (int64, bool) { return 0, false }
