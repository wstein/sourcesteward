// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/pullscmd"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// pullsTransport and pullsHook are test seams: production leaves the transport nil (HTTPS) and the hook unset.
var (
	pullsTransport hosting.Transport
	pullsHook      func(*pullscmd.Params)
)

func pullsCommand() *cobra.Command {
	var f pullscmd.Flags
	var noBots, botsOnly bool
	command := &cobra.Command{
		Use:   "pulls [PATH] [--workspace] [--repo OWNER/NAME]... [--owner OWNER]... [--no-bots | --bots-only] [--author LOGIN]... [--limit N] [--human | --json]",
		Short: "Print a read-only snapshot of every open pull request, grouped by what needs action",
		Long: "Read-only, stateless snapshot of all open pull requests, bot pull requests included, which the GitHub inbox does not list.\n" +
			"Every PR is placed in one group by an ordered rule: ready (GitHub reports it mergeable by rebase and the exact head has green checks),\n" +
			"checks (waiting on checks), conflict (conflicting or behind), draft, blocked (with a reason: checks-red, no-checks, unknown,\n" +
			"review-or-protection, unstable, not-rebaseable, not-read). A repository without any CI is never ready. ready says nothing about a review ledger.\n" +
			"Repositories are the union of --workspace [PATH], --repo flags and the repositories of --owner (found through one fixed GitHub search; archived\n" +
			"repositories excluded). Without any of the three, the workspace of the current directory is used as stw status finds it (outside one: usage\n" +
			"error); .stw/watch.toml is not read. At most 256 repositories per run. --no-bots, --bots-only and --author (repeatable) filter before the\n" +
			"detail budget is spent; --limit N (default 200, 1 to 500) is the number of non-draft pull requests (drafts too with --detail-drafts)\n" +
			"whose pulls/N and checks are read, newest first; the rest is listed as blocked, not-read and counted in summary.not_read.\n" +
			"Output is human on a terminal and one JSON document otherwise (--human, --json). Authentication is the gh token route of the hosting commands;\n" +
			"a missing token is exit 1 before any request and no token is ever printed. Nothing is written to disk or to the forge. On rate-limit\n" +
			"exhaustion the run stops reading, prints what it has and exits 1; it never sleeps and never loops.\n" +
			"GitHub computes mergeability lazily: a non-draft row with green checks and mergeable null or state unknown gets at most one bounded re-read\n" +
			"(pulls/N only, at the end, outside the --limit budget, never after a rate limit); it can stay blocked unknown, run again then.\n" +
			"Exit statuses: 0 all repositories read (also when nothing is open), 1 a repository or owner discovery failed or authentication is\n" +
			"missing (partial output is printed), 2 usage error.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.WorkspacePath = args[0]
			}
			f.NoBots, f.BotsOnly = noBots, botsOnly
			params, err := pullscmd.Prepare(cmd.Context(), f, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			params.Transport, params.Diag = pullsTransport, cmd.ErrOrStderr()
			if pullsHook != nil {
				pullsHook(&params)
			}
			return pullscmd.Execute(cmd.Context(), params)
		},
	}
	flags := command.Flags()
	flags.StringArrayVar(&f.Repos, "repo", nil, "Include OWNER/NAME (repeatable)")
	flags.BoolVar(&f.Workspace, "workspace", false, "Include the root and every initialized submodule with a GitHub remote (PATH or the current directory)")
	flags.StringArrayVar(&f.Owners, "owner", nil, "Also include the repositories of this GitHub user or organisation that have open pull requests (repeatable)")
	flags.StringVar(&f.Host, "host", watchcmd.DefaultHost, "GitHub host (one per run)")
	flags.BoolVar(&noBots, "no-bots", false, "Hide bot pull requests")
	flags.BoolVar(&botsOnly, "bots-only", false, "Show only bot pull requests")
	flags.StringArrayVar(&f.Authors, "author", nil, "Show only pull requests of this login (repeatable)")
	flags.BoolVar(&f.DetailDrafts, "detail-drafts", false, "Also read the detail and checks of drafts (they consume the --limit budget)")
	flags.IntVar(&f.Limit, "limit", 200, "Pull requests whose detail and checks are read (1 to 500)")
	flags.BoolVar(&f.Human, "human", false, "Print the human table (default on a terminal)")
	flags.BoolVar(&f.JSON, "json", false, "Print one JSON document (default otherwise)")
	return command
}
