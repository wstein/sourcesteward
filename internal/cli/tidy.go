// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/workspace"
)

func tidyCommand() *cobra.Command { return tidyCommandWith(workspace.PlanTidy) }
func tidyCommandWith(run func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult) *cobra.Command {
	return tidyCommandWithOperations(run, workspace.ApplyTidy)
}
func tidyCommandWithOperations(run func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult, applyRun func(context.Context, workspace.TidyApplyOptions) workspace.TidyResult) *cobra.Command {
	return tidyCommandWithV3Operations(run, applyRun, workspace.PlanTidyV3, workspace.ApplyTidyV3)
}
func tidyCommandWithV3Operations(run func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult, applyRun func(context.Context, workspace.TidyApplyOptions) workspace.TidyResult, planV3 func(context.Context, workspace.V3PlanOptions) workspace.V3Result, applyV3 func(context.Context, workspace.V3ApplyOptions) workspace.V3Result) *cobra.Command {
	return tidyCommandWithRestoreOperations(run, applyRun, planV3, applyV3, workspace.RestoreTidyV3, workspace.HistoryTidyV3)
}
func tidyCommandWithRestoreOperations(run func(context.Context, workspace.TidyPlanOptions) workspace.TidyResult, applyRun func(context.Context, workspace.TidyApplyOptions) workspace.TidyResult, planV3 func(context.Context, workspace.V3PlanOptions) workspace.V3Result, applyV3 func(context.Context, workspace.V3ApplyOptions) workspace.V3Result, restoreV3 func(context.Context, workspace.V3RestoreOptions) workspace.V3RestoreResult, historyV3 func(context.Context, workspace.V3HistoryOptions) workspace.V3HistoryResult) *cobra.Command {
	family := &cobra.Command{Use: "tidy", Short: "Review evidence-based local Git branch or jj bookmark cleanup", Long: "Review bounded local cleanup evidence against an explicit integration base.\nPlanning never deletes references, registrations or files. Default v1/v2 planning stays offline.\nContract 3 permits explicitly selected authenticated GitHub reads with --online.\nGit and jj are native Tier 1 backends; unsupported proof retains work.\nApply requires a stored plan, explicit authorization, stale checks and recorded recovery.\nOwned task retirement requires separate registration and exact filesystem selections.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	var options workspace.TidyPlanOptions
	var structured bool
	var v3 workspace.V3PlanOptions
	var contract string
	var hints []string
	var proofKinds string
	var wizard tidyWizardOptions
	plan := &cobra.Command{Use: "plan [PATH]", Short: "Review native references or exactly owned task retirement actions", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		layer, err := selectTidyPlanLayer(cmd, args, &wizard)
		if err != nil {
			return err
		}
		if layer == tidyWizard {
			return runTidyWizardPlan(cmd, &wizard, structured, v3.Timeout)
		}
		if cmd.Flags().Changed("contract") {
			if contract != "3" {
				return fmt.Errorf("--contract accepts exactly 3")
			}
			if err := validateTidyV3PresentFlags(cmd); err != nil {
				return err
			}
			v3.Path, v3.Base, v3.StateDir, v3.Refs = workspacePath(args), options.Base, options.StateDir, options.Refs
			v3.Task, v3.Owner, v3.RemoveRegistration, v3.RemoveFiles = options.Task, options.Owner, options.RemoveRegistration, options.RemoveFiles
			if cmd.Flags().Changed("integration-proof") {
				kinds, err := parseTidyV3ProofKinds(proofKinds)
				if err != nil {
					return err
				}
				v3.IntegrationProofs = kinds
			}
			if err := validateTidyV3ReviewFlags(cmd, &v3); err != nil {
				return err
			}
			if err := validateTidyV3PlanCLI(args, &v3, hints); err != nil {
				return err
			}
			return writeTidyV3Result(cmd, planV3(cmd.Context(), v3), structured)
		}
		if err := rejectTidyV3Flags(cmd, tidyV3PlanFlags); err != nil {
			return err
		}
		if (cmd.Flags().Changed("task") || cmd.Flags().Changed("owner") || cmd.Flags().Changed("remove-registration") || cmd.Flags().Changed("remove-files")) && options.Task == "" && options.Owner == "" && !options.RemoveRegistration && options.RemoveFiles == "" {
			return fmt.Errorf("retirement selectors require --task, --owner, --state-dir and --remove-registration")
		}
		if err := validateTidyPlanCLI(args, options); err != nil {
			return err
		}
		options.Path = workspacePath(args)
		result := run(cmd.Context(), options)
		return writeTidyResult(cmd, result, structured)
	}}
	plan.Flags().StringVar(&options.Base, "base", "", "Required literal native integration base; no inferred trunk")
	plan.Flags().StringArrayVar(&options.Refs, "ref", nil, "Select a literal local branch/bookmark name (repeatable; otherwise all bounded local references)")
	plan.Flags().StringVar(&options.StateDir, "state-dir", "", "Existing private mode0700 state directory; optional for reference planning (omission makes it stateless), required for retirement")
	plan.Flags().StringVar(&options.Task, "task", "", "Select an exactly owned completed task for retirement")
	plan.Flags().StringVar(&options.Owner, "owner", "", "Expected task owner; private store access remains the trust boundary")
	plan.Flags().BoolVar(&options.RemoveRegistration, "remove-registration", false, "Plan exact native registration removal; required for task retirement")
	plan.Flags().StringVar(&options.RemoveFiles, "remove-files", "", "Separately select the exact owned absolute destination for filesystem removal")
	plan.Flags().BoolVar(&structured, "json", false, "Output one versioned tidy result, including operational refusals")
	addTidyV3PlanFlags(plan, &contract, &v3, &hints, &proofKinds)
	addTidyWizardPlanFlags(plan, &wizard)
	var applyOptions workspace.TidyApplyOptions
	var applyStructured bool
	var applyContract string
	var applyV3Options workspace.V3ApplyOptions
	var applyYes bool
	apply := &cobra.Command{Use: "apply [PLAN-ID]", Short: "Apply a stored plan set or an authorized stored reference or task retirement plan", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("json") && cmd.Flags().Changed("human") {
			return fmt.Errorf("--json and --human exclude each other")
		}
		if err := validateTidyPlanSetApply(cmd, args, applyYes); err != nil {
			return err
		}
		if len(args) == 1 {
			return runTidyApplyPlanSet(cmd, args[0], applyOptions.StateDir, applyYes, applyStructured)
		}
		if cmd.Flags().Changed("contract") {
			if applyContract != "3" {
				return fmt.Errorf("--contract accepts exactly 3")
			}
			if err := validateTidyV3PresentFlags(cmd); err != nil {
				return err
			}
			if cmd.Flags().Changed("timeout") && applyV3Options.Timeout <= 0 {
				return fmt.Errorf("--timeout must be a positive duration")
			}
			applyV3Options.PlanID, applyV3Options.StateDir, applyV3Options.Authorize = applyOptions.PlanID, applyOptions.StateDir, applyOptions.Authorize
			if err := validateTidyV3ApplyCLI(applyV3Options); err != nil {
				return err
			}
			return writeTidyV3Result(cmd, applyV3(cmd.Context(), applyV3Options), applyStructured)
		}
		if err := rejectTidyV3Flags(cmd, tidyV3ApplyFlags); err != nil {
			return err
		}
		if err := validateWorkspaceCLI("plan", applyOptions.PlanID, "state-dir", applyOptions.StateDir); err != nil {
			return err
		}
		return writeTidyResult(cmd, applyRun(cmd.Context(), applyOptions), applyStructured)
	}}
	apply.Flags().StringVar(&applyOptions.PlanID, "plan", "", "Exact generated immutable tidy plan identity")
	apply.Flags().StringVar(&applyOptions.StateDir, "state-dir", "", "Private store containing the reviewed plan and durable recovery journal")
	apply.Flags().BoolVar(&applyOptions.Authorize, "authorize", false, "Authorize only the reviewed plan's declared recovery, reference or retirement effects")
	apply.Flags().BoolVar(&applyYes, "yes", false, "Authorize the plan set named by the explicit plan id without a prompt; never honoured for other or unknown items")
	apply.Flags().Bool("human", false, "Force text output; excludes --json")
	apply.Flags().BoolVar(&applyStructured, "json", false, "Output one versioned tidy result, including operational refusals and partial effects")
	addTidyV3ApplyFlags(apply, &applyContract, &applyV3Options)
	// The recorder sits inside the counting wrapper, so contract 3 still sees every occurrence.
	recordStateDirs(plan, &wizard.stateDirs)
	trackTidyV3Singletons(plan)
	trackTidyV3Singletons(apply)
	family.AddCommand(plan, apply, tidyRestoreCommand(restoreV3, historyV3), tidyHistoryCommand(historyV3))
	return family
}
func writeTidyResult(cmd *cobra.Command, result workspace.TidyResult, structured bool) error {
	var err error
	if structured {
		err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	} else {
		err = renderTidyText(cmd.OutOrStdout(), result)
	}
	if err != nil {
		return runtimeFailure{err}
	}
	if err = cmd.Context().Err(); err != nil {
		return runtimeFailure{err}
	}
	if result.Status != "ok" {
		return runtimeFailure{fmt.Errorf("tidy operation %q: %q", result.Command, result.Status)}
	}
	return nil
}
func validateTidyPlanCLI(args []string, options workspace.TidyPlanOptions) error {
	if options.Base == "" {
		return fmt.Errorf("--base is required")
	}
	if len(options.Refs) > 1024 {
		return fmt.Errorf("--ref exceeds 1024 selectors")
	}
	retirement := options.Task != "" || options.Owner != "" || options.RemoveRegistration || options.RemoveFiles != ""
	if retirement {
		if len(options.Refs) != 0 {
			return fmt.Errorf("--ref and retirement selectors are mutually exclusive")
		}
		if !options.RemoveRegistration {
			return fmt.Errorf("--remove-registration is required for task retirement")
		}
		if err := validateWorkspaceCLI("task", options.Task, "owner", options.Owner, "state-dir", options.StateDir); err != nil {
			return err
		}
		if options.RemoveFiles != "" {
			if err := validateWorkspaceCLI("remove-files", options.RemoveFiles); err != nil {
				return err
			}
		}
	}
	values := append([]string{options.Base}, options.Refs...)
	if options.StateDir != "" {
		values = append(values, options.StateDir)
	}
	if len(args) > 0 {
		values = append(values, args[0])
	}
	for _, value := range values {
		if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return fmt.Errorf("tidy paths and selectors require nonempty UTF-8 text of at most 4096 bytes without NUL")
		}
	}
	return nil
}
func renderTidyText(writer io.Writer, result workspace.TidyResult) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Tidy operation: %q\nStatus: %q\nBackend: %q\n", result.Command, result.Status, result.Backend)
	if result.Plan != nil {
		p := result.Plan
		fmt.Fprintf(&text, "Plan: %q\nSource: %q\nState directory: %q\nBase selector: %q\nBase commit: %q\nBase change: %q\n", p.PlanID, p.SourcePath, p.StateDir, p.Expected.BaseSelector, p.Expected.BaseCommit, p.Expected.BaseChange)
		for _, candidate := range p.Candidates {
			fmt.Fprintf(&text, "Candidate: %q kind=%q commit=%q change=%q decision=%q reason=%q proof=%q\n", candidate.Name, candidate.Kind, candidate.CommitID, candidate.ChangeID, candidate.Decision, candidate.Reason, candidate.Proof)
			for _, path := range candidate.WorkspacePaths {
				fmt.Fprintf(&text, "  Workspace: %q\n", path)
			}
			for _, limit := range candidate.Limitations {
				fmt.Fprintf(&text, "  Limitation: %q\n", limit)
			}
		}
		for _, effect := range p.Effects {
			fmt.Fprintf(&text, "Effect: %q\n", effect)
		}
		for _, limit := range p.RecoveryLimits {
			fmt.Fprintf(&text, "Recovery limit: %q\n", limit)
		}
	}
	if result.Retirement != nil {
		p := result.Retirement
		fmt.Fprintf(&text, "Retirement plan: %q\nTask: %q\nOwner: %q\nSource: %q\nDestination: %q\nState directory: %q\nBase selector: %q\nBase commit: %q\nBase change: %q\nRemove registration selected: %t\nFilesystem target selected: %q\n", p.PlanID, p.Task.TaskID, p.Task.Owner, p.SourcePath, p.Task.Destination, p.StateDir, p.Expected.Expected.BaseSelector, p.Expected.Expected.BaseCommit, p.Expected.Expected.BaseChange, p.RemoveRegistration, p.RemoveFiles)
		fmt.Fprintf(&text, "Source operation: %q\nTarget commit: %q\nTarget change: %q\nTarget operation: %q\nCreation journal digest: %q\nDestination identity: device=%d inode=%d\nManifest: digest=%q complete=%t\nMetadata manifest: digest=%q complete=%t\n", p.Expected.Expected.Identity.OperationID, p.Expected.Target.CommitID, p.Expected.Target.ChangeID, p.Expected.Target.OperationID, p.CreationJournalDigest, p.Expected.Destination.Device, p.Expected.Destination.Inode, p.Expected.Manifest.Digest, p.Expected.Manifest.Complete, p.Expected.Metadata.Digest, p.Expected.Metadata.Complete)
		for _, entry := range p.Expected.Manifest.Entries {
			fmt.Fprintf(&text, "Manifest entry: %q kind=%q digest=%q size=%d mode=%d executable=%t device=%d inode=%d\n", entry.Path, entry.Kind, entry.Digest, entry.Size, entry.Mode, entry.Executable, entry.Device, entry.Inode)
		}
		for _, entry := range p.Expected.Metadata.Entries {
			fmt.Fprintf(&text, "Metadata entry: %q kind=%q digest=%q size=%d mode=%d executable=%t device=%d inode=%d\n", entry.Path, entry.Kind, entry.Digest, entry.Size, entry.Mode, entry.Executable, entry.Device, entry.Inode)
		}
		for _, sibling := range p.Expected.Siblings {
			fmt.Fprintf(&text, "Protected sibling: %q kind=%q device=%d inode=%d\n", sibling.Identity.Path, sibling.Kind, sibling.Identity.Device, sibling.Identity.Inode)
		}
		for _, action := range p.Expected.Actions {
			fmt.Fprintf(&text, "Retirement action: %q target=%q decision=%q reason=%q proof=%q\n", action.Name, action.Target, action.Decision, action.Reason, action.Proof)
			for _, effect := range action.Effects {
				fmt.Fprintf(&text, "  Effect: %q\n", effect)
			}
			for _, blocker := range action.Blockers {
				fmt.Fprintf(&text, "  Blocker: %q\n", blocker)
			}
		}
		for _, effect := range p.Effects {
			fmt.Fprintf(&text, "Effect: %q\n", effect)
		}
		for _, limit := range p.RecoveryLimits {
			fmt.Fprintf(&text, "Recovery limit: %q\n", limit)
		}
		for _, limit := range p.Expected.Limitations {
			fmt.Fprintf(&text, "Observation limit: %q\n", limit)
		}
	}
	for _, action := range result.Actions {
		fmt.Fprintf(&text, "Action: %q status=%q detail=%q\n", action.Name, action.Status, action.Detail)
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintf(&text, "Diagnostic: %q %q\n", diagnostic.Code, diagnostic.Message)
	}
	for _, limit := range result.Limitations {
		fmt.Fprintf(&text, "Limitation: %q\n", limit)
	}
	_, err := io.WriteString(writer, text.String())
	return err
}
