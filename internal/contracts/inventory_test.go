// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	command = exec.Command(git, "-C", repo, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "initial")
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("commit: %s %v", raw, err)
	}
	linked := filepath.Join(t.TempDir(), "linked workspace")
	command = exec.Command(git, "-C", repo, "worktree", "add", "-b", "linked", linked)
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("worktree: %s %v", raw, err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{{"absent-reference", []string{"inventory", repo, "--query", "ref-exists", "--ref", "refs/heads/absent", "--json"}, 0}, {"missing-path", []string{"inventory", filepath.Join(home, "absent"), "--json"}, 1}, {"oversized-input", []string{"inventory", strings.Repeat("<", 100000), "--json"}, 1}, {"usage", []string{"inventory", repo, "--query", "ancestor", "--from", "main", "--json"}, 2}} {
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
			if out.Len() > 1048576 {
				t.Fatal("result exceeds inventory JSON byte bound")
			}
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
	var result inspection.InventoryResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Workspaces) != 2 || len(result.References) < 2 {
		t.Fatalf("expected registered worktrees and committed references: %s", out.String())
	}
	for _, reference := range result.References {
		if len(reference.AddedTargets) != 1 || reference.CommitID == nil || len(*reference.CommitID) != 40 {
			t.Fatalf("missing committed Git target: %+v", reference)
		}
	}
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
	command = exec.Command("jj", "-R", repo, "bookmark", "create", "task", "-r", "@")
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bookmark: %s %v", raw, err)
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
			if tc.name == "stored" {
				var result inspection.InventoryResult
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				found := false
				for _, reference := range result.References {
					if reference.Name != "task" {
						continue
					}
					found = true
					if len(reference.AddedTargets) != 1 || reference.CommitID == nil || reference.ChangeID == nil || len(*reference.CommitID) != 40 || len(*reference.ChangeID) != 32 {
						t.Fatalf("missing native jj target identities: %+v", reference)
					}
				}
				if !found {
					t.Fatalf("native bookmark absent: %s", out.String())
				}
			}
		})
	}
}

// Spec 027: real outputs of the large-repository, truncation, checkout and gitlink behaviors validate against
// the unchanged inventory schema, and the exit statuses follow the single exit rule (0 only for ok).
func TestRealInventoryOutputsOfLimitsAndCheckouts(t *testing.T) {
	s := schema(t, "inventory")
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	t.Setenv("GIT_TEMPLATE_DIR", t.TempDir())
	git := func(dir string, stdin string, args ...string) {
		t.Helper()
		c := exec.Command(gitBin, append([]string{"-c", "user.name=Tester", "-c", "user.email=test@example.org", "-c", "protocol.file.allow=always"}, args...)...)
		c.Dir = dir
		c.Stdin = strings.NewReader(stdin)
		if raw, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, raw, err)
		}
	}
	run := func(want int, args ...string) map[string]any {
		t.Helper()
		var out, diagnostic bytes.Buffer
		code := cli.Run(context.Background(), "test", append([]string{"inventory"}, append(args, "--json")...), &out, &diagnostic)
		if code != want {
			t.Fatalf("%v: exit %d want %d: %.400s %s", args, code, want, out.String(), diagnostic.String())
		}
		valid(t, s, out.Bytes())
		return value(t, out.Bytes()).(map[string]any)
	}
	big := t.TempDir()
	git(big, "", "init", "-q", "-b", "main")
	for i := 0; i < 1100; i++ {
		if err := os.WriteFile(filepath.Join(big, fmt.Sprintf("file%05d.txt", i)), []byte(fmt.Sprintf("content %d\n", i)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git(big, "", "add", "-A")
	git(big, "", "commit", "-qm", "initial")
	if r := run(0, big); r["status"] != "ok" {
		t.Fatalf("clean repository with 1,100 files: %v", r["status"])
	}
	var tags strings.Builder
	for i := 0; i < 700; i++ {
		fmt.Fprintf(&tags, "create refs/tags/release-%04d HEAD\n", i)
	}
	git(big, tags.String(), "update-ref", "--stdin")
	r := run(1, big)
	if r["status"] != "partial" || !strings.Contains(fmt.Sprint(r["boundary"]), "reference listing truncated") {
		t.Fatalf("truncated references: %v %v", r["status"], r["boundary"])
	}
	if q := run(0, big, "--query", "ref-exists", "--ref", "refs/tags/release-0699"); q["status"] != "ok" || q["query"].(map[string]any)["value"] != "true" {
		t.Fatalf("%v", q["query"])
	}
	for i := 0; i < 1100; i++ {
		os.WriteFile(filepath.Join(big, fmt.Sprintf("file%05d.txt", i)), []byte("changed\n"), 0600)
	}
	if d := run(0, big, "--query", "dirty"); d["status"] != "ok" || d["query"].(map[string]any)["value"] != "true" {
		t.Fatalf("dirty query on a truncated listing must exit 0: %v", d["status"])
	}
	if p := run(1, big); p["status"] != "partial" {
		t.Fatalf("%v", p["status"])
	}

	root := t.TempDir()
	src := filepath.Join(root, "lib-src")
	super := filepath.Join(root, "super")
	os.Mkdir(src, 0700)
	git(src, "", "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(src, "a"), []byte("a\n"), 0600)
	git(src, "", "add", "-A")
	git(src, "", "commit", "-qm", "lib")
	os.Mkdir(super, 0700)
	git(super, "", "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(super, "s"), []byte("s\n"), 0600)
	git(super, "", "add", "-A")
	git(super, "", "commit", "-qm", "super")
	git(super, "", "submodule", "add", "-q", src, "mods/lib")
	git(super, "", "commit", "-qm", "submodule")
	sub := run(0, filepath.Join(super, "mods", "lib"))
	ws := sub["workspaces"].([]any)[0].(map[string]any)
	if sub["status"] != "ok" || ws["tracked"].(map[string]any)["value"] != "false" || !strings.HasSuffix(ws["path"].(string), filepath.Join("super", "mods", "lib")) {
		t.Fatalf("submodule checkout: %v", ws)
	}
	if c := run(1, super); c["status"] != "partial" {
		t.Fatalf("clean superproject: %v", c["status"])
	}
	os.WriteFile(filepath.Join(super, "s"), []byte("edited\n"), 0600)
	d := run(1, super)
	if d["status"] != "partial" || d["workspaces"].([]any)[0].(map[string]any)["tracked"].(map[string]any)["value"] != "true" {
		t.Fatalf("superproject with a modified ordinary file: %v", d["workspaces"])
	}
	if q := run(0, super, "--query", "dirty"); q["status"] != "ok" || q["query"].(map[string]any)["value"] != "true" {
		t.Fatalf("%v", q["query"])
	}
}

// The data-model example of Spec 027 validates against the unchanged schema.
func TestSpec027ExampleValidates(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root(t), "specs/027-inventory-limits/data-model.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "```json\n")
	end := strings.LastIndex(text, "```")
	if start < 0 || end <= start {
		t.Fatal("example not found")
	}
	valid(t, schema(t, "inventory"), []byte(text[start+len("```json\n"):end]))
}
