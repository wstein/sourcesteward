// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/readycheck"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// readyCheckTransport and readyCheckHook are test seams: production leaves the transport nil
// (HTTPS) and the hook unset.
var (
	readyCheckTransport hosting.Transport
	readyCheckHook      func(*readycheck.Params)
)

func readyCheckCommand() *cobra.Command {
	var f readycheck.Flags
	command := &cobra.Command{
		Use:   "ready-check NUMBER [--repo OWNER/NAME] [--host HOST] [--human | --json]",
		Short: "Check read-only whether a pull request meets the preconditions for marking it Ready",
		Long: "Read-only check of the Ready preconditions of one pull request; it never marks anything Ready and writes nothing to GitHub.\n" +
			"Conditions, each with pass, fail or unknown and its evidence: open (the pull request is open), review (the review state of the exact head is\n" +
			"clear, or clear-carried by a carry-over chain; Spec 046), checks (all check runs and commit statuses at the exact head are green; the list of\n" +
			"required checks is not read, so this is at least as strict), base (the base is the default branch, or the text says \"Merge #N first\"),\n" +
			"blocker (no \"Blocker:\" line in the text names a blocker; none, keiner, n/a and - mean no blocker), conflict (GitHub reports no conflict with\n" +
			"the base) and rebaseable (GitHub reports the pull request rebaseable). A value GitHub has not computed yet, or a read that failed, is unknown\n" +
			"and does not pass. Being a Draft is reported but is not a condition. Without --repo the workspace of the current directory must have exactly\n" +
			"one GitHub repository.\n" +
			"Output is human on a terminal and one JSON document otherwise (--human, --json). Authentication is the gh token route of the hosting\n" +
			"commands. Exit statuses: 0 every condition passes, 1 a condition failed or is unknown or a read failed (the document is still printed),\n" +
			"2 usage error.",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			params, err := readycheck.Prepare(cmd.Context(), f, args, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			params.Transport, params.Review = readyCheckTransport, readycheck.ReviewUnavailable
			if readyCheckHook != nil {
				readyCheckHook(&params)
			}
			return readycheck.Execute(cmd.Context(), params)
		},
	}
	flags := command.Flags()
	flags.StringVar(&f.Repo, "repo", "", "OWNER/NAME of the pull request (default: the one GitHub repository of the workspace)")
	flags.StringVar(&f.Host, "host", watchcmd.DefaultHost, "GitHub host")
	flags.BoolVar(&f.Human, "human", false, "Print the human list (default on a terminal)")
	flags.BoolVar(&f.JSON, "json", false, "Print one JSON document (default otherwise)")
	return command
}
