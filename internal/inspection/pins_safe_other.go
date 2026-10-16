// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package inspection

import "io/fs"

// Directory identity is Unix-only (Spec 021, DD3). Elsewhere it reports "unknown".
const pinsOpenFlags = 0

func pinsFileID(fs.FileInfo) (pinsDirID, bool) { return pinsDirID{}, false }
