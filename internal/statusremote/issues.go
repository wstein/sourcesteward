// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/overview"
)

// Issues is what the issues source read for one repository. State is the outcome of the source. Open is
// the number of open issues read (pull request entries of the list are dropped) and Counts their number per
// priority, exactly those of `stw issues`; Truncated says that the repository has more than ten pages of open
// items and the oldest 1000 were read; Dropped is the number of pull request entries of the list.
type Issues struct {
	State     overview.SourceState
	Open      int
	Counts    issues.Counts
	Dropped   int
	Truncated bool
	Rows      []overview.Issue
}

// IssuesRead is the outcome of the issues source of a run: the viewer, which is the one `user` read, and one
// Issues per target keyed by Target.Name.
type IssuesRead struct {
	Viewer overview.SourceState
	Repos  map[string]Issues
}

// ReadIssues reads the open issues of every target that was not skipped with one issues cycle and default
// options, the cycle of `stw issues` without filters and with the largest row cap: the rule table, the
// page limit and the single `user` read of the run are those of the command, and no request is made when
// every target is skipped. The viewer carries the outcome of that read (the zero value when none was made).
//
// A failed read is the error of that repository's source, and so is the rate limit; a read that fails while
// the context expires is the skip `deadline`, never an error (the deadline wins, as in the pulls source). A
// repository whose issues are disabled is the skip `issues-disabled`. Only the rows that make an action item
// are kept, in the order of the cycle.
func ReadIssues(ctx context.Context, r issues.Reader, targets []Target) IssuesRead {
	out := IssuesRead{Repos: make(map[string]Issues, len(targets))}
	var slugs []string
	for _, t := range targets {
		if t.Skip != "" {
			out.Repos[t.Name] = Issues{State: overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: t.Skip}}
			continue
		}
		slugs = append(slugs, t.Slug)
	}
	if len(slugs) == 0 {
		return out
	}
	// Run returns no result only for a limit outside 1..MaxLimit; this one is inside.
	res, _ := issues.Run(ctx, r, issues.Options{Repos: slugs, Limit: issues.MaxLimit})
	out.Viewer = viewerState(ctx, res.Viewer)
	status := make(map[string]issues.RepoStatus, len(res.Repos))
	for _, st := range res.Repos {
		status[st.Repo] = st
	}
	rows := map[string][]overview.Issue{}
	for _, row := range res.Rows {
		if row.Priority == issues.Red || row.Priority == issues.Yellow {
			rows[row.Repo] = append(rows[row.Repo], overview.Issue{
				Number: row.Number, Title: row.Title, URL: row.URL, Author: row.Author, Reason: row.Reason,
				Labels: row.Labels, Assignees: row.Assignees,
			})
		}
	}
	for _, t := range targets {
		if t.Skip == "" {
			out.Repos[t.Name] = fromStatus(ctx, status[t.Slug], rows[t.Slug])
		}
	}
	return out
}

// viewerState maps the outcome of the `user` read. A read that failed while the context expired was cut
// short by the deadline.
func viewerState(ctx context.Context, v issues.Viewer) overview.SourceState {
	switch v.State {
	case issues.ViewerOK:
		return overview.SourceState{Outcome: overview.OutcomeOK}
	case issues.ViewerSkipped:
		return overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: v.Reason}
	}
	if cutShort(ctx, v.Error) {
		return overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
	}
	return overview.SourceState{Outcome: overview.OutcomeError, Code: v.Error}
}

// cutShort reports that a failed read ended because the context did: the code a canceled request gets is
// `network`, and any other code is an answer of the forge.
func cutShort(ctx context.Context, code string) bool {
	return ctx.Err() != nil && code == hosting.CodeNetwork
}

// fromStatus maps one repository of the cycle's result. A truncated repository (state partial) keeps what
// was read and says so in Truncated: it is no failure of the source.
func fromStatus(ctx context.Context, st issues.RepoStatus, rows []overview.Issue) Issues {
	i := Issues{State: overview.SourceState{Outcome: overview.OutcomeOK}}
	switch {
	case st.State == issues.StateSkipped:
		i.State = overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: st.Reason}
		return i
	case st.State == issues.StateError && cutShort(ctx, st.Error):
		i.State = overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
		return i
	case st.State == issues.StateError:
		i.State = overview.SourceState{Outcome: overview.OutcomeError, Code: st.Error}
		return i
	}
	i.Open, i.Counts, i.Truncated, i.Rows = *st.Open, st.Counts, st.State == issues.StatePartial, rows
	if st.PullsDropped != nil {
		i.Dropped = *st.PullsDropped
	}
	return i
}
