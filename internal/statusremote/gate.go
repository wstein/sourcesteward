// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/issues"
	"github.com/wstein/sourcesteward/internal/overview"
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

	trackStats bool
	requests   int
	cached304s int
	rateLimit  hosting.RateInfo
	sources    map[sourceKey]*SourceStats
	order      []sourceKey
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

func (g *gate) record(ctx context.Context, defaultRepo, defaultSource string, v any, err error, took time.Duration) {
	if !g.trackStats {
		return
	}
	repo, src := sourceFromContext(ctx)
	if repo == "" {
		repo = defaultRepo
	}
	if src == "" {
		src = defaultSource
	}
	if src == "" {
		src = "api"
	}
	var notModified bool
	switch res := v.(type) {
	case hosting.WatchResponse:
		notModified = res.NotModified
		if res.RateLimit.Known {
			g.updateRateLimit(res.RateLimit)
		}
	case hosting.PullListResult:
		notModified = res.Unchanged
	}
	var we *hosting.WatchError
	if errors.As(err, &we) && we.RateLimit.Known {
		g.updateRateLimit(we.RateLimit)
	}
	if rl, ok := g.c.(interface{ LastRateLimit() hosting.RateInfo }); ok {
		info := rl.LastRateLimit()
		if info.Known {
			g.updateRateLimit(info)
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests++
	if notModified {
		g.cached304s++
	}
	if g.sources == nil {
		g.sources = make(map[sourceKey]*SourceStats)
	}
	k := sourceKey{repo: repo, source: src}
	st := g.sources[k]
	if st == nil {
		st = &SourceStats{Repo: repo, Source: src}
		g.sources[k] = st
		g.order = append(g.order, k)
	}
	st.Requests++
	if notModified {
		st.Cached304s++
	}
	st.Latency += took
	st.Consumed = st.Requests - st.Cached304s
}

func (g *gate) updateRateLimit(info hosting.RateInfo) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rateLimit = info
}

func (g *gate) stats() RemoteStats {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := RemoteStats{
		Requests:   g.requests,
		Cached304s: g.cached304s,
		RateLimit:  g.rateLimit,
		Sources:    make([]SourceStats, 0, len(g.order)),
	}
	for _, k := range g.order {
		if st := g.sources[k]; st != nil {
			out.Sources = append(out.Sources, *st)
		}
	}
	sort.Slice(out.Sources, func(i, j int) bool {
		if out.Sources[i].Repo != out.Sources[j].Repo {
			return out.Sources[i].Repo < out.Sources[j].Repo
		}
		return out.Sources[i].Source < out.Sources[j].Source
	})
	return out
}

func through[T any](ctx context.Context, g *gate, repo, src string, read func() (T, error)) (T, error) {
	if err := g.admit(ctx); err != nil {
		var zero T
		return zero, err
	}
	start := time.Now()
	v, err := read()
	took := time.Since(start)
	g.observe(ctx, err)
	g.record(ctx, repo, src, v, err, took)
	return v, err
}

func (g *gate) ListOpenPulls(ctx context.Context, repo string, prior hosting.PullListState, maxOpen int) (hosting.PullListResult, error) {
	return through(ctx, g, repo, string(overview.SourcePulls), func() (hosting.PullListResult, error) { return g.c.ListOpenPulls(ctx, repo, prior, maxOpen) })
}

func (g *gate) PullDetail(ctx context.Context, repo string, number int, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, repo, string(overview.SourcePulls), func() (hosting.WatchResponse, error) { return g.c.PullDetail(ctx, repo, number, etag) })
}

func (g *gate) Branch(ctx context.Context, repo, name, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, repo, string(overview.SourceCIMain), func() (hosting.WatchResponse, error) { return g.c.Branch(ctx, repo, name, etag) })
}

func (g *gate) Commit(ctx context.Context, repo, sha, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, repo, string(overview.SourcePulls), func() (hosting.WatchResponse, error) { return g.c.Commit(ctx, repo, sha, etag) })
}

func (g *gate) PullCommits(ctx context.Context, repo string, number, page int, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, repo, string(overview.SourcePulls), func() (hosting.WatchResponse, error) { return g.c.PullCommits(ctx, repo, number, page, etag) })
}

func (g *gate) CheckRuns(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, repo, string(overview.SourcePulls), func() (hosting.WatchResponse, error) { return g.c.CheckRuns(ctx, repo, sha, page, etag) })
}

func (g *gate) Statuses(ctx context.Context, repo, sha string, page int, etag string) (hosting.WatchResponse, error) {
	return through(ctx, g, repo, string(overview.SourcePulls), func() (hosting.WatchResponse, error) { return g.c.Statuses(ctx, repo, sha, page, etag) })
}

func (g *gate) RateLimit(ctx context.Context) (hosting.RateInfo, error) {
	return through(ctx, g, "", "rate-limit", func() (hosting.RateInfo, error) { return g.c.RateLimit(ctx) })
}

func (g *gate) ListOpenPullItems(ctx context.Context, repo string, maxOpen int) ([]hosting.PullListItem, error) {
	return through(ctx, g, repo, string(overview.SourcePulls), func() ([]hosting.PullListItem, error) { return g.c.ListOpenPullItems(ctx, repo, maxOpen) })
}

func (g *gate) WorkflowRuns(ctx context.Context, repo, workflow, sha string) (hosting.WatchResponse, error) {
	return through(ctx, g, repo, string(overview.SourceCIRelease), func() (hosting.WatchResponse, error) { return g.c.WorkflowRuns(ctx, repo, workflow, sha) })
}

func (g *gate) RepoInfo(ctx context.Context, repo string) (hosting.RepoInfo, error) {
	return through(ctx, g, repo, string(overview.SourceRepo), func() (hosting.RepoInfo, error) { return g.c.RepoInfo(ctx, repo) })
}

func (g *gate) ReleaseList(ctx context.Context, repo string) ([]hosting.StatusRelease, error) {
	return through(ctx, g, repo, string(overview.SourceReleases), func() ([]hosting.StatusRelease, error) { return g.c.ReleaseList(ctx, repo) })
}

func (g *gate) TagList(ctx context.Context, repo string) ([]hosting.StatusTag, error) {
	return through(ctx, g, repo, string(overview.SourceTags), func() ([]hosting.StatusTag, error) { return g.c.TagList(ctx, repo) })
}

func (g *gate) IssuesPage(ctx context.Context, repo string, page int) (hosting.WatchResponse, error) {
	return through(ctx, g, repo, string(overview.SourceIssues), func() (hosting.WatchResponse, error) { return g.c.IssuesPage(ctx, repo, page) })
}

func (g *gate) Viewer(ctx context.Context) (string, error) {
	return through(ctx, g, "", "viewer", func() (string, error) { return g.c.Viewer(ctx) })
}

var _ Client = (*gate)(nil)

// pullsReader is what the pulls cycle reads through: the gate, and, when the review state was asked
// for and the client can read the comments of a pull request (Spec 046), the gate's review reads too.
// Otherwise the reader is no pulls.ReviewSource, so the cycle reads no review state at all.
func (g *gate) pullsReader(reviews bool) pulls.Reader {
	if rs, ok := g.c.(pulls.ReviewSource); ok && reviews {
		return reviewGate{gate: g, rs: rs}
	}
	return g
}

// reviewGate adds the two review reads to the gate; RepoInfo is the gate's own.
type reviewGate struct {
	*gate
	rs pulls.ReviewSource
}

func (g reviewGate) ReadIssueComments(ctx context.Context, repo string, number int) ([]hosting.IssueComment, bool, error) {
	type result struct {
		c  []hosting.IssueComment
		tr bool
	}
	v, err := through(ctx, g.gate, repo, string(overview.SourcePulls), func() (result, error) {
		c, tr, err := g.rs.ReadIssueComments(ctx, repo, number)
		return result{c, tr}, err
	})
	return v.c, v.tr, err
}

func (g reviewGate) ClosedPulls(ctx context.Context, repo string) (hosting.WatchResponse, error) {
	return through(ctx, g.gate, repo, string(overview.SourcePulls), func() (hosting.WatchResponse, error) { return g.rs.ClosedPulls(ctx, repo) })
}

var _ pulls.ReviewSource = reviewGate{}
