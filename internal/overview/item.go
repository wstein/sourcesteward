// SPDX-License-Identifier: EUPL-1.2

package overview

import (
	"strconv"
)

// newItem builds an item: the priority comes from the category row, the id is CATEGORY:REPO:SUBJECT
// (never time based) and the command from the validated values of t. The caller passes Title already
// escaped and URL already checked.
func newItem(c Category, repo string, s Subject, t target) Item {
	p, _ := PriorityOf(c)
	it := Item{ID: string(c) + ":" + repo + ":" + subjectID(s), Priority: p, Category: c, Repo: repo, Subject: s}
	it.Command, it.Effect = buildCommand(c, t)
	return it
}

// subjectID is the id part of a subject: the number of a pull request, the tag of a release or tag,
// otherwise the name; `-` when the subject has none.
func subjectID(s Subject) string {
	switch {
	case s.Kind == KindPR:
		return strconv.Itoa(s.Number)
	case s.Kind == KindRelease || s.Kind == KindTag:
		return s.Tag
	case s.Name == "":
		return "-"
	}
	return s.Name
}
