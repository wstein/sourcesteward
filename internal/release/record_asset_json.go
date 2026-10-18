// SPDX-License-Identifier: EUPL-1.2
package release

import (
	"encoding/json"
	"fmt"
)

// MarshalJSON preserves the closed file/image union in command result envelopes.
// Disk records still use the hand-written canonical document encoder.
func (a RecordAsset) MarshalJSON() ([]byte, error) {
	if (a.File == nil) == (a.Image == nil) {
		return nil, fmt.Errorf("asset needs exactly one kind")
	}
	if a.File != nil {
		return json.Marshal(a.File)
	}
	return json.Marshal(a.Image)
}
