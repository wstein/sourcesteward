// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// Spec 031 T019: --integration-proof and --accept-whitespace-differences.

func proofCommand(called *bool, got *workspace.V3PlanOptions) (planV3 func(context.Context, workspace.V3PlanOptions) workspace.V3Result) {
	return func(_ context.Context, o workspace.V3PlanOptions) workspace.V3Result {
		*called = true
		*got = o
		return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git"}
	}
}

func runProofCLI(args []string) (called bool, got workspace.V3PlanOptions, err error) {
	cmd := tidyCommandWithV3Operations(nil, nil, proofCommand(&called, &got), nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err = cmd.Execute()
	return
}

func TestTidyV3IntegrationProofFlagsReachThePlanner(t *testing.T) {
	base := []string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main"}
	called, got, err := runProofCLI(base)
	if err != nil || !called || len(got.IntegrationProofs) != 0 || got.AcceptWhitespaceDifferences {
		t.Fatalf("defaults: err=%v %+v", err, got)
	}
	called, got, err = runProofCLI(append(append([]string{}, base...), "--integration-proof", "patch-id,merge-noop", "--accept-whitespace-differences"))
	if err != nil || !called || !reflect.DeepEqual(got.IntegrationProofs, []string{"patch-id", "merge-noop"}) || !got.AcceptWhitespaceDifferences {
		t.Fatalf("flags: err=%v %+v", err, got)
	}
	called, got, err = runProofCLI(append(append([]string{}, base...), "--integration-proof=no-net-change,ancestor"))
	if err != nil || !called || !reflect.DeepEqual(got.IntegrationProofs, []string{"no-net-change", "ancestor"}) {
		t.Fatalf("equals form keeps the order given for the planner to canonicalize: err=%v %+v", err, got)
	}
}

func TestTidyV3IntegrationProofFlagMisuseExitsBeforeNativeWork(t *testing.T) {
	base := []string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main"}
	cases := map[string][]string{
		"duplicate kind":            append(append([]string{}, base...), "--integration-proof", "patch-id,patch-id"),
		"unknown kind":              append(append([]string{}, base...), "--integration-proof", "cherry-pick"),
		"empty member":              append(append([]string{}, base...), "--integration-proof", "patch-id,,merge-noop"),
		"empty value":               append(append([]string{}, base...), "--integration-proof", ""),
		"flag repeated":             append(append([]string{}, base...), "--integration-proof", "patch-id", "--integration-proof", "merge-noop"),
		"accept repeated":           append(append([]string{}, base...), "--accept-whitespace-differences", "--accept-whitespace-differences"),
		"upper case":                append(append([]string{}, base...), "--integration-proof", "Patch-ID"),
		"spaces":                    append(append([]string{}, base...), "--integration-proof", "patch-id, merge-noop"),
		"without contract 3":        {"plan", "--base", "refs/heads/main", "--integration-proof", "patch-id"},
		"accept without contract 3": {"plan", "--base", "refs/heads/main", "--accept-whitespace-differences"},
		"with retirement":           {"plan", "--contract", "3", "--base", "HEAD", "--task", "t", "--owner", "o", "--state-dir", "/tmp/state", "--remove-registration", "--remove-files", "/tmp/task", "--integration-proof", "patch-id"},
		"accept with retirement":    {"plan", "--contract", "3", "--base", "HEAD", "--task", "t", "--owner", "o", "--state-dir", "/tmp/state", "--remove-registration", "--remove-files", "/tmp/task", "--accept-whitespace-differences"},
		"with tracking refs":        append(append([]string{}, base...), "--online", "--tracking-remote", "origin", "--integration-proof", "patch-id"),
		"accept with tracking refs": append(append([]string{}, base...), "--online", "--tracking-remote", "origin", "--accept-whitespace-differences"),
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			called, _, err := runProofCLI(args)
			var runtime runtimeFailure
			if err == nil || errors.As(err, &runtime) || called {
				t.Fatalf("misuse reached the planner: err=%v called=%t", err, called)
			}
		})
	}
	// The exit code of the real command is the usage code 2.
	var out, diagnostic bytes.Buffer
	if code := Run(context.Background(), "dev", append(append([]string{"tidy"}, base...), "--integration-proof", "cherry-pick"), &out, &diagnostic); code != 2 {
		t.Fatalf("exit %d, want 2: %s %s", code, out.String(), diagnostic.String())
	}
}

// The human rendering carries every fact of the structured record.
func TestTidyV3HumanOutputAgreesWithJSONForProofEvidence(t *testing.T) {
	oid := strings.Repeat("b", 40)
	evidence := &workspace.V3IntegrationEvidence{Disposition: "needs-review", Reasons: []string{"whitespace-differs", "conflict"}, Flags: []string{"whitespace-differs"}, BaseSelector: "refs/heads/main", BaseOID: strings.Repeat("a", 40), TipOID: oid, Matched: 2, Unmatched: 1, Empty: 1, SkippedMerges: 3}
	proven := &workspace.V3IntegrationEvidence{Class: "patch-id", Strength: "history-equivalent", Disposition: "proven", Reasons: []string{}, Flags: []string{"accepted-whitespace-differences"}, BaseSelector: "refs/heads/main", BaseOID: strings.Repeat("a", 40), TipOID: oid, Matched: 3}
	r := workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git", Plan: &workspace.V3Plan{
		ProofPolicy: &workspace.V3ProofPolicy{Version: 1, Kinds: []string{"patch-id", "merge-noop"}, AcceptWhitespaceDifferences: true},
		Candidates: []workspace.V3Candidate{
			{Name: "refs/heads/review", Kind: "branch", OID: oid, Decision: "retain", Reason: "needs-review", Proof: "none", WorkspacePaths: []string{}, Evidence: &workspace.V3ProofEvidence{Integration: evidence}},
			{Name: "refs/heads/proven", Kind: "branch", OID: oid, Decision: "delete", Reason: "patch-id", Proof: "patch-id", WorkspacePaths: []string{}, Evidence: &workspace.V3ProofEvidence{Integration: proven}},
		}}}
	var human bytes.Buffer
	if err := renderTidyV3Text(&human, r); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{`kinds=["patch-id" "merge-noop"]`, "accept_whitespace_differences=true", `reasons=["whitespace-differs" "conflict"]`, `flags=["whitespace-differs"]`, "disposition=\"needs-review\"", "matched=2 unmatched=1 empty=1 skipped_merges=3", `class="patch-id"`, `strength="history-equivalent"`, `flags=["accepted-whitespace-differences"]`} {
		if !strings.Contains(human.String(), fact) {
			t.Errorf("fact %s missing from the human output:\n%s", fact, human.String())
		}
	}
}

func TestTidyV3TimeoutFlagReachesPlanAndApply(t *testing.T) {
	base := []string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main"}
	called, got, err := runProofCLI(base)
	if err != nil || !called || got.Timeout != 0 {
		t.Fatalf("default: err=%v %+v", err, got)
	}
	called, got, err = runProofCLI(append(append([]string{}, base...), "--timeout", "45m"))
	if err != nil || !called || got.Timeout != 45*time.Minute {
		t.Fatalf("plan: err=%v %+v", err, got)
	}
	for _, bad := range []string{"0", "-1m", "soon"} {
		if _, _, err = runProofCLI(append(append([]string{}, base...), "--timeout", bad)); err == nil {
			t.Fatalf("--timeout %s accepted", bad)
		}
	}
	for _, bad := range []string{"0", "-1m"} {
		c := tidyCommandWithV3Operations(nil, nil, nil, func(context.Context, workspace.V3ApplyOptions) workspace.V3Result {
			t.Fatal("apply ran with an invalid --timeout")
			return workspace.V3Result{}
		})
		c.SetOut(&bytes.Buffer{})
		c.SetErr(&bytes.Buffer{})
		c.SetArgs([]string{"apply", "--contract", "3", "--plan", "p1", "--state-dir", "/tmp/state", "--authorize", "--timeout", bad})
		if c.Execute() == nil {
			t.Fatalf("apply --timeout %s accepted", bad)
		}
	}
	var applied workspace.V3ApplyOptions
	cmd := tidyCommandWithV3Operations(nil, nil, nil, func(_ context.Context, o workspace.V3ApplyOptions) workspace.V3Result {
		applied = o
		return workspace.V3Result{SchemaVersion: 3, Command: "tidy-apply", Status: "ok", Backend: "git", Diagnostics: []workspace.Diagnostic{}}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "--contract", "3", "--plan", "p1", "--state-dir", "/tmp/state", "--authorize", "--timeout", "2h"})
	_ = cmd.Execute()
	if applied.Timeout != 2*time.Hour {
		t.Fatalf("apply timeout %s", applied.Timeout)
	}
}
