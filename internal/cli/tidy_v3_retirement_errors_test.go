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

// Retirement flag misuse names the missing or conflicting flag instead of one
// generic sentence, and an error result carries its code and first reason even
// without --json.
func TestTidyV3RetirementUsageErrorsNameTheFlag(t *testing.T) {
	retire := []string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main", "--completed-worktree", "/tmp/wt", "--owner", "o", "--state-dir", "/tmp/state", "--remove-registration", "--remove-files", "/tmp/wt"}
	without := func(flag string) []string {
		out := []string{}
		for i := 0; i < len(retire); i++ {
			if retire[i] == flag {
				if flag != "--remove-registration" {
					i++
				}
				continue
			}
			out = append(out, retire[i])
		}
		return out
	}
	cases := map[string]struct {
		args []string
		want string
	}{
		"state dir":        {without("--state-dir"), "--state-dir"},
		"registration":     {without("--remove-registration"), "--remove-registration"},
		"files":            {without("--remove-files"), "--remove-files"},
		"different files":  {append(without("--remove-files"), "--remove-files", "/tmp/other"), "one plan retires one worktree"},
		"report dir":       {append(append([]string{}, retire...), "--report-dir", "/tmp/reports"), "retirement plan writes no review report"},
		"protect for task": {[]string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main", "--task", "t", "--owner", "o", "--state-dir", "/tmp/state", "--remove-registration", "--remove-files", "/tmp/t", "--protect-worktree", "/tmp/x"}, "--protect-worktree"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			called, _, err := runProofCLI(tc.args)
			var runtime runtimeFailure
			if err == nil || called || errors.As(err, &runtime) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("called=%t err=%v, want a usage error naming %q", called, err, tc.want)
			}
		})
	}
}

func TestTidyV3ErrorResultNamesCodeAndReasonWithoutJSON(t *testing.T) {
	plan := func(context.Context, workspace.V3PlanOptions) workspace.V3Result {
		return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "error", Backend: "git", Diagnostics: []workspace.Diagnostic{{Code: "retirement-file-limit", Message: "too many files"}}}
	}
	cmd := tidyCommandWithV3Operations(nil, nil, plan, nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "retirement-file-limit: too many files") {
		t.Fatalf("error lacks the code and first reason: %v", err)
	}
}
