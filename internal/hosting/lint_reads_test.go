// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"strings"
	"testing"
)

func lintComments(n, page int) string {
	ep, _ := LintCommentsEndpoint("o/r", n, page)
	return ep
}

// Spec 050: the lint guard admits the one-shot set, the commit pages and the comment pages of one pull
// request; every other mode refuses the comment shape, and the lint guard refuses the watch set.
func TestGuardLintShapes(t *testing.T) {
	modes := []struct {
		name string
		mode guardMode
	}{{"oneShot", guardOneShot}, {"watch", guardWatch}, {"pulls", guardPulls}, {"status", guardStatus}, {"issues", guardIssues}, {"lint", guardLint}}
	cases := []struct {
		path string
		want [6]bool
	}{
		{lintComments(7, 1), [6]bool{false, false, false, false, false, true}},
		{lintComments(7, 10), [6]bool{false, false, false, false, false, true}},
		{"repos/o/r/pulls/7/commits?per_page=100&page=3", [6]bool{false, true, true, true, false, true}},
		{"repos/o/r/pulls/7", [6]bool{true, true, true, true, true, true}},
		{"repos/o/r/pulls/7/commits?per_page=100&page=4", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/pulls?state=open&per_page=25&page=1", [6]bool{false, true, true, true, false, false}},
		{"rate_limit", [6]bool{false, true, true, true, false, false}},
		{"repos/o/r/branches/main", [6]bool{false, true, true, true, false, false}},
		{lintComments(7, 1) + "&x=1", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/issues/7/comments?per_page=30&page=0", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/issues/7/comments?per_page=30&page=11", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/issues/7/comments?per_page=100&page=1", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/issues/07/comments?per_page=30&page=1", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/issues/7/comments?page=1&per_page=30", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/issues/7/comments", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/issues/7", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/pulls/7/comments?per_page=30&page=1", [6]bool{false, false, false, false, false, false}},
		{"repos/o/r/issues/7%2F8/comments?per_page=30&page=1", [6]bool{false, false, false, false, false, false}},
		{"user", [6]bool{false, false, false, true, true, false}},
	}
	for _, c := range cases {
		for i, m := range modes {
			if got := guardAllows(m.mode, "api.github.com", "/", "https://api.github.com/"+c.path); got != c.want[i] {
				t.Errorf("%s %s: allowed=%v want %v", m.name, c.path, got, c.want[i])
			}
		}
	}
}

func TestLintCommentsEndpointRefusesBadInput(t *testing.T) {
	for _, repo := range []string{"", "o", "o/r/x", "o/.."} {
		if _, err := LintCommentsEndpoint(repo, 1, 1); err == nil {
			t.Errorf("repo %q accepted", repo)
		}
	}
	for _, c := range [][2]int{{0, 1}, {-1, 1}, {1, 0}, {1, 11}} {
		if _, err := LintCommentsEndpoint("o/r", c[0], c[1]); err == nil {
			t.Errorf("number %d page %d accepted", c[0], c[1])
		}
	}
}

func TestLintReaderReadsAndOtherReadersRefuse(t *testing.T) {
	f := newFakeForge(t)
	f.serve(lintComments(7, 2), "", `[{"id":1}]`, map[string]string{"Link": `<https://evil.example/x?page=3>; rel="next"`})
	f.serve("repos/o/r/pulls/7/commits?per_page=100&page=1", "", `[]`, nil)
	f.serve("repos/o/r/pulls/7", "", `{}`, nil)
	r := readerForMode(f, guardLint)
	res, err := r.LintComments(context.Background(), "o/r", 7, 2)
	if err != nil || string(res.Body) != `[{"id":1}]` || !strings.Contains(res.Link, "next") {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := r.PullCommits(context.Background(), "o/r", 7, 1, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.PullDetail(context.Background(), "o/r", 7, ""); err != nil {
		t.Fatal(err)
	}
	if got := r.TakeCalls()[KindLintComments].Requests; got != 1 {
		t.Fatalf("comment calls %d", got)
	}
	// The watch list is not a lint read, whatever the caller asks.
	if _, err := r.PullListPage(context.Background(), "o/r", 25, 1, ""); err == nil {
		t.Error("lint reader listed pull requests")
	}
	f.log = nil
	for _, mode := range []guardMode{guardOneShot, guardWatch, guardPulls, guardStatus, guardIssues} {
		_, err := readerForMode(f, mode).LintComments(context.Background(), "o/r", 7, 2)
		assertWatchCode(t, err, CodeMalformed)
		// Defence in depth: a reader that claims lint mode over another guard is stopped by that guard.
		hostile := readerForMode(f, mode)
		hostile.mode = guardLint
		if _, err := hostile.LintComments(context.Background(), "o/r", 7, 2); err == nil {
			t.Errorf("guard %d let the comment page through", mode)
		}
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
}
