// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRetirementEnvelopeKeepsV1BytesAndV2Distinct(t *testing.T) {
	old := newTidyResult("tidy-plan")
	b, _ := json.Marshal(old)
	if strings.Contains(string(b), "retirement") {
		t.Fatal("v1 contract changed")
	}
	old.SchemaVersion = 2
	old.Retirement = &RetirementPlanRecord{SchemaVersion: 2}
	b, _ = json.Marshal(old)
	if !strings.Contains(string(b), `"retirement":`) || !strings.Contains(string(b), `"plan":null`) {
		t.Fatal("retirement envelope missing")
	}
}
