// SPDX-License-Identifier: EUPL-1.2

// Package issues is the stateless, read-only cycle behind `stw issues` (Spec 038): list the
// open issues of a set of repositories, give every issue one priority by the ordered rule
// table, apply the filters and the row cap locally and count what was read. It writes
// nothing: no file, no state, no forge write.
package issues

import (
	"strings"

	"github.com/wstein/sourcesteward/internal/hosting"
)

// Priorities in the fixed key order of the JSON document.
const (
	Red    = "red"
	Yellow = "yellow"
	Blue   = "blue"
	Violet = "violet"
)

// Reasons of a priority, one per rule of the table.
const (
	ReasonSecurity = "security-label"
	ReasonAssigned = "assigned-to-you"
	ReasonBot      = "bot"
	ReasonBug      = "bug-label"
	ReasonNone     = "none"
)

// classify applies the ordered rule table to a listed issue; the first match wins. Labels are
// matched over all labels the forge returned, never over a display cut. An empty viewer is
// an unknown login and disables rule 2 only.
func classify(it hosting.IssueListItem, viewer string) (priority, reason string) {
	switch {
	case containsFold(it.Labels, "security"):
		return Red, ReasonSecurity
	case viewer != "" && containsFold(it.Assignees, viewer):
		return Yellow, ReasonAssigned
	case isBot(it):
		return Violet, ReasonBot
	case containsFold(it.Labels, "bug"):
		return Yellow, ReasonBug
	}
	return Blue, ReasonNone
}

func isBot(it hosting.IssueListItem) bool {
	return it.AuthorType == "Bot" || strings.HasSuffix(it.Author, "[bot]")
}

// containsLogin reports whether list holds want, compared with ASCII case folding only.
func containsFold(list []string, want string) bool {
	for _, s := range list {
		if asciiEqual(s, want) {
			return true
		}
	}
	return false
}

// asciiEqual compares a and b byte-wise folding only A-Z to a-z. strings.EqualFold is not
// used on purpose: it folds Unicode, so U+017F and U+212A would match `security` and `bug`.
func asciiEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
