// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func TestWorkspaceSchemaFixtures(t *testing.T) {
	s := schema(t, "workspace")
	seed := workspace.Result{SchemaVersion: 1, Command: "workspace-apply", Status: "refused", Backend: "unknown", Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{{Code: "authorization-required", Message: "explicit authorization required"}}, Limitations: []string{}}
	raw, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	valid(t, s, raw)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"version", func(v map[string]any) { v["schema_version"] = 2 }},
		{"command", func(v map[string]any) { v["command"] = "inventory" }},
		{"status", func(v map[string]any) { v["status"] = "success" }},
		{"null-actions", func(v map[string]any) { v["actions"] = nil }},
		{"bad-action", func(v map[string]any) { v["actions"] = []any{map[string]any{"name": "create"}} }},
		{"missing-diagnostic", func(v map[string]any) { v["diagnostics"] = []any{} }},
		{"success-without-evidence", func(v map[string]any) { v["status"] = "ok"; v["diagnostics"] = []any{} }},
		{"untyped-plan", func(v map[string]any) { v["plan"] = map[string]any{"plan_id": "a"} }},
		{"untyped-task", func(v map[string]any) { v["task"] = map[string]any{"task_id": "a"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			tc.mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("invalid workspace output accepted")
			}
		})
	}
	v := value(t, raw).(map[string]any)
	v["future"] = true
	if err := s.Validate(v); err != nil {
		t.Fatal(err)
	}
}
func TestRealWorkspaceCLIErrorSchema(t *testing.T) {
	s := schema(t, "workspace")
	for _, args := range [][]string{
		{"workspace", "plan", "/nonexistent-source-stw-contract", "--base", "HEAD", "--destination", "/nonexistent-parent-stw-contract/dest", "--task", "contract", "--owner", "tester", "--state-dir", "/nonexistent-state-stw-contract", "--json"},
		{"workspace", "apply", "--plan", "absent", "--state-dir", "/nonexistent-state-stw-contract", "--json"},
		{"workspace", "handoff", "/nonexistent-source-stw-contract", "--state-dir", "/nonexistent-state-stw-contract", "--json"},
		{"workspace", "retire-plan", "/nonexistent-source-stw-contract", "--state-dir", "/nonexistent-state-stw-contract", "--json"},
	} {
		var out, diagnostic bytes.Buffer
		if code := cli.Run(context.Background(), "test", args, &out, &diagnostic); code != 1 {
			t.Fatalf("%q exit%d %s %s", args, code, out.String(), diagnostic.String())
		}
		valid(t, s, out.Bytes())
	}
}
