// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/carryover"
)

// carryOverGit is a test seam: production leaves it nil and uses the guarded runner in --checkout.
var carryOverGit carryover.Git

func reviewCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "review", Short: "Review helpers (read-only)", Args: cobra.NoArgs,
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	command.AddCommand(reviewCarryOverCommand())
	return command
}

func reviewCarryOverCommand() *cobra.Command {
	var jsonOutput bool
	var pr, remote, checkout string
	command := &cobra.Command{
		Use:   "carry-over --pr NUMBER OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD [--remote NAME] [--checkout PATH] [--json]",
		Short: "Check whether a review may be carried over to a rebased or restacked head",
		Long: "Read-only check of the carry-over rule after a rebase or restack. The four arguments are full 40-hex commit ids (a ref name, a leading dash, \"..\" or a space is a usage error).\n" +
			"All three conditions must hold: (1) the patch-id of `git diff BASE...HEAD` (git patch-id --stable) is equal for the old and the new head, each against its own base;\n" +
			"(2) there is no merge commit in NEW_BASE..NEW_HEAD; (3) git range-diff shows \"=\" for every commit, at the same position.\n" +
			"The output names each condition and the verdict. Only for a yes it ends with the line `Carried-over: from=<40-hex> to=<40-hex> patch-id=<hex> pr=<n>`.\n" +
			"The command posts nothing: a reviewer who is not the author posts the line after reading the result. The objects must exist in the checkout (the default is the current directory).\n" +
			"--remote NAME fetches refs/pull/NUMBER/head into refs/stw/carry-over/ (objects and that private ref only; no FETCH_HEAD, no tags, no user ref) and refuses unless it equals NEW_HEAD.\n" +
			"The patch-id does not prove that the new head works on its base; the checks of the pull request do. A carry-over also needs `from` to be CLEAR; stw does not read reviews.\n" +
			"Exit statuses: 0 carry-over allowed, 1 not allowed or a read failed (the result is still printed), 2 usage error.",
		Args: cobra.ArbitraryArgs, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			req, err := carryover.ParseRequest(args, pr, remote)
			if err != nil {
				return err
			}
			g := carryOverGit
			if g == nil {
				dir := checkout
				if dir == "" {
					dir = "."
				}
				g = carryover.NewRunner(dir)
			}
			res := carryover.Check(cmd.Context(), g, req)
			if jsonOutput {
				err = carryover.WriteJSON(cmd.OutOrStdout(), res)
			} else {
				err = carryover.WriteHuman(cmd.OutOrStdout(), res)
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if !res.CarryOver {
				return runtimeFailure{fmt.Errorf("carry-over not allowed: %v", res.Reasons)}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.StringVar(&pr, "pr", "", "Pull request number, written into the Carried-over line (required)")
	flags.StringVar(&remote, "remote", "", "Fetch the pull request head from this remote into refs/stw/carry-over/ and require it to equal NEW_HEAD")
	flags.StringVar(&checkout, "checkout", "", "Checkout or worktree that holds the objects (default: the current directory)")
	flags.BoolVar(&jsonOutput, "json", false, "Print one JSON document")
	return command
}
