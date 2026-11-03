// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/workspace"
)

func withDefaultBase(t *testing.T, f func(context.Context, string) (string, error)) *[]string {
	t.Helper()
	asked := &[]string{}
	old := tidyDefaultBase
	tidyDefaultBase = func(ctx context.Context, path string) (string, error) {
		*asked = append(*asked, path)
		return f(ctx, path)
	}
	t.Cleanup(func() { tidyDefaultBase = old })
	return asked
}

// Issue 389: without --base the expert plan uses the default base of the repository.
func TestTidyPlanWithoutBaseUsesTheDefaultBase(t *testing.T) {
	asked := withDefaultBase(t, func(context.Context, string) (string, error) { return "refs/remotes/origin/trunk", nil })
	var v1 workspace.TidyPlanOptions
	cmd := tidyCommandWith(func(_ context.Context, o workspace.TidyPlanOptions) workspace.TidyResult {
		v1 = o
		return workspace.TidyResult{SchemaVersion: 1, Command: "tidy-plan", Status: "ok", Backend: "git", Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{}}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "/repo"})
	if err := cmd.Execute(); err != nil || v1.Base != "refs/remotes/origin/trunk" || v1.Path != "/repo" || len(*asked) != 1 || (*asked)[0] != "/repo" {
		t.Fatalf("v1: err=%v options=%+v asked=%v", err, v1, *asked)
	}
	var v3 workspace.V3PlanOptions
	cmd = tidyCommandWithV3Operations(nil, nil, func(_ context.Context, o workspace.V3PlanOptions) workspace.V3Result {
		v3 = o
		return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git"}
	}, nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "--contract", "3"})
	if err := cmd.Execute(); err != nil || v3.Base != "refs/remotes/origin/trunk" {
		t.Fatalf("v3: err=%v options=%+v", err, v3)
	}
}

// An explicit --base is used as given and the repository is not asked.
func TestTidyPlanWithBaseDoesNotAskTheRepository(t *testing.T) {
	asked := withDefaultBase(t, func(context.Context, string) (string, error) { return "refs/remotes/origin/trunk", nil })
	var got workspace.TidyPlanOptions
	cmd := tidyCommandWith(func(_ context.Context, o workspace.TidyPlanOptions) workspace.TidyResult {
		got = o
		return workspace.TidyResult{SchemaVersion: 1, Command: "tidy-plan", Status: "ok", Backend: "git", Actions: []workspace.ActionOutcome{}, Diagnostics: []workspace.Diagnostic{}, Limitations: []string{}}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "/repo", "--base", "refs/heads/main"})
	if err := cmd.Execute(); err != nil || got.Base != "refs/heads/main" || len(*asked) != 0 {
		t.Fatalf("err=%v base=%q asked=%v", err, got.Base, *asked)
	}
}

// A repository without origin/HEAD fails with the message of the resolver, which names the command.
func TestTidyPlanWithoutBaseFailsWithTheCommandThatSetsOriginHead(t *testing.T) {
	withDefaultBase(t, func(ctx context.Context, path string) (string, error) {
		return "", errors.New("no --base given: run `git remote set-head origin -a`")
	})
	ran := false
	cmd := tidyCommandWith(func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult {
		ran = true
		return workspace.TidyResult{}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "/repo"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "git remote set-head origin -a") || ran {
		t.Fatalf("err=%v ran=%t", err, ran)
	}
}
