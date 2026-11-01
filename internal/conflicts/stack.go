// SPDX-License-Identifier: EUPL-1.2

package conflicts

import (
	"fmt"
	"sort"
)

// Status texts of a level.
const (
	StatusRestack = "blocked: restack needed"
)

// Node is one open pull request as the stack view sees it.
type Node struct {
	Number  int
	Repo    string
	Base    string
	HeadRef string
	Head    string
	Fork    bool
	Draft   bool
	Checks  string // none, pending, green, red; "" when not read
	Review  string // review state of Spec 046; "" when not read
	Finding Finding
}

// Level is one pull request in a stack with the status that follows from the conflicts below it.
type Level struct {
	Number    int
	Depth     int
	Parent    int // 0 for a root
	Head      string
	Clear     string // valid, invalid, none, unknown
	CI        string
	Conflict  string // the state of the finding; unknown when the pull request was not probed
	Status    string // StatusRestack, "blocked by #n" or empty
	BlockedBy int    // lowest conflicting ancestor, 0 when none
}

// Stack is a tree of pull requests of one repository in depth-first order.
type Stack struct {
	Root   int
	Levels []Level
}

// BuildStacks arranges the nodes in trees: B is the child of A when B.Base is A.HeadRef and both
// are pull requests of the repository (no fork). A head ref shared by several pull requests is
// ambiguous and links nothing. A tree is returned when it has a child or its root conflicts.
func BuildStacks(nodes []Node) []Stack {
	sorted := append([]Node{}, nodes...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].Number < sorted[b].Number })
	byRef := map[string][]int{}
	for i, n := range sorted {
		if !n.Fork {
			byRef[n.HeadRef] = append(byRef[n.HeadRef], i)
		}
	}
	parent := make([]int, len(sorted))
	for i, n := range sorted {
		parent[i] = -1
		if cands := byRef[n.Base]; !n.Fork && len(cands) == 1 {
			parent[i] = cands[0]
		}
	}
	for i := range sorted { // cut a cycle at the first node (by number) whose walk repeats
		seen := map[int]bool{i: true}
		for p := parent[i]; p >= 0; p = parent[p] {
			if seen[p] {
				parent[i] = -1
				break
			}
			seen[p] = true
		}
	}
	children := make([][]int, len(sorted))
	var roots []int
	for i, p := range parent {
		if p < 0 {
			roots = append(roots, i)
		} else {
			children[p] = append(children[p], i)
		}
	}
	var out []Stack
	for _, r := range roots {
		s := Stack{Root: sorted[r].Number}
		var walk func(i, depth, blockedBy int)
		walk = func(i, depth, blockedBy int) {
			n := sorted[i]
			l := Level{Number: n.Number, Depth: depth, Head: n.Head, Clear: clearOf(n.Review), CI: n.Checks, Conflict: n.Finding.State, BlockedBy: blockedBy}
			if p := parent[i]; p >= 0 {
				l.Parent = sorted[p].Number
			}
			if l.CI == "" {
				l.CI = "-"
			}
			if l.Conflict == "" {
				l.Conflict = StateUnknown
			}
			switch {
			case l.Conflict == StateConflicts:
				l.Status = StatusRestack
			case blockedBy > 0:
				l.Status = fmt.Sprintf("blocked by #%d", blockedBy)
			}
			s.Levels = append(s.Levels, l)
			next := blockedBy
			if next == 0 && l.Conflict == StateConflicts {
				next = n.Number
			}
			for _, c := range children[i] {
				walk(c, depth+1, next)
			}
		}
		walk(r, 0, 0)
		if len(s.Levels) > 1 || s.Levels[0].Conflict == StateConflicts {
			out = append(out, s)
		}
	}
	return out
}

func clearOf(review string) string {
	switch review {
	case "clear", "clear-carried":
		return "valid"
	case "clear-stale":
		return "invalid"
	case "":
		return "unknown"
	}
	return "none"
}
