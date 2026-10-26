// SPDX-License-Identifier: EUPL-1.2
package tidywizard

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestRegistrationPruneRowFields(t *testing.T) {
	b, err := json.Marshal(RegistrationPrune{Effects: []string{}, Limitations: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for name := range fields {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{"destination", "effects", "evidence_digest", "gitdir", "id", "kind", "limitations", "name", "owner", "owner_source", "proof", "reason", "repo", "scope", "task", "verdict"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("row fields = %v", got)
	}
	if string(fields["effects"]) != "[]" || string(fields["limitations"]) != "[]" {
		t.Fatal("present arrays must not be null")
	}
}
