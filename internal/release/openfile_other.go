// SPDX-License-Identifier: EUPL-1.2
//go:build !unix

package release

import "os"

// openRegular opens a file; without O_NOFOLLOW the caller's Lstat comparison is the guard.
func openRegular(path string) (*os.File, error) { return os.Open(path) }
