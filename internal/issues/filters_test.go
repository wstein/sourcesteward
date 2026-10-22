// SPDX-License-Identifier: EUPL-1.2
package issues

import (
	"fmt"
	"slices"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func filterFixture() *fakeForge {
	f := newForge()
	many := make([]string, 30)
	for i := range many {
		many[i] = fmt.Sprintf("l%02d", i)
	}
	f.pages["o/r"] = []string{body(
		raw(1, withLabels("bug")),
		raw(2, withLabels("docs")),
		raw(3, withLabels("bugfix", "type: bug")),
		raw(4, withLabels("Docs", "BUG"), withAssignees("Bob")),
		raw(5, withAuthor("dependabot[bot]", "Bot"), withAssignees("copilot[bot]")),
		raw(6, withLabels(many...), withAssignees("wstein", "bob")),
		raw(7, withAuthor("Carol", "User")),
	)}
	return f
}

func listed(t *testing.T, o Options) []string {
	t.Helper()
	o.Repos = []string{"o/r"}
	res := run(t, filterFixture(), o)
	var out []string
	for _, r := range res.Rows {
		out = append(out, fmt.Sprint(r.Number))
	}
	slices.Sort(out)
	return out
}

func TestFilterLabelsAreAnyOfExactAndAsciiFolded(t *testing.T) {
	for name, tc := range map[string]struct {
		labels []string
		want   []string
	}{
		"one":              {[]string{"bug"}, []string{"1", "4"}},
		"case":             {[]string{"BUG"}, []string{"1", "4"}},
		"any of":           {[]string{"docs", "bug"}, []string{"1", "2", "4"}},
		"exact":            {[]string{"bugfix"}, []string{"3"}},
		"substring":        {[]string{"ug"}, nil},
		"label past 20":    {[]string{"L25"}, []string{"6"}},
		"unicode no match": {[]string{"BUK"}, nil},
	} {
		if got := listed(t, Options{Labels: tc.labels}); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestFilterAssigneesNoneMeAndLogins(t *testing.T) {
	for name, tc := range map[string]struct {
		assignees []string
		want      []string
	}{
		"login case":    {[]string{"BOB"}, []string{"4", "6"}},
		"bot login":     {[]string{"Copilot[bot]"}, []string{"5"}},
		"bot suffix":    {[]string{"copilot"}, nil},
		"none":          {[]string{AssigneeNone}, []string{"1", "2", "3", "7"}},
		"me":            {[]string{AssigneeMe}, []string{"6"}},
		"me and none":   {[]string{AssigneeMe, AssigneeNone}, []string{"1", "2", "3", "6", "7"}},
		"any of logins": {[]string{"bob", "copilot[bot]"}, []string{"4", "5", "6"}},
	} {
		if got := listed(t, Options{Assignees: tc.assignees}); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestFilterAuthorsKeepTheBotMarker(t *testing.T) {
	for name, tc := range map[string]struct {
		authors []string
		want    []string
	}{
		"bot":       {[]string{"dependabot[bot]"}, []string{"5"}},
		"without":   {[]string{"dependabot"}, nil},
		"case":      {[]string{"carol"}, []string{"7"}},
		"any of":    {[]string{"carol", "DEPENDABOT[BOT]"}, []string{"5", "7"}},
		"the alice": {[]string{"alice"}, []string{"1", "2", "3", "4", "6"}},
	} {
		if got := listed(t, Options{Authors: tc.authors}); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

func TestFilterFlagsIntersect(t *testing.T) {
	got := listed(t, Options{Labels: []string{"bug", "docs"}, Assignees: []string{"bob"}, Authors: []string{"alice"}})
	if !slices.Equal(got, []string{"4"}) {
		t.Errorf("%v", got)
	}
	if got := listed(t, Options{Labels: []string{"bug"}, Assignees: []string{"bob"}}); !slices.Equal(got, []string{"4"}) {
		t.Errorf("%v", got)
	}
	if got := listed(t, Options{Labels: []string{"docs"}, Authors: []string{"carol"}}); got != nil {
		t.Errorf("%v", got)
	}
}

func TestFiltersNeverReachTheForgeAndCountsFollowThem(t *testing.T) {
	f := filterFixture()
	res := run(t, f, Options{Repos: []string{"o/r"}, Labels: []string{"bug"}, Assignees: []string{AssigneeNone, "wstein"}, Authors: []string{"alice"}})
	if got := f.callList(); !slices.Equal(got, []string{"user", "o/r#1"}) {
		t.Errorf("requests %v", got)
	}
	if st := repoStatus(t, res, "o/r"); *st.Open != 7 {
		t.Errorf("open %d counts what was read, before the filters", *st.Open)
	}
	if res.Summary.Counts != (Counts{Yellow: 1}) || res.Summary.Total != 1 {
		t.Errorf("summary %+v", res.Summary)
	}
}

func TestCapKeepsRedFirstAndCountsCoverEverythingRead(t *testing.T) {
	f := newForge()
	f.pages["a/x"] = []string{body(raw(1), raw(2), raw(3, withLabels("bug")))}
	f.pages["z/y"] = []string{body(raw(1, withLabels("security")), raw(2, withAuthor("b[bot]", "Bot")), raw(3))}
	res := run(t, f, Options{Repos: []string{"a/x", "z/y"}, Limit: 3})
	if got := numbers(res.Rows); !slices.Equal(got, []string{"z/y#1", "a/x#3", "a/x#1"}) {
		t.Fatalf("rows %v", got)
	}
	s := res.Summary
	if s.Total != 3 || s.Red != 1 || s.Yellow != 1 || s.Blue != 1 || s.Violet != 0 || s.Bots != 0 || s.Omitted != 3 {
		t.Errorf("summary %+v", s)
	}
	if s.Counts != (Counts{Red: 1, Yellow: 1, Blue: 3, Violet: 1}) {
		t.Errorf("counts %+v", s.Counts)
	}
	if res.Limit != 3 {
		t.Errorf("limit %d", res.Limit)
	}
}

func TestCapDefaultsTo200AndIsBounded(t *testing.T) {
	f := newForge()
	var entries []entry
	for i := 1; i <= 100; i++ {
		entries = append(entries, raw(i))
	}
	f.pages["o/r"] = []string{body(entries...), body(entries...), body(raw(1))}
	res := run(t, f, Options{Repos: []string{"o/r"}})
	if res.Limit != 200 || res.Summary.Total != 200 || res.Summary.Omitted != 1 || res.Summary.Counts.Blue != 201 {
		t.Errorf("limit %d summary %+v", res.Limit, res.Summary)
	}
	if res := run(t, f, Options{Repos: []string{"o/r"}, Limit: 500}); res.Summary.Omitted != 0 || res.Summary.Total != 201 {
		t.Errorf("summary %+v", res.Summary)
	}
	for _, limit := range []int{-1, 501} {
		if res, err := Run(t.Context(), f, Options{Repos: []string{"o/r"}, Limit: limit}); err == nil || res != nil {
			t.Errorf("limit %d: %v %v", limit, res, err)
		}
	}
}

func TestSecurityPastTheDisplayCutIsStillRed(t *testing.T) {
	f := newForge()
	labels := make([]string, 100)
	for i := range labels {
		labels[i] = fmt.Sprintf("l%d", i)
	}
	labels[99] = "security"
	f.pages["o/r"] = []string{body(raw(1, withLabels(labels...)))}
	row := run(t, f, Options{Repos: []string{"o/r"}}).Rows[0]
	if row.Priority != Red || len(row.Labels) != 20 || row.LabelsTotal != 100 || slices.Contains(row.Labels, "security") {
		t.Errorf("row %+v", row)
	}
}

func TestAssigneeMeWithoutALoginIsAFailedRunWithoutRows(t *testing.T) {
	for name, viewerErr := range map[string]error{
		"no identity": &hosting.WatchError{Code: hosting.CodeForbidden, Status: 403, Message: "access was refused"},
		"500":         &hosting.WatchError{Code: hosting.CodeHTTP5xx, Status: 500, Message: "server error"},
		"rate limit":  &hosting.WatchError{Code: hosting.CodeRateLimited, Status: 429, Message: "rate limit reached"},
	} {
		f := filterFixture()
		f.pages["p/q"] = f.pages["o/r"]
		f.viewer, f.viewerErr = "", viewerErr
		res := run(t, f, Options{Repos: []string{"o/r", "p/q"}, Assignees: []string{"bob", AssigneeMe}})
		wantFailed := 0
		if name == "rate limit" {
			wantFailed = 2
		}
		if len(res.Rows) != 0 || res.Summary != (Summary{ReposFailed: wantFailed}) {
			t.Errorf("%s: rows %d summary %+v", name, len(res.Rows), res.Summary)
		}
		if got := f.callList(); !slices.Equal(got, []string{"user"}) {
			t.Errorf("%s: requests %v", name, got)
		}
		var unknown []Error
		for _, e := range res.Errors {
			if e.Code == CodeViewerUnknown {
				unknown = append(unknown, e)
			}
		}
		if len(unknown) != 1 || unknown[0].Repo != "" {
			t.Errorf("%s: errors %+v", name, res.Errors)
		}
		for _, st := range res.Repos {
			if st.Open != nil || (name == "rate limit") != (st.State == StateError) || (name != "rate limit" && (st.State != StateSkipped || st.Reason != CodeViewerUnknown)) {
				t.Errorf("%s: status %+v", name, st)
			}
		}
	}
}

func TestAssigneeMeWithALoginListsTheAssignedIssues(t *testing.T) {
	f := filterFixture()
	res := run(t, f, Options{Repos: []string{"o/r"}, Assignees: []string{AssigneeMe}})
	if got := numbers(res.Rows); !slices.Equal(got, []string{"o/r#6"}) || res.Rows[0].Reason != ReasonAssigned || len(res.Errors) != 0 {
		t.Errorf("rows %v errors %+v", got, res.Errors)
	}
}
