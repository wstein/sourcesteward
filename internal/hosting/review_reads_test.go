// SPDX-License-Identifier: EUPL-1.2
package hosting

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const closedList = "repos/o/r/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=1"

func commentsURI(n, page int) string {
	return fmt.Sprintf("repos/o/r/issues/%d/comments?per_page=50&page=%d", n, page)
}

// Spec 046: the two review shapes are admitted by the pulls and status guards only.
func TestGuardAdmitsReviewShapesInPullsAndStatusOnly(t *testing.T) {
	cases := []struct {
		path        string
		pulls, stat bool
	}{
		{commentsURI(7, 1), true, true},
		{commentsURI(7, 999), true, true},
		{commentsURI(123456, 12), true, true},
		{closedList, true, true},
		{commentsURI(7, 0), false, false},
		{commentsURI(7, 1000), false, false},
		{commentsURI(0, 1), false, false},
		{"repos/o/r/issues/07/comments?per_page=50&page=1", false, false},
		{"repos/o/r/issues/7/comments?per_page=100&page=1", false, false},
		{"repos/o/r/issues/7/comments?per_page=50&page=1&x=1", false, false},
		{"repos/o/r/issues/7/comments?page=1&per_page=50", false, false},
		{"repos/o/r/issues/7/comments", false, false},
		{"repos/o/r/issues/7/comments/9", false, false},
		{"repos/o/r/issues/7", false, false},
		{"repos/o/r/issues/7%2F8/comments?per_page=50&page=1", false, false},
		{"repos/o/r/issues/-7/comments?per_page=50&page=1", false, false},
		{"repos/o/r/pulls?state=closed&sort=updated&per_page=100&page=1", false, false},
		{"repos/o/r/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=2", false, false},
		{"repos/o/r/pulls?state=closed&sort=updated&direction=asc&per_page=100&page=1", false, false},
		{"repos/o/r/pulls?state=all&sort=updated&direction=desc&per_page=100&page=1", false, false},
		{"repos/o/r/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=1&head=x", false, false},
		{"repos/o/r/pulls/7/comments?per_page=50&page=1", false, false},
	}
	modes := []struct {
		name string
		mode guardMode
	}{{"oneShot", guardOneShot}, {"watch", guardWatch}, {"pulls", guardPulls}, {"status", guardStatus}, {"issues", guardIssues}, {"land", guardLand}}
	for _, c := range cases {
		for _, m := range modes {
			want := (m.mode == guardPulls && c.pulls) || (m.mode == guardStatus && c.stat)
			g := guardedTransport{next: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, `[]`), nil }), host: "api.github.com", prefix: "/", mode: m.mode}
			req, err := http.NewRequest("GET", "https://api.github.com/"+c.path, nil)
			if err != nil {
				continue
			}
			if got := g.allowed(req); got != want {
				t.Errorf("%s %s: allowed=%v want %v", m.name, c.path, got, want)
			}
		}
	}
}

func TestReviewEndpointGeneratorsRefuseBadInput(t *testing.T) {
	for _, c := range []struct {
		repo    string
		n, page int
	}{{"o/r", 0, 1}, {"o/r", -1, 1}, {"o/r", 7, 0}, {"o/r", 7, 1000}, {"bad", 7, 1}, {"o/r/x", 7, 1}} {
		if _, err := IssueCommentsEndpoint(c.repo, c.n, c.page); err == nil {
			t.Errorf("%+v accepted", c)
		}
	}
	if _, err := ClosedPullsEndpoint("bad"); err == nil {
		t.Error("bad repo accepted")
	}
	if ep, err := IssueCommentsEndpoint("o/r", 7, 3); err != nil || ep != commentsURI(7, 3) {
		t.Errorf("%q %v", ep, err)
	}
}

func TestReviewReadsAreRefusedBeforeAnyRequestInOtherModes(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[commentsURI(7, 1)] = func(*http.Request) *http.Response { return response(200, `[]`) }
	f.handlers[closedList] = func(*http.Request) *http.Response { return response(200, `[]`) }
	for _, mode := range []guardMode{guardWatch, guardIssues, guardOneShot} {
		r := readerForMode(f, mode)
		if _, err := r.IssueComments(context.Background(), "o/r", 7, 1); err == nil {
			t.Errorf("mode %d read comments", mode)
		}
		if _, err := r.ClosedPulls(context.Background(), "o/r"); err == nil {
			t.Errorf("mode %d read the closed list", mode)
		}
	}
	if len(f.log) != 0 {
		t.Fatalf("sent %v", f.log)
	}
	for _, mode := range []guardMode{guardPulls, guardStatus} {
		r := readerForMode(f, mode)
		if _, err := r.IssueComments(context.Background(), "o/r", 7, 1); err != nil {
			t.Errorf("mode %d: %v", mode, err)
		}
		if _, err := r.ClosedPulls(context.Background(), "o/r"); err != nil {
			t.Errorf("mode %d: %v", mode, err)
		}
	}
}

func TestParseIssueComments(t *testing.T) {
	body := `[{"id":11,"html_url":"https://github.com/o/r/pull/7#issuecomment-11","created_at":"2026-11-01T09:00:00Z","updated_at":"2026-11-01T09:30:00Z","body":"Review: CLEAR x","user":{"login":"werner"},"author_association":"OWNER"},
	{"id":12,"html_url":"javascript:alert(1)","created_at":"2026-11-01T10:00:00Z","updated_at":"2026-11-01T10:00:00Z","body":null,"user":null}]`
	got, err := ParseIssueComments([]byte(body))
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].ID != 11 || got[0].Body != "Review: CLEAR x" || got[0].URL != "https://github.com/o/r/pull/7#issuecomment-11" ||
		!got[0].CreatedAt.Equal(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)) || !got[0].UpdatedAt.Equal(time.Date(2026, 11, 1, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("%+v", got[0])
	}
	if got[1].URL != "" || got[1].Body != "" {
		t.Fatalf("a non-https URL and a null body must be empty: %+v", got[1])
	}
	for _, bad := range []string{`{}`, `[{"id":0,"created_at":"2026-11-01T09:00:00Z"}]`, `[{"id":1,"created_at":"yesterday"}]`, `[{"id":1}]`, `nope`} {
		if _, err := ParseIssueComments([]byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestParseIssueCommentsAssociation(t *testing.T) {
	page := func(assoc string) string {
		return `[{"id":1,"created_at":"2026-11-01T09:00:00Z","body":"x"` + assoc + `}]`
	}
	for _, c := range []struct{ in, want string }{
		{`,"author_association":"OWNER"`, "OWNER"},
		{`,"author_association":"MEMBER"`, "MEMBER"},
		{`,"author_association":"COLLABORATOR"`, "COLLABORATOR"},
		{`,"author_association":"CONTRIBUTOR"`, "CONTRIBUTOR"},
		{`,"author_association":"FIRST_TIME_CONTRIBUTOR"`, "FIRST_TIME_CONTRIBUTOR"},
		{`,"author_association":"NONE"`, "NONE"},
		{`,"author_association":"owner"`, ""},
		{`,"author_association":"OWNER "`, ""},
		{`,"author_association":"OWNER1"`, ""},
		{`,"author_association":"OWN-ER"`, ""},
		{`,"author_association":"` + strings.Repeat("A", 40) + `"`, ""},
		{`,"author_association":null`, ""},
		{`,"author_association":7`, ""},
		{``, ""},
	} {
		got, err := ParseIssueComments([]byte(page(c.in)))
		if err != nil || got[0].Association != c.want {
			t.Errorf("%s: association %q (want %q), %v", c.in, got[0].Association, c.want, err)
		}
	}
}

func TestParseIssueCommentsCutsLongBodies(t *testing.T) {
	long := strings.Repeat("x", 100000)
	got, err := ParseIssueComments([]byte(`[{"id":1,"created_at":"2026-11-01T09:00:00Z","updated_at":"2026-11-01T09:00:00Z","body":"` + long + `"}]`))
	if err != nil || len(got[0].Body) > 4096 {
		t.Fatalf("body kept %d bytes, %v", len(got[0].Body), err)
	}
}

func TestParseClosedPulls(t *testing.T) {
	body := `[{"number":5,"merged_at":"2026-10-30T10:00:00Z","head":{"ref":"codex/parent","repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"o/r"}}},
	{"number":6,"merged_at":null,"head":{"ref":"codex/other","repo":{"full_name":"o/r"}},"base":{"repo":{"full_name":"o/r"}}},
	{"number":8,"merged_at":"2026-10-30T10:00:00Z","head":{"ref":"codex/fork","repo":{"full_name":"x/r"}},"base":{"repo":{"full_name":"o/r"}}},
	{"number":9,"merged_at":"2026-10-30T10:00:00Z","head":{"ref":"codex/gone","repo":null},"base":{"repo":{"full_name":"o/r"}}}]`
	got, err := ParseClosedPulls([]byte(body))
	if err != nil || len(got) != 4 {
		t.Fatalf("%+v %v", got, err)
	}
	if !got[0].Merged || got[0].HeadRef != "codex/parent" || got[0].Fork {
		t.Fatalf("%+v", got[0])
	}
	if got[1].Merged || !got[2].Fork || !got[3].Fork {
		t.Fatalf("closed without merge, fork and missing head repository: %+v", got[1:])
	}
	if _, err := ParseClosedPulls([]byte(`{}`)); err == nil {
		t.Fatal("object accepted")
	}
}

func TestParseCommitDate(t *testing.T) {
	d, err := ParseCommitDate([]byte(`{"sha":"x","commit":{"committer":{"date":"2026-11-01T08:00:00Z"},"author":{"date":"2020-01-01T00:00:00Z"}}}`))
	if err != nil || !d.Equal(time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("the committer date is the head time: %v %v", d, err)
	}
	for _, bad := range []string{`{}`, `{"commit":{"committer":{"date":"no"}}}`, `{"commit":{"committer":null}}`, `[]`} {
		if _, err := ParseCommitDate([]byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func comment(id int, min int, body string) string {
	at := time.Date(2026, 11, 1, 9, min, 0, 0, time.UTC).Format(time.RFC3339)
	return fmt.Sprintf(`{"id":%d,"created_at":%q,"updated_at":%q,"body":%q,"html_url":"https://github.com/o/r/pull/7#issuecomment-%d","user":{"login":"w"}}`, id, at, at, body, id)
}

func TestReadIssueCommentsFollowsLinks(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[commentsURI(7, 1)] = func(*http.Request) *http.Response {
		return forgeResponse(200, "["+comment(1, 0, "a")+"]", map[string]string{"Link": `<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=3>; rel="last"`})
	}
	f.handlers[commentsURI(7, 2)] = func(*http.Request) *http.Response {
		return forgeResponse(200, "["+comment(2, 1, "b")+"]", map[string]string{"Link": `<https://api.github.com/x?page=3>; rel="next", <https://api.github.com/x?page=3>; rel="last"`})
	}
	f.handlers[commentsURI(7, 3)] = func(*http.Request) *http.Response { return forgeResponse(200, "["+comment(3, 2, "c")+"]", nil) }
	cs, truncated, err := readerForMode(f, guardPulls).ReadIssueComments(context.Background(), "o/r", 7)
	if err != nil || truncated || len(cs) != 3 || cs[2].ID != 3 {
		t.Fatalf("%+v %v %v", cs, truncated, err)
	}
	if got := len(f.log); got != 3 {
		t.Fatalf("requests %v", f.log)
	}
}

func TestReadIssueCommentsLongThreadReadsFirstAndLastPages(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[commentsURI(7, 1)] = func(*http.Request) *http.Response {
		return forgeResponse(200, "["+comment(1, 0, "first")+"]", map[string]string{"Link": `<https://api.github.com/x?page=2>; rel="next", <https://api.github.com/x?page=20>; rel="last"`})
	}
	for p := 15; p <= 20; p++ {
		p := p
		f.handlers[commentsURI(7, p)] = func(*http.Request) *http.Response { return forgeResponse(200, "["+comment(p, p, "page")+"]", nil) }
	}
	cs, truncated, err := readerForMode(f, guardStatus).ReadIssueComments(context.Background(), "o/r", 7)
	if err != nil || !truncated {
		t.Fatalf("%v %v", truncated, err)
	}
	if len(f.log) != 6 || f.log[0] != commentsURI(7, 1) || f.log[1] != commentsURI(7, 16) || f.log[5] != commentsURI(7, 20) {
		t.Fatalf("the first page and the last five pages are read: %v", f.log)
	}
	if len(cs) != 6 || cs[0].ID != 1 || cs[5].ID != 20 {
		t.Fatalf("%+v", cs)
	}
}

func TestReadIssueCommentsErrors(t *testing.T) {
	f := newFakeForge(t)
	f.handlers[commentsURI(7, 1)] = func(*http.Request) *http.Response { return forgeResponse(200, `[`, nil) }
	if _, _, err := readerForMode(f, guardPulls).ReadIssueComments(context.Background(), "o/r", 7); err == nil {
		t.Fatal("malformed body accepted")
	}
	f.handlers[commentsURI(8, 1)] = func(*http.Request) *http.Response {
		return forgeResponse(200, "["+comment(1, 0, "a")+"]", map[string]string{"Link": `<https://api.github.com/x?page=3>; rel="next"`})
	}
	f.handlers[commentsURI(8, 2)] = func(*http.Request) *http.Response { return forgeResponse(200, `[]`, nil) }
	if _, _, err := readerForMode(f, guardPulls).ReadIssueComments(context.Background(), "o/r", 8); err == nil {
		t.Fatal("a next link that skips page 2 must be refused")
	}
	f.handlers[commentsURI(10, 1)] = func(*http.Request) *http.Response {
		return forgeResponse(200, "["+comment(1, 0, "a")+"]", map[string]string{"Link": `<https://api.github.com/x?page=2>; rel="next"`})
	}
	f.handlers[commentsURI(10, 2)] = func(*http.Request) *http.Response { return forgeResponse(200, "["+comment(2, 1, "b")+"]", nil) }
	if cs, tr, err := readerForMode(f, guardPulls).ReadIssueComments(context.Background(), "o/r", 10); err != nil || tr || len(cs) != 2 {
		t.Fatalf("without a last link the pages are followed one by one: %v %v %v", cs, tr, err)
	}
	if _, _, err := readerForMode(f, guardPulls).ReadIssueComments(context.Background(), "o/r", 9); err == nil {
		t.Fatal("404 accepted")
	}
}
