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
	for _, args := range [][]string{{"plan"}, {"plan", "one", "two", "--base", "HEAD"}, {"plan", "--base", "HEAD", "--authorize"}, {"apply"}, {"plan", "--base", strings.Repeat("x", 4097)}, {"plan", "--base", "HEAD", "--ref", ""}} {
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
	for _, args := range [][]string{{"tidy", "--help"}, {"tidy", "plan", "--help"}} {
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
