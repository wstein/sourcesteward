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

func syncCommand() *cobra.Command { return syncCommandWith(stwsync.Sync, stwsync.Restore) }

func syncCommandWith(sync func(context.Context, stwsync.Options) stwsync.Result, restore func(context.Context, stwsync.RestoreOptions) stwsync.Result) *cobra.Command {
	var runID string
	var jsonOutput, planOnly, dryRun, rebase, ffOnly, merge, autostash, noAutostash, prune, noPrune bool
	var options stwsync.Options
	command := &cobra.Command{
		Use:   "sync [restore] [PATH] [--run ID] [--plan] [--rebase|--ff-only|--merge] [--autostash|--no-autostash] [--prune|--no-prune] [--json]",
		Short: "Bring the workspace root and its submodules up to date, like git pull",
		Long: "Do what git pull would do in every repository of the workspace, from any directory inside it: the top-level\n" +
			"superproject and, depth-first, each initialized submodule (nested ones too; uninitialized ones are reported, never cloned).\n" +
			"Only the main worktree of each repository is synced, on whatever branch it sits (main, develop, a feature branch) as long as\n" +
			"that branch has an upstream; linked worktrees are never touched. A detached HEAD (a pinned submodule) is reported only.\n" +
			"There is no separate apply step; stw sync is the action, --plan (alias --dry-run) the preview.\n" +
			"It is never stricter than git pull, except in one respect (owner decision 2026-10-24): it never creates a merge commit on\n" +
			"its own. pull.rebase, branch.NAME.rebase, pull.ff=only, rebase.autoStash, merge.autoStash and fetch.prune apply as they do\n" +
			"for git pull, so with pull.rebase=true a diverged branch is rebased and with rebase.autoStash=true a dirty tree is stashed\n" +
			"and restored. --rebase, --ff-only, --merge, --autostash, --no-autostash, --prune and --no-prune override the configuration.\n" +
			"A branch that can be fast-forwarded is fast-forwarded. A diverged branch is rebased only with --rebase or pull.rebase=true,\n" +
			"merged only with the explicit --merge (no configuration, pull.rebase=false and pull.ff included, enables a merge commit),\n" +
			"and otherwise reported as needing a choice and left unchanged. A rebase or merge conflict is left in progress as git leaves it.\n" +
			"A branch without an upstream, an operation in\n" +
			"progress, and local changes the update would overwrite are reported and skipped; a failure in one repository does not\n" +
			"stop the others. --plan fetches the remote-tracking refs, the one change it makes, and prints what a run would do.\n" +
			"Each branch change is journaled with a recovery ref under refs/stw/recovery/sync/RUN_ID. Git only: a jj-owned path is refused.\n" +
			"stw sync restore [PATH] [--run ID] moves each branch the run changed back to its journaled old tip, only where the branch\n" +
			"still points at the tip the run left and a checked-out worktree is clean (git reset --keep); other branches are reported\n" +
			"changed-since and left alone. The default run is the latest one; pruned refs, stash reapplies and submodule moves are not undone.\n" +
			"Exit statuses: 0 unless a repository failed (failed, conflict, autostash-conflict) or the run was refused, 1 then, 2 usage error.",
		Args:         cobra.MaximumNArgs(2),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] == "restore" {
				for _, name := range []string{"plan", "dry-run", "rebase", "ff-only", "merge", "autostash", "no-autostash", "prune", "no-prune"} {
					if cmd.Flags().Changed(name) {
						return fmt.Errorf("--%s does not apply to stw sync restore", name)
					}
				}
				result := restore(cmd.Context(), stwsync.RestoreOptions{Path: workspacePath(args[1:]), RunID: runID})
				return finishSync(cmd, result, jsonOutput)
			}
			if len(args) == 2 {
				return errors.New("stw sync takes at most one PATH; use stw sync restore [PATH] to undo a run")
			}
			if runID != "" {
				return errors.New("--run applies to stw sync restore only")
			}
			options.Path = workspacePath(args)
			options.Plan = planOnly || dryRun
			options.Rebase = triFlag(rebase, ffOnly)
			options.Merge = merge
			options.Autostash = triFlag(autostash, noAutostash)
			options.Prune = triFlag(prune, noPrune)
			if err := stwsync.ValidateOptions(options); err != nil {
				return err
			}
			return finishSync(cmd, sync(cmd.Context(), options), jsonOutput)
		},
	}
	flags := command.Flags()
	flags.BoolVar(&planOnly, "plan", false, "Fetch the remote-tracking refs and print what a run would do; change nothing else")
	flags.BoolVar(&dryRun, "dry-run", false, "Alias of --plan")
	flags.BoolVar(&rebase, "rebase", false, "Rebase a diverged branch onto its upstream (overrides pull.rebase)")
	flags.BoolVar(&ffOnly, "ff-only", false, "Only fast-forward; a diverged branch is reported (overrides pull.rebase)")
	flags.BoolVar(&merge, "merge", false, "Merge a diverged branch (the only way to get a merge commit; a fast-forwardable branch is still fast-forwarded)")
	flags.BoolVar(&autostash, "autostash", false, "Stash local changes around the update and reapply them (overrides rebase.autoStash)")
	flags.BoolVar(&noAutostash, "no-autostash", false, "Never stash local changes")
	flags.BoolVar(&prune, "prune", false, "Fetch with --prune (overrides fetch.prune)")
	flags.BoolVar(&noPrune, "no-prune", false, "Fetch without pruning")
	flags.StringVar(&runID, "run", "", "With restore: the run ID to undo (default: the latest run)")
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned JSON object")
	command.MarkFlagsMutuallyExclusive("rebase", "ff-only", "merge")
	command.MarkFlagsMutuallyExclusive("autostash", "no-autostash")
	command.MarkFlagsMutuallyExclusive("prune", "no-prune")
	return command
}

// finishSync prints one result and maps it to the exit status (1 for a refusal or an item needing attention).
func finishSync(cmd *cobra.Command, result stwsync.Result, jsonOutput bool) error {
	if result.Error != nil && !jsonOutput {
		return runtimeFailure{fmt.Errorf("sync: %s: %s", result.Error.Code, result.Error.Message)}
	}
	var err error
	if jsonOutput {
		err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	} else {
		err = renderSyncText(cmd.OutOrStdout(), result, stwsync.ColorEnabled(cmd.OutOrStdout()))
	}
	if err != nil {
		return runtimeFailure{err}
	}
	if err = cmd.Context().Err(); err != nil {
		return runtimeFailure{err}
	}
	if result.Error != nil {
		return runtimeFailure{fmt.Errorf("sync: %s: %s", result.Error.Code, result.Error.Message)}
	}
	if result.ExitCode() != 0 {
		return runtimeFailure{fmt.Errorf("sync: %d of %d repositories need attention", syncAttention(result), len(result.Repos))}
	}
	return nil
}

func triFlag(on, off bool) stwsync.Tri {
	switch {
	case on:
		return stwsync.On
	case off:
		return stwsync.Off
	}
	return stwsync.Unset
}

func syncAttention(r stwsync.Result) int {
	n := 0
	for _, it := range r.Repos {
		switch it.Result {
		case stwsync.ResultFailed, stwsync.ResultConflict, stwsync.ResultAutostashConflict, stwsync.ResultChangedSince, stwsync.ResultJournalIncomplete:
			n++
		}
	}
	return n
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// syncText is the human status of one repository and the colour class of its line.
func syncText(rp stwsync.Repo, plan bool) (text, class string) {
	moved := reposAbbrevOID(rp.OldOID)
	if rp.NewOID != nil {
		moved += ".." + reposAbbrevOID(rp.NewOID)
	}
	reason := ""
	if rp.Reason != nil {
		reason = *rp.Reason
	}
	skipped := func(label string) (string, string) {
		if reason != "" {
			return "skipped: " + label + ": " + reason, "yellow"
		}
		return "skipped: " + label, "yellow"
	}
	switch rp.Result {
	case stwsync.ResultUpToDate:
		return "up to date", "dim"
	case stwsync.ResultAhead:
		return "up to date, " + plural(rp.Ahead, "local commit", "local commits") + " ahead", "dim"
	case stwsync.ResultFastForward:
		return "would fast-forward " + plural(rp.Behind, "commit", "commits") + " " + moved, "green"
	case stwsync.ResultFastForwarded:
		return "fast-forwarded " + plural(rp.Behind, "commit", "commits") + " " + moved, "green"
	case stwsync.ResultRebase:
		return fmt.Sprintf("would rebase %s onto %d new", plural(rp.Ahead, "local commit", "local commits"), rp.Behind), "green"
	case stwsync.ResultRebased:
		return fmt.Sprintf("rebased %s onto %d new %s", plural(rp.Ahead, "local commit", "local commits"), rp.Behind, moved), "green"
	case stwsync.ResultMerge:
		return fmt.Sprintf("would merge %s into %s", plural(rp.Behind, "upstream commit", "upstream commits"), plural(rp.Ahead, "local commit", "local commits")), "green"
	case stwsync.ResultMerged:
		return fmt.Sprintf("merged %s %s", plural(rp.Behind, "upstream commit", "upstream commits"), moved), "green"
	case stwsync.ResultDiverged:
		text, class = skipped("diverged (" + fmt.Sprintf("%d ahead, %d behind", rp.Ahead, rp.Behind) + ")")
		return strings.Replace(text, "): ", ") - ", 1), class
	case stwsync.ResultSkippedDirty:
		return skipped("dirty")
	case stwsync.ResultSkippedDetached:
		return "skipped: " + reason, "yellow"
	case stwsync.ResultSkippedNoUpstream:
		return "skipped: " + reason, "yellow"
	case stwsync.ResultSkippedBusy:
		return skipped("busy")
	case stwsync.ResultSkippedUninitialized:
		return "skipped: " + reason, "yellow"
	case stwsync.ResultRestored:
		return "restored " + moved, "green"
	case stwsync.ResultAlreadyRestored:
		return "already restored", "dim"
	case stwsync.ResultChangedSince:
		return "changed since: " + reason, "yellow"
	case stwsync.ResultJournalIncomplete:
		return "journal incomplete: " + reason, "yellow"
	case stwsync.ResultConflict:
		return "conflict: " + reason, "red"
	case stwsync.ResultAutostashConflict:
		return "autostash conflict: " + reason + " " + moved, "red"
	}
	return "failed: " + reason, "red"
}

var syncColors = map[string]string{"green": "32", "yellow": "33", "red": "31", "dim": "2"}

func renderSyncText(w io.Writer, r stwsync.Result, color bool) error {
	var b strings.Builder
	line := func(s string) { b.WriteString(escapeReposLine(s) + "\n") }
	paint := func(s, class string) string {
		if !color || syncColors[class] == "" {
			return s
		}
		return "\x1b[" + syncColors[class] + "m" + s + "\x1b[0m"
	}
	plan := r.Mode == "plan"
	if r.Mode == "restore" {
		line("SourceSteward sync restore: " + plural(len(r.Repos), "branch", "branches"))
	} else if plan {
		line("SourceSteward sync plan: " + plural(len(r.Repos), "repository", "repositories") + "; only remote-tracking refs were fetched")
	} else {
		line("SourceSteward sync: " + plural(len(r.Repos), "repository", "repositories"))
	}
	if r.Note != nil {
		line("note: " + *r.Note)
	}
	nameWidth, branchWidth := 1, 1
	for _, rp := range r.Repos {
		nameWidth, branchWidth = max(nameWidth, min(len(escapeReposLine(rp.Name)), 40)), max(branchWidth, min(len(syncBranch(rp)), 30))
	}
	for _, rp := range r.Repos {
		text, class := syncText(rp, plan)
		prefix := fmt.Sprintf("%-*s  %-*s  ", nameWidth, rp.Name, branchWidth, syncBranch(rp))
		// escape the whole plain line first, then colour the status part so that no repository text can inject escapes
		plain := escapeReposLine(prefix + text)
		if color && strings.HasSuffix(plain, escapeReposLine(text)) {
			cut := len(plain) - len(escapeReposLine(text))
			plain = plain[:cut] + paint(plain[cut:], class)
		}
		b.WriteString(plain + "\n")
		if rp.Detail != nil && (rp.Result == stwsync.ResultConflict || rp.Result == stwsync.ResultAutostashConflict) {
			for _, d := range strings.Split(*rp.Detail, "\n") {
				if strings.TrimSpace(d) != "" {
					line("    " + d)
				}
			}
		}
	}
	var counts []string
	order := []string{stwsync.ResultFastForwarded, stwsync.ResultFastForward, stwsync.ResultRebased, stwsync.ResultRebase, stwsync.ResultMerged, stwsync.ResultMerge, stwsync.ResultUpToDate, stwsync.ResultAhead, stwsync.ResultDiverged, stwsync.ResultSkippedDirty, stwsync.ResultSkippedDetached, stwsync.ResultSkippedNoUpstream, stwsync.ResultSkippedBusy, stwsync.ResultSkippedUninitialized, stwsync.ResultConflict, stwsync.ResultAutostashConflict, stwsync.ResultRestored, stwsync.ResultAlreadyRestored, stwsync.ResultChangedSince, stwsync.ResultJournalIncomplete, stwsync.ResultFailed}
	for _, name := range order {
		if n := r.Summary.ByResult[name]; n > 0 {
			counts = append(counts, fmt.Sprintf("%d %s", n, strings.ReplaceAll(name, "-", " ")))
		}
	}
	pruned := 0
	for _, rp := range r.Repos {
		pruned += rp.Pruned
	}
	summary := "summary: " + strings.Join(counts, ", ")
	if pruned > 0 {
		summary += "; " + plural(pruned, "remote-tracking ref", "remote-tracking refs") + " pruned"
	}
	line(summary)
	if r.RunID != nil {
		line("journal: run " + *r.RunID + ", recovery refs under refs/stw/recovery/sync/" + *r.RunID)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func syncBranch(rp stwsync.Repo) string {
	if rp.Branch == nil {
		return "-"
	}
	return *rp.Branch
}
