// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package inspection

import "os"

// osChangeStamp is unavailable here: only size, modification time and identity are compared.
func osChangeStamp(info os.FileInfo) (int64, bool) { return 0, false }
