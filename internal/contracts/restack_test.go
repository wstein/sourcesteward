// SPDX-License-Identifier: EUPL-1.2
package contracts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/cli"
	"github.com/wstein/sourcesteward/internal/restack"
)

func restackFixtureFile(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root(t), "internal/cli/testdata/restack-plan-"+name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func restackObject(t *testing.T, raw []byte) map[string]any { return value(t, raw).(map[string]any) }

func TestRestackPlanFixturesValidate(t *testing.T) {
	s := schema(t, "restack-plan")
	for name, want := range map[string][2]string{
		"clean": {"ok", "evidence-clean"}, "needs-attention": {"ok", "needs-attention"}, "conflict": {"ok", "conflicts-predicted"},
		"error": {"error", "none"}, "incomplete": {"incomplete", "none"},
	} {
		raw := restackFixtureFile(t, name)
		valid(t, s, raw)
		o := restackObject(t, raw)
		if o["status"] != want[0] || o["outcome"] != want[1] || o["kind"] != "restack-plan" {
			t.Errorf("%s: %v %v", name, o["status"], o["outcome"])
		}
	}
}

// restackRepo builds the rebase-merge scenario of the quickstart in a hermetic repository.
func restackRepo(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "repo")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	tick := 1_700_000_000
	run := func(args ...string) {
		t.Helper()
		tick += 60
		date := "@" + strconv.Itoa(tick) + " +0000"
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=Fixture Owner", "GIT_AUTHOR_EMAIL=owner@example.org", "GIT_COMMITTER_NAME=Fixture Owner", "GIT_COMMITTER_EMAIL=owner@example.org", "GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	file := func(name string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	file("a")
	run("add", "-A")
	run("commit", "-q", "-m", "A")
	run("checkout", "-q", "-b", "parent")
	file("b1")
	run("add", "-A")
	run("commit", "-q", "-m", "B1")
	run("checkout", "-q", "-b", "child")
	file("c1")
	run("add", "-A")
	run("commit", "-q", "-m", "C1")
	run("checkout", "-q", "main")
	run("cherry-pick", "parent")
	return dir
}

func TestRestackPlanRealOutputValidatesForEveryStatus(t *testing.T) {
	s := schema(t, "restack-plan")
	dir := restackRepo(t)
	plan := func(branch string, extra ...string) []byte {
		var out, errOut bytes.Buffer
		args := append([]string{"restack", "plan", dir, "--branch", branch, "--old-parent", "refs/heads/parent", "--new-parent", "refs/heads/main", "--json"}, extra...)
		cli.Run(context.Background(), "dev", args, &out, &errOut)
		return out.Bytes()
	}
	ok := plan("refs/heads/child", "--identity", "Fixture Owner <owner@example.org>")
	valid(t, s, ok)
	if o := restackObject(t, ok); o["outcome"] != "evidence-clean" {
		t.Fatalf("%v", o["outcome"])
	}
	valid(t, s, plan("refs/heads/child"))
	refused := plan("refs/heads/missing")
	valid(t, s, refused)
	if o := restackObject(t, refused); o["status"] != "error" || o["evidence"] != nil {
		t.Fatalf("%v", o)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	incomplete, err := json.Marshal(restack.Plan(ctx, restack.Options{Path: dir, Branch: "refs/heads/child", OldParent: "refs/heads/parent", NewParent: "refs/heads/main", Remote: "origin"}))
	if err != nil {
		t.Fatal(err)
	}
	valid(t, s, incomplete)
	if o := restackObject(t, incomplete); o["status"] != "incomplete" {
		t.Fatalf("%v", o["status"])
	}
}

func TestRestackPlanInvariants(t *testing.T) {
	s := schema(t, "restack-plan")
	cases := []struct {
		name   string
		seed   string
		mutate func(map[string]any)
	}{
		{"version", "clean", func(v map[string]any) { v["schema_version"] = 2 }},
		{"kind", "clean", func(v map[string]any) { v["kind"] = "other" }},
		{"status", "clean", func(v map[string]any) { v["status"] = "maybe" }},
		{"outcome", "clean", func(v map[string]any) { v["outcome"] = "safe-to-push" }},
		{"notice", "clean", func(v map[string]any) { v["notice"] = "safe" }},
		{"missing required", "clean", func(v map[string]any) { delete(v, "limitations") }},
		{"null array", "clean", func(v map[string]any) { v["diagnostics"] = nil }},
		{"limitation dropped", "clean", func(v map[string]any) { v["limitations"] = []any{"prediction-is-not-apply"} }},
		{"ok without evidence", "clean", func(v map[string]any) { v["evidence"] = nil }},
		{"ok with outcome none", "clean", func(v map[string]any) { v["outcome"] = "none" }},
		{"error with evidence", "error", func(v map[string]any) { v["evidence"] = map[string]any{} }},
		{"error with outcome", "error", func(v map[string]any) { v["outcome"] = "needs-attention" }},
		{"error without diagnostic", "error", func(v map[string]any) { v["diagnostics"] = []any{} }},
		{"incomplete with outcome", "incomplete", func(v map[string]any) { v["outcome"] = "evidence-clean" }},
		{"clean with unknown identity", "clean", func(v map[string]any) { ev(v, "identity")["state"] = "unknown" }},
		{"clean without the flag", "clean", func(v map[string]any) { ev(v, "identity")["source"] = "none" }},
		{"clean with overlap", "clean", func(v map[string]any) { ev(v, "overlap")["indicator"] = "some" }},
		{"clean with unequal pairing", "clean", func(v map[string]any) { ev(v, "range_pairing")["all_equal"] = false }},
		{"clean with undropped parent", "clean", func(v map[string]any) { ev(v, "parent_dropped")["all_dropped"] = false }},
		{"clean with empty-after", "clean", func(v map[string]any) {
			v["evidence"].(map[string]any)["empty_after_replay"] = []any{strings.Repeat("a", 40)}
		}},
		{"write disclosure", "clean", func(v map[string]any) { v["effects"].(map[string]any)["writes"].(map[string]any)["refs"] = true }},
		{"hook run by stw", "clean", func(v map[string]any) {
			v["effects"].(map[string]any)["hooks"] = []any{map[string]any{"name": "x", "present": true, "would_run_by_porcelain_rebase": true, "run_by_stw": true}}
		}},
		{"short oid", "clean", func(v map[string]any) { v["input"].(map[string]any)["branch"].(map[string]any)["old_head"] = "abc" }},
		{"lease new sha known", "clean", func(v map[string]any) {
			v["texts"].(map[string]any)["lease_push"].(map[string]any)["new_sha_known"] = true
		}},
	}
	for _, tc := range cases {
		v := restackObject(t, restackFixtureFile(t, tc.seed))
		tc.mutate(v)
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if s.Validate(value(t, raw)) == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
	// Additive compatibility: new properties and new state values are accepted.
	v := restackObject(t, restackFixtureFile(t, "clean"))
	v["future"] = true
	ev(v, "identity")["state"] = "future-state"
	ev(v, "range_pairing")["future"] = []any{}
	v["outcome"] = "needs-attention"
	raw, _ := json.Marshal(v)
	valid(t, s, raw)
}

func ev(v map[string]any, name string) map[string]any {
	return v["evidence"].(map[string]any)[name].(map[string]any)
}
