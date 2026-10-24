// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// Spec 041 WP-1: retirement reuses the Spec 011 forge-free proofs, so the plan
// and the apply result of a patch-id or no-net-change retirement carry the
// integration evidence arm, which the published v3 schema must admit.
func TestTidyV3RetirementWithForgeFreeProofValidatesAgainstSchema(t *testing.T) {
	binary := tidyV3FixtureGitBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, class := range []string{"patch-id", "no-net-change"} {
		t.Run(class, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", home)
			env := tidyV3FixtureEnv(home, t.TempDir())
			git := func(args ...string) string {
				t.Helper()
				raw, err := tidyV3FixtureCommand(binary, env, append([]string{"-c", "user.name=Tester", "-c", "user.email=test@example.org"}, args...)...).CombinedOutput()
				if err != nil {
					t.Fatalf("git %q: %s %v", args, raw, err)
				}
				return strings.TrimSpace(string(raw))
			}
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			source, state, completed, active := filepath.Join(root, "source"), filepath.Join(t.TempDir(), "state"), filepath.Join(root, "tasks", "completed"), filepath.Join(root, "tasks", "active")
			if err := os.Mkdir(state, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(completed), 0700); err != nil {
				t.Fatal(err)
			}
			write := func(dir, name, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			git("init", "-q", "-b", "main", source)
			write(source, "file", "stored\n")
			git("-C", source, "add", ".")
			git("-C", source, "commit", "-qm", "base")
			git("-C", source, "worktree", "add", "-q", "-b", "completed", completed, "HEAD")
			git("-C", source, "worktree", "add", "-q", "-b", "active", active, "HEAD")
			write(completed, "work.txt", "work\n")
			git("-C", completed, "add", "work.txt")
			git("-C", completed, "commit", "-qm", "work")
			if class == "patch-id" {
				git("-C", source, "cherry-pick", "-n", "completed")
				git("-C", source, "commit", "-qm", "work (rebase-merged)")
			} else {
				git("-C", completed, "rm", "-q", "work.txt")
				git("-C", completed, "commit", "-qm", "remove the work again")
			}
			ctx := context.Background()
			run := func(args ...string) workspace.V3Result {
				t.Helper()
				var out, diagnostic bytes.Buffer
				if code := cli.Run(ctx, "test", append(args, "--json"), &out, &diagnostic); code != 0 {
					t.Fatalf("code=%d args=%q JSON=%s diagnostic=%s", code, args, out.String(), diagnostic.String())
				}
				valid(t, schemaAt(t, "v3", "tidy"), out.Bytes())
				var r workspace.V3Result
				if err := json.Unmarshal(out.Bytes(), &r); err != nil {
					t.Fatal(err)
				}
				return r
			}
			planned := run("tidy", "plan", source, "--contract", "3", "--base", "refs/heads/main", "--state-dir", state, "--owner", "operator", "--remove-registration", "--remove-files", completed, "--completed-worktree", completed, "--protect-worktree", active)
			if planned.Plan == nil || planned.Plan.Retirement == nil || planned.Plan.Retirement.Integration.Proof != class || planned.Plan.Retirement.Integration.Decision != "delete" {
				t.Fatalf("not proven by %s: %+v", class, planned.Plan)
			}
			for name, mutate := range map[string]func(map[string]any){
				"proof without evidence":         func(i map[string]any) { i["evidence"] = nil },
				"retained with a deleting proof": func(i map[string]any) { i["decision"] = "retain" },
				"native arm with a forge-free proof": func(i map[string]any) {
					i["evidence"] = map[string]any{"native": map[string]any{"target_oid": i["oid"], "base_oid": i["oid"]}, "github": nil, "absence": nil}
				},
			} {
				raw, err := json.Marshal(planned)
				if err != nil {
					t.Fatal(err)
				}
				var doc map[string]any
				if err := json.Unmarshal(raw, &doc); err != nil {
					t.Fatal(err)
				}
				mutate(doc["plan"].(map[string]any)["retirement"].(map[string]any)["integration"].(map[string]any))
				broken, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				if schemaAt(t, "v3", "tidy").Validate(value(t, broken)) == nil {
					t.Errorf("schema admitted a forged retirement proof: %s", name)
				}
			}
			applied := run("tidy", "apply", "--contract", "3", "--plan", planned.Plan.PlanID, "--state-dir", state, "--authorize", "--completed-worktree", completed, "--owner", "operator", "--protect-worktree", active)
			if applied.Status != "ok" {
				t.Fatalf("apply: %+v", applied.Diagnostics)
			}
			if _, err := os.Lstat(completed); !os.IsNotExist(err) {
				t.Fatal("worktree not retired")
			}
		})
	}
}
