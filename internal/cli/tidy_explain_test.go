// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
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
	for _, want := range []string{`Explain: "refs/heads/open" reason="integration-not-proven"`, `Why: "It stays because it is not an ancestor of the base`, `Next: "stw tidy plan /repo --contract 3 --base refs/heads/main --ref refs/heads/open --online"`, `Explain: "refs/heads/used" reason="workspace-used"`, `at /ws/used`} {
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
	if e["name"] != "refs/heads/open" || e["reason"] != "integration-not-proven" || !strings.Contains(e["text"].(string), "offline") || e["next_command"] != "stw tidy plan /repo --contract 3 --base refs/heads/main --ref refs/heads/open --online" {
		t.Fatalf("explanation: %v", e)
	}
	delete(got, "explanations")
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(base)
	if string(a) != string(b) {
		t.Fatalf("--explain changed the rest of the JSON:\n%s\n%s", a, b)
	}
	text, err := runExplainV3(t, "--explain")
	if err != nil || !strings.Contains(text, "Explain: \"refs/heads/open\" reason=\"integration-not-proven\"\n  Why: ") || !strings.Contains(text, "\n  Next: \"stw tidy plan /repo") {
		t.Fatalf("err=%v text=\n%s", err, text)
	}
}
