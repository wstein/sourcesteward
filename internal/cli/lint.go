// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/lintprcmd"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// lintTransport is a test seam: production leaves the transport nil (HTTPS).
var lintTransport hosting.Transport

func lintCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "lint",
		Short: "Check work products against the working rules (read-only)",
		Args:  cobra.NoArgs, SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	command.AddCommand(lintPRCommand())
	return command
}

func lintPRCommand() *cobra.Command {
	var f lintprcmd.Flags
	command := &cobra.Command{
		Use:   "pr PR [PATH] [--repo OWNER/NAME] [--owner-identity \"Name <email>\"]... [--human | --json]",
		Short: "Check the commits, text and comments of a pull request against the machine-checkable working rules",
		Long: "Read-only (GET only). Reads one pull request, its commits (up to 300) and its conversation comments (up to 300) and reports one finding\n" +
			"per violated rule with the rule ID, the severity and the location (PR title or text, comment ID, commit SHA, line). Rules: no session link\n" +
			"(Claude session trailer or code-session URL) in commit messages, PR title, PR text and comments; author and committer of every commit is an\n" +
			"owner identity; Co-Authored-By trailer; Conventional Commit subjects; PR text about 25 lines (warning above 25, error above 30), a Blocker\n" +
			"line and an Author agent line; the first line of a review comment follows `Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n>`\n" +
			"(or the Carried-over form) for this pull request; a review comment has at most 15 lines. The rule table is data (package lintpr) that cites\n" +
			"the agent-rules core; `--json` lists it.\n" +
			"The repository is --repo or, without it, the single GitHub repository of the workspace of PATH (default: the current directory).\n" +
			"Owner identities are --owner-identity (repeatable), else user.name and user.email of the Git configuration read from that directory;\n" +
			"without any, usage error. Output is human on a terminal and one JSON document otherwise (--human, --json). Authentication is the gh token\n" +
			"route of the hosting commands. Nothing is written to disk or to the forge.\n" +
			"Exit statuses: 0 no finding of severity error (warnings do not fail), 3 at least one error finding or an incomplete read (a list cut at its bound; the output is printed), 1 a failed\n" +
			"read or missing authentication (no output), 2 usage error.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("%w: the pull request %q is not a number", watchcmd.ErrUsage, args[0])
			}
			f.PR = n
			if len(args) == 2 {
				f.WorkspacePath = args[1]
			}
			params, err := lintprcmd.Prepare(cmd.Context(), f, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			params.Transport, params.Diag = lintTransport, cmd.ErrOrStderr()
			return lintprcmd.Execute(cmd.Context(), params)
		},
	}
	flags := command.Flags()
	flags.StringVar(&f.Repo, "repo", "", "Pull request repository OWNER/NAME (default: the single GitHub repository of the workspace)")
	flags.StringVar(&f.Host, "host", watchcmd.DefaultHost, "GitHub host")
	flags.StringArrayVar(&f.Owners, "owner-identity", nil, "Owner identity \"Name <email>\" for the author and committer rules (repeatable; default: user.name and user.email of the Git configuration)")
	flags.BoolVar(&f.Human, "human", false, "Print the human list (default on a terminal)")
	flags.BoolVar(&f.JSON, "json", false, "Print one JSON document (default otherwise)")
	return command
}
