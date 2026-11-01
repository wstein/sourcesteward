// SPDX-License-Identifier: EUPL-1.2
package prviews

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/reviewstate"
)

// pr is a row with the given base and head branch.
func pr(repo string, n int, base, head string) pulls.Row {
	r := ready(repo, n, time.Duration(100-n)*time.Hour, head)
	r.Base = base
	return r
}

// shape renders a forest as "N(children...)" for comparison.
func shape(ns []*Node) string {
	var out []string
	for _, n := range ns {
		s := fmt.Sprintf("%s#%d", n.Repo, n.Number)
		if len(n.Children) > 0 {
			s += "(" + shape(n.Children) + ")"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func TestStacksBuildsTreesByBaseAndHeadBranch(t *testing.T) {
	rows := []pulls.Row{
		pr("o/a", 1, "main", "s1"),
		pr("o/a", 3, "s2", "s3"),
		pr("o/a", 2, "s1", "s2"),
		pr("o/a", 5, "s1", "side"),
		pr("o/a", 9, "main", "single"),
	}
	f := Stacks(rows)
	if got := shape(f.Trees); got != "o/a#1(o/a#2(o/a#3) o/a#5)" || f.Singles != 1 {
		t.Fatalf("%s singles %d", got, f.Singles)
	}
}

func TestStacksDoNotCrossRepositoriesAndForksHaveNoChildren(t *testing.T) {
	fork := pr("o/a", 2, "s1", "fork-head")
	fork.Fork = true
	rows := []pulls.Row{
		pr("o/a", 1, "main", "s1"),
		pr("o/b", 4, "s1", "x"), // same branch name in another repository
		fork,
		pr("o/a", 7, "fork-head", "y"), // based on a branch of the fork
	}
	f := Stacks(rows)
	if got := shape(f.Trees); got != "o/a#1(o/a#2)" {
		t.Fatalf("%s", got)
	}
	// o/b#4 and o/a#7 have a base that is not the default branch and no parent: roots with the hint
	rows[1].Hints = []string{reviewstate.HintBaseNotDefault}
	rows[3].Hints = []string{reviewstate.HintBaseNotDefault}
	f = Stacks(rows)
	if got := shape(f.Trees); got != "o/a#1(o/a#2) o/a#7 o/b#4" || f.Singles != 0 {
		t.Fatalf("orphan bases are shown: %s singles %d", got, f.Singles)
	}
}

func TestStacksRepositoryNamesCompareCaseInsensitively(t *testing.T) {
	rows := []pulls.Row{pr("O/A", 1, "main", "s1"), pr("o/a", 2, "s1", "s2")}
	if got := shape(Stacks(rows).Trees); got != "O/A#1(o/a#2)" {
		t.Fatalf("%s", got)
	}
}

func TestStacksLowestNumberIsTheParentWhenSeveralMatch(t *testing.T) {
	rows := []pulls.Row{pr("o/a", 4, "main", "dup"), pr("o/a", 2, "main", "dup"), pr("o/a", 6, "dup", "x")}
	f := Stacks(rows)
	if got := shape(f.Trees); got != "o/a#2(o/a#6)" { // o/a#4 has no child and is a single
		t.Fatalf("%s", got)
	}
	if f.Singles != 1 {
		t.Fatalf("singles %d", f.Singles)
	}
}

func TestStacksCycleIsCut(t *testing.T) {
	rows := []pulls.Row{pr("o/a", 1, "b", "a"), pr("o/a", 2, "a", "b")}
	f := Stacks(rows)
	if got := shape(f.Trees); got != "o/a#1(o/a#2)" {
		t.Fatalf("%s", got)
	}
}

func TestStacksOrderOfTrees(t *testing.T) {
	rows := []pulls.Row{
		pr("o/b", 1, "main", "b1"), pr("o/b", 2, "b1", "b2"),
		pr("o/a", 8, "main", "a8"), pr("o/a", 9, "a8", "a9"),
		pr("o/a", 3, "main", "a3"), pr("o/a", 4, "a3", "a4"),
	}
	rows[2].CreatedAt = t0.Add(-time.Hour) // o/a#8 is newer than o/a#3
	rows[4].CreatedAt = t0.Add(-100 * time.Hour)
	if got := shape(Stacks(rows).Trees); got != "o/a#3(o/a#4) o/a#8(o/a#9) o/b#1(o/b#2)" {
		t.Fatalf("%s", got)
	}
	// an unknown created_at comes last within the repository
	rows[4].CreatedAt = time.Time{}
	if got := shape(Stacks(rows).Trees); got != "o/a#8(o/a#9) o/a#3(o/a#4) o/b#1(o/b#2)" {
		t.Fatalf("%s", got)
	}
}

func TestStacksNodeCarriesTheFactsOfTheRow(t *testing.T) {
	r := pr("o/a", 1, "main", "s1")
	c := pr("o/a", 2, "s1", "s2")
	c.Checks, c.Review, c.Hints, c.Draft, c.Group = nil, nil, []string{reviewstate.HintBaseNotDefault}, true, pulls.GroupDraft
	f := Stacks([]pulls.Row{r, c})
	root, kid := f.Trees[0], f.Trees[0].Children[0]
	if root.Group != "ready" || root.Checks != "green" || root.Review != "clear" || root.Title != "title 1" || root.URL == "" || root.Base != "main" || root.HeadRef != "s1" || root.Draft {
		t.Fatalf("%+v", root)
	}
	if kid.Checks != "" || kid.Review != "" || !kid.Draft || kid.Group != "draft" || len(kid.Hints) != 1 {
		t.Fatalf("an unread value is empty: %+v", kid)
	}
	if root.Hints == nil || kid.Children == nil {
		t.Fatalf("hints and children are never nil: %#v %#v", root.Hints, kid.Children)
	}
}

func TestStacksEmpty(t *testing.T) {
	f := Stacks(nil)
	if f.Trees == nil || len(f.Trees) != 0 || f.Singles != 0 {
		t.Fatalf("%#v", f)
	}
}

func TestStacksRowsWithoutHeadBranchOrHintsAreHandledSafely(t *testing.T) {
	a := pr("o/a", 1, "main", "")
	a.Hints = nil
	b := pr("o/a", 2, "", "b2") // a base that names no branch is nobody's child
	f := Stacks([]pulls.Row{a, b})
	if len(f.Trees) != 0 || f.Singles != 2 {
		t.Fatalf("%+v", f)
	}
	c := pr("o/a", 3, "s", "s3")
	c.Hints = nil
	d := pr("o/a", 4, "s3", "s4")
	f = Stacks([]pulls.Row{c, d})
	if f.Trees[0].Hints == nil || f.Trees[0].Children[0].Hints == nil {
		t.Fatalf("hints are never nil")
	}
}

func TestStacksTreesAreOrderedByRepositoryNameIgnoringCase(t *testing.T) {
	rows := []pulls.Row{pr("B/x", 1, "main", "s1"), pr("B/x", 2, "s1", "s2"), pr("a/y", 3, "main", "t1"), pr("a/y", 4, "t1", "t2")}
	if got := shape(Stacks(rows).Trees); got != "a/y#3(a/y#4) B/x#1(B/x#2)" {
		t.Fatalf("%s", got)
	}
}
