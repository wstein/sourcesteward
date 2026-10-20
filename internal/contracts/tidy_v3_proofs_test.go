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

// Spec 031 T019: the forge-free proofs through the real CLI, offline, with no
// state directory, against the v3 candidate schema.
func TestTidyV3ForgeFreeProofsOfflineValidateAgainstSchema(t *testing.T) {
	binary := tidyV3FixtureGitBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	env := tidyV3FixtureEnv(home, t.TempDir())
	// Host-wide drivers and filters (git-lfs installs them system wide) are
	// invisible under the planning environment and must not block a proof.
	hostConfig := "[merge \"host\"]\n\tdriver = true\n[filter \"lfs\"]\n\tclean = git-lfs clean -- %f\n\tsmudge = git-lfs smudge -- %f\n\tprocess = git-lfs filter-process\n"
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(hostConfig), 0600); err != nil {
		t.Fatal(err)
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(parent, "source")
	git := func(args ...string) string {
		t.Helper()
		raw, err := tidyV3FixtureCommand(binary, env, append([]string{"-c", "user.name=Tester", "-c", "user.email=test@example.org"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %q: %s %v", args, raw, err)
		}
		return strings.TrimSpace(string(raw))
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(source, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string, files map[string]string) string {
		t.Helper()
		for name, content := range files {
			write(name, content)
		}
		git("-C", source, "add", "-A")
		git("-C", source, "commit", "-qm", msg)
		return git("-C", source, "rev-parse", "HEAD")
	}
	git("init", "-q", "-b", "main", source)
	m0 := commit("base", map[string]string{"file": "stored\n"})
	git("-C", source, "branch", "ancestor", m0)
	git("-C", source, "checkout", "-qb", "rebased", m0)
	r1 := commit("r1", map[string]string{"r1.txt": "r1\n"})
	git("-C", source, "checkout", "-q", "main")
	git("-C", source, "checkout", "-qb", "squashed", m0)
	commit("s1", map[string]string{"s1.txt": "s1\n"})
	commit("s2", map[string]string{"s2.txt": "s2\n"})
	git("-C", source, "checkout", "-q", "main")
	git("-C", source, "checkout", "-qb", "wip", m0)
	commit("wip", map[string]string{"w.txt": "wip\n"})
	git("-C", source, "revert", "--no-edit", "HEAD")
	git("-C", source, "checkout", "-q", "main")
	git("-C", source, "checkout", "-qb", "fresh", m0)
	commit("fresh", map[string]string{"fresh.txt": "fresh\n"})
	git("-C", source, "checkout", "-q", "main")
	commit("unrelated", map[string]string{"u.txt": "u\n"})
	git("-C", source, "cherry-pick", r1)
	commit("squash landed", map[string]string{"s1.txt": "s1\n", "s2.txt": "s2\n"})

	s := schemaAt(t, "v3", "tidy")
	run := func(extra ...string) (workspace.V3Result, []byte, string) {
		t.Helper()
		args := append([]string{"tidy", "plan", source, "--contract", "3", "--base", "refs/heads/main"}, extra...)
		var out, diagnostic bytes.Buffer
		if code := cli.Run(context.Background(), "test", append(args, "--json"), &out, &diagnostic); code != 0 {
			t.Fatalf("code=%d JSON=%s diagnostic=%s", code, out.String(), diagnostic.String())
		}
		valid(t, s, out.Bytes())
		var r workspace.V3Result
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		var human, humanDiagnostic bytes.Buffer
		if code := cli.Run(context.Background(), "test", args, &human, &humanDiagnostic); code != 0 {
			t.Fatalf("human code=%d %s", code, humanDiagnostic.String())
		}
		return r, out.Bytes(), human.String()
	}
	r, raw, human := run()
	if r.Boundary.NetworkAttempted || r.Plan == nil || r.Plan.StateDir != "" || r.Plan.ProofPolicy == nil || strings.Join(r.Plan.ProofPolicy.Kinds, ",") != "ancestor,patch-id,merge-noop,no-net-change" {
		t.Fatalf("offline stateless plan: network=%t plan=%+v", r.Boundary.NetworkAttempted, r.Plan)
	}
	want := map[string]string{"ancestor": "native-ancestry", "rebased": "patch-id", "squashed": "merge-noop", "wip": "no-net-change", "fresh": "needs-review"}
	mismatched := false
	for _, c := range r.Plan.Candidates {
		name := strings.TrimPrefix(c.Name, "refs/heads/")
		if name == "main" {
			continue
		}
		if c.Reason != want[name] {
			evidence, _ := json.Marshal(c.Evidence)
			t.Errorf("%s: reason %q, want %q (proof %q, evidence %s)", name, c.Reason, want[name], c.Proof, evidence)
			mismatched = true
		}
		if c.Evidence != nil && c.Evidence.Integration != nil {
			e := c.Evidence.Integration
			for _, fact := range append(append([]string{e.Disposition, e.Class}, e.Reasons...), e.Flags...) {
				if fact != "" && !strings.Contains(human, fact) {
					t.Errorf("%s: fact %q missing from the human output", name, fact)
				}
			}
		}
	}
	if mismatched {
		// Name the configuration source: what the admission read sees (host
		// system and global files included) and what the planning environment
		// leaves in effect.
		admission := tidyV3FixtureCommand(binary, nil, "-C", source, "config", "--show-origin", "--list")
		admission.Env = withoutEnv(withoutEnv(os.Environ(), "GIT_CONFIG_NOSYSTEM"), "GIT_CONFIG_GLOBAL")
		out, _ := admission.CombinedOutput()
		t.Logf("git config --show-origin --list (admission read):\n%s", out)
		planning := tidyV3FixtureCommand(binary, env, "-C", source, "config", "--show-origin", "--list")
		out, _ = planning.CombinedOutput()
		t.Logf("git config --show-origin --list (planning environment):\n%s", out)
		t.Fatal("the scenario did not produce every class; the forged-evidence cases below need a proven candidate of each")
	}
	// A narrowed, reordered selection is canonical in the plan.
	narrow, _, _ := run("--integration-proof", "no-net-change,patch-id", "--accept-whitespace-differences")
	if strings.Join(narrow.Plan.ProofPolicy.Kinds, ",") != "patch-id,no-net-change" || !narrow.Plan.ProofPolicy.AcceptWhitespaceDifferences {
		t.Fatalf("narrowed policy %+v", narrow.Plan.ProofPolicy)
	}
	// The closed candidate schema refuses forged proof evidence.
	forge := map[string]func(candidates []any){
		"unknown class":          func(c []any) { integrationOf(c, "rebased")["class"] = "cherry-pick" },
		"class and strength":     func(c []any) { integrationOf(c, "rebased")["strength"] = "content-present" },
		"proven with reasons":    func(c []any) { integrationOf(c, "rebased")["reasons"] = []any{"conflict"} },
		"unknown reason":         func(c []any) { integrationOf(c, "fresh")["reasons"] = []any{"looks-fine"} },
		"review without reasons": func(c []any) { integrationOf(c, "fresh")["reasons"] = []any{} },
		"review with a class":    func(c []any) { integrationOf(c, "fresh")["class"] = "patch-id" },
		"extra field":            func(c []any) { integrationOf(c, "rebased")["note"] = "trust me" },
		"negative count":         func(c []any) { integrationOf(c, "rebased")["matched"] = -1 },
		"merge-noop with flags":  func(c []any) { integrationOf(c, "squashed")["flags"] = []any{"whitespace-differs"} },
		"unknown proof":          func(c []any) { candidate(c, "rebased")["proof"] = "patch-similar" },
		"delete with none":       func(c []any) { candidate(c, "rebased")["proof"] = "none" },
		"policy kinds":           nil,
	}
	for name, mutate := range forge {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			plan := v["plan"].(map[string]any)
			if mutate == nil {
				plan["proof_policy"].(map[string]any)["kinds"] = []any{"cherry-pick"}
			} else {
				mutate(plan["candidates"].([]any))
			}
			if s.Validate(v) == nil {
				t.Fatal("forged evidence admitted")
			}
		})
	}
}

func candidate(candidates []any, name string) map[string]any {
	for _, c := range candidates {
		if m := c.(map[string]any); m["name"] == "refs/heads/"+name {
			return m
		}
	}
	panic("no candidate " + name)
}

func integrationOf(candidates []any, name string) map[string]any {
	return candidate(candidates, name)["evidence"].(map[string]any)["integration"].(map[string]any)
}

func withoutEnv(env []string, key string) []string {
	out := []string{}
	for _, item := range env {
		if !strings.HasPrefix(item, key+"=") {
			out = append(out, item)
		}
	}
	return out
}
