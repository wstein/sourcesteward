// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// fakeClock is a clock the test moves by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)} }

func ev(repo string, idx, count int, ph workspace.ObservePhase, done, total int) workspace.ObserveEvent {
	return workspace.ObserveEvent{Repo: repo, Index: idx, Count: count, Phase: ph, Done: done, Total: total}
}

func TestTidyProgressLiveLineOverwritesOneLine(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressLive, clk.now)
	clk.advance(38 * time.Second)
	p.Event(ev("sourcesteward", 3, 7, workspace.ObservePhaseWorktrees, 412, 1098))
	want := "\r[3/7] sourcesteward: observing worktrees 412/1,098 (38s)"
	if got := out.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// A shorter line blanks what the longer one left; no escape sequence, no newline.
	clk.advance(time.Second)
	p.Event(ev("a", 1, 7, workspace.ObservePhaseRefs, 0, 0))
	s := out.String()
	if strings.ContainsRune(s, 0x1b) || strings.Contains(s, "\n") {
		t.Fatalf("escape or newline in %q", s)
	}
	short := "\r[1/7] a: reading references (39s)"
	if !strings.Contains(s, short+strings.Repeat(" ", len(want)-len(short))) {
		t.Fatalf("the longer line is not blanked: %q", s)
	}
	out.Reset()
	p.Finish()
	if got := out.String(); !strings.HasPrefix(got, "\r ") || !strings.HasSuffix(got, "\r") || strings.TrimSpace(got) != "" {
		t.Fatalf("finish %q", got)
	}
	out.Reset()
	p.Finish()
	if out.Len() != 0 {
		t.Fatalf("a second finish wrote %q", out.String())
	}
}

func TestTidyProgressLiveLineIsThrottled(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressLive, clk.now)
	for i := 1; i <= 100; i++ {
		clk.advance(time.Millisecond)
		p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, i, 100))
	}
	if n := strings.Count(out.String(), "\r"); n != 1 {
		t.Fatalf("%d redraws within 100 ms", n)
	}
	clk.advance(tidyProgressLiveEvery)
	p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, 101, 101))
	if n := strings.Count(out.String(), "\r"); n != 2 {
		t.Fatalf("%d redraws after the interval", n)
	}
}

func TestTidyProgressOffWritesNothing(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressOff, clk.now)
	p.Event(ev("a", 1, 1, workspace.ObservePhaseStart, 0, 0))
	clk.advance(time.Hour)
	p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, 1, 2))
	p.Finish()
	if out.Len() != 0 {
		t.Fatalf("wrote %q", out.String())
	}
}

func TestTidyProgressPlainWritesWholeLinesNowAndThen(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressPlain, clk.now)
	p.Event(ev("a", 1, 2, workspace.ObservePhaseStart, 0, 0))
	for i := 1; i <= 50; i++ {
		clk.advance(time.Second)
		p.Event(ev("a", 1, 2, workspace.ObservePhaseWorktrees, i, 50))
	}
	p.Finish()
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	// The start line, then one line per interval (50 s at 10 s: five lines).
	if len(lines) != 6 {
		t.Fatalf("%d lines: %q", len(lines), lines)
	}
	if lines[0] != "[1/2] a: starting (0ms)" || lines[5] != "[1/2] a: observing worktrees 50/50 (50s)" {
		t.Fatalf("lines %q", lines)
	}
	if strings.ContainsAny(out.String(), "\r\x1b") {
		t.Fatalf("control characters in %q", out.String())
	}
}

func TestTidyProgressEscapesTheRepositoryName(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressPlain, clk.now)
	p.Event(ev("evil\x1b[2J\rname", 1, 1, workspace.ObservePhaseStart, 0, 0))
	p.Summary()
	if strings.ContainsRune(out.String(), 0x1b) || strings.Contains(out.String(), "\rname") {
		t.Fatalf("unescaped name: %q", out.String())
	}
}

func TestTidyProgressSummaryTimingsCountsAndWindow(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressOff, clk.now)
	p.Prepared(1200 * time.Millisecond)
	// Two repositories at the same time; the second one reports its window.
	p.Event(ev("b", 2, 2, workspace.ObservePhaseStart, 0, 0))
	p.Event(ev("a", 1, 2, workspace.ObservePhaseStart, 0, 0))
	clk.advance(2 * time.Second)
	p.Event(ev("a", 1, 2, workspace.ObservePhaseRefs, 0, 0))
	clk.advance(3 * time.Second)
	p.Event(ev("a", 1, 2, workspace.ObservePhaseWorktrees, 0, 1200))
	clk.advance(62 * time.Second)
	p.Event(ev("a", 1, 2, workspace.ObservePhaseWorktrees, 1200, 1200))
	p.Event(ev("a", 1, 2, workspace.ObservePhaseProofs, 0, 40))
	clk.advance(500 * time.Millisecond)
	p.Event(workspace.ObserveEvent{Repo: "b", Index: 2, Count: 2, Phase: workspace.ObservePhaseWindow, Note: "proofs"})
	p.Event(workspace.ObserveEvent{Repo: "a", Index: 1, Count: 2, Phase: workspace.ObservePhaseDone, Note: "analysed"})
	p.Event(workspace.ObserveEvent{Repo: "b", Index: 2, Count: 2, Phase: workspace.ObservePhaseDone, Note: "analysed"})
	p.Summary()
	want := `Timings: total 1m07s
  discovery and references: 1.2s
  [1/2] a: analysed in 1m07s
    reading references: 3.0s
    observing worktrees 1,200/1,200: 1m02s
    proving integration 0/40: 500ms
  [2/2] b: analysed in 1m07s
  observation window reached: b (proofs); raise --timeout or narrow the run with --member or --only
`
	if got := out.String(); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestTidyProgressSummaryWithoutAWindow(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressOff, clk.now)
	p.Summary()
	if got := out.String(); got != "Timings: total 0ms\n  observation window: not reached\n" {
		t.Fatalf("got %q", got)
	}
}

func TestShortDurationAndGroupDigits(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-time.Second: "0ms", 0: "0ms", 850 * time.Millisecond: "850ms", 3400 * time.Millisecond: "3.4s",
		38 * time.Second: "38s", 59*time.Second + 900*time.Millisecond: "59s", 125 * time.Second: "2m05s", 3600 * time.Second: "60m00s",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
	for n, want := range map[int]string{0: "0", 7: "7", 999: "999", 1000: "1,000", 1098: "1,098", 1234567: "1,234,567", -1234: "-1,234"} {
		if got := groupDigits(n); got != want {
			t.Errorf("%d: %q, want %q", n, got, want)
		}
	}
}

// The workers count before they report, so a report with a smaller count can arrive last: the
// shown and summarized count never goes backwards.
func TestTidyProgressCountNeverGoesBackwards(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressOff, clk.now)
	p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, 0, 9))
	p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, 5, 9))
	p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, 3, 9))
	p.Summary()
	if !strings.Contains(out.String(), "observing worktrees 5/9") {
		t.Fatalf("%s", out.String())
	}
}

// blockedWriter holds every write until it is released.
type blockedWriter struct {
	started chan struct{}
	release chan struct{}
}

func (w *blockedWriter) Write(b []byte) (int, error) {
	w.started <- struct{}{}
	<-w.release
	return len(b), nil
}

// A terminal that does not accept output holds up the one worker that writes to it; the other
// workers drop their frames and go on, and the bookkeeping is not blocked.
func TestTidyProgressStuckTerminalDoesNotStallTheOtherWorkers(t *testing.T) {
	clk := newClock()
	w := &blockedWriter{started: make(chan struct{}, 1), release: make(chan struct{})}
	p := newTidyProgress(w, tidyProgressLive, clk.now)
	done := make(chan struct{})
	go func() { p.Event(ev("a", 1, 2, workspace.ObservePhaseWorktrees, 1, 9)); close(done) }()
	<-w.started // the first worker is inside the blocked write
	clk.advance(time.Second)
	p.Event(ev("b", 2, 2, workspace.ObservePhaseWorktrees, 4, 9)) // returns at once: frame dropped
	p.Git(workspace.GitCall{Repo: "b", Argv: []string{"x"}})
	close(w.release)
	<-done
	p.mu.Lock()
	got := p.repos["b"].phases[0].done
	p.mu.Unlock()
	if got != 4 {
		t.Fatalf("the bookkeeping of the second worker was lost: %d", got)
	}
}

// The Git processes of the discovery belong to no repository: they are counted with the
// discovery, and no repository row is made for them.
func TestTidyProgressDiscoveryCallsGetNoRepositoryRow(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressOff, clk.now)
	p.Prepared(time.Second)
	p.Git(workspace.GitCall{Argv: []string{"rev-parse"}, Duration: 2 * time.Millisecond})
	p.Summary()
	want := "Timings: total 0ms\n  discovery and references: 1.0s (git processes: 1, 2ms in total)\n  observation window: not reached\n"
	if out.String() != want {
		t.Fatalf("%q", out.String())
	}
}
