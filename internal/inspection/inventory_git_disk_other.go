// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package inspection

import (
	"errors"
	"os"
)

func openGitInventoryDisk(*os.Root, string) (*os.File, error) {
	return nil, errors.New("safe native Git disk observation requires Linux or macOS")
}
