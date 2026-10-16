// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package inspection

import "io/fs"

// The ownership check and directory identity are Unix-only (Spec 021, DD3). Elsewhere they report
// "unknown", so every submodule is refused rather than trusted.
const pinsOpenFlags = 0

func pinsEffectiveUID() uint32                 { return 0 }
func pinsFileOwner(fs.FileInfo) (uint32, bool) { return 0, false }
func pinsFileID(fs.FileInfo) (pinsDirID, bool) { return pinsDirID{}, false }

func pinsEffectiveGID() uint32                 { return 0 }
func pinsFileGroup(fs.FileInfo) (uint32, bool) { return 0, false }
