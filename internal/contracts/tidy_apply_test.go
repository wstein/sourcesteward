// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func TestRealTidyApplySchema(t *testing.T) {
	for _, backend := range []string{"git", "jj"} {
		t.Run(backend, func(t *testing.T) {
			if _, err := exec.LookPath(backend); err != nil {
				if backend == "jj" && os.Getenv("STW_REQUIRE_JJ") != "1" {
					t.Skip("jj unavailable")
				}
				t.Fatal(err)
			}
			home := t.TempDir()
			config := filepath.Join(home, "config.toml")
			if err := os.WriteFile(config, []byte("[user]\nname = \"Tester\"\nemail = \"test@example.org\"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", home)
			t.Setenv("JJ_CONFIG", config)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			for _, scenario := range []string{"success", "stale"} {
				t.Run(scenario, func(t *testing.T) {
					dir, err := filepath.EvalSymlinks(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					source := filepath.Join(dir, "source")
					state := filepath.Join(dir, "state")
					if err := os.Mkdir(state, 0700); err != nil {
						t.Fatal(err)
					}
					run := func(args ...string) {
						t.Helper()
						if backend == "git" {
							args = append([]string{"-c", "maintenance.auto=false", "-c", "gc.auto=0"}, args...)
						}
						cmd := exec.Command(backend, args...)
						if out, err := cmd.CombinedOutput(); err != nil {
							t.Fatalf("native fixture %q: %s %v", args, out, err)
						}
					}
					base, ref := "refs/heads/main", "refs/heads/integrated"
					if backend == "git" {
						run("init", "-q", "-b", "main", source)
						run("-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "initial")
						run("-C", source, "branch", "integrated")
						run("-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-m", "advance base")
					} else {
						base, ref = "base", "integrated"
						run("git", "init", "--no-colocate", source)
						if err := os.WriteFile(filepath.Join(source, "file"), []byte("initial\n"), 0600); err != nil {
							t.Fatal(err)
						}
						run("-R", source, "describe", "-m", "initial")
						run("-R", source, "bookmark", "create", "integrated")
						run("-R", source, "new")
						if err := os.WriteFile(filepath.Join(source, "file"), []byte("advance\n"), 0600); err != nil {
							t.Fatal(err)
						}
						run("-R", source, "describe", "-m", "advance")
						run("-R", source, "bookmark", "create", "base")
						run("-R", source, "new")
					}
					s := schema(t, "tidy")
					invoke := func(expected int, args ...string) workspace.TidyResult {
						t.Helper()
						var out, diagnostic bytes.Buffer
						args = append(args, "--json")
						code := cli.Run(context.Background(), "test", args, &out, &diagnostic)
						valid(t, s, out.Bytes())
						var r workspace.TidyResult
						if err := json.Unmarshal(out.Bytes(), &r); err != nil {
							t.Fatal(err)
						}
						if code != expected {
							for _, d := range r.Diagnostics {
								if strings.Contains(d.Code, "sandbox-unavailable") && os.Getenv("STW_REQUIRE_JJ") != "1" && os.Getenv("STW_REQUIRE_NATIVE_SANDBOX") != "1" {
									t.Skip("native sandbox unavailable")
								}
							}
							t.Fatalf("exit%d want%d %s %s", code, expected, out.String(), diagnostic.String())
						}
						return r
					}
					plan := invoke(0, "tidy", "plan", source, "--base", base, "--ref", ref, "--state-dir", state)
					if plan.Plan == nil || len(plan.Plan.Candidates) != 1 || plan.Plan.Candidates[0].Decision != "delete" {
						t.Fatalf("missing eligible stored plan: %+v", plan)
					}
					args := []string{"tidy", "apply", "--state-dir", state, "--plan", plan.Plan.PlanID}
					refused := invoke(1, args...)
					if refused.Status != "refused" {
						t.Fatal(refused.Status)
					}
					if scenario == "stale" {
						if backend == "git" {
							run("-C", source, "branch", "-f", "integrated", "HEAD")
						} else {
							run("-R", source, "bookmark", "set", "integrated", "-r", "base")
						}
						invoke(1, append(args, "--authorize")...)
						return
					}
					applied := invoke(0, append(args, "--authorize")...)
					if len(applied.Actions) == 0 {
						t.Fatal("missing applied action/recovery outcomes")
					}
					invoke(0, append(args, "--authorize")...)
				})
			}
		})
	}
}

func TestPublishedTidyV1ContractUnchanged(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(root(t), "schemas/v1/tidy.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if digest := fmt.Sprintf("%x", sha256.Sum256(raw)); digest != "7b32f169e81b5351c9b542af7b09025a068279de98f3441ec8966d1239ec1161" {
		t.Fatalf("published tidy v1 schema bytes changed: %s", digest)
	}
}
