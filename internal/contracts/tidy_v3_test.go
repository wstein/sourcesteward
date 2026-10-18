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

// Native CLI acceptance uses only exclusive temporary repositories. Setup and
// readback Git commands share the fixture's allowlisted environment and controls.
func TestTidyV3CLIRealRetirementFlow(t *testing.T) {
	binary := tidyV3FixtureGitBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprint(owned), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", home)
			env := tidyV3FixtureEnv(home, t.TempDir())
			git := func(args ...string) string {
				t.Helper()
				raw, err := tidyV3FixtureCommand(binary, env, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("fixture git %q: %s %v", args, raw, err)
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
			if err := os.Mkdir(filepath.Dir(completed), 0700); err != nil {
				t.Fatal(err)
			}
			put := func(path string, data []byte) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			git("init", "-q", "-b", "main", source)
			git("-C", source, "remote", "add", "origin", "https://github.com/owner/repo.git")
			put(filepath.Join(home, "ignore"), []byte("cache/\n"))
			git("-C", source, "config", "core.excludesfile", filepath.Join(home, "ignore"))
			for n := 0; n < 315; n++ {
				put(filepath.Join(source, fmt.Sprintf("%s-%04d.txt", strings.Repeat("x", 142), n)), []byte("tracked content\n"))
			}
			put(filepath.Join(source, "large.svg"), bytes.Repeat([]byte("s"), 301117))
			git("-C", source, "add", ".")
			git("-C", source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "base")
			invoke := func(ctx context.Context, want int, version, name string, args ...string) []byte {
				t.Helper()
				var out, diagnostic bytes.Buffer
				code := cli.Run(ctx, "test", append(args, "--json"), &out, &diagnostic)
				if code != want {
					t.Fatalf("code=%d want=%d args=%q JSON=%s diagnostic=%s", code, want, args, out.String(), diagnostic.String())
				}
				valid(t, schemaAt(t, version, name), out.Bytes())
				return append([]byte{}, out.Bytes()...)
			}
			v3 := func(ctx context.Context, want int, args ...string) workspace.V3Result {
				t.Helper()
				var r workspace.V3Result
				if err := json.Unmarshal(invoke(ctx, want, "v3", "tidy", args...), &r); err != nil {
					t.Fatal(err)
				}
				return r
			}
			ctx := context.Background()
			originals := map[string][]byte{}
			if owned {
				var creation workspace.Result
				raw := invoke(ctx, 0, "v1", "workspace", "workspace", "plan", source, "--base", "HEAD", "--destination", completed, "--task", "cli-owned", "--owner", "operator", "--state-dir", state)
				if err := json.Unmarshal(raw, &creation); err != nil || creation.Plan == nil {
					t.Fatalf("creation: %s %v", raw, err)
				}
				invoke(ctx, 0, "v1", "workspace", "workspace", "apply", "--plan", creation.Plan.PlanID, "--state-dir", state, "--authorize")
				for _, path := range []string{filepath.Join(state, "plans", creation.Plan.PlanID+".json"), filepath.Join(state, "operations", creation.Plan.PlanID+".json"), filepath.Join(state, "tasks", "cli-owned.json")} {
					data, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					originals[path] = data
				}
				git("-C", completed, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "normal detached task progress")
				git("-C", source, "merge", "--ff-only", git("-C", completed, "rev-parse", "HEAD"))
			} else {
				git("-C", source, "worktree", "add", "-q", "-b", "completed", completed, "HEAD")
			}
			git("-C", completed, "update-index", "--index-version=4")
			if len(git("-C", source, "ls-tree", "-r", "-l", "-z", "--full-tree", "HEAD")) <= 65536 {
				t.Fatal("fixture must cross streamed tree cap")
			}
			if info, err := os.Stat(filepath.Join(git("-C", completed, "rev-parse", "--absolute-git-dir"), "index")); err != nil || info.Size() > 65536 {
				t.Fatal("fixture index must retain original metadata cap", err)
			}
			if n := len(git("-C", completed, "ls-files", "--stage", "-z")); n > 65536 {
				t.Fatalf("fixture stage control exceeds old cap: %d", n)
			}
			current := git("-C", completed, "rev-parse", "HEAD")
			git("-C", source, "worktree", "add", "-q", "-b", "active", active, "HEAD")
			git("-C", active, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "independent active task")
			put(filepath.Join(active, "operator-document"), []byte("active work must survive\n"))
			protected := map[string][]byte{filepath.Join(source, "operator-plan"): []byte("primary user plan\n"), filepath.Join(source, "cache", "entry"): []byte("ignored cache\n"), filepath.Join(active, "cache", "entry"): []byte("active ignored cache\n")}
			for path, data := range protected {
				put(path, data)
			}
			activeBefore := tidyV3SentinelFiles(t, active)
			planArgs := []string{"tidy", "plan", source, "--contract", "3", "--base", "refs/heads/main", "--state-dir", state, "--owner", "operator", "--remove-registration", "--remove-files", completed}
			if owned {
				planArgs = append(planArgs, "--task", "cli-owned")
			} else {
				planArgs = append(planArgs, "--completed-worktree", completed, "--protect-worktree", active)
			}

			// The only credential executable is an inert private fixture: no
			// token is produced and no request can reach GitHub.
			tools, marker := filepath.Join(home, "tools"), filepath.Join(home, "auth-marker")
			put(filepath.Join(tools, "gh"), []byte("#!/bin/sh\nprintf called >> '"+strings.ReplaceAll(marker, "'", "'\"'\"'")+"'\nexit 1\n"))
			if err := os.Chmod(filepath.Join(tools, "gh"), 0700); err != nil {
				t.Fatal(err)
			}
			originalPath := os.Getenv("PATH")
			t.Setenv("PATH", tools+string(os.PathListSeparator)+originalPath)
			for _, probe := range []struct {
				name, hint string
				code       int
				auth       bool
			}{
				{"unrelated", strings.Repeat("e", 40) + "=1", 2, false},
				{"negative", current + "=-1", 2, false},
				{"overflow", current + "=9223372036854775808", 2, false},
				{"valid", current + "=1", 1, true},
			} {
				if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				var out, diagnostic bytes.Buffer
				args := append(append([]string{}, planArgs...), "--online", "--remote", "origin", "--pr", probe.hint, "--json")
				code := cli.Run(ctx, "test", args, &out, &diagnostic)
				_, authErr := os.Stat(marker)
				if code != probe.code || (authErr == nil) != probe.auth {
					t.Errorf("hint %s: code=%d want=%d auth=%t want=%t result=%s diagnostic=%s", probe.name, code, probe.code, authErr == nil, probe.auth, out.String(), diagnostic.String())
				}
				if out.Len() > 0 {
					valid(t, schemaAt(t, "v3", "tidy"), out.Bytes())
					var result workspace.V3Result
					if err := json.Unmarshal(out.Bytes(), &result); err != nil {
						t.Fatal(err)
					}
					if result.Plan != nil || result.Boundary.NetworkAttempted {
						t.Fatalf("inert hint probe acquired effect authority: %+v", result)
					}
					if probe.name == "unrelated" && (len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "invalid-options") {
						t.Fatalf("unrelated hint lost syntax classification: %+v", result.Diagnostics)
					}
				}
			}
			t.Setenv("PATH", originalPath)
			planned := v3(ctx, 0, planArgs...)
			if planned.Plan == nil || planned.Plan.Retirement == nil || planned.Plan.Retirement.Target.CommitID != current {
				t.Fatalf("missing selected current retirement: %+v", planned)
			}
			p := planned.Plan
			apply := []string{"tidy", "apply", "--contract", "3", "--state-dir", state, "--plan", p.PlanID}
			if !owned {
				apply = append(apply, "--completed-worktree", completed, "--owner", "operator", "--protect-worktree", active)
			}
			v3(ctx, 1, apply...)
			authorized := append(append([]string{}, apply...), "--authorize")
			if r := v3(ctx, 1, append(append([]string{}, authorized...), "--online")...); r.Boundary.NetworkAttempted {
				t.Fatal("mismatched online acknowledgment reached network")
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			var canceledOut, canceledErr bytes.Buffer
			if code := cli.Run(canceled, "test", append(append([]string{}, authorized...), "--json"), &canceledOut, &canceledErr); code != 1 {
				t.Fatalf("canceled command exit=%d", code)
			}
			// Tampering with the immutable plan refuses before recovery or native writes.
			planPath := filepath.Join(state, "tidy-v3-plans", p.PlanID+".json")
			planBytes, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			changed := bytes.Replace(planBytes, []byte(current), []byte(strings.Repeat("f", 40)), 1)
			if bytes.Equal(changed, planBytes) {
				t.Fatal("tamper fixture did not alter selected object")
			}
			put(planPath, changed)
			before := tidyV3SentinelFiles(t, completed)
			v3(ctx, 1, authorized...)
			if !reflect.DeepEqual(before, tidyV3SentinelFiles(t, completed)) {
				t.Fatal("tampered plan changed selected files")
			}
			put(planPath, planBytes)
			// A late file invalidates the immutable observation without removal.
			late := filepath.Join(completed, "late-actor-document")
			put(late, []byte("late actor bytes\n"))
			v3(ctx, 1, authorized...)
			if got, err := os.ReadFile(late); err != nil || string(got) != "late actor bytes\n" {
				t.Fatal("stale apply changed late actor data")
			}
			if err := os.Remove(late); err != nil {
				t.Fatal(err)
			}
			// Removing the late file does not revive old physical timestamps. A
			// fresh plan is required after the stale refusal, even if bytes match.
			fresh := v3(ctx, 0, planArgs...)
			if fresh.Plan == nil {
				t.Fatal("fresh plan unavailable", fresh.Diagnostics)
			}
			for i, arg := range authorized {
				if arg == p.PlanID {
					authorized[i] = fresh.Plan.PlanID
				}
			}
			p = fresh.Plan
			planPath = filepath.Join(state, "tidy-v3-plans", p.PlanID+".json")
			planBytes, err = os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			result := v3(ctx, 0, authorized...)
			if !result.Boundary.Complete || len(result.Actions) == 0 || result.Plan == nil {
				t.Fatalf("incomplete removal: %+v", result)
			}
			if _, err := os.Lstat(completed); !os.IsNotExist(err) {
				t.Fatalf("completed worktree remains: %v", err)
			}
			if got, err := os.ReadFile(planPath); err != nil || !bytes.Equal(got, planBytes) {
				t.Fatal("apply rewrote immutable plan")
			}
			stateBefore := tidyV3SentinelFiles(t, state)
			refsBefore := git("-C", source, "for-each-ref", "--format=%(refname) %(objectname)")
			v3(ctx, 0, authorized...)
			if !reflect.DeepEqual(stateBefore, tidyV3SentinelFiles(t, state)) || refsBefore != git("-C", source, "for-each-ref", "--format=%(refname) %(objectname)") {
				t.Fatal("completed retry changed state or references")
			}
			if !strings.Contains(refsBefore, "refs/stw/recovery/") || !strings.Contains(refsBefore, current) {
				t.Fatal("current committed history has no recovery pin")
			}
			for path, want := range originals {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("original ownership record changed: %s", path)
				}
			}
			if !reflect.DeepEqual(activeBefore, tidyV3SentinelFiles(t, active)) {
				t.Fatal("active workspace bytes changed")
			}
			for path, want := range protected {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("protected primary/active bytes changed: %s", path)
				}
			}
			if !owned {
				if git("-C", source, "rev-parse", "refs/heads/completed") != current {
					t.Fatal("retirement removed attached branch")
				}
				retained := v3(ctx, 0, "tidy", "plan", source, "--contract", "3", "--base", "refs/heads/main", "--ref", "refs/heads/completed", "--state-dir", state)
				if retained.Plan == nil || len(retained.Plan.Candidates) != 1 || retained.Plan.Candidates[0].Decision != "retain" || retained.Plan.Candidates[0].Reason != "selected-base" {
					t.Fatal("exact base OID lost protection", retained)
				}
				git("-C", source, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-qm", "advance protected base")
				next := v3(ctx, 0, "tidy", "plan", source, "--contract", "3", "--base", "refs/heads/main", "--ref", "refs/heads/completed", "--state-dir", state)
				if next.Plan == nil || len(next.Plan.Candidates) != 1 || next.Plan.Candidates[0].Decision != "delete" {
					t.Fatalf("fresh post-retirement ref plan unavailable: %+v candidates=%+v", next, next.Plan)
				}
				v3(ctx, 0, "tidy", "apply", "--contract", "3", "--plan", next.Plan.PlanID, "--state-dir", state, "--authorize")
				if err := tidyV3FixtureCommand(binary, env, "-C", source, "show-ref", "--verify", "--quiet", "refs/heads/completed").Run(); err == nil {
					t.Fatal("fresh selected branch survived ref apply")
				}
			}
		})
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
			child := exec.Command(executable, "-test.run=^TestTidyV3CLIReal(MetadataReference|Retirement)Flow$", "-test.count=1", "-test.v")
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
			child := exec.Command(executable, "-test.run=^TestTidyV3CLIReal(MetadataReference|Retirement)Flow$", "-test.count=1", "-test.v")
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
