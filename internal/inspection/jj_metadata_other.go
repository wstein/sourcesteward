// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package inspection

import (
	"errors"
	"os"
)

func openMetadata(string) (*os.File, error) {
	return nil, errors.New("native jj metadata inspection is unsupported on this host")
}
func openMetadataDirectory(string) (*os.File, error) {
	return nil, errors.New("native jj metadata inspection is unsupported on this host")
}
