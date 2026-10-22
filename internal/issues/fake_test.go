// SPDX-License-Identifier: EUPL-1.2
package issues

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
)

var t0 = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// fakeForge serves canned issue list pages and the viewer and records every call. A page
// that has a successor carries a Link header with rel="next" to another host, as GitHub's can
// be hostile; the cycle must ask by page number only.
type fakeForge struct {
	mu        sync.Mutex
	pages     map[string][]string // repo -> page bodies
	errAt     map[string]error    // "repo#page"
	viewer    string
	viewerErr error
	calls     []string
	before    func(repo string, page int) // runs before a page is served, outside the lock
}

func newForge() *fakeForge {
	return &fakeForge{pages: map[string][]string{}, errAt: map[string]error{}, viewer: "wstein"}
}

func (f *fakeForge) IssuesPage(_ context.Context, repo string, page int) (hosting.WatchResponse, error) {
	if f.before != nil {
		f.before(repo, page)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("%s#%d", repo, page))
	if err := f.errAt[fmt.Sprintf("%s#%d", repo, page)]; err != nil {
		return hosting.WatchResponse{}, err
	}
	bodies := f.pages[repo]
	res := hosting.WatchResponse{Status: 200, Body: []byte("[]")}
	if page >= 1 && page <= len(bodies) {
		res.Body = []byte(bodies[page-1])
	}
	if page < len(bodies) {
		res.Link = fmt.Sprintf(`<https://evil.example/repos/x/issues?page=%d>; rel="next"`, page+1)
	}
	return res, nil
}

func (f *fakeForge) Viewer(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "user")
	return f.viewer, f.viewerErr
}

func (f *fakeForge) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeForge) callList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type entry map[string]any

// raw builds a list entry; mods change its fields.
func raw(number int, mods ...func(entry)) entry {
	e := entry{
		"number": number, "title": fmt.Sprintf("issue %d", number), "html_url": fmt.Sprintf("https://github.com/o/r/issues/%d", number),
		"created_at": "2026-10-20T09:00:00Z", "comments": 0,
		"user": map[string]any{"login": "alice", "type": "User"}, "labels": []any{}, "assignees": []any{},
	}
	for _, m := range mods {
		m(e)
	}
	return e
}

func withLabels(names ...string) func(entry) {
	return func(e entry) {
		l := []any{}
		for _, n := range names {
			l = append(l, map[string]any{"name": n})
		}
		e["labels"] = l
	}
}

func withAssignees(logins ...string) func(entry) {
	return func(e entry) {
		l := []any{}
		for _, n := range logins {
			l = append(l, map[string]any{"login": n})
		}
		e["assignees"] = l
	}
}

func withAuthor(login, kind string) func(entry) {
	return func(e entry) { e["user"] = map[string]any{"login": login, "type": kind} }
}

func asPull(e entry) { e["pull_request"] = map[string]any{"url": "x"} }

func body(entries ...entry) string {
	b, err := json.Marshal(entries)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func run(t *testing.T, f Reader, o Options) *Result {
	t.Helper()
	if o.Now == nil {
		o.Now = func() time.Time { return t0 }
	}
	res, err := Run(context.Background(), f, o)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func numbers(rows []Row) []string {
	var out []string
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s#%d", r.Repo, r.Number))
	}
	return out
}

func repoStatus(t *testing.T, res *Result, repo string) RepoStatus {
	t.Helper()
	for _, s := range res.Repos {
		if s.Repo == repo {
			return s
		}
	}
	t.Fatalf("no status for %s in %+v", repo, res.Repos)
	return RepoStatus{}
}

// skipLink answers page 1 with a Link whose next page is 3, not 2.
type skipLink struct{ *fakeForge }

func (s *skipLink) IssuesPage(ctx context.Context, repo string, page int) (hosting.WatchResponse, error) {
	res, err := s.fakeForge.IssuesPage(ctx, repo, page)
	if page == 1 {
		res.Link = `<https://api.github.com/x?page=3>; rel="next"`
	}
	return res, err
}
