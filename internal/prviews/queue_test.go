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

var t0 = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

func sha(c string) string { return strings.Repeat(c, 40) }

// ready is a ready-mergeable row of repo with the given head branch, created at t0 minus age.
func ready(repo string, n int, age time.Duration, headRef string) pulls.Row {
	checks, state := "green", "clean"
	return pulls.Row{
		Repo: repo, Number: n, Title: fmt.Sprintf("title %d", n), URL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, n),
		Base: "main", HeadRef: headRef, Head: sha(fmt.Sprintf("%x", n%15+1)), CreatedAt: t0.Add(-age), Group: pulls.GroupReady, Ready: true, Detail: true,
		Checks: &checks, MergeableState: &state,
		Review: &pulls.ReviewInfo{State: reviewstate.StateClear, Verdict: "CLEAR", Head: sha(fmt.Sprintf("%x", n%15+1)), At: t0.Add(-time.Hour)},
		Hints:  []string{reviewstate.HintReadyMergeable},
	}
}

func child(repo string, n int, base, headRef string) pulls.Row {
	r := ready(repo, n, time.Hour, headRef)
	r.Base, r.Hints, r.Group, r.Ready = base, []string{reviewstate.HintBaseNotDefault}, pulls.GroupChecks, false
	return r
}

func numbers(es []Entry) string {
	var out []string
	for _, e := range es {
		out = append(out, fmt.Sprintf("%s#%d", e.Repo, e.Number))
	}
	return strings.Join(out, ",")
}

func TestOwnerQueueListsOnlyReadyMergeableRows(t *testing.T) {
	notClear := ready("o/a", 2, time.Hour, "b2")
	notClear.Hints = nil
	draft := ready("o/a", 3, time.Hour, "b3")
	draft.Draft, draft.Hints = true, []string{reviewstate.HintClearNotReady}
	es, _ := OwnerQueue([]pulls.Row{ready("o/a", 1, time.Hour, "b1"), notClear, draft})
	if numbers(es) != "o/a#1" {
		t.Fatalf("%s", numbers(es))
	}
	if es[0].Position != 1 || es[0].Title != "title 1" || es[0].URL == "" || es[0].Head != sha("2") || es[0].Review != "clear" {
		t.Fatalf("%+v", es[0])
	}
}

func TestOwnerQueueOrder(t *testing.T) {
	rows := []pulls.Row{
		ready("o/b", 1, 5*time.Hour, "x1"),
		ready("O/A", 9, 2*time.Hour, "x2"),
		ready("o/a", 4, 3*time.Hour, "x3"),
		ready("o/a", 3, 3*time.Hour, "x4"),
		ready("o/a", 7, 10*time.Hour, "x5"),
	}
	noTime := ready("o/a", 1, 0, "x6")
	noTime.CreatedAt = time.Time{}
	rows = append(rows, noTime)
	es, _ := OwnerQueue(rows)
	if got := numbers(es); got != "o/a#7,o/a#3,o/a#4,O/A#9,o/a#1,o/b#1" {
		t.Fatalf("oldest first per repository, then number, unknown time last, repositories by name: %s", got)
	}
	for i, e := range es {
		if e.Position != i+1 {
			t.Fatalf("positions %+v", es)
		}
	}
}

func TestOwnerQueueThenNamesTheOpenDescendants(t *testing.T) {
	root := ready("o/a", 1, 9*time.Hour, "stack-1")
	rows := []pulls.Row{
		root,
		child("o/a", 3, "stack-2", "stack-3"),
		child("o/a", 2, "stack-1", "stack-2"),
		child("o/a", 5, "stack-1", "side"),
		child("o/b", 4, "stack-1", "other-repo"),
	}
	fork := child("o/a", 6, "stack-1", "fork-head")
	fork.Fork = true
	rows = append(rows, fork, child("o/a", 7, "fork-head", "grandchild-of-a-fork"))
	es, _ := OwnerQueue(rows)
	if len(es) != 1 || fmt.Sprint(es[0].Then) != "[2 3 5 6]" {
		t.Fatalf("a descendant has the head branch of its parent as base, in the same repository: %+v", es)
	}
	// the head of a fork pull request is not a branch of the repository: its children do not follow
	rows2 := []pulls.Row{root, child("o/a", 6, "stack-1", "fork-head"), child("o/a", 7, "fork-head", "x")}
	rows2[1].Fork = true
	if es, _ := OwnerQueue(rows2); fmt.Sprint(es[0].Then) != "[6]" {
		t.Fatalf("%+v", es[0].Then)
	}
	es, _ = OwnerQueue([]pulls.Row{root})
	if es[0].Then == nil || len(es[0].Then) != 0 {
		t.Fatalf("then is an empty list, never null: %#v", es[0].Then)
	}
}

func TestOwnerQueueDescendantsTerminateOnACycle(t *testing.T) {
	a := ready("o/a", 1, time.Hour, "a")
	b := child("o/a", 2, "a", "b")
	c := child("o/a", 3, "b", "a") // not possible on GitHub; must not loop
	es, _ := OwnerQueue([]pulls.Row{a, b, c})
	if fmt.Sprint(es[0].Then) != "[2 3]" {
		t.Fatalf("%+v", es[0].Then)
	}
}

func TestOwnerQueueWaitingForAParent(t *testing.T) {
	draft := child("o/a", 4, "x", "y")
	draft.Draft = true
	unread := child("o/a", 5, "x", "z")
	unread.Hints = nil
	_, waiting := OwnerQueue([]pulls.Row{ready("o/a", 1, time.Hour, "b"), child("o/a", 2, "b", "c"), child("o/a", 3, "c", "d"), draft, unread})
	if waiting != 2 {
		t.Fatalf("non-draft rows with base-not-default: %d", waiting)
	}
}

func TestOwnerQueueReasonIsBuiltFromFacts(t *testing.T) {
	r := ready("o/a", 1, time.Hour, "b")
	r.Review.At = time.Date(2026, 11, 1, 11, 5, 59, 0, time.FixedZone("CEST", 2*3600))
	es, _ := OwnerQueue([]pulls.Row{r})
	want := "CLEAR for head 2222222 per comment 2026-11-01T09:05Z, checks green, mergeable clean, base main"
	if es[0].Reason != want {
		t.Fatalf("%q\nwant %q", es[0].Reason, want)
	}
	r.Review = &pulls.ReviewInfo{State: reviewstate.StateClearCarried, Verdict: "CLEAR", Head: sha("e"), Carried: true, At: r.Review.At}
	es, _ = OwnerQueue([]pulls.Row{r})
	want = "CLEAR (carried from eeeeeee) for head 2222222 per comment 2026-11-01T09:05Z, checks green, mergeable clean, base main"
	if es[0].Reason != want || es[0].Review != "clear-carried" {
		t.Fatalf("%q", es[0].Reason)
	}
}

func TestOwnerQueueIgnoresAHintWithoutAReview(t *testing.T) {
	r := ready("o/a", 1, time.Hour, "b")
	r.Review = nil
	if es, _ := OwnerQueue([]pulls.Row{r}); len(es) != 0 {
		t.Fatalf("%+v", es)
	}
}
