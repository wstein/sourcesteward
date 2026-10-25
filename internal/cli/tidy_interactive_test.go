// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
)

// interactive captures what the wizard hands to the apply path and to the typed confirmation.
type interactive struct {
	*wizardWorld
	applied []planset.ApplyRequest
	// confirmOpened counts the openings of the controlling terminal for the typed confirmation.
	confirmOpened int
}

func newInteractive(t *testing.T, stdinTTY, stdoutTTY bool, environ ...string) *interactive {
	t.Helper()
	i := &interactive{wizardWorld: newWizardWorld(t, stdinTTY, stdoutTTY, environ...)}
	oldApply, oldConfirm := tidyApplyPlanSet, tidyConfirmOpener
	t.Cleanup(func() { tidyApplyPlanSet, tidyConfirmOpener = oldApply, oldConfirm })
	tidyApplyPlanSet = func(_ context.Context, r planset.ApplyRequest) planset.ApplyResult {
		i.applied = append(i.applied, r)
		return okSet()
	}
	tidyConfirmOpener = func() (io.ReadWriteCloser, error) {
		i.confirmOpened++
		return nil, owner.ErrNoTerminal
	}
	return i
}

func storedSets(t *testing.T, store string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(store, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.Contains(path, "plansets") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// Bare `stw tidy` on a terminal shows the overview, asks which rows, stores the plan set of the
// choice, says nothing has been changed, and asks whether to apply; the default is no.
func TestInteractiveStoresTheChosenPlanSetAndDoesNotApplyByDefault(t *testing.T) {
	i := newInteractive(t, true, true)
	out, err := runTidy(t, "a\n\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Analysed 2 repositories", "Apply which?", "Stored plan set ", "(5 effects in 2 repositories)", "Nothing has been changed", "Apply it now?", "stw tidy apply "} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing:\n%s", want, out)
		}
	}
	if len(i.applied) != 0 || len(i.planner.specs) == 0 {
		t.Errorf("applied %d, planned %d", len(i.applied), len(i.planner.specs))
	}
}

// Only an explicit "y" on that plan id runs the very same apply path, with the plan id as the
// authorization and the plan store first.
func TestInteractiveAppliesOnlyAfterAnExplicitYes(t *testing.T) {
	i := newInteractive(t, true, true)
	out, err := runTidy(t, "a\ny\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(i.applied) != 1 {
		t.Fatalf("applied %d\n%s", len(i.applied), out)
	}
	r := i.applied[0]
	if len(r.PlanID) != 12 || !r.Authorize || r.Mode.Mode != owner.ModeHuman || len(r.StateDirs) == 0 || r.StateDirs[0] != i.prep.PlanStore || r.Confirm == nil || r.Applier == nil {
		t.Errorf("%+v", r)
	}
	if !strings.Contains(out, "Tidy plan set") {
		t.Errorf("the apply report is printed:\n%s", out)
	}
	for _, answer := range []string{"a\nyes\nyes\nyes\n", "a\nn\n", "a\n", "a\nY\nY\nY\n"} {
		i := newInteractive(t, true, true)
		_, err := runTidy(t, answer)
		if len(i.applied) != 0 {
			t.Errorf("%q applied %d (err %v)", answer, len(i.applied), err)
		}
		// Declining, an invalid answer repeated and the end of input are not failures of the run.
		if answer == "a\n" || answer == "a\nn\n" {
			if err != nil {
				t.Errorf("%q: %v", answer, err)
			}
		}
	}
}

// Nothing chosen (default, none, end of input) stores nothing and exits 0.
func TestInteractiveNothingChosenStoresNothing(t *testing.T) {
	for _, in := range []string{"\n", "n\n", ""} {
		i := newInteractive(t, true, true)
		out, err := runTidy(t, in)
		if err != nil || len(i.planner.specs) != 0 || len(i.applied) != 0 || len(storedSets(t, i.prep.PlanStore)) != 0 || strings.Contains(out, "Stored plan set") {
			t.Errorf("%q: err %v\n%s", in, err, out)
		}
	}
}

// Three invalid answers end the run with exit 1 and nothing stored.
func TestInteractiveThreeInvalidAnswers(t *testing.T) {
	i := newInteractive(t, true, true)
	_, err := runTidy(t, "x\ny\nz\na\n")
	var failure runtimeFailure
	if !errors.As(err, &failure) || len(i.planner.specs) != 0 || len(i.applied) != 0 {
		t.Errorf("%v planned %d", err, len(i.planner.specs))
	}
}

// Without a terminal the wizard does not prompt: it prints the overview, stores nothing, hints at
// --save and --json, exits 0, and never reads stdin.
func TestInteractiveWithoutATerminalOnlyPlans(t *testing.T) {
	for _, tc := range []struct{ in, out bool }{{false, false}, {false, true}, {true, false}} {
		i := newInteractive(t, tc.in, tc.out)
		cmd := tidyCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(neverRead{t})
		cmd.SetArgs(nil)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%+v: %v", tc, err)
		}
		text := out.String()
		if !strings.Contains(text, "Analysed 2 repositories") || strings.Contains(text, "Apply which?") || !strings.Contains(text, "stw tidy plan --save") || !strings.Contains(text, "--json") {
			t.Errorf("%+v:\n%s", tc, text)
		}
		if len(i.planner.specs) != 0 || len(i.applied) != 0 {
			t.Errorf("%+v: planned %d applied %d", tc, len(i.planner.specs), len(i.applied))
		}
	}
}

type neverRead struct{ t *testing.T }

func (n neverRead) Read([]byte) (int, error) {
	n.t.Error("stdin was read without a terminal")
	return 0, io.EOF
}

// An agent marker on a terminal is agent mode: no prompt, plan only.
func TestInteractiveAgentModeDoesNotPrompt(t *testing.T) {
	i := newInteractive(t, true, true, "CLAUDECODE=1")
	cmd := tidyCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(neverRead{t})
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil || strings.Contains(out.String(), "Apply which?") || len(i.planner.specs) != 0 {
		t.Errorf("%v\n%s", err, out.String())
	}
}

// A foreign row can only be chosen with the typed confirmation on the controlling terminal: without
// one the choice fails with exit 1 and nothing is stored or applied.
func TestInteractiveForeignChoiceNeedsTheTypedConfirmation(t *testing.T) {
	i := newInteractive(t, true, true)
	_, err := runTidy(t, "3\n")
	var failure runtimeFailure
	if !errors.As(err, &failure) || len(i.applied) != 0 || len(storedSets(t, i.prep.PlanStore)) != 0 || i.confirmOpened == 0 {
		t.Errorf("err %v, terminal opened %d times", err, i.confirmOpened)
	}
}

// The details of a row and the failure messages are escaped; the lock of a decision row is in the
// detail the confirmation shows.
func TestInteractiveDetailsAreEscaped(t *testing.T) {
	i := newInteractive(t, true, true)
	i.res.Candidates[3].Evidence = "stw lock \x1b[31mforged\x1b[0m\nline"
	out, err := runTidy(t, "d 4\nn\n")
	if err != nil {
		t.Fatal(err)
	}
	// The detail is printed right after the question (the answer is not echoed by the test input).
	detail := ""
	if k := strings.LastIndex(out, "default n): [4] "); k >= 0 {
		detail, _, _ = strings.Cut(out[k:], "\n")
	}
	if strings.Contains(out, "\x1b") || !strings.Contains(detail, `stw lock \x1b[31mforged`) {
		t.Errorf("detail line %q\n%q", detail, out)
	}
}

// Bare `stw tidy --help` still prints help; arguments are still a usage error.
func TestBareTidyHelpAndArguments(t *testing.T) {
	newInteractive(t, true, true)
	out, err := runTidy(t, "", "--help")
	if err != nil || !strings.Contains(out, "Usage:") {
		t.Errorf("%v %q", err, out)
	}
	if _, err = runTidy(t, "", "stray"); !isUsage(err) {
		t.Errorf("%v", err)
	}
}
