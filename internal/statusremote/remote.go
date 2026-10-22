// SPDX-License-Identifier: EUPL-1.2

package statusremote

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
)

const (
	// DefaultTimeout is the overall deadline of the remote half of a run (Spec 037, `--timeout`).
	DefaultTimeout = 90 * time.Second
	// ReasonOffline is the skip reason of every source of a run with `--offline`.
	ReasonOffline = "offline"
	// CodeHostingUnavailable is the error code of every source when the reader could not be opened for a
	// reason other than a missing token (no `gh`, a `gh` that is too old): the attempt failed, which is
	// not a skip.
	CodeHostingUnavailable = "hosting-api-unavailable"
	// parallel is the number of repositories one source reads at the same time.
	parallel = 4
)

// Repository is a workspace repository of the run: what its origin says (Origin) and the clone the
// local reads use (Dir, the absolute path; empty when there is none).
type Repository struct {
	Origin
	Dir string
}

// Options configures Collect.
type Options struct {
	// Offline skips every source of every repository: no credential is looked up and no request is made.
	Offline bool
	// Timeout is the overall deadline of the remote half; zero means DefaultTimeout.
	Timeout time.Duration
	// Open builds the client (hosting.NewStatusReader in production) and returns the function that
	// releases it. It is called at most once, only when some repository can be read, and never when
	// Offline. An error that is hosting.ErrAuthentication means "no usable token".
	Open func(ctx context.Context) (Client, func(), error)
}

// RepoResult is what the remote sources read for one workspace repository. Remote carries the states of
// all six sources and the data of those that ended `ok`, in the input form of overview.Build; the data of
// a source that did not end `ok` is empty. Pulls and CIRelease hold what the overview does not carry (the
// counts of the pulls source, the runs and workflows of the release tags), and are nil unless their
// source ended `ok`. Slug is empty for a repository that is skipped for what its origin is.
type RepoResult struct {
	Name      string
	Slug      string
	Remote    *overview.Remote
	Pulls     *Pulls
	CIRelease *CIRelease
}

// Result is the outcome of Collect, one RepoResult per repository in the order of the input. Timeout is
// the overall deadline that applied and RateLimitReset the reset time of the first rate-limit evidence
// (zero when there was none or it gave no time): the two inputs of the notices of overview.Build.
type Result struct {
	Repos          []RepoResult
	Timeout        time.Duration
	RateLimitReset time.Time
}

// Collect runs the remote sources for the repositories and returns what each of them read; it never
// fails as a whole, every outcome is the state of a source. Repositories whose origin is no GitHub
// repository are skipped (see Select) and never cost a request. Under Offline, without a usable token
// or when the reader cannot be opened, nothing is read and every source of the repositories that could
// have been read carries the reason. Otherwise the pull request source and the others run side by side,
// each over its own pool of four repositories, one repository after the other inside a source, all
// under one deadline: when it passes (or ctx ends) requests not yet sent are dropped, requests in
// flight are cancelled and the unfinished sources are skipped with the reason `deadline`, keeping what
// the finished ones read. The first rate-limit evidence of any source stops every further request; the
// run never sleeps or retries.
func Collect(ctx context.Context, repos []Repository, o Options) Result {
	timeout := o.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	origins := make([]Origin, len(repos))
	for i, r := range repos {
		origins[i] = r.Origin
	}
	targets := Select(origins)
	c := &collector{repos: repos, targets: targets, slots: make([]slot, len(repos))}
	for i, t := range targets {
		if t.Skip == "" {
			c.selected = append(c.selected, i)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if len(c.selected) > 0 {
		c.run(ctx, o)
	}
	res := Result{Repos: c.results(), Timeout: timeout}
	if c.gate != nil {
		res.RateLimitReset = c.gate.resetAt()
	}
	return res
}

// run reads the selected repositories, or records why nothing was read.
func (c *collector) run(ctx context.Context, o Options) {
	if o.Offline {
		c.fallback = &overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: ReasonOffline}
		return
	}
	client, closeClient, st := open(ctx, o)
	if st != nil {
		c.fallback = st
		return
	}
	defer closeClient()
	c.gate = &gate{c: client}
	c.read(ctx)
}

// open builds the client, or gives the state that every source of the readable repositories gets
// instead: no usable token (nothing was attempted), the deadline, or a failed attempt.
func open(ctx context.Context, o Options) (Client, func(), *overview.SourceState) {
	client, closeClient, err := o.Open(ctx)
	switch {
	case err == nil:
		return client, closeClient, nil
	case ctx.Err() != nil:
		return nil, nil, &overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonDeadline}
	case errors.Is(err, hosting.ErrAuthentication):
		return nil, nil, &overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNoToken}
	}
	return nil, nil, &overview.SourceState{Outcome: overview.OutcomeError, Code: CodeHostingUnavailable}
}

// slot is what the sources read for one repository.
type slot struct {
	pulls    Pulls
	releases Releases
	products ReleaseProducts
}

// collector is the state of one run.
type collector struct {
	repos    []Repository
	targets  []Target
	selected []int // indices of the repositories that can be read
	slots    []slot
	gate     *gate
	fallback *overview.SourceState // the state of every source of the selected repositories when nothing was read
}

// read runs the sources and returns when all of them are done.
func (c *collector) read(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Go(func() { c.readPulls(ctx) })
	wg.Go(func() { c.each(func(idx int) { c.readReleases(ctx, idx) }) })
	wg.Wait()
}

// each runs fn for every selected repository, at most parallel at a time and starting them in order.
func (c *collector) each(fn func(idx int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, parallel)
	for _, idx := range c.selected {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			fn(idx)
		})
	}
	wg.Wait()
}

// readReleases reads the repository, its releases and its tags, with the tag patterns that the
// release contracts of the clone declare.
func (c *collector) readReleases(ctx context.Context, idx int) {
	s := &c.slots[idx]
	s.products = ReadReleaseProducts(c.repos[idx].Dir)
	s.releases = ReadReleases(ctx, c.gate, c.targets[idx].Slug, s.products.Patterns())
}

func (c *collector) readPulls(ctx context.Context) {
	targets := make([]Target, len(c.selected))
	for i, idx := range c.selected {
		targets[i] = c.targets[idx]
	}
	got := ReadPulls(ctx, c.gate, targets)
	for _, idx := range c.selected {
		c.slots[idx].pulls = got[c.targets[idx].Name]
	}
}

// results assembles one RepoResult per repository.
func (c *collector) results() []RepoResult {
	out := make([]RepoResult, len(c.repos))
	for i, t := range c.targets {
		r := RepoResult{Name: t.Name, Slug: t.Slug, Remote: &overview.Remote{Sources: map[overview.Source]overview.SourceState{}}}
		switch {
		case t.Skip != "":
			setAll(r.Remote, overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: t.Skip})
		case c.fallback != nil:
			setAll(r.Remote, *c.fallback)
		default:
			c.fill(&r, c.slots[i])
		}
		out[i] = r
	}
	return out
}

func setAll(r *overview.Remote, st overview.SourceState) {
	for _, src := range overview.Sources {
		r.Sources[src] = st
	}
}

// fill stores what the sources of one repository read. The data of a source that did not end `ok` is
// left out: ReadReleases and the CI reads already return none for a failed source, the pull requests
// that were read before a failure and the visibility that a failed repository read leaves as "unknown"
// are dropped here.
func (c *collector) fill(r *RepoResult, s slot) {
	r.Remote.Sources[overview.SourcePulls] = s.pulls.State
	if s.pulls.State == okState {
		r.Pulls, r.Remote.Pulls = &s.pulls, s.pulls.PRs
	}
	s.releases.Apply(r.Remote)
	if r.Remote.Sources[overview.SourceRepo] != okState {
		r.Remote.DefaultBranch, r.Remote.HTMLURL, r.Remote.Visibility = "", "", ""
	}
}

var okState = overview.SourceState{Outcome: overview.OutcomeOK}
