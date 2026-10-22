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
}

func newWorld() *world {
	return &world{
		errs: map[string]error{}, pulls: newForge(), info: map[string]hosting.RepoInfo{}, rels: map[string][]hosting.StatusRelease{},
		tags: map[string][]hosting.StatusTag{}, heads: map[string]string{}, flows: map[string]string{},
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
