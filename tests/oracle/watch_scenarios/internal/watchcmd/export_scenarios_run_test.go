// SPDX-License-Identifier: EUPL-1.2
package watchcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/hosting"
	"github.com/wstein/sourcesteward/internal/watchexport"
)

type rWorldAt struct {
	AfterSleeps int    `json:"after_sleeps"`
	World       wWorld `json:"world"`
}

type rInvocation struct {
	Note              string     `json:"note"`
	Action            string     `json:"action"` // run (default) or status
	Mode              string     `json:"mode"`
	UntilKinds        []string   `json:"until_kinds"`
	TimeoutSeconds    int        `json:"timeout_seconds"`
	MaxIdleSeconds    int        `json:"max_idle_seconds"`
	Human             bool       `json:"human"`
	Baseline          bool       `json:"baseline"`
	AdvanceSeconds    int        `json:"advance_seconds"`
	Worlds            []rWorldAt `json:"worlds"`
	CancelAfterSleeps int        `json:"cancel_after_sleeps"`
	StateDirKind      string     `json:"state_dir_kind"` // "" (a fresh private directory, kept across invocations), relative, inside-work-tree, wide-mode
	StateFile         string     `json:"state_file"`     // "", corrupt-state
	NoToken           bool       `json:"reader_cannot_open"`
	Json              bool       `json:"status_json"`
	StatusAtOffset    int        `json:"status_now_offset_seconds"`
}

type rEvent struct {
	Kind    string          `json:"kind"`
	Repo    *string         `json:"repo"`
	PR      *int            `json:"pr"`
	Time    string          `json:"time"`
	Details json.RawMessage `json:"details"`
	Go      xGo             `json:"go"`
}

type xGo struct {
	Seq int64 `json:"seq"`
}

type rOut struct {
	Invocation    int      `json:"invocation"`
	ExitCode      int      `json:"exit_code"`
	ErrorContains []string `json:"error_mentions"`
	Events        []rEvent `json:"stdout_events"`
	Lines         []string `json:"stdout_lines"`
	SleepsSeconds []int    `json:"sleeps_seconds"`
	StatusFresh   *bool    `json:"status_fresh"`
	StatusText    []string `json:"status_text"`
}

type rConfig struct {
	Repos         []string `json:"repos"`
	Host          string   `json:"host"`
	IntervalSecs  int      `json:"interval_seconds"`
	DetailEvery   int      `json:"detail_every"`
	PendingSecs   int      `json:"pending_timeout_seconds"`
	MaxOpenPRs    int      `json:"max_open_prs"`
	JitterSeconds int      `json:"jitter_seconds"`
	StartTime     string   `json:"start_time"`
}

type rDoc struct {
	watchexport.Common
	Config      rConfig       `json:"config"`
	Invocations []rInvocation `json:"invocations"`
	Expected    []rOut        `json:"expected"`
}

type rScript struct {
	t    *testing.T
	doc  *rDoc
	r    *runRig
	dir  string
	n    int
	fake *fakeReader
}

func newRScript(t *testing.T, name, desc string, tests ...string) *rScript {
	r := newRunRig(t, ModeOnce)
	s := &rScript{t: t, r: r, dir: r.p.StateDir}
	s.doc = &rDoc{Common: watchexport.NewCommon(name, watchexport.KindRunLoop, desc, tests...), Invocations: []rInvocation{}, Expected: []rOut{},
		Config: rConfig{Repos: []string{"o/r"}, Host: "github.com", IntervalSecs: 60, DetailEvery: 10, PendingSecs: 2700, MaxOpenPRs: 2000, JitterSeconds: 0, StartTime: r.now.Format(time.RFC3339)}}
	s.doc.Common.FormatDependent = []string{"go", "status_text"}
	return s
}

// world helper: the standard fake with tips and details as newRunRig sets them, edited by f.
func (s *rScript) world(f func(*fakeReader)) wWorld {
	fk := newFake()
	fk.tips["main"] = sha40("a")
	for n := 1; n <= 9; n++ {
		fk.details[n] = detailJSON
	}
	f(fk)
	return worldOf(fk, s.r.now)
}

func (s *rScript) do(inv rInvocation) *rOut {
	r := s.r
	s.n++
	if inv.Action == "" {
		inv.Action = "run"
	}
	if inv.UntilKinds == nil {
		inv.UntilKinds = []string{}
	}
	if inv.Worlds == nil {
		inv.Worlds = []rWorldAt{}
	}
	out := rOut{Invocation: s.n, Events: []rEvent{}, Lines: []string{}, SleepsSeconds: []int{}, ErrorContains: []string{}}
	r.now = r.now.Add(time.Duration(inv.AdvanceSeconds) * time.Second)
	r.out.Reset()
	r.sleeps = nil
	// state directory
	stateDir := r.p.StateDir
	switch inv.StateDirKind {
	case "relative":
		stateDir = "relative/state"
	case "inside-work-tree":
		repo := s.t.TempDir()
		os.Mkdir(filepath.Join(repo, ".git"), 0o755)
		stateDir = filepath.Join(repo, "state")
	case "wide-mode":
		stateDir = filepath.Join(s.t.TempDir(), "state")
		os.MkdirAll(stateDir, 0o755)
		os.Chmod(stateDir, 0o755)
	}
	if inv.StateFile == "corrupt-state" {
		os.MkdirAll(stateDir, 0o700)
		os.WriteFile(filepath.Join(stateDir, "state.json"), []byte("{broken"), 0o600)
	}
	if inv.Action == "status" {
		var buf bytes.Buffer
		err := RunStatus(StatusParams{StateDir: stateDir, JSON: inv.Json, Out: &buf, Now: func() time.Time { return r.now.Add(time.Duration(inv.StatusAtOffset) * time.Second) }})
		out.ExitCode = code(err)
		if err != nil && out.ExitCode == -1 {
			s.t.Fatal(err)
		}
		if inv.Json {
			var m map[string]any
			if json.Unmarshal(buf.Bytes(), &m) == nil {
				f, _ := m["fresh"].(bool)
				out.StatusFresh = &f
			}
		} else {
			out.StatusText = []string{}
			for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
				l = strings.ReplaceAll(l, stateDir, "<state_dir>")
				if i := strings.Index(l, "(pid "); i >= 0 {
					l = l[:i] + "(pid <pid>)"
				}
				out.StatusText = append(out.StatusText, l)
			}
		}
		s.doc.Invocations = append(s.doc.Invocations, inv)
		s.doc.Expected = append(s.doc.Expected, out)
		return &s.doc.Expected[len(s.doc.Expected)-1]
	}
	// run
	r.p.Mode = inv.Mode
	r.p.UntilKinds = map[string]bool{}
	for _, k := range inv.UntilKinds {
		r.p.UntilKinds[k] = true
	}
	r.p.Timeout = time.Duration(inv.TimeoutSeconds) * time.Second
	r.p.MaxIdle = time.Duration(inv.MaxIdleSeconds) * time.Second
	r.p.Human, r.p.Baseline = inv.Human, inv.Baseline
	r.p.StateDir = stateDir
	if len(inv.Worlds) == 0 {
		s.t.Fatalf("invocation %d has no world", s.n)
	}
	r.f = fakeOf(inv.Worlds[0].World, r.now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.script = func(i int) {
		for _, w := range inv.Worlds {
			if w.AfterSleeps == i && i > 0 {
				*r.f = *fakeOf(w.World, r.now)
			}
		}
		if inv.CancelAfterSleeps > 0 && i == inv.CancelAfterSleeps {
			cancel()
		}
	}
	r.p.OpenReader = func(context.Context, string, string) (Reader, func(), error) {
		if inv.NoToken {
			return nil, func() {}, errors.New("no token")
		}
		return r.f, func() {}, nil
	}
	err := r.run(ctx)
	r.p.StateDir = s.dir // the private directory of the rig survives every invocation
	out.ExitCode = code(err)
	if err != nil {
		msg := err.Error()
		for _, w := range []string{"hosting-api-unavailable", "--baseline", "interrupted", "no repository could be read", "no event of the requested kinds", "pid", "absolute"} {
			if strings.Contains(msg, w) {
				out.ErrorContains = append(out.ErrorContains, w)
			}
		}
	}
	for _, l := range strings.Split(strings.TrimSpace(r.out.String()), "\n") {
		if l == "" {
			continue
		}
		if inv.Human {
			out.Lines = append(out.Lines, l)
			continue
		}
		var e struct {
			Seq     int64           `json:"seq"`
			Time    string          `json:"time"`
			Repo    *string         `json:"repo"`
			PR      *int            `json:"pr"`
			Kind    string          `json:"kind"`
			Details json.RawMessage `json:"details"`
		}
		if json.Unmarshal([]byte(l), &e) != nil {
			s.t.Fatalf("not JSON: %q", l)
		}
		out.Events = append(out.Events, rEvent{Kind: e.Kind, Repo: e.Repo, PR: e.PR, Time: e.Time, Details: e.Details, Go: xGo{Seq: e.Seq}})
	}
	for _, d := range r.sleeps {
		out.SleepsSeconds = append(out.SleepsSeconds, int(d/time.Second))
	}
	s.doc.Invocations = append(s.doc.Invocations, inv)
	s.doc.Expected = append(s.doc.Expected, out)
	return &s.doc.Expected[len(s.doc.Expected)-1]
}

func (s *rScript) must(cond bool, msg string) {
	s.t.Helper()
	if !cond {
		s.t.Fatalf("%s: translation guard: %s", s.doc.Name, msg)
	}
}

func (s *rScript) write(dir string) {
	if err := watchexport.Write(dir, s.doc.Name, s.doc); err != nil {
		s.t.Fatal(err)
	}
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func exportRunLoops(t *testing.T, dir string) {
	items := func(it ...hosting.PullItem) func(*fakeReader) { return func(f *fakeReader) { f.items = it } }
	m := func(a, b func(*fakeReader)) func(*fakeReader) { return func(f *fakeReader) { a(f); b(f) } }
	_ = m
	kinds := func(o *rOut) string {
		var k []string
		for _, e := range o.Events {
			k = append(k, e.Kind)
		}
		return strings.Join(k, " ")
	}
	{
		s := newRScript(t, "run-once-baseline-then-transitions", "`--once`: the first run is a silent baseline (exit 0, no output); the next prints only the transition; `--baseline` after a state exists prints nothing and replaces the state.",
			"watchcmd/run_test.go:TestOnceBaselineIsSilentThenPrintsOnlyTransitions")
		w := func(it ...hosting.PullItem) []rWorldAt { return []rWorldAt{{0, s.world(items(it...))}} }
		o := s.do(rInvocation{Mode: ModeOnce, Worlds: w(item(1, "main"))})
		s.must(o.ExitCode == 0 && len(o.Events) == 0, "first run")
		o = s.do(rInvocation{Mode: ModeOnce, AdvanceSeconds: 60, Worlds: w(item(1, "main"), item(2, "main"))})
		s.must(o.ExitCode == 0 && kinds(o) == "new-pr", "second run")
		o = s.do(rInvocation{Mode: ModeOnce, Baseline: true, AdvanceSeconds: 60, Worlds: w(item(1, "main"), item(2, "main"), item(3, "main"))})
		s.must(o.ExitCode == 0 && len(o.Events) == 0, "forced baseline")
		o = s.do(rInvocation{Mode: ModeOnce, AdvanceSeconds: 60, Worlds: w(item(1, "main"), item(2, "main"), item(3, "main"))})
		s.must(o.ExitCode == 0 && len(o.Events) == 0, "after the baseline")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-until-event-matches-a-kind", "`--until-event`: exits 0 as soon as a poll printed an event of a requested kind; the wait between polls is the interval.",
			"watchcmd/run_test.go:TestUntilEventExitsZeroOnAMatchingKind")
		o := s.do(rInvocation{Mode: ModeUntil, UntilKinds: []string{"checks-green", "new-pr"}, TimeoutSeconds: 590, Worlds: []rWorldAt{
			{0, s.world(items(item(1, "main")))}, {2, s.world(items(item(1, "main"), item(2, "main")))}}})
		s.must(o.ExitCode == 0 && kinds(o) == "new-pr" && len(o.SleepsSeconds) == 2, "until match")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-until-event-timeout-exits-four", "`--until-event` without a matching event ends exactly at the timeout with exit 4 and no output; the last sleep is shortened to the remaining time.",
			"watchcmd/run_test.go:TestUntilEventTimeoutExitsFourWithoutOutput")
		o := s.do(rInvocation{Mode: ModeUntil, UntilKinds: []string{"merged"}, TimeoutSeconds: 300, Worlds: []rWorldAt{{0, s.world(items(item(1, "main")))}}})
		s.must(o.ExitCode == 4 && len(o.Events) == 0 && sum(o.SleepsSeconds) == 300, "timeout")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-until-event-prints-the-whole-poll", "`--until-event` matching a kind still prints every event of that poll before it exits.",
			"watchcmd/run_test.go:TestUntilEventMatchesAfterThePollThatPrintedIt")
		a, b := item(1, "main"), item(2, "main")
		a.HeadSHA, b.HeadSHA = sha40("c"), sha40("d")
		o := s.do(rInvocation{Mode: ModeUntil, UntilKinds: []string{"head-changed"}, TimeoutSeconds: 3600, Worlds: []rWorldAt{
			{0, s.world(items(item(1, "main"), item(2, "main")))}, {1, s.world(items(a, b, item(3, "main")))}}})
		s.must(o.ExitCode == 0 && kinds(o) == "head-changed head-changed new-pr" && len(o.SleepsSeconds) == 1, "whole poll")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-stream-ends-on-interrupt", "A `--stream` interrupted (SIGINT/SIGTERM) ends with exit 0.", "watchcmd/run_test.go:TestStreamEndsOnInterruptAndOnMaxIdle")
		o := s.do(rInvocation{Mode: ModeStream, CancelAfterSleeps: 3, Worlds: []rWorldAt{{0, s.world(items(item(1, "main")))}}})
		s.must(o.ExitCode == 0, "interrupt")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-stream-ends-on-max-idle", "A `--stream` with `--max-idle 150s` and no event ends with exit 0 after exactly 150 s of sleeping.", "watchcmd/run_test.go:TestStreamEndsOnInterruptAndOnMaxIdle")
		o := s.do(rInvocation{Mode: ModeStream, MaxIdleSeconds: 150, Worlds: []rWorldAt{{0, s.world(items(item(1, "main")))}}})
		s.must(o.ExitCode == 0 && sum(o.SleepsSeconds) == 150, "max idle")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-usage-and-failure-exit-codes", "Exit codes before any read: a relative state directory and a state directory inside a Git work tree are usage errors (2, nothing created); a state directory with a wide mode, a reader that cannot open (no token: `hosting-api-unavailable`, before any request) and a corrupt state (never silently reset, the message names --baseline) are failures (1); `--baseline` repairs the corrupt state.",
			"watchcmd/run_test.go:TestUsageAndFailureExitCodes")
		w := []rWorldAt{{0, s.world(items(item(1, "main")))}}
		o := s.do(rInvocation{Mode: ModeOnce, StateDirKind: "relative", Worlds: w})
		s.must(o.ExitCode == 2, "relative")
		o = s.do(rInvocation{Mode: ModeOnce, StateDirKind: "inside-work-tree", Worlds: w})
		s.must(o.ExitCode == 2, "work tree")
		o = s.do(rInvocation{Mode: ModeOnce, StateDirKind: "wide-mode", Worlds: w})
		s.must(o.ExitCode == 1, "wide mode")
		o = s.do(rInvocation{Mode: ModeOnce, NoToken: true, Worlds: w})
		s.must(o.ExitCode == 1 && len(o.ErrorContains) > 0 && o.ErrorContains[0] == "hosting-api-unavailable", "no token")
		o = s.do(rInvocation{Mode: ModeOnce, StateFile: "corrupt-state", Worlds: w})
		s.must(o.ExitCode == 1 && strings.Join(o.ErrorContains, ",") == "--baseline", "corrupt")
		o = s.do(rInvocation{Mode: ModeOnce, StateFile: "corrupt-state", Baseline: true, Worlds: w})
		s.must(o.ExitCode == 0, "baseline repairs")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-once-fails-only-when-no-repository-could-be-read", "`--once` exits 1 only when no repository could be read at all; the failure is still data on stdout (a watch-error event); a readable repository exits 0.",
			"watchcmd/run_test.go:TestOnceFailsOnlyWhenNoRepositoryCouldBeRead")
		bad := s.world(func(f *fakeReader) {
			f.listErr = &hosting.WatchError{Code: hosting.CodeNetwork, Message: "HTTP read failed"}
		})
		o := s.do(rInvocation{Mode: ModeOnce, Worlds: []rWorldAt{{0, bad}}})
		s.must(o.ExitCode == 1 && kinds(o) == "watch-error", "no repository")
		o = s.do(rInvocation{Mode: ModeOnce, StateDirKind: "", AdvanceSeconds: 60, Worlds: []rWorldAt{{0, s.world(items(item(1, "main")))}}})
		s.must(o.ExitCode == 0, "readable")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-human-output-and-slowdown", "`--human` prints the human line; a rate limit below 10 percent doubles the wait between polls.", "watchcmd/run_test.go:TestHumanOutputAndSlowdown")
		w0 := s.world(func(f *fakeReader) {
			f.items = []hosting.PullItem{item(1, "main")}
			f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 100, Reset: s.r.now.Add(time.Hour)}
		})
		w1 := s.world(func(f *fakeReader) {
			f.items = []hosting.PullItem{item(1, "main"), item(2, "main")}
			f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 100, Reset: s.r.now.Add(time.Hour)}
		})
		o := s.do(rInvocation{Mode: ModeStream, Human: true, MaxIdleSeconds: 600, Worlds: []rWorldAt{{0, w0}, {1, w1}}})
		s.must(len(o.Lines) == 1 && strings.HasPrefix(o.Lines[0], "09:02:00 o/r#2 new-pr PR 2 by alice into main") && o.SleepsSeconds[0] == 120, "human")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-rate-limit-holds-the-whole-run", "An exhausted rate limit holds the whole run until the reset: one run-level watch-error rate-limited (repo null) and a sleep of at least 9 minutes.", "watchcmd/run_test.go:TestRateLimitedHoldsTheWholeRun")
		w := s.world(func(f *fakeReader) {
			f.items = []hosting.PullItem{item(1, "main")}
			f.rate = hosting.RateInfo{Known: true, Limit: 5000, Remaining: 20, Reset: s.r.now.Add(10 * time.Minute)}
		})
		o := s.do(rInvocation{Mode: ModeStream, MaxIdleSeconds: 720, Worlds: []rWorldAt{{0, w}}})
		held := false
		for _, d := range o.SleepsSeconds {
			held = held || d >= 540
		}
		s.must(o.ExitCode == 0 && held && len(o.Events) > 0 && o.Events[0].Kind == "watch-error" && o.Events[0].Repo == nil, "rate limit hold")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-interrupted-until-event-is-a-failure", "An interrupted `--until-event` exits 1 (interrupted) and never looks like a match.", "watchcmd/run_test.go:TestInterruptedUntilEventIsAFailureAndNeverAMatch")
		o := s.do(rInvocation{Mode: ModeUntil, UntilKinds: []string{"merged"}, TimeoutSeconds: 3600, CancelAfterSleeps: 2, Worlds: []rWorldAt{{0, s.world(items(item(1, "main")))}}})
		s.must(o.ExitCode == 1 && len(o.ErrorContains) > 0 && o.ErrorContains[0] == "interrupted", "interrupted")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-restack-needed-end-to-end", "A merge read through the observer produces restack-needed for the child on the merged branch (class 1) and for the PR whose commits contain the old head (class 2), not for the unrelated PR; the event carries old parent, new parent, merged PR and Go's argv (owner decision F1).",
			"watchcmd/run_test.go:TestRestackNeededEventsAreProducedEndToEnd")
		w0 := s.world(func(f *fakeReader) {
			f.tips["feat/1"] = sha40("f")
			f.items = []hosting.PullItem{item(1, "main"), item(2, "feat/1"), item(3, "main"), item(4, "main")}
		})
		o := s.do(rInvocation{Mode: ModeOnce, Worlds: []rWorldAt{{0, w0}}})
		s.must(o.ExitCode == 0 && len(o.Events) == 0, "baseline")
		merge := sha40("e")
		w1 := s.world(func(f *fakeReader) {
			f.tips["feat/1"] = sha40("f")
			f.details[1] = `{"state":"closed","merged":true,"merge_commit_sha":"` + merge + `","merged_at":"2026-10-22T08:00:00Z","base":{"ref":"main"},"head":{"sha":"` + sha40("1") + `","ref":"feat/1"}}`
			f.commits[merge] = `{"parents":[{"sha":"x"}]}`
			f.pulls[3] = `[{"sha":"` + sha40("1") + `"},{"sha":"` + sha40("3") + `"}]`
			f.pulls[4] = `[{"sha":"` + sha40("4") + `"}]`
			f.items = []hosting.PullItem{item(2, "feat/1"), item(3, "main"), item(4, "main")}
		})
		o = s.do(rInvocation{Mode: ModeOnce, AdvanceSeconds: 60, Worlds: []rWorldAt{{0, w1}}})
		s.must(o.ExitCode == 0 && kinds(o) == "merged restack-needed restack-needed", "restack")
		s.write(dir)
	}
	{
		s := newRScript(t, "run-status-fresh-stale-missing", "`--status`: fresh within the window (exit 0), stale (exit 5, JSON fresh=false), missing heartbeat (exit 5), a relative directory (exit 2).",
			"watchcmd/run_test.go:TestRunStatus")
		o := s.do(rInvocation{Mode: ModeOnce, Worlds: []rWorldAt{{0, s.world(items(item(1, "main")))}}})
		s.must(o.ExitCode == 0, "run")
		o = s.do(rInvocation{Action: "status", StatusAtOffset: 60})
		s.must(o.ExitCode == 0, "fresh")
		o = s.do(rInvocation{Action: "status", Json: true, StatusAtOffset: 240})
		s.must(o.ExitCode == 5 && o.StatusFresh != nil && !*o.StatusFresh, "stale")
		o = s.do(rInvocation{Action: "status", StateDirKind: "relative"})
		s.must(o.ExitCode == 2, "relative")
		s.write(dir)
	}
	_ = fmt.Sprint
	_ = sort.Strings
}
