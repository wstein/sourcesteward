// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"path/filepath"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watch"
	"github.com/wstein/sourcesteward/internal/watch/watchhost"
)

// Modes of a run.
const (
	ModeOnce   = "once"
	ModeStream = "stream"
	ModeUntil  = "until-event"
)

// Exit codes of `stw watch` (Spec 035, "Exit codes").
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
	ExitLocked  = 3
	ExitTimeout = 4
	ExitStale   = 5
)

// Params is a fully validated run. Zero seams use the real clock, sleep, jitter and hosting reader.
type Params struct {
	Mode           string
	UntilKinds     map[string]bool
	Timeout        time.Duration // --until-event
	MaxIdle        time.Duration // --stream, 0 = off
	Interval       time.Duration
	DetailEvery    int
	PendingTimeout time.Duration
	MaxOpenPRs     int
	Baseline       bool
	Human          bool
	Host           string
	StateDir       string // absolute; empty selects the default directory
	Repos          []string
	Skipped        []watch.Skipped
	WorkspaceRoot  string
	IgnoreAuthors  []string
	IgnoreLabels   []string
	StackPatterns  []string
	Out            io.Writer
	Diag           io.Writer // one-line diagnostics (stderr); nil discards them

	Transport hosting.Transport // in-process HTTP fixture; nil means HTTPS
	Now       func() time.Time
	Sleep     func(context.Context, time.Duration) error
	Jitter    func() time.Duration
	// OpenReader replaces the hosting reader (tests). The target is the state directory, which bounds the
	// gh configuration check like the repository does for the one-shot operations.
	OpenReader func(ctx context.Context, target, host string) (Reader, func(), error)
}

// Failure is a terminal error with the exit code it maps to.
type Failure struct {
	Code int
	Err  error
}

func (f *Failure) Error() string { return f.Err.Error() }
func (f *Failure) Unwrap() error { return f.Err }

func fail(code int, format string, args ...any) *Failure {
	return &Failure{Code: code, Err: fmt.Errorf(format, args...)}
}

// StateDirFor is the state directory of a run: --state-dir, else the per-workspace-id default.
func StateDirFor(stateDir, host, workspaceRoot string, repos []string) string {
	if stateDir != "" {
		return stateDir
	}
	if workspaceRoot != "" {
		if real, err := filepath.EvalSymlinks(workspaceRoot); err == nil {
			workspaceRoot = real
		}
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			workspaceRoot = abs
		}
	}
	return watch.DefaultStateDir(watch.WorkspaceID(host, workspaceRoot, repos))
}

func sleepReal(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func jitterReal() time.Duration { return time.Duration(rand.Int64N(int64(5 * time.Second))) }

// counting tells a --once run whether any repository could be read at all.
type counting struct {
	inner      watch.Observer
	oks, fails int
}

func (c *counting) Observe(ctx context.Context, repo string, h watch.Hints) (watch.RepoObservation, error) {
	obs, err := c.inner.Observe(ctx, repo, h)
	if err != nil {
		c.fails++
	} else {
		c.oks++
	}
	return obs, err
}

// Run executes one watch. It returns nil when the run ended as specified with exit code 0, otherwise a
// *Failure carrying the exit code (1 failure, 2 usage, 3 locked, 4 timeout). A stream interrupted by
// ctx ends with exit 0.
func Run(ctx context.Context, p Params) error {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	sleep := p.Sleep
	if sleep == nil {
		sleep = sleepReal
	}
	jitter := p.Jitter
	if jitter == nil {
		jitter = jitterReal
	}
	dir := StateDirFor(p.StateDir, p.Host, p.WorkspaceRoot, p.Repos)
	if err := watch.EnsureStateDir(dir); err != nil {
		if errors.Is(err, watch.ErrStateDirUsage) || errors.Is(err, watch.ErrInsideWorkTree) {
			return &Failure{ExitUsage, err}
		}
		return &Failure{ExitFailure, err}
	}
	open := p.OpenReader
	if open == nil {
		open = func(ctx context.Context, target, host string) (Reader, func(), error) {
			r, cleanup, err := hosting.NewWatchReaderWithTransport(ctx, target, host, p.Transport)
			if err != nil {
				return nil, func() {}, err
			}
			return r, cleanup, nil
		}
	}
	reader, cleanup, err := open(ctx, dir, p.Host)
	if err != nil {
		return fail(ExitFailure, "hosting-api-unavailable: %v", err)
	}
	defer cleanup()
	host := NewHostObserver(reader, now, p.MaxOpenPRs, p.IgnoreAuthors, p.IgnoreLabels)
	if p.Diag != nil {
		host.Diag = func(line string) { fmt.Fprintln(p.Diag, esc(line)) }
	}
	obs := &counting{inner: host}
	var restackDiag func(string)
	if p.Diag != nil {
		restackDiag = func(line string) { fmt.Fprintln(p.Diag, esc(line)) }
	}
	opts := watch.Options{Dir: dir, Host: p.Host, Repos: p.Repos, Skipped: p.Skipped, Observer: obs, Restacker: watchhost.NewRestacker(reader, restackDiag), Clock: now, Out: p.Out,
		Baseline: p.Baseline, Interval: p.Interval, PendingTimeout: p.PendingTimeout, DetailEvery: p.DetailEvery,
		IgnoreAuthors: p.IgnoreAuthors, IgnoreLabels: p.IgnoreLabels, StackPatterns: p.StackPatterns, Mode: p.Mode}
	if p.Human {
		opts.Render = Human
	}
	w, err := watch.Open(opts)
	if err != nil {
		var locked *watch.LockedError
		switch {
		case errors.As(err, &locked):
			return &Failure{ExitLocked, err}
		case errors.Is(err, watch.ErrStateDirUsage) || errors.Is(err, watch.ErrInsideWorkTree):
			return &Failure{ExitUsage, err}
		}
		return &Failure{ExitFailure, err}
	}
	defer w.Close()
	start := now()
	lastEvent := start
	for {
		obs.oks, obs.fails = 0, 0
		res, err := w.Cycle(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return interrupted(p.Mode)
			}
			return &Failure{ExitFailure, err}
		}
		if len(res.Events) > 0 {
			lastEvent = now()
		}
		slow := host.AfterCycle(ctx)
		switch p.Mode {
		case ModeOnce:
			if obs.oks == 0 && obs.fails > 0 {
				return fail(ExitFailure, "no repository could be read")
			}
			return nil
		case ModeUntil:
			for _, ev := range res.Events {
				if p.UntilKinds[ev.Kind] {
					return nil
				}
			}
		}
		wait := p.Interval
		if slow {
			wait = min(2*p.Interval, max(10*time.Minute, p.Interval))
		}
		wait += jitter()
		wait = max(wait, res.RetryIn)
		switch p.Mode {
		case ModeUntil:
			remaining := p.Timeout - now().Sub(start)
			if remaining <= 0 {
				return fail(ExitTimeout, "no event of the requested kinds within %s", p.Timeout)
			}
			wait = min(wait, remaining)
		case ModeStream:
			if p.MaxIdle > 0 {
				idle := p.MaxIdle - now().Sub(lastEvent)
				if idle <= 0 {
					return nil
				}
				wait = min(wait, idle)
			}
		}
		if err := sleep(ctx, wait); err != nil {
			return interrupted(p.Mode)
		}
		if p.Mode == ModeUntil && now().Sub(start) >= p.Timeout {
			return fail(ExitTimeout, "no event of the requested kinds within %s", p.Timeout)
		}
	}
}

// interrupted is SIGINT/SIGTERM: success for a stream, a failure for a run that did not reach its end.
func interrupted(mode string) error {
	if mode == ModeStream {
		return nil
	}
	return fail(ExitFailure, "interrupted")
}

// StatusParams selects the report of --status.
type StatusParams struct {
	StateDir string // absolute; resolved by StateDirFor
	JSON     bool
	Out      io.Writer
	Now      func() time.Time
}

// RunStatus prints the liveness report. It reads the state directory only. The error is nil when the
// watcher is fresh and a *Failure (exit 5 stale or missing heartbeat, 1 unreadable, 2 usage) otherwise.
func RunStatus(p StatusParams) error {
	now := p.Now
	if now == nil {
		now = time.Now
	}
	st, err := watch.ReadStatus(p.StateDir, now())
	if err != nil {
		if errors.Is(err, watch.ErrStateDirUsage) {
			return &Failure{ExitUsage, err}
		}
		return &Failure{ExitFailure, err}
	}
	var werr error
	if p.JSON {
		werr = json.NewEncoder(p.Out).Encode(st)
	} else {
		werr = writeStatusText(p.Out, st)
	}
	if werr != nil {
		return &Failure{ExitFailure, werr}
	}
	if !st.Fresh {
		return &Failure{ExitStale, errors.New("the heartbeat is stale or missing")}
	}
	return nil
}

func writeStatusText(w io.Writer, st watch.Status) error {
	age := func(a *int) string {
		if a == nil {
			return "never"
		}
		return fmt.Sprintf("%ds ago", *a)
	}
	pr := func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }
	pr("state directory: %s", esc(st.StateDir))
	switch {
	case st.Heartbeat == nil:
		pr("heartbeat: missing")
	default:
		pr("heartbeat: %ds ago (pid %d)", st.Heartbeat.AgeSeconds, st.Heartbeat.PID)
	}
	if st.Fresh {
		pr("status: fresh (interval %ds)", st.IntervalSeconds)
	} else {
		pr("status: stale (interval %ds)", st.IntervalSeconds)
	}
	pr("last seq: %d", st.Seq)
	if st.LockHeld {
		if st.LockPID > 0 {
			pr("lock: held by pid %d", st.LockPID)
		} else {
			pr("lock: held")
		}
	} else {
		pr("lock: free")
	}
	for _, r := range st.Repos {
		line := fmt.Sprintf("  %s  last ok %s  failures %d", esc(r.Repo), age(r.AgeSeconds), r.Failures)
		if r.OpenError != nil {
			line += "  open error " + esc(*r.OpenError)
		}
		pr("%s", line)
	}
	return nil
}
