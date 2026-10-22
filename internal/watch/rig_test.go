// SPDX-License-Identifier: EUPL-1.2
package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var errCrash = errors.New("simulated crash")

// sha makes a 40-hex SHA from a short hex seed.
func sha(seed string) string { return strings.Repeat(seed, 40)[:40] }

var rigStart = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// snap is what the fake forge answers for one repository in one cycle.
type snap struct {
	obs RepoObservation
	err error
}

func ok(prs ...PRObs) snap { return snap{obs: RepoObservation{DefaultBranch: "main", PRs: prs}} }

func (s snap) gone(n int, g GoneObs) snap {
	m := map[int]GoneObs{}
	for k, v := range s.obs.Gone {
		m[k] = v
	}
	m[n] = g
	s.obs.Gone = m
	return s
}

func failWith(code string) snap { return snap{err: &ObserveError{Code: code, Message: "boom " + code}} }

// mk builds an open PR: base main, head sha(fmt n), draft false, no detail and no checks read.
func mk(n int, mods ...func(*PRObs)) PRObs {
	p := PRObs{Number: n, Title: fmt.Sprintf("PR %d", n), Author: "alice", Head: sha(fmt.Sprintf("%x1", n)), HeadRef: fmt.Sprintf("feat/%d", n), Base: "main"}
	for _, m := range mods {
		m(&p)
	}
	return p
}

func head(s string) func(*PRObs) { return func(p *PRObs) { p.Head = sha(s) } }
func base(s string) func(*PRObs) { return func(p *PRObs) { p.Base = s } }
func draft() func(*PRObs)        { return func(p *PRObs) { p.Draft = true } }
func author(a string) func(*PRObs) {
	return func(p *PRObs) { p.Author = a }
}
func labels(l ...string) func(*PRObs) { return func(p *PRObs) { p.Labels = l } }
func detail(m *bool, state string) func(*PRObs) {
	return func(p *PRObs) { p.Detail = &DetailObs{Mergeable: m, State: state} }
}
func tt() *bool { b := true; return &b }
func ff() *bool { b := false; return &b }
func runs(rs ...CheckRun) func(*PRObs) {
	return func(p *PRObs) { p.Checks = &ChecksObs{Runs: rs} }
}
func passing() CheckRun { return CheckRun{Name: "ci", Status: "completed", Conclusion: "success"} }
func running() CheckRun { return CheckRun{Name: "ci", Status: "in_progress"} }
func failing(name, text string) CheckRun {
	return CheckRun{Name: name, Status: "completed", Conclusion: "failure", Summary: text}
}
func merged(commit string, headSHA string) GoneObs {
	var c *string
	if commit != "" {
		c = &commit
	}
	return GoneObs{Merged: true, MergeCommit: c, MergedAt: "2026-10-22T09:30:00Z", Base: "main", Head: headSHA, HeadRef: "feat/x", MethodHint: "rebase-or-squash"}
}
func closedGone(headSHA string) GoneObs { return GoneObs{Base: "main", Head: headSHA} }

type fakeObserver struct {
	cur   map[string]snap
	hints map[string]Hints // the hints of the last Observe per repository
}

func (f *fakeObserver) Observe(ctx context.Context, repo string, h Hints) (RepoObservation, error) {
	if f.hints == nil {
		f.hints = map[string]Hints{}
	}
	f.hints[repo] = h
	s, ok := f.cur[repo]
	if !ok {
		return RepoObservation{}, &ObserveError{Code: "network", Message: "no fixture"}
	}
	return s.obs, s.err
}

// fakeRestacker selects children by the two classes of the spec, from the data the core hands it.
type fakeRestacker struct {
	calls   int
	byMerge map[int][]RestackCandidate
	err     error
}

func (f *fakeRestacker) Candidates(ctx context.Context, repo string, due RestackDue, open []OpenPR) ([]RestackCandidate, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.byMerge[due.MergedPR], nil
}

// rig drives a Watcher over a state directory with a fake clock; "crash" is Close plus a new Open.
type rig struct {
	t       *testing.T
	dir     string
	repos   []string
	skipped []Skipped
	obs     *fakeObserver
	rs      *fakeRestacker
	out     *bytes.Buffer
	w       *Watcher
	now     time.Time
	faults  func(point string, n int) error
	timeout time.Duration
	base    bool
	ignoreA []string
	ignoreL []string
}

func newRig(t *testing.T, repos ...string) *rig {
	t.Helper()
	if len(repos) == 0 {
		repos = []string{"o/r"}
	}
	return &rig{t: t, dir: filepath.Join(t.TempDir(), "state"), repos: repos, obs: &fakeObserver{cur: map[string]snap{}},
		rs: &fakeRestacker{byMerge: map[int][]RestackCandidate{}}, out: &bytes.Buffer{}, now: rigStart, timeout: 45 * time.Minute}
}

func (r *rig) options() Options {
	return Options{Dir: r.dir, Host: "github.com", Repos: r.repos, Skipped: r.skipped, Observer: r.obs, Restacker: r.rs,
		Clock: func() time.Time { return r.now }, Out: r.out, Interval: time.Minute, PendingTimeout: r.timeout,
		Mode: "once", Faults: func(p string, n int) error {
			if r.faults != nil {
				return r.faults(p, n)
			}
			return nil
		}, Baseline: r.base, IgnoreAuthors: r.ignoreA, IgnoreLabels: r.ignoreL}
}

// open (re)opens the watcher: a crashed process is gone, only the directory survives.
func (r *rig) open() error {
	if r.w != nil {
		r.w.Close()
		r.w = nil
	}
	w, err := Open(r.options())
	if err != nil {
		return err
	}
	r.w = w
	return nil
}

func (r *rig) mustOpen() {
	r.t.Helper()
	if err := r.open(); err != nil {
		r.t.Fatalf("open: %v", err)
	}
}

// set installs the fake forge answers of the next cycle.
func (r *rig) set(repo string, s snap) { r.obs.cur[repo] = s }

// cycle sets the answers for the single repository "o/r", advances the clock a minute and runs one cycle.
func (r *rig) cycle(s snap) ([]Event, error) {
	r.set("o/r", s)
	return r.run()
}

func (r *rig) run() ([]Event, error) {
	r.now = r.now.Add(time.Minute)
	if r.w == nil {
		if err := r.open(); err != nil {
			return nil, err
		}
	}
	res, err := r.w.Cycle(context.Background())
	return res.Events, err
}

func (r *rig) must(s snap) []Event {
	r.t.Helper()
	evs, err := r.cycle(s)
	if err != nil {
		r.t.Fatalf("cycle: %v", err)
	}
	return evs
}

func (r *rig) journal() []Event {
	r.t.Helper()
	raw, err := os.ReadFile(filepath.Join(r.dir, "journal.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		r.t.Fatal(err)
	}
	var out []Event
	for _, l := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if l == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			r.t.Fatalf("journal line %q: %v", l, err)
		}
		out = append(out, e)
	}
	return out
}

func (r *rig) journalIDs() []string {
	var ids []string
	for _, e := range r.journal() {
		ids = append(ids, e.ID)
	}
	return ids
}

func (r *rig) stdoutIDs() []string {
	var ids []string
	for _, l := range strings.Split(strings.TrimSpace(r.out.String()), "\n") {
		if l == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			r.t.Fatalf("stdout line %q: %v", l, err)
		}
		ids = append(ids, e.ID)
	}
	return ids
}

func (r *rig) state() *State {
	r.t.Helper()
	s, err := loadState(r.dir)
	if err != nil || s == nil {
		r.t.Fatalf("state: %v %v", s, err)
	}
	return s
}

func kinds(evs []Event) []string {
	var k []string
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

func ids(evs []Event) []string {
	var k []string
	for _, e := range evs {
		k = append(k, e.ID)
	}
	return k
}

// one asserts that evs is exactly one event of the kind and returns it.
func one(t *testing.T, evs []Event, kind string) Event {
	t.Helper()
	if len(evs) != 1 || evs[0].Kind != kind {
		t.Fatalf("want exactly one %s, got %v", kind, ids(evs))
	}
	return evs[0]
}

func none(t *testing.T, evs []Event) {
	t.Helper()
	if len(evs) != 0 {
		t.Fatalf("want no events, got %v", ids(evs))
	}
}

func uniq(t *testing.T, list []string) {
	t.Helper()
	seen := map[string]bool{}
	for _, id := range list {
		if seen[id] {
			t.Fatalf("duplicate id %s in %v", id, list)
		}
		seen[id] = true
	}
}

func errorsAs[T error](err error, target *T) bool { return errors.As(err, target) }
