// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/workspace"
)

// fakeScreen is a terminal that records every byte it is given and interprets the few sequences the
// block uses: carriage return, newline, cursor up (CSI n A), erase to the end of the line (CSI K),
// erase below (CSI J) and the colors (CSI m, which it drops). Any other escape sequence fails the
// test through unknown. A line longer than the width wraps like a real terminal, which is what
// the truncation must prevent.
type fakeScreen struct {
	cols, rows int
	grid       [][]rune
	row, col   int
	raw        bytes.Buffer
	unknown    []string
	wraps      int
}

func newFakeScreen(cols, rows int) *fakeScreen {
	return &fakeScreen{cols: cols, rows: rows, grid: [][]rune{{}}}
}

func (s *fakeScreen) Write(b []byte) (int, error) { s.feed(string(b)); return len(b), nil }

func (s *fakeScreen) feed(text string) {
	s.raw.WriteString(text)
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\r':
			s.col = 0
		case r == '\n':
			s.row++
			s.col = 0
			s.ensure()
		case r == 0x1b:
			j := i + 1
			if j >= len(rs) || rs[j] != '[' {
				s.unknown = append(s.unknown, "ESC")
				continue
			}
			k := j + 1
			for k < len(rs) && (rs[k] >= '0' && rs[k] <= '9' || rs[k] == ';') {
				k++
			}
			if k >= len(rs) {
				s.unknown = append(s.unknown, "truncated CSI")
				break
			}
			arg, final := string(rs[j+1:k]), rs[k]
			n, _ := strconv.Atoi(arg)
			switch final {
			case 'A':
				s.row = max(s.row-max(n, 1), 0)
			case 'K':
				s.ensure()
				s.grid[s.row] = s.grid[s.row][:min(s.col, len(s.grid[s.row]))]
			case 'J':
				s.ensure()
				s.grid[s.row] = s.grid[s.row][:min(s.col, len(s.grid[s.row]))]
				s.grid = s.grid[:s.row+1]
			case 'm':
			default:
				s.unknown = append(s.unknown, string(rs[i:k+1]))
			}
			i = k
		case r < 0x20:
			s.unknown = append(s.unknown, fmt.Sprintf("control %U", r))
		default:
			if s.col >= s.cols {
				s.wraps++
				s.row++
				s.col = 0
				s.ensure()
			}
			s.ensure()
			line := s.grid[s.row]
			for len(line) < s.col {
				line = append(line, ' ')
			}
			if s.col < len(line) {
				line[s.col] = r
			} else {
				line = append(line, r)
			}
			if runeWidth(r) == 2 {
				line = append(line, 0)
			}
			s.grid[s.row] = line
			s.col += runeWidth(r)
		}
	}
}

func (s *fakeScreen) ensure() {
	for len(s.grid) <= s.row {
		s.grid = append(s.grid, []rune{})
	}
}

// lines is the visible text, without the empty line the cursor stands on.
func (s *fakeScreen) lines() []string {
	var out []string
	for _, l := range s.grid {
		out = append(out, strings.TrimRight(strings.ReplaceAll(string(l), "\x00", ""), " "))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func equalLines(a, b []string) bool { return strings.Join(a, "\n") == strings.Join(b, "\n") }

// liveSink is a live sink over a fake screen of the given size.
func liveSink(cols, rows int) (*tidyProgress, *fakeScreen, *fakeClock) {
	clk := newClock()
	screen := newFakeScreen(cols, rows)
	p := newTidyProgress(screen, tidyProgressLive, clk.now)
	p.size = func() (int, int) { return cols, rows }
	return p, screen, clk
}

func done(repo string, idx, count int) workspace.ObserveEvent {
	return workspace.ObserveEvent{Repo: repo, Index: idx, Count: count, Phase: workspace.ObservePhaseDone, Note: "analysed"}
}

func TestTidyBlockFirstFrameShowsEveryRepositoryAndQueuedOnes(t *testing.T) {
	p, screen, clk := liveSink(100, 24)
	p.SetRepos([]string{"workharbor", "crewbook", "sheetmill"})
	p.Event(ev("crewbook", 1, 3, workspace.ObservePhaseStart, 0, 0))
	want := []string{
		"stw tidy: 0/3 repos, 0ms",
		"▸ crewbook   starting            0ms",
		"· sheetmill  waiting",
		"· workharbor waiting",
	}
	// The names are sorted like the analysis sorts them: crewbook, sheetmill, workharbor.
	want[2], want[3] = "· sheetmill  waiting", "· workharbor waiting"
	if got := screen.lines(); !equalLines(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if screen.row != 4 || screen.col != 0 || len(screen.unknown) != 0 || screen.wraps != 0 {
		t.Fatalf("cursor %d,%d unknown %q wraps %d", screen.row, screen.col, screen.unknown, screen.wraps)
	}
	if strings.HasPrefix(screen.raw.String(), "\x1b[") {
		t.Fatalf("the first frame moves the cursor up: %q", screen.raw.String())
	}
	_ = clk
}

func TestTidyBlockRedrawsInPlaceAndTheRunningLineShowsCountAndEstimate(t *testing.T) {
	p, screen, clk := liveSink(100, 24)
	p.SetRepos([]string{"a", "b"})
	p.Event(ev("a", 1, 2, workspace.ObservePhaseStart, 0, 0))
	p.Event(ev("a", 1, 2, workspace.ObservePhaseWorktrees, 0, 100))
	clk.advance(10 * time.Second)
	p.Event(ev("a", 1, 2, workspace.ObservePhaseWorktrees, 20, 100))
	if got := screen.lines(); len(got) != 3 || got[1] != "▸ a observing worktrees [#-------] 20/100 10s ~40s left" || got[2] != "· b waiting" {
		t.Fatalf("%q", got)
	}
	if !strings.Contains(screen.raw.String(), "\x1b[3A") {
		t.Fatalf("the redraw does not go up over the block: %q", screen.raw.String())
	}
	clk.advance(tidyProgressLiveEvery)
	p.Event(ev("a", 1, 2, workspace.ObservePhaseWorktrees, 100, 100))
	p.Event(done("a", 1, 2))
	clk.advance(tidyProgressLiveEvery)
	p.Event(ev("b", 2, 2, workspace.ObservePhaseStart, 0, 0))
	got := screen.lines()
	if len(got) != 3 || !strings.HasPrefix(got[1], "✓ a analysed") || !strings.HasPrefix(got[2], "▸ b starting") || got[0] != "stw tidy: 1/2 repos, 10s, slowest a 10s" {
		t.Fatalf("%q", got)
	}
	if screen.row != 3 || len(screen.unknown) != 0 {
		t.Fatalf("cursor %d unknown %q", screen.row, screen.unknown)
	}
}

func TestTidyBlockSeveralRepositoriesRunAtOnce(t *testing.T) {
	p, screen, clk := liveSink(100, 24)
	p.SetRepos([]string{"a", "b", "c"})
	for i, n := range []string{"a", "b", "c"} {
		p.Event(ev(n, i+1, 3, workspace.ObservePhaseStart, 0, 0))
		clk.advance(tidyProgressLiveEvery)
	}
	n := 0
	for _, l := range screen.lines() {
		if strings.HasPrefix(l, "▸ ") {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("%q", screen.lines())
	}
}

func TestTidyBlockRedrawsAtMostFivePerSecond(t *testing.T) {
	p, screen, clk := liveSink(100, 24)
	for i := 1; i <= 100; i++ {
		clk.advance(time.Millisecond)
		p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, i, 200))
	}
	if n := strings.Count(screen.raw.String(), "\x1b[J"); n != 1 {
		t.Fatalf("%d redraws within 100 ms", n)
	}
	clk.advance(tidyProgressLiveEvery)
	p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, 101, 200))
	if n := strings.Count(screen.raw.String(), "\x1b[J"); n != 2 {
		t.Fatalf("%d redraws after the interval", n)
	}
	if tidyProgressLiveEvery != 200*time.Millisecond {
		t.Fatalf("the cadence is %v", tidyProgressLiveEvery)
	}
}

func TestTidyBlockFinalBlockStaysAndTheCursorIsBelowIt(t *testing.T) {
	p, screen, clk := liveSink(100, 24)
	p.SetRepos([]string{"a", "b"})
	p.Event(ev("a", 1, 2, workspace.ObservePhaseStart, 0, 0))
	clk.advance(2 * time.Second)
	p.Event(done("a", 1, 2))
	clk.advance(tidyProgressLiveEvery)
	p.Event(ev("b", 2, 2, workspace.ObservePhaseStart, 0, 0))
	clk.advance(time.Second)
	p.Event(workspace.ObserveEvent{Repo: "b", Index: 2, Count: 2, Phase: workspace.ObservePhaseDone, Note: "error"})
	p.Finish()
	want := []string{
		"stw tidy: 2/2 repos, 3.2s, slowest a 2.0s",
		"✓ a analysed            2.0s",
		"! b error               1.0s",
	}
	if got := screen.lines(); !equalLines(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if screen.row != 3 || screen.col != 0 {
		t.Fatalf("cursor %d,%d", screen.row, screen.col)
	}
	before := screen.raw.Len()
	p.Finish()
	p.Event(ev("b", 2, 2, workspace.ObservePhaseTracking, 1, 2))
	if screen.raw.Len() != before {
		t.Fatalf("output after the end: %q", screen.raw.String()[before:])
	}
}

// A deadline or an interrupt ends the run with repositories running and others never started: the
// final block names them, nothing is erased.
func TestTidyBlockFinalBlockAfterAnInterrupt(t *testing.T) {
	p, screen, clk := liveSink(100, 24)
	p.SetRepos([]string{"a", "b", "c"})
	p.Event(ev("a", 1, 3, workspace.ObservePhaseStart, 0, 0))
	p.Event(done("a", 1, 3))
	p.Event(ev("b", 2, 3, workspace.ObservePhaseWorktrees, 5, 9))
	clk.advance(7 * time.Second)
	p.Finish()
	want := []string{
		"stw tidy: 1/3 repos, 7.0s, slowest b 7.0s",
		"✓ a analysed            0ms",
		"! b stopped observing worktrees [####----] 5/9 7.0s",
		"· c not started",
	}
	if got := screen.lines(); !equalLines(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if screen.row != 4 || screen.col != 0 || len(screen.unknown) != 0 {
		t.Fatalf("cursor %d,%d unknown %q", screen.row, screen.col, screen.unknown)
	}
}

func TestTidyBlockRunWithoutEventsLeavesNothing(t *testing.T) {
	p, screen, _ := liveSink(100, 24)
	p.SetRepos([]string{"a", "b"})
	p.Finish()
	if screen.raw.Len() != 0 {
		t.Fatalf("wrote %q", screen.raw.String())
	}
}

func TestTidyBlockNarrowTerminalTruncatesAndNeverWraps(t *testing.T) {
	for _, cols := range []int{12, 30, 40, 60} {
		p, screen, clk := liveSink(cols, 24)
		p.SetRepos([]string{"averyveryverylongrepositoryname-with-more", "日本語のリポジトリ"})
		p.Event(ev("averyveryverylongrepositoryname-with-more", 1, 2, workspace.ObservePhaseWorktrees, 20, 1098))
		clk.advance(tidyProgressLiveEvery)
		p.Event(ev("日本語のリポジトリ", 2, 2, workspace.ObservePhaseProofs, 3, 9))
		clk.advance(tidyProgressLiveEvery)
		p.Event(ev("日本語のリポジトリ", 2, 2, workspace.ObservePhaseProofs, 4, 9))
		p.Finish()
		if screen.wraps != 0 || len(screen.unknown) != 0 {
			t.Fatalf("cols %d: wraps %d unknown %q\n%q", cols, screen.wraps, screen.unknown, screen.lines())
		}
		for _, l := range screen.lines() {
			if w := textWidth(l); w > cols-1 {
				t.Fatalf("cols %d: %q is %d wide", cols, l, w)
			}
		}
		if len(screen.lines()) != 3 {
			t.Fatalf("cols %d: %q", cols, screen.lines())
		}
	}
}

func TestTidyBlockTruncationCutsWholeRunes(t *testing.T) {
	for in, want := range map[string]string{
		"abcdef":   "abc…",
		"日本語":      "日…", // 2 columns per rune; 4 wide fits one rune and the ellipsis
		"ab":       "ab",
		"e\u0301e": "e\u0301e", // a combining accent takes no column
	} {
		if got := truncateWidth(in, 4); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestTidyBlockCollapsesCompletedRepositoriesWhenTheyDoNotFit(t *testing.T) {
	// 8 rows leave a block of 5 lines: the header and four repositories.
	p, screen, clk := liveSink(100, 8)
	names := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7"}
	p.SetRepos(names)
	for i, n := range names[:5] {
		p.Event(ev(n, i+1, 7, workspace.ObservePhaseStart, 0, 0))
		clk.advance(time.Duration(i+1) * 10 * time.Second)
		p.Event(done(n, i+1, 7))
	}
	p.Event(ev("r6", 6, 7, workspace.ObservePhaseWorktrees, 1, 2))
	p.Finish()
	want := []string{
		"stw tidy: 5/7 repos, 2m30s, slowest r5 50s",
		"✓ 5 repos done (largest: r5 50s)",
		"! r6 stopped observing worktrees [####----] 1/2 0ms",
		"· r7 not started",
	}
	if got := screen.lines(); !equalLines(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if len(screen.lines()) > 5 {
		t.Fatalf("%d lines on 8 rows", len(screen.lines()))
	}
}

func TestTidyBlockTooManyRunningOrWaitingShowsACount(t *testing.T) {
	p, screen, clk := liveSink(100, 8)
	var names []string
	for i := 1; i <= 12; i++ {
		names = append(names, fmt.Sprintf("r%02d", i))
	}
	p.SetRepos(names)
	p.Event(ev("r01", 1, 12, workspace.ObservePhaseStart, 0, 0))
	clk.advance(time.Second)
	p.Event(ev("r02", 2, 12, workspace.ObservePhaseStart, 0, 0))
	got := screen.lines()
	if len(got) != 5 || !strings.HasPrefix(got[1], "▸ r01") || !strings.HasPrefix(got[2], "▸ r02") || got[4] != "· ... 9 more repos not shown" {
		t.Fatalf("%q", got)
	}
}

func TestTidyBlockShrinkingBlockErasesTheOldLines(t *testing.T) {
	p, screen, clk := liveSink(100, 8)
	names := []string{"r1", "r2", "r3", "r4", "r5", "r6"}
	p.SetRepos(names)
	p.Event(ev("r1", 1, 6, workspace.ObservePhaseStart, 0, 0))
	if len(screen.lines()) != 5 {
		t.Fatalf("%q", screen.lines())
	}
	for _, n := range names[:5] {
		p.Event(ev(n, 1, 6, workspace.ObservePhaseStart, 0, 0))
		p.Event(done(n, 1, 6))
	}
	clk.advance(tidyProgressLiveEvery)
	p.Event(ev("r6", 6, 6, workspace.ObservePhaseStart, 0, 0))
	got := screen.lines()
	if len(got) != 3 || !strings.HasPrefix(got[1], "✓ 5 repos done") {
		t.Fatalf("%q", got)
	}
}

func TestTidyBlockHostileNamesAreEscaped(t *testing.T) {
	p, screen, _ := liveSink(120, 24)
	evil := "evil\x1b[2J\rname\u202eRTL\nx"
	p.SetRepos([]string{evil})
	p.Event(ev(evil, 1, 1, workspace.ObservePhaseWorktrees, 1, 2))
	p.Finish()
	raw := screen.raw.String()
	if len(screen.unknown) != 0 {
		t.Fatalf("an injected sequence reached the terminal: %q", screen.unknown)
	}
	for _, bad := range []string{"\x1b[2J", "\u202e", "\rname"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("%q in %q", bad, raw)
		}
	}
	if !strings.Contains(strings.Join(screen.lines(), "\n"), `evil`) {
		t.Fatalf("%q", screen.lines())
	}
}

func TestTidyBlockColorFollowsTheRule(t *testing.T) {
	run := func(color bool) string {
		p, screen, clk := liveSink(100, 24)
		p.color = color
		p.SetRepos([]string{"a", "b"})
		p.Event(ev("a", 1, 2, workspace.ObservePhaseStart, 0, 0))
		p.Event(done("a", 1, 2))
		clk.advance(tidyProgressLiveEvery)
		p.Event(ev("b", 2, 2, workspace.ObservePhaseStart, 0, 0))
		return screen.raw.String()
	}
	plain, colored := run(false), run(true)
	if strings.Contains(plain, "\x1b[1m") || strings.Contains(plain, "\x1b[32m") || strings.Contains(plain, "\x1b[2m") || strings.Contains(plain, "\x1b[0m") {
		t.Fatalf("color without color: %q", plain)
	}
	for _, want := range []string{"\x1b[32m✓\x1b[0m", "\x1b[1m▸ b", "\x1b[1mstw tidy"} {
		if !strings.Contains(colored, want) {
			t.Errorf("%q missing in %q", want, colored)
		}
	}
	p, screen, _ := liveSink(100, 24)
	p.color = true
	p.SetRepos([]string{"a", "b"})
	p.Event(ev("a", 1, 2, workspace.ObservePhaseStart, 0, 0))
	if !strings.Contains(screen.raw.String(), "\x1b[2m· b waiting\x1b[0m") {
		t.Fatalf("queued is not dim: %q", screen.raw.String())
	}
}

func TestTidyBlockNoColorEnvironmentTurnsColorOff(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	progressWorld(t, true, false, false)
	_, errText, err := runTidyErr(t, "plan")
	if err != nil || regexp.MustCompile(`\x1b\[[0-9;]*m`).MatchString(errText) || !strings.Contains(errText, "\x1b[") {
		t.Fatalf("%v %q", err, errText)
	}
}

func TestTidyBlockSizeIsReadAtEveryRedraw(t *testing.T) {
	p, screen, clk := liveSink(100, 24)
	cols := 100
	p.size = func() (int, int) { return cols, 24 }
	p.Event(ev("averyveryverylongrepositoryname", 1, 1, workspace.ObservePhaseWorktrees, 1, 2))
	cols = 20
	clk.advance(tidyProgressLiveEvery)
	p.Event(ev("averyveryverylongrepositoryname", 1, 1, workspace.ObservePhaseWorktrees, 2, 2))
	for _, l := range screen.lines() {
		if textWidth(l) > 19 {
			t.Fatalf("%q is wider than the terminal", l)
		}
	}
	// An unknown size falls back to 80 columns by 24 rows.
	p.size = func() (int, int) { return 0, 0 }
	clk.advance(tidyProgressLiveEvery)
	p.Event(ev("a", 1, 1, workspace.ObservePhaseWorktrees, 2, 2))
}

func TestTidyBlockNonTerminalModesAreUnchanged(t *testing.T) {
	clk := newClock()
	var out bytes.Buffer
	p := newTidyProgress(&out, tidyProgressPlain, clk.now)
	p.SetRepos([]string{"a"})
	p.Event(ev("a", 1, 2, workspace.ObservePhaseStart, 0, 0))
	clk.advance(38 * time.Second)
	p.Event(ev("a", 1, 2, workspace.ObservePhaseWorktrees, 412, 1098))
	p.Finish()
	if got := out.String(); got != "[1/2] a: starting (0ms)\n[1/2] a: observing worktrees 412/1,098 (38s)\n" {
		t.Fatalf("%q", got)
	}
}

// Many workers report at once while a slow terminal writes: nothing races, the bookkeeping keeps
// every count and the final block is whole.
func TestTidyBlockConcurrentWorkersAreSafe(t *testing.T) {
	p, screen, clk := liveSink(100, 24)
	p.SetRepos([]string{"a", "b", "c", "d"})
	var wg sync.WaitGroup
	for i, n := range []string{"a", "b", "c", "d"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Event(ev(n, i+1, 4, workspace.ObservePhaseStart, 0, 0))
			for k := 1; k <= 200; k++ {
				clk.advance(time.Millisecond)
				p.Event(ev(n, i+1, 4, workspace.ObservePhaseWorktrees, k, 200))
				p.Git(workspace.GitCall{Repo: n})
			}
			p.Event(done(n, i+1, 4))
		}()
	}
	wg.Wait()
	p.Finish()
	got := screen.lines()
	if len(got) != 5 || len(screen.unknown) != 0 || screen.wraps != 0 {
		t.Fatalf("%q unknown %q", got, screen.unknown)
	}
	for _, l := range got[1:] {
		if !strings.HasPrefix(l, "✓ ") {
			t.Fatalf("%q", got)
		}
	}
}
