// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statuscmd"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// statusLevelCode is the SGR colour of a priority; it paints the icon and the level word only.
var statusLevelCode = map[overview.Priority]string{
	overview.Red: "31", overview.Yellow: "33", overview.Green: "32", overview.Blue: "34", overview.Violet: "35",
}

// statusLineNoun names what a grouped line stands for: `2 bot PRs: #3, #4`.
var statusLineNoun = map[overview.Category]string{
	overview.RepoError: "repository errors", overview.LocalOperation: "operations in progress", overview.MainRed: "red default branches",
	overview.ReleaseRunFailed: "failed release runs", overview.ReadError: "unread sources", overview.LocalChanges: "kinds of changes",
	overview.LocalDetached: "detached checkouts", overview.LocalUntracked: "untracked findings", overview.LocalBehind: "branches behind",
	overview.LocalNoUpstream: "branches without upstream", overview.MainBehindOrigin: "default branches behind origin",
	overview.GitlinkDrift: "drifted gitlinks", overview.WorktreeDirty: "dirty worktrees", overview.PRConflict: "PRs have conflicts",
	overview.PRBlocked: "PRs are blocked", overview.TagWithoutRelease: "tags have no release", overview.PRReady: "PRs are ready to merge",
	overview.ReleaseDraft: "release drafts are waiting", overview.PRChecksPending: "PRs are waiting for checks",
	overview.PRUnread: "PRs were not read", overview.LocalStash: "stashes", overview.GitlinkAhead: "gitlinks ahead",
	overview.BranchesUnmerged: "branch findings", overview.WorktreeStale: "stale worktrees", overview.PRBot: "bot PRs",
	overview.IssueSecurity: "security issues", overview.IssueAssigned: "issues are assigned to you", overview.IssueBug: "bug issues",
}

// maxStatusRepoWidth bounds the repository column of the action lines.
const maxStatusRepoWidth = 40

// renderStatusNoToken prints what a run without a usable token prints: exactly the output of --offline, then the
// notices (Spec 037): no Needs action section, no remote lines, no second summary.
func renderStatusNoToken(w io.Writer, rep statuscmd.Report, worktrees, color bool, marks statusMarks) error {
	if err := renderStatusText(w, rep.Local, worktrees, color, marks); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("\nNotices\n")
	for _, n := range rep.Overview.Notices {
		b.WriteString("  " + n.Message + "\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// renderStatusOnline prints the online overview: what needs action, the repositories with their remote
// line, the notices and one summary line. Stripping the colour leaves exactly the text without it.
func renderStatusOnline(w io.Writer, rep statuscmd.Report, icons statuscmd.IconSet, worktrees, color bool) error {
	var b strings.Builder
	paint := statusPainter(color)
	line := func(s string) { b.WriteString(s + "\n") }
	writeStatusActions(line, rep.Overview, icons, paint)
	line("")
	line("Repositories")
	if rep.Local.Note != nil {
		line("note: " + escapeReposLine(*rep.Local.Note))
	}
	marks := statusMarks{attention: icons.Attention(), failed: icons.Failure()}
	writeStatusRepos(line, rep.Local, worktrees, paint, marks, func(i int) string { return statusRemoteLine(rep.Remote.Repos[i]) })
	if len(rep.Overview.Notices) > 0 {
		line("")
		line("Notices")
		for _, n := range rep.Overview.Notices {
			line("  " + n.Message)
		}
	}
	line("")
	line(statusSummaryLine(rep.Overview.Summary))
	_, err := io.WriteString(w, b.String())
	return err
}

func writeStatusActions(line func(string), ov overview.Overview, icons statuscmd.IconSet, paint func(s, code string) string) {
	if len(ov.Items) == 0 {
		line("Needs action: nothing")
		return
	}
	line("Needs action: " + plural(len(ov.Items), "item", "items"))
	sections := overview.Group(ov.Items)
	width := 1
	for _, s := range sections {
		for _, l := range s.Lines {
			width = max(width, min(utf8.RuneCountInString(untrusted.Esc(l.Repo)), maxStatusRepoWidth))
		}
	}
	for _, s := range sections {
		code := statusLevelCode[s.Priority]
		level := string(s.Priority)
		for _, l := range s.Lines {
			text := statusLineText(l)
			if l.URL != "" {
				text += "  " + l.URL
			}
			line(fmt.Sprintf(" %s %s%s  %-*s  %s", paint(icons.Icon(s.Priority), code), paint(level, code), strings.Repeat(" ", 6-len(level)), width, untrusted.Esc(l.Repo), text))
			switch {
			case l.Command != "":
				line("     run: " + l.Command)
			case l.Count == 1 && l.Item.Hint != "":
				line("     hint: " + l.Item.Hint)
			}
		}
		if s.MoreLines > 0 {
			line(fmt.Sprintf("     and %d more %s %s", s.MoreItems, level, pluralNoun(s.MoreItems, "item", "items")))
			if s.MoreCommand != "" {
				line("     run: " + s.MoreCommand)
			}
		}
	}
}

// pluralNoun is the noun for n without the number.
func pluralNoun(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// statusLineText is the text of an action line: the title of a single item, else the count, what the
// items are and the first identifiers (`+N more` beyond).
func statusLineText(l overview.Line) string {
	if l.Count == 1 {
		return l.Item.Title
	}
	text := fmt.Sprintf("%d %s: %s", l.Count, statusLineNoun[l.Category], strings.Join(l.Labels, ", "))
	if l.More > 0 {
		text += fmt.Sprintf(", +%d more", l.More)
	}
	return text
}

func statusSummaryLine(s overview.Summary) string {
	var what string
	if p := s.Priorities; p == (overview.Counts{}) {
		what = "nothing needs action"
	} else {
		what = fmt.Sprintf("%d red, %d yellow, %d green, %d blue, %d violet", p.Red, p.Yellow, p.Green, p.Blue, p.Violet)
	}
	return fmt.Sprintf("summary: %s | %s | %s", what, plural(s.Repos, "repository", "repositories"), plural(s.Notices, "notice", "notices"))
}
