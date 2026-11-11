// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statuscmd"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// writeStatusCounts writes the `Counts` block: one aligned line per repository, then `All`. A figure is a number
// only when it was read: `n/a` is not read, `?` is a failed read, `n+` is a lower bound. Repository names are
// escaped; every other cell is a number or a fixed word.
func writeStatusCounts(line func(string), names []string, repos []statuscmd.Counts, total statuscmd.Counts) {
	rows := make([][]string, 0, len(repos)+1)
	labels := make([]string, 0, len(repos)+1)
	for i, c := range repos {
		labels = append(labels, escapeReposLine(names[i]))
		rows = append(rows, statusCountCells(c))
	}
	labels, rows = append(labels, "All"), append(rows, statusCountCells(total))
	nameWidth := 0
	for _, l := range labels {
		nameWidth = max(nameWidth, utf8.RuneCountInString(l))
	}
	widths := make([]int, 5)
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(cell))
		}
	}
	line("Counts")
	for i, row := range rows {
		var b strings.Builder
		b.WriteString("  " + labels[i] + strings.Repeat(" ", nameWidth-utf8.RuneCountInString(labels[i])) + "  ")
		for j, cell := range row {
			if j > 0 {
				b.WriteString(" | ")
			}
			b.WriteString(cell)
			if j < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[j]-utf8.RuneCountInString(cell)))
			}
		}
		line(b.String())
	}
}

func statusCountCells(c statuscmd.Counts) []string {
	return []string{
		statusSourceCell("PRs", "bot", c.PRs), statusSourceCell("Issues", "bug", c.Issues),
		"Branches " + statusNumber(c.BranchesLocal) + "/" + statusNumber(c.BranchesRemote),
		"Worktrees " + statusNumber(c.Worktrees), "Stash " + statusNumber(c.Stash),
	}
}

func statusSourceCell(label, sub string, s statuscmd.SourceCount) string {
	switch s.State {
	case statuscmd.CountOK:
		plus := ""
		if s.Truncated {
			plus = "+"
		}
		return fmt.Sprintf("%s %d%s (%s %d%s)", label, s.Open, plus, sub, s.Sub, plus)
	case statuscmd.CountFailed:
		return label + " ?"
	}
	return label + " n/a"
}

func statusNumber(n statuscmd.Number) string {
	switch n.State {
	case statuscmd.CountOK:
		return fmt.Sprint(n.N)
	case statuscmd.CountFailed:
		return "?"
	}
	return "n/a"
}

// statusCountNames are the names of the repositories of a local status, in order.
func statusCountNames(r stwsync.StatusResult) []string {
	names := make([]string, len(r.Repos))
	for i, rp := range r.Repos {
		names[i] = rp.Name
	}
	return names
}

// writeStatusNotices writes the `Notices (N)` block: the messages of the model, or `none`.
func writeStatusNotices(line func(string), notices []overview.Notice) {
	line(fmt.Sprintf("Notices (%d)", len(notices)))
	if len(notices) == 0 {
		line("  none")
	}
	for _, n := range notices {
		line("  " + n.Message)
	}
}

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
	if err := renderStatusText(w, rep.Local, rep.Remote.Repos, worktrees, color, marks); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("\n")
	writeStatusNotices(func(s string) { b.WriteString(s + "\n") }, rep.Overview.Notices)
	if rep.VerboseRequested {
		writeStatusVerbose(func(s string) { b.WriteString(s + "\n") }, rep)
	}
	if rep.StatsRequested || rep.VerboseRequested {
		b.WriteString(statusStatsLine(rep) + "\n")
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
	repoCounts, total := rep.Counts()
	writeStatusCounts(line, statusCountNames(rep.Local), repoCounts, total)
	line("")
	writeStatusActions(line, rep.Overview, icons, paint, rep.LongRequested)
	line("")
	line(fmt.Sprintf("Repositories (%d)", len(rep.Local.Repos)))
	if rep.Local.Note != nil {
		line("note: " + escapeReposLine(*rep.Local.Note))
	}
	marks := statusMarks{attention: icons.Attention(), failed: icons.Failure()}
	writeStatusRepos(line, rep.Local, worktrees, rep.LongRequested, paint, marks, func(i int) []string {
		rr := rep.Remote.Repos[i]
		if issues := statusIssuesLine(rr); issues != "" {
			return []string{statusRemoteLine(rr), issues}
		}
		return []string{statusRemoteLine(rr)}
	})
	line("")
	writeStatusNotices(line, rep.Overview.Notices)
	if rep.Local.Rules != nil {
		line("")
		renderRules(func(s string) { line(escapeReposLine(s)) }, rep.Local.Rules)
	}
	line("")
	line(statusSummaryLine(rep.Overview.Summary))
	if rep.VerboseRequested {
		writeStatusVerbose(line, rep)
	}
	if rep.StatsRequested || rep.VerboseRequested {
		line(statusStatsLine(rep))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func renderStatusVerbose(w io.Writer, rep statuscmd.Report) {
	var b strings.Builder
	writeStatusVerbose(func(s string) { b.WriteString(s + "\n") }, rep)
	b.WriteString(statusStatsLine(rep) + "\n")
	_, _ = io.WriteString(w, b.String())
}

func writeStatusVerbose(line func(string), rep statuscmd.Report) {
	line("")
	line("Diagnostic breakdown:")
	byRepo := make(map[string][]statusremote.SourceStats)
	var repoOrder []string
	var viewerStats *statusremote.SourceStats
	for _, st := range rep.Remote.Stats.Sources {
		if st.Source == "viewer" || st.Repo == "" {
			copySt := st
			viewerStats = &copySt
			continue
		}
		if _, ok := byRepo[st.Repo]; !ok {
			repoOrder = append(repoOrder, st.Repo)
		}
		byRepo[st.Repo] = append(byRepo[st.Repo], st)
	}
	for _, repo := range repoOrder {
		line(fmt.Sprintf("  %s:", repo))
		for _, st := range byRepo[repo] {
			line(fmt.Sprintf("    %-11s %s (%s, %d consumed) in %s",
				st.Source+":",
				plural(st.Requests, "req", "reqs"),
				pluralCached(st.Cached304s),
				st.Consumed,
				shortDuration(st.Latency),
			))
		}
	}
	if viewerStats != nil {
		line(fmt.Sprintf("  viewer:     %s (%s, %d consumed) in %s",
			plural(viewerStats.Requests, "req", "reqs"),
			pluralCached(viewerStats.Cached304s),
			viewerStats.Consumed,
			shortDuration(viewerStats.Latency),
		))
	}
}

func pluralCached(n int) string {
	if n == 1 {
		return "1 cached 304"
	}
	return fmt.Sprintf("%d cached 304s", n)
}

func statusStatsLine(rep statuscmd.Report) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("stats: API: %d reqs", rep.Remote.Stats.Requests))
	rate := rep.Remote.Stats.RateLimit
	if rate.Known {
		b.WriteString(fmt.Sprintf(" (%s/%s left, reset %s)",
			groupDigits(rate.Remaining),
			groupDigits(rate.Limit),
			rate.Reset.UTC().Format("15:04 UTC"),
		))
	}
	b.WriteString(fmt.Sprintf(" | %s elapsed (local %s, remote %s)",
		shortDuration(rep.TotalDuration),
		shortDuration(rep.LocalDuration),
		shortDuration(rep.RemoteDuration),
	))
	return b.String()
}

func writeStatusActions(line func(string), ov overview.Overview, icons statuscmd.IconSet, paint func(s, code string) string, long bool) {
	line(fmt.Sprintf("Needs action (%d)", len(ov.Items)))
	if len(ov.Items) == 0 {
		line("  nothing")
		return
	}
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
			if long {
				switch {
				case l.Command != "":
					line("     run: " + l.Command)
				case l.Count == 1 && l.Item.Hint != "":
					line("     hint: " + l.Item.Hint)
				}
			}
		}
		if s.MoreLines > 0 {
			line(fmt.Sprintf("     and %d more %s %s", s.MoreItems, level, pluralNoun(s.MoreItems, "item", "items")))
			if s.MoreCommand != "" && long {
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
