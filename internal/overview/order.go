// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"sort"
	"strings"
)

// allItems collects the items of every repository: a repository in state error yields only its
// repo-error item (its remote sources never ran), the others their local and remote items.
func allItems(in Input) []Item {
	var items []Item
	for _, r := range in.Repos {
		items = append(items, localItems(in.Root, r.Local)...)
		if r.Local.State != "error" {
			items = append(items, remoteItems(r)...)
		}
	}
	return items
}

// rank is the position of a priority in the fixed order; the category number is the position in the table.
func rank(p Priority) int {
	for i, q := range Priorities {
		if q == p {
			return i
		}
	}
	return len(Priorities)
}

func categoryNumber(c Category) int {
	for i, row := range categoryTable {
		if row.id == c {
			return i + 1
		}
	}
	return len(categoryTable) + 1
}

// sortItems orders items by priority, category number, repository name (case-insensitive, then exact),
// then subject (a pull request number numerically, a tag or release by version descending, anything else
// by name), then id and title so the order is total; it drops later items of the same id. The result
// depends only on the set of items, never on their input order.
func sortItems(items []Item) []Item {
	out := append([]Item(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return compareItems(out[i], out[j]) < 0 })
	kept := out[:0]
	for _, it := range out {
		if len(kept) > 0 && kept[len(kept)-1].ID == it.ID {
			continue
		}
		kept = append(kept, it)
	}
	return kept
}

func compareItems(a, b Item) int {
	if c := rank(a.Priority) - rank(b.Priority); c != 0 {
		return c
	}
	if c := categoryNumber(a.Category) - categoryNumber(b.Category); c != 0 {
		return c
	}
	if c := strings.Compare(strings.ToLower(a.Repo), strings.ToLower(b.Repo)); c != 0 {
		return c
	}
	if c := strings.Compare(a.Repo, b.Repo); c != 0 {
		return c
	}
	if c := compareSubjects(a.Subject, b.Subject); c != 0 {
		return c
	}
	if c := strings.Compare(a.ID, b.ID); c != 0 {
		return c
	}
	return strings.Compare(a.Title, b.Title)
}

func compareSubjects(a, b Subject) int {
	switch {
	case a.Kind == KindPR && b.Kind == KindPR:
		return a.Number - b.Number
	case (a.Kind == KindTag || a.Kind == KindRelease) && (b.Kind == KindTag || b.Kind == KindRelease):
		return naturalCompare(b.Tag, a.Tag)
	}
	return strings.Compare(a.Name, b.Name)
}

// naturalCompare compares two names run by run: digit runs by numeric value, other runs bytewise. Tags of
// one repository share a pattern, so this is the version order of `v1.9.0` before `v1.10.0`. Names equal
// in value but spelled differently (`007` and `7`) order bytewise, so the order is total.
func naturalCompare(a, b string) int {
	if c := natural(a, b); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func natural(a, b string) int {
	for a != "" && b != "" {
		ra, rb := run(a), run(b)
		switch {
		case isDigit(a[0]) && isDigit(b[0]):
			na, nb := strings.TrimLeft(ra, "0"), strings.TrimLeft(rb, "0")
			if len(na) != len(nb) {
				return sign(len(na) - len(nb))
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
		default:
			if c := strings.Compare(ra, rb); c != 0 {
				return c
			}
		}
		a, b = a[len(ra):], b[len(rb):]
	}
	return sign(len(a) - len(b))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// run returns the leading run of s: all digits, or all non-digits.
func run(s string) string {
	d := isDigit(s[0])
	i := 1
	for i < len(s) && isDigit(s[i]) == d {
		i++
	}
	return s[:i]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
