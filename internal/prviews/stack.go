// SPDX-License-Identifier: EUPL-1.2
package prviews

import (
	"sort"
	"strings"

	"github.com/wstein/sourcesteward/internal/pulls"
	"github.com/wstein/sourcesteward/internal/reviewstate"
)

// Node is one pull request in a stack. Checks and Review are empty when they were not read; Hints and
// Children are never nil.
type Node struct {
	Repo     string
	Number   int
	Title    string
	URL      string
	Draft    bool
	Group    string
	Checks   string
	Review   string
	Hints    []string
	Base     string
	HeadRef  string
	Children []*Node
}

// Forest is the stack view: the trees worth printing and the number of single pull requests left out.
type Forest struct {
	Trees   []*Node
	Singles int
}

// Stacks builds the forest of open pull requests: within a repository B is a child of A when B.base is the
// head branch of A and A is not a fork pull request. With several candidates the lowest number is the
// parent. A cycle is cut by making its lowest number a root. Only trees with a child and roots whose base
// is not the default branch are returned; the other roots count as singles.
func Stacks(rows []pulls.Row) Forest {
	type item struct {
		row      pulls.Row
		node     *Node
		parent   *item
		children []*item
		seen     bool
	}
	items := make([]*item, len(rows))
	byHead := map[string][]*item{}
	for i, r := range rows {
		it := &item{row: r}
		items[i] = it
		if !r.Fork && r.HeadRef != "" {
			k := key(r.Repo, r.HeadRef)
			byHead[k] = append(byHead[k], it)
		}
	}
	for _, it := range items {
		var best *item
		for _, c := range byHead[key(it.row.Repo, it.row.Base)] {
			if c != it && (best == nil || c.row.Number < best.row.Number) {
				best = c
			}
		}
		it.parent = best
		if best != nil {
			best.children = append(best.children, it)
		}
	}
	var walk func(it *item) *Node
	walk = func(it *item) *Node {
		it.seen = true
		r := it.row
		n := &Node{Repo: r.Repo, Number: r.Number, Title: r.Title, URL: r.URL, Draft: r.Draft, Group: r.Group, Base: r.Base, HeadRef: r.HeadRef,
			Hints: append([]string{}, r.Hints...), Children: []*Node{}}
		if r.Checks != nil {
			n.Checks = *r.Checks
		}
		if r.Review != nil {
			n.Review = r.Review.State
		}
		kids := it.children
		sort.SliceStable(kids, func(a, b int) bool { return kids[a].row.Number < kids[b].row.Number })
		for _, c := range kids {
			if !c.seen {
				n.Children = append(n.Children, walk(c))
			}
		}
		return n
	}
	type tree struct {
		row  pulls.Row
		node *Node
	}
	var trees []tree
	for _, it := range items {
		if it.parent == nil {
			trees = append(trees, tree{it.row, walk(it)})
		}
	}
	// what no root reaches is a cycle: its lowest number becomes a root
	rest := append([]*item{}, items...)
	sort.SliceStable(rest, func(a, b int) bool { return rest[a].row.Number < rest[b].row.Number })
	for _, it := range rest {
		if !it.seen {
			trees = append(trees, tree{it.row, walk(it)})
		}
	}
	sort.SliceStable(trees, func(a, b int) bool { return rootLess(trees[a].row, trees[b].row) })
	nodes := make([]*Node, len(trees))
	for i, t := range trees {
		nodes[i] = t.node
	}
	f := Forest{Trees: []*Node{}}
	for _, n := range nodes {
		if len(n.Children) > 0 || hasHintName(n.Hints, reviewstate.HintBaseNotDefault) {
			f.Trees = append(f.Trees, n)
		} else {
			f.Singles++
		}
	}
	return f
}

func hasHintName(hints []string, h string) bool {
	for _, x := range hints {
		if x == h {
			return true
		}
	}
	return false
}

// rootLess orders roots by repository, then created_at (unknown last), then number.
func rootLess(a, b pulls.Row) bool {
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
}
