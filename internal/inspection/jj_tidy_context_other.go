// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package inspection

import "os"

func jjTidySingleLink(os.FileInfo) bool { return false }
