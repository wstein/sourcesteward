// SPDX-License-Identifier: EUPL-1.2
package reviewstate

import (
	"sort"
	"time"
)

// The review states (Spec 046).
const (
	StateNone         = "none"
	StateFindings     = "findings"
	StateClear        = "clear"
	StateClearCarried = "clear-carried"
	StateClearStale   = "clear-stale"
	StateUnparsed     = "unparsed"
)

// States lists the states in the order of the documents.
var States = []string{StateNone, StateFindings, StateClear, StateClearCarried, StateClearStale, StateUnparsed}

// maxLinks bounds a carry-over chain.
const maxLinks = 8

// Comment is one pull request comment as the review state needs it.
type Comment struct {
	ID                   int64
	CreatedAt, UpdatedAt time.Time
	Body, URL            string
}

// Input is everything one evaluation looks at. HeadTime is the committer date of Head; the zero
// value means it is unknown, and then no comment can be shown to postdate the head commit.
type Input struct {
	PR        int
	Head      string
	HeadTime  time.Time
	Comments  []Comment
	Truncated bool
}

// Result is the review state with the evidence of its decisive comment. For a carry-over, Head and
// PatchID are those of the CLEAR it carries, At and URL those of the last carry-over.
type Result struct {
	State, Verdict, Head, PatchID, URL string
	At                                 time.Time
	Carried, Edited, Truncated         bool
}

type parsed struct {
	Comment
	Line
}

func (p parsed) before(o parsed) bool {
	if !p.CreatedAt.Equal(o.CreatedAt) {
		return p.CreatedAt.Before(o.CreatedAt)
	}
	return p.ID < o.ID
}

func parseAll(pr int, comments []Comment) (strict, unparsed []parsed) {
	for _, c := range comments {
		l := ParseLine(c.Body, pr)
		switch l.Kind {
		case KindReview, KindCarried:
			strict = append(strict, parsed{c, l})
		case KindUnparsed:
			unparsed = append(unparsed, parsed{c, l})
		}
	}
	sort.SliceStable(strict, func(i, j int) bool { return strict[i].before(strict[j]) })
	return strict, unparsed
}

// NeedsHeadTime reports whether Evaluate needs the committer date of head: only when a strict
// comment names the head (a review of it or a carry-over to it).
func NeedsHeadTime(pr int, head string, comments []Comment) bool {
	strict, _ := parseAll(pr, comments)
	for _, p := range strict {
		if (p.Kind == KindReview && p.Head == head) || (p.Kind == KindCarried && p.To == head) {
			return true
		}
	}
	return false
}

// root is a CLEAR review reached directly or through a chain of carry-overs.
type root struct {
	review parsed
	links  int
}

// origin finds the valid CLEAR for head F as it stood at time limit (inclusive): the latest of the
// direct reviews of F and the valid carry-overs to F, which must be a CLEAR (or a chain from one).
func origin(strict []parsed, f string, limit time.Time, used map[int64]bool, links int) (root, parsed, bool) {
	var best parsed
	var bestRoot root
	found := false
	for _, p := range strict {
		if p.CreatedAt.After(limit) || used[p.ID] {
			continue
		}
		switch {
		case p.Kind == KindReview && p.Head == f:
			if !found || best.before(p) {
				best, bestRoot, found = p, root{review: p}, true
				if p.Verdict != "CLEAR" {
					bestRoot = root{}
				}
			}
		case p.Kind == KindCarried && p.To == f && links < maxLinks:
			r, ok := carried(strict, p, used, links)
			if !ok {
				continue
			}
			if !found || best.before(p) {
				best, bestRoot, found = p, r, true
			}
		}
	}
	return bestRoot, best, found && bestRoot.review.Kind == KindReview
}

// carried checks one carry-over: its origin is a CLEAR with the same patch-id, posted not after it.
func carried(strict []parsed, c parsed, used map[int64]bool, links int) (root, bool) {
	used[c.ID] = true
	defer delete(used, c.ID)
	r, _, ok := origin(strict, c.From, c.CreatedAt, used, links+1)
	if !ok || r.review.PatchID != c.PatchID {
		return root{}, false
	}
	r.links++
	return r, true
}

// Evaluate derives the review state of one pull request from its comments.
func Evaluate(in Input) Result {
	strict, unparsed := parseAll(in.PR, in.Comments)
	res := Result{State: StateNone, Truncated: in.Truncated}

	// The decisive comment: the latest direct review of the head or valid carry-over to it, both
	// created at or after the committer date of the head commit.
	var decisive *parsed
	var decisiveRoot root
	if !in.HeadTime.IsZero() {
		for i := range strict {
			p := strict[i]
			if p.CreatedAt.Before(in.HeadTime) {
				continue
			}
			var r root
			switch {
			case p.Kind == KindReview && p.Head == in.Head:
				r = root{review: p}
			case p.Kind == KindCarried && p.To == in.Head:
				var ok bool
				if r, ok = carried(strict, p, map[int64]bool{}, 0); !ok {
					continue
				}
			default:
				continue
			}
			decisive, decisiveRoot = &strict[i], r
		}
	}

	var last time.Time // time of the latest strict comment, the base of the unparsed rule
	for _, p := range strict {
		last = p.CreatedAt
	}

	switch {
	case decisive != nil:
		last = decisive.CreatedAt
		res.At, res.URL, res.Edited = decisive.CreatedAt, decisive.URL, decisive.UpdatedAt.After(decisive.CreatedAt)
		if decisive.Kind == KindCarried {
			res.State, res.Verdict, res.Carried = StateClearCarried, "CLEAR", true
			res.Head, res.PatchID = decisiveRoot.review.Head, decisiveRoot.review.PatchID
		} else {
			res.Verdict, res.Head, res.PatchID = decisive.Verdict, decisive.Head, decisive.PatchID
			res.State = StateClear
			if decisive.Verdict == "FINDINGS" {
				res.State = StateFindings
			}
		}
	default:
		if st := latestStale(strict, in.Head); st != nil {
			res.State, res.Verdict, res.Head, res.PatchID = StateClearStale, "CLEAR", st.Head, st.PatchID
			res.At, res.URL, res.Edited = st.CreatedAt, st.URL, st.UpdatedAt.After(st.CreatedAt)
		}
	}
	for _, u := range unparsed {
		if u.CreatedAt.After(last) {
			return Result{State: StateUnparsed, Truncated: in.Truncated}
		}
	}
	return res
}

// latestStale returns the latest CLEAR review of a head other than head that is still the latest
// Review verdict of that head.
func latestStale(strict []parsed, head string) *parsed {
	latest := map[string]*parsed{}
	for i := range strict {
		p := &strict[i]
		if p.Kind == KindReview && p.Head != head {
			latest[p.Head] = p
		}
	}
	var best *parsed
	for _, p := range latest {
		if p.Verdict == "CLEAR" && (best == nil || best.before(*p)) {
			best = p
		}
	}
	return best
}
