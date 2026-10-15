// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		if backend == "jj" {
			plan.Expected.ReferenceDigest = identity.OperationID
		}
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
		v := value(t, raw).(map[string]any)
		other := "git"
		if backend == "git" {
			other = "jj"
		}
		v["backend"] = other
		if s.Validate(v) == nil {
			t.Fatalf("accepted mismatched %s native evidence", backend)
		}
		for _, field := range []string{"commit_id", "change_id", "operation_id"} {
			v := value(t, raw).(map[string]any)
			v["task"].(map[string]any)["native"].(map[string]any)[field] = "invalid-id"
			if s.Validate(v) == nil {
				t.Fatalf("backend %s accepted malformed %s", backend, field)
			}
		}
	}
}

func TestRealGitWorkspaceLifecycleSchema(t *testing.T) {
	// Production refuses Apple's developer-tool shim. Tests may select the actual
	// installed native executable; mandatory CI supplies pinned Git through PATH.
	if executable, err := exec.LookPath("git"); err == nil && runtime.GOOS == "darwin" && executable == "/usr/bin/git" {
		for _, candidate := range []string{"/Applications/Xcode.app/Contents/Developer/usr/bin/git", "/Library/Developer/CommandLineTools/usr/bin/git"} {
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				t.Setenv("PATH", filepath.Dir(candidate)+string(os.PathListSeparator)+os.Getenv("PATH"))
				break
			}
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	state := filepath.Join(root, "state")
	dest := filepath.Join(root, "task")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main", source}, {"-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %s %v", out, err)
		}
	}
	s := schema(t, "workspace")
	invoke := func(args ...string) workspace.Result {
		t.Helper()
		var out, diagnostic bytes.Buffer
		args = append(args, "--json")
		code := cli.Run(context.Background(), "test", args, &out, &diagnostic)
		valid(t, s, out.Bytes())
		var result workspace.Result
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if code != 0 {
			t.Fatalf("workspace exit%d: %s %s", code, out.String(), diagnostic.String())
		}
		return result
	}
	plan := invoke("workspace", "plan", source, "--base", "HEAD", "--destination", dest, "--task", "contract-task", "--owner", "tester", "--state-dir", state)
	if plan.Plan == nil {
		t.Fatal("missing real plan")
	}
	applied := invoke("workspace", "apply", "--plan", plan.Plan.PlanID, "--state-dir", state, "--authorize")
	if applied.Task == nil {
		t.Fatal("missing task identity")
	}
	invoke("workspace", "apply", "--plan", plan.Plan.PlanID, "--state-dir", state, "--authorize")
	invoke("workspace", "handoff", dest, "--state-dir", state)
	invoke("workspace", "retire-plan", dest, "--state-dir", state)
}

func TestRealJJWorkspaceLifecycleSchema(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		if os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatal("mandatory jj missing")
		}
		t.Skip("jj unavailable")
	}
	home := t.TempDir()
	config := filepath.Join(home, "config.toml")
	if err := os.WriteFile(config, []byte("[user]\nname = \"Tester\"\nemail = \"test@example.org\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("JJ_CONFIG", config)
	s := schema(t, "workspace")
	for _, colocate := range []bool{false, true} {
		t.Run(fmt.Sprintf("colocated-%t", colocate), func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(root, "source")
			state := filepath.Join(root, "state")
			dest := filepath.Join(root, "task")
			if err := os.Mkdir(state, 0700); err != nil {
				t.Fatal(err)
			}
			args := []string{"git", "init", "--no-colocate", source}
			if colocate {
				args = []string{"git", "init", "--colocate", source}
			}
			command := exec.Command("jj", args...)
			if out, err := command.CombinedOutput(); err != nil {
				t.Fatalf("jj fixture: %s %v", out, err)
			}
			invoke := func(args ...string) workspace.Result {
				t.Helper()
				var out, diagnostic bytes.Buffer
				args = append(args, "--json")
				code := cli.Run(context.Background(), "test", args, &out, &diagnostic)
				valid(t, s, out.Bytes())
				var result workspace.Result
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if code != 0 {
					for _, d := range result.Diagnostics {
						if (d.Code == "jj-sandbox-unavailable" || d.Code == "jj-workspace-sandbox-unavailable") && os.Getenv("STW_REQUIRE_JJ") != "1" && os.Getenv("STW_REQUIRE_NATIVE_SANDBOX") != "1" {
							t.Skip("native jj sandbox unavailable")
						}
					}
					t.Fatalf("workspace exit%d: %s %s", code, out.String(), diagnostic.String())
				}
				return result
			}
			plan := invoke("workspace", "plan", source, "--base", "@", "--destination", dest, "--task", "jj-contract-task", "--owner", "tester", "--state-dir", state)
			if plan.Plan == nil {
				t.Fatal("missing jj plan")
			}
			applied := invoke("workspace", "apply", "--plan", plan.Plan.PlanID, "--state-dir", state, "--authorize")
			if applied.Task == nil || applied.Task.Native.ChangeID == "" || applied.Task.Native.OperationID == "" {
				t.Fatal("missing native jj identities")
			}
			invoke("workspace", "apply", "--plan", plan.Plan.PlanID, "--state-dir", state, "--authorize")
			invoke("workspace", "handoff", dest, "--state-dir", state)
			invoke("workspace", "retire-plan", dest, "--state-dir", state)
		})
	}
}
