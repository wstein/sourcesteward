// SPDX-License-Identifier: EUPL-1.2
package issues

import "github.com/wstein/sourcesteward/internal/hosting"

// The two words of --assignee that are not logins; `@` cannot occur in a login.
const (
	AssigneeMe   = "@me"
	AssigneeNone = "@none"
)

// keep reports whether a decoded issue passes the filters. Everything is matched locally, with
// ASCII case folding and exactly, against all labels and assignees the forge returned.
func (c *cycle) keep(it hosting.IssueListItem) bool {
	return anyOf(c.o.Labels, func(f string) bool { return containsFold(it.Labels, f) }) &&
		anyOf(c.o.Assignees, func(f string) bool { return c.assigned(it, f) }) &&
		anyOf(c.o.Authors, func(f string) bool { return asciiEqual(it.Author, f) })
}

func (c *cycle) assigned(it hosting.IssueListItem, filter string) bool {
	switch filter {
	case AssigneeNone:
		return len(it.Assignees) == 0
	case AssigneeMe:
		return containsFold(it.Assignees, c.viewer)
	}
	return containsFold(it.Assignees, filter)
}

// anyOf is true for an empty filter and otherwise when one alternative matches.
func anyOf(filters []string, match func(string) bool) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		if match(f) {
			return true
		}
	}
	return false
}
