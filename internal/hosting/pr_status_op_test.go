// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/inspection"
)

func TestPRStatusOperationExpectedHead(t *testing.T) {
	head := strings.Repeat("a", 40)
	result := strings.Repeat("d", 40)
	merged := func() []byte {
		p := statusBody(t)
		p["state"], p["merged"], p["merge_commit_sha"] = "closed", true, result
		p["closed_at"], p["merged_at"] = "2026-10-18T12:00:00Z", "2026-10-18T12:00:00Z"
		return statusBytes(t, p)
	}
	for _, tc := range []struct {
		name, expect string
		body         []byte
		wantStatus   string
		wantStop     string
		wantExp      string
		wantCode     string
	}{
		{"none", "", statusBytes(t, statusBody(t)), "ok", "sampled", "not-requested", ""},
		{"match", head, statusBytes(t, statusBody(t)), "ok", "sampled", "matched", ""},
		{"mismatch-open", strings.Repeat("c", 40), statusBytes(t, statusBody(t)), "error", "expectation-mismatch", "mismatched", "hosting-head-mismatch"},
		{"mismatch-even-when-merged", strings.Repeat("c", 40), merged(), "error", "expectation-mismatch", "mismatched", "hosting-head-mismatch"},
		// The expectation names the head, never GitHub's result SHA.
		{"result-sha-is-not-the-head", result, merged(), "error", "expectation-mismatch", "mismatched", "hosting-head-mismatch"},
		{"merged-match", head, merged(), "ok", "sampled", "matched", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			d := fixtureDeps(func(_ context.Context, p string) ([]byte, error) {
				calls++
				if p != "repos/a/b/pulls/3" {
					t.Fatal(p)
				}
				return tc.body, nil
			})
			r := inspectPRStatus(context.Background(), ".", Options{Kind: "pr", Number: 3, ExpectHead: tc.expect, Status: true}, d)
			if calls != 1 || r.Status != tc.wantStatus || r.Observation.Stop != tc.wantStop || r.Observation.Expectation != tc.wantExp || r.Boundary.Requests != 1 {
				t.Fatalf("%+v calls=%d", r, calls)
			}
			if (tc.expect == "") != (r.Observation.ExpectedHead == nil) {
				t.Fatalf("expected_head %+v", r.Observation.ExpectedHead)
			}
			if tc.wantCode != "" && (len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != tc.wantCode || r.PullRequest == nil || r.Boundary.Complete) {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestPRStatusOperationFailuresMakeNoRetryAndNoGETOnNativeFailure(t *testing.T) {
	calls := 0
	d := fixtureDeps(func(context.Context, string) ([]byte, error) {
		calls++
		return nil, errors.New("HTTP 429 Retry-After: 60 token-secret")
	})
	r := inspectPRStatus(context.Background(), ".", Options{Kind: "pr", Number: 3, ExpectHead: strings.Repeat("a", 40), Status: true}, d)
	if calls != 1 || r.Status != "error" || r.Observation.Stop != "failed" || r.Observation.Expectation != "unobserved" || r.PullRequest != nil || strings.Contains(r.Error(), "secret") {
		t.Fatalf("%+v calls=%d", r, calls)
	}
	native := fixtureDeps(func(context.Context, string) ([]byte, error) { calls++; return nil, nil })
	native.remotes = func(context.Context, string) inspection.RemoteResult {
		return inspection.RemoteResult{Status: "error", Identity: inspection.Result{Status: "error", Backend: "git"}}
	}
	calls = 0
	r = inspectPRStatus(context.Background(), ".", Options{Kind: "pr", Number: 3, Status: true}, native)
	if calls != 0 || r.Status != "error" || r.Boundary.Network || r.Boundary.Requests != 0 || len(r.Diagnostics) != 1 || r.Diagnostics[0].Code != "hosting-native-failed" || r.Backend != "git" {
		t.Fatalf("%+v calls=%d", r, calls)
	}
}

func TestPRStatusOptionsValidation(t *testing.T) {
	for _, o := range []Options{
		{Kind: "pr", Number: 3, Status: true, ExpectHead: "ABCDEF"},
		{Kind: "pr", Number: 3, Status: true, ExpectHead: strings.Repeat("A", 40)},
		{Kind: "pr", Number: 3, Status: true, ExpectHead: strings.Repeat("a", 64)},
		{Kind: "pr", Number: 3, ExpectHead: strings.Repeat("a", 40)}, // v1 cannot carry an expectation
		{Kind: "ci", Number: 3, Status: true},
		{Kind: "prs", HeadSHA: "short"},
		{Kind: "prs", HeadSHA: strings.Repeat("a", 40), Number: 3},
		{Kind: "prs", HeadSHA: strings.Repeat("a", 40), ExpectHead: strings.Repeat("a", 40)},
	} {
		if ValidateOptions(o) == nil {
			t.Fatalf("accepted %+v", o)
		}
	}
	for _, o := range []Options{{Kind: "pr", Number: 3, Status: true, ExpectHead: strings.Repeat("a", 40)}, {Kind: "prs", HeadSHA: strings.Repeat("a", 40)}} {
		if e := ValidateOptions(o); e != nil {
			t.Fatal(e)
		}
	}
}

func compileOnceSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	raw, e := os.ReadFile("../../schemas/v2/hosting-pr.schema.json")
	if e != nil {
		t.Fatal(e)
	}
	doc, e := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(prStatusSchemaLoader{})
	c.AssertFormat()
	const id = "https://wstein.github.io/sourcesteward/schemas/v2/hosting-pr.schema.json"
	if e = c.AddResource(id, doc); e != nil {
		t.Fatal(e)
	}
	s, e := c.Compile(id)
	if e != nil {
		t.Fatal(e)
	}
	return s
}

// The design examples and the slice-1 schema are one contract: every once example
// validates, and each deferred (watch) shape is rejected.
func TestPRStatusOnceSchemaExamplesAndCounterexamples(t *testing.T) {
	schema := compileOnceSchema(t)
	files, e := filepath.Glob("../../specs/030-pr-status/contracts/once/*.json")
	if e != nil || len(files) != 7 {
		t.Fatalf("%v %v", files, e)
	}
	load := func(f string) map[string]any {
		raw, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		var m map[string]any
		if e = json.Unmarshal(raw, &m); e != nil {
			t.Fatal(e)
		}
		return m
	}
	for _, f := range files {
		if e := schema.Validate(load(f)); e != nil {
			t.Fatalf("%s: %v", f, e)
		}
	}
	for name, mutate := range map[string]func(map[string]any){
		"watch-mode":    func(m map[string]any) { m["observation"].(map[string]any)["mode"] = "watch" },
		"max-requests":  func(m map[string]any) { m["boundary"].(map[string]any)["max_requests"] = 8.0 },
		"stop-merged":   func(m map[string]any) { m["observation"].(map[string]any)["stop"] = "merged" },
		"stop-timeout":  func(m map[string]any) { m["observation"].(map[string]any)["stop"] = "timeout" },
		"stop-changed":  func(m map[string]any) { m["observation"].(map[string]any)["stop"] = "changed" },
		"unbounded":     func(m map[string]any) { m["observation"].(map[string]any)["stop"] = "output-unbounded" },
		"identity-move": func(m map[string]any) { m["observation"].(map[string]any)["identity_changed"] = true },
		"watch-limit": func(m map[string]any) {
			b := m["boundary"].(map[string]any)
			b["limitations"] = append(b["limitations"].([]any), "native-descendant-state-unverified")
		},
	} {
		m := load("../../specs/030-pr-status/contracts/once/once-open.json")
		mutate(m)
		if schema.Validate(m) == nil {
			t.Fatalf("schema accepted deferred shape %s", name)
		}
	}
}
