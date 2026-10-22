// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// Options configure a Watcher. Only Dir, Repos, Observer and Out are needed in production; Faults is the
// crash-injection seam of the tests.
type Options struct {
	Dir      string   // absolute state directory
	Host     string   // informational
	Repos    []string // the resolved repository set, in polling order
	Skipped  []Skipped
	Observer Observer
	// Restacker is the hook of WP-D. Nil: a merge records no restack work.
	Restacker Restacker
	Clock     func() time.Time
	Out       io.Writer // stdout; every event is one JSON line unless Render is set
	Render    func(Event) []byte
	// Baseline forces the first cycle to be a silent baseline even when state exists.
	Baseline       bool
	Interval       time.Duration
	PendingTimeout time.Duration // 0 disables checks-pending-timeout
	DetailEvery    int
	IgnoreAuthors  []string
	IgnoreLabels   []string
	StackPatterns  []string
	Mode           string // recorded in the heartbeat
	// Faults is called after each durable step ("append", "print", "state", "replay-state", "replay-print")
	// with the 1-based index of the step; an error stops the watcher as a crash would.
	Faults func(point string, n int) error
}

// CycleResult is what one poll cycle produced.
type CycleResult struct {
	Events  []Event
	RetryIn time.Duration    // a run-wide failure asks for this pause (0: none)
	Errors  map[string]error // restack hook failures, kept for the next cycle
}

// Watcher owns a state directory: lock, journal and state.
type Watcher struct {
	o        Options
	lock     *dirLock
	j        *journal
	st       *State
	broken   error
	baseline bool
}

// Open locks the state directory, replays the journal into the state and re-prints the events that
// may not have reached stdout (at-least-once). Nothing is polled.
func Open(o Options) (*Watcher, error) {
	if o.Observer == nil {
		return nil, errors.New("watch: no observer")
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Interval <= 0 {
		o.Interval = time.Minute
	}
	if o.DetailEvery < 1 {
		o.DetailEvery = 10
	}
	if o.Mode == "" {
		o.Mode = "stream"
	}
	if err := EnsureStateDir(o.Dir); err != nil {
		return nil, err
	}
	lock, err := acquireLock(o.Dir)
	if err != nil {
		return nil, err
	}
	w := &Watcher{o: o, lock: lock, baseline: o.Baseline}
	if err := w.start(); err != nil {
		w.Close()
		return nil, err
	}
	return w, nil
}

func (w *Watcher) start() error {
	j, err := openJournal(w.o.Dir)
	if err != nil {
		return err
	}
	w.j = j
	st, err := loadState(w.o.Dir)
	lost := false
	switch {
	case errors.Is(err, ErrCorruptState):
		if !w.o.Baseline {
			return err
		}
		st, lost = nil, true
	case err != nil:
		return err
	}
	if st == nil && j.Tail() > 0 {
		lost = true
	}
	if st == nil {
		st = newState()
	}
	if st.AppliedSeq > j.Tail() || st.PrintedSeq > j.Tail() {
		if !w.o.Baseline {
			return fmt.Errorf("%w: the journal ends at %d, behind the state", ErrCorruptState, j.Tail())
		}
		st, lost = newState(), true
	}
	w.st = st
	tail := j.Tail()
	evs, err := j.After(st.AppliedSeq)
	if err != nil {
		return err
	}
	for _, ev := range evs { // replay: counters and observed effects, idempotent
		st.apply(ev)
	}
	st.AppliedSeq = tail
	if lost { // the journal rebuilt the counters; the observed truth is unknown: baseline silently
		st.PrintedSeq = tail
		st.Repos, st.Skipped = nil, nil
		w.baseline = true
	}
	if len(evs) > 0 || lost {
		if err := saveState(w.o.Dir, st); err != nil {
			return err
		}
		if err := w.fault("replay-state", 1); err != nil {
			return err
		}
	}
	pending, err := j.After(st.PrintedSeq)
	if err != nil {
		return err
	}
	for i, ev := range pending {
		if err := w.print(ev); err != nil {
			return err
		}
		if err := w.fault("replay-print", i+1); err != nil {
			return err
		}
	}
	if len(pending) > 0 {
		st.PrintedSeq = tail
		if err := saveState(w.o.Dir, st); err != nil {
			return err
		}
	}
	return nil
}

func (w *Watcher) fault(point string, n int) error {
	if w.o.Faults == nil {
		return nil
	}
	return w.o.Faults(point, n)
}

func (w *Watcher) print(ev Event) error {
	var line []byte
	if w.o.Render != nil {
		line = w.o.Render(ev)
	} else {
		raw, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		line = append(raw, '\n')
	}
	_, err := w.o.Out.Write(line)
	return err
}

// Close releases the journal and the lock.
func (w *Watcher) Close() error {
	if w.j != nil {
		w.j.Close()
	}
	if w.lock != nil {
		return w.lock.Close()
	}
	return nil
}

// State returns the in-memory state (read only for callers such as --status).
func (w *Watcher) State() *State { return w.st }

// Cycle polls every repository once and commits in the exact order of the spec: compute the events,
// append each to the journal and fsync, print them, write state.json (applied_seq = printed_seq = the last
// seq of the cycle). After an error the Watcher is unusable: reopen the directory, as after a crash.
func (w *Watcher) Cycle(ctx context.Context) (CycleResult, error) {
	var res CycleResult
	if w.broken != nil {
		return res, w.broken
	}
	c := &cycle{w: w, st: w.st, now: w.o.Clock().UTC().Truncate(time.Second), errs: map[string]error{}}
	c.nowS = c.now.Format(time.RFC3339)
	if err := c.compute(ctx); err != nil {
		w.broken = err
		return res, err
	}
	for i := range c.events {
		ev, err := w.j.Append(c.events[i])
		if err != nil {
			w.broken = err
			return res, err
		}
		c.events[i] = ev
		if err := w.fault("append", i+1); err != nil {
			w.broken = err
			return res, err
		}
	}
	for i, ev := range c.events {
		if err := w.print(ev); err != nil {
			w.broken = err
			return res, err
		}
		if err := w.fault("print", i+1); err != nil {
			w.broken = err
			return res, err
		}
	}
	w.st.AppliedSeq, w.st.PrintedSeq = w.j.Tail(), w.j.Tail()
	if err := saveState(w.o.Dir, w.st); err != nil {
		w.broken = err
		return res, err
	}
	if err := w.fault("state", 1); err != nil {
		w.broken = err
		return res, err
	}
	if err := writeHeartbeat(w.o.Dir, c.now, os.Getpid(), w.o.Mode); err != nil {
		w.broken = err
		return res, err
	}
	res.Events, res.RetryIn = c.events, c.retryIn
	if len(c.errs) > 0 {
		res.Errors = c.errs
	}
	return res, nil
}
