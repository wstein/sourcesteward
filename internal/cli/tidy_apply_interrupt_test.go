// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

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
