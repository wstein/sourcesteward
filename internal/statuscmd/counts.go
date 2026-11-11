// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// The states of a count. A count is a number only in the state ok: what was not read is never 0.
const (
	CountOK       = "ok"        // read: the figures are numbers
	CountNotRead  = "not-read"  // not read on purpose or not at all: offline, no token, the deadline
	CountNoRemote = "no-remote" // the repository has no GitHub origin: a total leaves it out
	CountFailed   = "failed"    // the read failed (a notice says why) or the repository cannot be read locally
	// CountNotApplicable is a fact of the repository, not a gap: its issues are disabled, or it is an uninitialized
	// submodule (nothing there to count). A total leaves it out, like no-remote; the repository line shows n/a.
	CountNotApplicable = "not-applicable"
)

// SourceCount is a hosting count of one repository or of all: the open pull requests with how many are bot
// pull requests, or the open issues with how many have the label bug. Truncated says that Open is a lower
// bound (the issues listing hit its page limit).
type SourceCount struct {
	State     string
	Open, Sub int
	Truncated bool
}

// Number is a local figure; N is meaningful only in the state ok.
type Number struct {
	State string
	N     int
}

// Counts are the counters of one repository, or the totals of all (Spec 037 amendment, `Counts` block).
type Counts struct {
	PRs, Issues                                     SourceCount
	BranchesLocal, BranchesRemote, Worktrees, Stash Number
}

// CountsOf is the counters of one repository. rr is its remote half, nil when none was read (--offline,
// a failed local read). Worktrees are the linked ones whose directory exists, the main checkout excluded.
func CountsOf(rp stwsync.RepoStatus, rr *statusremote.RepoResult) Counts {
	switch rp.State {
	case "error":
		return all(CountFailed)
	case "ok":
	default: // uninitialized: nothing was there to read
		return all(CountNotApplicable)
	}
	c := Counts{
		BranchesLocal: Number{CountOK, rp.Branches.Local}, BranchesRemote: Number{State: CountFailed},
		Worktrees: Number{CountOK, max(rp.Worktrees.Linked-rp.Worktrees.Missing, 0)}, Stash: Number{CountOK, rp.Stash},
		PRs: SourceCount{State: CountNotRead}, Issues: SourceCount{State: CountNotRead},
	}
	if rp.RemoteBranches != nil {
		c.BranchesRemote = Number{CountOK, *rp.RemoteBranches}
	}
	if rr == nil || rr.Remote == nil {
		return c
	}
	if st := rr.Remote.Sources[overview.SourcePulls]; st.Outcome == overview.OutcomeOK && rr.Pulls != nil {
		open := rr.Pulls.Counts.Total
		if rr.Pulls.Open != nil {
			open = *rr.Pulls.Open
		}
		c.PRs = SourceCount{State: CountOK, Open: open, Sub: rr.Pulls.Counts.Bots}
	} else {
		c.PRs.State = unreadState(st)
	}
	if st := rr.Remote.Sources[overview.SourceIssues]; st.Outcome == overview.OutcomeOK && rr.Issues != nil {
		c.Issues = SourceCount{State: CountOK, Open: rr.Issues.Open, Sub: rr.Issues.Bugs, Truncated: rr.Issues.Truncated}
	} else {
		c.Issues.State = unreadState(st)
		if st.FactOfRepository(overview.SourceIssues) {
			c.Issues.State = CountNotApplicable
		}
	}
	return c
}

func all(state string) Counts {
	n, s := Number{State: state}, SourceCount{State: state}
	return Counts{PRs: s, Issues: s, BranchesLocal: n, BranchesRemote: n, Worktrees: n, Stash: n}
}

// unreadState is the state of a hosting count whose source did not end ok: failed for an error, no-remote for
// a repository that is not on GitHub (nothing to read), not-read for every other skip.
func unreadState(st overview.SourceState) string {
	switch {
	case st.Outcome == overview.OutcomeError:
		return CountFailed
	case st.Outcome == overview.OutcomeSkipped && (st.Reason == overview.ReasonNotGitHub || st.Reason == overview.ReasonOtherHost || st.Reason == overview.ReasonInvalidRepo):
		return CountNoRemote
	}
	return CountNotRead
}

// SumCounts are the totals. A hosting count is a number when at least one repository gave one and none failed
// or was not read (a repository without a GitHub origin, with disabled issues or uninitialized is left out); a local figure is a number only when every
// repository that applies gave one (not-applicable is left out). A failed read wins over not read.
func SumCounts(cs []Counts) Counts {
	var t Counts
	t.PRs = sumSource(len(cs), func(i int) SourceCount { return cs[i].PRs })
	t.Issues = sumSource(len(cs), func(i int) SourceCount { return cs[i].Issues })
	t.BranchesLocal = sumNumber(len(cs), func(i int) Number { return cs[i].BranchesLocal })
	t.BranchesRemote = sumNumber(len(cs), func(i int) Number { return cs[i].BranchesRemote })
	t.Worktrees = sumNumber(len(cs), func(i int) Number { return cs[i].Worktrees })
	t.Stash = sumNumber(len(cs), func(i int) Number { return cs[i].Stash })
	return t
}

func sumSource(n int, at func(int) SourceCount) SourceCount {
	var sum SourceCount
	failed, notRead, read, na := false, false, false, false
	for i := range n {
		s := at(i)
		switch s.State {
		case CountFailed:
			failed = true
		case CountNotRead:
			notRead = true
		case CountNotApplicable:
			na = true
		case CountOK:
			read = true
			sum.Open, sum.Sub, sum.Truncated = sum.Open+s.Open, sum.Sub+s.Sub, sum.Truncated || s.Truncated
		}
	}
	switch {
	case failed:
		return SourceCount{State: CountFailed}
	case notRead:
		return SourceCount{State: CountNotRead}
	case read:
		sum.State = CountOK
		return sum
	}
	if na { // only repositories for which it does not apply
		return SourceCount{State: CountNotApplicable}
	}
	return SourceCount{State: CountNoRemote}
}

func sumNumber(n int, at func(int) Number) Number {
	sum, failed, notRead := 0, false, false
	for i := range n {
		switch v := at(i); v.State {
		case CountFailed:
			failed = true
		case CountNotRead:
			notRead = true
		case CountNotApplicable: // left out of the sum
		default:
			sum += v.N
		}
	}
	switch {
	case failed:
		return Number{State: CountFailed}
	case notRead:
		return Number{State: CountNotRead}
	}
	return Number{CountOK, sum}
}

// Counts are the counters of every repository, in the order of the local status, and their totals. A
// repository without a remote half (a failed local read, a run that read nothing) has not-read hosting counts.
func (r Report) Counts() ([]Counts, Counts) {
	return CountsOfRun(r.Local, r.Remote.Repos)
}

// CountsOfRun pairs the local repositories with the remote results by position; remote may be empty or shorter.
func CountsOfRun(local stwsync.StatusResult, remote []statusremote.RepoResult) ([]Counts, Counts) {
	repos := make([]Counts, len(local.Repos))
	for i, rp := range local.Repos {
		var rr *statusremote.RepoResult
		if i < len(remote) {
			rr = &remote[i]
		}
		repos[i] = CountsOf(rp, rr)
	}
	return repos, SumCounts(repos)
}
