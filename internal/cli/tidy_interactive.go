// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/tidywizard"
	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
	"github.com/wstein/sourcesteward/internal/tidywizard/planset"
	"github.com/wstein/sourcesteward/internal/tidywizard/prompt"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
)

// tidyConfirmOpener opens the controlling terminal for the typed confirmation of foreign rows; a
// seam so that tests need no terminal. It is never stdin or stdout.
var tidyConfirmOpener owner.Opener = owner.DevTTY

// runTidyInteractive is bare `stw tidy` (Spec 041 wizard interaction): analyse read only, show the
// overview, ask which rows to plan, store the plan set of the choice, and apply it only after an
// explicit "y" on that plan id, through the very same apply path as `stw tidy apply`. Without a
// terminal, or in agent mode, it does not prompt: it behaves as `tidy plan` without --save.
func runTidyInteractive(cmd *cobra.Command, w *tidyWizardOptions, timeout time.Duration) error {
	run, err := startWizard(cmd, w, timeout)
	if err != nil {
		return err
	}
	defer run.cancel()
	prep, res, mode := run.prep, run.res, run.mode
	doc := render.Build(render.Input{Workspace: prep.Workspace, Derived: prep.Derived, Repos: prep.Meta, Result: res, Mode: mode, Notices: prep.Notices, Now: run.now})
	out := cmd.OutOrStdout()
	// A person on a terminal still chooses by the numbers of the row view until the group steps of
	// Spec 044 replace the question; everyone else gets the grouped overview unless --rows asks for
	// the rows.
	text := tidyOverviewText(out, doc, w.rows || mode.Mode == owner.ModeHuman)
	if _, err := io.WriteString(out, text); err != nil {
		return runtimeFailure{err}
	}
	exit := func() error { return wizardExit(doc.Truncated, prep, res) }

	// Prompts are for a human on a terminal only (human mode needs both streams on terminals).
	if mode.Mode != owner.ModeHuman {
		fmt.Fprintln(out, "\nNo terminal (or an agent run): nothing was asked and nothing was stored.\nRun `stw tidy plan --save` to store a plan set, or `stw tidy plan --json` for the whole overview.")
		return exit()
	}
	in := bufio.NewReader(cmd.InOrStdin())
	items := promptItems(doc)
	choice, err := prompt.Select(in, out, items, prompt.Options{Interactive: true})
	// An interrupt that arrived at a prompt is reported as one, whatever the answer was.
	if cause := interruptedWizard(run, ""); cause != nil {
		return runtimeFailure{cause}
	}
	if err != nil {
		return runtimeFailure{errors.New(escFull(err.Error()))}
	}
	if choice.Outcome != prompt.OutcomeChosen {
		fmt.Fprintln(out, "Nothing chosen; nothing was stored.")
		return exit()
	}
	set, err := planset.Build(run.ctx, planset.Request{Candidates: res.Candidates, Chosen: choice.Chosen, Derived: prep.Derived, Mode: mode, DecisionConfirmed: choice.Confirmed, Confirm: tidyConfirmOpener}, tidyPlannerFor(prep))
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
	fmt.Fprintf(out, "\nStored plan set %s (%s). Nothing has been changed.\n", set.PlanID, summarize(set, res.Candidates))
	yes, err := prompt.YesNo(in, out, fmt.Sprintf("Apply it now? Run: stw tidy apply %s (or answer y here)", set.PlanID), true)
	if cause := interruptedWizard(run, set.PlanID); cause != nil {
		return runtimeFailure{cause}
	}
	switch {
	case errors.Is(err, prompt.ErrEndOfInput):
		return exit()
	case err != nil:
		// The plan set is stored; say so, and that nothing was applied.
		reason := escFull(err.Error())
		if errors.Is(err, prompt.ErrTooManyInvalid) {
			reason = "too many invalid answers at the apply question"
		}
		return runtimeFailure{fmt.Errorf("%s; the plan set %s is stored and nothing was applied", reason, set.PlanID)}
	case !yes:
		return exit()
	}
	// The explicit y on this plan id is the authorization; foreign items still need the typed
	// confirmation inside the apply path.
	return runTidyApplyPlanSet(cmd, set.PlanID, prep.PlanStore, true, false)
}

// interruptedWizard reports an interrupt or a deadline as such (never the raw context error). With a
// plan id the plan set is already stored: the message says so and that nothing was applied.
func interruptedWizard(run *wizardRun, planID string) error {
	cause := interruption(run.ctx, run.timeoutSet, run.timeout)
	if cause == nil || planID == "" {
		return cause
	}
	return fmt.Errorf("interrupted; the plan set %s is stored and nothing was applied", planID)
}

// promptItems builds the choosable rows of the prompt from the overview. The detail is escaped text:
// the reason and the evidence, which for a decision row carries the lock.
func promptItems(doc render.Document) []prompt.Item {
	var items []prompt.Item
	for _, n := range render.Numbered(doc) {
		c := n.Candidate
		detail := escFull(c.Reason)
		if c.Evidence != "" {
			detail += ": " + escFull(c.Evidence)
		}
		if c.Verdict == tidywizard.VerdictDecision {
			// The confirmation names the lock and the worktree it is about.
			lock := ""
			if c.Task != "" {
				lock = "lock stw:" + escFull(c.Task) + "; "
			}
			detail += " [" + lock + "path " + escFull(c.Path) + "]"
		}
		items = append(items, prompt.Item{Number: n.Number, ID: c.ID, Verdict: string(c.Verdict), Scope: c.Scope, Owner: escFull(c.Owner), Detail: detail})
	}
	return items
}

// summarize says how much a stored plan set covers.
func summarize(set planset.Set, cands []tidywizard.Candidate) string {
	byID := map[string]tidywizard.Candidate{}
	for _, c := range cands {
		byID[c.ID] = c
	}
	effects, repos := 0, map[string]bool{}
	for _, it := range set.Items {
		c := byID[it.ID]
		effects += len(c.Effects)
		repos[c.Repo] = true
	}
	return fmt.Sprintf("%d effects in %d repositories", effects, len(repos))
}
