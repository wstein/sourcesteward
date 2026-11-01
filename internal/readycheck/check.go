// SPDX-License-Identifier: EUPL-1.2

// Package readycheck implements `stw ready-check` (Spec 048): the read-only preconditions for
// marking a pull request Ready. Evaluate is pure; Run gathers the facts with GET reads only.
package readycheck

import (
	"fmt"
	"regexp"
	"strings"

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
}

// Condition is one precondition with its evidence.
type Condition struct {
	ID       string
	Status   string
	Evidence string
}

var (
	// blockerLine matches "Blocker: x", "- **Blocker:** x" and "> Blocker: x" at the start of a line.
	blockerLine = regexp.MustCompile(`(?i)^[\s>*-]*(?:\*\*|__)?blockers?(?:\*\*|__)?\s*:\s*(?:\*\*|__)?\s*(.*)$`)
	parentNote  = regexp.MustCompile(`(?im)^[\s>*-]*merge #([1-9][0-9]*) first\b`)
	noBlocker   = map[string]bool{"none": true, "keiner": true, "keine": true, "n/a": true, "-": true, "no": true}
)

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
	return Condition{ID: id, Status: status, Evidence: fmt.Sprintf(format, args...)}
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
	case f.RepoErr != "" || f.DefaultBranch == "":
		return c(CondBase, StatusUnknown, "default branch not read: %s", esc(f.RepoErr))
	case f.BaseRef == f.DefaultBranch:
		return c(CondBase, StatusPass, "base %s is the default branch", esc(f.BaseRef))
	}
	if m := parentNote.FindStringSubmatch(f.Body); m != nil {
		return c(CondBase, StatusPass, "stack child on %s, text says merge #%s first (parent not checked)", esc(f.BaseRef), m[1])
	}
	return c(CondBase, StatusFail, "base %s is not the default branch %s and the text has no \"Merge #N first\" line", esc(f.BaseRef), esc(f.DefaultBranch))
}

func blocker(f Facts) Condition {
	if f.PullErr != "" {
		return c(CondBlocker, StatusUnknown, "text not read: %s", esc(f.PullErr))
	}
	for _, line := range strings.Split(f.Body, "\n") {
		m := blockerLine.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if m == nil {
			continue
		}
		v := strings.Trim(strings.ToLower(strings.TrimSpace(m[1])), ".*_ ")
		if noBlocker[v] {
			continue
		}
		if strings.TrimSpace(m[1]) == "" {
			return c(CondBlocker, StatusFail, "a Blocker line has no value")
		}
		return c(CondBlocker, StatusFail, "Blocker line: %s", esc(strings.TrimSpace(m[1])))
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
