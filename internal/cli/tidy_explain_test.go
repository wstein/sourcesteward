// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

func explainTidyResult() workspace.TidyResult {
	oid := strings.Repeat("a", 40)
	r := workspace.TidyResult{SchemaVersion: 1, Command: "tidy-plan", Status: "ok", Backend: "git", Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{}}
	r.Plan = &workspace.TidyPlanRecord{SchemaVersion: 1, SourcePath: "/repo", Expected: workspace.Snapshot{BaseSelector: "refs/heads/main", BaseCommit: oid}, Candidates: []workspace.TidyCandidate{
		{Kind: "branch", Name: "refs/heads/done", CommitID: oid, Decision: "delete", Reason: "native-ancestry", Proof: "native-ancestry", WorkspacePaths: []string{}, Limitations: []string{}},
		{Kind: "branch", Name: "refs/heads/open", CommitID: oid, Decision: "retain", Reason: "integration-not-proven", Proof: "none", WorkspacePaths: []string{}, Limitations: []string{}},
		{Kind: "branch", Name: "refs/heads/used", CommitID: oid, Decision: "retain", Reason: "workspace-used", Proof: "none", WorkspacePaths: []string{"/ws/used"}, Limitations: []string{}},
	}, Effects: []string{}, RecoveryLimits: []string{}}
	return r
}

func runExplainV1(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := tidyCommandWith(func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult { return explainTidyResult() })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"plan", "/repo", "--base", "refs/heads/main"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

// Issue 388: the output without --explain is unchanged; with it, one explanation per retained candidate is appended.
func TestTidyPlanExplainAppendsToTheUnchangedText(t *testing.T) {
	plain, err := runExplainV1(t)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain, "Explain:") {
		t.Fatalf("explanation without --explain:\n%s", plain)
	}
	explained, err := runExplainV1(t, "--explain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(explained, plain) {
		t.Fatalf("--explain changed the plan text:\n%s", explained)
	}
	extra := strings.TrimPrefix(explained, plain)
	if strings.Count(extra, "Explain:") != 2 || strings.Contains(extra, "refs/heads/done") {
		t.Fatalf("explanations for the retained candidates only:\n%s", extra)
	}
	for _, want := range []string{`Explain: "refs/heads/open" reason="integration-not-proven"`, `Why: "It stays because it is not an ancestor of the base`, `Next: stw tidy plan /repo --contract 3 --base refs/heads/main --ref refs/heads/open --online --remote origin`, `Explain: "refs/heads/used" reason="workspace-used"`, `at /ws/used`} {
		if !strings.Contains(extra, want) {
			t.Errorf("missing %q in:\n%s", want, extra)
		}
	}
}

// The default JSON contract is frozen; --explain with it needs contract 3.
func TestTidyPlanExplainJSONNeedsContract3(t *testing.T) {
	out, err := runExplainV1(t, "--explain", "--json")
	if err == nil || !strings.Contains(err.Error(), "--explain with --json needs --contract 3") || strings.Contains(out, "Tidy operation") || strings.Contains(out, `"schema_version"`) {
		t.Fatalf("err=%v out=%q", err, out)
	}
}

func explainV3Result() workspace.V3Result {
	oid := strings.Repeat("a", 40)
	return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git", Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{},
		Plan: &workspace.V3Plan{SourcePath: "/repo", Expected: workspace.Snapshot{BaseSelector: "refs/heads/main", BaseCommit: oid}, ProofPolicy: &workspace.V3ProofPolicy{Version: 1, Kinds: []string{"ancestor"}, PRProof: "unavailable-offline"}, Candidates: []workspace.V3Candidate{
			{Name: "refs/heads/done", OID: oid, Decision: "delete", Reason: "native-ancestry", Proof: "native-ancestry"},
			{Name: "refs/heads/open", OID: oid, Decision: "retain", Reason: "integration-not-proven", Proof: "none"},
		}}}
}

func runExplainV3(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := tidyCommandWithV3Operations(nil, nil, func(context.Context, workspace.V3PlanOptions) workspace.V3Result { return explainV3Result() }, nil)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"plan", "/repo", "--contract", "3", "--base", "refs/heads/main"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestTidyPlanExplainContract3JSONHasASeparateField(t *testing.T) {
	plain, err := runExplainV3(t, "--json")
	if err != nil || strings.Contains(plain, `"explanations"`) {
		t.Fatalf("err=%v plain=%s", err, plain)
	}
	explained, err := runExplainV3(t, "--json", "--explain")
	if err != nil {
		t.Fatal(err)
	}
	var got, base map[string]any
	if json.Unmarshal([]byte(explained), &got) != nil || json.Unmarshal([]byte(plain), &base) != nil {
		t.Fatal("not JSON")
	}
	list, ok := got["explanations"].([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("explanations: %v", got["explanations"])
	}
	e := list[0].(map[string]any)
	if e["name"] != "refs/heads/open" || e["reason"] != "integration-not-proven" || !strings.Contains(e["text"].(string), "offline") || e["next_command"] != "stw tidy plan /repo --contract 3 --base refs/heads/main --ref refs/heads/open --online --remote origin" {
		t.Fatalf("explanation: %v", e)
	}
	delete(got, "explanations")
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(base)
	if string(a) != string(b) {
		t.Fatalf("--explain changed the rest of the JSON:\n%s\n%s", a, b)
	}
	text, err := runExplainV3(t, "--explain")
	if err != nil || !strings.Contains(text, "Explain: \"refs/heads/open\" reason=\"integration-not-proven\"\n  Why: ") || !strings.Contains(text, "\n  Next: stw tidy plan /repo") {
		t.Fatalf("err=%v text=\n%s", err, text)
	}
}

// shellArgs runs a next command in dir with stw replaced by a shell function that prints its
// arguments, and returns the arguments stw would have received.
func shellStwArgs(t *testing.T, dir, command string) []string {
	t.Helper()
	cmd := exec.Command("sh", "-c", `stw() { for a in "$@"; do printf '%s\0' "$a"; done; }; `+command)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the next command does not run in a shell: %q: %v", command, err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}

// Issue 388: a next command that starts stw is accepted by the real flag parsing and validators, and
// every other next command runs against a real repository. Prefix checks would not catch a missing
// --remote or a relative --report-dir.
func TestExplainNextCommandsAreExecutable(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@e.x", "-c", "init.defaultBranch=main"}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("commit", "--allow-empty", "-qm", "base")
	git("branch", "topic")
	git("remote", "add", "origin", repo)
	oid := strings.Repeat("a", 40)
	var stwSeen, otherSeen int
	for _, reason := range workspace.ExplainedReasons {
		for _, contract3 := range []bool{true, false} {
			e := workspace.ExplainRetention(workspace.ExplainContext{Source: repo, Base: "refs/heads/main", Remote: "origin", Contract3: contract3, PRProof: "unavailable-offline"}, "refs/heads/topic", oid, reason, []string{"/ws/topic"}, []string{"partial-match"})
			work := filepath.Join(root, "work")
			if err := os.MkdirAll(work, 0o700); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(e.NextCommand, "stw tidy plan") {
				stwSeen++
				args := shellStwArgs(t, work, e.NextCommand)
				cmd := tidyCommandWithV3Operations(nil, nil, func(context.Context, workspace.V3PlanOptions) workspace.V3Result { return explainV3Result() }, nil)
				cmd.SetOut(&bytes.Buffer{})
				cmd.SetErr(&bytes.Buffer{})
				cmd.SetArgs(args[1:])
				if err := cmd.Execute(); err != nil {
					t.Errorf("%s: the real command rejects %q: %v", reason, e.NextCommand, err)
				}
				continue
			}
			if strings.HasPrefix(e.NextCommand, "git ") {
				otherSeen++
				c := exec.Command("sh", "-c", e.NextCommand)
				c.Dir = work
				if out, err := c.CombinedOutput(); err != nil {
					t.Errorf("%s: %q fails: %v\n%s", reason, e.NextCommand, err, out)
				}
			}
		}
	}
	if stwSeen == 0 || otherSeen == 0 {
		t.Fatalf("vacuous: %d stw commands and %d git commands checked", stwSeen, otherSeen)
	}
}

// The printed Next: line is the command as typed: parsed by a shell it gives the arguments the
// unrendered command gives, also for a name that holds quotes and backslashes and for the report
// directory of needs-review, which holds double quotes.
func TestRenderedNextLineRoundTripsThroughTheShell(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"refs/heads/topic", `refs/heads/we"ird\back'q`} {
		for _, reason := range []string{"needs-review", "integration-not-proven"} {
			e := workspace.ExplainRetention(workspace.ExplainContext{Source: "/repo", Base: "refs/heads/main", Contract3: true, PRProof: "unavailable-offline"}, name, strings.Repeat("a", 40), reason, nil, []string{"partial-match"})
			var out bytes.Buffer
			if err := renderTidyExplanations(&out, []workspace.V3Explanation{e}); err != nil {
				t.Fatal(err)
			}
			var next string
			for _, line := range strings.Split(out.String(), "\n") {
				if rest, ok := strings.CutPrefix(line, "  Next: "); ok {
					next = rest
				}
			}
			if next != e.NextCommand {
				t.Errorf("%s %q: printed %q, command %q", reason, name, next, e.NextCommand)
			}
			want := shellStwArgs(t, dir, e.NextCommand)
			got := shellStwArgs(t, dir, next)
			if !reflect.DeepEqual(got, want) || !strings.Contains(strings.Join(got, " "), name) {
				t.Errorf("%s %q: printed command parses to %q, want %q", reason, name, got, want)
			}
		}
	}
	var out bytes.Buffer
	_ = renderTidyExplanations(&out, []workspace.V3Explanation{{Name: "n", Reason: "r", Text: "t", NextCommand: "git log\x1b[2J\n\u202e\u2066\u200b\u2028\u2029\xff ok é"}})
	if strings.ContainsAny(out.String(), "\x1b\u202e\u2066\u200b\u2028\u2029\xff") || strings.Count(out.String(), "\n") != 3 || !strings.Contains(out.String(), "ok é") {
		t.Errorf("non-printable characters reach the terminal: %q", out.String())
	}
	for _, want := range []string{`\x{1b}`, `\x{202e}`, `\x{2066}`, `\x{200b}`, `\x{2028}`, `\x{2029}`, `\x{ff}`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%s is not escaped visibly in %q", want, out.String())
		}
	}
}

// The pr-ambiguous command runs gh in the repository from a subshell, with the path quoted: a path
// with a space and quotes reaches gh as the working directory and the caller's shell stays where it is.
func TestPRAmbiguousNextCommandQuotesTheSourceAndLeavesTheShell(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, `my "dir's`)
	if err := os.Mkdir(src, 0o700); err != nil {
		t.Fatal(err)
	}
	e := workspace.ExplainRetention(workspace.ExplainContext{Source: src, Base: "refs/heads/main", Contract3: true}, "refs/heads/t", strings.Repeat("a", 40), "pr-ambiguous", nil, nil)
	var out bytes.Buffer
	if err := renderTidyExplanations(&out, []workspace.V3Explanation{e}); err != nil {
		t.Fatal(err)
	}
	var next string
	for _, line := range strings.Split(out.String(), "\n") {
		if rest, ok := strings.CutPrefix(line, "  Next: "); ok {
			next = rest
		}
	}
	cmd := exec.Command("sh", "-c", `gh() { printf '%s\0' "$PWD" "$@"; }; `+next+`; printf '%s\0' "$PWD"`)
	cmd.Dir = root
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("%q: %v", next, err)
	}
	got := strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	if len(got) < 4 || got[0] != src || got[1] != "pr" || got[2] != "list" || got[len(got)-1] != root {
		t.Errorf("gh ran in %q; the shell ended in %q: %q", got[0], got[len(got)-1], got)
	}
}
