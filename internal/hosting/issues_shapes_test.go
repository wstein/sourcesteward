// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"strings"
	"testing"
)

func TestIssuesEndpointGeneratorIsExact(t *testing.T) {
	got, err := IssuesEndpoint("wstein/sourcesteward", 3)
	want := "repos/wstein/sourcesteward/issues?state=open&per_page=100&sort=created&direction=asc&page=3"
	if err != nil || got != want {
		t.Fatalf("%q %v", got, err)
	}
	for _, p := range []int{0, 11, -1} {
		if _, err := IssuesEndpoint("o/r", p); err == nil {
			t.Errorf("page %d admitted", p)
		}
	}
	for _, r := range []string{"", "o", "o/r/x", "o/../r", "o/.", "o/r?x", "o/r%2F", "o r/x", "ä/r"} {
		if _, err := IssuesEndpoint(r, 1); err == nil {
			t.Errorf("repository %q admitted", r)
		}
	}
}

func TestIssueSearchEndpointGeneratorIsExact(t *testing.T) {
	got, err := IssueSearchEndpoint("wstein", 3)
	want := "search/issues?q=user%3Awstein+is%3Aissue+is%3Aopen+archived%3Afalse&per_page=100&page=3&sort=created&order=asc"
	if err != nil || got != want {
		t.Fatalf("%q %v", got, err)
	}
	for _, p := range []int{0, 11, -1} {
		if _, err := IssueSearchEndpoint("wstein", p); err == nil {
			t.Errorf("page %d admitted", p)
		}
	}
	for _, o := range []string{"x+is:private", "a%0ab", "ä", strings.Repeat("a", 40), ""} {
		if _, err := IssueSearchEndpoint(o, 1); err == nil {
			t.Errorf("owner %q admitted", o)
		}
	}
	pr, _ := SearchEndpoint("wstein", 3)
	if pr == got || !strings.Contains(pr, "is%3Apr") {
		t.Fatalf("the PR search must keep its own shape: %q", pr)
	}
}

const issuesListBase = "repos/o/r/issues?state=open&per_page=100&sort=created&direction=asc&page="

func TestIssuesListMatcherIsExact(t *testing.T) {
	cases := []struct {
		name, path string
		want       bool
	}{
		{"page 1", issuesListBase + "1", true},
		{"page 10", issuesListBase + "10", true},
		{"dots and dashes", "repos/a-b/c.d_e/issues?state=open&per_page=100&sort=created&direction=asc&page=2", true},
		{"page 0", issuesListBase + "0", false},
		{"page 11", issuesListBase + "11", false},
		{"page 01", issuesListBase + "01", false},
		{"page 100", issuesListBase + "100", false},
		{"page empty", issuesListBase, false},
		{"page letters", issuesListBase + "x", false},
		{"per_page 99", strings.Replace(issuesListBase, "per_page=100", "per_page=99", 1) + "1", false},
		{"per_page 101", strings.Replace(issuesListBase, "per_page=100", "per_page=101", 1) + "1", false},
		{"state all", strings.Replace(issuesListBase, "state=open", "state=all", 1) + "1", false},
		{"state closed", strings.Replace(issuesListBase, "state=open", "state=closed", 1) + "1", false},
		{"labels", "repos/o/r/issues?state=open&per_page=100&sort=created&direction=asc&labels=&page=1", false},
		{"assignee", issuesListBase + "1&assignee=x", false},
		{"creator", issuesListBase + "1&creator=x", false},
		{"extra leading", "repos/o/r/issues?x=1&state=open&per_page=100&sort=created&direction=asc&page=1", false},
		{"other order", "repos/o/r/issues?per_page=100&state=open&sort=created&direction=asc&page=1", false},
		{"page first", "repos/o/r/issues?page=1&state=open&per_page=100&sort=created&direction=asc", false},
		{"sort updated", strings.Replace(issuesListBase, "sort=created", "sort=updated", 1) + "1", false},
		{"direction desc", strings.Replace(issuesListBase, "direction=asc", "direction=desc", 1) + "1", false},
		{"no query", "repos/o/r/issues", false},
		{"lowercase escape", "repos/o/r%2fx/issues?state=open&per_page=100&sort=created&direction=asc&page=1", false},
		{"encoded slash", "repos/o%2Fr/issues?state=open&per_page=100&sort=created&direction=asc&page=1", false},
		{"encoded segment", "repos/o/%72/issues?state=open&per_page=100&sort=created&direction=asc&page=1", false},
		{"dot segment", "repos/o/../issues?state=open&per_page=100&sort=created&direction=asc&page=1", false},
		{"dot repository", "repos/o/./issues?state=open&per_page=100&sort=created&direction=asc&page=1", false},
		{"owner 101", "repos/" + strings.Repeat("a", 101) + "/r/issues?state=open&per_page=100&sort=created&direction=asc&page=1", false},
		{"trailing slash", "repos/o/r/issues/?state=open&per_page=100&sort=created&direction=asc&page=1", false},
		{"sub path", "repos/o/r/issues/1", false},
		{"newline", issuesListBase + "1\n", false},
		{"uppercase key", strings.Replace(issuesListBase, "state=", "State=", 1) + "1", false},
		{"pulls", "repos/o/r/pulls?state=open&per_page=100&sort=created&direction=asc&page=1", false},
	}
	for _, c := range cases {
		if got := issuesListReadEndpoint(c.path); got != c.want {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
		if c.want {
			if !issueReadEndpoint(c.path) {
				t.Errorf("%s: not an issue read", c.name)
			}
		}
	}
}

func TestUserMatcherIsExact(t *testing.T) {
	for p, want := range map[string]bool{"user": true, "user?": false, "user?x=1": false, "users/x": false, "user/": false, "user/emails": false, "User": false, "user\n": false, "users": false, "": false} {
		if got := issueReadEndpoint(p); got != want {
			t.Errorf("%q: %v want %v", p, got, want)
		}
	}
}

func TestIssueSearchMatcherIsExactAndDistinctFromPRSearch(t *testing.T) {
	issue := func(owner string, page int) string { p, _ := IssueSearchEndpoint(owner, page); return p }
	pr := func(owner string, page int) string { p, _ := SearchEndpoint(owner, page); return p }
	good := issue("wstein", 1)
	for _, c := range []struct {
		name, path string
		want       bool
	}{
		{"shape", good, true},
		{"page 10", issue("wstein", 10), true},
		{"owner 39", issue(strings.Repeat("a", 39), 1), true},
		{"page 11", strings.Replace(good, "page=1&", "page=11&", 1), false},
		{"page 0", strings.Replace(good, "page=1&", "page=0&", 1), false},
		{"page 01", strings.Replace(good, "page=1&", "page=01&", 1), false},
		{"owner 40", strings.Replace(good, "Awstein", "A"+strings.Repeat("a", 40), 1), false},
		{"owner injection", strings.Replace(good, "Awstein", "Ax+is:private", 1), false},
		{"owner encoded", strings.Replace(good, "Awstein", "A%41", 1), false},
		{"owner double hyphen", strings.Replace(good, "Awstein", "Aa--b", 1), false},
		{"lowercase escape", strings.Replace(good, "%3A", "%3a", 1), false},
		{"extra param", good + "&x=1", false},
		{"reordered", "search/issues?per_page=100&q=user%3Awstein+is%3Aissue+is%3Aopen+archived%3Afalse&page=1&sort=created&order=asc", false},
		{"sort updated", strings.Replace(good, "sort=created", "sort=updated", 1), false},
		{"order desc", strings.Replace(good, "order=asc", "order=desc", 1), false},
		{"per_page 50", strings.Replace(good, "per_page=100", "per_page=50", 1), false},
		{"extra term", strings.Replace(good, "+is%3Aopen", "+is%3Aopen+is%3Aprivate", 1), false},
		{"no archived", "search/issues?q=user%3Awstein+is%3Aissue+is%3Aopen&per_page=100&page=1&sort=created&order=asc", false},
		{"pr search", pr("wstein", 1), false},
		{"other search", "search/code?q=user%3Awstein", false},
	} {
		if got := issueSearchReadEndpoint(c.path); got != c.want {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
	}
	if searchReadEndpoint(good) || !searchReadEndpoint(pr("wstein", 1)) {
		t.Fatal("the two search matchers must each match only their own shape")
	}
}
