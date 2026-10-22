// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wstein/sourcesteward/internal/statuscmd"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

func workspaceStatusCommand() *cobra.Command { return workspaceStatusCommandWith(statuscmd.Params{}) }

// workspaceStatusCommandWith builds the command over base: its function fields replace the production
// readers (local status, discovery, forge reader) and are nil in production.
func workspaceStatusCommandWith(base statuscmd.Params) *cobra.Command {
	var jsonOutput, offline bool
	var icons string
	var timeout time.Duration
	var options stwsync.StatusOptions
	command := &cobra.Command{
		Use:   "status [PATH] [--offline] [--fetch] [--worktrees] [--icons unicode|nerd|ascii] [--timeout DURATION] [--json]",
		Short: "Show what needs action in the workspace and the state of every repository",
		Long: "Read-only overview of the whole workspace, from any directory inside it. It starts with what needs action, ordered by\n" +
			"priority (red, yellow, green, blue, violet), each item with its link and a command you can run yourself; then one block\n" +
			"per repository, the notices about anything that could not be read, and a summary line. stw never runs a suggested command.\n" +
			"Per repository the local half is what git status, git worktree list and git branch -vv tell for the top-level superproject\n" +
			"and each initialized submodule (nested ones too), main worktree only: branch or detached HEAD, upstream with ahead/behind,\n" +
			"staged/unstaged/untracked counts, conflicts, a rebase/merge/cherry-pick/revert/bisect in progress, stashes, local branches\n" +
			"not yet merged into the default branch of the upstream remote, linked worktrees (dirty, locked, prunable) and, for a\n" +
			"submodule, the drift between the commit the superproject records and the one checked out.\n" +
			"The remote half reads, with GET requests only and the gh token of the other hosting commands, the open pull requests with\n" +
			"their stw pulls verdict, the release drafts, the tags without a release, and the CI of the default branch and of the\n" +
			"release workflow runs, for every repository whose origin is a GitHub repository on github.com. A missing token or a source\n" +
			"that failed never hides the rest: the notices name what is missing. --timeout (default 90s, 1s to 10m) is the overall\n" +
			"deadline of the remote half; one request takes at most 15 s.\n" +
			"--offline sends no request and reads no credential: it prints the local half alone, exactly as before the online default.\n" +
			"Nothing is changed and nothing is fetched unless --fetch asks for it (one git fetch per repository, remote-tracking\n" +
			"refs only). --worktrees lists the linked worktrees. Colour only on a terminal (NO_COLOR turns it off). --icons picks the\n" +
			"glyphs of the text: unicode (default on a terminal with a UTF-8 locale), nerd (Nerd Font) or ascii (default otherwise).\n" +
			"A progress line goes to stderr, only when stderr is a terminal. --json prints one versioned document: schema 2 online,\n" +
			"the frozen schema 1 with --offline.\n" +
			"Exit statuses: 0 also when there is something to do, when a token is missing or when the deadline cut a read; 1 when a\n" +
			"request that was made failed, a repository cannot be read or the run fails; 2 usage error.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true, SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Path = workspacePath(args)
			if err := stwsync.ValidateOptions(stwsync.Options{Path: options.Path}); err != nil {
				return err
			}
			flags := cmd.Flags()
			switch {
			case offline && flags.Changed("timeout"):
				return errors.New("--timeout cannot be used with --offline: nothing is read from the forge")
			case timeout < statuscmd.MinTimeout || timeout > statuscmd.MaxTimeout:
				return fmt.Errorf("--timeout %s is outside %s to %s", timeout, statuscmd.MinTimeout, statuscmd.MaxTimeout)
			}
			term := statuscmd.DetectTerminal(cmd.OutOrStdout(), cmd.ErrOrStderr())
			set, err := statuscmd.ResolveIcons(icons, term)
			if err != nil {
				return err
			}
			// the local layout, which is also what a run without a usable token prints, keeps the plain marks
			// unless --icons was given
			marks := asciiStatusMarks
			if flags.Changed("icons") {
				marks = statusMarks{attention: set.Attention(), failed: set.Failure()}
			}
			if offline {
				return runStatusOffline(cmd, base, options, jsonOutput, marks)
			}
			base.Status, base.Timeout = options, timeout
			if term.Stderr {
				base.Progress = cmd.ErrOrStderr()
			}
			return runStatusOnline(cmd, base, set, marks, term.Color, jsonOutput)
		},
	}
	flags := command.Flags()
	flags.BoolVar(&offline, "offline", false, "Local only: send no request, read no credential and print the output of the local status")
	flags.BoolVar(&options.Fetch, "fetch", false, "Fetch every repository first (remote-tracking refs only; the one thing that is not read-only)")
	flags.BoolVar(&options.Worktrees, "worktrees", false, "List the linked worktrees of each repository")
	flags.StringVar(&icons, "icons", "", "Glyph set of the text: unicode, nerd or ascii (default: unicode on a UTF-8 terminal, else ascii)")
	flags.DurationVar(&timeout, "timeout", statusremote.DefaultTimeout, "Overall deadline of the remote half, 1s to 10m (not with --offline)")
	flags.BoolVar(&jsonOutput, "json", false, "Output one versioned JSON object")
	return command
}

// runStatusOffline prints the local status alone, byte for byte as the command did before the online
// default: the text, or the schema 1 document.
func runStatusOffline(cmd *cobra.Command, base statuscmd.Params, options stwsync.StatusOptions, jsonOutput bool, marks statusMarks) error {
	local := base.Local
	if local == nil {
		local = stwsync.Status
	}
	result := local(cmd.Context(), options)
	if result.Error != nil && !jsonOutput {
		return runtimeFailure{fmt.Errorf("status: %s: %s", result.Error.Code, result.Error.Message)}
	}
	var err error
	if jsonOutput {
		err = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	} else {
		err = renderStatusText(cmd.OutOrStdout(), result, options.Worktrees, stwsync.ColorEnabled(cmd.OutOrStdout()), marks)
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
}

// runStatusOnline reads the local and the remote half and prints the overview or the schema 2 document.
// The output is printed even when the run failed: the failure is the exit status.
func runStatusOnline(cmd *cobra.Command, p statuscmd.Params, icons statuscmd.IconSet, localMarks statusMarks, color, jsonOutput bool) error {
	rep, err := statuscmd.Run(cmd.Context(), p)
	if err != nil {
		return runtimeFailure{err}
	}
	if rep.Local.Error != nil && !jsonOutput {
		return runtimeFailure{rep.Failure()}
	}
	switch {
	case jsonOutput:
		err = json.NewEncoder(cmd.OutOrStdout()).Encode(rep.Document())
	case rep.NoToken():
		err = renderStatusNoToken(cmd.OutOrStdout(), rep, p.Status.Worktrees, color, localMarks)
	default:
		err = renderStatusOnline(cmd.OutOrStdout(), rep, icons, p.Status.Worktrees, color)
	}
	if err != nil {
		return runtimeFailure{err}
	}
	if err = cmd.Context().Err(); err != nil {
		return runtimeFailure{err}
	}
	if err = rep.Failure(); err != nil {
		return runtimeFailure{err}
	}
	return nil
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
		}{{w.Dirty, "dirty"}, {w.Locked, "locked"}, {w.Prunable, "prunable"}, {w.Missing, "missing"}} {
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
		text := "gitlink: recorded " + reposAbbrevOID(g.Recorded) + ", checked out "
		if g.Relation != nil && *g.Relation != "unknown" {
			text += *g.Relation + " (" + reposAbbrevOID(g.CheckedOut) + ")"
		} else {
			text += reposAbbrevOID(g.CheckedOut)
		}
		lines = append(lines, text)
	}
	if rp.FetchError != nil {
		lines = append(lines, "fetch failed: "+*rp.FetchError)
	}
	return lines
}

// statusMarks are the two markers of the repository lines: the repository needs attention, or cannot be read.
// Both are two columns wide, like the blank marker of a clean repository.
type statusMarks struct{ attention, failed string }

// asciiStatusMarks are the markers of the Spec 033 text.
var asciiStatusMarks = statusMarks{attention: "! ", failed: "! "}

// statusPainter returns the function that colours a piece of text with an SGR code, or leaves it as it is.
func statusPainter(color bool) func(s, code string) string {
	return func(s, code string) string {
		if !color || code == "" {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
}

func renderStatusText(w io.Writer, r stwsync.StatusResult, worktrees, color bool, marks statusMarks) error {
	var b strings.Builder
	paint := statusPainter(color)
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
	writeStatusRepos(line, r, worktrees, paint, marks, nil)
	switch {
	case r.Summary.Attention == 0 && r.Summary.Errors == 0:
		line(paint("summary: "+plural(r.Summary.Total, "repository", "repositories")+", nothing needs attention", "32"))
	default:
		line(paint(fmt.Sprintf("summary: %d of %d repositories need attention", r.Summary.Attention, r.Summary.Total), "33"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// writeStatusRepos writes the block of every repository: one line, the extra facts and, with worktrees,
// the linked worktrees. remote, when set, gives the line that goes right under the line of repository i.
func writeStatusRepos(line func(string), r stwsync.StatusResult, worktrees bool, paint func(s, code string) string, marks statusMarks, remote func(i int) string) {
	esc := escapeReposLine
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
	for i, rp := range r.Repos {
		marker, code := "  ", "2"
		switch {
		case rp.Error != nil:
			marker, code = marks.failed, "31"
		case rp.Attention:
			marker, code = marks.attention, "33"
		}
		sync, changes := esc(statusSyncText(rp)), ""
		if rp.State == "ok" {
			changes = "  " + statusChangesText(rp.Changes)
		}
		line(fmt.Sprintf("%s%-*s  %-*s  %s%s", paint(marker, code), nameWidth, esc(rp.Name), branchWidth, branchOf(rp), sync, changes))
		if remote != nil {
			line("    " + remote(i))
		}
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
				if wt.Missing {
					text += "  missing"
				}
				line(text)
			}
		}
	}
}
