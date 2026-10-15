// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
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

func TestWorkspaceNativePlanAndTaskSchema(t *testing.T) {
	s := schema(t, "workspace")
	for _, backend := range []string{"git", "jj"} {
		identity := workspace.NativeIdentity{Backend: backend, RepositoryDir: "/repo/.git", WorkspaceRoot: "/repo", GitDir: "/repo/.git", CommitID: strings.Repeat("a", 40), ObjectFormat: "sha1"}
		baseChange := ""
		if backend == "jj" {
			identity.RepositoryDir = "/repo/.jj/repo"
			identity.WorkspaceName = "default"
			identity.ChangeID = strings.Repeat("k", 32)
			identity.OperationID = strings.Repeat("b", 128)
			baseChange = identity.ChangeID
		}
		plan := &workspace.MutationPlan{SchemaVersion: 1, PlanID: "generated-id", TaskID: "task", Owner: "owner", SourcePath: "/repo", Destination: "/task", StateDir: "/state", Backend: backend, Reason: "isolated task", Digest: strings.Repeat("c", 64), Expected: workspace.Snapshot{Identity: identity, BaseSelector: "HEAD", BaseCommit: identity.CommitID, BaseChange: baseChange, ReferenceDigest: strings.Repeat("d", 64), RegistrationDigest: strings.Repeat("e", 64), SafetyDigest: strings.Repeat("f", 64)}, DestinationParent: workspace.PathIdentity{Path: "/", Device: 1, Inode: 1}, Effects: []string{"native workspace"}, RecoveryLimits: []string{"manual retention"}}
		result := workspace.Result{SchemaVersion: 1, Command: "workspace-plan", Status: "ok", Backend: backend, Plan: plan, Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{}}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
		result.Command = "workspace-handoff"
		result.Task = &workspace.TaskRecord{SchemaVersion: 1, TaskID: "task", Owner: "owner", PlanID: plan.PlanID, Backend: backend, Destination: "/task", RecoveryPointer: "/state/operations/generated-id.json", Native: identity, DestinationIdentity: workspace.PathIdentity{Path: "/task", Device: 1, Inode: 2}, BaseCommit: identity.CommitID, BaseChange: baseChange, State: "observed", RemainingWork: []string{}, RecoveryLimits: []string{}}
		raw, err = json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
		for _, field := range []string{"commit_id", "change_id", "operation_id"} {
			v := value(t, raw).(map[string]any)
			v["task"].(map[string]any)["native"].(map[string]any)[field] = "invalid-id"
			if s.Validate(v) == nil {
				t.Fatalf("backend %s accepted malformed %s", backend, field)
			}
		}
	}
}
