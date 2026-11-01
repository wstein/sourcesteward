// SPDX-License-Identifier: EUPL-1.2

// Package readycheck implements `stw ready-check` (Spec 048): the read-only preconditions for
// marking a pull request Ready. Evaluate is pure; Run gathers the facts with GET reads only.
package readycheck

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/snapcmd"
	"github.com/wstein/sourcesteward/internal/untrusted"
)

// Condition ids, in the fixed order of the document.
const (
	CondOpen       = "open"
	CondReview     = "review"
	CondChecks     = "checks"
	CondBase       = "base"
	CondBlocker    = "blocker"
	CondConflict   = "conflict"
	CondRebaseable = "rebaseable"
)

// Statuses of a condition. Only pass counts; unknown (a read failed or GitHub has not computed a
// value yet) never passes.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusUnknown = "unknown"
)

// Review is what the review-state parser of Spec 046 reports for the current head. State is one of
// none, findings, clear, clear-carried, clear-stale, unparsed. Head is the head named by the
// decisive comment (for a carry-over the head of the CLEAR it carries).
type Review struct {
	State   string
	Head    string
	Carried bool
}

// Facts is everything Evaluate looks at. A non-empty *Err says that part could not be read.
type Facts struct {
	PullErr, RepoErr, ChecksErr, ReviewErr string

	State          string // open, closed or merged
	Draft          bool
	Head           string
	BaseRef        string
	Body           string
	Mergeable      *bool
	MergeableState string
	Rebaseable     *bool

	DefaultBranch string
	ChecksClass   string // none|pending|green|red at the exact head
	Review        Review

	// ParentPR is the lowest number of another open pull request in the repository whose head
	// branch is this pull request's base branch, 0 if none. It is read only when the base is not
	// the default branch; ParentErr says that read failed.
	ParentPR  int
	ParentErr string
}

// Condition is one precondition with its evidence.
type Condition struct {
	ID       string
	Status   string
	Evidence string
}

var (
	headingLine  = regexp.MustCompile(`^\s*#{1,6}(\s|$)`)
	numberPrefix = regexp.MustCompile(`^[0-9]+[.)]\s+`)
	listMarker   = regexp.MustCompile(`^(?:[-*+]|[0-9]+[.)])\s+`)
	blockerWord  = regexp.MustCompile(`(?i)\bblockers?\b`)
	noBlocker    = map[string]bool{"none": true, "keiner": true, "keine": true, "n/a": true, "-": true, "no": true}
	emphasis     = strings.NewReplacer("*", "", "_", "", "`", "")
)

// NoneSpellings lists the values that mean "no blocker", sorted; the command help names them all.
func NoneSpellings() []string {
	out := make([]string, 0, len(noBlocker))
	for k := range noBlocker {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// maxEvidence is the schema limit of an evidence string (characters; bytes here, which is stricter).
const maxEvidence = 400

func esc(s string) string { return untrusted.Esc(s) }

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// Evaluate returns the seven conditions in fixed order. It never reads anything.
func Evaluate(f Facts) []Condition {
	return []Condition{open(f), review(f), checks(f), base(f), blocker(f), conflict(f), rebaseable(f)}
}

// Ready reports whether every condition passes.
func Ready(cs []Condition) bool {
	for _, c := range cs {
		if c.Status != StatusPass {
			return false
		}
	}
	return len(cs) > 0
}

func c(id, status, format string, args ...any) Condition {
	return Condition{ID: id, Status: status, Evidence: snapcmd.Cut(fmt.Sprintf(format, args...), maxEvidence)}
}

func open(f Facts) Condition {
	switch {
	case f.PullErr != "":
		return c(CondOpen, StatusUnknown, "pull request not read: %s", esc(f.PullErr))
	case f.State == "open":
		return c(CondOpen, StatusPass, "state open")
	default:
		return c(CondOpen, StatusFail, "state %s", esc(f.State))
	}
}

func review(f Facts) Condition {
	switch r := f.Review; {
	case f.ReviewErr != "":
		return c(CondReview, StatusUnknown, "review state not read: %s", esc(f.ReviewErr))
	case f.PullErr != "":
		return c(CondReview, StatusUnknown, "head not read: %s", esc(f.PullErr))
	case r.State == "clear" && r.Head == f.Head:
		return c(CondReview, StatusPass, "CLEAR for head %s per comment", short(f.Head))
	case r.State == "clear":
		return c(CondReview, StatusFail, "CLEAR names head %s, current head is %s", short(r.Head), short(f.Head))
	case r.State == "clear-carried" && r.Carried:
		return c(CondReview, StatusPass, "CLEAR of %s carried over to head %s per comment", short(r.Head), short(f.Head))
	case r.State == "clear-carried":
		return c(CondReview, StatusFail, "carried-over state without a carry-over chain")
	case r.State == "clear-stale":
		return c(CondReview, StatusFail, "CLEAR exists only for an older head %s", short(r.Head))
	case r.State == "findings":
		return c(CondReview, StatusFail, "review of head %s has findings", short(f.Head))
	case r.State == "none":
		return c(CondReview, StatusFail, "no CLEAR for head %s", short(f.Head))
	case r.State == "unparsed":
		return c(CondReview, StatusUnknown, "an unparsed review comment is newer than the last strict one")
	default:
		return c(CondReview, StatusUnknown, "review state %q is not known", esc(r.State))
	}
}

func checks(f Facts) Condition {
	switch {
	case f.ChecksErr != "":
		return c(CondChecks, StatusUnknown, "checks not read: %s", esc(f.ChecksErr))
	case f.PullErr != "":
		return c(CondChecks, StatusUnknown, "head not read: %s", esc(f.PullErr))
	case f.ChecksClass == "green":
		return c(CondChecks, StatusPass, "all check runs and statuses of head %s are green", short(f.Head))
	case f.ChecksClass == "red":
		return c(CondChecks, StatusFail, "checks of head %s are red", short(f.Head))
	case f.ChecksClass == "pending":
		return c(CondChecks, StatusFail, "checks of head %s are pending", short(f.Head))
	case f.ChecksClass == "none":
		return c(CondChecks, StatusFail, "no checks reported for head %s", short(f.Head))
	default:
		return c(CondChecks, StatusUnknown, "check class %q is not known", esc(f.ChecksClass))
	}
}

func base(f Facts) Condition {
	switch {
	case f.PullErr != "":
		return c(CondBase, StatusUnknown, "base not read: %s", esc(f.PullErr))
	case f.RepoErr != "":
		return c(CondBase, StatusUnknown, "default branch not read: %s", esc(f.RepoErr))
	case f.DefaultBranch == "":
		return c(CondBase, StatusUnknown, "default branch unknown")
	case f.BaseRef == f.DefaultBranch:
		return c(CondBase, StatusPass, "base %s is the default branch", esc(f.BaseRef))
	case f.ParentErr != "":
		return c(CondBase, StatusUnknown, "open pull requests not read: %s", esc(f.ParentErr))
	case f.ParentPR > 0:
		return c(CondBase, StatusPass, "stack child: base %s is the head branch of open #%d, merge #%d first", esc(f.BaseRef), f.ParentPR, f.ParentPR)
	}
	return c(CondBase, StatusFail, "base %s is not the default branch %s and no other open pull request has it as head branch", esc(f.BaseRef), esc(f.DefaultBranch))
}

// noBlockerValue reports whether v, a value of a Blocker line or one line of a Blocker section,
// is one of the none spellings (list marker, emphasis and a trailing period ignored).
func noBlockerValue(v string) bool {
	v = strings.TrimSpace(emphasis.Replace(v))
	v = strings.TrimSpace(listMarker.ReplaceAllString(v, ""))
	v = strings.Trim(v, "| \t")
	return noBlocker[strings.ToLower(strings.TrimRight(v, ".,; "))]
}

// blockerSep finds the word "blocker" or "blockers" followed, after an optional parenthesis, by a
// colon, a pipe or a dash (an em or en dash, or a hyphen that is followed by a space). The value of
// the Blocker line is the text after it. "Blocker-free" and prose that only uses the word do not match.
var blockerSep = regexp.MustCompile(`(?i)\bblockers?\b\s*(?:\([^)]*\))?\s*(?::|\||[\x{2013}\x{2014}]|-(?:\s|$))`)

// blockerValues returns the values of the Blocker labels of one line. section is true when the line
// has a Blocker label without a value (the last label is empty, or the line is a Blocker heading or a
// bare "Blocker" word): the lines below it then hold the value. Quote, list, number and heading
// marks and emphasis are ignored, so "## Blocker", "**Blocker (Draft):** x", "1. Blocker: x",
// "> Blocker: x", "Status: Draft. Blocker: x", "Blocker - x" and "| Blocker | x |" all match.
func blockerValues(line string) (values []string, section bool, ok bool) {
	heading := headingLine.MatchString(line)
	l := strings.TrimSpace(emphasis.Replace(line))
	matches := blockerSep.FindAllStringIndex(l, -1)
	for k, m := range matches {
		stop := len(l)
		if k+1 < len(matches) {
			stop = matches[k+1][0]
		}
		v := strings.Trim(strings.TrimSpace(l[m[1]:stop]), "| \t")
		values = append(values, v)
		section = v == ""
	}
	if len(matches) > 0 {
		return values, section, true
	}
	bare := strings.TrimSpace(numberPrefix.ReplaceAllString(strings.TrimLeft(l, " \t#>-+"), ""))
	if (heading && blockerWord.MatchString(bare)) || strings.EqualFold(bare, "blocker") || strings.EqualFold(bare, "blockers") {
		return nil, true, true
	}
	return nil, false, false
}

func blocker(f Facts) Condition {
	if f.PullErr != "" {
		return c(CondBlocker, StatusUnknown, "text not read: %s", esc(f.PullErr))
	}
	lines := strings.Split(f.Body, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		values, section, ok := blockerValues(line)
		if !ok {
			continue
		}
		for _, v := range values {
			if v != "" && !noBlockerValue(v) {
				return c(CondBlocker, StatusFail, "Blocker line: %s", esc(v))
			}
		}
		if !section {
			continue
		}
		// No value on the line: the lines below it hold the value. Below a heading they run to the
		// next heading; below any other line to the first blank line after a content line.
		heading := headingLine.MatchString(line)
		var below []string
		j := i + 1
		for ; j < len(lines); j++ {
			l := strings.TrimRight(lines[j], "\r")
			if headingLine.MatchString(l) || (!heading && strings.TrimSpace(l) == "" && len(below) > 0) {
				break
			}
			if strings.TrimSpace(l) != "" {
				below = append(below, l)
			}
		}
		if len(below) == 0 {
			return c(CondBlocker, StatusFail, "a Blocker line has no value")
		}
		for _, l := range below {
			if !noBlockerValue(l) {
				return c(CondBlocker, StatusFail, "Blocker section: %s", esc(strings.TrimSpace(l)))
			}
		}
		i = j - 1
	}
	return c(CondBlocker, StatusPass, "no Blocker line names a blocker")
}

func conflict(f Facts) Condition {
	switch {
	case f.PullErr != "":
		return c(CondConflict, StatusUnknown, "mergeability not read: %s", esc(f.PullErr))
	case f.Mergeable == nil:
		return c(CondConflict, StatusUnknown, "mergeable not computed by GitHub yet, run again")
	case !*f.Mergeable || f.MergeableState == "dirty":
		return c(CondConflict, StatusFail, "conflict with the base (mergeable %t, state %s)", *f.Mergeable, esc(f.MergeableState))
	default:
		return c(CondConflict, StatusPass, "no conflict with the base (state %s)", esc(f.MergeableState))
	}
}

func rebaseable(f Facts) Condition {
	switch {
	case f.PullErr != "":
		return c(CondRebaseable, StatusUnknown, "rebaseable not read: %s", esc(f.PullErr))
	case f.Rebaseable == nil:
		return c(CondRebaseable, StatusUnknown, "rebaseable not computed by GitHub yet, run again")
	case !*f.Rebaseable:
		return c(CondRebaseable, StatusFail, "rebaseable is false")
	default:
		return c(CondRebaseable, StatusPass, "rebaseable is true")
	}
}
