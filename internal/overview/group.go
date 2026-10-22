// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"strconv"

	"github.com/wstein/sourcesteward/internal/untrusted"
)

// MaxLinesPerPriority is the cap of the human output: more lines of one priority are counted, not listed.
const MaxLinesPerPriority = 10

// maxLabels is the number of identifiers a grouped line names before `+N more`.
const maxLabels = 3

// Line is one line of the human output: all items with the same (priority, category, repository)
// grouped. A line of one item keeps its URL and command; a grouped line has no URL and carries the
// command all its items share, else the drill-down command of the category, else none.
type Line struct {
	Priority Priority
	Category Category
	Repo     string
	Count    int
	Labels   []string // identifiers of the first three items, escaped
	More     int      // items beyond Labels
	Item     Item     // the first item; the whole line when Count is 1
	URL      string
	Command  string
	Effect   Effect
}

// Section is the human output of one priority: at most MaxLinesPerPriority lines, then the number of
// omitted lines and items and the drill-down command of the first omitted line (its category and repository,
// effect read; empty when the category has none). It is never the command of an item.
type Section struct {
	Priority    Priority
	Lines       []Line
	MoreLines   int
	MoreItems   int
	MoreCommand string
	MoreEffect  Effect
}

// Group groups items into the lines of the human output, per priority in the fixed order; priorities
// without items have no section. The items are sorted first, so the result does not depend on their
// order. Grouping never crosses priorities, and the counts of the lines (and of the omitted ones)
// add up to the number of items; the JSON output lists every item and uses none of this.
func Group(items []Item) []Section {
	var sections []Section
	var lines []Line
	for _, it := range sortItems(items) {
		if n := len(lines); n > 0 && lines[n-1].Priority == it.Priority && lines[n-1].Category == it.Category && lines[n-1].Repo == it.Repo {
			lines[n-1] = lines[n-1].add(it)
			continue
		}
		lines = append(lines, newLine(it))
	}
	for _, p := range Priorities {
		var s Section
		for _, l := range lines {
			if l.Priority != p {
				continue
			}
			if len(s.Lines) < MaxLinesPerPriority {
				s.Lines = append(s.Lines, l.finish())
				continue
			}
			if s.MoreLines == 0 {
				s.MoreCommand, s.MoreEffect = drillDown(l.Category, l.Repo)
			}
			s.MoreLines++
			s.MoreItems += l.Count
		}
		if s.Lines != nil {
			s.Priority = p
			sections = append(sections, s)
		}
	}
	return sections
}

func newLine(it Item) Line {
	l := Line{Priority: it.Priority, Category: it.Category, Repo: it.Repo, Item: it}
	return l.add(it)
}

// add counts one item; Command starts as the command of the first item and becomes empty when another differs.
func (l Line) add(it Item) Line {
	if l.Count == 0 {
		l.Command, l.Effect = it.Command, it.Effect
	} else if l.Command != it.Command {
		l.Command, l.Effect = "", ""
	}
	l.Count++
	if len(l.Labels) < maxLabels {
		l.Labels = append(l.Labels, label(it.Subject))
	} else {
		l.More++
	}
	return l
}

// finish settles the URL and command of a line: one item keeps its own, a group has no URL and falls back to
// the drill-down command of its category when its items do not share a command.
func (l Line) finish() Line {
	if l.Count == 1 {
		l.URL = l.Item.URL
		return l
	}
	if l.Command == "" {
		l.Command, l.Effect = drillDown(l.Category, l.Repo)
	}
	return l
}

// drillDown is the command that lists what a grouped line stands for, for the categories of one remote repository.
func drillDown(c Category, repo string) (string, Effect) {
	switch c {
	case PRReady, PRConflict, PRBlocked, PRChecksPending, PRUnread:
		return buildCommand(PRBlocked, target{Slug: repo})
	case PRBot:
		return buildCommand(PRBot, target{Slug: repo})
	case IssueSecurity, IssueAssigned, IssueBug:
		if untrusted.ValidRepo(repo) {
			return issuesDrillDown(repo), Read
		}
	case ReleaseDraft:
		if untrusted.ValidRepo(repo) {
			return "gh release list -R " + repo, Read
		}
	}
	return "", ""
}

// label is the identifier of a subject in a grouped line: `#N` for a pull request, else the tag or name.
func label(s Subject) string {
	switch s.Kind {
	case KindPR, KindIssue:
		return "#" + strconv.Itoa(s.Number)
	case KindRelease, KindTag:
		return untrusted.Esc(s.Tag)
	}
	return untrusted.Esc(s.Name)
}
