// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// stw tidy restore and stw tidy history (Spec 031 FR-042..FR-047, contract
// restore.md). Every argument error is a usage error (exit 2) found before any
// store or native work; results with a status other than ok exit 1.

type tidyRestoreFlags struct {
	contract, plan, stateDir, as string
	actions                      []string
	apply, authorize, list       bool
	limit                        int
	timeout                      time.Duration
	structured                   bool
}

func tidyRestoreCommand(restore func(context.Context, workspace.V3RestoreOptions) workspace.V3RestoreResult, history func(context.Context, workspace.V3HistoryOptions) workspace.V3HistoryResult) *cobra.Command {
	var f tidyRestoreFlags
	command := &cobra.Command{Use: "restore [PATH]", Short: "Plan or apply the restore of branches deleted by tidy, or list the stored journals", Long: "Restore deleted local branches from the recovery journal of a contract 3 tidy apply.\nThe default is a read-only plan. --apply with --authorize creates only the planned local branches\nby compare-and-swap and never overwrites, moves or deletes a reference. Only tips and their history\nare restored: configuration, reflogs and worktrees are not. --list shows the stored journals.", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if f.contract == "4" && !f.list {
			return runPruneV4Restore(cmd, args, f)
		}
		if err := validateTidyRestoreCLI(cmd, args, f); err != nil {
			return err
		}
		path := workspacePath(args)
		if f.list {
			if f.contract == "4" {
				return runPruneV4History(cmd, args, f)
			}
			o := workspace.V3HistoryOptions{Path: path, StateDir: f.stateDir, Limit: f.limit, List: true}
			return writeTidyHistoryResult(cmd, history(cmd.Context(), o), f.structured)
		}
		o := workspace.V3RestoreOptions{Path: path, StateDir: f.stateDir, PlanID: f.plan, Actions: f.actions, As: f.as, Apply: f.apply, Authorize: f.authorize, Timeout: f.timeout}
		return writeTidyRestoreResult(cmd, restore(cmd.Context(), o), f.structured)
	}}
	fl := command.Flags()
	fl.StringVar(&f.contract, "contract", "", "Select exactly contract 3 or 4")
	fl.StringVar(&f.plan, "plan", "", "Identity of the applied tidy plan whose journal is restored")
	fl.StringVar(&f.stateDir, "state-dir", "", "Private state directory that holds the plan and the journal")
	fl.StringArrayVar(&f.actions, "action", nil, "Restrict the restore to an action of the journal (repeatable)")
	fl.StringVar(&f.as, "as", "", "Restore the one selected action under this full refs/heads/ name")
	fl.BoolVar(&f.apply, "apply", false, "Create the planned branches instead of only planning them")
	fl.BoolVar(&f.authorize, "authorize", false, "Authorize exactly the local branch creations of --apply")
	fl.BoolVar(&f.list, "list", false, "List the stored journals instead of planning a restore")
	fl.IntVar(&f.limit, "limit", 0, "Most journals listed (default 256, ceiling 1024); only with --list")
	fl.DurationVar(&f.timeout, "timeout", 0, "Replace the default deadline (60 s plus 5 s per restored branch); not with --list")
	fl.BoolVar(&f.structured, "json", false, "Output one versioned result")
	trackTidyV3Singletons(command)
	return command
}

func tidyHistoryCommand(history func(context.Context, workspace.V3HistoryOptions) workspace.V3HistoryResult) *cobra.Command {
	var f tidyRestoreFlags
	command := &cobra.Command{Use: "history [PATH]", Short: "List the stored tidy journals, newest first", Long: "List the stored contract 3 tidy journals newest first with their counts, mode, recovery-ref\npresence and the kind of reviewer decisions. Read-only; every bound is reported as a truncation.", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateTidyHistoryCLI(cmd, args, f); err != nil {
			return err
		}
		if f.contract == "4" {
			return runPruneV4History(cmd, args, f)
		}
		o := workspace.V3HistoryOptions{Path: workspacePath(args), StateDir: f.stateDir, Limit: f.limit}
		return writeTidyHistoryResult(cmd, history(cmd.Context(), o), f.structured)
	}}
	fl := command.Flags()
	fl.StringVar(&f.contract, "contract", "", "Select exactly contract 3 or 4")
	fl.StringVar(&f.stateDir, "state-dir", "", "Private state directory that holds the journals")
	fl.IntVar(&f.limit, "limit", 0, "Most journals listed (default 256, ceiling 1024)")
	fl.BoolVar(&f.structured, "json", false, "Output one versioned result")
	trackTidyV3Singletons(command)
	return command
}

// validateTidyRestoreCommon checks what restore and history share.
func validateTidyRestoreCommon(cmd *cobra.Command, args []string, f tidyRestoreFlags) error {
	if !cmd.Flags().Changed("contract") || f.contract != "3" && f.contract != "4" {
		return fmt.Errorf("--contract 3 or 4 is required")
	}
	if err := validateTidyV3PresentFlags(cmd); err != nil {
		return err
	}
	if len(args) > 0 && !tidyV3Path(args[0]) {
		return fmt.Errorf("PATH requires an absolute canonical path")
	}
	if f.stateDir == "" {
		return fmt.Errorf("--state-dir is required: the private directory that holds the plan and journals of the tidy apply")
	}
	if !tidyV3Path(f.stateDir) {
		return fmt.Errorf("--state-dir requires an absolute canonical path")
	}
	if cmd.Flags().Changed("limit") && (f.limit < 1 || f.limit > 1024) {
		return fmt.Errorf("--limit must be between 1 and 1024")
	}
	return nil
}

func validateTidyHistoryCLI(cmd *cobra.Command, args []string, f tidyRestoreFlags) error {
	return validateTidyRestoreCommon(cmd, args, f)
}

func validateTidyRestoreCLI(cmd *cobra.Command, args []string, f tidyRestoreFlags) error {
	if err := validateTidyRestoreCommon(cmd, args, f); err != nil {
		return err
	}
	changed := cmd.Flags().Changed
	if f.list {
		if changed("timeout") {
			return fmt.Errorf("--timeout does not apply to --list")
		}
		if changed("plan") || changed("action") || changed("as") || f.apply || f.authorize || f.contract == "4" && (changed("apply") || changed("authorize")) {
			return fmt.Errorf("--list takes only --state-dir, --limit and --json")
		}
		return nil
	}
	if changed("limit") {
		return fmt.Errorf("--limit applies only with --list")
	}
	if changed("timeout") && f.timeout <= 0 {
		return fmt.Errorf("--timeout must be a positive duration")
	}
	if f.plan == "" {
		return fmt.Errorf("--plan is required unless --list is given")
	}
	if err := validateWorkspaceCLI("plan", f.plan); err != nil {
		return err
	}
	if changed("as") && f.as == "" {
		return fmt.Errorf("--as requires a nonempty value")
	}
	o := workspace.V3RestoreOptions{StateDir: f.stateDir, PlanID: f.plan, Actions: f.actions, As: f.as, Apply: f.apply, Authorize: f.authorize}
	if err := workspace.ValidateV3RestoreOptions(o); err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(err.Error()))
	}
	return nil
}

func writeTidyRestoreResult(cmd *cobra.Command, r workspace.V3RestoreResult, structured bool) error {
	return writeTidyRestoreValue(cmd, r, structured, r.Status, r.Command, func(w io.Writer) error { return renderTidyRestoreText(w, r) })
}

func writeTidyHistoryResult(cmd *cobra.Command, r workspace.V3HistoryResult, structured bool) error {
	return writeTidyRestoreValue(cmd, r, structured, r.Status, r.Command, func(w io.Writer) error { return renderTidyHistoryText(w, r) })
}

func writeTidyRestoreValue(cmd *cobra.Command, value any, structured bool, status, command string, render func(io.Writer) error) error {
	var err error
	if structured {
		err = json.NewEncoder(cmd.OutOrStdout()).Encode(value)
	} else {
		err = render(cmd.OutOrStdout())
	}
	if err != nil {
		return runtimeFailure{err}
	}
	if err = cmd.Context().Err(); err != nil {
		return runtimeFailure{err}
	}
	if status != "ok" {
		return runtimeFailure{fmt.Errorf("tidy operation %q: %q", command, status)}
	}
	return nil
}

// Branch names, reasons and reviewer text come from the journal: every value is
// quoted so that no control character or line break reaches the terminal.
func renderTidyRestoreText(w io.Writer, r workspace.V3RestoreResult) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Tidy operation: %q\nContract: 3\nStatus: %q\nMode: %q\nPlan: %q\n", r.Command, r.Status, r.Mode, r.PlanID)
	for _, e := range r.Entries {
		reason := ""
		if e.Reason != nil {
			reason = *e.Reason
		}
		fmt.Fprintf(&text, "Entry: action=%q ref=%q oid=%q state=%q reason=%q class=%q flags=%q recovery=%q", e.ActionID, e.Ref, e.OID, e.State, reason, e.Class, e.Flags, e.RecoveryRef)
		if e.Outcome != "" {
			fmt.Fprintf(&text, " outcome=%q", e.Outcome)
		}
		text.WriteString("\n")
		if d := e.Decision; d != nil {
			fmt.Fprintf(&text, "  Decision: reviewer=%q kind=%q decision=%q reason=%q\n", d.Reviewer, d.Kind, d.Decision, d.Reason)
		}
		if bc := e.BranchConfig; bc != nil {
			fmt.Fprintf(&text, "  Branch config: status=%q reflog_tip=%q branch_oid=%q truncated=%t\n", bc.Status, bc.ReflogTip, bc.BranchOID, bc.Truncated)
			for _, k := range bc.Keys {
				fmt.Fprintf(&text, "    Key: key=%q state=%q", k.Key, k.State)
				if k.Remote != nil {
					fmt.Fprintf(&text, " remote=%q", *k.Remote)
				}
				if k.Outcome != "" {
					fmt.Fprintf(&text, " outcome=%q", k.Outcome)
				}
				text.WriteString("\n")
			}
		}
		if j := e.RestoreJournal; j != nil {
			fmt.Fprintf(&text, "  Restore journal: intent=%q outcome=%q\n", j.Intent, j.Outcome)
		}
	}
	c := r.Counts
	fmt.Fprintf(&text, "Counts: restorable=%d already-present=%d name-taken=%d recovery-missing=%d journal-incomplete=%d\n", c.Restorable, c.AlreadyPresent, c.NameTaken, c.RecoveryMissing, c.JournalIncomplete)
	for _, l := range r.Limits {
		fmt.Fprintf(&text, "Limit: %q\n", l)
	}
	for _, d := range r.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", d.Code, d.Message)
	}
	_, err := io.WriteString(w, text.String())
	return err
}

func renderTidyHistoryText(w io.Writer, r workspace.V3HistoryResult) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Tidy operation: %q\nContract: 3\nStatus: %q\nTotal: %d scan_truncated=%t\n", r.Command, r.Status, r.Total, r.ScanTruncated)
	for _, e := range r.Entries {
		fmt.Fprintf(&text, "Journal: plan=%q time=%q mode=%q deleted=%d retained=%d restorable=%d recovery_refs=%q decisions_human=%d decisions_ai=%d status=%q\n", e.PlanID, e.Time, e.Mode, e.Deleted, e.Retained, e.Restorable, e.RecoveryRefs, e.DecisionKinds.Human, e.DecisionKinds.AI, e.JournalStatus)
		if m := e.RunMode; m != nil {
			fmt.Fprintf(&text, "  Run mode: mode=%q stdin_tty=%t stdout_tty=%t markers=%q mine=%t by_owner=%q caller=%q caller_source=%q\n", m.Mode, m.StdinTTY, m.StdoutTTY, m.Markers, m.Mine, m.ByOwner, m.Caller, m.CallerSource)
		}
	}
	for _, t := range r.Truncated {
		fmt.Fprintf(&text, "Truncated: unit=%q shown=%d total=%d\n", t.Unit, t.Shown, t.Total)
	}
	for _, d := range r.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", d.Code, d.Message)
	}
	_, err := io.WriteString(w, text.String())
	return err
}
