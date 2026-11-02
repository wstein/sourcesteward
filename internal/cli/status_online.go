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
	writeStatusActions(line, rep.Overview, icons, paint, rep.LongRequested)
	line("")
	line("Repositories")
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
	if len(rep.Overview.Notices) > 0 {
		line("")
		line("Notices")
		for _, n := range rep.Overview.Notices {
			line("  " + n.Message)
		}
	}
	renderRules(func(s string) { line(escapeReposLine(s)) }, rep.Local.Rules)
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
			if long || (s.Priority != overview.Blue && s.Priority != overview.Violet) {
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
			if s.MoreCommand != "" && (long || (s.Priority != overview.Blue && s.Priority != overview.Violet)) {
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
