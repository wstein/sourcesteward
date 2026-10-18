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
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// Exercise the real CLI boundary, secure store and native prepared transaction.
// The full base exceeds the old 64 KiB tree channel, even with one selected ref.
func TestTidyV3CLIRealMetadataReferenceFlow(t *testing.T) {
	binary := tidyV3FixtureGitBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	env := tidyV3FixtureEnv(home, t.TempDir())
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, state := filepath.Join(parent, "source"), filepath.Join(parent, "state")
	if err := os.Mkdir(state, 0700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		raw, err := tidyV3FixtureCommand(binary, env, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %q: %s %v", args, raw, err)
		}
		return strings.TrimSpace(string(raw))
	}
	git("init", "-q", "-b", "main", source)
	for n := 0; n < 695; n++ {
		name := fmt.Sprintf("%04d-%s.txt", n, strings.Repeat("x", 50))
		if err := os.WriteFile(filepath.Join(source, name), []byte("complete base content\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("-C", source, "add", ".")
	git("-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "-qm", "base")
	if metadata := git("-C", source, "ls-tree", "-r", "-l", "-z", "--full-tree", "HEAD"); len(metadata) <= 65536 {
		t.Fatalf("fixture did not cross the original metadata cap: %d", len(metadata))
	}
	git("-C", source, "branch", "integrated")
	git("-C", source, "branch", "stale")
	git("-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-qm", "advance")
	// Dirty primary user data is outside the declared reference effects.
	protected := map[string][]byte{"operator-note": []byte("preserve this document\n"), "cache/entry": []byte("preserve ignored cache\n")}
	git("-C", source, "config", "core.excludesfile", filepath.Join(home, "ignore"))
	if err := os.WriteFile(filepath.Join(home, "ignore"), []byte("cache/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for path, data := range protected {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(source, path)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := schemaAt(t, "v3", "tidy")
	invoke := func(want int, args ...string) workspace.V3Result {
		t.Helper()
		var out, diagnostic bytes.Buffer
		code := cli.Run(context.Background(), "test", append(args, "--json"), &out, &diagnostic)
		if code != want {
			t.Fatalf("code=%d want=%d args=%q JSON=%s diagnostic=%s", code, want, args, out.String(), diagnostic.String())
		}
		valid(t, s, out.Bytes())
		var r workspace.V3Result
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	// Online proof cannot be attempted for a hint unrelated to selected heads.
	invalid := invoke(2, "tidy", "plan", source, "--contract", "3", "--base", "refs/heads/main", "--ref", "refs/heads/integrated", "--online", "--remote", "origin", "--pr", strings.Repeat("b", 40)+"=42")
	if invalid.Plan != nil || invalid.Boundary.NetworkAttempted {
		t.Fatalf("invalid hints obtained authority: %+v", invalid)
	}
	for _, ref := range []string{"integrated", "stale"} {
		plan := invoke(0, "tidy", "plan", source, "--contract", "3", "--base", "refs/heads/main", "--ref", "refs/heads/"+ref, "--state-dir", state)
		if plan.Plan == nil || len(plan.Plan.Candidates) != 1 || plan.Plan.Candidates[0].Decision != "delete" || plan.Boundary.NetworkAttempted {
			t.Fatalf("%+v", plan)
		}
		args := []string{"tidy", "apply", "--contract", "3", "--plan", plan.Plan.PlanID, "--state-dir", state}
		invoke(1, args...)
		mismatch := invoke(1, append(args, "--authorize", "--online")...)
		if mismatch.Boundary.NetworkAttempted || mismatch.Plan != nil {
			t.Fatalf("unacknowledged online effect: %+v", mismatch)
		}
		if ref == "stale" {
			git("-C", source, "branch", "-f", ref, "HEAD")
			refused := invoke(1, append(args, "--authorize")...)
			if refused.Plan != nil {
				t.Fatal("stale plan remained executable")
			}
			continue
		}
		applied := invoke(0, append(args, "--authorize")...)
		if len(applied.Actions) == 0 || !applied.Boundary.Complete {
			t.Fatalf("%+v", applied)
		}
		if err := tidyV3FixtureCommand(binary, env, "-C", source, "show-ref", "--verify", "--quiet", "refs/heads/"+ref).Run(); err == nil {
			t.Fatal("selected reference survived successful apply")
		}
		refs := git("-C", source, "for-each-ref", "--format=%(refname) %(objectname)")
		if !strings.Contains(refs, plan.Plan.Candidates[0].OID) || !strings.Contains(refs, "refs/stw/recovery/") {
			t.Fatalf("missing exact committed recovery: %s", refs)
		}
		journal := filepath.Join(state, "tidy-v3-operations", plan.Plan.PlanID+".json")
		before, err := os.ReadFile(journal)
		if err != nil {
			t.Fatal(err)
		}
		invoke(0, append(args, "--authorize")...)
		after, err := os.ReadFile(journal)
		if err != nil || !bytes.Equal(before, after) || git("-C", source, "for-each-ref", "--format=%(refname) %(objectname)") != refs {
			t.Fatal("completed retry changed native refs or journal")
		}
	}
	for path, want := range protected {
		got, err := os.ReadFile(filepath.Join(source, path))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("protected primary data changed at %s: %v", path, err)
		}
	}
}

func tidyV3FixtureGitBinary(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("git")
	if err != nil {
		if os.Getenv("STW_REQUIRE_NATIVE_SANDBOX") == "1" || os.Getenv("STW_REQUIRE_JJ") == "1" {
			t.Fatalf("mandatory native Git unavailable: %v", err)
		}
		t.Skip("native Git unavailable; optional local fixture skipped, not native acceptance")
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal(err)
	}
	return binary
}

// Match the harness's isolated native controls; do not inherit repository,
// index, config, templates, hooks or dynamic-loader routing from the caller.
func tidyV3FixtureEnv(home, templates string) []string {
	return []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home, "TMPDIR=" + home, "LANG=C", "LC_ALL=C", "TZ=UTC", "TERM=dumb", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_SYSTEM=" + os.DevNull, "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TEMPLATE_DIR=" + templates, "GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat"}
}
func tidyV3FixtureCommand(binary string, env []string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary, append([]string{"-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "core.fsmonitor=false", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Env = append([]string{}, env...)
	return cmd
}
func tidyV3SentinelFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files[strings.TrimPrefix(path, root+string(os.PathSeparator))] = fmt.Sprintf("%v:%x", info.Mode(), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
func TestTidyV3ContractFixtureGitEnvironmentIsolation(t *testing.T) {
	binary := tidyV3FixtureGitBinary(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, routing := range []bool{true, false} {
		t.Run(fmt.Sprint(routing), func(t *testing.T) {
			root := t.TempDir()
			home, templates, sentinel, hooks := filepath.Join(root, "home"), filepath.Join(root, "templates"), filepath.Join(root, "sentinel"), filepath.Join(root, "hooks")
			for _, path := range []string{home, templates, hooks} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			env := tidyV3FixtureEnv(home, templates)
			native := func(args ...string) {
				t.Helper()
				out, err := tidyV3FixtureCommand(binary, env, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("sentinel git: %s %v", out, err)
				}
			}
			native("init", "-q", "-b", "sentinel", sentinel)
			if err := os.WriteFile(filepath.Join(sentinel, "tracked"), []byte("committed sentinel\n"), 0600); err != nil {
				t.Fatal(err)
			}
			native("-C", sentinel, "add", "tracked")
			native("-C", sentinel, "-c", "user.name=Sentinel", "-c", "user.email=sentinel@example.invalid", "commit", "-qm", "sentinel")
			if err := os.WriteFile(filepath.Join(sentinel, "tracked"), []byte("dirty sentinel must remain uncommitted\n"), 0600); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(hooks, "invoked")
			hook := "#!/bin/sh\nprintf invoked >> '" + marker + "'\n"
			if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte(hook), 0700); err != nil {
				t.Fatal(err)
			}
			injectedTemplate := filepath.Join(root, "injected-template")
			if err := os.MkdirAll(filepath.Join(injectedTemplate, "hooks"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(injectedTemplate, "hooks", "pre-commit"), []byte(hook), 0700); err != nil {
				t.Fatal(err)
			}
			injectedConfig := filepath.Join(root, "injected-config")
			config := []byte("[core]\n\thooksPath = " + hooks + "\n")
			if err := os.WriteFile(injectedConfig, config, 0600); err != nil {
				t.Fatal(err)
			}
			before := tidyV3SentinelFiles(t, sentinel)
			hooksBefore := tidyV3SentinelFiles(t, hooks)
			child := exec.Command(executable, "-test.run=^TestTidyV3CLIRealMetadataReferenceFlow$", "-test.count=1", "-test.v")
			child.Env = append(append([]string{}, env...), "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0="+hooks, "GIT_CONFIG_GLOBAL="+injectedConfig, "GIT_CONFIG_SYSTEM="+injectedConfig, "GIT_TEMPLATE_DIR="+injectedTemplate)
			if routing {
				child.Env = append(child.Env, "GIT_DIR="+filepath.Join(sentinel, ".git"), "GIT_WORK_TREE="+sentinel, "GIT_INDEX_FILE="+filepath.Join(sentinel, ".git", "alternate-index"))
			}
			out, runErr := child.CombinedOutput()
			if !reflect.DeepEqual(before, tidyV3SentinelFiles(t, sentinel)) || !reflect.DeepEqual(hooksBefore, tidyV3SentinelFiles(t, hooks)) {
				t.Errorf("fixture changed external dummy repository/index/config/hooks; output=%s", out)
			}
			after, err := os.ReadFile(injectedConfig)
			if err != nil || !bytes.Equal(after, config) {
				t.Errorf("fixture changed injected config: %v", err)
			}
			if runErr != nil {
				t.Fatalf("isolated fixture failed under injected controls: %s %v", out, runErr)
			}
		})
	}
}

func TestTidyV3ContractFixtureMissingGitClassification(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []bool{false, true} {
		t.Run(fmt.Sprint(required), func(t *testing.T) {
			home := t.TempDir()
			env := tidyV3FixtureEnv(home, t.TempDir())
			for i, item := range env {
				if strings.HasPrefix(item, "PATH=") {
					env[i] = "PATH=" + t.TempDir()
				}
			}
			if required {
				env = append(env, "STW_REQUIRE_NATIVE_SANDBOX=1")
			}
			child := exec.Command(executable, "-test.run=^TestTidyV3CLIRealMetadataReferenceFlow$", "-test.count=1", "-test.v")
			child.Env = env
			out, err := child.CombinedOutput()
			if required {
				if err == nil || !bytes.Contains(out, []byte("mandatory native Git unavailable")) {
					t.Fatalf("mandatory missing Git was not a failure: %s %v", out, err)
				}
			} else if err != nil || !bytes.Contains(out, []byte("--- SKIP:")) {
				t.Fatalf("optional missing Git not classified as skipped gap: %s %v", out, err)
			}
		})
	}
}
