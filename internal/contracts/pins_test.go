// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"reflect"

	"github.com/wstein/sourcesteward/internal/inspection"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The normative finding table of specs/021-submodule-pin-audit/spec.md, kept independent of the Go table.
var pinsFindings = map[string]string{
	"pin-unpublished": "error", "gitlink-without-entry": "error", "url-has-credentials": "error",
	"submodule-path-unsafe": "error", "submodule-unsafe-owner": "error", "pins-limit": "error",
	"pins-state-changed": "error", "pin-object-absent": "error",
	"submodule-unsupported-backend": "warning", "staged-pin-differs": "warning", "checkout-differs-from-pin": "warning",
	"submodule-dirty": "warning", "url-mismatch": "warning", "entry-without-gitlink": "warning", "gitmodules-modified": "warning",
	"submodule-uninitialized": "info", "pin-behind-tracking": "info", "nested-submodules-present": "info", "remote-refs-may-be-stale": "info",
}

func pinsFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root(t), "internal/cli/testdata/pins-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func pinsObject(t *testing.T, name string) map[string]any {
	t.Helper()
	return value(t, pinsFixture(t, name)).(map[string]any)
}
func TestPinsSchemaFixtures(t *testing.T) {
	s := schema(t, "pins")
	for name, status := range map[string]string{"success": "ok", "mixed": "ok", "incomplete": "incomplete", "refused": "refused", "error": "error"} {
		valid(t, s, pinsFixture(t, name))
		if got := pinsObject(t, name)["status"]; got != status {
			t.Fatalf("%s fixture has status %v", name, got)
		}
	}
}
func TestPinsContractExampleValidates(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root(t), "specs/021-submodule-pin-audit/contracts/pins.md"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile("(?s)```json\n(.*?)\n```").FindSubmatch(raw)
	if match == nil {
		t.Fatal("contract has no JSON example")
	}
	valid(t, schema(t, "pins"), match[1])
}
func TestExistingContractFixturesStayValid(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(root(t), "internal/cli/testdata/hosting*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("missing existing fixtures: %v", err)
	}
	s := schema(t, "hosting")
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
	}
}
func TestPinsSchemaPinsFindingCodesAndSeverities(t *testing.T) {
	s := schema(t, "pins")
	for code, severity := range pinsFindings {
		for _, candidate := range []string{"error", "warning", "info"} {
			fixture := "success"
			if code == "pins-limit" || code == "pins-state-changed" {
				fixture = "incomplete" // these codes are only valid in an incomplete result
			}
			v := pinsObject(t, fixture)
			v["findings"] = []any{map[string]any{"code": code, "severity": candidate, "submodule": nil, "message": "m"}}
			if err := s.Validate(v); (err == nil) != (candidate == severity) {
				t.Fatalf("%s as %s: %v", code, candidate, err)
			}
		}
	}
	v := pinsObject(t, "success")
	v["findings"] = []any{map[string]any{"code": "future-code", "severity": "info", "submodule": nil, "message": "m"}}
	if s.Validate(v) == nil {
		t.Fatal("unknown finding code accepted")
	}
	if len(pinsFindings) != 19 {
		t.Fatal("finding table must list 19 codes")
	}
}
func TestPinsSchemaRefusalTable(t *testing.T) {
	s := schema(t, "pins")
	for _, tc := range []struct {
		status, code, backend string
		ok                    bool
	}{
		{"refused", "unsupported-backend", "jj", true}, {"refused", "unsupported-backend", "gitbutler", true},
		{"refused", "ambiguous-backend", "ambiguous", true}, {"refused", "bare-repository", "git", true},
		{"refused", "not-repository", "unknown", true}, {"error", "pins-failed", "git", true},
		{"refused", "unsupported-backend", "git", false}, {"refused", "ambiguous-backend", "jj", false},
		{"refused", "bare-repository", "unknown", false}, {"refused", "not-repository", "git", false},
		{"refused", "pins-failed", "git", false}, {"error", "unsupported-backend", "jj", false},
		{"error", "pins-failed", "unknown", true},
		{"error", "pins-failed", "jj", false}, {"error", "pins-failed", "ambiguous", false}, {"refused", "other", "git", false},
	} {
		v := pinsObject(t, "refused")
		v["status"], v["backend"] = tc.status, tc.backend
		v["error"] = map[string]any{"code": tc.code, "message": "m"}
		if err := s.Validate(v); (err == nil) != tc.ok {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
}
func TestPinsSchemaInvariants(t *testing.T) {
	s := schema(t, "pins")
	for _, tc := range []struct {
		name, fixture string
		mutate        func(map[string]any)
	}{
		{"refusal-code-any-string", "mixed", func(v map[string]any) {
			v["submodules"].([]any)[0].(map[string]any)["refusal"].(map[string]any)["code"] = "anything"
		}},
		{"escaped-path-without-refusal", "mixed", func(v map[string]any) { v["submodules"].([]any)[0].(map[string]any)["refusal"] = nil }},
		{"escaped-path-wrong-refusal", "mixed", func(v map[string]any) {
			v["submodules"].([]any)[0].(map[string]any)["refusal"].(map[string]any)["code"] = "submodule-unsafe-owner"
		}},
		{"ok-identity-null", "success", func(v map[string]any) { v["identity"] = nil }},
		{"ok-identity-error-status", "success", func(v map[string]any) {
			i := v["identity"].(map[string]any)
			i["status"], i["repository"] = "error", nil
			i["diagnostics"] = []any{map[string]any{"code": "git-failed", "message": "m"}}
		}},
		{"ok-identity-not-git", "success", func(v map[string]any) { v["identity"].(map[string]any)["backend"] = "jj" }},
		{"incomplete-identity-error-status", "incomplete", func(v map[string]any) { v["identity"].(map[string]any)["status"] = "error" }},
		{"refused-with-findings", "refused", func(v map[string]any) { v["findings"] = pinsObject(t, "success")["findings"] }},
		{"error-with-findings", "error", func(v map[string]any) { v["findings"] = pinsObject(t, "success")["findings"] }},
		{"refused-with-counts", "refused", func(v map[string]any) { v["summary"].(map[string]any)["errors"] = 1 }},
		{"error-with-publication-counts", "error", func(v map[string]any) {
			v["summary"].(map[string]any)["publication"].(map[string]any)["unknown"] = 1
		}},
		{"reachable-without-refs", "success", func(v map[string]any) {
			v["submodules"].([]any)[0].(map[string]any)["publication"].(map[string]any)["reachable_from"] = []any{}
		}},
		{"version", "success", func(v map[string]any) { v["schema_version"] = 2 }},
		{"command", "success", func(v map[string]any) { v["command"] = "inventory" }},
		{"null-submodules", "success", func(v map[string]any) { v["submodules"] = nil }},
		{"null-findings", "success", func(v map[string]any) { v["findings"] = nil }},
		{"ok-incomplete", "success", func(v map[string]any) { v["boundary"].(map[string]any)["complete"] = false }},
		{"ok-with-error", "success", func(v map[string]any) { v["error"] = map[string]any{"code": "pins-failed", "message": "m"} }},
		{"incomplete-complete", "incomplete", func(v map[string]any) { v["boundary"].(map[string]any)["complete"] = true }},
		{"incomplete-without-limit-finding", "incomplete", func(v map[string]any) { v["findings"] = []any{} }},
		{"incomplete-with-error", "incomplete", func(v map[string]any) { v["error"] = map[string]any{"code": "pins-failed", "message": "m"} }},
		{"refused-complete", "refused", func(v map[string]any) { v["boundary"].(map[string]any)["complete"] = true }},
		{"refused-with-submodules", "refused", func(v map[string]any) { v["submodules"] = pinsObject(t, "success")["submodules"] }},
		{"refused-with-identity", "refused", func(v map[string]any) { v["identity"] = pinsObject(t, "success")["identity"] }},
		{"refused-without-error", "refused", func(v map[string]any) { v["error"] = nil }},
		{"error-without-error", "error", func(v map[string]any) { v["error"] = nil }},
		{"network-effect", "success", func(v map[string]any) { v["boundary"].(map[string]any)["effects"].(map[string]any)["network"] = true }},
		{"writes-effect", "success", func(v map[string]any) { v["boundary"].(map[string]any)["effects"].(map[string]any)["writes"] = true }},
		{"limit", "success", func(v map[string]any) {
			v["boundary"].(map[string]any)["limits"].(map[string]any)["max_submodules"] = 257
		}},
		{"missing-staleness", "success", func(v map[string]any) { delete(v["boundary"].(map[string]any), "staleness") }},
		{"short-commit", "success", func(v map[string]any) {
			v["submodules"].([]any)[0].(map[string]any)["pin"].(map[string]any)["head_commit"] = "abc"
		}},
		{"bad-publication", "success", func(v map[string]any) {
			v["submodules"].([]any)[0].(map[string]any)["publication"].(map[string]any)["status"] = "maybe"
		}},
		{"unknown-publication-without-reason", "success", func(v map[string]any) {
			p := v["submodules"].([]any)[0].(map[string]any)["publication"].(map[string]any)
			p["status"], p["reason"] = "unknown", ""
		}},
		{"too-many-refs", "success", func(v map[string]any) {
			refs := []any{}
			for i := 0; i < 17; i++ {
				refs = append(refs, map[string]any{"remote": "origin", "ref": "refs/remotes/origin/b" + strings.Repeat("x", i)})
			}
			v["submodules"].([]any)[0].(map[string]any)["publication"].(map[string]any)["reachable_from"] = refs
		}},
		{"capped-count", "success", func(v map[string]any) {
			v["submodules"].([]any)[1].(map[string]any)["checkout"].(map[string]any)["behind"] = 10001
		}},
		{"bad-relation", "success", func(v map[string]any) {
			v["submodules"].([]any)[1].(map[string]any)["checkout"].(map[string]any)["relation_to_pin"] = "sideways"
		}},
		{"bad-dirty", "success", func(v map[string]any) {
			v["submodules"].([]any)[1].(map[string]any)["checkout"].(map[string]any)["dirty"].(map[string]any)["tracked"] = "true"
		}},
		{"dirty-unknown-without-reason", "success", func(v map[string]any) {
			d := v["submodules"].([]any)[1].(map[string]any)["checkout"].(map[string]any)["dirty"].(map[string]any)
			d["tracked"], d["reason"] = "unknown", ""
		}},
		{"bad-encoding", "success", func(v map[string]any) { v["submodules"].([]any)[0].(map[string]any)["path_encoding"] = "latin1" }},
		{"missing-summary", "success", func(v map[string]any) { delete(v, "summary") }},
		{"negative-summary", "success", func(v map[string]any) { v["summary"].(map[string]any)["errors"] = -1 }},
		{"bad-backend", "success", func(v map[string]any) { v["backend"] = "svn" }},
		{"success-backend-not-git", "success", func(v map[string]any) { v["backend"] = "jj" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := pinsObject(t, tc.fixture)
			tc.mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("invalid pins result accepted")
			}
		})
	}
}

// The Go result type and its finalization must reproduce each schema-valid fixture exactly.
func TestPinsResultRoundTripsAndFinalizesFixtures(t *testing.T) {
	s := schema(t, "pins")
	for _, name := range []string{"success", "mixed", "incomplete", "refused", "error"} {
		var r inspection.PinsResult
		if err := json.Unmarshal(pinsFixture(t, name), &r); err != nil {
			t.Fatal(err)
		}
		r.Finalize()
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
		var got, want any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(pinsFixture(t, name), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s fixture changed by the Go result:\n%s", name, raw)
		}
	}
}

// Real operation results, not only fixtures, must satisfy the schema.
func TestPinsOperationResultsValidateAgainstSchema(t *testing.T) {
	s := schema(t, "pins")
	jj, empty, bare, repo, butler := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(jj, ".jj"), 0o700); err != nil {
		t.Fatal(err)
	}
	run(bare, "init", "-q", "--bare")
	run(repo, "init", "-q")
	run(butler, "init", "-q")
	if err := os.MkdirAll(filepath.Join(butler, ".git", "gitbutler"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{jj: "unsupported-backend", empty: "not-repository", bare: "bare-repository", repo: "pins-failed", butler: "unsupported-backend"} {
		r := inspection.Pins(context.Background(), inspection.PinsOptions{Path: name})
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
		if r.Error == nil || r.Error.Code != want {
			t.Fatalf("%s: %+v", name, r)
		}
	}
}

func TestPinsMixedFixtureCoversRefusedEscapedUninitializedAndUnknown(t *testing.T) {
	valid(t, schema(t, "pins"), pinsFixture(t, "mixed"))
	var r inspection.PinsResult
	if err := json.Unmarshal(pinsFixture(t, "mixed"), &r); err != nil {
		t.Fatal(err)
	}
	var escaped, refused, uninitialized, unknown bool
	for _, s := range r.Submodules {
		escaped = escaped || s.PathEncoding == "escaped"
		refused = refused || s.Refusal != nil
		uninitialized = uninitialized || s.Checkout.State == "uninitialized"
		unknown = unknown || s.Publication.Status == "unknown"
	}
	if !escaped || !refused || !uninitialized || !unknown {
		t.Fatal("mixed fixture lacks a required case")
	}
}
func TestPublishedErrorFixtureHasRealisticMessage(t *testing.T) {
	if strings.Contains(string(pinsFixture(t, "error")), "not implemented") {
		t.Fatal("published error fixture holds the stub message")
	}
}
