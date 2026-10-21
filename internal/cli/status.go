// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

func workspaceStatusCommand() *cobra.Command { return workspaceStatusCommandWith(stwsync.Status) }

func workspaceStatusCommandWith(status func(context.Context, stwsync.StatusOptions) stwsync.StatusResult) *cobra.Command {
	var jsonOutput bool
	var options stwsync.StatusOptions
	command := &cobra.Command{
		Use:   "status [PATH] [--fetch] [--worktrees] [--json]",
		Short: "Show where you are in the workspace root and every submodule, in one screen",
		Long: "Read-only overview of the whole workspace, from any directory inside it: what git status, git worktree list and\n" +
			"git branch -vv tell for the top-level superproject and each initialized submodule (nested ones too), main worktree\n" +
			"only. Per repository: branch or detached HEAD, upstream with ahead/behind, staged/unstaged/untracked counts,\n" +
			"conflicts, a rebase/merge/cherry-pick/revert/bisect in progress, stashes, local branches not yet merged into the\n" +
			"default branch of the upstream remote, linked worktrees (dirty, locked, prunable) and, for a submodule, the drift\n" +
			"between the commit the superproject records and the one checked out.\n" +
			"A repository needs attention when it has an operation in progress, conflicts, staged, unstaged or untracked files,\n" +
			"commits ahead of or behind its upstream, a gone or missing upstream, a drifted gitlink, a dirty, locked or prunable\n" +
			"linked worktree or a failed fetch; stashes, unmerged branches and a detached HEAD are shown but do not count.\n" +
			"Nothing is changed and nothing is fetched unless --fetch asks for it (one git fetch per repository, remote-tracking\n" +
			"refs only). --worktrees lists the linked worktrees. Colour only on a terminal (NO_COLOR turns it off). Git only.\n" +
			"Exit statuses: 0 also when repositories need attention, 1 when a repository cannot be read or the run fails, 2 usage error.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Path = workspacePath(args)
			if err := stwsync.ValidateOptions(stwsync.Options{Path: options.Path}); err != nil {
				return err
			}
			result := status(cmd.Context(), options)
			if result.Error != nil && !jsonOutput {
				return runtimeFailure{fmt.Errorf("status: %s: %s", result.Error.Code, result.Error.Message)}
			}
			var err error
			if jsonOutput {
				err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
			} else {
				err = renderStatusText(cmd.OutOrStdout(), result, options.Worktrees, stwsync.ColorEnabled(cmd.OutOrStdout()))
			}
			if err != nil {
				return runtimeFailure{err}
			}
			if err = cmd.Context().Err(); err != nil {
				return runtimeFailure{err}
			}
			if result.Error != nil {
				return runtimeFailure{fmt.Errorf("status: %s: %s", result.Error.Code, result.Error.Message)}
			}
			if result.Summary.Errors > 0 {
				return runtimeFailure{fmt.Errorf("status: %s cannot be read", plural(result.Summary.Errors, "repository", "repositories"))}
			}
			return nil
		},
	}
	flags := command.Flags()
	flags.BoolVar(&options.Fetch, "fetch", false, "Fetch every repository first (remote-tracking refs only; the one thing that is not read-only)")
	flags.BoolVar(&options.Worktrees, "worktrees", false, "List the linked worktrees of each repository")
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned JSON object")
	return command
}

func statusSyncText(rp stwsync.RepoStatus) string {
	switch {
	case rp.State == "uninitialized":
		if rp.Gitlink != nil && rp.Gitlink.Recorded != nil {
			return "not initialized, recorded " + reposAbbrevOID(rp.Gitlink.Recorded)
		}
		return "not initialized"
	case rp.State == "error":
		return rp.Error.Message
	case rp.Detached:
		return "detached at " + reposAbbrevOID(rp.Head)
	case rp.Upstream == nil:
		return "no upstream"
	case rp.UpstreamGone:
		return *rp.Upstream + " (gone)"
	case *rp.Ahead == 0 && *rp.Behind == 0:
		return *rp.Upstream + " up to date"
	case *rp.Behind == 0:
		return fmt.Sprintf("%s ahead %d", *rp.Upstream, *rp.Ahead)
	case *rp.Ahead == 0:
		return fmt.Sprintf("%s behind %d", *rp.Upstream, *rp.Behind)
	}
	return fmt.Sprintf("%s ahead %d, behind %d", *rp.Upstream, *rp.Ahead, *rp.Behind)
}

func statusChangesText(c stwsync.Changes) string {
	var parts []string
	for _, p := range []struct {
		n    int
		text string
	}{{c.Staged, "staged"}, {c.Unstaged, "unstaged"}, {c.Untracked, "untracked"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.text))
		}
	}
	if c.Conflicts > 0 {
		parts = append(parts, plural(c.Conflicts, "conflict", "conflicts"))
	}
	if len(parts) == 0 {
		return "clean"
	}
	return strings.Join(parts, ", ")
}

// statusDetails are the extra facts of one repository, one line each, only when there is something to say.
func statusDetails(rp stwsync.RepoStatus) []string {
	var lines []string
	if rp.Operation != nil {
		if *rp.Operation == "unreadable" {
			lines = append(lines, "the worktree state cannot be read")
		} else {
			lines = append(lines, *rp.Operation+" in progress")
		}
	}
	if rp.Stash > 0 {
		lines = append(lines, "stash "+fmt.Sprint(rp.Stash))
	}
	if rp.Branches.NotIntegrated != nil && *rp.Branches.NotIntegrated > 0 {
		lines = append(lines, fmt.Sprintf("branches: %d local, %d not integrated into %s", rp.Branches.Local, *rp.Branches.NotIntegrated, *rp.Branches.Base))
	}
	if w := rp.Worktrees; w.Linked > 0 {
		var flags []string
		for _, f := range []struct {
			n    int
			text string
		}{{w.Dirty, "dirty"}, {w.Locked, "locked"}, {w.Prunable, "prunable"}} {
			if f.n > 0 {
				flags = append(flags, fmt.Sprintf("%d %s", f.n, f.text))
			}
		}
		text := plural(w.Linked, "linked worktree", "linked worktrees")
		if len(flags) > 0 {
			text += " (" + strings.Join(flags, ", ") + ")"
		}
		lines = append(lines, text)
	}
	if g := rp.Gitlink; g != nil && g.State == "drift" {
		text := "gitlink drift: recorded " + reposAbbrevOID(g.Recorded) + ", checked out " + reposAbbrevOID(g.CheckedOut)
		if g.Relation != nil && *g.Relation != "unknown" {
			text += " (checked out is " + *g.Relation + ")"
		}
		lines = append(lines, text)
	}
	if rp.FetchError != nil {
		lines = append(lines, "fetch failed: "+*rp.FetchError)
	}
	return lines
}

func renderStatusText(w io.Writer, r stwsync.StatusResult, worktrees, color bool) error {
	var b strings.Builder
	paint := func(s, code string) string {
		if !color || code == "" {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
	// every piece of repository text is escaped before it is coloured, so no repository text can inject escapes
	esc := escapeReposLine
	line := func(s string) { b.WriteString(s + "\n") }
	head := "SourceSteward status: " + plural(len(r.Repos), "repository", "repositories")
	if r.Root != nil {
		head += " in " + esc(*r.Root)
	}
	line(head)
	if r.Note != nil {
		line("note: " + esc(*r.Note))
	}
	nameWidth, branchWidth := 1, 1
	branchOf := func(rp stwsync.RepoStatus) string {
		switch {
		case rp.Branch != nil:
			return esc(*rp.Branch)
		case rp.Detached:
			return "(detached)"
		}
		return "-"
	}
	for _, rp := range r.Repos {
		nameWidth, branchWidth = max(nameWidth, min(len(esc(rp.Name)), 40)), max(branchWidth, min(len(branchOf(rp)), 30))
	}
	for _, rp := range r.Repos {
		marker, code := "  ", "2"
		switch {
		case rp.Error != nil:
			marker, code = "! ", "31"
		case len(rp.Attention) > 0:
			marker, code = "! ", "33"
		}
		sync, changes := esc(statusSyncText(rp)), ""
		if rp.State == "ok" {
			changes = "  " + statusChangesText(rp.Changes)
		}
		line(fmt.Sprintf("%s%-*s  %-*s  %s%s", paint(marker, code), nameWidth, esc(rp.Name), branchWidth, branchOf(rp), sync, changes))
		for _, d := range statusDetails(rp) {
			line("    " + paint(esc(d), "2"))
		}
		if worktrees {
			for _, wt := range rp.Worktrees.List {
				what := "detached"
				if wt.Branch != nil {
					what = esc(*wt.Branch)
				}
				text := "    worktree " + esc(wt.Path) + "  " + what
				if wt.Dirty {
					text += "  dirty"
				}
				if wt.Locked != nil {
					text += "  locked"
					if *wt.Locked != "" {
						text += ": " + esc(*wt.Locked)
					}
				}
				if wt.Prunable {
					text += "  prunable"
				}
				line(text)
			}
		}
	}
	switch {
	case r.Summary.Attention == 0 && r.Summary.Errors == 0:
		line(paint("summary: "+plural(r.Summary.Total, "repository", "repositories")+", nothing needs attention", "32"))
	default:
		line(paint(fmt.Sprintf("summary: %d of %d repositories need attention", r.Summary.Attention, r.Summary.Total), "33"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
