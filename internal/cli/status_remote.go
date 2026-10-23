// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"strings"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statuscmd"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// maxStatusDrafts is the number of release drafts the remote line names before `and N more`.
const maxStatusDrafts = 3

// statusRemoteLine is the `remote:` line of a repository: its pull requests, the CI of the default
// branch and the release drafts, or why they were not read.
func statusRemoteLine(rr statusremote.RepoResult) string {
	src := rr.Remote.Sources
	allMissing, first := true, ""
	for _, s := range overview.Sources {
		if s == overview.SourceIssues { // it has the line of its own
			continue
		}
		if src[s].Outcome == overview.OutcomeOK {
			allMissing = false
		} else if first == "" {
			first = statusWhy(src[s])
		}
	}
	if allMissing {
		return "remote: not read (" + first + ")"
	}
	var parts []string
	missing := func(s overview.Source, what string) bool {
		if src[s].Outcome == overview.OutcomeOK {
			return false
		}
		parts = append(parts, what+" not read ("+statusWhy(src[s])+")")
		return true
	}
	if !missing(overview.SourcePulls, "pull requests") {
		parts = append(parts, statusPullsText(statuscmd.PullsData(rr)))
	}
	if !missing(overview.SourceCIMain, "main CI") {
		parts = append(parts, "main CI "+untrusted.Esc(rr.Remote.Main.Class))
	}
	if !missing(overview.SourceReleases, "releases") {
		parts = append(parts, statusDraftsText(rr.Remote))
	}
	missing(overview.SourceTags, "tags")
	missing(overview.SourceCIRelease, "release runs")
	if i := rr.Issues; src[overview.SourceIssues].Outcome == overview.OutcomeOK && i.Counts.Blue+i.Counts.Violet > 0 && untrusted.ValidRepo(rr.Slug) {
		parts = append(parts, "other issues: stw issues --repo "+rr.Slug) // blue and violet issues are counted, never items
	}
	return "remote: " + strings.Join(parts, ", ")
}

// statusIssuesLine is the `issues:` line of a repository: the open issues with the counts of the two
// priorities that need action, or why they were not read. It is empty for a repository whose issues
// were never in question (it is not on GitHub or could not be read locally): the remote line says so.
func statusIssuesLine(rr statusremote.RepoResult) string {
	st := rr.Remote.Sources[overview.SourceIssues]
	switch st.Outcome {
	case overview.OutcomeOK:
		i, plus := rr.Issues, ""
		if i.Truncated {
			plus = "+"
		}
		text := fmt.Sprintf("issues: %d%s open (%d red, %d yellow", i.Open, plus, i.Counts.Red, i.Counts.Yellow)
		if i.Truncated {
			text += ", truncated"
		}
		return text + ")"
	case overview.OutcomeSkipped:
		switch st.Reason {
		case overview.ReasonIssuesDisabled:
			return "issues: disabled"
		case overview.ReasonNotGitHub, overview.ReasonOtherHost, overview.ReasonNotReadLocally, overview.ReasonInvalidRepo:
			return ""
		}
	case overview.OutcomeError:
	default:
		return ""
	}
	return "issues: not read (" + statusWhy(st) + ")"
}

// statusWhy is the skip reason or error code of a source that was not read.
func statusWhy(st overview.SourceState) string {
	if st.Outcome == overview.OutcomeError {
		return untrusted.Esc(st.Code)
	}
	return untrusted.Esc(st.Reason)
}

// statusPullsText names the non-zero groups of the open pull requests.
func statusPullsText(d statuscmd.DocPullsData) string {
	if d.Open == 0 {
		return "no open PRs"
	}
	var groups []string
	for _, g := range []struct {
		n    int
		text string
	}{{d.Ready, "ready"}, {d.Checks, "checks pending"}, {d.Red, "red"}, {d.Conflict, "conflict"}, {d.Draft, "draft"}, {d.Blocked - d.Red, "other blocked"}, {d.NotRead, "not read"}} {
		if g.n > 0 {
			groups = append(groups, fmt.Sprintf("%d %s", g.n, g.text))
		}
	}
	text := plural(d.Open, "open PR", "open PRs")
	if len(groups) > 0 {
		text += " (" + strings.Join(groups, ", ") + ")"
	}
	return text
}

// statusDraftsText says what is known about the release drafts: never zero when the token cannot see them.
func statusDraftsText(r *overview.Remote) string {
	switch r.Visibility {
	case overview.NoWriteAccess:
		return "release drafts not visible (no write access)"
	case overview.WriteAccess:
	default:
		return "release drafts unknown (write access not determined)"
	}
	if len(r.Drafts) == 0 {
		return "no release drafts"
	}
	var names []string
	for _, d := range r.Drafts[:min(len(r.Drafts), maxStatusDrafts)] {
		name := d.Name
		if name == "" {
			name = d.Tag
		}
		when := ""
		if !d.CreatedAt.IsZero() {
			when = d.CreatedAt.UTC().Format("2006-01-02") + ", "
		}
		names = append(names, fmt.Sprintf("%s (%s%s)", untrusted.Esc(name), when, plural(d.Assets, "asset", "assets")))
	}
	text := plural(len(r.Drafts), "release draft", "release drafts") + ": " + strings.Join(names, ", ")
	if more := len(r.Drafts) - len(names); more > 0 {
		text += fmt.Sprintf(", and %d more", more)
	}
	return text
}
