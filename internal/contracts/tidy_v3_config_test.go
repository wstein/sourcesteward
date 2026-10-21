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

// Issue #79 through the real CLI: a repository carrying hooksPath and submodule
// configuration plans, the plan records neutralization notes that validate
// against the v3 candidate schema, and apply refuses the hook-running effect.
func TestTidyV3PlanNotesNeutralizedConfigurationAndApplyAdmits(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(source, "file"), []byte("stored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("-C", source, "add", ".")
	git("-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "-qm", "base")
	git("-C", source, "branch", "integrated")
	git("-C", source, "-c", "user.name=Tester", "-c", "user.email=test@example.org", "commit", "--allow-empty", "-qm", "advance")
	hooks := filepath.Join(parent, "hooks")
	if err := os.Mkdir(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	git("-C", source, "config", "core.hooksPath", hooks)
	git("-C", source, "config", "submodule.member.url", "https://example.invalid/member.git")
	s := schemaAt(t, "v3", "tidy")
	invoke := func(want int, args ...string) (workspace.V3Result, []byte) {
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
		return r, out.Bytes()
	}
	plan, raw := invoke(0, "tidy", "plan", source, "--contract", "3", "--base", "refs/heads/main", "--ref", "refs/heads/integrated", "--state-dir", state)
	if plan.Plan == nil || len(plan.Plan.Candidates) != 1 || plan.Plan.Candidates[0].Decision != "delete" {
		t.Fatalf("%+v", plan)
	}
	classes := map[string]string{}
	for _, n := range plan.Plan.ConfigNotes {
		classes[n.KeyClass] = n.Disposition + "/" + n.ApplyEffect
	}
	if classes["core.hooksPath"] != "neutralized/none" || classes["submodule.*"] != "neutralized/none" {
		t.Fatalf("notes %v (system or global configuration may add further classes)", classes)
	}
	// The v3 candidate schema is closed: a forged class or disposition fails.
	for name, mutate := range map[string]func(map[string]any){
		"unknown-class":       func(n map[string]any) { n["key_class"] = "core.other" },
		"unknown-disposition": func(n map[string]any) { n["disposition"] = "ignored" },
		"short-digest":        func(n map[string]any) { n["digest"] = "abc" },
		"extra-field":         func(n map[string]any) { n["value"] = "/etc/hooks" },
	} {
		t.Run(name, func(t *testing.T) {
			v := value(t, raw).(map[string]any)
			note := v["plan"].(map[string]any)["config_notes"].([]any)[0].(map[string]any)
			mutate(note)
			if s.Validate(v) == nil {
				t.Fatal("invalid configuration note admitted")
			}
		})
	}
	applied, _ := invoke(0, "tidy", "apply", "--contract", "3", "--plan", plan.Plan.PlanID, "--state-dir", state, "--authorize")
	if applied.Status != "ok" {
		t.Fatalf("apply refused the benign configuration: %+v", applied)
	}
	if err := tidyV3FixtureCommand(binary, env, "-C", source, "show-ref", "--verify", "--quiet", "refs/heads/integrated").Run(); err == nil {
		t.Fatal("apply left the reference")
	}
}
