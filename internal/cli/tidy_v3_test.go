// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/wstein/sourcesteward/internal/workspace"
	"reflect"
	"strconv"
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

func TestTidyV3RetirementPlanSelectors(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprint(owned), func(t *testing.T) {
			called := false
			cmd := tidyCommandWithV3Operations(nil, nil, func(_ context.Context, o workspace.V3PlanOptions) workspace.V3Result {
				called = true
				if o.Path != "/fixture/source" || o.Base != "refs/heads/main" || o.StateDir != "/fixture/state" || o.Owner != "operator" || !o.RemoveRegistration || o.RemoveFiles != "/fixture/completed" || len(o.Refs) != 0 {
					t.Fatalf("retirement selectors changed: %+v", o)
				}
				if owned {
					if o.Task != "owned-task" || o.CompletedWorktree != "" || len(o.ProtectWorktrees) != 0 {
						t.Fatalf("owned provenance changed: %+v", o)
					}
				} else if o.Task != "" || o.CompletedWorktree != "/fixture/completed" || len(o.ProtectWorktrees) != 1 || o.ProtectWorktrees[0] != "/fixture/active" {
					t.Fatalf("completion declaration changed: %+v", o)
				}
				return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git"}
			}, nil)
			args := []string{"plan", "/fixture/source", "--contract", "3", "--base", "refs/heads/main", "--state-dir", "/fixture/state", "--owner", "operator", "--remove-registration", "--remove-files", "/fixture/completed"}
			if owned {
				args = append(args, "--task", "owned-task")
			} else {
				args = append(args, "--completed-worktree", "/fixture/completed", "--protect-worktree", "/fixture/active")
			}
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil || !called {
				t.Fatalf("valid retirement grammar rejected: %v %t", err, called)
			}
		})
	}
}

func TestTidyV3RetirementInvalidModesBeforeOperations(t *testing.T) {
	base := []string{"plan", "/fixture/source", "--contract", "3", "--base", "refs/heads/main", "--state-dir", "/fixture/state", "--owner", "operator", "--remove-registration", "--remove-files", "/fixture/completed"}
	cases := [][]string{
		append(append([]string{}, base...), "--task", "owned", "--completed-worktree", "/fixture/completed"),
		append(append([]string{}, base...), "--completed-worktree", "/fixture/completed", "--ref", "refs/heads/task"),
		append(append([]string{}, base...), "--completed-worktree", "/fixture/completed", "--online", "--tracking-remote", "origin"),
		append(append([]string{}, base...), "--completed-worktree", "/fixture/different"),
		append(append([]string{}, base...), "--task", "owned", "--protect-worktree", "/fixture/active"),
		{"plan", "--base", "HEAD", "--completed-worktree", "/fixture/completed"},
		{"plan", "--contract", "3", "--base", "HEAD", "--state-dir", "/fixture/state", "--completed-worktree", "/fixture/completed", "--owner", "operator", "--remove-registration"},
		{"apply", "--contract", "3", "--plan", "stored", "--state-dir", "/fixture/state", "--authorize", "--base", "refs/heads/main"},
		{"apply", "--contract", "3", "--plan", "stored", "--state-dir", "/fixture/state", "--authorize", "--remote", "origin"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			called := false
			cmd := tidyCommandWithV3Operations(nil, nil, func(context.Context, workspace.V3PlanOptions) workspace.V3Result {
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
				t.Fatalf("invalid retirement grammar reached operation: %v %t", err, called)
			}
		})
	}
}

// This verifies only typed CLI forwarding and result handling. Persistence,
// provider admission and actual native removal stay with their own conformance
// tests; injected callbacks here neither read a store nor perform any effect.
func TestTidyV3LegacyStoredPlanApplyAndRetryBoundary(t *testing.T) {
	var stored *workspace.V3Plan
	planRun := func(_ context.Context, o workspace.V3PlanOptions) workspace.V3Result {
		if o.CompletedWorktree != "/fixture/completed" || o.Owner != "operator" || len(o.ProtectWorktrees) != 1 || o.ProtectWorktrees[0] != "/fixture/active" || !o.Online || o.Remote != "origin" {
			t.Fatalf("plan declaration changed: %+v", o)
		}
		stored = &workspace.V3Plan{SchemaVersion: 3, PlanID: "stored-retirement", Mode: "legacy-retirement", StateDir: o.StateDir, Retirement: &workspace.V3Retirement{RemoveRegistration: true, RemoveFiles: o.CompletedWorktree}}
		return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git", Plan: stored}
	}
	var received []workspace.V3ApplyOptions
	applyRun := func(_ context.Context, o workspace.V3ApplyOptions) workspace.V3Result {
		received = append(received, o)
		if stored == nil || o.PlanID != stored.PlanID || o.StateDir != stored.StateDir || o.CompletedWorktree != "/fixture/completed" || o.Owner != "operator" || len(o.ProtectWorktrees) != 1 || o.ProtectWorktrees[0] != "/fixture/active" {
			t.Fatalf("stored selection/reassertion changed: %+v", o)
		}
		r := workspace.V3Result{SchemaVersion: 3, Command: "tidy-apply", Status: "refused", Backend: "git", Diagnostics: []workspace.Diagnostic{{Code: "authorization-required", Message: "Exact local and online acknowledgment required."}}}
		if o.Authorize && o.Online {
			r.Status = "ok"
			r.Plan = stored
			r.Diagnostics = []workspace.Diagnostic{}
		}
		return r
	}
	invoke := func(args ...string) error {
		t.Helper()
		cmd := tidyCommandWithV3Operations(nil, nil, planRun, applyRun)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(args)
		return cmd.Execute()
	}
	if err := invoke("plan", "/fixture/source", "--contract", "3", "--base", "refs/heads/main", "--state-dir", "/fixture/state", "--completed-worktree", "/fixture/completed", "--owner", "operator", "--remove-registration", "--remove-files", "/fixture/completed", "--protect-worktree", "/fixture/active", "--online", "--remote", "origin"); err != nil {
		t.Fatal(err)
	}
	args := []string{"apply", "--contract", "3", "--plan", stored.PlanID, "--state-dir", stored.StateDir, "--completed-worktree", "/fixture/completed", "--owner", "operator", "--protect-worktree", "/fixture/active"}
	for _, ack := range [][]string{nil, {"--authorize"}, {"--online"}} {
		var runtime runtimeFailure
		if err := invoke(append(append([]string{}, args...), ack...)...); !errors.As(err, &runtime) {
			t.Fatalf("missing acknowledgment lost refusal: %v", err)
		}
	}
	complete := append(append([]string{}, args...), "--authorize", "--online")
	for retry := 0; retry < 2; retry++ {
		if err := invoke(complete...); err != nil {
			t.Fatalf("completed apply/retry envelope failed: %v", err)
		}
	}
	if len(received) != 5 || received[0].Authorize || received[0].Online || !received[1].Authorize || received[1].Online || received[2].Authorize || !received[2].Online || !received[3].Authorize || !received[3].Online || !received[4].Authorize || !received[4].Online {
		t.Fatalf("CLI invented/dropped acknowledgment: %+v", received)
	}
}

// Synthetic renderer field coverage only; schema/store authority is tested separately.
func TestTidyV3RetirementHumanJSONAgreement(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(fmt.Sprint(owned), func(t *testing.T) {
			kind, mode := "legacy-completion", "legacy-retirement"
			if owned {
				kind, mode = "owned", "owned-retirement"
			}
			retirement := &workspace.V3Retirement{ActionID: "retirement-selected", Provenance: workspace.V3Provenance{Kind: kind}, Destination: workspace.V3PhysicalIdentity{Path: "/fixture/completed"}, Parent: workspace.V3PhysicalIdentity{Path: "/fixture"}, Registration: workspace.V3PhysicalIdentity{Path: "/fixture/source/.git/worktrees/completed"}, RegistrationParent: workspace.V3PhysicalIdentity{Path: "/fixture/source/.git/worktrees"}, RegistrationSiblings: []workspace.V3PhysicalIdentity{{Path: "/fixture/source/.git/worktrees/active"}}, Target: workspace.NativeIdentity{CommitID: strings.Repeat("b", 40), HeadRef: "refs/heads/completed"}, RemoveRegistration: true, RemoveFiles: "/fixture/completed", ContentPolicy: "git-v3", Disk: workspace.V3Manifest{Complete: true, Digest: strings.Repeat("c", 64), Entries: []workspace.V3ManifestEntry{}}, Metadata: workspace.V3Manifest{Complete: true, Digest: strings.Repeat("d", 64), Entries: []workspace.V3ManifestEntry{}}, Integration: workspace.V3CommitProof{OID: strings.Repeat("b", 40), Proof: "native-ancestry", Decision: "delete"}}
			if owned {
				retirement.Provenance.Owned = &workspace.V3OwnedProvenance{Task: workspace.TaskRecord{TaskID: "original-task", Owner: "operator"}, CreationJournalDigest: strings.Repeat("e", 64), LockReason: "stw:original-task"}
			} else {
				retirement.Provenance.Legacy = &workspace.V3LegacyProvenance{Owner: "operator", Completed: true, SameUserAuthority: true, SelectedPath: "/fixture/completed", ProtectedRoots: []string{"/fixture/active"}}
			}
			result := workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git", Plan: &workspace.V3Plan{Mode: mode, Retirement: retirement, Effects: []string{"Remove exact native worktree registration for /fixture/completed.", "Remove exact verified worktree files at /fixture/completed."}, RecoveryLimits: []string{"Recovery preserves selected committed history, not index, reflog, untracked data or automatic restoration."}}}
			var human bytes.Buffer
			if err := renderTidyV3Text(&human, result); err != nil {
				t.Fatal(err)
			}
			cmd := tidyCommandWithV3Operations(nil, nil, nil, nil)
			cmd.SetContext(context.Background())
			var structured bytes.Buffer
			cmd.SetOut(&structured)
			if err := writeTidyV3Result(cmd, result, true); err != nil {
				t.Fatal(err)
			}
			var decoded workspace.V3Result
			if err := json.Unmarshal(structured.Bytes(), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Plan == nil || decoded.Plan.Retirement == nil || decoded.Plan.Retirement.Provenance.Kind != kind || decoded.Plan.Retirement.RegistrationParent.Path != retirement.RegistrationParent.Path {
				t.Fatalf("structured retirement facts changed: %s", structured.String())
			}
			want := []string{kind, "/fixture/completed", "/fixture/source/.git/worktrees/completed", "/fixture/source/.git/worktrees/active", "operator", "git-v3", strings.Repeat("b", 40), strings.Repeat("c", 64), strings.Repeat("d", 64), "Remove exact native worktree registration for /fixture/completed.", "Remove exact verified worktree files at /fixture/completed.", "Recovery preserves selected committed history, not index, reflog, untracked data or automatic restoration."}
			if owned {
				want = append(want, "original-task", "stw:original-task", strings.Repeat("e", 64))
			} else {
				want = append(want, "/fixture/active")
			}
			for _, fact := range want {
				if !strings.Contains(human.String(), fact) {
					t.Errorf("retirement fact %q missing from human output: %s", fact, human.String())
				}
			}
		})
	}
}

func TestTidyV3RetirementTextEscapesCompleteFacts(t *testing.T) {
	unsafe := "/fixture/line\n\x1b[31m\u202eactor"
	r := workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git", Plan: &workspace.V3Plan{Retirement: &workspace.V3Retirement{Destination: workspace.V3PhysicalIdentity{Path: unsafe}, Provenance: workspace.V3Provenance{Kind: "legacy-completion", Legacy: &workspace.V3LegacyProvenance{Owner: unsafe, ProtectedRoots: []string{unsafe}}}}}}
	var human bytes.Buffer
	if err := renderTidyV3Text(&human, r); err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"\x1b", "\u202e", unsafe} {
		if strings.Contains(human.String(), control) {
			t.Fatalf("unescaped control %q in human evidence", control)
		}
	}
	var facts string
	for _, line := range strings.Split(human.String(), "\n") {
		if strings.HasPrefix(line, "Retirement facts: ") {
			var err error
			facts, err = strconv.Unquote(strings.TrimPrefix(line, "Retirement facts: "))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	var decoded workspace.V3Retirement
	if err := json.Unmarshal([]byte(facts), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, *r.Plan.Retirement) {
		t.Fatal("quoted evidence lost typed retirement facts")
	}
}

func tidyV3HintRun(args ...string) (workspace.V3PlanOptions, error) {
	var got workspace.V3PlanOptions
	cmd := tidyCommandWithV3Operations(nil, nil, func(_ context.Context, o workspace.V3PlanOptions) workspace.V3Result {
		got = o
		return workspace.V3Result{SchemaVersion: 3, Command: "tidy-plan", Status: "ok", Backend: "git"}
	}, nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"plan", "--contract", "3", "--base", "HEAD", "--online", "--remote", "origin"}, args...))
	err := cmd.Execute()
	return got, err
}

func TestTidyV3IdenticalRepeatedHintIsAcceptedOnce(t *testing.T) {
	oid := strings.Repeat("a", 40)
	got, err := tidyV3HintRun("--pr", oid+"=12", "--pr", oid+"=12")
	if err != nil || len(got.PRHints) != 1 || got.PRHints[0].OID != oid || got.PRHints[0].Number != 12 {
		t.Fatalf("%v %+v", err, got.PRHints)
	}
}

func TestTidyV3HintUsageErrorsNameTheSelector(t *testing.T) {
	oid := strings.Repeat("a", 40)
	for name, tc := range map[string]struct {
		args []string
		want []string
	}{
		"conflict":  {[]string{"--pr", oid + "=1", "--pr", oid + "=2"}, []string{`--pr "` + oid + `=1"`, `--pr "` + oid + `=2"`}},
		"no number": {[]string{"--pr", oid + "="}, []string{`--pr "` + oid + `="`}},
		"short oid": {[]string{"--pr", "abc=1"}, []string{`--pr "abc=1"`}},
		"escaped":   {[]string{"--pr", "x\ny=1"}, []string{`--pr "x\ny=1"`}},
		"zero":      {[]string{"--pr", oid + "=0"}, []string{`--pr "` + oid + `=0"`}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tidyV3HintRun(tc.args...)
			if err == nil {
				t.Fatal("accepted")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("%q lacks %q", err, want)
				}
			}
		})
	}
	_, err := tidyV3HintRun("--pr", strings.Repeat("z", 10000))
	if err == nil || len(err.Error()) > 4200 {
		t.Fatalf("unbounded selector message: %d", len(err.Error()))
	}
}

func TestTidyV3HintSameNumberSpelledTwoWaysIsIdempotent(t *testing.T) {
	oid := strings.Repeat("a", 40)
	got, err := tidyV3HintRun("--pr", oid+"=07", "--pr", oid+"=7")
	if err != nil || len(got.PRHints) != 1 || got.PRHints[0].Number != 7 {
		t.Fatalf("%v %+v", err, got.PRHints)
	}
	if _, err = tidyV3HintRun("--pr", oid+"=07", "--pr", oid+"=8"); err == nil || !strings.Contains(err.Error(), `"`+oid+`=07"`) {
		t.Fatalf("%v", err)
	}
}

func TestTidyV3QuoteBoundsOnRuneAndEscapeBoundaries(t *testing.T) {
	for _, v := range []string{strings.Repeat("é", 5000), strings.Repeat("\n", 5000), strings.Repeat("\xff", 5000), strings.Repeat("z", 5000)} {
		q := tidyV3Quote(v)
		if len(q) > 4096 || !strings.HasPrefix(q, `"`) || !strings.HasSuffix(q, `"`) || !strings.HasSuffix(q, `..."`) {
			t.Fatalf("len=%d head=%q tail=%q", len(q), q[:8], q[len(q)-8:])
		}
		inner := strings.TrimSuffix(q[1:len(q)-1], "...")
		if _, err := strconv.Unquote(`"` + inner + `"`); err != nil {
			t.Fatalf("cut inside an escape or rune: %v", err)
		}
	}
	if q := tidyV3Quote("a\nb"); q != `"a\nb"` {
		t.Fatal(q)
	}
}
