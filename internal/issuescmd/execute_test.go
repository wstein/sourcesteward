// SPDX-License-Identifier: EUPL-1.2
package issuescmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/watchcmd"
)

// fakeSource serves canned issue pages and a canned owner search and records every call. The
// filters can only be applied locally: IssuesPage knows nothing but a repository and a page.
type fakeSource struct {
	mu        sync.Mutex
	pages     map[string]string // repo -> page 1 body
	errs      map[string]error
	viewer    string
	viewerErr error
	owners    map[string]hosting.OwnerDiscovery
	calls     []string
}

func newSource() *fakeSource {
	return &fakeSource{pages: map[string]string{}, errs: map[string]error{}, owners: map[string]hosting.OwnerDiscovery{}, viewer: "wstein"}
}

func (s *fakeSource) IssuesPage(_ context.Context, repo string, page int) (hosting.WatchResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, fmt.Sprintf("%s#%d", repo, page))
	if err := s.errs[repo]; err != nil {
		return hosting.WatchResponse{}, err
	}
	body := s.pages[repo]
	if body == "" || page > 1 {
		body = "[]"
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func (s *fakeSource) Viewer(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "user")
	return s.viewer, s.viewerErr
}

func (s *fakeSource) DiscoverIssueOwners(_ context.Context, owners []string) []hosting.OwnerDiscovery {
	var out []hosting.OwnerDiscovery
	for _, o := range owners {
		s.mu.Lock()
		s.calls = append(s.calls, "search/"+o)
		d, ok := s.owners[strings.ToLower(o)]
		s.mu.Unlock()
		if !ok {
			d = hosting.OwnerDiscovery{Owner: o}
		}
		out = append(out, d)
	}
	return out
}

func (s *fakeSource) count(prefix string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// issue is one list entry as GitHub returns it.
func issue(repo string, n int, author, kind string, labels []string, assignees ...string) map[string]any {
	ls, as := []any{}, []any{}
	for _, l := range labels {
		ls = append(ls, map[string]any{"name": l})
	}
	for _, a := range assignees {
		as = append(as, map[string]any{"login": a})
	}
	return map[string]any{"number": n, "title": fmt.Sprintf("issue %d", n), "html_url": fmt.Sprintf("https://github.com/%s/issues/%d", repo, n),
		"created_at": "2026-10-20T09:00:00Z", "comments": 1, "user": map[string]any{"login": author, "type": kind}, "labels": ls, "assignees": as}
}

func page(entries ...map[string]any) string {
	b, _ := json.Marshal(entries)
	return string(b)
}

func sampleSource() *fakeSource {
	s := newSource()
	s.pages["o/a"] = page(
		issue("o/a", 1, "alice", "User", []string{"security"}),
		issue("o/a", 2, "bob", "User", nil, "wstein"),
		issue("o/a", 3, "carol", "User", []string{"docs"}),
		map[string]any{"number": 9, "title": "a pull request", "pull_request": map[string]any{}, "user": map[string]any{"login": "x", "type": "User"}},
	)
	s.pages["o/b"] = page(issue("o/b", 4, "dependabot[bot]", "Bot", []string{"bug"}), issue("o/b", 5, "dave", "User", []string{"BUG"}))
	return s
}

func exec(t *testing.T, src *fakeSource, f Flags, discover func(context.Context, string) (stwsync.Workspace, error)) (string, string, error) {
	t.Helper()
	var out, diag bytes.Buffer
	p, err := prepare(context.Background(), f, &out, discover)
	if err != nil {
		t.Fatal(err)
	}
	p.Diag, p.Now = &diag, func() time.Time { return t0 }
	p.Open = func(context.Context, string, string) (Source, func(), error) { return src, func() {}, nil }
	err = Execute(context.Background(), p)
	return out.String(), diag.String(), err
}

func numbersOf(m map[string]any, priority string) []int {
	var out []int
	for _, r := range m["issues"].(map[string]any)[priority].([]any) {
		out = append(out, int(r.(map[string]any)["number"].(float64)))
	}
	return out
}

func TestExecuteJSONDocumentIsOneSchemaValidLine(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/b", "o/a"}, true
	src := sampleSource()
	out, diag, err := exec(t, src, f, nil)
	if err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	m := decode(t, out)
	if fmt.Sprint(numbersOf(m, "red")) != "[1]" || fmt.Sprint(numbersOf(m, "yellow")) != "[2 5]" || fmt.Sprint(numbersOf(m, "blue")) != "[3]" || fmt.Sprint(numbersOf(m, "violet")) != "[4]" {
		t.Fatalf("%s", out)
	}
	if s := m["summary"].(map[string]any); s["total"] != 5.0 || s["bots"] != 1.0 || s["repos_read"] != 2.0 {
		t.Fatalf("%v", s)
	}
	if r := m["repos"].([]any)[0].(map[string]any); r["repo"] != "o/a" || r["open"] != 3.0 || r["pulls_dropped"] != 1.0 {
		t.Fatalf("%v", r)
	}
	if src.count("user") != 1 {
		t.Fatalf("one user read per run: %v", src.calls)
	}
}

func TestExecuteFiltersAreLocalAndNeverReachTheReader(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON, f.Labels, f.Assignees, f.Authors = []string{"o/a", "o/b"}, true, []string{"bug", "security"}, []string{"@none"}, []string{"alice", "dave"}
	src := sampleSource()
	out, _, err := exec(t, src, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	if fmt.Sprint(numbersOf(m, "red")) != "[1]" || fmt.Sprint(numbersOf(m, "yellow")) != "[5]" || m["summary"].(map[string]any)["total"] != 2.0 {
		t.Fatalf("%s", out)
	}
	for _, c := range src.calls {
		if c != "user" && c != "o/a#1" && c != "o/b#1" {
			t.Errorf("unexpected read %s", c)
		}
	}
	if o := m["options"].(map[string]any); fmt.Sprint(o["labels"]) != "[bug security]" || fmt.Sprint(o["assignees"]) != "[@none]" {
		t.Fatalf("%v", o)
	}
}

func TestExecuteLimitCapsTheRowsAndCountsTheRest(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON, f.Limit = []string{"o/a", "o/b"}, true, 2
	out, _, err := exec(t, sampleSource(), f, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	s := m["summary"].(map[string]any)
	if fmt.Sprint(numbersOf(m, "red")) != "[1]" || fmt.Sprint(numbersOf(m, "yellow")) != "[2]" || s["omitted"] != 3.0 || s["counts"].(map[string]any)["violet"] != 1.0 {
		t.Fatalf("%s", out)
	}
}

func TestExecuteHumanOutput(t *testing.T) {
	f := baseFlags()
	f.Repos, f.Human = []string{"o/a"}, true
	out, _, err := exec(t, sampleSource(), f, nil)
	if err != nil || !strings.HasPrefix(out, "RED (1)\no/a#1  ") || !strings.Contains(out, "  run: gh issue view 1 -R o/a\n") || !strings.HasSuffix(out, "3 open issues in 1 repository, 1 red, 1 yellow\n") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestExecutePartialFailureIsExitOneWithOutput(t *testing.T) {
	src := sampleSource()
	src.errs["o/b"] = &hosting.WatchError{Code: hosting.CodeForbidden, Message: "token\nlacks access"}
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a", "o/b"}, true
	out, diag, err := exec(t, src, f, nil)
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) || fail.Code != watchcmd.ExitFailure || !strings.Contains(err.Error(), "1 repository could not be read") {
		t.Fatalf("%v", err)
	}
	m := decode(t, out)
	errs := m["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["code"] != "forbidden" || errs[0].(map[string]any)["repo"] != "o/b" {
		t.Fatalf("%v", errs)
	}
	if fmt.Sprint(numbersOf(m, "red")) != "[1]" || len(numbersOf(m, "violet")) != 0 {
		t.Fatalf("rows of the failed repository must be gone: %s", out)
	}
	if !strings.Contains(diag, "o/b") || !strings.Contains(diag, "forbidden") || strings.Contains(diag, "\n\n") || strings.Count(diag, "\n") != 1 {
		t.Fatalf("stderr %q", diag)
	}
}

func TestExecuteTheViewerOutcomes(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a"}, true
	// 403 or 404 without rate-limit evidence: a skip, exit 0, the assigned rule is off.
	src := sampleSource()
	src.viewerErr = &hosting.WatchError{Code: hosting.CodeForbidden, Message: "m"}
	out, diag, err := exec(t, src, f, nil)
	m := decode(t, out)
	if err != nil || m["viewer"].(map[string]any)["reason"] != "no-identity" || fmt.Sprint(numbersOf(m, "yellow")) != "[]" {
		t.Fatalf("%v\n%s", err, out)
	}
	if want := "notice no-identity: the token cannot read the authenticated user: issues assigned to you are not flagged\n"; diag != want || strings.Contains(out, "notice") {
		t.Fatalf("stderr %q, stdout %q", diag, out)
	}
	f.JSON, f.Human = false, true
	out, diag, err = exec(t, src, f, nil)
	if err != nil || !strings.Contains(diag, "notice no-identity:") || strings.Contains(out, "notice") {
		t.Fatalf("%v %q %q", err, diag, out)
	}
	f.JSON, f.Human = true, false
	// any other failure: exit 1, the issues are still listed.
	src = sampleSource()
	src.viewerErr = &hosting.WatchError{Code: hosting.CodeHTTP5xx, Message: "m"}
	out, diag, err = exec(t, src, f, nil)
	var fail *watchcmd.Failure
	m = decode(t, out)
	if !errors.As(err, &fail) || m["viewer"].(map[string]any)["error"] != "http-5xx" || fmt.Sprint(numbersOf(m, "red")) != "[1]" || !strings.Contains(diag, "http-5xx") {
		t.Fatalf("%v\n%s\n%s", err, out, diag)
	}
	if !strings.Contains(diag, "notice viewer-unknown: the authenticated user could not be read (http-5xx): issues assigned to you are not flagged\n") || strings.Contains(out, "notice") {
		t.Fatalf("stderr %q", diag)
	}
	// --assignee @me without a login: exit 1, no rows, nothing read.
	src = sampleSource()
	src.viewerErr = &hosting.WatchError{Code: hosting.CodeForbidden, Message: "m"}
	f.Assignees = []string{"@me"}
	out, diag, err = exec(t, src, f, nil)
	m = decode(t, out)
	if !errors.As(err, &fail) || m["summary"].(map[string]any)["total"] != 0.0 || src.count("o/a") != 0 || !strings.Contains(diag, "viewer-unknown") {
		t.Fatalf("%v\n%s\n%s", err, out, diag)
	}
	if r := m["repos"].([]any)[0].(map[string]any); r["state"] != "skipped" || r["reason"] != "viewer-unknown" {
		t.Fatalf("%v", r)
	}
}

func TestExecuteOwnerModeMergesDedupesAndLabelsTheSource(t *testing.T) {
	src := sampleSource()
	src.pages["wstein/lab"] = page(issue("wstein/lab", 7, "erin", "User", nil))
	src.owners["wstein"] = hosting.OwnerDiscovery{Owner: "wstein", Repos: []string{"wstein/lab", "O/A"}}
	f := baseFlags()
	f.Repos, f.Owners, f.JSON = []string{"o/a"}, []string{"wstein"}, true
	out, _, err := exec(t, src, f, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := decode(t, out)
	repos := m["repos"].([]any)
	if len(repos) != 2 || repos[0].(map[string]any)["source"] != "repo" || repos[1].(map[string]any)["repo"] != "wstein/lab" || repos[1].(map[string]any)["source"] != "owner" || m["options"].(map[string]any)["scope"] != "mixed" {
		t.Fatalf("%v", repos)
	}
	if src.count("search/") != 1 || src.count("o/a#") != 1 {
		t.Fatalf("%v", src.calls)
	}
}

func TestExecuteOwnerFailuresAreErrorsAndExitOne(t *testing.T) {
	src := sampleSource()
	src.owners["wstein"] = hosting.OwnerDiscovery{Owner: "wstein", Repos: []string{"wstein/lab"}, Code: hosting.CodeSearchIncomplete, Message: "incomplete"}
	src.pages["wstein/lab"] = page(issue("wstein/lab", 7, "erin", "User", nil))
	f := baseFlags()
	f.Repos, f.Owners, f.JSON = []string{"o/a"}, []string{"wstein"}, true
	out, diag, err := exec(t, src, f, nil)
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) || !strings.Contains(err.Error(), "1 owner problem") {
		t.Fatalf("%v", err)
	}
	m := decode(t, out)
	if e := m["errors"].([]any)[0].(map[string]any); e["owner"] != "wstein" || e["repo"] != nil || e["code"] != "search-incomplete" {
		t.Fatalf("%v", e)
	}
	if m["summary"].(map[string]any)["repos_read"] != 2.0 || !strings.Contains(diag, "owner wstein") {
		t.Fatalf("the repositories seen are still read: %s %q", out, diag)
	}
}

func TestExecuteOwnerResultsCountAgainstTheRepositoryCap(t *testing.T) {
	src := newSource()
	var repos []string
	for i := 0; i < 256; i++ {
		repos = append(repos, fmt.Sprintf("w/r%d", i))
	}
	src.owners["x"] = hosting.OwnerDiscovery{Owner: "x", Repos: []string{"x/extra"}}
	f := baseFlags()
	f.Repos, f.Owners, f.JSON = repos, []string{"x"}, true
	out, _, err := exec(t, src, f, nil)
	if !errors.Is(err, watchcmd.ErrUsage) || out != "" || src.count("w/") != 0 {
		t.Fatalf("%v %q %v", err, out, src.calls[:min(3, len(src.calls))])
	}
}

func TestExecuteReaderFailureIsExitOne(t *testing.T) {
	var out bytes.Buffer
	f := baseFlags()
	f.Repos = []string{"o/a"}
	p, err := prepare(context.Background(), f, &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.Open = func(context.Context, string, string) (Source, func(), error) {
		return nil, func() {}, errors.New("gh is missing")
	}
	err = Execute(context.Background(), p)
	var fail *watchcmd.Failure
	if !errors.As(err, &fail) || fail.Code != 1 || out.Len() != 0 || !strings.Contains(err.Error(), "gh is missing") {
		t.Fatalf("%v", err)
	}
}

func TestExecuteReportsSkippedMembersOnStderr(t *testing.T) {
	f := baseFlags()
	f.JSON = true
	discover := ws("/w", member("s", "https://github.com/wstein/s.git"), stwsync.Member{Name: "cold"}, member("lab", "https://gitlab.com/a/b.git"))
	_, diag, err := exec(t, newSource(), f, discover)
	if err != nil || !strings.Contains(diag, "cold") || !strings.Contains(diag, "lab") {
		t.Fatalf("%v %q", err, diag)
	}
}

func TestExecuteNothingOpenIsExitZero(t *testing.T) {
	f := baseFlags()
	f.Repos, f.Human = []string{"o/empty"}, true
	out, _, err := exec(t, newSource(), f, nil)
	if err != nil || out != "0 open issues in 1 repository, 0 red, 0 yellow\n" {
		t.Fatalf("%v %q", err, out)
	}
}

func TestExecuteDoesNotRequestTheViewerWithoutARepository(t *testing.T) {
	src := newSource()
	f := baseFlags()
	f.Owners, f.JSON = []string{"wstein"}, true
	out, _, err := exec(t, src, f, nil)
	m := decode(t, out)
	if err != nil || src.count("user") != 0 || m["viewer"].(map[string]any)["reason"] != "not-github-origin" {
		t.Fatalf("%v %s %v", err, out, src.calls)
	}
}

func TestExecuteGivesNoViewerNoticeWhenTheViewerIsKnownOrNeverAsked(t *testing.T) {
	f := baseFlags()
	f.Repos, f.JSON = []string{"o/a"}, true
	if _, diag, err := exec(t, sampleSource(), f, nil); err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
	f.Repos, f.Owners = nil, []string{"wstein"}
	if _, diag, err := exec(t, newSource(), f, nil); err != nil || diag != "" {
		t.Fatalf("%v %q", err, diag)
	}
}
