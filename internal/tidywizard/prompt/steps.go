// SPDX-License-Identifier: EUPL-1.2

package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// Group is one step of the janitor wizard (Spec 044 "Wizard steps"): the rows of one effect class
// and place. Title, Place and Lines are text the caller already put through `esc` (they come from
// the renderer's own sentences and escaped names); every Row field is raw and escaped here.
type Group struct {
	Key string
	// Title is the header of the step, for example "40 directories /tmp/whr-*".
	Title string
	// NounOne and NounMany name the rows ("directory", "directories"); Place is the escaped place
	// ("/tmp/whr-*"). The summary line of a group is the selected count, the noun and the place.
	NounOne, NounMany, Place string
	Level                    risk.Level
	Counts                   risk.Counts
	// Lines are the consequence lines under the header (owner, evidence, effect, recovery, why).
	Lines []string
	// Rows are the choosable rows of the group, in the order of their numbers.
	Rows []Row
}

// FlowResult is what RunFlow ended with: the chosen rows in number order and the decision rows
// whose second confirmation was answered "y".
type FlowResult struct {
	Outcome   Outcome
	Rows      []Row
	Confirmed map[string]bool
}

// ids returns the chosen ids in number order.
func (r FlowResult) IDs() []string {
	out := make([]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		out = append(out, row.ID)
	}
	return out
}

var errQuit = errors.New("quit")

// pageSize is how many rows `l` shows at a time.
const pageSize = 20

// RunFlow walks the groups of the janitor wizard: the first question, one step per group, then the
// summary with its last line. Nothing it does applies anything; it builds a selection. It never
// reads without a terminal. The end of input anywhere chooses nothing; three invalid answers to
// one question end the run with ErrTooManyInvalid.
func RunFlow(in *bufio.Reader, out io.Writer, groups []Group, opt Options) (FlowResult, error) {
	none := FlowResult{Outcome: OutcomeNone, Rows: []Row{}, Confirmed: map[string]bool{}}
	if !opt.Interactive {
		none.Outcome = OutcomeNoTTY
		return none, nil
	}
	if len(groups) == 0 {
		return none, nil
	}
	s := &session{in: in, out: out, opt: opt, groups: groups, selected: map[int]Row{}, confirmed: map[string]bool{}, groupOf: map[string]int{}}
	for gi, g := range groups {
		for _, r := range g.Rows {
			s.all = append(s.all, r)
			s.groupOf[r.ID] = gi
		}
	}
	sort.Slice(s.all, func(i, j int) bool { return s.all[i].Number < s.all[j].Number })
	res, err := s.run()
	switch {
	case errors.Is(err, ErrEndOfInput):
		return none, nil
	case errors.Is(err, errQuit):
		fmt.Fprintln(out, "\nQuit; nothing was stored.")
		none.Outcome = OutcomeQuit
		return none, nil
	case err != nil:
		return none, err
	}
	return res, nil
}

type session struct {
	in        *bufio.Reader
	out       io.Writer
	opt       Options
	groups    []Group
	all       []Row
	groupOf   map[string]int
	selected  map[int]Row
	confirmed map[string]bool
}

func (s *session) width() int {
	if s.opt.Width <= 0 {
		return 80
	}
	return s.opt.Width
}

// answer is one line read after a question. A line over 256 bytes is flagged and never matches.
type answer struct {
	text    string
	tooLong bool
}

func (s *session) ask(question string) (answer, error) {
	fmt.Fprint(s.out, question)
	l, ok := readLine(s.in)
	if !ok {
		return answer{}, ErrEndOfInput
	}
	return answer{text: strings.TrimSpace(l.text), tooLong: l.tooLong}, nil
}

// reject counts one invalid answer to the current question: the third ends the run.
func (s *session) reject(tries *int, message string) error {
	*tries++
	if *tries >= MaxAttempts {
		return ErrTooManyInvalid
	}
	fmt.Fprintln(s.out, message)
	return nil
}

func (s *session) run() (FlowResult, error) {
	skipSteps, err := s.first()
	if err != nil {
		return FlowResult{}, err
	}
	if !skipSteps {
		for i := range s.groups {
			if err := s.step(i); err != nil {
				return FlowResult{}, err
			}
		}
	}
	return s.summary()
}

// first is the question after the overview: Enter goes through the groups, `a` takes all green rows
// and goes to the summary, `q` quits.
func (s *session) first() (skipSteps bool, err error) {
	for tries := 0; ; {
		a, err := s.ask("Continue? [Enter] step through the groups  [a] take all green  [q] quit: ")
		if err != nil {
			return false, err
		}
		if !a.tooLong {
			switch a.text {
			case "":
				return false, nil
			case "a":
				s.addGreen()
				return true, nil
			case "q":
				return false, errQuit
			}
		}
		if err := s.reject(&tries, "Answer Enter, a or q."); err != nil {
			return false, err
		}
	}
}

func (s *session) addGreen() {
	for _, r := range s.all {
		if r.Takeable() && r.Level == risk.Green {
			s.selected[r.Number] = r
		}
	}
}

func (s *session) take(rows []Row) (n int) {
	for _, r := range rows {
		if r.Takeable() {
			s.selected[r.Number] = r
			n++
		}
	}
	return n
}

// step shows one group and asks what to do with it.
func (s *session) step(i int) error {
	g := s.groups[i]
	s.showGroup(i, g)
	def, defWord := "skip", false
	if g.Level == risk.Green {
		def, defWord = "take", true
	}
	for tries := 0; ; {
		a, err := s.ask(fmt.Sprintf("Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = %s): ", def))
		if err != nil {
			return err
		}
		if a.tooLong {
			if err := s.reject(&tries, stepHelp); err != nil {
				return err
			}
			continue
		}
		switch a.text {
		case "":
			if defWord {
				s.takeGroup(g)
			}
			return nil
		case "y":
			s.takeGroup(g)
			return nil
		case "s":
			return nil
		case "q":
			return errQuit
		case "l":
			if err := s.list(g); err != nil {
				return err
			}
			continue
		case "r":
			return s.review(g)
		}
		if rest, ok := strings.CutPrefix(a.text, "d "); ok {
			if s.details(strings.TrimSpace(rest)) {
				continue
			}
			if err := s.reject(&tries, "That number is not offered."); err != nil {
				return err
			}
			continue
		}
		rows, err := ParseSelection(a.text, s.all)
		if err != nil {
			if err := s.reject(&tries, err.Error()+". "+stepHelp); err != nil {
				return err
			}
			continue
		}
		added, err := s.addTerms(rows)
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "Added %d %s; %d chosen so far. This group is still open.\n", added, plural(added, "row", "rows"), len(s.selected))
	}
}

const stepHelp = "Answer y, s, l, r, q, d N, or numbers like 1,2 or 3-5 or a selector (a b w t o:LABEL m:REPO, except)."

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (s *session) takeGroup(g Group) {
	if n := s.take(g.Rows); n == 0 {
		fmt.Fprintln(s.out, "Nothing to take here: a decision row is chosen by its own number.")
	}
}

// addTerms adds the rows of a line of terms; a decision row first asks its own confirmation.
func (s *session) addTerms(rows []Row) (int, error) {
	added := 0
	for _, r := range rows {
		if r.Verdict == Decision && !s.confirmed[r.ID] {
			yes, err := s.confirmDecision(r)
			if err != nil {
				return added, err
			}
			if !yes {
				continue
			}
		}
		if _, had := s.selected[r.Number]; !had {
			added++
		}
		s.selected[r.Number] = r
	}
	return added, nil
}

// confirmDecision is the second confirmation of a decision row (Spec 041), naming the lock the
// caller put into the detail.
func (s *session) confirmDecision(r Row) (bool, error) {
	fmt.Fprintf(s.out, "[%d] is a decision row: %s\n", r.Number, escape(r.Detail))
	yes, err := YesNo(s.in, s.out, "Choose it anyway?", true)
	if err != nil {
		return false, err
	}
	if yes {
		s.confirmed[r.ID] = true
	}
	return yes, nil
}

// details prints `d N`; false when the number is not offered.
func (s *session) details(n string) bool {
	if !digitsOnly(n) || len(n) > 9 {
		return false
	}
	num, _ := strconv.Atoi(n)
	for _, r := range s.all {
		if r.Number == num {
			detail := escape(r.Detail)
			if detail == "" {
				detail = "no details"
			}
			fmt.Fprintf(s.out, "[%d] %s: %s\n", r.Number, s.rowSubject(r), detail)
			return true
		}
	}
	return false
}

func (s *session) rowSubject(r Row) string {
	if r.Path != "" {
		return escape(r.Path)
	}
	return escape(r.Branch)
}

// list shows every row of the group, 20 at a time.
func (s *session) list(g Group) error {
	for i := 0; i < len(g.Rows); i += pageSize {
		end := min(i+pageSize, len(g.Rows))
		for _, r := range g.Rows[i:end] {
			fmt.Fprintln(s.out, s.rowLine(r))
		}
		if end < len(g.Rows) {
			a, err := s.ask(fmt.Sprintf("-- %d more (Enter = next %d, q = stop): ", len(g.Rows)-end, pageSize))
			if err != nil {
				return err
			}
			if a.text == "q" {
				return nil
			}
		}
	}
	return nil
}

// review asks about each row of the group. A decision row is named and never asked here.
func (s *session) review(g Group) error {
rows:
	for _, r := range g.Rows {
		fmt.Fprintln(s.out, s.rowLine(r))
		if !r.Takeable() {
			fmt.Fprintf(s.out, "      [%d] is a decision row: choose it by its own number\n", r.Number)
			continue
		}
		for tries := 0; ; {
			a, err := s.ask("Take it? [y/n/q] (default n): ")
			if err != nil {
				return err
			}
			if !a.tooLong {
				switch a.text {
				case "y":
					s.selected[r.Number] = r
					continue rows
				case "", "n":
					continue rows
				case "q":
					break rows
				}
			}
			if err := s.reject(&tries, "Answer y, n or q (default n)."); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- screens ----

func levelCounts(c risk.Counts) string {
	var parts []string
	for _, p := range []struct {
		n    int
		name string
	}{{c.Green, "green"}, {c.Yellow, "yellow"}, {c.Orange, "orange"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.name))
		}
	}
	if len(parts) > 1 {
		return " (" + strings.Join(parts, ", ") + ")"
	}
	return ""
}

func (s *session) showGroup(i int, g Group) {
	fmt.Fprintf(s.out, "\nStep %d of %d   %s   risk %s%s\n", i+1, len(s.groups), g.Title, g.Level, levelCounts(g.Counts))
	for _, l := range g.Lines {
		fmt.Fprintln(s.out, l)
	}
	if s.width() >= 40 {
		for _, r := range g.Rows[:min(3, len(g.Rows))] {
			fmt.Fprintln(s.out, s.rowLine(r))
		}
	}
	if len(g.Rows) > 3 || s.width() < 40 {
		shown := min(3, len(g.Rows))
		if s.width() < 40 {
			shown = 0
		}
		fmt.Fprintf(s.out, "      ... %d more (l = list)\n", len(g.Rows)-shown)
	}
	fmt.Fprintln(s.out)
}

// rowLine is one row of a list: number, level, the path or branch, then the branch of a directory
// or the repository of a branch. Below 60 columns the last column goes; the line is cut to the width.
func (s *session) rowLine(r Row) string {
	width := s.width()
	head := fmt.Sprintf("  %5d  %-6s  ", r.Number, r.Level)
	subject, extra := escape(r.Branch), escape(r.Repo)
	if r.Path != "" {
		subject, extra = escape(r.Path), escape(r.Branch)
	}
	tail := ""
	if r.Verdict == Decision {
		tail = "  (decision: choose it by number)"
	}
	if width >= 60 && extra != "" {
		tail = "  " + extra + tail
	}
	room := width - len([]rune(head)) - len([]rune(tail))
	if room < 12 {
		room = 12
		tail = ""
	}
	if rs := []rune(subject); len(rs) > room {
		subject = string(rs[:room-3]) + "..."
	}
	return head + subject + tail
}

// escape is `esc` without the 120-character cut, for a detail the person asked for; the text is
// still bounded.
func escape(s string) string {
	rs := []rune(s)
	cut := false
	if len(rs) > 2000 {
		rs, cut = rs[:2000], true
	}
	var b strings.Builder
	for len(rs) > 0 {
		n := min(len(rs), 100)
		b.WriteString(untrusted.Esc(string(rs[:n])))
		rs = rs[n:]
	}
	if cut {
		b.WriteString("...")
	}
	return b.String()
}
