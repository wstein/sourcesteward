// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/pulls"
)

// Client is every read the online `stw status` makes, over all its sources; *hosting.WatchReader built by
// hosting.NewStatusReader is one. Every method is a GET.
type Client interface {
	pulls.Reader
	CIReader
	Reader
	issues.Reader
}

var _ Client = (*hosting.WatchReader)(nil)

// gate puts the stop rules of a run in front of a Client, for all sources at once: a request is not sent
// once the context is done, and not sent after the first rate-limit evidence of any source (Spec 037:
// the command never waits or retries). A stopped read fails with the rate-limit code, the same error a
// source would get from the forge, so each source reports its unread repositories as `rate-limited`.
type gate struct {
	c Client

	mu      sync.Mutex
	limited bool
	reset   time.Time
}

// admit refuses a request that must not be sent.
func (g *gate) admit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.limited {
		return &hosting.WatchError{Code: hosting.CodeRateLimited, Message: "rate limit reached before this request was sent"}
	}
	return nil
}

// observe records rate-limit evidence. A throttle that arrives as the context ends is not evidence:
// the deadline wins, as in the pulls cycle.
func (g *gate) observe(ctx context.Context, err error) {
	var we *hosting.WatchError
	if ctx.Err() != nil || !errors.As(err, &we) || we.Code != hosting.CodeRateLimited {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.limited {
		g.limited = true
		if we.RateLimit.Known {
			g.reset = we.RateLimit.Reset
		}
	}
}

// resetAt is the reset time the forge gave with the first rate-limit evidence, zero when there was
// none or it gave no time.
func (g *gate) resetAt() time.Time {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reset
}

func through[T any](ctx context.Context, g *gate, read func() (T, error)) (T, error) {
	if err := g.admit(ctx); err != nil {
		var zero T
		return zero, err
	}
	v, err := read()
	g.observe(ctx, err)
	return v, err
}

func (g *gate) ListOpenPulls(ctx context.Context, repo string, prior hosting.PullListState, maxOpen int) (hosting.PullListResult, error) {
	return through(ctx, g, func() (hosting.PullListResult, error) { return g.c.ListOpenPulls(ctx, repo, prior, maxOpen) })
}

func (g *gate) PullDetail(ctx context.Context, repo string, number int, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, func() (hosting.WatchResponse, error) { return g.c.PullDetail(ctx, repo, number, etag) })
}

func (g *gate) Branch(ctx context.Context, repo, name, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, func() (hosting.WatchResponse, error) { return g.c.Branch(ctx, repo, name, etag) })
}

func (g *gate) Commit(ctx context.Context, repo, sha, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, func() (hosting.WatchResponse, error) { return g.c.Commit(ctx, repo, sha, etag) })
}

func (g *gate) PullCommits(ctx context.Context, repo string, number, page int, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, func() (hosting.WatchResponse, error) { return g.c.PullCommits(ctx, repo, number, page, etag) })
}

func (g *gate) CheckRuns(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, func() (hosting.WatchResponse, error) { return g.c.CheckRuns(ctx, repo, sha, page, etag) })
}

func (g *gate) Statuses(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, func() (hosting.WatchResponse, error) { return g.c.Statuses(ctx, repo, sha, page, etag) })
}

func (g *gate) RateLimit(ctx context.Context) (hosting.RateInfo, error) {
	return through(ctx, g, func() (hosting.RateInfo, error) { return g.c.RateLimit(ctx) })
}

func (g *gate) ListOpenPullItems(ctx context.Context, repo string, maxOpen int) ([]hosting.PullListItem, error) {
	return through(ctx, g, func() ([]hosting.PullListItem, error) { return g.c.ListOpenPullItems(ctx, repo, maxOpen) })
}

func (g *gate) WorkflowRuns(ctx context.Context, repo, workflow, sha string) (hosting.WatchResponse, error) {
	return through(ctx, g, func() (hosting.WatchResponse, error) { return g.c.WorkflowRuns(ctx, repo, workflow, sha) })
}

func (g *gate) RepoInfo(ctx context.Context, repo string) (hosting.RepoInfo, error) {
	return through(ctx, g, func() (hosting.RepoInfo, error) { return g.c.RepoInfo(ctx, repo) })
}

func (g *gate) ReleaseList(ctx context.Context, repo string) ([]hosting.StatusRelease, error) {
	return through(ctx, g, func() ([]hosting.StatusRelease, error) { return g.c.ReleaseList(ctx, repo) })
}

func (g *gate) TagList(ctx context.Context, repo string) ([]hosting.StatusTag, error) {
	return through(ctx, g, func() ([]hosting.StatusTag, error) { return g.c.TagList(ctx, repo) })
}

func (g *gate) IssuesPage(ctx context.Context, repo string, page int) (hosting.WatchResponse, error) {
	return through(ctx, g, func() (hosting.WatchResponse, error) { return g.c.IssuesPage(ctx, repo, page) })
}

func (g *gate) Viewer(ctx context.Context) (string, error) {
	return through(ctx, g, func() (string, error) { return g.c.Viewer(ctx) })
}

var _ Client = (*gate)(nil)
