//go:build !linux && !darwin

// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"errors"
	"os"
)

func retirementSingleLink(os.FileInfo) bool { return false }

func retirementV3Physical(string, os.FileInfo) (V3PhysicalIdentity, error) {
	return V3PhysicalIdentity{}, errors.New("v3 physical owner/time capability unavailable")
}
