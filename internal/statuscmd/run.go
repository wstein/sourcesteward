// SPDX-License-Identifier: EUPL-1.2

package statuscmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/overview"
	"github.com/wstein/sourcesteward/internal/statusremote"
	"github.com/wstein/sourcesteward/internal/stwsync"
)

// The bounds of --timeout, the overall deadline of the remote half.
const (
	MinTimeout = time.Second
	MaxTimeout = 10 * time.Minute
)

// Params is one online run. The function fields default to the production readers when nil.
type Params struct {
	Status  stwsync.StatusOptions
	Timeout time.Duration
	// Progress receives the single rewritten progress line; nil prints none. The caller sets it only
	// when stderr is a terminal.
	Progress io.Writer

	Now func() time.Time
	// Local is stwsync.Status and Discover is stwsync.Discover.
	Local    func(context.Context, stwsync.StatusOptions) stwsync.StatusResult
	Discover func(context.Context, string) (stwsync.Workspace, error)
	// Open builds the client for the workspace root and host; the default is hosting.NewStatusReader,
	// whose per-request timeout is 15 s.
	Open func(ctx context.Context, target, host string) (statusremote.Client, func(), error)
}

// Report is everything one online run found: the local status, what the remote sources read (one result
// per local repository, in the same order) and the action model over both.
type Report struct {
	Time     time.Time
	Local    stwsync.StatusResult
	Remote   statusremote.Result
	Overview overview.Overview
}

// Failure is nil when the run succeeded and the reason of exit status 1 otherwise: the run failed, a
// repository cannot be read locally, or an attempted remote request failed. A skipped source (no
// token, the deadline, a missing workflow) and the items never are a failure.
func (r Report) Failure() error {
	switch n := r.Local.Summary.Errors; {
	case r.Local.Error != nil:
		return fmt.Errorf("status: %s: %s", r.Local.Error.Code, r.Local.Error.Message)
	case n == 1:
		return fmt.Errorf("status: 1 repository cannot be read")
	case n > 1:
		return fmt.Errorf("status: %d repositories cannot be read", n)
	case r.Overview.Summary.Failed:
		return fmt.Errorf("status: a remote read failed (see the notices)")
	}
	return nil
}

// NoToken reports that the remote half was not read because no usable token was found before any request. The
// output is then the local view alone plus the notice (Spec 037, "No usable token").
func (r Report) NoToken() bool {
	noToken := overview.SourceState{Outcome: overview.OutcomeSkipped, Reason: overview.ReasonNoToken}
	for _, rr := range r.Remote.Repos {
		if rr.Remote != nil && rr.Remote.Sources[overview.SourcePulls] == noToken {
			return true
		}
	}
	return false
}

// Run reads the local status, then the remote sources of every repository whose origin is a GitHub
// repository, and builds the action model. A failed local read returns the report without remote
// data. The error is only for a workspace whose origins cannot be read.
func Run(ctx context.Context, p Params) (Report, error) {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	rep := Report{Time: now().UTC()}
	local := p.Local
	if local == nil {
		local = stwsync.Status
	}
	rep.Local = local(ctx, p.Status)
	if rep.Local.Error != nil {
		rep.Overview = overview.Build(overview.Input{})
		return rep, nil
	}
	discover := p.Discover
	if discover == nil {
		discover = stwsync.Discover
	}
	ws, err := discover(ctx, p.Status.Path)
	if err != nil {
		return rep, fmt.Errorf("status: the remotes of the workspace cannot be read: %w", err)
	}
	root := ws.Root
	if rep.Local.Root != nil {
		root = *rep.Local.Root
	}
	rep.Remote = statusremote.Collect(ctx, repositories(rep.Local.Repos, ws.Members), statusremote.Options{
		Timeout: p.Timeout,
		Open:    p.opener(root),
	})
	rep.Overview = overview.Build(rep.Remote.Input(root, rep.Local.Repos))
	return rep, nil
}

// repositories pairs each local repository with the origin and directory of the workspace member of the same name.
func repositories(locals []stwsync.RepoStatus, members []stwsync.Member) []statusremote.Repository {
	byName := make(map[string]stwsync.Member, len(members))
	for _, m := range members {
		byName[m.Name] = m
	}
	repos := make([]statusremote.Repository, len(locals))
	for i, rp := range locals {
		m := byName[rp.Name]
		repos[i] = statusremote.Repository{Origin: statusremote.Origin{Name: rp.Name, Remote: m.Remote, Readable: rp.State == "ok"}, Dir: m.Path}
	}
	return repos
}

// opener is the Open function of the collection: the reader of the host, with the progress line in
// front of it when there is one.
func (p Params) opener(root string) func(context.Context) (statusremote.Client, func(), error) {
	open := p.Open
	if open == nil {
		open = func(ctx context.Context, target, host string) (statusremote.Client, func(), error) {
			r, cleanup, err := hosting.NewStatusReader(ctx, target, host)
			if err != nil {
				return nil, func() {}, err
			}
			return r, cleanup, nil
		}
	}
	return func(ctx context.Context) (statusremote.Client, func(), error) {
		client, cleanup, err := open(ctx, root, statusremote.Host)
		if err != nil || p.Progress == nil {
			return client, cleanup, err
		}
		line := &progressLine{w: p.Progress}
		return progressClient{Client: client, line: line}, func() { line.clear(); cleanup() }, nil
	}
}
