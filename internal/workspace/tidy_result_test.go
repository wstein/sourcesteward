// SPDX-License-Identifier: EUPL-1.2
package workspace

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTidyResultEnvelopeArraysAndIndependentPlan(t *testing.T) {
	r := newTidyResult("tidy-plan")
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"schema_version":1`, `"command":"tidy-plan"`, `"plan":null`, `"actions":[]`, `"diagnostics":[]`, `"limitations":[]`} {
		if !strings.Contains(string(b), field) {
			t.Fatalf("missing %s in %s", field, b)
		}
	}
	if strings.Contains(string(b), `"task"`) {
		t.Fatal("tidy leaked legacy workspace task contract")
	}
}
