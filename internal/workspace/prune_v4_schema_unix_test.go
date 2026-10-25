// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRegistrationPruneV4SchemaMatchesNativePlan(t *testing.T) {
	_, plan := fixturePruneV4(t)
	raw, err := os.ReadFile("../../schemas/v4/registration-prune.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const url = "https://wstein.github.io/sourcesteward/schemas/v4/registration-prune.schema.json"
	if err = compiler.AddResource(url, doc); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	result := pruneV4Result()
	result.Status, result.Backend, result.Plan = "ok", "git", &plan
	for _, candidate := range []any{plan, result, pruneV4Result()} {
		b, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		value, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		if err = schema.Validate(value); err != nil {
			t.Fatal(err)
		}
	}
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "operation", "plan_id", "digest", "source_path", "state_dir", "backend", "mode", "authorized_scopes", "registration", "expected", "payload_path", "effects", "recovery_limits"} {
		t.Run("required-"+key, func(t *testing.T) {
			v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			delete(v.(map[string]any), key)
			if schema.Validate(v) == nil {
				t.Fatal("schema admitted missing required field", key)
			}
		})
	}
	for _, kind := range []string{"old-version", "unknown-field", "missing-mount", "missing-false", "null-metadata"} {
		t.Run(kind, func(t *testing.T) {
			v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			m := v.(map[string]any)
			switch kind {
			case "old-version":
				m["schema_version"] = 3
			case "unknown-field":
				m["extra"] = true
			case "missing-mount":
				delete(m["expected"].(map[string]any)["common_dir_mount"].(map[string]any), "mount")
			case "missing-false":
				delete(m["mode"].(map[string]any), "stdin_tty")
			case "null-metadata":
				m["expected"].(map[string]any)["metadata"] = nil
			}
			if schema.Validate(v) == nil {
				t.Fatal("schema admitted incompatible plan", kind)
			}
		})
	}
}
