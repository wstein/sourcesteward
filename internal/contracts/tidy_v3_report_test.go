// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		"more than the 4,096 file ceiling": func(v map[string]any) {
			d := first(v)["unintegrated_diff"].(map[string]any)
			f := d["files"].([]any)
			for len(f) <= 4096 {
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
		"entries over the ceiling": func(v map[string]any) { v["plan"].(map[string]any)["review"].(map[string]any)["max_entries"] = 4097 },
		"files over the ceiling":   func(v map[string]any) { v["plan"].(map[string]any)["review"].(map[string]any)["max_files"] = 4097 },
		"unknown review field":     func(v map[string]any) { v["plan"].(map[string]any)["review"].(map[string]any)["max_widgets"] = 1 },
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

// A proof-limit entry names the bound and says why its diff is missing.
func TestTidyV3ReportSchemaDescribesAProofLimitEntry(t *testing.T) {
	s := schemaAt(t, "v3", "tidy-report")
	raw, err := os.ReadFile(filepath.Join(root(t), "internal", "workspace", "testdata", "tidy_v3_report", "tidy-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	entry := func(v map[string]any) map[string]any { return v["entries"].([]any)[0].(map[string]any) }
	with := func(mutate func(map[string]any)) map[string]any {
		v := value(t, raw).(map[string]any)
		e := entry(v)
		e["proof_limit"] = map[string]any{"name": "diff-tree --raw", "unit": "records", "limit": 8192, "observed": 8193}
		e["unintegrated_diff"].(map[string]any)["source"] = "unavailable"
		e["unintegrated_diff"].(map[string]any)["unavailable_reason"] = "proof-limit: diff-tree --raw hit its records bound"
		mutate(v)
		return v
	}
	if err := s.Validate(with(func(map[string]any) {})); err != nil {
		t.Fatalf("a proof-limit entry was refused: %v", err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"unit outside the table": func(v map[string]any) { entry(v)["proof_limit"].(map[string]any)["unit"] = "pages" },
		"missing observed":       func(v map[string]any) { delete(entry(v)["proof_limit"].(map[string]any), "observed") },
		"negative limit":         func(v map[string]any) { entry(v)["proof_limit"].(map[string]any)["limit"] = -1 },
		"empty reason":           func(v map[string]any) { entry(v)["unintegrated_diff"].(map[string]any)["unavailable_reason"] = "" },
	} {
		t.Run(name, func(t *testing.T) {
			if s.Validate(with(mutate)) == nil {
				t.Fatal("the schema admitted a violation")
			}
		})
	}
}

// A commit says whether the base carries a commit with the same patch.
func TestTidyV3ReportSchemaMarksMatchedAndUnmatchedCommits(t *testing.T) {
	s := schemaAt(t, "v3", "tidy-report")
	raw, err := os.ReadFile(filepath.Join(root(t), "internal", "workspace", "testdata", "tidy_v3_report", "tidy-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	commit := func(v map[string]any) map[string]any {
		return v["entries"].([]any)[0].(map[string]any)["commits"].([]any)[0].(map[string]any)
	}
	for name, mutate := range map[string]func(map[string]any){
		"status outside the table": func(v map[string]any) { commit(v)["patch_match"] = "similar" },
		"matched_by without a status": func(v map[string]any) {
			delete(commit(v), "patch_match")
			commit(v)["matched_by"] = strings.Repeat("a", 40)
		},
		"matched_by not an OID": func(v map[string]any) { commit(v)["matched_by"] = "abc" },
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

// The gitlink-change reason names the path and the side that changed it.
func TestTidyV3ReportSchemaNamesGitlinkChanges(t *testing.T) {
	s := schemaAt(t, "v3", "tidy-report")
	raw, err := os.ReadFile(filepath.Join(root(t), "internal", "workspace", "testdata", "tidy_v3_report", "tidy-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	with := func(mutate func(map[string]any)) map[string]any {
		v := value(t, raw).(map[string]any)
		e := v["entries"].([]any)[0].(map[string]any)
		e["gitlink_changes"] = []any{map[string]any{"side": "base", "path": map[string]any{"untrusted": true, "text": "libs/sub"}, "old_oid": strings.Repeat("a", 40), "new_oid": ""}}
		mutate(e)
		return v
	}
	if err := s.Validate(with(func(map[string]any) {})); err != nil {
		t.Fatalf("a gitlink entry was refused: %v", err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"side outside the table": func(e map[string]any) { e["gitlink_changes"].([]any)[0].(map[string]any)["side"] = "tip" },
		"path not untrusted":     func(e map[string]any) { e["gitlink_changes"].([]any)[0].(map[string]any)["path"] = "libs/sub" },
		"more than the 4,096 ceiling": func(e map[string]any) {
			g := e["gitlink_changes"].([]any)
			for len(g) <= 4096 {
				g = append(g, g[0])
			}
			e["gitlink_changes"] = g
		},
	} {
		t.Run(name, func(t *testing.T) {
			if s.Validate(with(mutate)) == nil {
				t.Fatal("the schema admitted a violation")
			}
		})
	}
}
