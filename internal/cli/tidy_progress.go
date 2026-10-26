// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wstein/sourcesteward/internal/stwsync"
	"github.com/wstein/sourcesteward/internal/tidywizard/render"
	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/workspace"
)

// The cadence of the progress output: a terminal line is rewritten at most this often, a plain
// line (verbose output that is not a terminal) is written at most this often.
const (
	tidyProgressLiveEvery  = 200 * time.Millisecond
	tidyProgressPlainEvery = 10 * time.Second
)

// tidyProgressMode says what the progress sink writes while the analysis runs.
type tidyProgressMode int

const (
	// tidyProgressOff writes nothing live.
	tidyProgressOff tidyProgressMode = iota
	// tidyProgressLive rewrites one terminal line (carriage return and blanks, no escape sequence).
	tidyProgressLive
	// tidyProgressPlain writes an ordinary line now and then.
	tidyProgressPlain
)

// tidyProgress follows the observation events of the analysis (Spec 041 progress): it writes the
// live progress line to stderr and keeps the per-repository, per-phase timings for --verbose.
// Nothing it writes goes to stdout. Its methods may be called from the workers of the analysis at
// the same time.
type tidyProgress struct {
	mu        sync.Mutex
	w         io.Writer
	mode      tidyProgressMode
	now       func() time.Time
	start     time.Time
	last      time.Time
	lineLen   int
	prepared  time.Duration
	preparedK bool
	repos     map[string]*tidyRepoProgress
	order     []string
	slow      []tidySlowItem
	// trace receives one JSON line per Git process (--trace); traceBytes bounds it.
	trace      io.Writer
	traceBytes int
	traceCut   bool
}

// tidySlowItem is one finished item for the list of the slowest.
type tidySlowItem struct {
	repo  string
	phase workspace.ObservePhase
	item  string
	took  time.Duration
}

// tidyTraceLimit bounds the --trace file; the line that follows the last written one says it was cut.
const tidyTraceLimit = 8 << 20

// tidySlowest is the number of slowest items --verbose lists.
const tidySlowest = 10

type tidyRepoProgress struct {
	index, count int
	started      time.Time
	ended        time.Time
	state        string
	phases       []*tidyPhaseProgress
	windows      []string
	gitCalls     int
	gitTime      time.Duration
	caches       []string
}

type tidyPhaseProgress struct {
	phase       workspace.ObservePhase
	started     time.Time
	ended       time.Time
	done, total int
	eta         time.Duration // smoothed estimate of the time left, zero until there is one
}

func newTidyProgress(w io.Writer, mode tidyProgressMode, now func() time.Time) *tidyProgress {
	return &tidyProgress{w: w, mode: mode, now: now, start: now(), repos: map[string]*tidyRepoProgress{}}
}

// Prepared records how long the discovery and the reading of the references took.
func (p *tidyProgress) Prepared(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prepared, p.preparedK = d, true
}

// Event is the sink of the observation events.
func (p *tidyProgress) Event(e workspace.ObserveEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	r := p.repos[e.Repo]
	if r == nil {
		r = &tidyRepoProgress{started: now}
		p.repos[e.Repo] = r
		p.order = append(p.order, e.Repo)
	}
	r.index, r.count = e.Index, e.Count
	switch e.Phase {
	case workspace.ObservePhaseStart:
		r.started = now
	case workspace.ObservePhaseDone:
		r.ended, r.state = now, e.Note
		p.closePhase(r, now)
	case workspace.ObservePhaseWindow:
		r.windows = append(r.windows, e.Note)
	case workspace.ObservePhaseCache:
		r.caches = append(r.caches, fmt.Sprintf("%s: %s reused, %s computed", e.Note, groupDigits(e.Done), groupDigits(e.Total)))
	default:
		cur := (*tidyPhaseProgress)(nil)
		if n := len(r.phases); n > 0 {
			cur = r.phases[n-1]
		}
		if cur == nil || cur.phase != e.Phase {
			p.closePhase(r, now)
			cur = &tidyPhaseProgress{phase: e.Phase, started: now}
			r.phases = append(r.phases, cur)
		}
		cur.done, cur.total = e.Done, e.Total
		if e.Item != "" {
			p.addSlow(tidySlowItem{repo: e.Repo, phase: e.Phase, item: e.Item, took: e.Took})
		}
		p.estimate(cur, now)
	}
	p.draw(e, now)
}

// addSlow keeps the tidySlowest slowest items.
func (p *tidyProgress) addSlow(it tidySlowItem) {
	p.slow = append(p.slow, it)
	sort.SliceStable(p.slow, func(i, j int) bool { return p.slow[i].took > p.slow[j].took })
	if len(p.slow) > tidySlowest {
		p.slow = p.slow[:tidySlowest]
	}
}

// estimate updates the time left of a phase. It is cautious: nothing before 5 percent of the items
// and 3 seconds of the phase, the rate is the average of the whole phase so far, the estimate is
// smoothed with the previous one, and it is shown rounded up to 5 seconds.
func (p *tidyProgress) estimate(ph *tidyPhaseProgress, now time.Time) {
	elapsed := now.Sub(ph.started)
	if ph.total <= 0 || ph.done <= 0 || ph.done >= ph.total || ph.done*20 < ph.total || elapsed < 3*time.Second {
		ph.eta = 0
		return
	}
	cur := time.Duration(float64(elapsed) / float64(ph.done) * float64(ph.total-ph.done))
	if ph.eta > 0 {
		cur = (ph.eta*7 + cur*3) / 10
	}
	ph.eta = cur
}

// Git is the sink of the Git processes: it counts them per repository and writes the --trace line.
func (p *tidyProgress) Git(c workspace.GitCall) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := p.repos[c.Repo]
	if r == nil {
		r = &tidyRepoProgress{started: p.now()}
		p.repos[c.Repo] = r
		p.order = append(p.order, c.Repo)
	}
	r.gitCalls++
	r.gitTime += c.Duration
	if p.trace == nil || p.traceCut {
		return
	}
	line, err := json.Marshal(struct {
		Repo string   `json:"repo"`
		Argv []string `json:"argv"`
		MS   float64  `json:"ms"`
		Exit int      `json:"exit"`
	}{c.Repo, c.Argv, float64(c.Duration.Microseconds()) / 1000, c.Exit})
	if err != nil {
		return
	}
	if p.traceBytes+len(line)+1 > tidyTraceLimit {
		p.traceCut = true
		fmt.Fprintln(p.trace, `{"truncated":true}`)
		return
	}
	p.traceBytes += len(line) + 1
	fmt.Fprintf(p.trace, "%s\n", line)
}

func (p *tidyProgress) closePhase(r *tidyRepoProgress, now time.Time) {
	if n := len(r.phases); n > 0 && r.phases[n-1].ended.IsZero() {
		r.phases[n-1].ended = now
	}
}

// phaseLabel is the words of a phase on the progress line.
func phaseLabel(ph workspace.ObservePhase) string {
	switch ph {
	case workspace.ObservePhaseRefs:
		return "reading references"
	case workspace.ObservePhaseWorktrees:
		return "observing worktrees"
	case workspace.ObservePhaseBranches:
		return "reading branch history"
	case workspace.ObservePhaseProofs:
		return "proving integration"
	case workspace.ObservePhaseTracking:
		return "reading tracking evidence"
	case workspace.ObservePhaseStart:
		return "starting"
	case workspace.ObservePhaseDone:
		return "finished"
	}
	return string(ph)
}

// draw writes the progress line for an event when the cadence allows it. A repository that starts
// or ends is always shown. The caller holds the lock.
func (p *tidyProgress) draw(e workspace.ObserveEvent, now time.Time) {
	if p.mode == tidyProgressOff || e.Phase == workspace.ObservePhaseWindow {
		return
	}
	every := tidyProgressLiveEvery
	if p.mode == tidyProgressPlain {
		every = tidyProgressPlainEvery
	}
	forced := e.Phase == workspace.ObservePhaseStart && p.mode == tidyProgressPlain
	if !forced && !p.last.IsZero() && now.Sub(p.last) < every {
		return
	}
	p.last = now
	line := p.line(e, now)
	if p.mode == tidyProgressPlain {
		fmt.Fprintln(p.w, line)
		return
	}
	pad := ""
	if n := p.lineLen - len(line); n > 0 {
		pad = strings.Repeat(" ", n)
	}
	fmt.Fprint(p.w, "\r"+line+pad)
	p.lineLen = len(line)
}

// line is `[3/7] name: observing worktrees 12/40 (38s)`; the elapsed time is that of the whole
// analysis. The repository name comes from the workspace, so it passes untrusted.Esc.
func (p *tidyProgress) line(e workspace.ObserveEvent, now time.Time) string {
	var b strings.Builder
	if e.Count > 0 {
		fmt.Fprintf(&b, "[%d/%d] ", e.Index, e.Count)
	}
	name := e.Repo
	if name == "" {
		name = "workspace"
	}
	b.WriteString(untrusted.Esc(name) + ": " + phaseLabel(e.Phase))
	if e.Total > 0 {
		fmt.Fprintf(&b, " %s/%s", groupDigits(e.Done), groupDigits(e.Total))
	}
	left := ""
	if r := p.repos[e.Repo]; r != nil && e.Total > 0 {
		if n := len(r.phases); n > 0 && r.phases[n-1].eta > 0 {
			left = ", ~" + shortDuration(roundUp(r.phases[n-1].eta, 5*time.Second)) + " left"
		}
	}
	fmt.Fprintf(&b, " (%s%s)", shortDuration(now.Sub(p.start)), left)
	return b.String()
}

// Finish ends the live line (a terminal line is blanked), so that the output that follows starts
// at the left edge of a clean line.
func (p *tidyProgress) Finish() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mode == tidyProgressLive && p.lineLen > 0 {
		fmt.Fprint(p.w, "\r"+strings.Repeat(" ", p.lineLen)+"\r")
		p.lineLen = 0
	}
}

// Summary writes the --verbose report: the timings and counts per repository and phase, and the
// repository and phase where the observation window ended.
func (p *tidyProgress) Summary() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	fmt.Fprintf(p.w, "Timings: total %s\n", shortDuration(now.Sub(p.start)))
	if p.preparedK {
		fmt.Fprintf(p.w, "  discovery and references: %s\n", shortDuration(p.prepared))
	}
	repos := make([]string, len(p.order))
	copy(repos, p.order)
	sortProgressRepos(repos, p.repos)
	var windows []string
	for _, name := range repos {
		r := p.repos[name]
		end := r.ended
		if end.IsZero() {
			end = now
		}
		state := r.state
		if state == "" {
			state = "unfinished"
		}
		fmt.Fprintf(p.w, "  [%d/%d] %s: %s in %s\n", r.index, r.count, untrusted.Esc(name), untrusted.Esc(state), shortDuration(end.Sub(r.started)))
		for _, ph := range r.phases {
			pend := ph.ended
			if pend.IsZero() {
				pend = end
			}
			counts := ""
			if ph.total > 0 {
				counts = fmt.Sprintf(" %s/%s", groupDigits(ph.done), groupDigits(ph.total))
			}
			fmt.Fprintf(p.w, "    %s%s: %s\n", phaseLabel(ph.phase), counts, shortDuration(pend.Sub(ph.started)))
		}
		if r.gitCalls > 0 {
			fmt.Fprintf(p.w, "    git processes: %s, %s in total\n", groupDigits(r.gitCalls), shortDuration(r.gitTime))
		}
		for _, c := range r.caches {
			fmt.Fprintf(p.w, "    cache %s\n", untrusted.Esc(c))
		}
		for _, w := range r.windows {
			windows = append(windows, untrusted.Esc(name)+" ("+untrusted.Esc(w)+")")
		}
	}
	if len(p.slow) > 0 {
		fmt.Fprintf(p.w, "  slowest %d items:\n", len(p.slow))
		for _, it := range p.slow {
			fmt.Fprintf(p.w, "    %s  %s %s: %s\n", shortDuration(it.took), untrusted.Esc(it.repo), phaseLabel(it.phase), untrusted.Esc(it.item))
		}
	}
	if len(windows) == 0 {
		fmt.Fprintln(p.w, "  observation window: not reached")
		return
	}
	fmt.Fprintf(p.w, "  observation window reached: %s; raise --timeout or narrow the run with --member or --only\n", strings.Join(windows, ", "))
}

// sortProgressRepos orders the repositories by their position in the run, then by name.
func sortProgressRepos(names []string, repos map[string]*tidyRepoProgress) {
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if repos[a].index != repos[b].index {
			return repos[a].index < repos[b].index
		}
		return a < b
	})
}

// roundUp rounds d up to a multiple of step.
func roundUp(d, step time.Duration) time.Duration {
	if d <= 0 {
		return step
	}
	return (d + step - 1) / step * step
}

// groupDigits writes n with a comma between thousands: 1098 is 1,098.
func groupDigits(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// shortDuration is a duration for people: 850ms below a second, 38s below a minute (one decimal
// below ten seconds), 2m05s above.
func shortDuration(d time.Duration) string {
	switch {
	case d < 0:
		d = 0
		fallthrough
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return strconv.FormatInt(int64(d/time.Second), 10) + "s"
	}
	secs := int64(d / time.Second)
	return fmt.Sprintf("%dm%02ds", secs/60, secs%60)
}

// tidyTextOptions colors the human text only for a terminal without NO_COLOR (never --json, which
// does not use the text renderer); a buffer or a pipe gets the plain bytes.
func tidyTextOptions(out io.Writer) render.TextOptions {
	return render.TextOptions{Color: stwsync.ColorEnabled(out)}
}
