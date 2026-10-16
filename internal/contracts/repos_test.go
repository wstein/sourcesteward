// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func reposFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root(t), "internal/cli/testdata/repos-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func reposObject(t *testing.T, name string) map[string]any {
	t.Helper()
	return value(t, reposFixture(t, name)).(map[string]any)
}

func TestReposSchemaFixtures(t *testing.T) {
	s := schema(t, "repos")
	for name, wantStatus := range map[string]string{
		"ok":                "ok",
		"partial":           "partial",
		"error":             "error",
		"skipped":           "ok",
		"omitted-inventory": "partial",
		"duplicate":         "partial",
	} {
		raw := reposFixture(t, name)
		valid(t, s, raw)
		obj := reposObject(t, name)
		if got := obj["status"]; got != wantStatus {
			t.Fatalf("%s fixture has status %v, want %s", name, got, wantStatus)
		}
	}
}

func TestReposContractExampleValidates(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root(t), "specs/023-multi-repo-inventory/contracts/repos.md"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindSubmatch(raw)
	if match == nil {
		t.Fatal("contract has no JSON example")
	}
	valid(t, schema(t, "repos"), match[1])
}

func TestReposEmbeddedInventoriesValidate(t *testing.T) {
	invSchema := schema(t, "inventory")
	for _, name := range []string{"ok", "partial", "error", "skipped", "omitted-inventory", "duplicate"} {
		obj := reposObject(t, name)
		entries, ok := obj["entries"].([]any)
		if !ok {
			t.Fatalf("%s: missing entries array", name)
		}
		for i, entryRaw := range entries {
			entry := entryRaw.(map[string]any)
			inv, hasInv := entry["inventory"]
			if hasInv && inv != nil {
				invBytes, err := json.Marshal(inv)
				if err != nil {
					t.Fatalf("%s entry %d: marshal inventory: %v", name, i, err)
				}
				valid(t, invSchema, invBytes)
			}
		}
	}
}

func TestReposDataModelInvariants(t *testing.T) {
	s := schema(t, "repos")
	base := reposObject(t, "ok")

	mutate := func(fn func(map[string]any)) []byte {
		raw, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		var copy map[string]any
		if err := json.Unmarshal(raw, &copy); err != nil {
			t.Fatal(err)
		}
		fn(copy)
		out, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	invalid := func(name string, raw []byte) {
		t.Helper()
		if err := s.Validate(value(t, raw)); err == nil {
			t.Fatalf("expected invalid for %s, but schema passed", name)
		}
	}

	// Null arrays must be rejected
	invalid("null input_paths", mutate(func(m map[string]any) { m["input_paths"] = nil }))
	invalid("null entries", mutate(func(m map[string]any) { m["entries"] = nil }))
	invalid("null diagnostics", mutate(func(m map[string]any) { m["diagnostics"] = nil }))
	invalid("null boundary limitations", mutate(func(m map[string]any) {
		b := m["boundary"].(map[string]any)
		b["limitations"] = nil
	}))
	invalid("null entry notes", mutate(func(m map[string]any) {
		entries := m["entries"].([]any)
		e0 := entries[0].(map[string]any)
		e0["notes"] = nil
	}))

	// Status ok with non-null error must be rejected
	invalid("ok status with error", mutate(func(m map[string]any) {
		entries := m["entries"].([]any)
		e0 := entries[0].(map[string]any)
		e0["error"] = map[string]any{"code": "something", "message": "bad"}
	}))

	// Status error with null error must be rejected
	invalid("error status with null error", mutate(func(m map[string]any) {
		entries := m["entries"].([]any)
		e0 := entries[0].(map[string]any)
		e0["status"] = "error"
		e0["error"] = nil
	}))

	// Unknown entry status must be rejected
	invalid("unknown entry status", mutate(func(m map[string]any) {
		entries := m["entries"].([]any)
		e0 := entries[0].(map[string]any)
		e0["status"] = "bogus"
	}))

	// Unknown backend must be rejected
	invalid("unknown backend", mutate(func(m map[string]any) {
		entries := m["entries"].([]any)
		e0 := entries[0].(map[string]any)
		e0["backend"] = "mercurial"
	}))
}
