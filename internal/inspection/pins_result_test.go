// SPDX-License-Identifier: EUPL-1.2
package inspection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// Independent copy of the normative finding table in specs/021-submodule-pin-audit/spec.md.
var wantPinsFindings = map[string]string{
	"pin-unpublished": "error", "gitlink-without-entry": "error", "url-has-credentials": "error",
	"submodule-path-unsafe": "error", "submodule-unsafe-owner": "error", "pins-limit": "error",
	"pins-state-changed": "error", "pin-object-absent": "error",
	"submodule-unsupported-backend": "warning", "staged-pin-differs": "warning", "checkout-differs-from-pin": "warning",
	"submodule-dirty": "warning", "url-mismatch": "warning", "entry-without-gitlink": "warning", "gitmodules-modified": "warning",
	"submodule-uninitialized": "info", "pin-behind-tracking": "info", "nested-submodules-present": "info", "remote-refs-may-be-stale": "info",
}

func TestPinsFindingTableMatchesSpec(t *testing.T) {
	codes := PinsFindingCodes()
	if len(codes) != 19 {
		t.Fatalf("codes %v", codes)
	}
	for _, code := range codes {
		got, ok := PinsFindingSeverity(code)
		if !ok || got != wantPinsFindings[code] {
			t.Fatalf("%s: %q %t", code, got, ok)
		}
	}
	if _, ok := PinsFindingSeverity("future-code"); ok {
		t.Fatal("unknown code has a severity")
	}
	// The returned slice is a copy: callers cannot corrupt the table.
	codes[0] = "changed"
	if PinsFindingCodes()[0] == "changed" {
		t.Fatal("table exposed")
	}
}
func TestPinsFindingTableMatchesSchema(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "v1", "pins.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs struct {
			Submodule struct {
				Properties struct {
					FindingCodes struct {
						Items struct{ Enum []string }
					} `json:"finding_codes"`
				}
			}
			Finding struct {
				OneOf []struct {
					Properties struct {
						Code     struct{ Enum []string }
						Severity struct{ Const string }
					}
				}
			}
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, alternative := range doc.Defs.Finding.OneOf {
		for _, code := range alternative.Properties.Code.Enum {
			got[code] = alternative.Properties.Severity.Const
		}
	}
	want := map[string]string{}
	for _, code := range PinsFindingCodes() {
		want[code], _ = PinsFindingSeverity(code)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema findings differ from the Go table: %v", got)
	}
	listed := append([]string(nil), doc.Defs.Submodule.Properties.FindingCodes.Items.Enum...)
	all := PinsFindingCodes()
	sort.Strings(listed)
	sort.Strings(all)
	if !reflect.DeepEqual(listed, all) {
		t.Fatalf("finding_codes enum differs from the Go table: %v", listed)
	}
}
func TestValidatePinsOptions(t *testing.T) {
	for _, path := range []string{"", ".", "repo", "with space\nnewline"} {
		if err := ValidatePinsOptions(PinsOptions{Path: path}); err != nil {
			t.Fatalf("%q: %v", path, err)
		}
	}
	for name, path := range map[string]string{"nul": "a\x00b", "utf8": "\xff", "long": strings.Repeat("a", outputLimit+1)} {
		if err := ValidatePinsOptions(PinsOptions{Path: path}); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
func TestNewPinsResultHasNoNullArraysAndFixedBoundary(t *testing.T) {
	r := NewPinsResult("repo")
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"submodules", "findings"} {
		if a, ok := v[key].([]any); !ok || len(a) != 0 {
			t.Fatalf("%s = %v", key, v[key])
		}
	}
	b := v["boundary"].(map[string]any)
	if !reflect.DeepEqual(b["effects"], map[string]any{"network": false, "fetch": false, "writes": false, "hooks": false}) {
		t.Fatalf("effects %v", b["effects"])
	}
	wantLimits := map[string]any{"max_submodules": 256.0, "per_submodule_seconds": 5.0, "total_seconds": 30.0, "max_output_bytes": 1048576.0, "max_remote_refs": 16.0, "max_count": 10000.0, "dirty_scan_seconds": 3.0, "dirty_scan_entries": 20000.0}
	if !reflect.DeepEqual(b["limits"], wantLimits) || !strings.Contains(b["staleness"].(string), "no fetch was made") {
		t.Fatalf("boundary %v", b)
	}
	if v["schema_version"] != 1.0 || v["command"] != "pins" || v["identity"] != nil || v["error"] != nil || v["backend"] != "unknown" {
		t.Fatalf("%v", v)
	}
}
func TestPinsFinalizeOrdersDeterministicallyAndSummarizes(t *testing.T) {
	r := NewPinsResult(".")
	r.Status, r.Backend = "ok", "git"
	r.Submodules = []PinsSubmodule{{Path: "b"}, {Path: "a/z"}, {Path: "a"}, {Path: "B"}}
	r.Submodules[0].Publication.Status = "unreachable"
	r.Submodules[1].Publication.Status = "reachable"
	r.Submodules[2].Publication.Status = "unknown"
	r.Submodules[3].Publication.Status = "reachable"
	mustAdd := func(code string, sub *string) {
		t.Helper()
		if err := r.AddFinding(code, sub, "message"); err != nil {
			t.Fatal(err)
		}
	}
	mustAdd("remote-refs-may-be-stale", nil)
	mustAdd("submodule-dirty", ptr("a"))
	mustAdd("pin-unpublished", ptr("b"))
	mustAdd("pin-unpublished", ptr("a"))
	mustAdd("gitlink-without-entry", ptr("a"))
	mustAdd("submodule-uninitialized", ptr("a"))
	mustAdd("url-mismatch", ptr("a"))
	r.Finalize()
	paths := []string{}
	for _, s := range r.Submodules {
		paths = append(paths, s.Path)
	}
	if !reflect.DeepEqual(paths, []string{"B", "a", "a/z", "b"}) {
		t.Fatalf("submodule order %v", paths)
	}
	type key struct{ code, sub string }
	got := []key{}
	for _, f := range r.Findings {
		sub := "-"
		if f.Submodule != nil {
			sub = *f.Submodule
		}
		got = append(got, key{f.Code, sub})
	}
	// Severity first (error, warning, info), then submodule path with global findings first, then code.
	want := []key{{"gitlink-without-entry", "a"}, {"pin-unpublished", "a"}, {"pin-unpublished", "b"}, {"submodule-dirty", "a"}, {"url-mismatch", "a"}, {"remote-refs-may-be-stale", "-"}, {"submodule-uninitialized", "a"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("finding order\n got %v\nwant %v", got, want)
	}
	if !reflect.DeepEqual(r.Submodules[1].FindingCodes, []string{"gitlink-without-entry", "pin-unpublished", "submodule-dirty", "submodule-uninitialized", "url-mismatch"}) || len(r.Submodules[0].FindingCodes) != 0 || r.Submodules[0].FindingCodes == nil {
		t.Fatalf("finding codes %#v %#v", r.Submodules[1].FindingCodes, r.Submodules[0].FindingCodes)
	}
	s := r.Summary
	if s.Submodules != 4 || s.Errors != 3 || s.Warnings != 2 || s.Infos != 2 || s.Publication.Reachable != 2 || s.Publication.Unreachable != 1 || s.Publication.Unknown != 1 {
		t.Fatalf("%+v", s)
	}
	if r.Status != "ok" || !r.Boundary.Complete {
		t.Fatalf("%s %t", r.Status, r.Boundary.Complete)
	}
	// Finalize is idempotent.
	before, _ := json.Marshal(r)
	r.Finalize()
	if after, _ := json.Marshal(r); string(before) != string(after) {
		t.Fatal("not idempotent")
	}
}
func TestPinsCompletenessRules(t *testing.T) {
	for _, code := range []string{"pins-limit", "pins-state-changed"} {
		r := NewPinsResult(".")
		r.Status, r.Backend = "ok", "git"
		if err := r.AddFinding(code, nil, "bound"); err != nil {
			t.Fatal(err)
		}
		r.Finalize()
		if r.Status != "incomplete" || r.Boundary.Complete {
			t.Fatalf("%s: %s %t", code, r.Status, r.Boundary.Complete)
		}
	}
	// Refusals, unknown observations and absent objects are valid observations and keep a result complete.
	r := NewPinsResult(".")
	r.Status, r.Backend = "incomplete", "git"
	r.Submodules = []PinsSubmodule{{Path: "x", Refusal: &PinsRefusal{Code: "submodule-path-unsafe", Message: "m"}}}
	for _, code := range []string{"submodule-path-unsafe", "pin-object-absent", "submodule-uninitialized", "submodule-unsafe-owner"} {
		if err := r.AddFinding(code, ptr("x"), "m"); err != nil {
			t.Fatal(err)
		}
	}
	r.Finalize()
	if r.Status != "ok" || !r.Boundary.Complete {
		t.Fatalf("%s %t", r.Status, r.Boundary.Complete)
	}
	for _, status := range []string{"refused", "error"} {
		r := NewPinsResult(".")
		r.Status = status
		r.Boundary.Complete = true
		r.Error = &PinsError{Code: "pins-failed", Message: "m"}
		r.Finalize()
		if r.Status != status || r.Boundary.Complete {
			t.Fatalf("%s: %s %t", status, r.Status, r.Boundary.Complete)
		}
	}
}
func TestPinsAddFindingRejectsUnknownCodeAndBoundsMessage(t *testing.T) {
	r := NewPinsResult(".")
	if err := r.AddFinding("future-code", nil, "m"); err == nil || len(r.Findings) != 0 {
		t.Fatal("unknown code accepted")
	}
	if err := r.AddFinding("submodule-dirty", nil, strings.Repeat("é", 1000)+"\x00\xff"); err != nil {
		t.Fatal(err)
	}
	m := r.Findings[0].Message
	if len(m) > pinsMessageLimit || !utf8.ValidString(m) || strings.ContainsRune(m, 0) {
		t.Fatalf("message %d bytes valid=%t", len(m), utf8.ValidString(m))
	}
}
func TestPinsFinalizeNeverEmitsNullArrays(t *testing.T) {
	r := PinsResult{Status: "ok", Backend: "git"}
	r.Submodules = []PinsSubmodule{{Path: "a"}}
	r.Finalize()
	raw, _ := json.Marshal(r)
	for _, bad := range []string{`"submodules":null`, `"findings":null`, `"finding_codes":null`, `"reachable_from":null`} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("%s in %s", bad, raw)
		}
	}
}

func TestPinsRejectsInvalidUTF8InsteadOfReplacingIt(t *testing.T) {
	r := NewPinsResult(".")
	r.Status, r.Backend = "ok", "git"
	r.Submodules = []PinsSubmodule{{Path: "bad\xffpath", PathEncoding: "utf8"}}
	if err := r.Finalize(); err == nil {
		t.Fatal("Finalize accepted a non-UTF-8 path")
	}
	if raw, err := json.Marshal(r); err == nil {
		t.Fatalf("marshal replaced invalid bytes silently: %s", raw)
	}
	// The escaped form is valid text and encodes unchanged.
	r.Submodules[0].Path, r.Submodules[0].PathEncoding = `bad\xffpath`, "escaped"
	if err := r.Finalize(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(raw), `bad\\xffpath`) {
		t.Fatalf("%s %v", raw, err)
	}
	// Strings nested in pointers and identity are checked as well.
	bad := "\xff"
	r.Submodules[0].URL.Configured = &bad
	if _, err := json.Marshal(r); err == nil {
		t.Fatal("nested pointer string accepted")
	}
}

func TestPinsFindingMessageEscapesInvalidBytesInsteadOfReplacingThem(t *testing.T) {
	r := NewPinsResult(".")
	if err := r.AddFinding("submodule-dirty", nil, "a\xffb\x00c"); err != nil {
		t.Fatal(err)
	}
	if got := r.Findings[0].Message; got != `a\xffbc` {
		t.Fatalf("%q", got)
	}
	if err := r.Finalize(); err != nil {
		t.Fatal(err)
	}
}
