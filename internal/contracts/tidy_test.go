// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func TestTidySchemaFixtures(t *testing.T) {
	s := schema(t, "tidy")
	for _, backend := range []string{"git", "jj"} {
		id := workspace.NativeIdentity{Backend: backend, RepositoryDir: "/repo/.git", WorkspaceRoot: "/repo", GitDir: "/repo/.git", CommitID: strings.Repeat("a", 40), ObjectFormat: "sha1"}
		candidate := workspace.TidyCandidate{Kind: "branch", Name: "topic", CommitID: id.CommitID, Decision: "delete", Reason: "integrated", Proof: "native-ancestry", WorkspacePaths: []string{}, Limitations: []string{}}
		if backend == "jj" {
			id.RepositoryDir = "/repo/.jj/repo"
			id.WorkspaceName = "default"
			id.ChangeID = strings.Repeat("k", 32)
			id.OperationID = strings.Repeat("b", 128)
			candidate.Kind = "bookmark"
			candidate.ChangeID = id.ChangeID
		}
		plan := &workspace.TidyPlanRecord{SchemaVersion: 1, SourcePath: "/repo", Backend: backend, Digest: strings.Repeat("c", 64), Expected: workspace.Snapshot{Identity: id, BaseSelector: "main", BaseCommit: id.CommitID, BaseChange: id.ChangeID, ReferenceDigest: strings.Repeat("d", 64), RegistrationDigest: strings.Repeat("e", 64), SafetyDigest: strings.Repeat("f", 64)}, SelectedRefs: []string{}, Candidates: []workspace.TidyCandidate{candidate}, Effects: []string{}, RecoveryLimits: []string{}}
		if backend == "jj" {
			plan.Expected.ReferenceDigest = id.OperationID
		}
		seed := workspace.TidyResult{SchemaVersion: 1, Command: "tidy-plan", Status: "ok", Backend: backend, Plan: plan, Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{}}
		raw, err := json.Marshal(seed)
		if err != nil {
			t.Fatal(err)
		}
		valid(t, s, raw)
		for _, tc := range []struct {
			name   string
			mutate func(map[string]any)
		}{
			{"bad-version", func(v map[string]any) { v["schema_version"] = 2 }},
			{"no-plan", func(v map[string]any) { v["plan"] = nil }},
			{"nil-actions", func(v map[string]any) { v["actions"] = nil }},
			{"bad-proof", func(v map[string]any) {
				v["plan"].(map[string]any)["candidates"].([]any)[0].(map[string]any)["proof"] = "merged-pr"
			}},
			{"no-proof-delete", func(v map[string]any) {
				v["plan"].(map[string]any)["candidates"].([]any)[0].(map[string]any)["proof"] = "none"
			}},
			{"used-delete", func(v map[string]any) {
				v["plan"].(map[string]any)["candidates"].([]any)[0].(map[string]any)["workspace_paths"] = []any{"/repo"}
			}},
			{"empty-native-id", func(v map[string]any) {
				v["plan"].(map[string]any)["candidates"].([]any)[0].(map[string]any)["commit_id"] = ""
			}},
			{"wrong-backend", func(v map[string]any) { v["backend"] = "unknown" }},
		} {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				v := value(t, raw).(map[string]any)
				tc.mutate(v)
				if s.Validate(v) == nil {
					t.Fatal("invalid tidy result accepted")
				}
			})
		}
		v := value(t, raw).(map[string]any)
		v["future"] = true
		if err := s.Validate(v); err != nil {
			t.Fatal(err)
		}
		if backend == "jj" {
			plan.Candidates[0].CommitID = ""
			plan.Candidates[0].ChangeID = ""
			plan.Candidates[0].Decision = "retain"
			plan.Candidates[0].Proof = "none"
			plan.Candidates[0].Reason = "ambiguous-target"
			raw, _ := json.Marshal(seed)
			valid(t, s, raw)
		}
	}
}
func TestRealTidyCLIErrorSchema(t *testing.T) {
	s := schema(t, "tidy")
	var out, diagnostic bytes.Buffer
	code := cli.Run(context.Background(), "test", []string{"tidy", "plan", "/nonexistent-source-stw-contract", "--base", "HEAD", "--json"}, &out, &diagnostic)
	if code != 1 {
		t.Fatalf("exit%d %s %s", code, out.String(), diagnostic.String())
	}
	valid(t, s, out.Bytes())
}
func TestRealGitTidyPlanSchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source")
	state := filepath.Join(dir, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "main", source}, {"-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "initial"}, {"-C", source, "branch", "integrated"}, {"-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "advance base"}} {
		if out, err := exec.Command("git", append([]string{"-c", "maintenance.auto=false", "-c", "gc.auto=0"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
	}
	s := schema(t, "tidy")
	for _, persist := range []bool{false, true} {
		var out, diagnostic bytes.Buffer
		args := []string{"tidy", "plan", source, "--base", "refs/heads/main", "--ref", "refs/heads/integrated", "--json"}
		if persist {
			args = append(args, "--state-dir", state)
		}
		code := cli.Run(context.Background(), "test", args, &out, &diagnostic)
		valid(t, s, out.Bytes())
		if code != 0 {
			t.Fatalf("exit%d %s %s", code, out.String(), diagnostic.String())
		}
		var r workspace.TidyResult
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Plan == nil || len(r.Plan.Candidates) != 1 || r.Plan.Candidates[0].Decision != "delete" {
			t.Fatalf("%s", out.String())
		}
	}
}

func TestRealJJTidyPlanSchema(t *testing.T) {
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
	s := schema(t, "tidy")
	for _, colocate := range []bool{false, true} {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		source := filepath.Join(dir, "source")
		mode := "--no-colocate"
		if colocate {
			mode = "--colocate"
		}
		if out, err := exec.Command("jj", "git", "init", mode, source).CombinedOutput(); err != nil {
			t.Fatalf("%s %v", out, err)
		}
		if err := os.WriteFile(filepath.Join(source, "file"), []byte("content\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"describe", "-m", "initial"}, {"bookmark", "create", "base", "integrated"}, {"new"}} {
			cmd := exec.Command("jj", args...)
			cmd.Dir = source
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s %v", out, err)
			}
		}
		var out, diagnostic bytes.Buffer
		code := cli.Run(context.Background(), "test", []string{"tidy", "plan", source, "--base", "base", "--ref", "integrated", "--json"}, &out, &diagnostic)
		valid(t, s, out.Bytes())
		if code != 0 {
			var r workspace.TidyResult
			if err := json.Unmarshal(out.Bytes(), &r); err != nil {
				t.Fatal(err)
			}
			for _, d := range r.Diagnostics {
				if strings.Contains(d.Code, "sandbox-unavailable") && os.Getenv("STW_REQUIRE_JJ") != "1" && os.Getenv("STW_REQUIRE_NATIVE_SANDBOX") != "1" {
					t.Skip("native jj sandbox unavailable")
				}
			}
			t.Fatalf("exit%d %s %s", code, out.String(), diagnostic.String())
		}
	}
}
