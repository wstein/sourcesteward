// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/statusremote"
)

// progressLine is the single, rewritten progress line on stderr: the number of requests sent so far
// (the total is not known, the pull request source decides it as it reads). It is cleared when the
// reader is released, so nothing of it remains in a terminal.
type progressLine struct {
	mu      sync.Mutex
	w       io.Writer
	n       int
	written bool
}

func (l *progressLine) tick() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n++
	l.written = true
	_, _ = fmt.Fprintf(l.w, "\rreading remote: %d requests", l.n)
}

func (l *progressLine) clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.written {
		_, _ = io.WriteString(l.w, "\r\x1b[K")
		l.written = false
	}
}

// progressClient counts every request of the client it wraps.
type progressClient struct {
	statusremote.Client
	line *progressLine
}

func (c progressClient) ListOpenPulls(ctx context.Context, repo string, prior hosting.PullListState, maxOpen int) (hosting.PullListResult, error) {
	c.line.tick()
	return c.Client.ListOpenPulls(ctx, repo, prior, maxOpen)
}

func (c progressClient) PullDetail(ctx context.Context, repo string, number int, etag string) (hosting.WatchResponse, error) {
	c.line.tick()
	return c.Client.PullDetail(ctx, repo, number, etag)
}

func (c progressClient) Branch(ctx context.Context, repo, name, etag string) (hosting.WatchResponse, error) {
	c.line.tick()
	return c.Client.Branch(ctx, repo, name, etag)
}

func (c progressClient) Commit(ctx context.Context, repo, sha, etag string) (hosting.WatchResponse, error) {
	c.line.tick()
	return c.Client.Commit(ctx, repo, sha, etag)
}

func (c progressClient) PullCommits(ctx context.Context, repo string, number, page int, etag string) (hosting.WatchResponse, error) {
	c.line.tick()
	return c.Client.PullCommits(ctx, repo, number, page, etag)
}

func (c progressClient) CheckRuns(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error) {
	c.line.tick()
	return c.Client.CheckRuns(ctx, repo, sha, page, etag)
}

func (c progressClient) Statuses(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error) {
	c.line.tick()
	return c.Client.Statuses(ctx, repo, sha, page, etag)
}

func (c progressClient) RateLimit(ctx context.Context) (hosting.RateInfo, error) {
	c.line.tick()
	return c.Client.RateLimit(ctx)
}

func (c progressClient) ListOpenPullItems(ctx context.Context, repo string, maxOpen int) ([]hosting.PullListItem, error) {
	c.line.tick()
	return c.Client.ListOpenPullItems(ctx, repo, maxOpen)
}

func (c progressClient) WorkflowRuns(ctx context.Context, repo, workflow, sha string) (hosting.WatchResponse, error) {
	c.line.tick()
	return c.Client.WorkflowRuns(ctx, repo, workflow, sha)
}

func (c progressClient) RepoInfo(ctx context.Context, repo string) (hosting.RepoInfo, error) {
	c.line.tick()
	return c.Client.RepoInfo(ctx, repo)
}

func (c progressClient) ReleaseList(ctx context.Context, repo string) ([]hosting.StatusRelease, error) {
	c.line.tick()
	return c.Client.ReleaseList(ctx, repo)
}

func (c progressClient) TagList(ctx context.Context, repo string) ([]hosting.StatusTag, error) {
	c.line.tick()
	return c.Client.TagList(ctx, repo)
}

func (c progressClient) IssuesPage(ctx context.Context, repo string, page int) (hosting.WatchResponse, error) {
	c.line.tick()
	return c.Client.IssuesPage(ctx, repo, page)
}

func (c progressClient) Viewer(ctx context.Context) (string, error) {
	c.line.tick()
	return c.Client.Viewer(ctx)
}
