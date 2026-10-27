// SPDX-License-Identifier: EUPL-1.2

package prompt

import (
	"errors"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/wstein/sourcesteward/internal/tidywizard/risk"
)

// Row is one choosable row of the janitor wizard (Spec 044): a safe or decision row whose level is
// green, yellow or orange. Red and kept rows are never a Row, so they have no number and match no
// selector. Every text field is raw data from outside (a path, a branch, an owner label): the
// prompt escapes it where it prints it and compares it as it is.
type Row struct {
	Number  int
	ID      string
	Verdict string // Safe or Decision
	Level   risk.Level
	Kind    string // the candidate kind: "worktree", "branch" or "tracking-ref"
	Repo    string
	Path    string
	Branch  string
	// Owner is the derived label (empty for none); OwnerShown is the wording of the overview. A
	// selector `o:` accepts either, exactly.
	Owner, OwnerShown string
	// Word is the typed confirmation word of a row of another owner or an unknown one, empty for
	// an own row; the summary lists the sorted words.
	Word string
	// Dirs, Branches and Tracking count what the row deletes: directories (for good), branches
	// (restorable) and local tracking references.
	Dirs, Branches, Tracking int
	// Detail is shown by `d N` (raw text, the codes included).
	Detail string
}

// Takeable reports whether a group answer, `a` or a selector may take the row: a safe row, never a
// decision row (which only its own number takes).
func (r Row) Takeable() bool { return r.Verdict == Safe && Choosable(r.Verdict, r.Level) }

// Choosable is the janitor rule of FR-001 and FR-003: a safe or a decision row whose level is
// green, yellow or orange, whoever owns it. A red or kept row, and any level this version does not
// know, is never choosable.
func Choosable(verdict string, level risk.Level) bool {
	if verdict != Safe && verdict != Decision {
		return false
	}
	switch level {
	case risk.Green, risk.Yellow, risk.Orange:
		return true
	}
	return false
}

// MaxSelectionBytes bounds one line of terms, like every answer of the wizard (256 bytes).
const MaxSelectionBytes = maxLine

// ErrSelection is every invalid line of terms; the text is a fixed sentence and never repeats the
// input. ErrLineTooLong is the one for a line over MaxSelectionBytes.
var (
	ErrSelection   = errors.New("not understood")
	ErrLineTooLong = selectionError("the line is longer than 256 bytes")
)

type selectionError string

func (e selectionError) Error() string        { return "not understood: " + string(e) }
func (e selectionError) Is(target error) bool { return target == ErrSelection }

func bad(reason string) error { return selectionError(reason) }

// ParseSelection parses a line of terms (Spec 044 "Keys and grammar"):
//
//	line     = term { (" " | ",") term } [ " except " term { (" " | ",") term } ]
//	term     = NUMBER | NUMBER "-" NUMBER | selector
//	selector = "a" | "b" | "w" | "t" | "o:" LABEL | "m:" REPO
//
// It returns the union of the terms before `except`, minus the union after it, in number order.
// A number takes any listed row, a decision row included; a range and every selector take safe
// rows only, so a decision row is taken by its own number and by nothing else. A number or range
// that is not listed (a red row has no number) is an error, a selector that matches nothing is
// not. A label or repository with a space is quoted: o:"author:Werner Stein".
func ParseSelection(line string, rows []Row) ([]Row, error) {
	if len(line) > MaxSelectionBytes {
		return nil, ErrLineTooLong
	}
	if !utf8.ValidString(line) {
		return nil, bad("the line is not text")
	}
	tokens, err := tokenize(line)
	if err != nil {
		return nil, err
	}
	var plus, minus []token
	cur, seenExcept := &plus, false
	for _, t := range tokens {
		if t.text == "except" && !t.quoted {
			if seenExcept {
				return nil, bad("except is given twice")
			}
			seenExcept, cur = true, &minus
			continue
		}
		*cur = append(*cur, t)
	}
	if len(plus) == 0 {
		return nil, bad("no term before except")
	}
	if seenExcept && len(minus) == 0 {
		return nil, bad("except needs a term")
	}
	// A row that is not choosable (red, kept, any other verdict) has no number here, whatever the
	// caller passed.
	offered := make([]Row, 0, len(rows))
	for _, r := range rows {
		if Choosable(r.Verdict, r.Level) {
			offered = append(offered, r)
		}
	}
	rows = offered
	byNumber := map[int]Row{}
	for _, r := range rows {
		byNumber[r.Number] = r
	}
	chosen := map[int]Row{}
	for _, t := range plus {
		got, err := evaluate(t, rows, byNumber)
		if err != nil {
			return nil, err
		}
		for _, r := range got {
			chosen[r.Number] = r
		}
	}
	for _, t := range minus {
		got, err := evaluate(t, rows, byNumber)
		if err != nil {
			return nil, err
		}
		for _, r := range got {
			delete(chosen, r.Number)
		}
	}
	out := make([]Row, 0, len(chosen))
	for _, r := range chosen {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// token is one term of the line. text is the word, with the quotes of a label removed.
type token struct {
	text   string
	quoted bool
}

// tokenize splits the line at spaces and single commas. A comma may be surrounded by spaces, but
// two commas, a leading one and a trailing one are junk, and so is any control byte. A double
// quote is allowed only as the value of `o:` or `m:`, and must be closed and followed by a
// separator.
func tokenize(line string) ([]token, error) {
	var out []token
	i := 0
	for i < len(line) {
		// A run of separators: spaces and at most one comma, and only between two terms.
		commas, start := 0, i
		for i < len(line) && (line[i] == ' ' || line[i] == ',') {
			if line[i] == ',' {
				commas++
			}
			i++
		}
		if commas > 1 || (commas == 1 && (start == 0 || i == len(line))) {
			return nil, bad("misplaced comma")
		}
		if i == len(line) {
			break
		}
		begin := i
		quoted := false
		var b strings.Builder
		for i < len(line) && line[i] != ' ' && line[i] != ',' {
			c := line[i]
			switch {
			case c < 0x20 || c == 0x7f:
				return nil, bad("a control character")
			case c == '"':
				prefix := line[begin:i]
				if prefix != "o:" && prefix != "m:" {
					return nil, bad("a quote outside a label")
				}
				end := strings.IndexByte(line[i+1:], '"')
				if end < 0 {
					return nil, bad("an unterminated quote")
				}
				value := line[i+1 : i+1+end]
				if value == "" {
					return nil, bad("an empty label")
				}
				if strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
					return nil, bad("a control character")
				}
				b.WriteString(value)
				i += end + 2
				quoted = true
				if i < len(line) && line[i] != ' ' && line[i] != ',' {
					return nil, bad("text after a closing quote")
				}
				continue
			}
			b.WriteByte(c)
			i++
		}
		if quoted {
			out = append(out, token{text: b.String(), quoted: true})
			continue
		}
		out = append(out, token{text: b.String()})
	}
	return out, nil
}

// evaluate resolves one term to rows.
func evaluate(t token, rows []Row, byNumber map[int]Row) ([]Row, error) {
	s := t.text
	switch {
	case s != "" && s[0] >= '0' && s[0] <= '9':
		lo, hi, isRange := s, s, false
		if a, b, ok := strings.Cut(s, "-"); ok {
			lo, hi, isRange = a, b, true
		}
		if !digitsOnly(lo) || !digitsOnly(hi) || len(lo) > 9 || len(hi) > 9 {
			return nil, bad("a number is not a number")
		}
		l, _ := strconv.Atoi(lo)
		h, _ := strconv.Atoi(hi)
		if l > h {
			return nil, bad("a range runs backwards")
		}
		if !isRange {
			r, ok := byNumber[l]
			if !ok {
				return nil, bad("a number is not offered")
			}
			return []Row{r}, nil
		}
		var out []Row
		inRange := 0
		for _, r := range rows {
			if r.Number >= l && r.Number <= h {
				inRange++
				if r.Takeable() {
					out = append(out, r)
				}
			}
		}
		if inRange != h-l+1 {
			return nil, bad("a range holds a number that is not offered")
		}
		return out, nil
	case s == "a":
		return takeable(rows, func(r Row) bool { return r.Level == risk.Green }), nil
	case s == "b":
		return takeable(rows, func(r Row) bool { return r.Kind == "branch" }), nil
	case s == "w":
		return takeable(rows, func(r Row) bool { return r.Kind == "worktree" }), nil
	case s == "t":
		return takeable(rows, func(r Row) bool {
			return r.Kind == "worktree" && strings.HasPrefix(path.Clean(r.Path), "/tmp/")
		}), nil
	case strings.HasPrefix(s, "o:") && len(s) > 2:
		label := s[2:]
		return takeable(rows, func(r Row) bool { return r.Owner == label || r.OwnerShown == label }), nil
	case strings.HasPrefix(s, "m:") && len(s) > 2:
		repo := s[2:]
		return takeable(rows, func(r Row) bool { return r.Repo == repo }), nil
	}
	return nil, bad("a word that is not a term")
}

func takeable(rows []Row, match func(Row) bool) []Row {
	var out []Row
	for _, r := range rows {
		if r.Takeable() && match(r) {
			out = append(out, r)
		}
	}
	return out
}
