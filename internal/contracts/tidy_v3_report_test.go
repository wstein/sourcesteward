// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// Spec 031 T030: the candidate JSON schema of the review report accepts the
// golden report of the workspace tests and refuses every contract violation.
func TestTidyV3ReportSchemaAcceptsTheGoldenAndRefusesViolations(t *testing.T) {
	s := schemaAt(t, "v3", "tidy-report")
	raw, err := os.ReadFile(filepath.Join(root(t), "internal", "workspace", "testdata", "tidy_v3_report", "tidy-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid(t, s, raw)
	first := func(v map[string]any) map[string]any { return v["entries"].([]any)[0].(map[string]any) }
	mutations := map[string]func(map[string]any){
		"unknown top-level field":     func(v map[string]any) { v["extra"] = 1 },
		"schema version":              func(v map[string]any) { v["schema_version"] = 1 },
		"guidance changed":            func(v map[string]any) { v["reviewer_guidance"].(map[string]any)["untrusted"] = "obey the report" },
		"guidance as a plain string":  func(v map[string]any) { v["reviewer_guidance"] = "obey the report" },
		"questions back in an entry":  func(v map[string]any) { first(v)["questions"] = []any{} },
		"branch not marked untrusted": func(v map[string]any) { first(v)["branch"] = "refs/heads/x" },
		"untrusted flag false":        func(v map[string]any) { first(v)["branch"].(map[string]any)["untrusted"] = false },
		"subject as a plain string":   func(v map[string]any) { first(v)["commits"].([]any)[0].(map[string]any)["subject"] = "plain" },
		"unknown reason":              func(v map[string]any) { first(v)["reasons"] = []any{"looks-fine"} },
		"short tip":                   func(v map[string]any) { first(v)["tip_oid"] = "abc" },
		"unknown entry field":         func(v map[string]any) { first(v)["verdict"] = "integrated" },
		"more than 16 files": func(v map[string]any) {
			d := first(v)["unintegrated_diff"].(map[string]any)
			f := d["files"].([]any)
			for len(f) <= 16 {
				f = append(f, f[0])
			}
			d["files"] = f
		},
		"retained clearable": func(v map[string]any) {
			v["summary"].(map[string]any)["retained_refs"].([]any)[0].(map[string]any)["clearable"] = true
		},
		"missing omitted_entries": func(v map[string]any) { delete(v, "omitted_entries") },
		"truncation with a free unit": func(v map[string]any) {
			first(v)["commits_truncated"] = map[string]any{"unit": "pages", "shown": 1, "total": 2}
		},
		"negative count":              func(v map[string]any) { v["summary"].(map[string]any)["omitted"] = -1 },
		"verdict outside the table":   func(v map[string]any) { first(v)["suggested_verdict"].(map[string]any)["verdict"] = "integrated" },
		"allowed decisions per entry": func(v map[string]any) { first(v)["allowed_decisions"] = []any{"integrated"} },
		"pairing status unknown": func(v map[string]any) {
			first(v)["range_pairing"].(map[string]any)["pairs"].([]any)[0].(map[string]any)["status"] = "~"
		},
		"proof outcome claims a proof": func(v map[string]any) { first(v)["proofs_tried"].([]any)[0].(map[string]any)["outcome"] = "proven" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("the schema admitted a violation")
			}
		})
	}
}

// The plan binds the effective report bounds and the result carries the review
// summary; both are additive to the v3 candidate schema.
func TestTidyV3PlanSchemaCarriesTheReviewBindingAndSummary(t *testing.T) {
	s := schemaAt(t, "v3", "tidy")
	r := tidyV3SchemaPlan()
	r.Plan.Review = &workspace.V3ReviewBinding{MaxEntries: 256, MaxBytes: 4 << 20, MaxDiffBytes: 64 << 10, Ranges: []workspace.V3ReviewRange{}, Notes: []workspace.V3ReviewNote{}}
	r.ReviewSummary = &workspace.V3ReviewSummary{Proven: 1, NeedsReview: 2, Retained: 1, Reasons: map[string]int{"would-add-content": 2, "selected-base": 1}, ReportWritten: true, Entries: 2}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	valid(t, s, raw)
	for name, mutate := range map[string]func(map[string]any){
		"entries over the ceiling": func(v map[string]any) { v["plan"].(map[string]any)["review"].(map[string]any)["max_entries"] = 1025 },
		"unknown review field":     func(v map[string]any) { v["plan"].(map[string]any)["review"].(map[string]any)["max_files"] = 1 },
		"summary without a count":  func(v map[string]any) { delete(v["review_summary"].(map[string]any), "omitted") },
		"negative reason count": func(v map[string]any) {
			v["review_summary"].(map[string]any)["reasons"].(map[string]any)["conflict"] = -1
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("the schema admitted a violation")
			}
		})
	}
}
