// SPDX-License-Identifier: EUPL-1.2
package conflicts

import (
	"reflect"
	"testing"
)

func node(n int, base, headRef string, state string) Node {
	return Node{Number: n, Repo: "o/a", Base: base, HeadRef: headRef, Head: sh("0"), Review: "clear", Checks: "green", Finding: Finding{Number: n, State: state}}
}

func levelOf(t *testing.T, stacks []Stack, n int) Level {
	t.Helper()
	for _, s := range stacks {
		for _, l := range s.Levels {
			if l.Number == n {
				return l
			}
		}
	}
	t.Fatalf("#%d is in no stack", n)
	return Level{}
}

func TestBuildStacksStatus(t *testing.T) {
	nodes := []Node{
		node(10, "main", "a", StateClean),
		node(11, "a", "b", StateConflicts),
		node(12, "b", "c", StateClean),
		node(13, "a", "d", StateClean),
		node(20, "main", "x", StateClean), // single, clean: not shown
		node(30, "main", "y", StateConflicts),
	}
	st := BuildStacks(nodes)
	if len(st) != 2 || st[0].Root != 10 || st[1].Root != 30 {
		t.Fatalf("stacks: %+v", st)
	}
	order := []int{}
	for _, l := range st[0].Levels {
		order = append(order, l.Number)
	}
	if !reflect.DeepEqual(order, []int{10, 11, 12, 13}) {
		t.Fatalf("depth-first order %v", order)
	}
	cases := []struct {
		n       int
		depth   int
		parent  int
		blocked int
		status  string
	}{
		{10, 0, 0, 0, ""},
		{11, 1, 10, 0, StatusRestack},
		{12, 2, 11, 11, "blocked by #11"},
		{13, 1, 10, 0, ""},
		{30, 0, 0, 0, StatusRestack},
	}
	for _, c := range cases {
		l := levelOf(t, st, c.n)
		if l.Depth != c.depth || l.Parent != c.parent || l.BlockedBy != c.blocked || l.Status != c.status {
			t.Errorf("#%d: %+v", c.n, l)
		}
	}
}

func TestBuildStacksBlockedByIsTheLowestConflict(t *testing.T) {
	st := BuildStacks([]Node{
		node(1, "main", "a", StateConflicts),
		node(2, "a", "b", StateConflicts),
		node(3, "b", "c", StateClean),
	})
	if l := levelOf(t, st, 3); l.BlockedBy != 1 || l.Status != "blocked by #1" {
		t.Fatalf("%+v", l)
	}
	if l := levelOf(t, st, 2); l.BlockedBy != 1 || l.Status != StatusRestack {
		t.Fatalf("a conflicting child keeps its own status and names the parent: %+v", l)
	}
}

func TestBuildStacksIgnoresForksAmbiguityAndCycles(t *testing.T) {
	fork := node(2, "a", "b", StateClean)
	fork.Fork = true
	if st := BuildStacks([]Node{node(1, "main", "a", StateConflicts), fork}); len(st) != 1 || len(st[0].Levels) != 1 {
		t.Fatalf("a fork pull request is not a child: %+v", st)
	}
	forkParent := node(1, "main", "a", StateConflicts)
	forkParent.Fork = true
	if st := BuildStacks([]Node{forkParent, node(2, "a", "b", StateClean)}); levelOf(t, st, 1).Status != StatusRestack || len(st[0].Levels) != 1 {
		t.Fatalf("a fork pull request is not a parent: %+v", st)
	}
	// two pull requests with the same head ref: the parent is ambiguous, so the child is a root
	st := BuildStacks([]Node{node(1, "main", "a", StateClean), node(2, "main", "a", StateConflicts), node(3, "a", "c", StateClean)})
	if l := levelOf(t, st, 2); l.Status != StatusRestack {
		t.Fatalf("%+v", l)
	}
	for _, s := range st {
		for _, l := range s.Levels {
			if l.Number == 3 {
				t.Fatalf("an ambiguous parent must not link the child: %+v", st)
			}
		}
	}
	// a cycle (not possible on GitHub) is cut, nothing loops
	cyc := BuildStacks([]Node{node(1, "b", "a", StateConflicts), node(2, "a", "b", StateClean)})
	if n := 0; len(cyc) == 0 {
		t.Fatal("cycle dropped everything")
	} else {
		for _, s := range cyc {
			n += len(s.Levels)
		}
		if n != 2 {
			t.Fatalf("cycle: %+v", cyc)
		}
	}
	// a pull request whose base equals its own head ref is not its own parent
	self := BuildStacks([]Node{node(1, "a", "a", StateConflicts)})
	if len(self) != 1 || self[0].Levels[0].Parent != 0 {
		t.Fatalf("%+v", self)
	}
}

func TestLevelColumns(t *testing.T) {
	cases := []struct{ review, want string }{
		{"clear", "valid"}, {"clear-carried", "valid"}, {"clear-stale", "invalid"},
		{"none", "none"}, {"findings", "none"}, {"unparsed", "none"}, {"", "unknown"},
	}
	for _, c := range cases {
		n := node(1, "main", "a", StateConflicts)
		n.Review = c.review
		if got := BuildStacks([]Node{n})[0].Levels[0].Clear; got != c.want {
			t.Errorf("review %q: clear %q, want %q", c.review, got, c.want)
		}
	}
	n := node(1, "main", "a", StateConflicts)
	n.Checks = ""
	l := BuildStacks([]Node{n})[0].Levels[0]
	if l.CI != "-" || l.Conflict != StateConflicts || l.Head != sh("0") {
		t.Fatalf("%+v", l)
	}
	n.Finding = Finding{}
	if l := BuildStacks([]Node{n, node(2, "a", "b", StateClean)})[0].Levels[0]; l.Conflict != StateUnknown {
		t.Fatalf("a pull request that was not probed is unknown: %+v", l)
	}
}
