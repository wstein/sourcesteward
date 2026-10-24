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
	"github.com/wstein/sourcesteward/internal/tidywizard/derive"
	"github.com/wstein/sourcesteward/internal/workspace"
)

type workspaceOperations struct {
	plan    func(context.Context, workspace.PlanOptions) workspace.Result
	apply   func(context.Context, workspace.ApplyOptions) workspace.Result
	handoff func(context.Context, workspace.Options) workspace.Result
	retire  func(context.Context, workspace.Options) workspace.Result
}

func workspaceCommand() *cobra.Command {
	return workspaceCommandWith(workspaceOperations{workspace.PlanCreate, workspace.Apply, workspace.Handoff, workspace.RetirePlan})
}
func workspaceCommandWith(operations workspaceOperations) *cobra.Command {
	family := &cobra.Command{Use: "workspace", Short: "Plan and create isolated native Git or jj task workspaces", Long: "Create isolated task workspaces through reviewable plans and explicit local authorization.\nPlanning writes private state records; applying creates a native workspace and recovery journal.\nHandoff and retirement preparation preserve work; use stw tidy for separately authorized owned retirement.\nGit and jj are native Tier 1 backends. No fetch, push, setup scripts or automatic cleanup.", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	var p workspace.PlanOptions
	plan := &cobra.Command{Use: "plan [PATH]", Short: "Persist an inspectable isolated-workspace creation plan", Args: cobra.MaximumNArgs(1)}
	plan.Flags().StringVar(&p.Base, "base", "", "Explicit native base revision, resolved in the source checkout PATH (not in the current directory); the plan shows the resolved commit")
	plan.Flags().StringVar(&p.Destination, "destination", "", "Unused absolute destination path")
	plan.Flags().StringVar(&p.Task, "task", "", "Task identity label")
	plan.Flags().StringVar(&p.Owner, "owner", "", "Owner label; private store access is the trust boundary")
	plan.Flags().StringVar(&p.StateDir, "state-dir", "", "Private mode0700 state directory; default ${XDG_STATE_HOME:-~/.local/state}/stw/workspace or $STW_STATE_DIR")
	plan.RunE = workspaceRun(plan, func(cmd *cobra.Command, args []string) workspace.Result {
		p.Path = workspacePath(args)
		return operations.plan(cmd.Context(), p)
	}, func() error {
		return validateWorkspaceDefaulted("base", p.Base, "destination", p.Destination, "task", p.Task, "owner", p.Owner, "state-dir", p.StateDir)
	}, &p.StateDir)
	var a workspace.ApplyOptions
	apply := &cobra.Command{Use: "apply", Short: "Apply a stored plan after explicit authorization and stale-state checks", Args: cobra.NoArgs}
	apply.Flags().StringVar(&a.PlanID, "plan", "", "Generated immutable plan identity")
	apply.Flags().StringVar(&a.StateDir, "state-dir", "", "Private store containing the plan and recovery journal; default as for plan")
	apply.Flags().BoolVar(&a.Authorize, "authorize", false, "Authorize only the reviewed plan's declared local effects")
	apply.RunE = workspaceRun(apply, func(cmd *cobra.Command, _ []string) workspace.Result { return operations.apply(cmd.Context(), a) }, func() error { return validateWorkspaceDefaulted("plan", a.PlanID, "state-dir", a.StateDir) }, &a.StateDir)
	for _, entry := range []struct {
		name, short string
		run         func(context.Context, workspace.Options) workspace.Result
	}{{"handoff", "Describe an owned task workspace and remaining work", operations.handoff}, {"retire-plan", "Prepare retention evidence without deleting a workspace", operations.retire}} {
		var o workspace.Options
		command := &cobra.Command{Use: entry.name + " [PATH]", Short: entry.short, Args: cobra.MaximumNArgs(1)}
		command.Flags().StringVar(&o.StateDir, "state-dir", "", "Private task ownership and recovery store; default as for plan")
		command.RunE = workspaceRun(command, func(cmd *cobra.Command, args []string) workspace.Result {
			o.Path = workspacePath(args)
			return entry.run(cmd.Context(), o)
		}, func() error { return validateWorkspaceDefaulted("state-dir", o.StateDir) }, &o.StateDir)
		family.AddCommand(command)
	}
	family.AddCommand(plan, apply)
	return family
}
func workspacePath(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "."
}

// validateWorkspaceDefaulted validates like validateWorkspaceCLI but lets --state-dir be omitted: the
// shared default (or $STW_STATE_DIR) is used then, see derive.ResolveStateDir.
func validateWorkspaceDefaulted(pairs ...string) error {
	var given []string
	for i := 0; i < len(pairs); i += 2 {
		if pairs[i] != "state-dir" || pairs[i+1] != "" {
			given = append(given, pairs[i], pairs[i+1])
		}
	}
	return validateWorkspaceCLI(given...)
}
func validateWorkspaceCLI(pairs ...string) error {
	for i := 0; i < len(pairs); i += 2 {
		name, value := pairs[i], pairs[i+1]
		if value == "" {
			return fmt.Errorf("--%s is required", name)
		}
		if len(value) > 4096 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return fmt.Errorf("--%s exceeds the text contract", name)
		}
		if name == "task" || name == "owner" || name == "plan" {
			if len(value) > 128 {
				return fmt.Errorf("--%s exceeds 128 bytes", name)
			}
			for j, c := range []byte(value) {
				if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || (j > 0 && (c == '.' || c == '_' || c == '-')) {
					continue
				}
				return fmt.Errorf("--%s requires an ASCII identifier", name)
			}
		}
	}
	return nil
}
func workspaceRun(command *cobra.Command, run func(*cobra.Command, []string) workspace.Result, validate func() error, stateDir *string) func(*cobra.Command, []string) error {
	var structured bool
	command.Flags().BoolVar(&structured, "json", false, "Output one versioned workspace result, including operational refusals")
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 && (len(args[0]) > 4096 || !utf8.ValidString(args[0]) || strings.ContainsRune(args[0], 0)) {
			return fmt.Errorf("PATH exceeds the text contract")
		}
		if err := validate(); err != nil {
			return err
		}
		if err := cmd.Context().Err(); err != nil {
			return runtimeFailure{err}
		}
		var result workspace.Result
		if dir, err := derive.ResolveStateDir(*stateDir); err != nil {
			// Like an explicit unsafe --state-dir, the refusal is a versioned result, not a bare error.
			result = workspace.Result{SchemaVersion: 1, Command: "workspace-" + cmd.Name(), Status: "error", Backend: "unknown",
				Actions: []workspace.ActionOutcome{}, Limitations: []string{}, Diagnostics: []workspace.Diagnostic{{Code: "unsafe-state", Message: err.Error()}}}
		} else {
			*stateDir = dir
			result = run(cmd, args)
		}
		var err error
		if structured {
			err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		} else {
			err = renderWorkspaceText(cmd.OutOrStdout(), result)
		}
		if err != nil {
			return runtimeFailure{err}
		}
		if err = cmd.Context().Err(); err != nil {
			return runtimeFailure{err}
		}
		if result.Status != "ok" {
			return runtimeFailure{fmt.Errorf("workspace operation %q: %q", result.Command, result.Status)}
		}
		return nil
	}
}
func renderWorkspaceText(writer io.Writer, result workspace.Result) error {
	var text strings.Builder
	fmt.Fprintf(&text, "Workspace operation: %q\nStatus: %q\nBackend: %q\n", result.Command, result.Status, result.Backend)
	if result.Plan != nil {
		p := result.Plan
		fmt.Fprintf(&text, "Plan: %q\nTask: %q\nOwner: %q\nSource: %q\nDestination: %q\nBase commit: %q\nBase change: %q\n", p.PlanID, p.TaskID, p.Owner, p.SourcePath, p.Destination, p.Expected.BaseCommit, p.Expected.BaseChange)
		for _, effect := range p.Effects {
			fmt.Fprintf(&text, "Effect: %q\n", effect)
		}
		for _, limit := range p.RecoveryLimits {
			fmt.Fprintf(&text, "Recovery limit: %q\n", limit)
		}
	}
	if s := result.Source; s != nil {
		fmt.Fprintf(&text, "Source checkout: %q\nBase: %q resolved to %q\nSource HEAD: %q\n", s.SourcePath, s.BaseSelector, s.BaseCommit, s.SourceHead)
		if s.CallerDirectory != "" {
			fmt.Fprintf(&text, "Caller directory: %q\n", s.CallerDirectory)
		}
		if s.Notice != "" {
			fmt.Fprintf(&text, "Notice: %q\n", s.Notice)
		}
	}
	if n := result.Next; n != nil {
		if n.Command != "" {
			fmt.Fprintf(&text, "Next: %s\n", n.Command)
		} else {
			text.WriteString("Next: the apply call holds characters that are not safe to print; take it from next_call.argv of --json.\n")
		}
		text.WriteString("Nothing is applied until you run it; running it is the authorization of the effects above.\n")
	}
	if result.Task != nil {
		t := result.Task
		fmt.Fprintf(&text, "Task: %q\nOwner: %q\nDestination: %q\nCommit: %q\nChange: %q\nOperation: %q\nRecovery: %q\n", t.TaskID, t.Owner, t.Destination, t.Native.CommitID, t.Native.ChangeID, t.Native.OperationID, t.RecoveryPointer)
		for _, work := range t.RemainingWork {
			fmt.Fprintf(&text, "Remaining: %q\n", work)
		}
		for _, limit := range t.RecoveryLimits {
			fmt.Fprintf(&text, "Recovery limit: %q\n", limit)
		}
		if t.Inventory != nil {
			var inventory strings.Builder
			if err := renderInventoryText(&inventory, *t.Inventory); err != nil {
				return err
			}
			text.WriteString(inventory.String())
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
