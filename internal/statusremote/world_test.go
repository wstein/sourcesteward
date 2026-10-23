// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// world is a whole fake forge behind the Client interface: pull requests, repositories, releases, tags,
// the head of the default branch and workflow runs, keyed by repository. Every request is recorded as
// one line ("repo o/r", "list o/r", ...), passes the hook and then the canned error of its line. It holds
// no credential and reaches no network.
type world struct {
	mu    sync.Mutex
	calls []string
	hook  func(ctx context.Context, call string) error
	errs  map[string]error

	pulls *forge
	info  map[string]hosting.RepoInfo
	rels  map[string][]hosting.StatusRelease
	tags  map[string][]hosting.StatusTag
	heads map[string]string // repository -> head of its default branch
	flows map[string]string // "repo workflow@sha" -> workflow runs body

	issues    map[string][]string // repository -> bodies of its issue list pages
	viewer    string              // the login `user` answers with
	viewerErr error
}

func newWorld() *world {
	return &world{
		errs: map[string]error{}, pulls: newForge(), info: map[string]hosting.RepoInfo{}, rels: map[string][]hosting.StatusRelease{},
		tags: map[string][]hosting.StatusTag{}, heads: map[string]string{}, flows: map[string]string{},
		issues: map[string][]string{}, viewer: "wstein",
	}
}

var _ Client = (*world)(nil)

func (w *world) enter(ctx context.Context, call string) error {
	w.mu.Lock()
	w.calls = append(w.calls, call)
	hook, canned := w.hook, w.errs[call]
	w.mu.Unlock()
	if hook != nil {
		if err := hook(ctx, call); err != nil {
			return err
		}
	}
	return canned
}

func (w *world) requests() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.calls...)
}

// count is the number of recorded requests that start with prefix.
func (w *world) count(prefix string) int {
	n := 0
	for _, c := range w.requests() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// addRepo gives a repository the default branch main, write access, a released v1.0.0 and, when
// unreleased is set, an unreleased tag v1.1.0.
func (w *world) addRepo(repo string, unreleased bool) {
	t, f := true, false
	w.info[repo] = hosting.RepoInfo{DefaultBranch: "main", HTMLURL: "https://github.com/" + repo, Admin: &f, Maintain: &f, Push: &t}
	w.rels[repo] = []hosting.StatusRelease{{Tag: "v1.0.0", URL: "https://github.com/" + repo + "/releases/1"}}
	w.tags[repo] = []hosting.StatusTag{{Name: "v1.0.0", SHA: sha("1")}}
	if unreleased {
		w.tags[repo] = append(w.tags[repo], hosting.StatusTag{Name: "v1.1.0", SHA: sha("2")})
	}
	w.heads[repo] = sha("a")
}

func (w *world) RepoInfo(ctx context.Context, repo string) (hosting.RepoInfo, error) {
	if err := w.enter(ctx, "repo "+repo); err != nil {
		return hosting.RepoInfo{}, err
	}
	return w.info[repo], nil
}

func (w *world) ReleaseList(ctx context.Context, repo string) ([]hosting.StatusRelease, error) {
	if err := w.enter(ctx, "releases "+repo); err != nil {
		return nil, err
	}
	return w.rels[repo], nil
}

func (w *world) TagList(ctx context.Context, repo string) ([]hosting.StatusTag, error) {
	if err := w.enter(ctx, "tags "+repo); err != nil {
		return nil, err
	}
	return w.tags[repo], nil
}

func (w *world) Branch(ctx context.Context, repo, name, _ string) (hosting.WatchResponse, error) {
	if err := w.enter(ctx, "branch "+repo+" "+name); err != nil {
		return hosting.WatchResponse{}, err
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(ciBranch(w.heads[repo]))}, nil
}

func (w *world) WorkflowRuns(ctx context.Context, repo, workflow, sha string) (hosting.WatchResponse, error) {
	if err := w.enter(ctx, fmt.Sprintf("flow %s %s@%s", repo, workflow, sha[:4])); err != nil {
		return hosting.WatchResponse{}, err
	}
	body, ok := w.flows[repo+" "+workflow+"@"+sha]
	if !ok {
		body = `{"total_count":0,"workflow_runs":[]}`
	}
	return hosting.WatchResponse{Status: 200, Body: []byte(body)}, nil
}

func (w *world) ListOpenPullItems(ctx context.Context, repo string, maxOpen int) ([]hosting.PullListItem, error) {
	if err := w.enter(ctx, "list "+repo); err != nil {
		return nil, err
	}
	return w.pulls.ListOpenPullItems(ctx, repo, maxOpen)
}

func (w *world) PullDetail(ctx context.Context, repo string, n int, etag string) (hosting.WatchResponse, error) {
	if err := w.enter(ctx, fmt.Sprintf("detail %s#%d", repo, n)); err != nil {
		return hosting.WatchResponse{}, err
	}
	return w.pulls.PullDetail(ctx, repo, n, etag)
}

func (w *world) CheckRuns(ctx context.Context, repo, s string, page int, etag string) (hosting.WatchResponse, error) {
	if err := w.enter(ctx, "check-runs "+repo+" "+s[:4]); err != nil {
		return hosting.WatchResponse{}, err
	}
	return w.pulls.CheckRuns(ctx, repo, s, page, etag)
}

func (w *world) Statuses(ctx context.Context, repo, s string, page int, etag string) (hosting.WatchResponse, error) {
	if err := w.enter(ctx, "statuses "+repo+" "+s[:4]); err != nil {
		return hosting.WatchResponse{}, err
	}
	return w.pulls.Statuses(ctx, repo, s, page, etag)
}

func (w *world) ListOpenPulls(ctx context.Context, repo string, _ hosting.PullListState, _ int) (hosting.PullListResult, error) {
	return hosting.PullListResult{}, w.enter(ctx, "list-open "+repo)
}

func (w *world) Commit(ctx context.Context, repo, s, _ string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, w.enter(ctx, "commit "+repo+" "+s[:4])
}

func (w *world) PullCommits(ctx context.Context, repo string, n, _ int, _ string) (hosting.WatchResponse, error) {
	return hosting.WatchResponse{}, w.enter(ctx, fmt.Sprintf("commits %s#%d", repo, n))
}

func (w *world) RateLimit(ctx context.Context) (hosting.RateInfo, error) {
	return hosting.RateInfo{}, w.enter(ctx, "rate-limit")
}

// IssuesPage serves the canned pages of a repository ("issues o/r#N" is the request line); a page that has a
// successor announces it in the Link header, which only says that another page exists.
func (w *world) IssuesPage(ctx context.Context, repo string, page int) (hosting.WatchResponse, error) {
	if err := w.enter(ctx, fmt.Sprintf("issues %s#%d", repo, page)); err != nil {
		return hosting.WatchResponse{}, err
	}
	bodies := w.issues[repo]
	res := hosting.WatchResponse{Status: 200, Body: []byte("[]")}
	if page >= 1 && page <= len(bodies) {
		res.Body = []byte(bodies[page-1])
	}
	if page < len(bodies) {
		res.Link = fmt.Sprintf(`<https://api.github.com/repos/%s/issues?page=%d>; rel="next"`, repo, page+1)
	}
	return res, nil
}

func (w *world) Viewer(ctx context.Context) (string, error) {
	if err := w.enter(ctx, "user"); err != nil {
		return "", err
	}
	return w.viewer, w.viewerErr
}

// issueList is the body of an issue list page: entries are (number, label...) tuples written by issue().
func issueList(entries ...string) string { return "[" + strings.Join(entries, ",") + "]" }

// issue is one list entry created a day ago by alice, with the labels and assignees given.
func issue(number int, labels, assignees []string) string {
	named := func(key string, names []string) string {
		var parts []string
		for _, n := range names {
			parts = append(parts, fmt.Sprintf(`{%q:%q}`, key, n))
		}
		return "[" + strings.Join(parts, ",") + "]"
	}
	return fmt.Sprintf(`{"number":%d,"title":"issue %d","html_url":"https://github.com/o/r/issues/%d","created_at":"2026-10-20T09:00:00Z","comments":0,`+
		`"user":{"login":"alice","type":"User"},"labels":%s,"assignees":%s}`, number, number, number, named("name", labels), named("login", assignees))
}
