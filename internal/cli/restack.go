// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/restack"
)

// restackIncomplete is the typed exit-status-3 error of `stw restack plan`: a bound, a
// cancellation or a timeout stopped evidence collection. The incomplete result was already written.
type restackIncomplete struct{ text string }

func (e restackIncomplete) Error() string { return e.text }

func restackCommand() *cobra.Command { return restackCommandWith(restack.Plan) }

func restackCommandWith(plan func(context.Context, restack.Options) restack.Result) *cobra.Command {
	command := &cobra.Command{
		Use: "restack", Short: "Plan the local restack of a branch after its parent was rebase-merged",
		Long: "Plan the local restack of a branch after its parent pull request was rebase-merged.\nRead-only: native Git only (jj and GitButler checkouts are refused, with no Git fallback).",
		Args: cobra.NoArgs, SilenceUsage: true, SilenceErrors: true,
		// Hidden until the conformance evidence of Spec 015 (minimum and current Git) exists, so a
		// partial feature is never advertised.
		Hidden: true,
		RunE:   func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	command.AddCommand(restackPlanCommand(plan))
	return command
}

func restackPlanCommand(plan func(context.Context, restack.Options) restack.Result) *cobra.Command {
	var jsonOutput bool
	var branch, oldParent, newParent []string
	var expectHead, remote string
	var identities []string
	command := &cobra.Command{
		Use: "plan [PATH] --branch REF --old-parent REF|OID --new-parent REF|OID", Short: "Predict the restack of one branch onto the rebased parent without writing anything",
		Long: "Predict `git rebase --onto <new parent> <old parent> <branch>` with plumbing in a private object quarantine and report evidence:\n" +
			"range-diff pairing, cherry classification of the parent's commits, own-diff equality, author and committer identities,\n" +
			"predicted conflicts, the touched-paths overlap, affected refs and worktrees, and the inert lease push and resync texts.\n" +
			"Each of --branch, --old-parent and --new-parent is a full 40-hex commit id or a fully qualified ref (the branch must be refs/heads/...).\n" +
			"Read-only and offline: no ref, index, worktree, configuration or repository-object write, no hook, helper, pager or network.\n" +
			"The result is evidence, never a verdict: semantic breaks after a clean replay are not detected.\n" +
			"Exit statuses: 0 complete plan; 1 refusal or error; 2 usage error; 3 incomplete (a bound, cancellation or timeout).",
		Args: cobra.MaximumNArgs(1), SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options := restack.Options{Remote: remote, ExpectHead: expectHead, Identities: identities}
			for name, values := range map[string]*[]string{"--branch": &branch, "--old-parent": &oldParent, "--new-parent": &newParent} {
				if len(*values) != 1 {
					return fmt.Errorf("%s must be given exactly once", name)
				}
			}
			options.Branch, options.OldParent, options.NewParent = branch[0], oldParent[0], newParent[0]
			if len(args) == 1 {
				if args[0] == "" {
					return errors.New("path must not be empty; omit it to plan from the current directory")
				}
				options.Path = args[0]
			}
			if err := restack.ValidateOptions(&options); err != nil {
				return err
			}
			result := plan(cmd.Context(), options)
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				err = renderRestackText(cmd.OutOrStdout(), result)
			}
			if err != nil {
				return runtimeFailure{err}
			}
			switch result.Status {
			case restack.StatusOK:
				return nil
			case restack.StatusIncomplete:
				return restackIncomplete{"restack plan incomplete: " + restackErrorText(result)}
			}
			return runtimeFailure{fmt.Errorf("restack plan refused: %s", restackErrorText(result))}
		},
	}
	flags := command.Flags()
	flags.StringArrayVar(&branch, "branch", nil, "Branch to restack: a fully qualified refs/heads/... ref")
	flags.StringArrayVar(&oldParent, "old-parent", nil, "Old parent: a 40-hex commit id or a fully qualified ref; it must be an ancestor of the branch")
	flags.StringArrayVar(&newParent, "new-parent", nil, "New parent: a 40-hex commit id or a fully qualified ref")
	flags.StringVar(&expectHead, "expect-head", "", "Refuse unless the branch tip is this 40-hex commit id")
	flags.StringVar(&remote, "remote", "origin", "Remote name used only in the printed texts; it is never contacted")
	flags.StringArrayVar(&identities, "identity", nil, "Expected author and committer \"NAME <EMAIL>\" (repeatable); the only source of expected identities")
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned restack-plan result, including refusals and incomplete results")
	return command
}

func restackErrorText(r restack.Result) string {
	if len(r.Diagnostics) == 0 {
		return "no diagnostic"
	}
	return escapePinsText(r.Diagnostics[0].Code + ": " + r.Diagnostics[0].Message)
}
