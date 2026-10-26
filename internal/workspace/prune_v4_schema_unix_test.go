// SPDX-License-Identifier: EUPL-1.2
//go:build linux || darwin

package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestRegistrationPruneV4SchemaMatchesApplyRecords(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "partial"}[partial], func(t *testing.T) {
			testRegistrationPruneV4SchemaApplyRecords(t, partial)
		})
	}
}

func testRegistrationPruneV4SchemaApplyRecords(t *testing.T, partial bool) {
	_, p := fixturePruneV4(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	if partial {
		ctx = context.WithValue(ctx, pruneObservationHookKey{}, pruneObservationHook(func(stage, path string, _ any) {
			if stage == "v4-before-unlink" {
				calls++
				if calls == 2 {
					cancel()
				}
			}
		}))
	}
	opts := RegistrationPruneV4ApplyOptions{StateDir: p.StateDir, PlanID: p.PlanID, Authorize: true, Mode: p.Mode}
	r := ApplyRegistrationPruneV4(ctx, opts)
	want := "ok"
	if partial {
		want = "partial"
	}
	if r.Status != want {
		t.Fatal(r)
	}
	raw, e := os.ReadFile("../../schemas/v4/registration-prune.schema.json")
	if e != nil {
		t.Fatal(e)
	}
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	compiler := jsonschema.NewCompiler()
	if e = compiler.AddResource("contract4", doc); e != nil {
		t.Fatal(e)
	}
	schema, e := compiler.Compile("contract4")
	if e != nil {
		t.Fatal(e)
	}
	paths := []string{filepath.Join(p.StateDir, pruneV4Namespace, "journals", p.PlanID+".json"), filepath.Join(p.PayloadPath, "manifest.json")}
	encoded, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	documents := [][]byte{encoded}
	replay := ApplyRegistrationPruneV4(context.Background(), opts)
	if replay.Status != want {
		t.Fatalf("replay: %+v", replay)
	}
	b, e := json.Marshal(replay)
	if e != nil {
		t.Fatal(e)
	}
	documents = append(documents, b)
	opts.Mode.Caller = "wrong-owner"
	refused := ApplyRegistrationPruneV4(context.Background(), opts)
	if refused.Status != "refused" || len(refused.Diagnostics) != 1 || refused.Diagnostics[0].Code != "prune-scope-refused" {
		t.Fatalf("post-plan refusal: %+v", refused)
	}
	b, e = json.Marshal(refused)
	if e != nil {
		t.Fatal(e)
	}
	documents = append(documents, b)
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		documents = append(documents, b)
	}
	for i, raw := range documents {
		v, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		if e = schema.Validate(v); e != nil {
			t.Fatalf("record %d: %v", i, e)
		}
	}
}
