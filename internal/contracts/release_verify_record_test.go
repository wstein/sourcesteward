// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"context"
	"encoding/json"
	"github.com/wstein/sourcesteward/internal/release"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReleaseVerifyAndRecordResultSchemas(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result any
	}{{"release-verify", release.Verify(context.Background(), release.VerifyOptions{PlanOptions: release.PlanOptions{Path: t.TempDir(), Subject: "v1.0.0"}})}, {"release-record-result", release.Record(context.Background(), release.RecordOptions{Path: t.TempDir()})}} {
		t.Run(tc.name, func(t *testing.T) {
			s := schema(t, tc.name)
			raw, e := json.Marshal(tc.result)
			if e != nil {
				t.Fatal(e)
			}
			var value map[string]any
			if e = json.Unmarshal(raw, &value); e != nil {
				t.Fatal(e)
			}
			if e = s.Validate(value); e != nil {
				t.Fatal(e)
			}
			for _, field := range []string{"status", "error", "command"} {
				mutant := map[string]any{}
				for k, v := range value {
					mutant[k] = v
				}
				delete(mutant, field)
				if s.Validate(mutant) == nil {
					t.Fatalf("missing %s accepted", field)
				}
			}
		})
	}
}

func TestReleaseRecordResultEmbedsExactRecordContract(t *testing.T) {
	raw, e := os.ReadFile(filepath.Join(root(t), "schemas/v1/release-record.schema.json"))
	if e != nil {
		t.Fatal(e)
	}
	var record map[string]any
	json.Unmarshal(raw, &record)
	raw, e = os.ReadFile(filepath.Join(root(t), "schemas/v1/release-record-result.schema.json"))
	if e != nil {
		t.Fatal(e)
	}
	var result map[string]any
	json.Unmarshal(raw, &result)
	defs := record["$defs"]
	delete(record, "$defs")
	delete(record, "$schema")
	delete(record, "$id")
	properties := result["properties"].(map[string]any)
	embedded := properties["record"].(map[string]any)["anyOf"].([]any)[1]
	if !reflect.DeepEqual(defs, result["$defs"]) || !reflect.DeepEqual(record, embedded) {
		t.Fatal("record result wrapper drifted from the strict record contract")
	}
	sample, e := os.ReadFile(filepath.Join(root(t), "specs/026-release-process/examples/go-cli.record.json"))
	if e != nil {
		t.Fatal(e)
	}
	doc, ff := release.ParseReleaseDocument(sample)
	if len(ff) > 0 {
		t.Fatal(ff)
	}
	value := release.RecordResult{SchemaVersion: 1, Command: "release-record", Status: "ok", Record: doc.(*release.ReleaseRecord), Path: "docs/releases/v0.2.0.json", CanonicalSHA256: strings.Repeat("a", 64), Backend: "git"}
	data, _ := json.Marshal(value)
	var model any
	json.Unmarshal(data, &model)
	if e = schema(t, "release-record-result").Validate(model); e != nil {
		t.Fatal(e)
	}
}
