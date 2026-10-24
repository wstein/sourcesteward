// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/statuscmd"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

func statusPtrTo[T any](v T) *T { return &v }

var (
	onlineOK  = overview.SourceState{Outcome: overview.OutcomeOK}
	onlineSHA = strings.Repeat("3f2a9c1", 6)[:40]
)

func onlineSources(st overview.SourceState) map[overview.Source]overview.SourceState {
	m := map[overview.Source]overview.SourceState{}
	for _, s := range overview.Sources {
		m[s] = st
	}
	return m
}

// fixtureReport is a workspace of four repositories: sourcesteward (everything read, red main, a draft,
// a failed release run), crewbook (drafts not visible, a PR with conflicts), workharbor (two bot PRs,
// the CI of main cut by the deadline) and one that cannot be read locally.
func onlineReport() statuscmd.Report {
	local := stwsync.StatusResult{
		SchemaVersion: 1, Command: "status", Root: statusPtrTo("/ws"), Options: stwsync.StatusEcho{Worktrees: true},
		Repos: []stwsync.RepoStatus{
			{Name: ".", Path: "/ws", Kind: "root", State: "ok", Branch: statusPtrTo("main"), Upstream: statusPtrTo("origin/main"), Ahead: statusPtrTo(0), Behind: statusPtrTo(2),
				Changes: stwsync.Changes{Unstaged: 2}, Stash: 1, Attention: true, Reasons: []string{"unstaged", "behind"}},
			{Name: "crewbook", Path: "/ws/crewbook", Kind: "submodule", State: "ok", Branch: statusPtrTo("main"), Upstream: statusPtrTo("origin/main"), Ahead: statusPtrTo(0), Behind: statusPtrTo(0), Reasons: []string{}},
			{Name: "workharbor", Path: "/ws/workharbor", Kind: "submodule", State: "ok", Branch: statusPtrTo("main"), Upstream: statusPtrTo("origin/main"), Ahead: statusPtrTo(0), Behind: statusPtrTo(0), Reasons: []string{}},
			{Name: "gone", Path: "/ws/gone", Kind: "submodule", State: "error", Reasons: []string{}, Error: &stwsync.Failure{Code: "status-read-failed", Message: "cannot read the repository status"}},
		},
		Summary: stwsync.StatusSummary{Total: 4, Attention: 1, Errors: 1},
	}
	failedRun := overview.Run{Workflow: "release.yml", Conclusion: "failure", Status: "completed", URL: "https://github.com/wstein/sourcesteward/actions/runs/7"}
	greenRun := overview.Run{Workflow: "release.yml", Conclusion: "success", Status: "completed", URL: "https://github.com/wstein/sourcesteward/actions/runs/5"}
	behind := true
	steward := statusremote.RepoResult{
		Name: ".", Slug: "wstein/sourcesteward",
		Remote: &overview.Remote{
			Sources: onlineSources(onlineOK), DefaultBranch: "main", HTMLURL: "https://github.com/wstein/sourcesteward", Visibility: overview.WriteAccess,
			Pulls: []overview.PR{
				{Number: 57, Title: "build(deps): bump cobra", URL: "https://github.com/wstein/sourcesteward/pull/57", Author: "alice", Group: overview.GroupReady},
				{Number: 58, Title: "draft work", URL: "https://github.com/wstein/sourcesteward/pull/58", Author: "alice", Draft: true, Group: overview.GroupDraft},
				{Number: 59, Title: "Fix the tests", URL: "https://github.com/wstein/sourcesteward/pull/59", Author: "bob", Group: overview.GroupBlocked, Reason: "checks-red"},
			},
			Drafts: []overview.Draft{{Name: "Release one", Tag: "v1.0.0", URL: "https://github.com/wstein/sourcesteward/releases/tag/untagged-1", Assets: 4, CreatedAt: time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)}},
			Tags: []overview.ReleaseTag{
				{Pattern: "v*.*.*", Tag: "v1.1.0", SHA: strings.Repeat("b", 40), Latest: true, Unreleased: true},
				{Pattern: "v*.*.*", Tag: "v1.0.0", SHA: strings.Repeat("a", 40), Latest: false},
			},
			Main: &overview.Main{Branch: "main", Head: onlineSHA, Class: "red", BehindOrigin: &behind},
			Issues: []overview.Issue{
				{Number: 12, Title: "leak", URL: "https://github.com/wstein/sourcesteward/issues/12", Author: "alice", Reason: overview.IssueReasonSecurity, Labels: []string{"security"}},
				{Number: 13, Title: "review me", URL: "https://github.com/wstein/sourcesteward/issues/13", Author: "bob", Reason: overview.IssueReasonAssigned, Assignees: []string{"wstein"}},
			},
		},
		Issues: &statusremote.Issues{State: onlineOK, Open: 5, Counts: issues.Counts{Red: 1, Yellow: 2, Blue: 1, Violet: 1}},
		Pulls: &statusremote.Pulls{State: onlineOK, Open: statusPtrTo(3), Counts: pulls.Summary{Total: 3, Ready: 1, Draft: 1, Blocked: 1},
			PRs: []overview.PR{
				{Number: 57, Group: overview.GroupReady}, {Number: 58, Draft: true, Group: overview.GroupDraft}, {Number: 59, Group: overview.GroupBlocked, Reason: "checks-red"},
			}},
		CIRelease: &statusremote.CIRelease{Tags: []statusremote.CIReleaseTag{
			{Pattern: "v*.*.*", Product: "sourcesteward", Tag: "v1.1.0", SHA: strings.Repeat("b", 40), Latest: true, Unreleased: true, Workflow: "release.yml", Runs: []overview.Run{failedRun}},
			{Pattern: "v*.*.*", Product: "sourcesteward", Tag: "v1.0.0", SHA: strings.Repeat("a", 40), Workflow: "release.yml", Runs: []overview.Run{greenRun}},
		}},
	}
	steward.Remote.Tags = append(steward.Remote.Tags, steward.CIRelease.ReleaseTags()...)
	disabled := onlineSources(onlineOK)
	disabled[overview.SourceIssues] = overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonIssuesDisabled}
	crewbook := statusremote.RepoResult{
		Name: "crewbook", Slug: "wstein/crewbook",
		Remote: &overview.Remote{
			Sources: disabled, DefaultBranch: "main", HTMLURL: "https://github.com/wstein/crewbook", Visibility: overview.NoWriteAccess,
			Pulls: []overview.PR{{Number: 12, Title: "Update docs", URL: "https://github.com/wstein/crewbook/pull/12", Author: "carol", Group: overview.GroupConflict}},
			Main:  &overview.Main{Branch: "main", Head: strings.Repeat("c", 40), Class: "green"},
		},
		Pulls:     &statusremote.Pulls{State: onlineOK, Open: statusPtrTo(1), Counts: pulls.Summary{Total: 1, Conflict: 1}, PRs: []overview.PR{{Number: 12, Group: overview.GroupConflict}}},
		CIRelease: &statusremote.CIRelease{},
	}
	deadline := overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
	harborSources := onlineSources(onlineOK)
	harborSources[overview.SourceCIMain], harborSources[overview.SourceCIRelease] = deadline, deadline
	harbor := statusremote.RepoResult{
		Name: "workharbor", Slug: "wstein/workharbor",
		Remote: &overview.Remote{
			Sources: harborSources, DefaultBranch: "main", HTMLURL: "https://github.com/wstein/workharbor", Visibility: overview.WriteAccess,
			Pulls: []overview.PR{
				{Number: 3, Title: "bump a", URL: "https://github.com/wstein/workharbor/pull/3", Author: "dependabot[bot]", Bot: true, Group: overview.GroupReady},
				{Number: 4, Title: "bump b", URL: "https://github.com/wstein/workharbor/pull/4", Author: "dependabot[bot]", Bot: true, Group: overview.GroupChecks},
			},
		},
		Pulls:  &statusremote.Pulls{State: onlineOK, Open: statusPtrTo(2), Counts: pulls.Summary{Total: 2, Ready: 1, Checks: 1, Bots: 2}, PRs: []overview.PR{{Number: 3, Bot: true, Group: overview.GroupReady}, {Number: 4, Bot: true, Group: overview.GroupChecks}}},
		Issues: &statusremote.Issues{State: onlineOK},
	}
	gone := statusremote.RepoResult{Name: "gone", Remote: &overview.Remote{Sources: onlineSources(overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotReadLocally})}}
	res := statusremote.Result{Repos: []statusremote.RepoResult{steward, crewbook, harbor, gone}, Viewer: onlineOK, Timeout: 90 * time.Second}
	return statuscmd.Report{Time: time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC), Local: local, Remote: res, Overview: overview.Build(res.Input("/ws", local.Repos))}
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func renderOnline(t *testing.T, rep statuscmd.Report, icons statuscmd.IconSet, worktrees, color bool) string {
	t.Helper()
	var b bytes.Buffer
	if err := renderStatusOnline(&b, rep, icons, worktrees, color); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestOnlineTextGoldens(t *testing.T) {
	for _, set := range []statuscmd.IconSet{statuscmd.Unicode, statuscmd.Nerd, statuscmd.ASCII} {
		plain := renderOnline(t, onlineReport(), set, true, false)
		coloured := renderOnline(t, onlineReport(), set, true, true)
		statusGolden(t, "status-online-"+string(set)+".txt", plain)
		statusGolden(t, "status-online-"+string(set)+"-colour.txt", coloured)
		if strings.Contains(plain, "\x1b") || ansi.ReplaceAllString(coloured, "") != plain {
			t.Errorf("%s: stripping the colour must leave the same text", set)
		}
	}
}

func TestOnlineTextSections(t *testing.T) {
	out := renderOnline(t, onlineReport(), statuscmd.Unicode, false, false)
	order := []string{"Needs action: 15 items", "\nRepositories\n", "\nNotices\n", "\nsummary: 4 red, 6 yellow, 2 green, 1 blue, 2 violet | 4 repositories | 3 notices\n"}
	last := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i <= last {
			t.Fatalf("%q is missing or out of order in\n%s", want, out)
		}
		last = i
	}
	for _, want := range []string{
		"2 bot PRs: #3, #4\n     run: stw pulls --repo wstein/workharbor --bots-only",
		"remote: 1 open PR (1 conflict), main CI green, release drafts not visible (no write access)",
		"remote: not read (not-read-locally)",
		"issues: 5 open (1 red, 2 yellow)\n",
		"issues: disabled\n",
		"issues: 0 open (0 red, 0 yellow)\n",
		"1 stash\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "worktree") {
		t.Error("no worktree list without --worktrees")
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "http") && strings.Contains(l, "bot PRs") {
			t.Errorf("a grouped line carries no URL: %s", l)
		}
	}
}

func TestOnlineTextWithNothingToDo(t *testing.T) {
	rep := onlineReport()
	rep.Local.Repos = rep.Local.Repos[1:2]
	rep.Remote.Repos = rep.Remote.Repos[1:2]
	rep.Remote.Repos[0].Remote.Pulls = nil
	rep.Remote.Repos[0].Pulls = &statusremote.Pulls{State: onlineOK, Open: statusPtrTo(0)}
	rep.Local.Note = statusPtrTo("started in a\nlinked worktree")
	rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
	out := renderOnline(t, rep, statuscmd.ASCII, false, false)
	for _, want := range []string{"Needs action: nothing\n", "note: started in a\\nlinked worktree\n", "remote: no open PRs, main CI green, release drafts not visible", "summary: nothing needs action | 1 repository | 1 notice\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestOnlineTextCapsALongSectionAndKeepsTheCount(t *testing.T) {
	for repos, more := range map[int]string{11: "     and 1 more yellow item\n", 12: "     and 2 more yellow items\n"} {
		rep := onlineReport()
		rep.Local.Repos = nil
		rep.Remote.Repos = nil
		for i := 0; i < repos; i++ {
			name := "repo" + string(rune('a'+i))
			slug := "acme/" + name
			rep.Local.Repos = append(rep.Local.Repos, stwsync.RepoStatus{Name: name, Path: name, State: "ok", Reasons: []string{}})
			rep.Remote.Repos = append(rep.Remote.Repos, statusremote.RepoResult{Name: name, Slug: slug, Remote: &overview.Remote{
				Sources: onlineSources(onlineOK), Visibility: overview.WriteAccess, Main: &overview.Main{Branch: "main", Head: strings.Repeat("e", 40), Class: "green"},
				Pulls: []overview.PR{{Number: i + 1, Title: "t", URL: "https://github.com/" + slug + "/pull/1", Author: "a", Group: overview.GroupConflict}},
			}, Pulls: &statusremote.Pulls{State: onlineOK, Open: statusPtrTo(1), Counts: pulls.Summary{Total: 1, Conflict: 1}}, Issues: &statusremote.Issues{State: onlineOK}})
		}
		rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
		out := renderOnline(t, rep, statuscmd.ASCII, false, false)
		if got := strings.Count(out, "PR #"); got != 10 {
			t.Errorf("%d lines of the section, want the cap of 10\n%s", got, out)
		}
		if !strings.Contains(out, more+"     run: stw pulls --repo acme/repok\n") || !strings.Contains(out, fmt.Sprintf("Needs action: %d items", repos)) {
			t.Errorf("the omitted lines are counted and drilled down\n%s", out)
		}
	}
}

func TestOnlineTextHintOfAnItemWithoutACommand(t *testing.T) {
	rep := onlineReport()
	rep.Remote.Repos[0].Remote.Tags = []overview.ReleaseTag{{Pattern: "v*.*.*", Tag: "v9.9.9", SHA: strings.Repeat("d", 40), Unreleased: true}}
	rep.Remote.Repos[0].CIRelease = nil
	rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
	out := renderOnline(t, rep, statuscmd.ASCII, false, false)
	if !strings.Contains(out, "tag v9.9.9 has no release\n     hint: see the repository's docs/releasing.md") {
		t.Errorf("%s", out)
	}
}

func TestEveryCategoryHasANounForGroupedLines(t *testing.T) {
	for _, c := range overview.Categories() {
		if statusLineNoun[c] == "" {
			t.Errorf("no noun for %s", c)
		}
	}
	if len(statusLineNoun) != len(overview.Categories()) {
		t.Error("a noun for a category that does not exist")
	}
	for _, p := range overview.Priorities {
		if statusLevelCode[p] == "" {
			t.Errorf("no colour for %s", p)
		}
	}
}

func TestRemoteLineTexts(t *testing.T) {
	failed := overview.SourceState{Outcome: overview.OutcomeError, Code: "network"}
	rr := onlineReport().Remote.Repos[0]
	rr.Remote.Drafts = []overview.Draft{{Tag: "v1", Assets: 1}, {Name: "two", Tag: "v2"}, {Name: "three", Tag: "v3"}, {Name: "four", Tag: "v4"}, {Name: "five", Tag: "v5"}}
	if got := statusRemoteLine(rr); !strings.Contains(got, "5 release drafts: v1 (1 asset), two (0 assets), three (0 assets), and 2 more") {
		t.Errorf("%s", got)
	}
	rr.Remote.Visibility = overview.Unknown
	if got := statusRemoteLine(rr); !strings.Contains(got, "release drafts unknown (write access not determined)") {
		t.Errorf("%s", got)
	}
	rr.Remote.Visibility, rr.Remote.Drafts = overview.WriteAccess, nil
	rr.Remote.Sources = onlineSources(onlineOK)
	rr.Remote.Sources[overview.SourcePulls], rr.Remote.Sources[overview.SourceTags], rr.Remote.Sources[overview.SourceReleases] = failed, failed, failed
	got := statusRemoteLine(rr)
	for _, want := range []string{"pull requests not read (network)", "releases not read (network)", "tags not read (network)", "main CI red"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
	rr.Remote.Sources = onlineSources(failed)
	if got := statusRemoteLine(rr); got != "remote: not read (network)" {
		t.Errorf("%s", got)
	}
}

func TestRemoteLineSaysAMissingReleaseWorkflowAsAFact(t *testing.T) {
	rr := onlineReport().Remote.Repos[0]
	rr.Remote.Sources = onlineSources(onlineOK)
	rr.Remote.Sources[overview.SourceCIRelease] = overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNotFound}
	got := statusRemoteLine(rr)
	if !strings.Contains(got, "no release workflow") || strings.Contains(got, "release runs not read") || strings.Contains(got, "not-found") {
		t.Errorf("%s", got)
	}
	rr.Remote.Sources[overview.SourceCIRelease] = overview.SourceState{Outcome: overview.OutcomeError, Code: "network"}
	if got := statusRemoteLine(rr); !strings.Contains(got, "release runs not read (network)") {
		t.Errorf("%s", got)
	}
}

func TestOnlineTextEscapesHostileStrings(t *testing.T) {
	const canary = "ghp_CANARY0123456789"
	hostile := "x\x1b[31m\u202e\n\x07" + strings.Repeat("A", 10_000)
	rep := onlineReport()
	r0 := rep.Remote.Repos[0].Remote
	r0.Pulls = []overview.PR{{Number: 57, Title: hostile, URL: "https://github.com/wstein/sourcesteward/pull/57", Author: hostile, Group: overview.GroupReady}}
	r0.Drafts = []overview.Draft{{Name: hostile, Tag: hostile, URL: "javascript:alert(1)", Assets: 1}}
	r0.Tags = []overview.ReleaseTag{{Pattern: hostile, Tag: hostile, SHA: strings.Repeat("d", 40), Unreleased: true}}
	r0.Main.Branch = hostile
	rep.Remote.Repos[0].Pulls = &statusremote.Pulls{State: onlineOK, Open: statusPtrTo(1), Counts: pulls.Summary{Total: 1, Ready: 1}}
	rep.Remote.Repos[0].CIRelease = nil
	rep.Remote.Repos[0].Remote.Sources[overview.SourceCIMain] = overview.SourceState{Outcome: overview.OutcomeError, Code: hostile + canary[:0]}
	rep.Local.Repos[1].Name = hostile
	rep.Remote.Repos[1].Name = hostile
	rep.Local.Repos[1].Path = hostile
	rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
	for _, set := range []statuscmd.IconSet{statuscmd.Unicode, statuscmd.Nerd, statuscmd.ASCII} {
		out := renderOnline(t, rep, set, true, false)
		for _, bad := range []string{"\x1b", "\u202e", "\x07", "javascript:"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: raw %q in the output", set, bad)
			}
		}
		for _, l := range strings.Split(out, "\n") {
			if len(l) > 2000 {
				t.Errorf("%s: a line of %d bytes", set, len(l))
			}
		}
		if strings.Contains(out, strings.Repeat("A", 600)) {
			t.Errorf("%s: 10 kB reached the output", set)
		}
	}
}

func TestOnlineTextGroupsAndNamesThreeIdentifiers(t *testing.T) {
	rep := onlineReport()
	r1 := rep.Remote.Repos[1].Remote
	r1.Pulls = nil
	for i := 1; i <= 5; i++ {
		r1.Pulls = append(r1.Pulls, overview.PR{Number: i, Title: "t", URL: "https://github.com/wstein/crewbook/pull/1", Author: "a", Group: overview.GroupConflict})
	}
	rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
	out := renderOnline(t, rep, statuscmd.ASCII, false, false)
	if !strings.Contains(out, "5 PRs have conflicts: #1, #2, #3, +2 more\n     run: stw pulls --repo wstein/crewbook\n") {
		t.Errorf("%s", out)
	}
}

func TestIssuesLineTexts(t *testing.T) {
	rr := onlineReport().Remote.Repos[0]
	for name, tc := range map[string]struct {
		state  overview.SourceState
		issues *statusremote.Issues
		want   string
	}{
		"counts":    {onlineOK, &statusremote.Issues{State: onlineOK, Open: 40, Counts: issues.Counts{Red: 3, Yellow: 7, Blue: 20, Violet: 10}}, "issues: 40 open (3 red, 7 yellow)"},
		"truncated": {onlineOK, &statusremote.Issues{State: onlineOK, Open: 1000, Counts: issues.Counts{Red: 1, Blue: 999}, Truncated: true}, "issues: 1000+ open (1 red, 0 yellow, truncated)"},
		"disabled":  {overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonIssuesDisabled}, nil, "issues: disabled"},
		"deadline":  {overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}, nil, "issues: not read (deadline)"},
		"no token":  {overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNoToken}, nil, "issues: not read (no-token)"},
		"offline":   {overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonOffline}, nil, "issues: not read (offline)"},
		"failed":    {overview.SourceState{Outcome: overview.OutcomeError, Code: "rate-limited"}, nil, "issues: not read (rate-limited)"},
		"hostile":   {overview.SourceState{Outcome: overview.OutcomeError, Code: "x\x1b[31m"}, nil, `issues: not read (x\x1b[31m)`},
	} {
		rr.Issues, rr.Remote.Sources = tc.issues, onlineSources(onlineOK)
		rr.Remote.Sources[overview.SourceIssues] = tc.state
		if got := statusIssuesLine(rr); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
	// what is a fact of the repository is told by the remote line alone
	for _, reason := range []string{overview.ReasonNotGitHub, overview.ReasonOtherHost, overview.ReasonNotReadLocally, overview.ReasonInvalidRepo} {
		rr.Remote.Sources = onlineSources(overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: reason})
		if got := statusIssuesLine(rr); got != "" {
			t.Errorf("%s: %q", reason, got)
		}
	}
}

// The remote line is about the six sources it always was; the issues source has a line of its own.
func TestTheRemoteLineIgnoresTheIssuesSource(t *testing.T) {
	rr := onlineReport().Remote.Repos[0]
	rr.Remote.Sources[overview.SourceIssues] = overview.SourceState{Outcome: overview.OutcomeError, Code: "network"}
	if got := statusRemoteLine(rr); strings.Contains(got, "issues") || strings.Contains(got, "network") {
		t.Errorf("%s", got)
	}
}

func TestOnlineTextGroupsIssuesOfOneKind(t *testing.T) {
	rep := onlineReport()
	r0 := rep.Remote.Repos[0].Remote
	r0.Issues = nil
	for _, n := range []int{12, 15, 19} {
		r0.Issues = append(r0.Issues, overview.Issue{Number: n, Title: "t", URL: "https://github.com/wstein/sourcesteward/issues/1", Author: "a", Reason: overview.IssueReasonSecurity})
	}
	rep.Overview = overview.Build(rep.Remote.Input("/ws", rep.Local.Repos))
	out := renderOnline(t, rep, statuscmd.ASCII, false, false)
	if !strings.Contains(out, "3 security issues: #12, #15, #19\n     run: stw issues --repo wstein/sourcesteward\n") {
		t.Errorf("%s", out)
	}
}

// Blue and violet issues are only counted; the remote line names the drill-down when there are any (Spec 038).
func TestTheRemoteLineNamesTheIssuesDrillDown(t *testing.T) {
	rr := onlineReport().Remote.Repos[0]
	const hint = "other issues: stw issues --repo wstein/sourcesteward"
	if got := statusRemoteLine(rr); !strings.HasSuffix(got, ", "+hint) {
		t.Errorf("blue and violet issues exist: %s", got)
	}
	for name, c := range map[string]issues.Counts{"blue": {Blue: 1}, "violet": {Violet: 1}} {
		rr.Issues = &statusremote.Issues{State: onlineOK, Open: 1, Counts: c}
		if got := statusRemoteLine(rr); !strings.Contains(got, hint) {
			t.Errorf("%s: %s", name, got)
		}
	}
	rr.Issues = &statusremote.Issues{State: onlineOK, Open: 2, Counts: issues.Counts{Red: 1, Yellow: 1}}
	if got := statusRemoteLine(rr); strings.Contains(got, "stw issues") {
		t.Errorf("only red and yellow issues, all of them items: %s", got)
	}
	rr.Issues = &statusremote.Issues{State: onlineOK, Open: 1, Counts: issues.Counts{Blue: 1}}
	rr.Remote.Sources[overview.SourceIssues] = overview.SourceState{Outcome: overview.OutcomeError, Code: "network"}
	if got := statusRemoteLine(rr); strings.Contains(got, "stw issues") {
		t.Errorf("issues that were not read have no count to point to: %s", got)
	}
}

// The issues source has no say in the remote line: six sources that were not read give "not read" however well
// the issues were read.
func TestTheRemoteLineIsNotReadWhenOnlyTheIssuesWereRead(t *testing.T) {
	rr := onlineReport().Remote.Repos[0]
	rr.Remote.Sources = onlineSources(overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline})
	rr.Remote.Sources[overview.SourceIssues] = onlineOK
	if got := statusRemoteLine(rr); got != "remote: not read (deadline)" {
		t.Errorf("%s", got)
	}
}
