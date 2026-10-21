// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

func syncCommand() *cobra.Command { return syncCommandWith(stwsync.Plan, stwsync.Run) }

func syncCommandWith(plan, run func(context.Context, stwsync.Options) stwsync.Result) *cobra.Command {
	var jsonOutput, planOnly, dryRun bool
	var options stwsync.Options
	command := &cobra.Command{
		Use:   "sync [PATH] [--remote NAME] [--prune] [--json]",
		Short: "Fetch one remote and fast-forward every local branch that tracks it (--plan only reports)",
		Long: "Fetch one remote (remote-tracking refs only) and classify every local branch that tracks it:\n" +
			"fast-forward, up-to-date, ahead, diverged, skipped-dirty (a checked-out branch whose worktree has\n" +
			"tracked or staged changes) and upstream-gone (only after --prune removed the remote-tracking ref).\n" +
			"Branches without an upstream under the remote are counted, not listed. The plan changes no local branch,\n" +
			"worktree or stash and never creates a merge commit; the repository's own hooks run as Git runs them for a fetch.\n" +
			"Without --plan the run then fast-forwards every fast-forward branch (git merge --ff-only in the worktree of a\n" +
			"checked-out branch, a ref update with the expected old value for any other), journals each ref change with a\n" +
			"recovery ref under refs/stw/recovery/sync/RUN_ID and reports fast-forwarded or failed per branch.\n" +
			"Git only: a jj-owned path is refused.\n" +
			"Exit statuses: 0 nothing needs attention, 1 a run failure or a skipped-dirty, diverged or failed branch, 2 usage error.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && args[0] == "restore" {
				return errors.New("stw sync restore is not available yet; use ./restore for a directory of that name")
			}
			options.Path = workspacePath(args)
			if cmd.Flags().Changed("remote") && options.Remote == "" {
				return errors.New("--remote must not be empty")
			}
			if err := stwsync.ValidateOptions(options); err != nil {
				return err
			}
			mode, operation := "sync", run
			if planOnly || dryRun {
				mode, operation = "sync plan", plan
			}
			result := operation(cmd.Context(), options)
			if result.Error != nil && !jsonOutput {
				return runtimeFailure{fmt.Errorf("%s: %s: %s", mode, result.Error.Code, result.Error.Message)}
			}
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				err = renderSyncText(cmd.OutOrStdout(), result)
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if err = cmd.Context().Err(); err != nil {
				return runtimeFailure{err}
			}
			if result.Error != nil {
				return runtimeFailure{fmt.Errorf("%s: %s: %s", mode, result.Error.Code, result.Error.Message)}
			}
			if result.ExitCode() != 0 {
				return runtimeFailure{fmt.Errorf("%s: %d of %d branches need attention", mode, syncAttention(result), len(result.Items))}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.BoolVar(&planOnly, "plan", false, "Fetch and print what a run would do; changes only remote-tracking refs")
	flags.BoolVar(&dryRun, "dry-run", false, "Alias of --plan")
	flags.StringVar(&options.Remote, "remote", "", "Remote to fetch and compare with (default origin)")
	flags.BoolVar(&options.Prune, "prune", false, "Fetch with --prune: remove stale remote-tracking refs of that remote")
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned JSON object")
	return command
}

func syncAttention(r stwsync.Result) int {
	n := 0
	for _, it := range r.Items {
		if it.Result == stwsync.ResultSkippedDirty || it.Result == stwsync.ResultDiverged || it.Result == stwsync.ResultFailed {
			n++
		}
	}
	return n
}

func syncOID(oid *string) string {
	if oid == nil {
		return "-"
	}
	return reposAbbrevOID(oid)
}

func renderSyncText(w io.Writer, r stwsync.Result) error {
	var b strings.Builder
	line := func(s string) { b.WriteString(escapeReposLine(s) + "\n") }
	if r.Mode == "plan" {
		line(fmt.Sprintf("SourceSteward sync plan: remote %s, %d branches", r.Remote, len(r.Items)))
		line("effects: one fetch of the remote-tracking refs; no local branch, worktree or stash changed")
	} else {
		id := "-"
		if r.RunID != nil {
			id = *r.RunID
		}
		line(fmt.Sprintf("SourceSteward sync: remote %s, %d branches, run %s", r.Remote, len(r.Items), id))
		line("effects: one fetch; fast-forwarded branches journaled with recovery refs under refs/stw/recovery/sync")
	}
	for _, it := range r.Items {
		tree := "-"
		if it.Worktree != nil {
			tree = *it.Worktree
		}
		moved := syncOID(it.OldOID)
		if it.NewOID != nil {
			moved += ".." + syncOID(it.NewOID)
		}
		text := fmt.Sprintf("%s  %s  %s  %s", it.Branch, tree, it.Result, moved)
		if it.Reason != nil {
			text += "  (" + *it.Reason + ")"
		}
		line(text)
	}
	var counts []string
	for _, name := range []string{stwsync.ResultFastForward, stwsync.ResultFastForwarded, stwsync.ResultFailed, stwsync.ResultUpToDate, stwsync.ResultAhead, stwsync.ResultDiverged, stwsync.ResultSkippedDirty, stwsync.ResultUpstreamGone} {
		if n := r.Summary.ByResult[name]; n > 0 {
			counts = append(counts, fmt.Sprintf("%s %d", name, n))
		}
	}
	counts = append(counts, fmt.Sprintf("untracked %d", r.Summary.Untracked))
	line("summary: " + strings.Join(counts, ", ") + fmt.Sprintf("; fetch %t; pruned %d", r.Fetch.Performed, r.Fetch.PrunedTotal))
	for _, ref := range r.Fetch.PrunedRefs {
		line("pruned " + ref)
	}
	if r.Fetch.PrunedTotal > len(r.Fetch.PrunedRefs) {
		line(fmt.Sprintf("pruned +%d more", r.Fetch.PrunedTotal-len(r.Fetch.PrunedRefs)))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
