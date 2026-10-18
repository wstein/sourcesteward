// SPDX-License-Identifier: EUPL-1.2
//go:build !linux && !darwin

package release

import "errors"

var errOutputExists = errors.New("output-exists")

func writeRecordExclusive(root, path string, data []byte) (bool, error) {
	return false, errRecordWriteUnsupported
}
