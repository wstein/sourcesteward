// SPDX-License-Identifier: EUPL-1.2
package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/wstein/sourcesteward/internal/untrusted"
	"github.com/wstein/sourcesteward/internal/workspace"
	"golang.org/x/text/width"
)

// The live block of a terminal (Spec 041 progress): one line per repository, redrawn in place with
// cursor-up and erase-line sequences. It never takes over the screen: the last frame stays printed
// and the cursor ends on the line below it.

// The size assumed when the terminal cannot tell, and the lines the block leaves free: one for the
// prompt, one for the cursor line and one for a shell that wraps a long line.
const (
	tidyBlockDefaultCols = 80
	tidyBlockDefaultRows = 24
	tidyBlockSpare       = 3
	tidyBlockMinLines    = 4
	// tidyBlockMinRows is the least height of a terminal that gets the block; a lower one gets the
	// plain lines, because a block and its cursor line would not fit and every frame would leave a
	// stale line behind in the scrollback.
	tidyBlockMinRows    = 5
	tidyBlockNameMax    = 24
	tidyBlockBar        = 8
	tidyBlockPhaseWidth = len("observing worktrees")
)

type blockKind int

const (
	blockHeader blockKind = iota
	blockQueued
	blockRunning
	blockDone
	blockRetained
	blockSummary
)

// mark is the glyph of a state: Unicode, or ASCII where the locale is not UTF-8.
func (p *tidyProgress) mark(k blockKind) string {
	if p.ascii {
		switch k {
		case blockQueued:
			return "."
		case blockRunning:
			return ">"
		case blockDone, blockSummary:
			return "v"
		}
		return "!"
	}
	switch k {
	case blockQueued:
		return "·"
	case blockRunning:
		return "▸"
	case blockDone, blockSummary:
		return "✓"
	}
	return "!"
}

// ellipsis is the sign of a cut.
func (p *tidyProgress) ellipsis() string {
	if p.ascii {
		return "..."
	}
	return "…"
}

// trunc cuts s to max columns with the ellipsis of the terminal.
func (p *tidyProgress) trunc(s string, max int) string { return truncateWith(s, max, p.ellipsis()) }

type blockLine struct {
	text string
	kind blockKind
}

// blockEntry is one repository of the block, in discovery order.
type blockEntry struct {
	name  string // already escaped
	index int
	r     *tidyRepoProgress // nil: not started
}

// liveLabel is the phase on a block line.
func liveLabel(ph workspace.ObservePhase) string {
	switch ph {
	case workspace.ObservePhaseRefs:
		return "discovery"
	case workspace.ObservePhaseWorktrees:
		return "observing worktrees"
	case workspace.ObservePhaseBranches:
		return "observing branches"
	case workspace.ObservePhaseProofs:
		return "proofs"
	case workspace.ObservePhaseTracking:
		return "tracking"
	}
	return phaseLabel(ph)
}

// SetRepos tells the sink which repositories the analysis will visit, so that the block can show
// the ones still waiting. The names are ordered like the analysis orders them.
func (p *tidyProgress) SetRepos(names []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = append([]string{}, names...)
	sort.Strings(p.queue)
	out := p.queue[:0]
	for i, n := range p.queue {
		if i == 0 || n != p.queue[i-1] {
			out = append(out, n)
		}
	}
	p.queue = out
}

// entries lists every known repository in discovery order. The caller holds the lock.
func (p *tidyProgress) entries() []blockEntry {
	seen := map[string]bool{}
	var out []blockEntry
	for i, n := range p.queue {
		seen[n] = true
		out = append(out, blockEntry{name: untrusted.Esc(n), index: i + 1, r: p.begun(n)})
	}
	var extra []blockEntry
	for _, n := range p.order {
		if !seen[n] && p.begun(n) != nil {
			extra = append(extra, blockEntry{name: untrusted.Esc(n), index: p.repos[n].index, r: p.repos[n]})
		}
	}
	sort.SliceStable(extra, func(i, j int) bool { return extra[i].index < extra[j].index })
	return append(out, extra...)
}

// begun is the record of a repository that reported, or nil.
func (p *tidyProgress) begun(name string) *tidyRepoProgress {
	if r := p.repos[name]; r != nil && r.seen {
		return r
	}
	return nil
}

// dims is the size of the terminal now, or the default when it cannot be read.
func (p *tidyProgress) dims() (cols, rows int) {
	cols, rows = tidyBlockDefaultCols, tidyBlockDefaultRows
	if p.size != nil {
		if c, r := p.size(); c > 0 && r > 0 {
			cols, rows = c, r
		}
	}
	return cols, rows
}

// renderedBlock is a block ready to write: its lines (with colors), the display width of each line
// without them, and the size of the terminal it was made for.
type renderedBlock struct {
	lines      []string
	widths     []int
	cols, rows int
}

// render is the block for now: the lines to show, within the size of the terminal. final marks the
// block that stays on the screen at the end of the run. The caller holds the lock.
func (p *tidyProgress) render(now time.Time, final bool) renderedBlock {
	cols, rows := p.dims()
	entries := p.entries()
	maxLines := min(max(rows-tidyBlockSpare, tidyBlockMinLines), rows-1)
	nameW := 0
	for _, e := range entries {
		nameW = max(nameW, textWidth(e.name))
	}
	nameW = min(nameW, tidyBlockNameMax)

	done, started, slowName, slowFor := 0, 0, "", time.Duration(-1)
	lines := make([]blockLine, 0, len(entries))
	for _, e := range entries {
		l := p.repoLine(e, now, final, nameW, cols, true)
		if e.r != nil && !e.r.ended.IsZero() {
			done++
		}
		if e.r != nil {
			started++
			end := e.r.ended
			if end.IsZero() {
				end = now
			}
			if d := end.Sub(e.r.started); d > slowFor {
				slowName, slowFor = e.name, d
			}
		}
		lines = append(lines, l)
	}
	total := len(entries)
	head := fmt.Sprintf("stw tidy: %d/%d repos, %s", done, total, shortDuration(now.Sub(p.start)))
	if started > 1 {
		head += ", slowest " + p.trunc(slowName, tidyBlockNameMax) + " " + shortDuration(slowFor)
	}
	out := []blockLine{{head, blockHeader}}
	out = append(out, p.fit(entries, lines, maxLines-1)...)
	res := renderedBlock{cols: cols, rows: rows}
	for _, l := range out {
		text := p.trunc(l.text, cols-1)
		res.lines = append(res.lines, p.paint(text, l.kind))
		res.widths = append(res.widths, textWidth(text))
	}
	return res
}

// fit keeps the block within its line budget: first the completed repositories collapse into one
// summary line, then the waiting ones that do not fit are counted in a last line.
func (p *tidyProgress) fit(entries []blockEntry, lines []blockLine, budget int) []blockLine {
	if len(lines) <= budget {
		return lines
	}
	var summary *blockLine
	var keep []int
	n, largest, largestFor := 0, "", time.Duration(-1)
	for i, l := range lines {
		if l.kind == blockDone {
			n++
			if d := entries[i].r.ended.Sub(entries[i].r.started); d > largestFor {
				largest, largestFor = entries[i].name, d
			}
			continue
		}
		keep = append(keep, i)
	}
	if n > 0 {
		noun := "repos"
		if n == 1 {
			noun = "repo"
		}
		summary = &blockLine{fmt.Sprintf("%s %d %s done (largest: %s %s)", p.mark(blockDone), n, noun, p.trunc(largest, tidyBlockNameMax), shortDuration(largestFor)), blockSummary}
		budget--
	}
	var more *blockLine
	if len(keep) > budget {
		// Running first, then the retained ones, then the waiting ones; the order of the block is kept.
		rank := func(k blockKind) int {
			switch k {
			case blockRunning:
				return 0
			case blockRetained:
				return 1
			}
			return 2
		}
		byRank := append([]int{}, keep...)
		sort.SliceStable(byRank, func(i, j int) bool { return rank(lines[byRank[i]].kind) < rank(lines[byRank[j]].kind) })
		room := max(budget-1, 0)
		hidden := len(keep) - room
		chosen := map[int]bool{}
		for _, i := range byRank[:room] {
			chosen[i] = true
		}
		var kept []int
		for _, i := range keep {
			if chosen[i] {
				kept = append(kept, i)
			}
		}
		keep = kept
		more = &blockLine{fmt.Sprintf("%s ... %d more repos not shown", p.mark(blockQueued), hidden), blockQueued}
	}
	var out []blockLine
	if summary != nil {
		out = append(out, *summary)
	}
	for _, i := range keep {
		out = append(out, lines[i])
	}
	if more != nil {
		out = append(out, *more)
	}
	return out
}

// repoLine is the line of one repository. The caller holds the lock.
func (p *tidyProgress) repoLine(e blockEntry, now time.Time, final bool, nameW, cols int, bar bool) blockLine {
	name := padWidth(p.trunc(e.name, tidyBlockNameMax), nameW)
	r := e.r
	if r == nil {
		word := "waiting"
		if final {
			word = "not started"
		}
		return blockLine{p.mark(blockQueued) + " " + name + " " + word, blockQueued}
	}
	phase, counts, left := "starting", "", ""
	if n := len(r.phases); n > 0 {
		ph := r.phases[n-1]
		phase = liveLabel(ph.phase)
		if ph.total > 0 {
			counts = groupDigits(ph.done) + "/" + groupDigits(ph.total)
			if bar {
				filled := min(ph.done*tidyBlockBar/ph.total, tidyBlockBar)
				counts = "[" + strings.Repeat("#", filled) + strings.Repeat("-", tidyBlockBar-filled) + "] " + counts
			}
		}
		if ph.eta > 0 && r.ended.IsZero() && !final {
			left = "~" + shortDuration(roundUp(ph.eta, 5*time.Second)) + " left"
		}
	}
	if !r.ended.IsZero() {
		took := shortDuration(r.ended.Sub(r.started))
		if r.state == "analysed" {
			return blockLine{p.mark(blockDone) + " " + name + " " + padWidth("analysed", tidyBlockPhaseWidth) + " " + took, blockDone}
		}
		state := untrusted.Esc(r.state)
		if state == "" {
			state = "stopped"
		}
		return blockLine{p.mark(blockRetained) + " " + name + " " + padWidth(state, tidyBlockPhaseWidth) + " " + took, blockRetained}
	}
	took := shortDuration(now.Sub(r.started))
	parts := []string{padWidth(phase, tidyBlockPhaseWidth)}
	if counts != "" {
		parts = append(parts, counts)
	}
	parts = append(parts, took)
	if left != "" {
		parts = append(parts, left)
	}
	mark, kind := p.mark(blockRunning), blockRunning
	if final {
		// The run ended with this repository unfinished: a deadline or an interrupt.
		mark, kind = p.mark(blockRetained), blockRetained
		parts = append([]string{"stopped"}, parts...)
	}
	text := mark + " " + name + " " + strings.Join(parts, " ")
	if bar && textWidth(text) > cols-1 {
		return p.repoLine(e, now, final, nameW, cols, false)
	}
	return blockLine{text, kind}
}

// paint colors a line that is already escaped and cut to the width.
func (p *tidyProgress) paint(s string, kind blockKind) string {
	if !p.color {
		return s
	}
	const reset = "\x1b[0m"
	switch kind {
	case blockHeader, blockRunning:
		return "\x1b[1m" + s + reset
	case blockQueued:
		return "\x1b[2m" + s + reset
	case blockDone, blockSummary:
		if rest, ok := strings.CutPrefix(s, p.mark(kind)); ok {
			return "\x1b[32m" + p.mark(kind) + reset + rest
		}
	}
	return s
}

// writeBlock draws the block: the cursor goes up over the previous one, every line is rewritten
// with the rest of the old line erased, and what is left below the new block is erased. The cursor
// ends at the start of the line below. A terminal that rewraps its lines when it gets narrower shows
// an old line of width w in ceil(w/cols) rows, so the way up is counted in rows of the size of now,
// and never more than the screen holds. The caller holds the write lock.
func (p *tidyProgress) writeBlock(b renderedBlock) {
	var out strings.Builder
	up := 0
	for _, w := range p.drawnW {
		up += max(1, (w+b.cols-1)/b.cols)
	}
	up = min(up, b.rows-1)
	if len(p.drawnW) > 0 && up > 0 {
		fmt.Fprintf(&out, "\x1b[%dA", up)
	}
	for _, l := range b.lines {
		out.WriteString("\r" + l + "\x1b[K\n")
	}
	out.WriteString("\x1b[J")
	fmt.Fprint(p.w, out.String())
	p.drawnW = b.widths
}

// runeWidth is the number of terminal columns of r.
func runeWidth(r rune) int {
	switch {
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Cf, r), unicode.IsControl(r):
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// cellWidths is the width of every rune of s. A variation selector 16 asks for the emoji form of the
// rune before it, which takes two columns. East Asian ambiguous runes count one column; the lines
// keep a column of slack (the block is cut at the width minus one).
func cellWidths(s string) []int {
	var out []int
	for _, r := range s {
		if r == 0xFE0F && len(out) > 0 && out[len(out)-1] == 1 {
			out[len(out)-1] = 2
			out = append(out, 0)
			continue
		}
		out = append(out, runeWidth(r))
	}
	return out
}

func textWidth(s string) int {
	n := 0
	for _, w := range cellWidths(s) {
		n += w
	}
	return n
}

// truncateWith cuts s to at most max columns, ending in ell when it cut.
func truncateWith(s string, max int, ell string) string {
	if max < 1 {
		return ""
	}
	if textWidth(s) <= max {
		return s
	}
	keep := max - textWidth(ell)
	var b strings.Builder
	w := 0
	widths := cellWidths(s)
	i := 0
	for _, r := range s {
		rw := 0
		if i < len(widths) {
			rw = widths[i]
		}
		i++
		if r == 0xFE0F {
			// Its width was added to the rune before it, which was written (or the loop ended).
			b.WriteRune(r)
			continue
		}
		if w+rw > keep {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + ell
}

// padWidth pads s with blanks to n columns.
func padWidth(s string, n int) string {
	if w := textWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}
