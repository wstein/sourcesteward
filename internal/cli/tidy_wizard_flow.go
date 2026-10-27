// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/tidywizard/prompt"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

// tidyTerminalWidth is the width of the terminal the wizard writes to, in columns; zero when it is
// not known. A seam so that tests need no terminal.
var tidyTerminalWidth = func(out io.Writer) int {
	cols, _ := stwsync.TerminalSize(out)
	return cols
}

// wizardApplyStep receives the stored selection and its same-run confirmation receipt.
// The terminal apply step obtains explicit authorization before entering the shared apply path.
type wizardApplyStep interface {
	Confirm(h wizardHandoff) error
}

// wizardHandoff is everything the apply step may use: the stored set, its store, the streams of the
// run, and an opaque receipt. It carries no word that was typed or stdin answer source.
type wizardHandoff struct {
	Cmd     *cobra.Command
	Run     *wizardRun
	Set     planset.Set
	Store   string
	Summary string
	Receipt *planset.Receipt
	Out     io.Writer
}

// terminalApplyStep asks the remaining authorization question on the controlling terminal.
type terminalApplyStep struct{}

func (terminalApplyStep) Confirm(h wizardHandoff) error {
	if _, err := fmt.Fprintf(h.Out, "\nStored plan set %s (%s). Nothing has been changed.\nApply it with: stw tidy apply %s --state-dir %s\n", h.Set.PlanID, h.Summary, h.Set.PlanID, shellWord(h.Store)); err != nil {
		return err
	}
	dirs, branches := 0, 0
	chosen := map[string]bool{}
	for _, item := range h.Set.Items {
		chosen[item.ID] = true
	}
	for _, candidate := range h.Run.res.Candidates {
		if !chosen[candidate.ID] {
			continue
		}
		for _, effect := range candidate.Effects {
			if effect == "remove-files" {
				dirs++
			}
			if strings.HasPrefix(effect, "delete-branch:") {
				branches++
			}
		}
	}
	question := fmt.Sprintf("Apply it now? Plan %s (directories deleted for good: %d; branches (restorable): %d)", h.Set.PlanID, dirs, branches)
	if h.Receipt != nil {
		question += "; the words typed above cover this plan"
	}
	yes, err := tidyTerminalQuestion(question, false)
	if cause := interruptedWizard(h.Run, h.Set.PlanID); cause != nil {
		return cause
	}
	if errors.Is(err, prompt.ErrEndOfInput) || errors.Is(err, prompt.ErrQuit) || (err == nil && !yes) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s; the plan set %s is stored and nothing was applied", escFull(err.Error()), h.Set.PlanID)
	}
	err = runTidyApplyPlanSet(h.Cmd, h.Set.PlanID, h.Store, true, false, h.Receipt)
	if cause := interruption(h.Run.ctx, h.Run.timeoutSet, h.Run.timeout); cause != nil {
		return fmt.Errorf("%v; plan set %s is stored; inspect the apply result before retrying", cause, h.Set.PlanID)
	}
	return err
}

// tidyApplyStep is the handoff seam; every real apply still passes through planset.Apply.
var tidyApplyStep wizardApplyStep = terminalApplyStep{}

var shellSafe = regexp.MustCompile(`\A[A-Za-z0-9_./:@%+=,-]+\z`)

// shellWord quotes a path for the command line the wizard prints. The text of a path is outside
// data: it is escaped first, so a control character shows as text and never reaches the terminal.
func shellWord(p string) string {
	if shellSafe.MatchString(p) {
		return p
	}
	return "'" + strings.ReplaceAll(escFull(p), "'", `'\''`) + "'"
}

// janitorPlan is what the janitor wizard offers: the groups with their numbered rows.
type janitorPlan struct {
	groups []prompt.Group
	kept   int
}

// buildJanitorPlan turns the groups of the overview into the steps of the wizard (Spec 044 FR-001,
// FR-003). A row is choosable iff its verdict is safe or decision and its level is green, yellow
// or orange, whoever owns it; red and kept rows are in no group and have no number. The numbers
// run through the groups in the order of the steps and stay the same for the whole run.
func buildJanitorPlan(doc render.Document) janitorPlan {
	byID := map[string]tidywizard.Candidate{}
	for _, c := range doc.AllCandidates() {
		byID[c.ID] = c
	}
	plan := janitorPlan{kept: doc.KeptRows()}
	n := 0
	for _, g := range render.Groups(doc) {
		pg := prompt.Group{Title: g.Label(), Place: g.PlaceText(), Level: g.Level, Counts: g.Counts, Lines: doc.StepLines(g)}
		pg.NounOne, pg.NounMany = g.Noun()
		for _, r := range g.Rows {
			c, ok := byID[r.ID]
			if !ok || !prompt.Choosable(string(c.Verdict), r.Risk.Level) {
				continue
			}
			n++
			pg.Rows = append(pg.Rows, janitorRow(c, n, doc))
			pg.Rows[len(pg.Rows)-1].Level = r.Risk.Level
		}
		if len(pg.Rows) > 0 {
			plan.groups = append(plan.groups, pg)
		}
	}
	return plan
}

// janitorRow is the row of a candidate; the caller sets its Level (the CLI imports no risk package).
func janitorRow(c tidywizard.Candidate, number int, doc render.Document) prompt.Row {
	row := prompt.Row{
		Number: number, ID: c.ID, Verdict: string(c.Verdict), Kind: string(c.Kind), Repo: c.Repo, Path: c.Path, Branch: c.Branch,
		Owner: c.Owner, OwnerShown: render.OwnerLabel(c),
	}
	if c.Scope != owner.ScopeOwn {
		row.Word = owner.ConfirmWord(owner.Owner{Label: c.Owner, Source: c.OwnerSource, Scope: c.Scope})
	}
	for _, e := range c.Effects {
		switch {
		case e == "remove-files":
			row.Dirs++
		case strings.HasPrefix(e, "delete-branch:"):
			row.Branches++
		case strings.HasPrefix(e, "delete-tracking-ref:"):
			row.Tracking++
		}
	}
	detail := "reason " + c.Reason + "; proof " + string(c.Proof)
	if c.Evidence != "" {
		detail += "; evidence " + c.Evidence
	}
	detail += "; owner " + render.OwnerLabel(c) + " (" + string(c.OwnerSource) + ", scope " + string(c.Scope) + "); risk " + string(doc.RiskLevel(c.ID))
	if reasons := doc.RiskReasons(c.ID); len(reasons) > 0 {
		detail += " (" + strings.Join(reasons, ", ") + ")"
	}
	if c.Verdict == tidywizard.VerdictDecision {
		lock := ""
		if c.Task != "" {
			lock = "lock stw:" + c.Task + "; "
		}
		detail += " [" + lock + "path " + c.Path + "]"
	}
	row.Detail = detail
	return row
}

// runTidyJanitor is the human wizard of Spec 044 after the grouped overview: one step per group,
// the final summary, then the plan set of the selection is built and stored through the same
// planset.Build as before (which asks the typed words of foreign rows on /dev/tty) and handed to
// the apply step. A selection applies nothing.
func runTidyJanitor(cmd *cobra.Command, run *wizardRun, doc render.Document, in *bufio.Reader, exit func() error) error {
	out := cmd.OutOrStdout()
	plan := buildJanitorPlan(doc)
	if len(plan.groups) == 0 {
		// Nothing is offered for deletion: the overview said so and why; no question is asked.
		return exit()
	}
	res, err := prompt.RunFlow(in, out, plan.groups, prompt.Options{Interactive: true, Width: tidyTerminalWidth(out), Kept: plan.kept})
	if cause := interruptedWizard(run, ""); cause != nil {
		return runtimeFailure{cause}
	}
	if err != nil {
		return runtimeFailure{errors.New(escFull(err.Error()))}
	}
	if res.Outcome != prompt.OutcomeChosen {
		if res.Outcome != prompt.OutcomeQuit {
			fmt.Fprintln(out, "Nothing chosen; nothing was stored.")
		}
		return exit()
	}
	if err := tidyTerminalAvailable(); err != nil {
		return runtimeFailure{err}
	}
	prep := run.prep
	foreign := false
	for _, r := range res.Rows {
		foreign = foreign || r.Word != ""
	}
	if !foreign {
		// An own-only selection has no typed word; the summary line is followed by the store question.
		yes, err := tidyTerminalQuestion("Store this plan set?", true)
		if cause := interruptedWizard(run, ""); cause != nil {
			return runtimeFailure{cause}
		}
		switch {
		case errors.Is(err, prompt.ErrEndOfInput):
			fmt.Fprintln(out, "\nNothing was stored.")
			return exit()
		case errors.Is(err, prompt.ErrQuit):
			fmt.Fprintln(out, "\nQuit; nothing was stored.")
			return exit()
		case err != nil:
			return runtimeFailure{errors.New("too many invalid answers at the store question; nothing was stored")}
		case !yes:
			fmt.Fprintln(out, "Nothing was stored.")
			return exit()
		}
	}
	set, receipt, err := planset.Build(run.ctx, planset.Request{Candidates: run.res.Candidates, Chosen: res.IDs(), Derived: prep.Derived, Mode: run.mode, DecisionConfirmed: res.Confirmed, Confirm: tidyConfirmOpener}, tidyPlannerFor(prep))
	if err != nil {
		if cause := interruptedWizard(run, ""); cause != nil {
			return runtimeFailure{cause}
		}
		return runtimeFailure{errors.New(escFull(err.Error()))}
	}
	if prep.PlanStore == "" {
		return runtimeFailure{errors.New("no valid state directory to store the plan set in (state-dir-unsafe); give --state-dir to `stw tidy plan --save`")}
	}
	if err := planset.Store(prep.PlanStore, set); err != nil {
		return runtimeFailure{errors.New(escFull(err.Error()))}
	}
	if err := tidyApplyStep.Confirm(wizardHandoff{Cmd: cmd, Run: run, Set: set, Store: prep.PlanStore, Summary: summarize(set, run.res.Candidates), Receipt: receipt, Out: out}); err != nil {
		return runtimeFailure{errors.New(escFull(err.Error()))}
	}
	return exit()
}
