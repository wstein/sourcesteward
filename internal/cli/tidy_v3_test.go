// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wstein/sourcesteward/internal/workspace"
	"strings"
	"testing"
)

// Unsupported ownership must cross the explicit v3 boundary and produce its
// structured refusal, rather than a Cobra syntax error or raw Git fallback.
func TestTidyV3UnsupportedBackendStructured(t *testing.T) {
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), "dev", []string{"tidy", "plan", t.TempDir(), "--contract", "3", "--base", "HEAD", "--json"}, &out, &diagnostic)
	var result struct {
		SchemaVersion int    `json:"schema_version"`
		Status        string `json:"status"`
		Plan          any    `json:"plan"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || code != 1 || result.SchemaVersion != 3 || result.Status != "refused" || result.Plan != nil {
		t.Fatalf("code=%d result=%+v JSON=%s diagnostic=%s parse=%v", code, result, out.String(), diagnostic.String(), err)
	}
}

func TestTidyV3InvalidGrammarBeforeOperations(t *testing.T) {
	oid := strings.Repeat("a", 40)
	cases := [][]string{
		{"plan", "--contract", "3", "--base", "HEAD", "--completed-worktree", ""},
		{"plan", "--contract", "3", "--base", "HEAD", "--remote", ""},
		{"plan", "--contract", "3", "--base", "HEAD", "--owner", ""},
		{"plan", "--contract", "3", "--base", "HEAD", "--state-dir", ""},
		{"apply", "--contract", "3", "--plan", "id", "--state-dir", "/tmp/state", "--completed-worktree", ""},
		{"plan", "--base", "HEAD", "--online"},
		{"plan", "--base", "HEAD", "--online=false"},
		{"plan", "--base", "HEAD", "--remote", "origin"},
		{"plan", "--contract", "2", "--base", "HEAD"},
		{"plan", "--contract", "3", "--base", "HEAD", "--tracking-remote", "origin"},
		{"plan", "--contract", "3", "--base", "HEAD", "--online", "--remote", "origin", "--tracking-remote", "origin"},
		{"plan", "--contract", "3", "--base", "HEAD", "--ref", "topic"},
		{"plan", "--contract", "3", "--base", "HEAD", "--ref", "refs/heads/topic", "--ref", "refs/heads/topic"},
		{"plan", "relative", "--contract", "3", "--base", "HEAD"},
		{"plan", "--contract", "3", "--base", "HEAD", "--state-dir", "/tmp/../state"},
		{"plan", "--contract", "3", "--base", "HEAD", "--online", "--remote", "origin", "--pr", oid + "=-1"},
		{"plan", "--contract", "3", "--base", "HEAD", "--online", "--remote", "origin", "--pr", oid + "=0"},
		{"plan", "--contract", "3", "--base", "HEAD", "--online", "--remote", "origin", "--pr", oid + "=1", "--pr", oid + "=2"},
		{"apply", "--plan", "id", "--state-dir", "/tmp/state", "--online"},
		{"apply", "--contract", "3", "--plan", "id", "--state-dir", "/tmp/state", "--owner", "owner"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			called := false
			cmd := tidyCommandWithV3Operations(func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult {
				called = true
				return workspace.TidyResult{Status: "ok"}
			}, func(context.Context, workspace.TidyApplyOptions) workspace.TidyResult {
				called = true
				return workspace.TidyResult{Status: "ok"}
			}, func(context.Context, workspace.V3PlanOptions) workspace.V3Result {
				called = true
				return workspace.V3Result{Status: "ok"}
			}, func(context.Context, workspace.V3ApplyOptions) workspace.V3Result {
				called = true
				return workspace.V3Result{Status: "ok"}
			})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(args)
			err := cmd.Execute()
			var runtime runtimeFailure
			if err == nil || errors.As(err, &runtime) || called {
				t.Fatalf("err=%v called=%t", err, called)
			}
		})
	}
}

func TestTidyV3LiteralOnlineDispatch(t *testing.T) {
	oid := strings.Repeat("a", 40)
	calls := 0
	cmd := tidyCommandWithV3Operations(nil, nil, func(_ context.Context, o workspace.V3PlanOptions) workspace.V3Result {
		calls++
		if o.Path != "/tmp/repo" || !o.Online || o.Remote != "origin" || o.Repo != "owner/repo" || o.Host != "github.com" || len(o.Refs) != 1 || o.Refs[0] != "refs/heads/a,b" || len(o.PRHints) != 1 || o.PRHints[0].OID != oid || o.PRHints[0].Number != 12 {
			t.Fatalf("%+v", o)
		}
		return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git"}
	}, nil)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"plan", "/tmp/repo", "--contract", "3", "--base", "refs/heads/main", "--ref", "refs/heads/a,b", "--online", "--remote", "origin", "--repo", "owner/repo", "--host", "github.com", "--pr", oid + "=12", "--json"})
	if err := cmd.Execute(); err != nil || calls != 1 {
		t.Fatalf("%v %d", err, calls)
	}
	var result workspace.V3Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil || result.SchemaVersion != 3 {
		t.Fatalf("%s %v", out.String(), err)
	}
}

func TestTidyV3ApplyLiteralAcknowledgmentsAndStatus(t *testing.T) {
	for _, status := range []string{"ok", "refused", "partial", "error"} {
		for _, structured := range []bool{false, true} {
			calls := 0
			cmd := tidyCommandWithV3Operations(nil, nil, nil, func(_ context.Context, o workspace.V3ApplyOptions) workspace.V3Result {
				calls++
				if o.PlanID != "plan-id" || o.StateDir != "/tmp/state" || !o.Authorize || !o.Online || o.CompletedWorktree != "/tmp/completed" || o.Owner != "operator" || len(o.ProtectWorktrees) != 2 || o.ProtectWorktrees[0] != "/tmp/active-one" || o.ProtectWorktrees[1] != "/tmp/active-two" {
					t.Fatalf("%+v", o)
				}
				return workspace.V3Result{SchemaVersion: 3, Command: "tidy-apply", Status: status, Backend: "git", Actions: []workspace.ActionOutcome{{Name: "refs/heads/literal\nbranch", Status: "complete", Detail: "recovery confirmed"}}, Diagnostics: []workspace.Diagnostic{{Code: "test", Message: "literal\nwarning\x1b[31m"}}}
			})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			args := []string{"apply", "--contract", "3", "--plan", "plan-id", "--state-dir", "/tmp/state", "--authorize", "--online", "--completed-worktree", "/tmp/completed", "--owner", "operator", "--protect-worktree", "/tmp/active-one", "--protect-worktree", "/tmp/active-two"}
			if structured {
				args = append(args, "--json")
			}
			cmd.SetArgs(args)
			err := cmd.Execute()
			var runtime runtimeFailure
			if calls != 1 || (err != nil) != (status != "ok") || err != nil && !errors.As(err, &runtime) || out.Len() == 0 || strings.ContainsRune(out.String(), '\x1b') {
				t.Fatalf("status=%s structured=%t calls=%d err=%v out=%q", status, structured, calls, err, out.String())
			}
		}
	}
}

func TestTidyV3WriterFailureAndCancellation(t *testing.T) {
	for _, structured := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cmd := tidyCommandWithV3Operations(nil, nil, func(context.Context, workspace.V3PlanOptions) workspace.V3Result {
			cancel()
			return workspace.V3Result{SchemaVersion: 3, Status: "ok"}
		}, nil)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		args := []string{"plan", "--contract", "3", "--base", "HEAD"}
		if structured {
			args = append(args, "--json")
		}
		cmd.SetArgs(args)
		var runtime runtimeFailure
		if err := cmd.ExecuteContext(ctx); !errors.As(err, &runtime) {
			t.Fatalf("canceled result escaped: %v", err)
		}
		cmd = tidyCommandWithV3Operations(nil, nil, func(context.Context, workspace.V3PlanOptions) workspace.V3Result {
			return workspace.V3Result{SchemaVersion: 3, Status: "ok"}
		}, nil)
		cmd.SetOut(&failOnWrite{fail: 1})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		if err := cmd.Execute(); !errors.As(err, &runtime) {
			t.Fatalf("writer failure escaped: %v", err)
		}
	}
}

func TestTidyV3DuplicateSingletonsBeforeOperations(t *testing.T) {
	type example struct {
		base        []string
		name, value string
		boolean     bool
	}
	local := []string{"plan", "--contract", "3", "--base", "HEAD"}
	online := append(append([]string{}, local...), "--online", "--remote", "origin")
	owned := append(append([]string{}, local...), "--task", "task", "--owner", "owner", "--state-dir", "/tmp/state", "--remove-registration", "--remove-files", "/tmp/task")
	apply := []string{"apply", "--contract", "3", "--plan", "id", "--state-dir", "/tmp/state"}
	cases := []example{
		{[]string{"plan", "--contract", "3"}, "base", "HEAD", false},
		{[]string{"plan", "--base", "HEAD"}, "contract", "3", false},
		{local, "state-dir", "/tmp/state", false},
		{local, "json", "true", true},
		{append(append([]string{}, local...), "--remote", "origin"), "online", "true", true},
		{append(append([]string{}, local...), "--online"), "remote", "origin", false},
		{append(append([]string{}, local...), "--online"), "tracking-remote", "origin", false},
		{online, "repo", "owner/repo", false},
		{online, "host", "github.com", false},
		{owned, "task", "task", false},
		{owned, "owner", "owner", false},
		{owned, "remove-files", "/tmp/task", false},
		{owned, "remove-registration", "true", true},
		{append(append([]string{}, local...), "--owner", "owner", "--state-dir", "/tmp/state", "--remove-registration", "--remove-files", "/tmp/task"), "completed-worktree", "/tmp/task", false},
		{[]string{"apply", "--contract", "3", "--state-dir", "/tmp/state"}, "plan", "id", false},
		{apply, "authorize", "true", true},
		{apply, "online", "true", true},
		{apply, "state-dir", "/tmp/state", false},
	}
	for _, c := range cases {
		for _, equals := range []bool{false, true} {
			t.Run(c.name+fmt.Sprint(equals), func(t *testing.T) {
				called := false
				cmd := tidyCommandWithV3Operations(nil, nil, func(context.Context, workspace.V3PlanOptions) workspace.V3Result {
					called = true
					return workspace.V3Result{Status: "ok"}
				}, func(context.Context, workspace.V3ApplyOptions) workspace.V3Result {
					called = true
					return workspace.V3Result{Status: "ok"}
				})
				args := append([]string{}, c.base...)
				for n := 0; n < 2; n++ {
					if equals {
						args = append(args, "--"+c.name+"="+c.value)
					} else {
						args = append(args, "--"+c.name)
						if !c.boolean {
							args = append(args, c.value)
						}
					}
				}
				cmd.SetOut(&bytes.Buffer{})
				cmd.SetErr(&bytes.Buffer{})
				cmd.SetArgs(args)
				err := cmd.Execute()
				var runtime runtimeFailure
				if err == nil || errors.As(err, &runtime) || called {
					t.Fatalf("duplicate singleton reached operation: %q err=%v called=%t", args, err, called)
				}
			})
		}
	}
}

func TestTidyDefaultDuplicateSingletonMeaningUnchanged(t *testing.T) {
	called := false
	cmd := tidyCommandWith(func(_ context.Context, o workspace.TidyPlanOptions) workspace.TidyResult {
		called = true
		if o.Base != "second" {
			t.Fatal(o.Base)
		}
		return workspace.TidyResult{Status: "ok"}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "--base", "first", "--base=second"})
	if err := cmd.Execute(); err != nil || !called {
		t.Fatalf("default behavior changed: %v %t", err, called)
	}
}

func TestTidyV3RepeatableListsRemainLiteral(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	called := false
	cmd := tidyCommandWithV3Operations(nil, nil, func(_ context.Context, o workspace.V3PlanOptions) workspace.V3Result {
		called = true
		if len(o.Refs) != 2 || o.Refs[0] != "refs/heads/a,b" || o.Refs[1] != "refs/heads/second" || len(o.PRHints) != 2 || o.PRHints[0].OID != a || o.PRHints[1].OID != b {
			t.Fatalf("repeatable lists changed: %+v", o)
		}
		return workspace.V3Result{Status: "ok"}
	}, nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"plan", "--contract=3", "--base=HEAD", "--online", "--remote=origin", "--ref", "refs/heads/a,b", "--ref=refs/heads/second", "--pr", a + "=1", "--pr=" + b + "=2"})
	if err := cmd.Execute(); err != nil || !called {
		t.Fatalf("repeatable lists rejected: %v %t", err, called)
	}
}
