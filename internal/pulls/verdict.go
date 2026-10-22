// SPDX-License-Identifier: EUPL-1.2
package pulls

import "github.com/wstein/sourcesteward/internal/watch"

// Groups of the verdict, in the fixed key order of the JSON document.
const (
	GroupReady    = "ready"
	GroupChecks   = "checks"
	GroupConflict = "conflict"
	GroupDraft    = "draft"
	GroupBlocked  = "blocked"
)

// Reasons of a blocked row, in the order of the specification.
const (
	ReasonChecksRed = "checks-red"
	ReasonNoChecks  = "no-checks"
	ReasonUnknown   = "unknown"
	ReasonReview    = "review-or-protection"
	ReasonUnstable  = "unstable"
	ReasonNotRebase = "not-rebaseable"
	ReasonNotRead   = "not-read"
)

const (
	maxListed       = 20
	defaultLimit    = 200
	defaultParallel = 4
)

// Bounds shared with the command layer.
const (
	MaxString = 2048 // bytes of any JSON string
	MaxLimit  = 500  // --limit
)

// facts is everything the ordered table looks at.
type facts struct {
	draft      bool
	detail     bool   // pulls/N and the checks were read
	headOK     bool   // listed head is valid and pulls/N reports the same head
	class      string // watch check class
	mergeable  *bool
	state      string
	rebaseable *bool
}

// verdict evaluates the ordered table of Spec 036, first match wins. Unknown or
// malformed input never reaches `ready`: it falls through to blocked/unknown.
func verdict(f facts) (group, reason string) {
	switch {
	case f.draft: // 1
		return GroupDraft, ""
	case !f.detail: // 2
		return GroupBlocked, ReasonNotRead
	case !f.headOK: // 3
		return GroupBlocked, ReasonUnknown
	case f.class == watch.ClassRed: // 4
		return GroupBlocked, ReasonChecksRed
	case f.class == watch.ClassNone: // 5
		return GroupBlocked, ReasonNoChecks
	case f.mergeable == nil || f.state == "unknown": // 6
		return GroupBlocked, ReasonUnknown
	case f.class == watch.ClassPending: // 7
		return GroupChecks, ""
	case f.state == "dirty" || !*f.mergeable || f.state == "behind": // 8
		return GroupConflict, ""
	case f.state == "blocked": // 9
		return GroupBlocked, ReasonReview
	case f.state == "unstable": // 10
		return GroupBlocked, ReasonUnstable
	case f.rebaseable != nil && !*f.rebaseable: // 11
		return GroupBlocked, ReasonNotRebase
	case f.state == "has_hooks": // 12
		return GroupBlocked, ReasonUnknown
	case f.class == watch.ClassGreen && *f.mergeable && f.state == "clean" && f.rebaseable != nil && *f.rebaseable: // 13
		return GroupReady, ""
	default: // 14
		return GroupBlocked, ReasonUnknown
	}
}

// ValidSHA reports whether s is 40 lowercase hexadecimal digits.
func ValidSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !(s[i] >= '0' && s[i] <= '9' || s[i] >= 'a' && s[i] <= 'f') {
			return false
		}
	}
	return true
}
