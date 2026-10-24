// SPDX-License-Identifier: EUPL-1.2

// Package prompt asks the human which of the overview's rows to plan, and whether to apply the
// stored plan set (Spec 041 T016). It is pure: it reads one *bufio.Reader and writes one io.Writer.
// Answers are parsed strictly and the default is always none/no. Without a terminal it neither
// reads nor asks, so it can never block on stdin and a pipe can never choose anything. The end of
// input anywhere in a choice ends it with nothing chosen. Surrounding spaces around an answer are
// ignored, number tokens may carry leading zeros ("01"), and letters are case-sensitive.
//
// Contract with the caller: every Item.Detail and Item.Owner is text the caller has already put
// through `esc` and its bound (the shared untrusted.Esc); this package prints it after flattening
// control characters to spaces and does no escaping of its own.
package prompt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/wstein/sourcesteward/internal/tidywizard/owner"
)

// The verdicts of choosable rows.
const (
	Safe     = "safe"
	Decision = "decision"
)

// MaxAttempts is how many invalid answers end the run.
const MaxAttempts = 3

// maxLine bounds one answer; a longer line is an invalid answer.
const maxLine = 256

// ErrEndOfInput is what YesNo reports at the end of input: not a "no", so that a caller in the
// middle of a choice can end the whole run instead of going on with a partial answer.
var ErrEndOfInput = errors.New("end of input")

// ErrTooManyInvalid ends the run after MaxAttempts invalid answers: exit 1, nothing is stored.
var ErrTooManyInvalid = errors.New("too many invalid answers; nothing was chosen")

// Item is one choosable row of the overview: a safe or a decision row with its number.
type Item struct {
	Number  int
	ID      string
	Verdict string // Safe or Decision
	Scope   owner.Scope
	Owner   string // owner label, empty for an unknown owner
	// Detail is shown by "d N" and by the confirmation of a decision row. The caller puts the lock of
	// a decision row into it (the confirmation names the lock only through this text) and passes text
	// that already went through `esc`; the prompt only flattens control characters to spaces.
	Detail string
}

// Options say whether the run has a terminal on both streams.
type Options struct {
	Interactive bool
}

// Outcome is what a prompt run ended with.
type Outcome int

// The outcomes. OutcomeNoTTY is the plan-only degradation: exit 0, nothing stored, nothing asked.
const (
	OutcomeNone Outcome = iota
	OutcomeChosen
	OutcomeNoTTY
)

// Result is the chosen ids in number order. Confirmed holds the decision rows whose second
// confirmation was answered "y".
type Result struct {
	Outcome   Outcome
	Chosen    []string
	Confirmed map[string]bool
}

// Select asks "Apply which?" and returns the choice. It never reads without a terminal.
func Select(in *bufio.Reader, out io.Writer, items []Item, opt Options) (Result, error) {
	none := Result{Outcome: OutcomeNone, Chosen: []string{}, Confirmed: map[string]bool{}}
	if !opt.Interactive {
		none.Outcome = OutcomeNoTTY
		return none, nil
	}
	if len(items) == 0 {
		return none, nil
	}
	byNumber := map[int]Item{}
	for _, it := range items {
		byNumber[it.Number] = it
	}
	invalid := 0
	for {
		fmt.Fprint(out, "Apply which? [a]ll own safe  [1,2,6 / 1-2,6]  [o]wner  [d]etails N  [n]one (default n): ")
		line, ok := readLine(in)
		if !ok { // end of input
			return none, nil
		}
		answer := strings.TrimSpace(line.text)
		var chosen []Item
		valid := !line.tooLong
		if valid {
			switch {
			case answer == "" || answer == "n":
				return none, nil
			case answer == "a":
				for _, it := range items {
					if it.Verdict == Safe && it.Scope == owner.ScopeOwn {
						chosen = append(chosen, it)
					}
				}
			case answer == "o":
				var err error
				chosen, err = askOwners(in, out, items)
				if err != nil {
					return none, endOfInputIsNone(err)
				}
			case strings.HasPrefix(answer, "d "):
				n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(answer, "d ")))
				it, found := byNumber[n]
				if err != nil || !found || !digitsOnly(strings.TrimSpace(strings.TrimPrefix(answer, "d "))) {
					valid = false
					break
				}
				fmt.Fprintf(out, "[%d] %s\n", it.Number, sanitize(it.Detail))
				continue
			default:
				chosen, valid = parseNumbers(answer, byNumber)
			}
		}
		if !valid {
			invalid++
			if invalid == MaxAttempts {
				return none, ErrTooManyInvalid
			}
			fmt.Fprintln(out, "Not understood; answer a, n, o, d N, numbers like 1,2 or 1-3, or nothing for none.")
			continue
		}
		res, err := finish(in, out, chosen)
		if err != nil {
			return none, endOfInputIsNone(err)
		}
		return res, nil
	}
}

// endOfInputIsNone turns the end of input inside a nested question into "nothing chosen" (the
// wizard then behaves as plan without --save); any other error stays an error.
func endOfInputIsNone(err error) error {
	if errors.Is(err, ErrEndOfInput) {
		return nil
	}
	return err
}

// finish asks the second confirmation of each decision row and builds the result.
func finish(in *bufio.Reader, out io.Writer, chosen []Item) (Result, error) {
	res := Result{Outcome: OutcomeNone, Chosen: []string{}, Confirmed: map[string]bool{}}
	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Number < chosen[j].Number })
	seen := map[int]bool{}
	for _, it := range chosen {
		if seen[it.Number] {
			continue
		}
		seen[it.Number] = true
		if it.Verdict == Decision {
			fmt.Fprintf(out, "[%d] is a decision row: %s\n", it.Number, sanitize(it.Detail))
			yes, err := YesNo(in, out, "Choose it anyway?", true)
			if err != nil {
				return Result{Outcome: OutcomeNone, Chosen: []string{}, Confirmed: map[string]bool{}}, err
			}
			if !yes {
				continue
			}
			res.Confirmed[it.ID] = true
		}
		res.Chosen = append(res.Chosen, it.ID)
	}
	if len(res.Chosen) > 0 {
		res.Outcome = OutcomeChosen
	}
	return res, nil
}

// askOwners asks, per owner label in sorted order, whether to take that owner's safe items. A
// decision row is never taken by an owner answer. An unknown (empty) owner is not asked.
func askOwners(in *bufio.Reader, out io.Writer, items []Item) ([]Item, error) {
	byOwner := map[string][]Item{}
	for _, it := range items {
		if it.Verdict == Safe && it.Owner != "" {
			byOwner[it.Owner] = append(byOwner[it.Owner], it)
		}
	}
	labels := make([]string, 0, len(byOwner))
	for l := range byOwner {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	var chosen []Item
	for _, l := range labels {
		yes, err := YesNo(in, out, fmt.Sprintf("Take the %d safe item(s) of owner %s?", len(byOwner[l]), sanitize(l)), true)
		if err != nil {
			return nil, err
		}
		if yes {
			chosen = append(chosen, byOwner[l]...)
		}
	}
	return chosen, nil
}

// YesNo asks a yes/no question whose default is no. Only an exact "y" (surrounding spaces ignored)
// is yes; an empty line or "n" is no; anything else is asked again, MaxAttempts times in all, then
// ErrTooManyInvalid. The end of input is ErrEndOfInput, also after an invalid answer. Without a
// terminal it neither reads nor asks and answers no.
func YesNo(in *bufio.Reader, out io.Writer, question string, interactive bool) (bool, error) {
	if !interactive {
		return false, nil
	}
	for attempt := 1; ; attempt++ {
		fmt.Fprintf(out, "%s [y/N]: ", question)
		line, ok := readLine(in)
		if !ok {
			return false, ErrEndOfInput
		}
		switch strings.TrimSpace(line.text) {
		case "y":
			if !line.tooLong {
				return true, nil
			}
		case "", "n":
			if !line.tooLong {
				return false, nil
			}
		}
		if attempt == MaxAttempts {
			return false, ErrTooManyInvalid
		}
		fmt.Fprintln(out, "Answer y or n (default n).")
	}
}

// parseNumbers parses "1", "1,2,6", "1-2,6": digits only, every number a listed item, ranges
// ascending with every number of the range listed. Spaces around a token are ignored.
func parseNumbers(answer string, byNumber map[int]Item) ([]Item, bool) {
	var out []Item
	for _, tok := range strings.Split(answer, ",") {
		tok = strings.TrimSpace(tok)
		lo, hi := tok, tok
		if a, b, isRange := strings.Cut(tok, "-"); isRange {
			lo, hi = strings.TrimSpace(a), strings.TrimSpace(b)
		}
		if !digitsOnly(lo) || !digitsOnly(hi) || len(lo) > 9 || len(hi) > 9 {
			return nil, false
		}
		l, _ := strconv.Atoi(lo)
		h, _ := strconv.Atoi(hi)
		if l > h {
			return nil, false
		}
		for n := l; n <= h; n++ {
			it, ok := byNumber[n]
			if !ok {
				return nil, false
			}
			out = append(out, it)
		}
	}
	return out, len(out) > 0
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type line struct {
	text    string
	tooLong bool
}

// readLine reads one line of at most maxLine bytes; a longer line is drained and flagged. ok is
// false at the end of input with nothing read.
func readLine(in *bufio.Reader) (line, bool) {
	var l line
	read := false
	for {
		part, isPrefix, err := in.ReadLine()
		if err != nil {
			return l, read
		}
		read = true
		if len(l.text)+len(part) > maxLine {
			l.tooLong = true
		} else {
			l.text += string(part)
		}
		if !isPrefix {
			return l, true
		}
	}
}

// sanitize keeps a label or detail on one line of the prompt; the real escaping of outside text
// is the renderer's (`esc`), and the caller passes text that already went through it.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}
