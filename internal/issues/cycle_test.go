// SPDX-License-Identifier: EUPL-1.2
package issues

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func TestRunClassifiesSortsAndFillsTheRows(t *testing.T) {
	f := newForge()
	f.pages["o/b"] = []string{body(raw(9), raw(3, withLabels("bug")))}
	f.pages["O/a"] = []string{body(
		raw(7, withLabels("docs")),
		raw(6),
		raw(2, withLabels("Security", "bug"), withAssignees("bob", "carol")),
		raw(5, withAuthor("renovate[bot]", "Bot")),
		raw(4, withLabels("bug")),
	)}
	res := run(t, f, Options{Repos: []string{"o/b", "O/a"}})

	want := []string{"O/a#2", "O/a#4", "o/b#3", "O/a#6", "O/a#7", "o/b#9", "O/a#5"}
	if got := numbers(res.Rows); !slices.Equal(got, want) {
		t.Fatalf("rows %v, want %v", got, want)
	}
	first := res.Rows[0]
	if first.Priority != Red || first.Reason != ReasonSecurity || first.Command != "gh issue view 2 -R O/a" || first.Effect != "read" {
		t.Errorf("red row: %+v", first)
	}
	if first.Title != "issue 2" || first.URL != "https://github.com/o/r/issues/2" || first.Author != "alice" || first.Bot {
		t.Errorf("red row fields: %+v", first)
	}
	if !slices.Equal(first.Labels, []string{"Security", "bug"}) || first.LabelsTotal != 2 || !slices.Equal(first.Assignees, []string{"bob", "carol"}) {
		t.Errorf("red row lists: %+v", first)
	}
	if first.AgeDays == nil || *first.AgeDays != 2 || first.Comments == nil || *first.Comments != 0 {
		t.Errorf("age or comments: %+v", first)
	}
	if yellow := res.Rows[1]; yellow.Priority != Yellow || yellow.Reason != ReasonBug || yellow.Command != "gh issue view 4 -R O/a" || yellow.Effect != "read" {
		t.Errorf("yellow row: %+v", yellow)
	}
	blue, violet := res.Rows[3], res.Rows[6]
	if blue.Priority != Blue || blue.Command != "" || blue.Effect != "" {
		t.Errorf("blue row carries a command: %+v", blue)
	}
	if violet.Priority != Violet || !violet.Bot || violet.Command != "" {
		t.Errorf("violet row: %+v", violet)
	}
	s := res.Summary
	if s.Total != 7 || s.Red != 1 || s.Yellow != 2 || s.Blue != 3 || s.Violet != 1 || s.Bots != 1 || s.ReposRead != 2 || s.ReposFailed != 0 {
		t.Errorf("summary %+v", s)
	}
	if s.Counts != (Counts{Red: 1, Yellow: 2, Blue: 3, Violet: 1}) {
		t.Errorf("counts %+v", s.Counts)
	}
	if len(res.Errors) != 0 {
		t.Errorf("errors %+v", res.Errors)
	}
	if st := repoStatus(t, res, "O/a"); st.State != StateOK || st.Open == nil || *st.Open != 5 || st.PullsDropped == nil || *st.PullsDropped != 0 {
		t.Errorf("status %+v", st)
	}
}

func TestRunRowEdgeCases(t *testing.T) {
	f := newForge()
	long := strings.Repeat("é", 2000) // 4000 bytes
	labels := make([]string, 30)
	for i := range labels {
		labels[i] = fmt.Sprintf("l%02d", i)
	}
	f.pages["o/r"] = []string{body(
		raw(1, func(e entry) {
			e["title"] = long
			e["html_url"] = "http://github.com/o/r/issues/1"
			e["created_at"] = nil
			e["comments"] = nil
		}),
		raw(2, withLabels(labels...), withAssignees(labels...), func(e entry) { e["created_at"] = "2026-10-23T09:00:00Z" }),
		raw(3, func(e entry) { e["user"] = nil }),
	)}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	byNumber := map[int]Row{}
	for _, r := range res.Rows {
		byNumber[r.Number] = r
	}
	one := byNumber[1]
	if len(one.Title) > 2048 || !strings.HasPrefix(long, one.Title) || len(one.Title)%2 != 0 {
		t.Errorf("title not cut on a boundary: %d bytes", len(one.Title))
	}
	if one.URL != "" || one.AgeDays != nil || !one.CreatedAt.IsZero() || one.Comments != nil {
		t.Errorf("row without url, date, comments: %+v", one)
	}
	two := byNumber[2]
	if len(two.Labels) != 20 || two.LabelsTotal != 30 || len(two.Assignees) != 20 || two.AgeDays == nil || *two.AgeDays != 0 {
		t.Errorf("row two: labels %d total %d assignees %d age %v", len(two.Labels), two.LabelsTotal, len(two.Assignees), two.AgeDays)
	}
	if three := byNumber[3]; three.Author != "ghost" || three.Bot {
		t.Errorf("ghost: %+v", three)
	}
}

func TestRunDropsPullRequestsAndCountsThem(t *testing.T) {
	f := newForge()
	f.pages["o/r"] = []string{body(raw(1, asPull), raw(2), raw(3, func(e entry) { e["pull_request"] = nil }))}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if got := numbers(res.Rows); !slices.Equal(got, []string{"o/r#2"}) {
		t.Fatalf("rows %v", got)
	}
	if st := repoStatus(t, res, "o/r"); *st.Open != 1 || *st.PullsDropped != 2 {
		t.Errorf("status open %d dropped %d", *st.Open, *st.PullsDropped)
	}
}

func TestRunReadsPagesByNumberAndNeverFollowsLink(t *testing.T) {
	f := newForge()
	f.pages["o/r"] = []string{body(raw(1)), body(raw(2)), body(raw(3))}
	res := run(t, f, Options{Repos: []string{"o/r", "O/R"}})
	if got := f.callList(); !slices.Equal(got, []string{"o/r#1", "o/r#2", "o/r#3"}) {
		t.Fatalf("calls %v", got)
	}
	if len(res.Rows) != 3 || len(res.Repos) != 1 {
		t.Errorf("rows %d repos %d", len(res.Rows), len(res.Repos))
	}
}

func TestRunRefusesALinkThatSkipsAPage(t *testing.T) {
	f := newForge()
	f.pages["o/r"] = []string{body(raw(1)), body(raw(2))}
	hostile := &skipLink{fakeForge: f}
	res := run(t, hostile, Options{Repos: []string{"o/r"}})
	if st := repoStatus(t, res, "o/r"); st.State != StateError || st.Error != "malformed-response" || len(res.Rows) != 0 {
		t.Errorf("status %+v rows %d", st, len(res.Rows))
	}
}

func TestRunMarksTheTenthPageAsATruncation(t *testing.T) {
	f := newForge()
	for i := 1; i <= 11; i++ {
		f.pages["o/r"] = append(f.pages["o/r"], body(raw(i)))
	}
	f.pages["p/q"] = []string{body(raw(1))}
	res := run(t, f, Options{Repos: []string{"o/r", "p/q"}})
	if f.count("o/r#") != 10 {
		t.Fatalf("calls %v", f.callList())
	}
	st := repoStatus(t, res, "o/r")
	if st.State != StatePartial || st.Error != CodeTooManyOpenIssues || st.Open == nil || *st.Open != 10 {
		t.Errorf("status %+v", st)
	}
	if len(res.Rows) != 11 { // ten kept rows of o/r and the one of p/q
		t.Errorf("rows %d", len(res.Rows))
	}
	if len(res.Errors) != 1 || res.Errors[0].Repo != "o/r" || res.Errors[0].Code != CodeTooManyOpenIssues {
		t.Errorf("errors %+v", res.Errors)
	}
	if res.Summary.ReposFailed != 1 || res.Summary.ReposRead != 1 {
		t.Errorf("summary %+v", res.Summary)
	}
}

func TestRunTenthPageWithoutSuccessorIsNoTruncation(t *testing.T) {
	f := newForge()
	for i := 1; i <= 10; i++ {
		f.pages["o/r"] = append(f.pages["o/r"], body(raw(i)))
	}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if st := repoStatus(t, res, "o/r"); st.State != StateOK || len(res.Errors) != 0 || len(res.Rows) != 10 {
		t.Errorf("status %+v errors %v rows %d", st, res.Errors, len(res.Rows))
	}
}

func TestRunIssuesDisabledIsSkippedNotAnError(t *testing.T) {
	f := newForge()
	f.errAt["o/off#1"] = &hosting.WatchError{Code: hosting.CodeIssuesDisabled, Status: 410, Message: "issues are disabled for this repository"}
	f.pages["o/on"] = []string{body(raw(1))}
	res := run(t, f, Options{Repos: []string{"o/off", "o/on"}})
	st := repoStatus(t, res, "o/off")
	if st.State != StateSkipped || st.Reason != hosting.CodeIssuesDisabled || st.Error != "" || st.Open == nil || *st.Open != 0 {
		t.Errorf("status %+v", st)
	}
	if len(res.Errors) != 0 || res.Summary.ReposFailed != 0 || res.Summary.ReposRead != 2 || len(res.Rows) != 1 {
		t.Errorf("errors %v summary %+v rows %d", res.Errors, res.Summary, len(res.Rows))
	}
}

func TestRunFailedRepositoryDiscardsItsRows(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code string
	}{
		"500":       {&hosting.WatchError{Code: hosting.CodeHTTP5xx, Status: 500, Message: "server error"}, hosting.CodeHTTP5xx},
		"network":   {fmt.Errorf("dial tcp: secret-token-canary"), hosting.CodeNetwork},
		"forbidden": {&hosting.WatchError{Code: hosting.CodeForbidden, Status: 404, Message: "resource not found or not accessible"}, hosting.CodeForbidden},
	} {
		f := newForge()
		f.pages["a/x"] = []string{body(raw(1)), body(raw(2))}
		f.pages["b/y"] = []string{body(raw(3))}
		f.errAt["a/x#2"] = tc.err
		res := run(t, f, Options{Repos: []string{"a/x", "b/y"}})
		st := repoStatus(t, res, "a/x")
		if st.State != StateError || st.Error != tc.code || st.Open != nil || st.PullsDropped != nil {
			t.Errorf("%s: status %+v", name, st)
		}
		if got := numbers(res.Rows); !slices.Equal(got, []string{"b/y#3"}) {
			t.Errorf("%s: rows %v", name, got)
		}
		if len(res.Errors) != 1 || res.Errors[0].Repo != "a/x" || res.Errors[0].Code != tc.code || strings.Contains(res.Errors[0].Message, "canary") {
			t.Errorf("%s: errors %+v", name, res.Errors)
		}
		if res.Summary.ReposFailed != 1 || res.Summary.ReposRead != 1 || res.Summary.Counts.Blue != 1 {
			t.Errorf("%s: summary %+v", name, res.Summary)
		}
	}
}

func TestRunMalformedPageFailsTheRepository(t *testing.T) {
	f := newForge()
	f.pages["o/r"] = []string{`{"not":"an array"}`}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if st := repoStatus(t, res, "o/r"); st.State != StateError || st.Error != hosting.CodeMalformed {
		t.Errorf("status %+v", st)
	}
}

func TestRunErrorsAreSortedByRepositoryThenCode(t *testing.T) {
	f := newForge()
	for _, r := range []string{"b/y", "A/x", "c/z"} {
		f.errAt[r+"#1"] = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "server error"}
	}
	res := run(t, f, Options{Repos: []string{"b/y", "c/z", "A/x"}})
	var got []string
	for _, e := range res.Errors {
		got = append(got, e.Repo)
	}
	if !slices.Equal(got, []string{"A/x", "b/y", "c/z"}) {
		t.Errorf("errors %v", got)
	}
}

func TestRunNoRepositoriesIsAnEmptyCleanResult(t *testing.T) {
	res := run(t, newForge(), Options{})
	if len(res.Rows) != 0 || len(res.Repos) != 0 || len(res.Errors) != 0 || res.Summary != (Summary{}) {
		t.Errorf("result %+v", res)
	}
}

func TestResultIsIndependentOfInputOrder(t *testing.T) {
	build := func() *fakeForge {
		f := newForge()
		for i, r := range []string{"a/x", "B/y", "c/z", "d/w", "e/v", "F/u"} {
			f.pages[r] = []string{body(raw(i+1), raw(10+i, withLabels("bug")), raw(20+i, withLabels("security")))}
		}
		return f
	}
	repos := []string{"a/x", "B/y", "c/z", "d/w", "e/v", "F/u"}
	want := run(t, build(), Options{Repos: repos})
	for _, order := range [][]string{{"F/u", "e/v", "d/w", "c/z", "B/y", "a/x"}, {"c/z", "F/u", "a/x", "e/v", "B/y", "d/w"}} {
		got := run(t, build(), Options{Repos: order})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("order %v changed the result", order)
		}
	}
}
