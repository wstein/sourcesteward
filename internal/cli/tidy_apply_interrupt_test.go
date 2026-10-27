// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
)

var storedPlanID = regexp.MustCompile(`Stored plan set ([0-9a-f]{12})`)

// planIDIn is the id of the plan set the run stored, read from its output.
func planIDIn(t *testing.T, out string) string {
	t.Helper()
	m := storedPlanID.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no stored plan set in the output:\n%s", out)
	}
	return m[1]
}

// answerApply types the right words and says y at the apply question.
func answerApply(q string) string {
	if strings.Contains(q, "Apply it now?") {
		return "y"
	}
	return rightWord(q)
}

// After the apply has started, the analysis wording "nothing was changed" is false. A cancel
// during a partial apply must say that the result shows what was applied (exit 1).
func TestJanitorCancelDuringPartialApplyDoesNotClaimNothingChanged(t *testing.T) {
	j := newJanitor(t)
	ctx, cancel := context.WithCancel(context.Background())
	tidyApplyPlanSet = func(context.Context, planset.ApplyRequest) planset.ApplyResult {
		cancel()
		result := okSet()
		result.Status = "partial"
		return result
	}
	out, err := j.runContext(ctx, "a\n\n", answerApply)
	var failure runtimeFailure
	if !errors.As(err, &failure) || !strings.Contains(out, `Status: "partial"`) {
		t.Fatalf("exit 1 with the partial result printed: %v\n%s", err, out)
	}
	for _, bad := range []string{"nothing was changed", "nothing was applied", "context canceled", "during the analysis"} {
		if strings.Contains(err.Error(), bad) {
			t.Fatalf("false claim %q: %v", bad, err)
		}
	}
	id := planIDIn(t, out)
	for _, want := range []string{"interrupted during the apply of plan set " + id, "shows what was applied", "stw tidy apply " + id} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("%q missing: %v", want, err)
		}
	}
}

// The --timeout deadline bounds the analysis only. A successful apply that outlasts it is exit 0.
func TestJanitorTimeoutElapsedDuringSuccessfulApplyIsSuccess(t *testing.T) {
	j := newJanitor(t)
	tidyApplyPlanSet = func(context.Context, planset.ApplyRequest) planset.ApplyResult {
		time.Sleep(1200 * time.Millisecond)
		return okSet()
	}
	out, err := j.runContext(context.Background(), "a\n\n", answerApply, "--timeout", "1s")
	if err != nil || !strings.Contains(out, `Status: "ok"`) {
		t.Fatalf("a completed apply must exit 0: %v\n%s", err, out)
	}
}

// The --rows path reaches the same apply code: a cancel during a partial apply says the same.
func TestRowsCancelDuringPartialApplyDoesNotClaimNothingChanged(t *testing.T) {
	i := newInteractive(t, true, true)
	i.tty.Reader = strings.NewReader("y\n")
	ctx, cancel := context.WithCancel(context.Background())
	tidyApplyPlanSet = func(context.Context, planset.ApplyRequest) planset.ApplyResult {
		cancel()
		result := okSet()
		result.Status = "partial"
		return result
	}
	cmd := tidyCommand()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("a\n"))
	cmd.SetArgs([]string{"--rows"})
	err := cmd.ExecuteContext(ctx)
	var failure runtimeFailure
	if !errors.As(err, &failure) || !strings.Contains(err.Error(), "interrupted during the apply of plan set "+planIDIn(t, out.String())) || strings.Contains(err.Error(), "nothing was") || strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("%v\n%s", err, out.String())
	}
}

// An interrupt that arrived while the apply question was open is an interruption, whatever the
// answer was: the apply context carries no deadline, so nothing else would stop the apply.
func TestJanitorInterruptAtTheApplyQuestionAppliesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		timeout string
		stop    func(cancel func())
		want    string
	}{
		"cancel":          {"", func(cancel func()) { cancel() }, "interrupted"},
		"timeout elapsed": {"1s", func(func()) { time.Sleep(1100 * time.Millisecond) }, "interrupted"},
	} {
		t.Run(name, func(t *testing.T) {
			j := newJanitor(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			applies := 0
			tidyApplyPlanSet = func(context.Context, planset.ApplyRequest) planset.ApplyResult {
				applies++
				return okSet()
			}
			var args []string
			if tc.timeout != "" {
				args = []string{"--timeout", tc.timeout}
			}
			out, err := j.runContext(ctx, "a\n\n", func(q string) string {
				if strings.Contains(q, "Apply it now?") {
					tc.stop(cancel)
					return "y"
				}
				return rightWord(q)
			}, args...)
			var failure runtimeFailure
			if !errors.As(err, &failure) || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "nothing was applied") || applies != 0 {
				t.Fatalf("applies=%d err=%v\n%s", applies, err, out)
			}
		})
	}
}

// With --rows a foreign row needs its typed words once; the same words cover the apply that follows
// "y" (SC-014), so the real plan-set apply gets the receipt and reaches the backend.
func TestRowsForeignRowTypedWordsAreAskedOnceAndCoverTheApply(t *testing.T) {
	j := newJanitor(t)
	backend := &p4bCountingApplier{}
	tidyApplyPlanSet = func(ctx context.Context, req planset.ApplyRequest) planset.ApplyResult {
		req.Applier = backend
		return planset.Apply(ctx, req)
	}
	out, tty, err := j.run("1,3\n", answerApply, "--rows")
	typed := 0
	for _, q := range tty.prompts {
		if strings.Contains(q, "Type ") {
			typed++
		}
	}
	if err != nil || backend.calls == 0 || typed != 1 {
		t.Fatalf("typed=%d backend calls=%d err=%v prompts=%q\n%s", typed, backend.calls, err, tty.prompts, out)
	}
}

// Once the plan set is stored, a missing terminal at the apply question must not send the user to
// `stw tidy plan --save` again: the next step is `stw tidy apply ID` from a terminal.
func TestNoTerminalAfterTheStoreNamesTheStoredPlanSet(t *testing.T) {
	for name, args := range map[string][]string{"janitor": nil, "rows": {"--rows"}} {
		t.Run(name, func(t *testing.T) {
			j := newJanitor(t)
			input := "a\n\n"
			if name == "rows" {
				input = "1,3\n"
			}
			out := &bytes.Buffer{}
			tty := &answeringTTY{out: out, reply: answerApply}
			tidyConfirmOpener = func() (io.ReadWriteCloser, error) {
				if len(storedSets(t, j.prep.PlanStore)) > 0 {
					return nil, owner.ErrNoTerminal
				}
				return tty, nil
			}
			cmd := tidyCommand()
			cmd.SetOut(out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetIn(strings.NewReader(input))
			cmd.SetArgs(args)
			err := cmd.Execute()
			id := planIDIn(t, out.String())
			var failure runtimeFailure
			if !errors.As(err, &failure) || !strings.Contains(err.Error(), "no controlling terminal") || strings.Contains(err.Error(), "plan --save") ||
				!strings.Contains(err.Error(), "stw tidy apply "+id) || !strings.Contains(err.Error(), "nothing was applied") {
				t.Fatalf("%v", err)
			}
		})
	}
}

func TestSeparateApplyWithoutTerminalDoesNotSuggestSavingAgain(t *testing.T) {
	got := fakeSet(t, okSet(), true, true)
	old := tidyConfirmOpener
	tidyConfirmOpener = func() (io.ReadWriteCloser, error) { return nil, owner.ErrNoTerminal }
	t.Cleanup(func() { tidyConfirmOpener = old })
	_, err := runTidy(t, "y\n", "apply", setID, "--state-dir", "/s")
	if err == nil || strings.Contains(err.Error(), "plan --save") || !strings.Contains(err.Error(), "from a terminal") || !strings.Contains(err.Error(), "stw tidy apply "+setID) || len(*got) != 0 {
		t.Fatalf("%v calls=%v", err, *got)
	}
}
