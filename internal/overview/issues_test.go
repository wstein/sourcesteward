// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/issues"
)

// The reason strings of the model are those of the issues cycle (Spec 038), which this package must not import.
func TestIssueReasonsMatchTheIssuesCycle(t *testing.T) {
	pairs := [][2]string{{IssueReasonSecurity, issues.ReasonSecurity}, {IssueReasonAssigned, issues.ReasonAssigned}, {IssueReasonBug, issues.ReasonBug}}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("%q != %q", p[0], p[1])
		}
	}
	if string(SourceIssues) != "issues" || ReasonIssuesDisabled != "issues-disabled" {
		t.Error("source or skip reason renamed")
	}
}

func issue(n int, reason string, mod func(*Issue)) Issue {
	i := Issue{Number: n, Title: fmt.Sprintf("issue %d", n), URL: fmt.Sprintf("https://github.com/wstein/sourcesteward/issues/%d", n), Author: "alice", Reason: reason}
	if mod != nil {
		mod(&i)
	}
	return i
}

// withIssues is a remote whose issues source ended ok.
func withIssues(rows ...Issue) func(*Remote) {
	return func(r *Remote) {
		r.Sources[SourceIssues] = SourceState{Outcome: OutcomeOK}
		r.Issues = rows
	}
}

func TestIssueRules(t *testing.T) {
	cases := []struct {
		name     string
		row      Issue
		id       string
		priority Priority
		cat      Category
		title    string
	}{
		{"26 security", issue(12, IssueReasonSecurity, nil), "issue-security:wstein/sourcesteward:12", Red, IssueSecurity, "issue #12 is labelled security: issue 12"},
		{"27 assigned", issue(13, IssueReasonAssigned, nil), "issue-assigned:wstein/sourcesteward:13", Yellow, IssueAssigned, "issue #13 is assigned to you: issue 13"},
		{"28 bug", issue(14, IssueReasonBug, nil), "issue-bug:wstein/sourcesteward:14", Yellow, IssueBug, "bug issue #14: issue 14"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items := remote(withIssues(c.row))
			if len(items) != 1 {
				t.Fatalf("items = %v", ids(items))
			}
			it := items[0]
			wantCmd := fmt.Sprintf("gh issue view %d -R wstein/sourcesteward", c.row.Number)
			if it.ID != c.id || it.Priority != c.priority || it.Category != c.cat || it.Title != c.title || it.Command != wantCmd || it.Effect != Read ||
				it.URL != c.row.URL || it.Repo != slug || it.Path != "sourcesteward" || it.Subject != (Subject{Kind: KindIssue, Number: c.row.Number}) {
				t.Errorf("item = %+v", it)
			}
		})
	}
	if n := categoryNumber(IssueSecurity); n != 26 || categoryNumber(IssueAssigned) != 27 || categoryNumber(IssueBug) != 28 {
		t.Errorf("numbers %d", n)
	}
}

func TestBlueVioletAndUnknownIssuesAreNeverItems(t *testing.T) {
	rows := []Issue{issue(1, "none", nil), issue(2, "bot", nil), issue(3, "", nil), issue(4, "weird", nil)}
	if items := remote(withIssues(rows...)); len(items) != 0 {
		t.Fatalf("items = %v", ids(items))
	}
}

func TestIssueItemsNeedAnOkSource(t *testing.T) {
	rows := []Issue{issue(1, IssueReasonSecurity, nil)}
	for name, st := range map[string]SourceState{
		"error":           {Outcome: OutcomeError, Code: "network"},
		"rate limited":    {Outcome: OutcomeError, Code: CodeRateLimited},
		"deadline":        {Outcome: OutcomeSkipped, Reason: ReasonDeadline},
		"issues disabled": {Outcome: OutcomeSkipped, Reason: ReasonIssuesDisabled},
		"not attempted":   {},
	} {
		items := remote(func(r *Remote) { r.Sources[SourceIssues], r.Issues = st, rows })
		for _, it := range items {
			if it.Category != ReadError {
				t.Errorf("%s: item %s from a source that did not end ok", name, it.ID)
			}
		}
	}
}

// A failed or rate-skipped issue read is one read-error with the drill-down command and no issue item; a
// deadline skip and a repository with issues disabled are no item at all.
func TestIssueReadErrorsAndSkips(t *testing.T) {
	items := remote(func(r *Remote) {
		r.Sources[SourceIssues] = SourceState{Outcome: OutcomeError, Code: "forbidden"}
		r.Issues = []Issue{issue(1, IssueReasonSecurity, nil)}
	})
	if len(items) != 1 {
		t.Fatalf("items = %v", ids(items))
	}
	it := items[0]
	if it.ID != "read-error:wstein/sourcesteward:issues" || it.Priority != Yellow || it.Command != "" || it.Effect != "" || it.Hint != "stw issues --repo wstein/sourcesteward" ||
		it.Subject != (Subject{Kind: KindSource, Name: "issues"}) || it.Title != "issues could not be read: forbidden" {
		t.Errorf("item = %+v", it)
	}
	for _, st := range []SourceState{{Outcome: OutcomeSkipped, Reason: ReasonDeadline}, {Outcome: OutcomeSkipped, Reason: ReasonIssuesDisabled}, {Outcome: OutcomeSkipped, Reason: ReasonNoToken}} {
		if items := remote(func(r *Remote) { r.Sources[SourceIssues] = st }); len(items) != 0 {
			t.Errorf("%+v gave %v", st, ids(items))
		}
	}
}

func TestIssueEvidenceIsBoundedAndEscaped(t *testing.T) {
	var labels, assignees []string
	for i := 0; i < 30; i++ {
		labels = append(labels, fmt.Sprintf("l%d", i))
		assignees = append(assignees, fmt.Sprintf("a%d", i))
	}
	row := issue(1, IssueReasonSecurity, func(i *Issue) { i.Labels, i.Assignees, i.Author = labels, assignees, "ev\x1b[31mil" })
	ev := remote(withIssues(row))[0].Evidence
	if ev["reason"] != IssueReasonSecurity || ev["author"] != `ev\x1b[31mil` || len(ev["labels"].([]string)) != 20 || len(ev["assignees"].([]string)) != 20 {
		t.Fatalf("%v", ev)
	}
	if reflect.DeepEqual(ev["labels"], labels) {
		t.Fatal("labels not cut")
	}
	row = issue(2, IssueReasonBug, func(i *Issue) { i.Labels = []string{"bu\x00g"} })
	if got := remote(withIssues(row))[0].Evidence["labels"].([]string); got[0] != `bu\x00g` {
		t.Fatalf("%q", got)
	}
}

func TestHostileIssueValuesNeverReachTitleIdURLOrCommand(t *testing.T) {
	row := issue(7, IssueReasonSecurity, func(i *Issue) {
		i.Title, i.URL = "t\x1b[31m\r\nitle‮"+strings.Repeat("x", 10000), "http://github.com/o/r/issues/7"
	})
	it := remote(withIssues(row))[0]
	if it.URL != "" || strings.ContainsAny(it.Title, "\x1b\r\n‮") || len(it.Title) > 400 || it.Command != "gh issue view 7 -R wstein/sourcesteward" {
		t.Fatalf("%+v", it)
	}
	// An invalid repository has no remote items, and a number below 1 has no command.
	bad := remoteRepo(withIssues(issue(1, IssueReasonSecurity, nil)))
	bad.Slug = "no slash; rm"
	if items := remoteItems(bad); len(items) != 0 {
		t.Fatalf("%v", ids(items))
	}
	if cmd, eff := buildCommand(IssueSecurity, target{Slug: slug, Number: 0}); cmd != "" || eff != "" {
		t.Fatalf("%q", cmd)
	}
	if cmd, _ := buildCommand(IssueBug, target{Slug: "o/r;rm", Number: 1}); cmd != "" {
		t.Fatalf("%q", cmd)
	}
}

func TestSameIssueListedTwiceIsOneItem(t *testing.T) {
	items := Build(Input{Repos: []Repo{remoteRepo(withIssues(issue(1, IssueReasonBug, nil), issue(1, IssueReasonBug, nil)))}}).Items
	if len(items) != 1 {
		t.Fatalf("%v", ids(items))
	}
}

// Priority first, then the category number: the red issue follows the four red rows of the first table, the yellow
// issues follow the sixteen yellow ones.
func TestIssueItemsOrderAfterTheOlderCategoriesOfTheirPriority(t *testing.T) {
	yes := true
	rows := withIssues(issue(1, IssueReasonSecurity, nil), issue(2, IssueReasonAssigned, nil), issue(3, IssueReasonBug, nil))
	r := remoteRepo(func(rem *Remote) {
		rows(rem)
		rem.Main = &Main{Branch: "main", Head: sha40, Class: "red", BehindOrigin: &yes}
		rem.Pulls = []PR{pr(9, GroupReady, "", nil), pr(8, GroupConflict, "", nil)}
	})
	got := ids(Build(Input{Repos: []Repo{r}}).Items)
	want := []string{
		"main-red:wstein/sourcesteward:main", "issue-security:wstein/sourcesteward:1",
		"main-behind-origin:wstein/sourcesteward:main", "pr-conflict:wstein/sourcesteward:8", "issue-assigned:wstein/sourcesteward:2", "issue-bug:wstein/sourcesteward:3",
		"pr-ready:wstein/sourcesteward:9",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestIssueNumbersOrderNumerically(t *testing.T) {
	rows := withIssues(issue(10, IssueReasonSecurity, nil), issue(9, IssueReasonSecurity, nil), issue(100, IssueReasonSecurity, nil))
	got := ids(Build(Input{Repos: []Repo{remoteRepo(rows)}}).Items)
	want := []string{"issue-security:wstein/sourcesteward:9", "issue-security:wstein/sourcesteward:10", "issue-security:wstein/sourcesteward:100"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestIssueItemsGroupIntoOneLineWithThreeIdsAndTheDrillDown(t *testing.T) {
	var rows []Issue
	for _, n := range []int{12, 15, 19, 21, 22} {
		rows = append(rows, issue(n, IssueReasonSecurity, nil))
	}
	secs := Group(Build(Input{Repos: []Repo{remoteRepo(withIssues(rows...))}}).Items)
	if len(secs) != 1 || len(secs[0].Lines) != 1 {
		t.Fatalf("%+v", secs)
	}
	l := secs[0].Lines[0]
	if l.Count != 5 || strings.Join(l.Labels, ",") != "#12,#15,#19" || l.More != 2 || l.URL != "" || l.Command != "stw issues --repo wstein/sourcesteward" || l.Effect != Read {
		t.Fatalf("%+v", l)
	}
	one := Group(Build(Input{Repos: []Repo{remoteRepo(withIssues(rows[0]))}}).Items)[0].Lines[0]
	if one.Count != 1 || one.URL != rows[0].URL || one.Command != "gh issue view 12 -R wstein/sourcesteward" {
		t.Fatalf("%+v", one)
	}
}

func TestViewerNotices(t *testing.T) {
	cases := []struct {
		name   string
		viewer SourceState
		want   []Notice
	}{
		{"not attempted", SourceState{}, nil},
		{"ok", SourceState{Outcome: OutcomeOK}, nil},
		{"no identity", SourceState{Outcome: OutcomeSkipped, Reason: ReasonNoIdentity}, []Notice{{Code: "no-identity", Source: "viewer",
			Message: "the token cannot read the authenticated user: issues assigned to you are not flagged"}}},
		{"failed", SourceState{Outcome: OutcomeError, Code: "http-5xx"}, []Notice{{Code: "viewer-unknown", Source: "viewer",
			Message: "the authenticated user could not be read (http-5xx): issues assigned to you are not flagged"}}},
		{"rate limited", SourceState{Outcome: OutcomeError, Code: CodeRateLimited}, []Notice{{Code: "viewer-unknown", Source: "viewer",
			Message: "the authenticated user could not be read (rate-limited): issues assigned to you are not flagged"}}},
		{"hostile code", SourceState{Outcome: OutcomeError, Code: "x\x1b[31m"}, []Notice{{Code: "viewer-unknown", Source: "viewer",
			Message: `the authenticated user could not be read (x\x1b[31m): issues assigned to you are not flagged`}}},
		{"no request is no notice", SourceState{Outcome: OutcomeSkipped, Reason: ReasonNoToken}, nil},
		{"offline is no notice", SourceState{Outcome: OutcomeSkipped, Reason: "offline"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ov := Build(Input{Repos: []Repo{remoteRepo(withIssues())}, Viewer: c.viewer})
			if !reflect.DeepEqual(ov.Notices, c.want) && !(len(ov.Notices) == 0 && len(c.want) == 0) {
				t.Fatalf("got %+v\nwant %+v", ov.Notices, c.want)
			}
			if ov.Summary.Notices != len(c.want) {
				t.Errorf("summary notices %d", ov.Summary.Notices)
			}
		})
	}
}

func TestViewerOutcomeShapesTheSummary(t *testing.T) {
	repo := remoteRepo(withIssues())
	for _, c := range []struct {
		name             string
		viewer           SourceState
		degraded, failed bool
	}{
		{"ok", SourceState{Outcome: OutcomeOK}, false, false},
		{"not attempted", SourceState{}, false, false},
		{"no identity is a skip", SourceState{Outcome: OutcomeSkipped, Reason: ReasonNoIdentity}, true, false},
		{"failed read", SourceState{Outcome: OutcomeError, Code: "network"}, true, true},
	} {
		s := Build(Input{Repos: []Repo{repo}, Viewer: c.viewer}).Summary
		if s.Degraded != c.degraded || s.Failed != c.failed {
			t.Errorf("%s: %+v", c.name, s)
		}
	}
}

// A repository with issues disabled is a fact, not a gap: no notice, not degraded, no failure.
func TestIssuesDisabledIsNeitherANoticeNorDegraded(t *testing.T) {
	r := remoteRepo(func(r *Remote) {
		r.Sources[SourceIssues] = SourceState{Outcome: OutcomeSkipped, Reason: ReasonIssuesDisabled}
	})
	ov := Build(Input{Repos: []Repo{r}})
	if len(ov.Notices) != 0 || ov.Summary.Degraded || ov.Summary.Failed || len(ov.Items) != 0 {
		t.Fatalf("%+v", ov)
	}
	// The other skips of the source keep their meaning.
	r = remoteRepo(func(r *Remote) {
		r.Sources[SourceIssues] = SourceState{Outcome: OutcomeSkipped, Reason: ReasonDeadline}
	})
	if ov := Build(Input{Repos: []Repo{r}, Deadline: 90 * time.Second}); !ov.Summary.Degraded || len(ov.Notices) != 1 || ov.Notices[0].Code != "deadline" {
		t.Fatalf("%+v", ov)
	}
}

func TestFailedIssuesSourceGetsItsOwnNotice(t *testing.T) {
	r := remoteRepo(func(r *Remote) { r.Sources[SourceIssues] = SourceState{Outcome: OutcomeError, Code: "forbidden"} })
	ov := Build(Input{Repos: []Repo{r}})
	if len(ov.Notices) != 1 || ov.Notices[0].Source != "issues" || ov.Notices[0].Message != "forbidden: issues of 1 repositories not read" || !ov.Summary.Failed {
		t.Fatalf("%+v", ov.Notices)
	}
}
