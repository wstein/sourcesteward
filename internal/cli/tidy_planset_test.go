// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/workspace"
)

const setID = "3f9c2a7e41b8"

// fakeSet replaces the apply seam and the terminal facts for one test.
func fakeSet(t *testing.T, result planset.ApplyResult, stdinTTY, stdoutTTY bool) *[]planset.ApplyRequest {
	t.Helper()
	var got []planset.ApplyRequest
	oldApply, oldTerm, oldFind, oldEnv := tidyApplyPlanSet, tidyTerminalFacts, tidyFindPlanSet, tidyEnviron
	tidyEnviron = func() []string { return nil }
	tidyApplyPlanSet = func(_ context.Context, r planset.ApplyRequest) planset.ApplyResult {
		got = append(got, r)
		return result
	}
	tidyTerminalFacts = func(any, any) (bool, bool) { return stdinTTY, stdoutTTY }
	tidyFindPlanSet = func([]string, string) (planset.Set, error) {
		return planset.Set{PlanID: setID, SubPlans: []planset.SubPlan{{ID: "a"}, {ID: "b"}}, Items: []planset.Item{{ID: "i"}, {ID: "j"}, {ID: "k"}}}, nil
	}
	t.Cleanup(func() {
		tidyApplyPlanSet, tidyTerminalFacts, tidyFindPlanSet, tidyEnviron = oldApply, oldTerm, oldFind, oldEnv
	})
	return &got
}

func okSet() planset.ApplyResult {
	return planset.ApplyResult{SchemaVersion: 1, Command: "tidy-apply-set", Status: "ok", PlanID: setID, Outcomes: []planset.Outcome{{SubPlan: "a", Repo: "r\nx", Kind: planset.KindBranches, Status: planset.StatusApplied}}}
}

func runTidy(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := tidyCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func isUsage(err error) bool {
	var failure runtimeFailure
	return err != nil && !errors.As(err, &failure)
}

func TestTidyApplyPlanIDWithYesAuthorizesAndReportsJSON(t *testing.T) {
	got := fakeSet(t, okSet(), false, false)
	out, err := runTidy(t, "", "apply", setID, "--yes", "--state-dir", "/s/one", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 {
		t.Fatalf("calls %d", len(*got))
	}
	r := (*got)[0]
	if r.PlanID != setID || !r.Authorize || len(r.StateDirs) == 0 || r.StateDirs[0] != "/s/one" || r.Mode.Mode != owner.ModeAgent || r.Applier == nil || r.Confirm == nil {
		t.Fatalf("%+v", r)
	}
	var res planset.ApplyResult
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Command != "tidy-apply-set" || len(res.Outcomes) != 1 {
		t.Fatalf("%s %v", out, err)
	}
}

func TestTidyApplyConfirmsOnTheControllingTerminalNeverOnStdin(t *testing.T) {
	got := fakeSet(t, okSet(), false, false)
	if _, err := runTidy(t, "codex 3f9c2a\n", "apply", setID, "--yes", "--state-dir", "/s"); err != nil {
		t.Fatal(err)
	}
	confirm := (*got)[0].Confirm
	if confirm == nil || reflect.ValueOf(confirm).Pointer() != reflect.ValueOf(owner.DevTTY).Pointer() {
		t.Fatal("the typed confirmation must be read from /dev/tty (owner.DevTTY), not from stdin")
	}
}

func TestTidyApplyUsageErrorsExitTwoAndChangeNothing(t *testing.T) {
	for name, args := range map[string][]string{
		"--yes without an id":          {"apply", "--yes"},
		"--yes with --plan":            {"apply", "--plan", "x", "--yes", "--state-dir", "/s"},
		"id with --plan":               {"apply", setID, "--plan", "x"},
		"id with --authorize":          {"apply", setID, "--authorize"},
		"id with --contract":           {"apply", setID, "--contract", "3"},
		"id with a legacy apply flag":  {"apply", setID, "--owner", "o"},
		"two ids":                      {"apply", setID, setID},
		"id that is not a plan set id": {"apply", "../x", "--yes"},
		"id of the wrong length":       {"apply", "3f9c", "--yes"},
		"upper case id":                {"apply", "3F9C2A7E41B8", "--yes"},
	} {
		got := fakeSet(t, okSet(), false, false)
		if _, err := runTidy(t, "", args...); !isUsage(err) || len(*got) != 0 {
			t.Errorf("%s: err %v calls %d", name, err, len(*got))
		}
	}
}

func TestTidyApplyWithoutYesAndWithoutTerminalIsNotAuthorized(t *testing.T) {
	refused := planset.ApplyResult{SchemaVersion: 1, Command: "tidy-apply-set", Status: "refused", Code: planset.CodeAuthorizationRequired, PlanID: setID, Outcomes: []planset.Outcome{}}
	got := fakeSet(t, refused, false, false)
	_, err := runTidy(t, "y\n", "apply", setID, "--state-dir", "/s")
	var failure runtimeFailure
	if !errors.As(err, &failure) || len(*got) != 1 || (*got)[0].Authorize {
		t.Fatalf("stdin must never authorize without a terminal: err %v calls %+v", err, *got)
	}
}

func TestTidyApplyPromptOnATerminal(t *testing.T) {
	for answer, want := range map[string]bool{"y\n": true, "n\n": false, "\n": false, "yes\n": false, "Y\n": false, "": false} {
		got := fakeSet(t, okSet(), true, true)
		out, err := runTidy(t, answer, "apply", setID, "--state-dir", "/s")
		if want {
			if err != nil || len(*got) != 1 || !(*got)[0].Authorize || (*got)[0].Mode.Mode != owner.ModeHuman {
				t.Errorf("%q: err %v calls %+v", answer, err, *got)
			}
			if !strings.Contains(out, "Apply plan "+setID+" (2 sub-plans, 3 items)? [y/N]") {
				t.Errorf("prompt: %q", out)
			}
			continue
		}
		var failure runtimeFailure
		if !errors.As(err, &failure) || len(*got) != 0 {
			t.Errorf("%q must cancel with exit 1 and apply nothing: err %v calls %d", answer, err, len(*got))
		}
	}
}

func TestTidyApplyYesIsNotAPromptAnswerOnATerminalEither(t *testing.T) {
	got := fakeSet(t, okSet(), true, true)
	out, err := runTidy(t, "", "apply", setID, "--yes", "--state-dir", "/s")
	if err != nil || len(*got) != 1 || !(*got)[0].Authorize || strings.Contains(out, "[y/N]") {
		t.Fatalf("--yes skips the prompt: err %v out %q", err, out)
	}
}

func TestTidyApplyOutcomeToExitCodeAndEscapedText(t *testing.T) {
	for _, status := range []string{"refused", "partial", "failed"} {
		res := okSet()
		res.Status, res.Code, res.Message = status, "c\x1b[31m", "m\nline"
		res.Outcomes[0].Status = planset.StatusStale
		fakeSet(t, res, false, false)
		out, err := runTidy(t, "", "apply", setID, "--yes", "--state-dir", "/s")
		var failure runtimeFailure
		if !errors.As(err, &failure) {
			t.Errorf("%s must exit 1: %v", status, err)
		}
		if strings.Contains(out, "\x1b") || strings.Contains(out, "r\nx") {
			t.Errorf("%s: control bytes reach the terminal: %q", status, out)
		}
	}
	fakeSet(t, okSet(), false, false)
	out, err := runTidy(t, "", "apply", setID, "--yes", "--state-dir", "/s")
	if err != nil || !strings.Contains(out, setID) {
		t.Fatalf("%q %v", out, err)
	}
}

func TestTidyApplyStateDirDefaultsToTheDerivedStores(t *testing.T) {
	env := t.TempDir()
	if err := os.Chmod(env, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STW_STATE_DIR", env)
	got := fakeSet(t, okSet(), false, false)
	if _, err := runTidy(t, "", "apply", setID, "--yes"); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || len((*got)[0].StateDirs) == 0 || (*got)[0].StateDirs[0] != env {
		t.Fatalf("calls %+v", *got)
	}
}

func TestTidyApplyLegacyFormIsUnchanged(t *testing.T) {
	got := fakeSet(t, okSet(), false, false)
	called := false
	cmd := tidyCommandWithOperations(nil, func(context.Context, workspace.TidyApplyOptions) workspace.TidyResult {
		called = true
		return workspace.TidyResult{Status: "ok"}
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"apply", "--plan", "p", "--state-dir", "/s", "--authorize"})
	if err := cmd.Execute(); err != nil || !called || len(*got) != 0 {
		t.Fatalf("legacy apply: %v called %t set calls %d", err, called, len(*got))
	}
}
