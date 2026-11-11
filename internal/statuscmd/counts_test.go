// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"encoding/json"
	"testing"

	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

func okRepo(branches, remote, linked, missing, stash int) stwsync.RepoStatus {
	return stwsync.RepoStatus{
		Name: "r", State: "ok", Reasons: []string{}, Stash: stash, RemoteBranches: &remote,
		Branches: stwsync.BranchCounts{Local: branches}, Worktrees: stwsync.WorktreeCounts{Linked: linked, Missing: missing},
	}
}

func readRemote(open, bots, issues, bugs int, truncated bool) *statusremote.RepoResult {
	return &statusremote.RepoResult{
		Name: "r", Slug: "o/r",
		Remote: &overview.Remote{Sources: allSources(okSource)},
		Pulls:  &statusremote.Pulls{State: okSource, Open: ptr(open), Counts: pulls.Summary{Total: open, Bots: bots}},
		Issues: &statusremote.Issues{State: okSource, Open: issues, Bugs: bugs, Truncated: truncated},
	}
}

func withSource(rr *statusremote.RepoResult, src overview.Source, st overview.SourceState) *statusremote.RepoResult {
	c := *rr
	c.Remote = &overview.Remote{Sources: map[overview.Source]overview.SourceState{}}
	for k, v := range rr.Remote.Sources {
		c.Remote.Sources[k] = v
	}
	c.Remote.Sources[src] = st
	return &c
}

func skip(reason string) overview.SourceState {
	return overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: reason}
}

func failed(code string) overview.SourceState {
	return overview.SourceState{Outcome: overview.OutcomeError, Code: code}
}

func num(n int) Number { return Number{State: CountOK, N: n} }

func TestCountsOfOneRepository(t *testing.T) {
	read := readRemote(3, 2, 3, 2, false)
	allSkipped := func(reason string) *statusremote.RepoResult {
		return withSource(withSource(read, overview.SourcePulls, skip(reason)), overview.SourceIssues, skip(reason))
	}
	cases := []struct {
		name string
		rp   stwsync.RepoStatus
		rr   *statusremote.RepoResult
		want Counts
	}{
		{"everything read", okRepo(3, 2, 1, 0, 2), read, Counts{
			PRs: SourceCount{State: CountOK, Open: 3, Sub: 2}, Issues: SourceCount{State: CountOK, Open: 3, Sub: 2},
			BranchesLocal: num(3), BranchesRemote: num(2), Worktrees: num(1), Stash: num(2)}},
		{"a missing worktree directory is no worktree", okRepo(1, 0, 3, 2, 0), read, Counts{
			PRs: SourceCount{State: CountOK, Open: 3, Sub: 2}, Issues: SourceCount{State: CountOK, Open: 3, Sub: 2},
			BranchesLocal: num(1), BranchesRemote: num(0), Worktrees: num(1), Stash: num(0)}},
		{"a full issues listing is a lower bound", okRepo(1, 0, 0, 0, 0), readRemote(0, 0, 900, 40, true), Counts{
			PRs: SourceCount{State: CountOK}, Issues: SourceCount{State: CountOK, Open: 900, Sub: 40, Truncated: true},
			BranchesLocal: num(1), BranchesRemote: num(0), Worktrees: num(0), Stash: num(0)}},
		{"a failed read is ? and keeps the other counts", okRepo(1, 1, 0, 0, 0), withSource(read, overview.SourcePulls, failed("forbidden")), Counts{
			PRs: SourceCount{State: CountFailed}, Issues: SourceCount{State: CountOK, Open: 3, Sub: 2},
			BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}},
		{"a page that is too large is a failed read", okRepo(1, 1, 0, 0, 0), withSource(read, overview.SourceIssues, failed("page-too-large")), Counts{
			PRs: SourceCount{State: CountOK, Open: 3, Sub: 2}, Issues: SourceCount{State: CountFailed},
			BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}},
		{"the deadline is not read, never 0", okRepo(1, 1, 0, 0, 0), withSource(read, overview.SourcePulls, skip(overview.ReasonDeadline)), Counts{
			PRs: SourceCount{State: CountNotRead}, Issues: SourceCount{State: CountOK, Open: 3, Sub: 2},
			BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}},
		{"no token is not read", okRepo(1, 1, 0, 0, 0), allSkipped(overview.ReasonNoToken), Counts{
			PRs: SourceCount{State: CountNotRead}, Issues: SourceCount{State: CountNotRead},
			BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}},
		{"offline has no remote half: not read", okRepo(1, 1, 0, 0, 0), nil, Counts{
			PRs: SourceCount{State: CountNotRead}, Issues: SourceCount{State: CountNotRead},
			BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}},
		{"a repository without a GitHub origin has no remote", okRepo(1, 1, 0, 0, 0), allSkipped(overview.ReasonNotGitHub), Counts{
			PRs: SourceCount{State: CountNoRemote}, Issues: SourceCount{State: CountNoRemote},
			BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}},
		{"another host has no remote", okRepo(1, 1, 0, 0, 0), allSkipped(overview.ReasonOtherHost), Counts{
			PRs: SourceCount{State: CountNoRemote}, Issues: SourceCount{State: CountNoRemote},
			BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}},
		{"disabled issues are a fact of the repository", okRepo(1, 1, 0, 0, 0), withSource(read, overview.SourceIssues, skip(overview.ReasonIssuesDisabled)), Counts{
			PRs: SourceCount{State: CountOK, Open: 3, Sub: 2}, Issues: SourceCount{State: CountNotApplicable},
			BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}},
		{"remote branches that cannot be listed are ?", func() stwsync.RepoStatus { r := okRepo(1, 1, 0, 0, 0); r.RemoteBranches = nil; return r }(), read, Counts{
			PRs: SourceCount{State: CountOK, Open: 3, Sub: 2}, Issues: SourceCount{State: CountOK, Open: 3, Sub: 2},
			BranchesLocal: num(1), BranchesRemote: Number{State: CountFailed}, Worktrees: num(0), Stash: num(0)}},
		{"a repository that cannot be read is ? everywhere", stwsync.RepoStatus{Name: "r", State: "error", Reasons: []string{}, Stash: 7}, withSource(read, overview.SourcePulls, skip(overview.ReasonNotReadLocally)), Counts{
			PRs: SourceCount{State: CountFailed}, Issues: SourceCount{State: CountFailed},
			BranchesLocal: Number{State: CountFailed}, BranchesRemote: Number{State: CountFailed}, Worktrees: Number{State: CountFailed}, Stash: Number{State: CountFailed}}},
		{"an uninitialized submodule is not applicable, never 0", stwsync.RepoStatus{Name: "r", State: "uninitialized", Reasons: []string{}}, allSkipped(overview.ReasonNotReadLocally), Counts{
			PRs: SourceCount{State: CountNotApplicable}, Issues: SourceCount{State: CountNotApplicable},
			BranchesLocal: Number{State: CountNotApplicable}, BranchesRemote: Number{State: CountNotApplicable}, Worktrees: Number{State: CountNotApplicable}, Stash: Number{State: CountNotApplicable}}},
	}
	for _, tc := range cases {
		if got := CountsOf(tc.rp, tc.rr); got != tc.want {
			t.Errorf("%s:\n got  %+v\n want %+v", tc.name, got, tc.want)
		}
	}
}

func TestTotalsAreANumberOnlyWhenEveryRepositoryGaveOne(t *testing.T) {
	a := Counts{PRs: SourceCount{State: CountOK, Open: 3, Sub: 2}, Issues: SourceCount{State: CountOK, Open: 3, Sub: 2},
		BranchesLocal: num(3), BranchesRemote: num(2), Worktrees: num(1), Stash: num(2)}
	b := Counts{PRs: SourceCount{State: CountOK, Open: 1}, Issues: SourceCount{State: CountOK, Open: 4, Sub: 1, Truncated: true},
		BranchesLocal: num(1), BranchesRemote: num(1), Worktrees: num(0), Stash: num(0)}
	none := Counts{PRs: SourceCount{State: CountNoRemote}, Issues: SourceCount{State: CountNoRemote},
		BranchesLocal: num(0), BranchesRemote: num(0), Worktrees: num(0), Stash: num(0)}
	notRead := Counts{PRs: SourceCount{State: CountNotRead}, Issues: SourceCount{State: CountNotRead},
		BranchesLocal: Number{State: CountNotRead}, BranchesRemote: num(0), Worktrees: num(0), Stash: num(0)}
	bad := Counts{PRs: SourceCount{State: CountFailed}, Issues: SourceCount{State: CountFailed},
		BranchesLocal: Number{State: CountFailed}, BranchesRemote: num(0), Worktrees: num(0), Stash: num(0)}

	if got, want := SumCounts([]Counts{a, b}), (Counts{
		PRs: SourceCount{State: CountOK, Open: 4, Sub: 2}, Issues: SourceCount{State: CountOK, Open: 7, Sub: 3, Truncated: true},
		BranchesLocal: num(4), BranchesRemote: num(3), Worktrees: num(1), Stash: num(2)}); got != want {
		t.Errorf("sum of two read repositories:\n got  %+v\n want %+v", got, want)
	}
	if got := SumCounts([]Counts{a, none}); got.PRs != (SourceCount{State: CountOK, Open: 3, Sub: 2}) {
		t.Errorf("a repository without a GitHub origin is left out of the PR total: %+v", got.PRs)
	}
	if got := SumCounts([]Counts{none, none}); got.PRs.State != CountNoRemote || got.Issues.State != CountNoRemote {
		t.Errorf("no GitHub origin anywhere is no-remote, not not-read: %+v", got)
	}
	if got := SumCounts([]Counts{a, notRead}); got.PRs.State != CountNotRead || got.BranchesLocal.State != CountNotRead || got.BranchesRemote != num(2) {
		t.Errorf("one repository that was not read makes the total not read: %+v", got)
	}
	if got := SumCounts([]Counts{a, notRead, bad}); got.PRs.State != CountFailed || got.Issues.State != CountFailed || got.BranchesLocal.State != CountFailed {
		t.Errorf("a failed read wins over not read: %+v", got)
	}
	if got := SumCounts(nil); got.PRs.State != CountNoRemote || got.Stash != num(0) {
		t.Errorf("no repository: %+v", got)
	}
}

// The report pairs the repositories by position; a report without a remote half (a failed local read, a
// local-only run) gives the counts of the local half and not-read hosting counts.
func TestReportCountsPairLocalAndRemoteByPosition(t *testing.T) {
	rep := Report{
		Local: stwsync.StatusResult{Repos: []stwsync.RepoStatus{okRepo(2, 1, 0, 0, 0), okRepo(1, 1, 0, 0, 0)}},
		Remote: statusremote.Result{Repos: []statusremote.RepoResult{
			*readRemote(5, 1, 2, 1, false), *withSource(withSource(readRemote(0, 0, 0, 0, false), overview.SourcePulls, skip(overview.ReasonNotGitHub)), overview.SourceIssues, skip(overview.ReasonNotGitHub)),
		}},
	}
	repos, all := rep.Counts()
	if len(repos) != 2 || repos[0].PRs != (SourceCount{State: CountOK, Open: 5, Sub: 1}) || repos[1].PRs.State != CountNoRemote || all.PRs != (SourceCount{State: CountOK, Open: 5, Sub: 1}) {
		t.Fatalf("%+v %+v", repos, all)
	}
	rep.Remote = statusremote.Result{}
	repos, all = rep.Counts()
	if repos[0].PRs.State != CountNotRead || all.PRs.State != CountNotRead || all.BranchesLocal != num(3) {
		t.Fatalf("%+v %+v", repos, all)
	}
}

// The schema 2 document carries `counts` per repository and `totals` next to the repositories; the keys of both
// are never omitted and the shape is that of the Counts block: `bot` and `bug` next to `open`, `truncated` for a
// lower bound, null for a number that was not read.
func TestDocumentCarriesCountsPerRepositoryAndTotals(t *testing.T) {
	m := decode(t, fixtureReport().Document())
	repos := m["repos"].([]any)
	want := []string{
		`{"branches":{"local":0,"remote":2},"issues":{"bug":2,"open":5,"state":"ok","truncated":true},"pull_requests":{"bot":0,"open":3,"state":"ok","truncated":false},"stash":1,"worktrees":0}`,
		`{"branches":{"local":0,"remote":1},"issues":{"bug":null,"open":null,"state":"not-applicable","truncated":false},"pull_requests":{"bot":0,"open":1,"state":"ok","truncated":false},"stash":0,"worktrees":0}`,
		`{"branches":{"local":0,"remote":1},"issues":{"bug":0,"open":0,"state":"ok","truncated":false},"pull_requests":{"bot":2,"open":2,"state":"ok","truncated":false},"stash":4,"worktrees":0}`,
		`{"branches":{"local":null,"remote":null},"issues":{"bug":null,"open":null,"state":"failed","truncated":false},"pull_requests":{"bot":null,"open":null,"state":"failed","truncated":false},"stash":null,"worktrees":null}`,
	}
	for i, w := range want {
		raw, _ := json.Marshal(repos[i].(map[string]any)["counts"])
		if string(raw) != w {
			t.Errorf("repo %d counts\n got  %s\n want %s", i, raw, w)
		}
	}
	raw, _ := json.Marshal(m["totals"])
	if w := `{"branches":{"local":null,"remote":null},"issues":{"bug":null,"open":null,"state":"failed","truncated":false},"pull_requests":{"bot":null,"open":null,"state":"failed","truncated":false},"stash":null,"worktrees":null}`; string(raw) != w {
		t.Errorf("totals\n got  %s\n want %s", raw, w)
	}
	requireKeys(t, "counts", repos[0].(map[string]any)["counts"].(map[string]any), "pull_requests", "issues", "branches", "worktrees", "stash")
}

// The totals of a workspace in which everything was read are sums, and the document still validates.
func TestDocumentTotalsOfAHealthyWorkspaceAreSums(t *testing.T) {
	rep := fixtureReport()
	rep.Local.Repos, rep.Remote.Repos = rep.Local.Repos[:3], rep.Remote.Repos[:3]
	rep.Local.Summary.Errors = 0
	m := decode(t, rep.Document())
	raw, _ := json.Marshal(m["totals"])
	if w := `{"branches":{"local":0,"remote":4},"issues":{"bug":2,"open":5,"state":"ok","truncated":true},"pull_requests":{"bot":2,"open":6,"state":"ok","truncated":false},"stash":5,"worktrees":0}`; string(raw) != w {
		t.Errorf("totals\n got  %s\n want %s", raw, w)
	}
	requireValid(t, "healthy workspace", rep.Document())
}

// A repository for which a count does not apply (an uninitialized submodule, disabled issues) never turns the
// total into n/a: All stays the sum of the others, and a total of only such repositories is not-applicable.
func TestTotalsLeaveOutWhatDoesNotApply(t *testing.T) {
	read := CountsOf(okRepo(3, 2, 1, 0, 2), readRemote(3, 2, 3, 2, false))
	uninit := CountsOf(stwsync.RepoStatus{Name: "u", State: "uninitialized", Reasons: []string{}}, nil)
	disabled := CountsOf(okRepo(1, 1, 0, 0, 0), withSource(readRemote(1, 0, 0, 0, false), overview.SourceIssues, skip(overview.ReasonIssuesDisabled)))
	got := SumCounts([]Counts{read, uninit, disabled})
	want := Counts{
		PRs: SourceCount{State: CountOK, Open: 4, Sub: 2}, Issues: SourceCount{State: CountOK, Open: 3, Sub: 2},
		BranchesLocal: num(4), BranchesRemote: num(3), Worktrees: num(1), Stash: num(2)}
	if got != want {
		t.Errorf("\n got  %+v\n want %+v", got, want)
	}
	if got := SumCounts([]Counts{uninit, disabled}); got.Issues.State != CountNotApplicable || got.PRs != (SourceCount{State: CountOK, Open: 1}) || got.Stash != num(0) {
		t.Errorf("only repositories that do not apply: %+v", got)
	}
}
