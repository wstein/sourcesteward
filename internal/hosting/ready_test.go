// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

func TestReadyPreservesExistingHostingResultAndSelectors(t *testing.T) {
	for _, kind := range []string{"repo", "pr", "ci"} {
		number := 42
		if kind == "repo" {
			number = 0
		}
		if err := ValidateOptions(Options{Kind: kind, Number: number}); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := json.Marshal(Result{})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(result))
	for key := range result {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"backend", "boundary", "candidates", "checks", "diagnostics", "identity", "kind", "observed_at", "pull_request", "repository", "schema_version", "status", "statuses"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("existing hosting result fields changed: %v", keys)
	}
	if err := ValidateOptions(Options{Kind: "ready-check", Number: 42}); err == nil {
		t.Fatal("existing inspection must retain its closed kind set")
	}
}
