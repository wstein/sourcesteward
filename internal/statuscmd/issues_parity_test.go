// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/issuescmd"
	"github.com/wstein/sourcesteward/internal/overview"
)

// issuesSource lets the forge of the status tests answer `stw issues` too: the same pages, the same user.
type issuesSource struct{ *forgeFake }

func (issuesSource) DiscoverIssueOwners(context.Context, []string) []hosting.OwnerDiscovery {
	return nil
}

func issueEntry(number int, author, kind string, labels, assignees []string) string {
	named := func(key string, names []string) string {
		var parts []string
		for _, n := range names {
			parts = append(parts, fmt.Sprintf(`{%q:%q}`, key, n))
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return fmt.Sprintf(`{"number":%d,"title":"issue %d","html_url":"https://github.com/acme/root/issues/%d","created_at":"2026-10-20T09:00:00Z","comments":0,`+
		`"user":{"login":%q,"type":%q},"labels":%s,"assignees":%s}`, number, number, number, author, kind, named("name", labels), named("login", assignees))
}

// `stw status` and `stw issues` read the same issues with one cycle: for one forge the open count, the counts per
// priority and the issues that need action are the same in both documents, whatever the filters of the status
// (it has none) and the cap of the command (it is far above this fixture).
func TestStatusAndIssuesAgreeOnWhatTheyRead(t *testing.T) {
	page := "[" + strings.Join([]string{
		issueEntry(1, "alice", "User", []string{"security"}, nil),
		issueEntry(2, "bob", "User", nil, []string{"WStein"}),
		issueEntry(3, "carol", "User", []string{"Bug"}, nil),
		issueEntry(4, "carol", "User", []string{"docs"}, nil),
		issueEntry(5, "dependabot[bot]", "Bot", []string{"bug"}, nil),
		issueEntry(6, "dave", "User", []string{"security", "bug"}, []string{"wstein"}),
	}, ",") + "]"
	forge := &forgeFake{issuePage: page}
	rep, _ := runWith(t, forge, nil, nil)
	root := rep.Remote.Repos[0]
	if root.Issues == nil {
		t.Fatalf("%+v", root.Remote.Sources)
	}

	var out bytes.Buffer
	err := issuescmd.Execute(t.Context(), issuescmd.Params{
		Host: "github.com", Repos: []string{"acme/root"}, Sources: map[string]string{"acme/root": "repo"}, Scope: "repo", Out: &out,
		Options: issues.Options{Limit: 200},
		Open: func(context.Context, string, string) (issuescmd.Source, func(), error) {
			return issuesSource{forge}, func() {}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc issuescmd.Document
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	c := doc.Summary.Counts
	if got := (issues.Counts{Red: c.Red, Yellow: c.Yellow, Blue: c.Blue, Violet: c.Violet}); got != root.Issues.Counts {
		t.Errorf("counts: status %+v, issues %+v", root.Issues.Counts, got)
	}
	if doc.Repos[0].Open == nil || *doc.Repos[0].Open != root.Issues.Open {
		t.Errorf("open: status %d, issues %v", root.Issues.Open, doc.Repos[0].Open)
	}
	if root.Issues.Counts != (issues.Counts{Red: 2, Yellow: 2, Blue: 1, Violet: 1}) || rep.Remote.Viewer.Outcome != overview.OutcomeOK {
		t.Errorf("the fixture is not what the test thinks: %+v", root.Issues.Counts)
	}
	var want, got []string
	for _, r := range append(doc.Issues.Red, doc.Issues.Yellow...) {
		want = append(want, fmt.Sprintf("%s:%d:%s", r.Priority, r.Number, r.Reason))
	}
	for _, it := range rep.Overview.Items {
		if it.Subject.Kind == overview.KindIssue {
			got = append(got, fmt.Sprintf("%s:%d:%s", map[overview.Priority]string{overview.Red: "red", overview.Yellow: "yellow"}[it.Priority], it.Subject.Number, it.Evidence["reason"]))
		}
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		t.Errorf("items: status %v, issues %v", got, want)
	}
	// one `user` read and one list read per run in each command
	if n := countCalls(forge, "user"); n != 2 {
		t.Errorf("%d user reads for two commands", n)
	}
}

func countCalls(f *forgeFake, call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == call {
			n++
		}
	}
	return n
}
