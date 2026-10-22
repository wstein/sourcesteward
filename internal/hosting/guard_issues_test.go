// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"net/http"
	"strings"
	"testing"
)

var guardModesWithIssues = []struct {
	name string
	mode guardMode
}{{"oneShot", guardOneShot}, {"watch", guardWatch}, {"pulls", guardPulls}, {"status", guardStatus}, {"issues", guardIssues}}

func guardAllows(mode guardMode, host, prefix, target string) bool {
	g := guardedTransport{host: host, prefix: prefix, mode: mode}
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return false
	}
	return g.allowed(req)
}

// The guard table of the five modes against the three issue shapes, the PR search and the
// sets the modes already had (Spec 038 amendment of Spec 022 FR-005). Columns: one-shot,
// watch, pulls, status, issues; every column is asserted for every row.
func TestGuardTableOfTheFiveModes(t *testing.T) {
	sha40 := strings.Repeat("a", 40)
	list := func(page string) string { return issuesListBase + page }
	issueSearch, _ := IssueSearchEndpoint("wstein", 1)
	prSearch, _ := SearchEndpoint("wstein", 1)
	cases := []struct {
		path                          string
		oneShot, watch, pulls, st, is bool
	}{
		// the three new shapes
		{list("1"), false, false, false, true, true},
		{list("10"), false, false, false, true, true},
		{"user", false, false, false, true, true},
		{issueSearch, false, false, false, false, true},
		// the PR search stays with pulls
		{prSearch, false, false, true, false, false},
		// the watch set: watch, pulls and status; never issues
		{"repos/a/b/pulls?state=open&per_page=25&page=1", false, true, true, true, false},
		{"repos/a/b/pulls?state=open&per_page=12&page=167", false, true, true, true, false},
		{"repos/a/b/pulls/3/commits?per_page=100&page=3", false, true, true, true, false},
		{"rate_limit", false, true, true, true, false},
		{"repos/a/b/commits/" + sha40, false, true, true, true, false},
		{"repos/a/b/branches/main", false, true, true, true, false},
		// the one-shot set: every mode
		{"repos/a/b/pulls/3", true, true, true, true, true},
		{"repos/a/b/commits/" + sha40 + "/check-runs?per_page=100&page=1", true, true, true, true, true},
		{"repos/a/b/commits/" + sha40 + "/statuses?per_page=100&page=10", true, true, true, true, true},
		{"repos/o/r/releases?per_page=100&page=1", true, true, true, true, true},
		{"repos/o/r/tags?per_page=100&page=10", true, true, true, true, true},
		{"repos/o/r", true, true, true, true, true},
		// the list shape outside its fixed form: refused by every mode
		{list("0"), false, false, false, false, false},
		{list("11"), false, false, false, false, false},
		{list("01"), false, false, false, false, false},
		{strings.Replace(list("1"), "per_page=100", "per_page=99", 1), false, false, false, false, false},
		{strings.Replace(list("1"), "state=open", "state=all", 1), false, false, false, false, false},
		{strings.Replace(list("1"), "page=1", "labels=&page=1", 1), false, false, false, false, false},
		{list("1") + "&assignee=x", false, false, false, false, false},
		{"repos/o/r/issues?per_page=100&state=open&sort=created&direction=asc&page=1", false, false, false, false, false},
		{"repos/o/r/issues", false, false, false, false, false},
		{"repos/o/r/issues/1", false, false, false, false, false},
		{"repos/o/r/issues/comments", false, false, false, false, false},
		{"repos/o/r%2fx/issues?state=open&per_page=100&sort=created&direction=asc&page=1", false, false, false, false, false},
		{"repos/o%2Fr/issues?state=open&per_page=100&sort=created&direction=asc&page=1", false, false, false, false, false},
		// user and the issue search outside their fixed form
		{"user?x=1", false, false, false, false, false},
		{"users/x", false, false, false, false, false},
		{"user/emails", false, false, false, false, false},
		{strings.Replace(issueSearch, "is%3Aissue", "is%3Aissue+is%3Aprivate", 1), false, false, false, false, false},
		{strings.Replace(issueSearch, "%3A", "%3a", 1), false, false, false, false, false},
		{strings.Replace(issueSearch, "page=1", "page=11", 1), false, false, false, false, false},
		{"search/issues", false, false, false, false, false},
		{"search/code?q=a", false, false, false, false, false},
	}
	for _, c := range cases {
		for i, m := range guardModesWithIssues {
			want := []bool{c.oneShot, c.watch, c.pulls, c.st, c.is}[i]
			if got := guardAllows(m.mode, "api.github.com", "/", "https://api.github.com/"+c.path); got != want {
				t.Errorf("%s %s: allowed=%v want %v", m.name, c.path, got, want)
			}
		}
	}
}

// An escaped path is a watch-reader feature: the issues guard does not carry the watch set, so it
// refuses it even where it would decode to the same path; status keeps it.
func TestGuardIssuesRefusesEscapedPathStatusKeepsIt(t *testing.T) {
	for _, m := range guardModesWithIssues {
		want := m.mode == guardWatch || m.mode == guardPulls || m.mode == guardStatus
		if got := guardAllows(m.mode, "api.github.com", "/", "https://api.github.com/repos/a/b/branches/feat%2Fx"); got != want {
			t.Errorf("%s: %v want %v", m.name, got, want)
		}
	}
}

// The issue shapes follow the Enterprise prefix and every mode keeps the request rules.
func TestGuardIssuesKeepsRequestRulesAndHostPrefix(t *testing.T) {
	issueSearch, _ := IssueSearchEndpoint("wstein", 1)
	for _, path := range []string{issuesListBase + "1", "user", issueSearch} {
		g := guardedTransport{host: "ghe.example.com", prefix: "/api/v3/", mode: guardIssues}
		req, _ := http.NewRequest("GET", "https://ghe.example.com/api/v3/"+path, nil)
		if !g.allowed(req) {
			t.Errorf("enterprise prefix refused %s", path)
		}
		for name, build := range map[string]func() *http.Request{
			"post":     func() *http.Request { r, _ := http.NewRequest("POST", "https://api.github.com/"+path, nil); return r },
			"head":     func() *http.Request { r, _ := http.NewRequest("HEAD", "https://api.github.com/"+path, nil); return r },
			"http":     func() *http.Request { r, _ := http.NewRequest("GET", "http://api.github.com/"+path, nil); return r },
			"host":     func() *http.Request { r, _ := http.NewRequest("GET", "https://evil.example/"+path, nil); return r },
			"userinfo": func() *http.Request { r, _ := http.NewRequest("GET", "https://u@api.github.com/"+path, nil); return r },
			"fragment": func() *http.Request {
				r, _ := http.NewRequest("GET", "https://api.github.com/"+path+"#x", nil)
				return r
			},
			"body": func() *http.Request {
				r, _ := http.NewRequest("GET", "https://api.github.com/"+path, strings.NewReader("x"))
				return r
			},
			"prefix": func() *http.Request {
				r, _ := http.NewRequest("GET", "https://api.github.com/api/v3/"+path, nil)
				return r
			},
		} {
			for _, mode := range []guardMode{guardIssues, guardStatus} {
				if mode == guardStatus && path == issueSearch {
					continue
				}
				gg := guardedTransport{host: "api.github.com", prefix: "/", mode: mode}
				if gg.allowed(build()) {
					t.Errorf("mode %d admitted %s for %s", mode, name, path)
				}
			}
		}
	}
}

// The mode wiring: neither the legacy watch field nor any older mode reaches an issue shape.
func TestGuardIssueShapesUnreachableFromTheOlderModes(t *testing.T) {
	issueSearch, _ := IssueSearchEndpoint("wstein", 1)
	variants := map[string]guardedTransport{
		"zero value":   {host: "api.github.com", prefix: "/"},
		"legacy watch": {host: "api.github.com", prefix: "/", watch: true},
		"watch":        {host: "api.github.com", prefix: "/", mode: guardWatch},
		"pulls":        {host: "api.github.com", prefix: "/", mode: guardPulls},
	}
	for name, g := range variants {
		for _, path := range []string{issuesListBase + "1", "user", issueSearch} {
			req, _ := http.NewRequest("GET", "https://api.github.com/"+path, nil)
			if g.allowed(req) {
				t.Errorf("%s reached %s", name, path)
			}
		}
	}
	for _, m := range guardModesWithIssues {
		if got, want := m.mode.admitsIssueReads(), m.mode == guardIssues || m.mode == guardStatus; got != want {
			t.Errorf("%s admitsIssueReads %v", m.name, got)
		}
		if got, want := admitsIssueSearch(m.mode), m.mode == guardIssues; got != want {
			t.Errorf("%s admitsIssueSearch %v", m.name, got)
		}
		if got, want := admitsSearch(m.mode), m.mode == guardPulls; got != want {
			t.Errorf("%s admitsSearch %v", m.name, got)
		}
	}
}
