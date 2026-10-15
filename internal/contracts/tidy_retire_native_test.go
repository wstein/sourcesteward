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

func TestRealOwnedRetirementSchema(t *testing.T) {
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
			for _, dirty := range []bool{false, true} {
				t.Run(map[bool]string{false: "clean", true: "dirty"}[dirty], func(t *testing.T) {
					dir, err := filepath.EvalSymlinks(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					source := filepath.Join(dir, "source")
					dest := filepath.Join(dir, "task")
					state := filepath.Join(dir, "state")
					if err := os.Mkdir(state, 0700); err != nil {
						t.Fatal(err)
					}
					native := func(args ...string) {
						t.Helper()
						if backend == "git" {
							args = append([]string{"-c", "maintenance.auto=false", "-c", "gc.auto=0"}, args...)
						}
						if out, err := exec.Command(backend, args...).CombinedOutput(); err != nil {
							t.Fatalf("native fixture %q %s %v", args, out, err)
						}
					}
					base := "refs/heads/main"
					if backend == "git" {
						native("init", "-q", "-b", "main", source)
					} else {
						base = "base"
						native("git", "init", "--no-colocate", source)
					}
					if err := os.WriteFile(filepath.Join(source, "file"), []byte("owned content\n"), 0600); err != nil {
						t.Fatal(err)
					}
					if backend == "git" {
						native("-C", source, "add", "file")
						native("-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "-m", "initial")
					} else {
						native("-R", source, "describe", "-m", "initial")
						native("-R", source, "bookmark", "create", "base")
					}
					invoke := func(expected int, version, name string, args ...string) []byte {
						t.Helper()
						var out, diagnostic bytes.Buffer
						args = append(args, "--json")
						code := cli.Run(context.Background(), "test", args, &out, &diagnostic)
						valid(t, schemaAt(t, version, name), out.Bytes())
						if code != expected {
							if strings.Contains(out.String(), "sandbox-unavailable") && os.Getenv("STW_REQUIRE_JJ") != "1" && os.Getenv("STW_REQUIRE_NATIVE_SANDBOX") != "1" {
								t.Skip("native sandbox unavailable")
							}
							t.Fatalf("exit%d want%d %s %s", code, expected, out.String(), diagnostic.String())
						}
						return out.Bytes()
					}
					raw := invoke(0, "v1", "workspace", "workspace", "plan", source, "--base", base, "--destination", dest, "--task", "owned-task", "--owner", "tester", "--state-dir", state)
					var creation workspace.Result
					if err := json.Unmarshal(raw, &creation); err != nil {
						t.Fatal(err)
					}
					invoke(0, "v1", "workspace", "workspace", "apply", "--plan", creation.Plan.PlanID, "--state-dir", state, "--authorize")
					if dirty {
						if err := os.WriteFile(filepath.Join(dest, "file"), []byte("uncommitted work\n"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					expected := 0
					if dirty {
						expected = 1
					}
					raw = invoke(expected, "v2", "tidy", "tidy", "plan", source, "--base", base, "--task", "owned-task", "--owner", "tester", "--state-dir", state, "--remove-registration", "--remove-files", dest)
					if dirty {
						return
					}
					var plan workspace.TidyResult
					if err := json.Unmarshal(raw, &plan); err != nil {
						t.Fatal(err)
					}
					if plan.Retirement == nil {
						t.Fatal("missing owned retirement plan")
					}
					args := []string{"tidy", "apply", "--plan", plan.Retirement.PlanID, "--state-dir", state}
					invoke(1, "v2", "tidy", args...)
					invoke(0, "v2", "tidy", append(args, "--authorize")...)
					invoke(0, "v2", "tidy", append(args, "--authorize")...)
					if _, err := os.Stat(dest); !os.IsNotExist(err) {
						t.Fatalf("authorized owned destination remains: %v", err)
					}
				})
			}
		})
	}
}
