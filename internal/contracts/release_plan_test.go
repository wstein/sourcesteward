// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/release"
)

func TestReleasePlanResultSchema(t *testing.T) {
	s := schema(t, "release-plan")
	r := release.Plan(context.Background(), release.PlanOptions{Path: t.TempDir(), Subject: strings.Repeat("a", 40), Version: "v1.2.3"})
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(value); err != nil {
		t.Fatal(err)
	}
	result := value.(map[string]any)
	for _, field := range []string{"gates", "would_build", "would_publish", "network", "advisories"} {
		mutant := map[string]any{}
		for k, v := range result {
			mutant[k] = v
		}
		mutant[field] = nil
		if err := s.Validate(mutant); err == nil {
			t.Fatalf("accepted null %s", field)
		}
	}
}

func TestReleasePlanAllowlistMatchSchema(t *testing.T) {
	s := schema(t, "release-plan")
	r := release.Plan(context.Background(), release.PlanOptions{Path: t.TempDir(), Subject: strings.Repeat("a", 40), Version: "v1.2.3"})
	data, _ := json.Marshal(r)
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	many := make([]any, 257)
	for i := range many {
		many[i] = map[string]any{"pattern": "docs/**", "tracked_files": 0}
	}
	for _, tc := range []struct {
		name  string
		rows  any
		valid bool
	}{
		{"null", nil, true},
		{"empty", []any{}, true},
		{"zero", []any{map[string]any{"pattern": "docs/**", "tracked_files": 0}}, true},
		{"negative", []any{map[string]any{"pattern": "docs/**", "tracked_files": -1}}, false},
		{"fraction", []any{map[string]any{"pattern": "docs/**", "tracked_files": 0.5}}, false},
		{"missing count", []any{map[string]any{"pattern": "docs/**"}}, false},
		{"over bound", many, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value["allowlist_matches"] = tc.rows
			err := s.Validate(value)
			if (err == nil) != tc.valid {
				t.Fatalf("validation=%v", err)
			}
		})
	}
	delete(value, "allowlist_matches")
	if err := s.Validate(value); err == nil {
		t.Fatal("missing disclosure accepted")
	}
}
