// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

func TestTidyUsageBeforeObservation(t *testing.T) {
	for _, args := range [][]string{{"plan", "one", "two", "--base", "HEAD"}, {"plan", "--base", "HEAD", "--authorize"}, {"apply"}, {"plan", "--base", strings.Repeat("x", 4097)}, {"plan", "--base", "HEAD", "--ref", ""}} {
		called := false
		cmd := tidyCommandWith(func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult {
			called = true
			return workspace.TidyResult{Status: "ok"}
		})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		err := cmd.Execute()
		var failure runtimeFailure
		if err == nil || errors.As(err, &failure) || called {
			t.Fatalf("%q err=%v called=%t", args, err, called)
		}
	}
}
func TestTidyLiteralPlanOptions(t *testing.T) {
	cmd := tidyCommandWith(func(_ context.Context, o workspace.TidyPlanOptions) workspace.TidyResult {
		if o.Path != "literal\nrepo" || o.Base != "HEAD" || o.StateDir != "/tmp/state" || len(o.Refs) != 2 || o.Refs[0] != "a,b" || o.Refs[1] != "literal\nbranch" {
			t.Fatalf("%+v", o)
		}
		return workspace.TidyResult{SchemaVersion: 1, Command: "tidy-plan", Status: "ok", Backend: "git"}
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"plan", "literal\nrepo", "--base", "HEAD", "--state-dir", "/tmp/state", "--ref", "a,b", "--ref", "literal\nbranch", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result workspace.TidyResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Command != "tidy-plan" {
		t.Fatalf("%s %v", out.String(), err)
	}
}
func TestTidyOutputAndRuntimeFailures(t *testing.T) {
	for _, status := range []string{"ok", "refused", "partial", "error"} {
		for _, structured := range []bool{false, true} {
			run := func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult {
				return workspace.TidyResult{SchemaVersion: 1, Command: "tidy-plan", Status: status, Backend: "git", Limitations: []string{"literal\nwarning\x1b[31m"}}
			}
			cmd := tidyCommandWith(run)
			var out bytes.Buffer
			cmd.SetOut(&out)
			args := []string{"plan", "--base", "HEAD"}
			if structured {
				args = append(args, "--json")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			var failure runtimeFailure
			if (err != nil) != (status != "ok") || (err != nil && !errors.As(err, &failure)) {
				t.Fatalf("status=%s err=%v", status, err)
			}
			if out.Len() == 0 || strings.ContainsRune(out.String(), '\x1b') {
				t.Fatalf("unsafe/missing %q", out.String())
			}
			cmd = tidyCommandWith(run)
			cmd.SetOut(&failOnWrite{fail: 1})
			cmd.SetArgs(args)
			if err := cmd.Execute(); !errors.As(err, &failure) {
				t.Fatalf("writer=%v", err)
			}
		}
	}
}
func TestTidyCancellationAfterObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := tidyCommandWith(func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult {
		cancel()
		return workspace.TidyResult{Status: "ok"}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "--base", "HEAD"})
	var failure runtimeFailure
	if err := cmd.ExecuteContext(ctx); !errors.As(err, &failure) {
		t.Fatal(err)
	}
}
func TestTidyHelpWithoutBackends(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{{"tidy", "--help"}, {"tidy", "plan", "--help"}, {"tidy", "apply", "--help"}} {
		var out, diagnostic bytes.Buffer
		if code := Run(context.Background(), "dev", args, &out, &diagnostic); code != 0 || out.Len() == 0 || diagnostic.Len() != 0 {
			t.Fatalf("%q %d %s", args, code, diagnostic.String())
		}
	}
}

func TestTidyDuplicateSelectionStructuredRefusal(t *testing.T) {
	cmd := tidyCommandWith(func(_ context.Context, o workspace.TidyPlanOptions) workspace.TidyResult {
		if len(o.Refs) != 2 || o.Refs[0] != o.Refs[1] {
			t.Fatal(o.Refs)
		}
		return workspace.TidyResult{SchemaVersion: 1, Command: "tidy-plan", Status: "refused", Backend: "unknown", Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{{Code: "invalid-input", Message: "duplicate selectors"}}, Limitations: []string{}}
	})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"plan", "--base", "HEAD", "--ref", "a", "--ref", "a", "--json"})
	var failure runtimeFailure
	if err := cmd.Execute(); !errors.As(err, &failure) {
		t.Fatal(err)
	}
	var result workspace.TidyResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.Status != "refused" {
		t.Fatalf("%s %v", out.String(), err)
	}
}

func TestTidyReferenceCountBeforeSelectorCopy(t *testing.T) {
	refs := make([]string, 1025)
	refs[0] = "\x00"
	if err := validateTidyPlanCLI(nil, workspace.TidyPlanOptions{Base: "HEAD", Refs: refs}); err == nil || err.Error() != "--ref exceeds 1024 selectors" {
		t.Fatalf("count must be checked before inspecting/copying selectors: %v", err)
	}
}

func TestTidyApplyUsageBeforeOperation(t *testing.T) {
	for _, args := range [][]string{{"apply"}, {"apply", "--plan", "id"}, {"apply", "--state-dir", "/tmp/state"}, {"apply", "extra", "--plan", "id", "--state-dir", "/tmp/state"}, {"apply", "--plan", "../escape", "--state-dir", "/tmp/state"}, {"apply", "--plan", "id", "--state-dir", "/tmp/state", "--ref", "topic"}} {
		called := false
		cmd := tidyCommandWithOperations(nil, func(context.Context, workspace.TidyApplyOptions) workspace.TidyResult {
			called = true
			return workspace.TidyResult{Status: "ok"}
		})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		err := cmd.Execute()
		var failure runtimeFailure
		if err == nil || errors.As(err, &failure) || called {
			t.Fatalf("%q err=%v called=%t", args, err, called)
		}
	}
}
func TestTidyApplyAuthorizationAndStructuredOutcomes(t *testing.T) {
	for _, authorize := range []bool{false, true} {
		for _, status := range []string{"ok", "refused", "partial", "error"} {
			cmd := tidyCommandWithOperations(nil, func(_ context.Context, o workspace.TidyApplyOptions) workspace.TidyResult {
				if o.PlanID != "plan-id" || o.StateDir != "/tmp/state" || o.Authorize != authorize {
					t.Fatal(o)
				}
				actual := status
				if !authorize {
					actual = "refused"
				}
				return workspace.TidyResult{SchemaVersion: 1, Command: "tidy-apply", Status: actual, Backend: "git", Actions: []workspace.ActionOutcome{{Name: "recovery", Status: "recorded", Detail: "/tmp/state/operations/tidy-plan-id.json"}}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{"partial recovery requires review"}}
			})
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
			var out bytes.Buffer
			cmd.SetOut(&out)
			args := []string{"apply", "--plan", "plan-id", "--state-dir", "/tmp/state", "--json"}
			if authorize {
				args = append(args, "--authorize")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			var failure runtimeFailure
			if (err != nil) != (!authorize || status != "ok") || (err != nil && !errors.As(err, &failure)) {
				t.Fatal(err)
			}
			var result workspace.TidyResult
			if e := json.Unmarshal(out.Bytes(), &result); e != nil || result.Command != "tidy-apply" || len(result.Actions) != 1 {
				t.Fatalf("%s %v", out.String(), e)
			}
		}
	}
}
func TestTidyApplyHumanRecoveryAndWriterFailure(t *testing.T) {
	run := func(context.Context, workspace.TidyApplyOptions) workspace.TidyResult {
		return workspace.TidyResult{Command: "tidy-apply", Status: "partial", Backend: "jj", Actions: []workspace.ActionOutcome{{Name: "recovery", Status: "prepared", Detail: "literal\nrecovery\x1b"}}, Limitations: []string{"native pin retained"}}
	}
	cmd := tidyCommandWithOperations(nil, run)
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"apply", "--plan", "plan-id", "--state-dir", "/tmp/state", "--authorize"})
	var failure runtimeFailure
	if err := cmd.Execute(); !errors.As(err, &failure) {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "recovery") || !strings.Contains(out.String(), "native pin") || strings.ContainsRune(out.String(), '\x1b') {
		t.Fatal(out.String())
	}
	cmd = tidyCommandWithOperations(nil, run)
	cmd.SetOut(&failOnWrite{fail: 1})
	cmd.SetArgs([]string{"apply", "--plan", "plan-id", "--state-dir", "/tmp/state", "--authorize"})
	if err := cmd.Execute(); !errors.As(err, &failure) {
		t.Fatal(err)
	}
}

func TestTidyRetirementSelectorsBeforeOperation(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "--base", "HEAD", "--task", ""},
		{"plan", "--base", "HEAD", "--task", "task"},
		{"plan", "--base", "HEAD", "--task", "task", "--owner", "owner", "--remove-registration"},
		{"plan", "--base", "HEAD", "--task", "task", "--owner", "owner", "--state-dir", "/tmp/state"},
		{"plan", "--base", "HEAD", "--task", "task", "--owner", "owner", "--state-dir", "/tmp/state", "--remove-registration", "--ref", "topic"},
		{"plan", "--base", "HEAD", "--remove-files", "/tmp/task"},
		{"plan", "--base", "HEAD", "--task", "../escape", "--owner", "owner", "--state-dir", "/tmp/state", "--remove-registration"},
	} {
		called := false
		cmd := tidyCommandWith(func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult {
			called = true
			return workspace.TidyResult{Status: "ok"}
		})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		var failure runtimeFailure
		if err := cmd.Execute(); err == nil || errors.As(err, &failure) || called {
			t.Fatalf("%q err=%v called=%t", args, err, called)
		}
	}
}
func TestTidyRetirementFlagsAndQuotedHumanActions(t *testing.T) {
	cmd := tidyCommandWith(func(_ context.Context, o workspace.TidyPlanOptions) workspace.TidyResult {
		if o.Task != "task" || o.Owner != "owner" || !o.RemoveRegistration || o.RemoveFiles != "/tmp/task" || o.StateDir != "/tmp/state" || len(o.Refs) != 0 {
			t.Fatal(o)
		}
		return workspace.TidyResult{SchemaVersion: 2, Command: "tidy-plan", Status: "ok", Backend: "git", Retirement: &workspace.RetirementPlanRecord{PlanID: "retirement", Backend: "git", RemoveRegistration: true, RemoveFiles: o.RemoveFiles, Expected: workspace.RetirementObservation{Target: workspace.NativeIdentity{CommitID: "target-commit", ChangeID: "target-change", OperationID: "target-operation"}, Manifest: workspace.RetirementManifest{Complete: true, Entries: []workspace.RetirementEntry{{Path: "owned-file", Kind: "regular", Digest: "file-digest"}}}, Actions: []workspace.RetirementAction{{Name: "registration", Target: "task", Decision: "delete", Proof: "native-ancestry", Reason: "clean"}, {Name: "files", Target: "literal\npath\x1b", Decision: "delete", Proof: "native-ancestry", Reason: "separate-authorization"}}}, Effects: []string{"coupled native registration and files removal"}, RecoveryLimits: []string{"retained recovery pin"}}, Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{}}
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"plan", "--base", "HEAD", "--task", "task", "--owner", "owner", "--remove-registration", "--remove-files", "/tmp/task", "--state-dir", "/tmp/state"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Retirement plan:", "registration", "files", "separate-authorization", "coupled native", "recovery pin", "target-commit", "target-change", "target-operation", "Manifest entry:", "owned-file", "file-digest"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q: %s", want, out.String())
		}
	}
	if strings.ContainsRune(out.String(), '\x1b') {
		t.Fatal(out.String())
	}
}
