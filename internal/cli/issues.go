// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/issuescmd"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// issuesTransport is a test seam: production leaves it nil (HTTPS).
var issuesTransport hosting.Transport

func issuesCommand() *cobra.Command {
	var f issuescmd.Flags
	command := &cobra.Command{
		Use:   "issues [PATH] [--workspace] [--repo OWNER/NAME]... [--owner OWNER]... [--label NAME]... [--assignee LOGIN|@me|@none]... [--author LOGIN]... [--limit N] [--human | --json]",
		Short: "Print a read-only snapshot of every open issue, each with one priority",
		Long: "Read-only, stateless snapshot of all open issues (pull requests excluded), bot issues included, which the GitHub inbox does not list.\n" +
			"Every issue gets one priority by an ordered rule over all its labels: red (labelled security), yellow (assigned to the authenticated user, or\n" +
			"labelled bug), violet (opened by a bot) and blue (anything else). Labels match case-insensitively (ASCII) and exactly. The authenticated user\n" +
			"is the login the token resolves to, read once per run; when the token class cannot read it (403 or 404) the assigned rule is off and the run\n" +
			"goes on (exit 0), and --assignee @me then fails with viewer-unknown.\n" +
			"Repositories are the union of --workspace [PATH], --repo flags and the repositories of --owner (found through one fixed GitHub search; archived\n" +
			"repositories excluded). Without any of the three, the workspace of the current directory is used as stw status finds it (outside one: usage\n" +
			"error); .stw/watch.toml is not read. At most 256 repositories per run. Repositories with issues disabled are skipped, not failed.\n" +
			"--label, --assignee and --author (each repeatable; repeats of one flag are alternatives, different flags intersect) filter locally after the read and\n" +
			"never reach the forge. --limit N (default 200, 1 to 500) caps the printed rows after sorting by priority; the rest is counted in summary.omitted\n" +
			"and summary.counts covers everything read. A repository with more than 1000 open issues and pull requests keeps its oldest 1000, is marked\n" +
			"partial (too-many-open-issues) and exits 1.\n" +
			"Output is human on a terminal and one JSON document otherwise (--human, --json). Authentication is the gh token route of the hosting commands;\n" +
			"a missing token is exit 1 before any request and no token is ever printed. Nothing is written to disk or to the forge, and the suggested\n" +
			"gh issue view commands are never run. On rate-limit exhaustion the run stops reading, prints what it has and exits 1; it never sleeps.\n" +
			"Exit statuses: 0 all repositories read (also when nothing is open), 1 a repository, owner discovery or the viewer read failed or authentication\n" +
			"is missing (partial output is printed), 2 usage error.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.WorkspacePath = args[0]
			}
			params, err := issuescmd.Prepare(cmd.Context(), f, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			params.Transport, params.Diag = issuesTransport, cmd.ErrOrStderr()
			return issuescmd.Execute(cmd.Context(), params)
		},
	}
	flags := command.Flags()
	flags.StringArrayVar(&f.Repos, "repo", nil, "Include OWNER/NAME (repeatable)")
	flags.BoolVar(&f.Workspace, "workspace", false, "Include the root and every initialized submodule with a GitHub remote (PATH or the current directory)")
	flags.StringArrayVar(&f.Owners, "owner", nil, "Also include the repositories of this GitHub user or organisation that have open issues (repeatable)")
	flags.StringVar(&f.Host, "host", watchcmd.DefaultHost, "GitHub host (one per run)")
	flags.StringArrayVar(&f.Labels, "label", nil, "Show only issues with this label (repeatable; exact, ASCII case-insensitive)")
	flags.StringArrayVar(&f.Assignees, "assignee", nil, "Show only issues assigned to this login, @me (the authenticated user) or @none (repeatable)")
	flags.StringArrayVar(&f.Authors, "author", nil, "Show only issues opened by this login, a bot with its [bot] suffix (repeatable)")
	flags.IntVar(&f.Limit, "limit", 200, "Rows printed, red first (1 to 500)")
	flags.BoolVar(&f.Human, "human", false, "Print the human table (default on a terminal)")
	flags.BoolVar(&f.JSON, "json", false, "Print one JSON document (default otherwise)")
	return command
}
