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
	family := &cobra.Command{Use: "tidy", Short: "Review evidence-based local Git branch or jj bookmark cleanup", Long: "Review bounded local cleanup evidence against an explicit integration base.\nPlanning never deletes references, registrations or files and never contacts a forge.\nGit and jj are native Tier 1 backends; unsupported proof retains work.\nApplying cleanup and retiring workspaces remain planned.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	var options workspace.TidyPlanOptions
	var structured bool
	plan := &cobra.Command{Use: "plan [PATH]", Short: "Review native ancestry and retained local reference candidates", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateTidyPlanCLI(args, options); err != nil {
			return err
		}
		options.Path = workspacePath(args)
		result := run(cmd.Context(), options)
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
	}}
	plan.Flags().StringVar(&options.Base, "base", "", "Required literal native integration base; no inferred trunk")
	plan.Flags().StringArrayVar(&options.Refs, "ref", nil, "Select a literal local branch/bookmark name (repeatable; otherwise all bounded local references)")
	plan.Flags().StringVar(&options.StateDir, "state-dir", "", "Optional existing private mode0700 state directory; omission makes planning stateless")
	plan.Flags().BoolVar(&structured, "json", false, "Output one versioned tidy result, including operational refusals")
	family.AddCommand(plan)
	return family
}
func validateTidyPlanCLI(args []string, options workspace.TidyPlanOptions) error {
	if options.Base == "" {
		return fmt.Errorf("--base is required")
	}
	if len(options.Refs) > 1024 {
		return fmt.Errorf("--ref exceeds 1024 selectors")
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
