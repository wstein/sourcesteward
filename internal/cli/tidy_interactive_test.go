// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
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

// storedSets lists every regular file in the state directory: the fake planner writes none, so
// any file is a stored plan set, whatever the directory of the store is called.
func storedSets(t *testing.T, store string) []string {
	t.Helper()
	var out []string
	if err := filepath.WalkDir(store, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// The store keeps a lock file of its own; every other file is a stored record.
		if !d.IsDir() && d.Name() != "lock" {
			out = append(out, path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// The helper itself sees a stored plan set (it is not vacuous).
func TestInteractiveStoredSetsHelperSeesAStoredSet(t *testing.T) {
	i := newInteractive(t, true, true)
	if _, err := runTidy(t, "a\n\n", "--rows"); err != nil {
		t.Fatal(err)
	}
	if got := storedSets(t, i.prep.PlanStore); len(got) != 1 {
		t.Errorf("stored files: %v", got)
	}
}

// Bare `stw tidy` on a terminal shows the overview, asks which rows, stores the plan set of the
// choice, says nothing has been changed, and asks whether to apply; the default is no.
func TestInteractiveStoresTheChosenPlanSetAndDoesNotApplyByDefault(t *testing.T) {
	i := newInteractive(t, true, true)
	out, err := runTidy(t, "a\n\n", "--rows")
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
	out, err := runTidy(t, "a\ny\n", "--rows")
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
		_, err := runTidy(t, answer, "--rows")
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
		out, err := runTidy(t, in, "--rows")
		if err != nil || len(i.planner.specs) != 0 || len(i.applied) != 0 || len(storedSets(t, i.prep.PlanStore)) != 0 || strings.Contains(out, "Stored plan set") {
			t.Errorf("%q: err %v\n%s", in, err, out)
		}
	}
}

// Three invalid answers end the run with exit 1 and nothing stored.
func TestInteractiveThreeInvalidAnswers(t *testing.T) {
	i := newInteractive(t, true, true)
	_, err := runTidy(t, "x\ny\nz\na\n", "--rows")
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
		if !strings.Contains(text, "Workspace /ws") || strings.Contains(text, "Apply which?") || !strings.Contains(text, "stw tidy plan --save") || !strings.Contains(text, "--json") {
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
	_, err := runTidy(t, "4\n", "--rows")
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
	out, err := runTidy(t, "d 5\nn\n", "--rows")
	if err != nil {
		t.Fatal(err)
	}
	// The detail is printed right after the question (the answer is not echoed by the test input).
	detail := ""
	if k := strings.LastIndex(out, "default n): [5] "); k >= 0 {
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

// numberOf is the number the prompt gives to a candidate.
func numberOf(t *testing.T, i *interactive, id string) int {
	t.Helper()
	for _, n := range render.Numbered(render.Build(render.Input{Result: i.res, Repos: i.prep.Meta})) {
		if n.Candidate.ID == id {
			return n.Number
		}
	}
	t.Fatalf("%s is not numbered", id)
	return 0
}

func addCandidate(i *interactive, c tidywizard.Candidate) {
	i.res.Candidates = append(i.res.Candidates, c)
}

// An own decision row chosen and confirmed ("y") is planned with the confirmation; the lock is named
// (stw:<task>, the path), escaped, in the question.
func TestInteractiveDecisionRowNamesTheLockAndNeedsItsConfirmation(t *testing.T) {
	i := newInteractive(t, true, true)
	dec := wcand("a", tidywizard.KindWorktree, "lock1", tidywizard.VerdictDecision, "claude", owner.SourceTaskRecord, owner.ScopeOwn)
	dec.Reason, dec.Evidence, dec.Task = "locked-stw", "stw lock whose creation record does not match", "t9\x1b"
	dec.Path = "/w/lock1\x1b[2J"
	dec.Proof, dec.Effects = tidywizard.ProofAncestor, []string{"remove-registration", "remove-files"}
	addCandidate(i, dec)
	n := numberOf(t, i, dec.ID)
	out, err := runTidy(t, strconv.Itoa(n)+"\ny\nn\n", "--rows")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"decision row", "stw lock whose creation record does not match", `stw:t9\x1b`, `/w/lock1\x1b[2J`} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from the confirmation:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("raw escape in the output")
	}
	if got := storedSets(t, i.prep.PlanStore); len(got) != 1 {
		t.Errorf("the confirmed decision row was not planned: %v", got)
	}
	// Declined: nothing is stored.
	i = newInteractive(t, true, true)
	addCandidate(i, dec)
	if _, err = runTidy(t, strconv.Itoa(n)+"\nn\n", "--rows"); err != nil || len(storedSets(t, i.prep.PlanStore)) != 0 {
		t.Errorf("%v %v", err, storedSets(t, i.prep.PlanStore))
	}
}

// Owner labels are outside text: the owner questions show them escaped.
func TestInteractiveOwnerLabelsAreEscaped(t *testing.T) {
	i := newInteractive(t, true, true)
	o := wcand("a", tidywizard.KindWorktree, "hostile", tidywizard.VerdictSafe, "co\x1b[31mdex\n", owner.SourcePathConvention, owner.ScopeOther)
	addCandidate(i, o)
	out, err := runTidy(t, "o\nn\nn\nn\n", "--rows")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `of owner co\x1b[31mdex\n?`) || strings.Contains(out, "\x1b") {
		t.Errorf("%q", out)
	}
}

// Declining the plan set does not hide incomplete evidence: the overview's exit rules still apply.
func TestInteractiveDecliningKeepsTheExitRules(t *testing.T) {
	i := newInteractive(t, true, true)
	i.res.Repositories[1] = tidywizard.RepoResult{Name: "b", State: "error", Reason: "boom", Notices: []string{}}
	for _, in := range []string{"n\n", "\n", "a\nn\n"} {
		_, err := runTidy(t, in, "--rows")
		var failure runtimeFailure
		if !errors.As(err, &failure) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

// Three invalid answers at the apply question: exit 1, and the message says that the plan set is
// stored and that nothing was applied.
func TestInteractiveInvalidAnswersAtTheApplyQuestion(t *testing.T) {
	i := newInteractive(t, true, true)
	_, err := runTidy(t, "a\nx\nx\nx\n", "--rows")
	var failure runtimeFailure
	files := storedSets(t, i.prep.PlanStore)
	if !errors.As(err, &failure) || len(files) != 1 || len(i.applied) != 0 {
		t.Fatalf("%v %v", err, files)
	}
	id := strings.TrimSuffix(filepath.Base(files[0]), ".json")
	if !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "stored") || !strings.Contains(err.Error(), "nothing was applied") || strings.Contains(err.Error(), "nothing was chosen") || !strings.Contains(err.Error(), "too many invalid answers at the apply question") {
		t.Errorf("%v", err)
	}
}

// A plan set that cannot be stored is an error, never a silent "stored".
func TestInteractiveStoreFailureIsReported(t *testing.T) {
	i := newInteractive(t, true, true)
	if err := os.Chmod(i.prep.PlanStore, 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := runTidy(t, "a\ny\n", "--rows")
	var failure runtimeFailure
	if !errors.As(err, &failure) || strings.Contains(out, "Stored plan set") || len(i.applied) != 0 {
		t.Errorf("%v\n%s", err, out)
	}
}

// An interrupt at a prompt is exit 1 naming the interruption, never the raw context error.
type cancelOnRead struct {
	r      *strings.Reader
	cancel func()
	after  int
	reads  int
}

func (c *cancelOnRead) Read(p []byte) (int, error) {
	c.reads++
	if c.reads > c.after {
		c.cancel()
	}
	// One line per read, like a terminal: the later questions are read later.
	var line []byte
	for len(line) < len(p) {
		b, err := c.r.ReadByte()
		if err != nil {
			if len(line) == 0 {
				return 0, err
			}
			break
		}
		line = append(line, b)
		if b == '\n' {
			break
		}
	}
	return copy(p, line), nil
}

func TestInteractiveInterruptIsReportedAsAnInterruption(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		after int
		want  string
	}{
		{"during the first question", "a\n", 0, "interrupted; nothing was changed"},
		{"at the apply question", "a\ny\n", 1, "the plan set"},
	} {
		i := newInteractive(t, true, true)
		ctx, cancel := context.WithCancel(context.Background())
		in := &cancelOnRead{r: strings.NewReader(tc.input), cancel: cancel, after: tc.after}
		cmd := tidyCommand()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(in)
		cmd.SetArgs([]string{"--rows"})
		err := cmd.ExecuteContext(ctx)
		var failure runtimeFailure
		if !errors.As(err, &failure) || !strings.Contains(err.Error(), "interrupted") || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "context canceled") || len(i.applied) != 0 {
			t.Errorf("%s: %v (applied %d)", tc.name, err, len(i.applied))
		}
		if tc.after == 1 && !strings.Contains(err.Error(), "nothing was applied") {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// The sample session of docs/tidy.md and of the specification is the real output of the wizard on
// this fixture (answers a and n); the plan id (12 hex characters of a SHA-256 over the plan content) is normalized to 3f9c2a7e41b8 so the sample stays stable.
func TestTidyExampleMatchesTheDocs(t *testing.T) {
	newInteractive(t, true, true)
	out, err := runTidy(t, "a\nn\n", "--rows")
	if err != nil {
		t.Fatal(err)
	}
	out = regexp.MustCompile(`\b[0-9a-f]{12}\b`).ReplaceAllString(out, "3f9c2a7e41b8")
	lines := strings.Split(out, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	want := "$ stw tidy\n" + strings.Join(lines, "\n") + "\n```\n"
	for _, p := range []string{"../../docs/tidy.md", "../../specs/041-tidy-wizard/spec.md"} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("%s does not contain the real sample session:\n%s", p, want)
		}
	}
}
