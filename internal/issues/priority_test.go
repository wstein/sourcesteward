// SPDX-License-Identifier: EUPL-1.2
package issues

import (
	"fmt"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
)

func issue(mods ...func(*hosting.IssueListItem)) hosting.IssueListItem {
	it := hosting.IssueListItem{Number: 1, Title: "t", Author: "alice", AuthorType: "User"}
	for _, m := range mods {
		m(&it)
	}
	return it
}

func labels(l ...string) func(*hosting.IssueListItem) {
	return func(it *hosting.IssueListItem) { it.Labels = l }
}

func assignees(a ...string) func(*hosting.IssueListItem) {
	return func(it *hosting.IssueListItem) { it.Assignees = a }
}

func author(login, kind string) func(*hosting.IssueListItem) {
	return func(it *hosting.IssueListItem) { it.Author, it.AuthorType = login, kind }
}

func TestClassifyRuleTable(t *testing.T) {
	many := make([]string, 99)
	for i := range many {
		many[i] = fmt.Sprintf("l%d", i)
	}
	for _, tc := range []struct {
		name     string
		it       hosting.IssueListItem
		viewer   string
		priority string
		reason   string
	}{
		{"rule 1 security", issue(labels("security")), "", Red, ReasonSecurity},
		{"rule 1 any position", issue(labels("a", "b", "security")), "", Red, ReasonSecurity},
		{"rule 2 assigned", issue(assignees("wstein")), "wstein", Yellow, ReasonAssigned},
		{"rule 3 bot by type", issue(author("renovate", "Bot")), "", Violet, ReasonBot},
		{"rule 3 bot by suffix", issue(author("dependabot[bot]", "User")), "", Violet, ReasonBot},
		{"rule 4 bug", issue(labels("bug")), "", Yellow, ReasonBug},
		{"rule 5 none", issue(labels("docs")), "wstein", Blue, ReasonNone},
		{"rule 5 no labels", issue(), "", Blue, ReasonNone},
		{"ghost is no bot", issue(author(hosting.GhostLogin, "")), "", Blue, ReasonNone},

		{"security beats bot", issue(labels("security"), author("x[bot]", "Bot")), "", Red, ReasonSecurity},
		{"security beats assigned", issue(labels("security"), assignees("wstein")), "wstein", Red, ReasonSecurity},
		{"security beats bug", issue(labels("bug", "security")), "", Red, ReasonSecurity},
		{"assigned beats bot", issue(assignees("wstein"), author("x[bot]", "Bot")), "wstein", Yellow, ReasonAssigned},
		{"assigned beats bug", issue(assignees("wstein"), labels("bug")), "wstein", Yellow, ReasonAssigned},
		{"bot beats bug", issue(labels("bug"), author("x[bot]", "Bot")), "", Violet, ReasonBot},

		{"security upper", issue(labels("SECURITY")), "", Red, ReasonSecurity},
		{"security mixed", issue(labels("Security")), "", Red, ReasonSecurity},
		{"bug mixed", issue(labels("Bug")), "", Yellow, ReasonBug},
		{"assignee case", issue(assignees("WStein")), "wstein", Yellow, ReasonAssigned},
		{"viewer case", issue(assignees("wstein")), "WSTEIN", Yellow, ReasonAssigned},

		{"long s is not s", issue(labels("ſecurity")), "", Blue, ReasonNone},
		{"kelvin is not k", issue(labels("Kug")), "", Blue, ReasonNone},
		{"kelvin in bug", issue(labels("buK")), "", Blue, ReasonNone},
		{"long s in assignee", issue(assignees("ſtein")), "stein", Blue, ReasonNone},

		{"security-review", issue(labels("security-review")), "", Blue, ReasonNone},
		{"area/security", issue(labels("area/security")), "", Blue, ReasonNone},
		{"security with space", issue(labels("security ")), "", Blue, ReasonNone},
		{"security zero width", issue(labels("security​")), "", Blue, ReasonNone},
		{"type: bug", issue(labels("type: bug")), "", Blue, ReasonNone},
		{"bugfix", issue(labels("bugfix")), "", Blue, ReasonNone},

		{"security as the 100th label", issue(labels(append(many, "security")...)), "", Red, ReasonSecurity},
		{"viewer unknown disables only rule 2", issue(assignees("wstein"), labels("bug")), "", Yellow, ReasonBug},
		{"viewer unknown, unmatched empty login", issue(assignees("")), "", Blue, ReasonNone},
		{"other assignee", issue(assignees("bob")), "wstein", Blue, ReasonNone},
	} {
		priority, reason := classify(tc.it, tc.viewer)
		if priority != tc.priority || reason != tc.reason {
			t.Errorf("%s: got %s/%s, want %s/%s", tc.name, priority, reason, tc.priority, tc.reason)
		}
	}
}

func TestAsciiEqualFoldsOnlyASCII(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"bug", "BUG", true}, {"Bug", "bUg", true}, {"bug", "bu", false}, {"bug", "bugs", false},
		{"", "", true}, {"[", "{", false}, {"@", "`", false}, {"ſ", "s", false}, {"K", "k", false},
	} {
		if got := asciiEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("asciiEqual(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}
