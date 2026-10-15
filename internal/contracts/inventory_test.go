// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/inspection"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func inventorySeed() string {
	return `{"schema_version":1,"command":"inventory","status":"ok","backend":"git","input_path":"/repo","identity":{"schema_version":1,"status":"ok","backend":"git","input_path":"/repo","evidence":[],"repository":{"git_dir":"/repo/.git","common_dir":"/repo/.git","worktree":"/repo","bare":false,"linked_worktree":false,"object_format":"sha1","head":{"state":"attached","ref":"refs/heads/main","oid":"` + strings.Repeat("a", 40) + `"}},"diagnostics":[]},"boundary":{"complete":true,"freshness":"sampled-nontransactional","limitations":[],"limits":{"deadline_seconds":10,"command_bytes":65536,"result_bytes":1048576,"records":1024}},"workspaces":[],"references":[],"bases":[],"files":[],"query":{"predicate":"ref-exists","value":"false","reason":"absent","evidence":[]},"diagnostics":[]}`
}
func TestInventorySchemaInvariants(t *testing.T) {
	s := schema(t, "inventory")
	valid(t, s, []byte(inventorySeed()))
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"version", func(v map[string]any) { v["schema_version"] = 2 }},
		{"command", func(v map[string]any) { v["command"] = "inspect" }},
		{"null-array", func(v map[string]any) { v["files"] = nil }},
		{"untyped-file", func(v map[string]any) { v["files"] = []any{map[string]any{"path": "x"}} }},
		{"unknown-success", func(v map[string]any) { v["query"].(map[string]any)["value"] = "unknown" }},
		{"boolean-query", func(v map[string]any) { v["query"].(map[string]any)["value"] = false }},
		{"partial-complete", func(v map[string]any) { v["status"] = "partial" }},
		{"error-empty-diagnostics", func(v map[string]any) { v["status"] = "error"; v["boundary"].(map[string]any)["complete"] = false }},
		{"backend-mismatch", func(v map[string]any) { v["backend"] = "jj" }},
		{"limit", func(v map[string]any) { v["boundary"].(map[string]any)["limits"].(map[string]any)["records"] = 1025 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := value(t, []byte(inventorySeed())).(map[string]any)
			tc.mutate(v)
			if s.Validate(v) == nil {
				t.Fatal("invalid inventory accepted")
			}
		})
	}
	v := value(t, []byte(inventorySeed())).(map[string]any)
	v["future"] = true
	v["status"] = "partial"
	v["boundary"].(map[string]any)["complete"] = false
	v["query"].(map[string]any)["value"] = "unknown"
	if err := s.Validate(v); err != nil {
		t.Fatal(err)
	}
	v["status"] = "error"
	v["diagnostics"] = []any{map[string]any{"code": "future-code", "message": "failed"}}
	if err := s.Validate(v); err != nil {
		t.Fatal(err)
	}
}
func TestInventoryNativeIdentityConstraints(t *testing.T) {
	s := schema(t, "inventory")
	v := value(t, []byte(inventorySeed())).(map[string]any)
	v["backend"] = "jj"
	v["identity"] = value(t, []byte(jjSeed()))
	v["status"] = "partial"
	v["boundary"].(map[string]any)["complete"] = false
	v["query"] = nil
	v["workspaces"] = []any{map[string]any{"path": nil, "name": "default", "manager": "jj", "registration": "unknown", "task_owner": "unknown", "commit_id": strings.Repeat("a", 40), "change_id": strings.Repeat("k", 32), "operation_id": strings.Repeat("b", 128), "tracked": map[string]any{"value": "unknown", "reason": "unsnapshotted"}, "untracked": map[string]any{"value": "unknown", "reason": "unsnapshotted"}, "conflicts": map[string]any{"value": "false", "reason": ""}, "limitations": []any{}}}
	if err := s.Validate(v); err != nil {
		t.Fatal(err)
	}
	for field, bad := range map[string]any{"commit_id": strings.Repeat("a", 64), "change_id": strings.Repeat("a", 32), "operation_id": strings.Repeat("b", 127), "task_owner": "me", "tracked": map[string]any{"value": "unknown", "reason": ""}} {
		raw, _ := json.Marshal(v)
		copy := value(t, raw).(map[string]any)
		copy["workspaces"].([]any)[0].(map[string]any)[field] = bad
		if s.Validate(copy) == nil {
			t.Fatalf("accepted bad %s", field)
		}
	}
}

// Validate real CLI envelopes, including known-false and operational failures.
func TestRealInventoryCLIOutputs(t *testing.T) {
	s := schema(t, "inventory")
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	repo := t.TempDir()
	command := exec.Command(git, "init", "-q", "-b", "main", repo)
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("init: %s %v", raw, err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{{"absent-reference", []string{"inventory", repo, "--query", "ref-exists", "--ref", "refs/heads/absent", "--json"}, 0}, {"missing-path", []string{"inventory", filepath.Join(home, "absent"), "--json"}, 1}, {"usage", []string{"inventory", repo, "--query", "ancestor", "--from", "main", "--json"}, 2}} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			code := cli.Run(context.Background(), "test", tc.args, &out, &diagnostic)
			if code != tc.want {
				t.Fatalf("exit %d want %d: %s %s", code, tc.want, out.String(), diagnostic.String())
			}
			if code == 2 {
				if out.Len() != 0 {
					t.Fatal("usage emitted JSON")
				}
				return
			}
			valid(t, s, out.Bytes())
			if !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
				t.Fatal("missing final newline")
			}
		})
	}
	if err := os.WriteFile(filepath.Join(repo, "literal\n日本語"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	if code := cli.Run(context.Background(), "test", []string{"inventory", repo, "--json"}, &out, &diagnostic); code != 0 {
		t.Fatalf("%d %s %s", code, out.String(), diagnostic.String())
	}
	valid(t, s, out.Bytes())
}

func TestRealJJInventoryCLIOutputs(t *testing.T) {
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
	repo := filepath.Join(t.TempDir(), "repo")
	command := exec.Command("jj", "git", "init", "--no-colocate", repo)
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("init: %s %v", raw, err)
	}
	s := schema(t, "inventory")
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{{"false-reference", []string{"--query", "ref-exists", "--ref", "absent"}, 0}, {"unsnapshotted-disk", []string{"--query", "dirty"}, 1}, {"stored", []string{"--scope", "stored"}, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			args := append([]string{"inventory", repo, "--json"}, tc.args...)
			code := cli.Run(context.Background(), "test", args, &out, &diagnostic)
			if code != tc.want && os.Getenv("STW_REQUIRE_JJ") != "1" {
				var r inspection.InventoryResult
				if json.Unmarshal(out.Bytes(), &r) == nil {
					for _, d := range r.Diagnostics {
						if d.Code == "jj-sandbox-unavailable" {
							t.Skip("native sandbox unavailable")
						}
					}
				}
			}
			if code != tc.want {
				t.Fatalf("exit %d want %d: %s %s", code, tc.want, out.String(), diagnostic.String())
			}
			valid(t, s, out.Bytes())
		})
	}
}
