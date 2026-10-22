// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// watchTransport and watchHook are test seams: production leaves the transport nil (HTTPS) and the hook unset.
var (
	watchTransport hosting.Transport
	watchHook      func(*watchcmd.Params)
)

func watchCommand() *cobra.Command {
	var f watchcmd.Flags
	var interval, timeout, maxIdle, pending string
	command := &cobra.Command{
		Use:   "watch [PATH] [--repo OWNER/NAME]... [--workspace] [--once | --stream | --until-event KIND[,KIND]...] [--status]",
		Short: "Watch the open pull requests of GitHub repositories and print only the transitions",
		// Hidden until Spec 035 is accepted, like stw status was.
		Hidden: true,
		Long: "Read-only watcher: polls the open pull requests of one or more GitHub repositories, compares each poll with the previous\n" +
			"one and prints only the transitions as JSON Lines events (new-pr, head-changed, checks-green, checks-red,\n" +
			"checks-pending-timeout, mergeable-changed, ready-state-changed, base-changed, merged, closed, repos-changed, watch-error,\n" +
			"watch-recovered, watch-stalled). The first run is a silent baseline; --baseline forces one. It never marks a PR Ready,\n" +
			"reruns a check, merges, comments or edits, and it decides nothing: a consumer reads the stream.\n" +
			"Repositories come from --repo flags, then --workspace (the root and every initialized submodule with a GitHub remote, found\n" +
			"as stw status finds the workspace, from PATH or the current directory), then the repos of --config. Without any of them the\n" +
			".stw/watch.toml of the current workspace is used when it exists. Authentication is the gh token route of the hosting\n" +
			"commands; a missing token is exit 1 before any request, and no token is ever printed or stored.\n" +
			"Modes: --once polls every repository once; --stream (default) polls until SIGINT or SIGTERM; --until-event KIND[,KIND]...\n" +
			"is --stream that exits 0 right after the poll that printed a listed kind (--timeout, default 590s, exits 4 first).\n" +
			"--status reads the state directory only (no network, no lock) and reports the heartbeat; --human prints one line per event.\n" +
			"State is kept under $XDG_STATE_HOME/stw/watch/<workspace-id>/ (mode 0700), never inside a repository, unless --state-dir\n" +
			"names an absolute directory. Exit statuses: 0 done, 1 failure (no repository readable in --once, missing authentication,\n" +
			"unwritable or corrupt state), 2 usage or config error, 3 another watcher holds the lock, 4 --until-event timed out,\n" +
			"5 --status heartbeat stale or missing.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cmd.Flags()
			if len(args) == 1 {
				f.WorkspacePath = args[0]
			}
			f.Interval, f.IntervalSet = interval, flags.Changed("interval")
			f.Timeout, f.TimeoutSet = timeout, flags.Changed("timeout")
			f.MaxIdle, f.MaxIdleSet = maxIdle, flags.Changed("max-idle")
			f.PendingTO, f.PendingTOSet = pending, flags.Changed("pending-timeout")
			f.DetailSet = flags.Changed("detail-every")
			prepared, err := watchcmd.Prepare(cmd.Context(), f, cmd.OutOrStdout())
			if err != nil {
				return err
			}
			if prepared.IsStat {
				return watchcmd.RunStatus(prepared.Status)
			}
			prepared.Run.Transport, prepared.Run.Diag = watchTransport, cmd.ErrOrStderr()
			if watchHook != nil {
				watchHook(&prepared.Run)
			}
			return watchcmd.Run(cmd.Context(), prepared.Run)
		},
	}
	flags := command.Flags()
	flags.StringArrayVar(&f.Repos, "repo", nil, "Watch OWNER/NAME (repeatable)")
	flags.BoolVar(&f.Workspace, "workspace", false, "Watch the root and every initialized submodule with a GitHub remote (PATH or the current directory)")
	flags.StringVar(&f.Config, "config", "", "Read this watch.toml (its repos join the union; flags override its settings)")
	flags.StringVar(&interval, "interval", "60s", "Polling interval (minimum 10s; a jitter of up to 5s is added)")
	flags.StringVar(&f.StateDir, "state-dir", "", "Absolute state directory (default $XDG_STATE_HOME/stw/watch/<workspace-id>)")
	flags.StringVar(&f.Host, "host", watchcmd.DefaultHost, "GitHub host (one per run)")
	flags.BoolVar(&f.Once, "once", false, "Poll every repository once, print its events and exit")
	flags.BoolVar(&f.Stream, "stream", false, "Poll until SIGINT or SIGTERM (default)")
	flags.StringSliceVar(&f.UntilEvent, "until-event", nil, "Exit 0 right after the poll that printed one of these event kinds")
	flags.StringVar(&timeout, "timeout", "590s", "With --until-event: exit 4 after this long without a matching event")
	flags.BoolVar(&f.Baseline, "baseline", false, "Treat the poll as the first run: silent, state replaced")
	flags.BoolVar(&f.Human, "human", false, "One line per event instead of JSON Lines")
	flags.StringVar(&maxIdle, "max-idle", "0", "With --stream: exit 0 after this long without an event (0: off)")
	flags.StringVar(&pending, "pending-timeout", "45m", "Report checks-pending-timeout after this long (0 disables)")
	flags.IntVar(&f.DetailEvery, "detail-every", watchcmd.DefaultDetailEvery, "Cycles between refreshes of PR detail and settled checks (minimum 1)")
	flags.BoolVar(&f.Status, "status", false, "Report the liveness of the watcher from its state directory (exit 5 when stale)")
	flags.BoolVar(&f.JSON, "json", false, "With --status: output one JSON object")
	return command
}
