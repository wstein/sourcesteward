// SPDX-License-Identifier: EUPL-1.2

// Package prviews builds the owner list and the stack view of Spec 046 from the rows of a pulls
// snapshot. It reads nothing and writes nothing: every statement it makes is derived from the facts
// already on the rows, and a review state stays what a comment claims.
package prviews

import (
	"fmt"
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/reviewstate"
)

// Entry is one pull request the owner can merge now, in merge order.
type Entry struct {
	Position int
	Repo     string
	Number   int
	Title    string
	URL      string
	Head     string
	Review   string // clear or clear-carried
	Reason   string
	Then     []int // open descendants, transitively, in number order; never nil
}

func hasHint(r pulls.Row, h string) bool {
	for _, x := range r.Hints {
		if x == h {
			return true
		}
	}
	return false
}

// OwnerQueue lists the rows with the hint ready-mergeable, per repository (by name, case-insensitive)
// oldest created_at first, then by number; a row without a created_at comes last. The second result
// counts the non-draft rows with base-not-default, which wait for a parent.
func OwnerQueue(rows []pulls.Row) ([]Entry, int) {
	var ready []pulls.Row
	waiting := 0
	for _, r := range rows {
		if hasHint(r, reviewstate.HintReadyMergeable) && r.Review != nil {
			ready = append(ready, r)
		}
		if !r.Draft && hasHint(r, reviewstate.HintBaseNotDefault) {
			waiting++
		}
	}
	sort.SliceStable(ready, func(i, j int) bool {
		a, b := ready[i], ready[j]
		if ra, rb := strings.ToLower(a.Repo), strings.ToLower(b.Repo); ra != rb {
			return ra < rb
		}
		za, zb := a.CreatedAt.IsZero(), b.CreatedAt.IsZero()
		switch {
		case za != zb:
			return zb
		case !za && !a.CreatedAt.Equal(b.CreatedAt):
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.Number < b.Number
	})
	idx := newIndex(rows)
	out := make([]Entry, len(ready))
	for i, r := range ready {
		out[i] = Entry{Position: i + 1, Repo: r.Repo, Number: r.Number, Title: r.Title, URL: r.URL, Head: r.Head, Review: r.Review.State,
			Reason: reason(r), Then: idx.descendants(r)}
	}
	return out, waiting
}

// reason states the facts behind a ready-mergeable row.
func reason(r pulls.Row) string {
	verdict := r.Review.Verdict
	if r.Review.Carried {
		verdict += " (carried from " + short(r.Review.Head) + ")"
	}
	at := "an unknown time"
	if !r.Review.At.IsZero() {
		at = r.Review.At.UTC().Format("2006-01-02T15:04Z")
	}
	checks, state := "unknown", "unknown"
	if r.Checks != nil {
		checks = *r.Checks
	}
	if r.MergeableState != nil {
		state = *r.MergeableState
	}
	return fmt.Sprintf("%s for head %s per comment %s, checks %s, mergeable %s, base %s", verdict, short(r.Head), at, checks, state, r.Base)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// index finds the pull requests that are based on the head branch of another one.
type index struct{ byBase map[string][]pulls.Row }

func key(repo, branch string) string { return strings.ToLower(repo) + "\x00" + branch }

func newIndex(rows []pulls.Row) index {
	idx := index{byBase: map[string][]pulls.Row{}}
	for _, r := range rows {
		k := key(r.Repo, r.Base)
		idx.byBase[k] = append(idx.byBase[k], r)
	}
	return idx
}

// children are the pull requests whose base is the head branch of r. The head branch of a fork pull
// request is not a branch of the repository, so a fork has no children.
func (idx index) children(r pulls.Row) []pulls.Row {
	if r.Fork || r.HeadRef == "" {
		return nil
	}
	return idx.byBase[key(r.Repo, r.HeadRef)]
}

// descendants lists the numbers of the pull requests below r, transitively, in number order. A
// repeated node ends the walk (a cycle cannot exist on GitHub).
func (idx index) descendants(r pulls.Row) []int {
	out := []int{}
	seen := map[int]bool{r.Number: true}
	queue := []pulls.Row{r}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range idx.children(cur) {
			if seen[c.Number] {
				continue
			}
			seen[c.Number] = true
			out = append(out, c.Number)
			queue = append(queue, c)
		}
	}
	sort.Ints(out)
	return out
}
