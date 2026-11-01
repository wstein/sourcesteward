// SPDX-License-Identifier: EUPL-1.2
package lintpr

import (
	"regexp"
	"strconv"
	"strings"
)

// ReviewLine is a parsed first line of a review comment.
type ReviewLine struct {
	Carried  bool
	Verdict  string // CLEAR or FINDINGS (review lines)
	Head     string // reviewed head (review lines)
	From, To string // heads of a carry-over
	PatchID  string
	PR       int
}

// The grammar of the agent-rules core: fixed keys in fixed order, full 40-hex SHAs, lower-case hex,
// a 40-hex patch-id, nothing else on the line. This duplicates the grammar of internal/reviewstate (Spec
// 046, PR 349) on purpose: that package is not on main yet. When it is, ParseReviewLine becomes a call
// into it (follow-up). The only difference is the patch-id: reviewstate also admits 64 hex, the core
// text says 40.
const (
	hexSHA = `[0-9a-f]{40}`
	number = `([1-9][0-9]{0,8})`
)

var (
	reviewLine  = regexp.MustCompile(`\AReview: (CLEAR|FINDINGS) head=(` + hexSHA + `) patch-id=(` + hexSHA + `) pr=` + number + `\z`)
	carriedLine = regexp.MustCompile(`\ACarried-over: from=(` + hexSHA + `) to=(` + hexSHA + `) patch-id=(` + hexSHA + `) pr=` + number + `\z`)
	// reviewish marks a comment that claims to be a review: its first non-blank line starts with the key
	// and a colon, ignoring case, blanks and markdown marks.
	reviewish = regexp.MustCompile("(?i)\\A[\\s*_#>`-]*(?:review|carried-over)\\s*:")
)

// ParseReviewLine parses one line (without line break) against the strict grammar.
func ParseReviewLine(line string) (ReviewLine, bool) {
	if m := reviewLine.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[4])
		return ReviewLine{Verdict: m[1], Head: m[2], PatchID: m[3], PR: n}, true
	}
	if m := carriedLine.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[4])
		return ReviewLine{Carried: true, From: m[1], To: m[2], PatchID: m[3], PR: n}, true
	}
	return ReviewLine{}, false
}

// firstLine is the text up to the first "\n"; a "\r" before it stays, so a CRLF line fails the grammar.
func firstLine(body string) string {
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		return body[:i]
	}
	return body
}

// isReviewComment reports whether body claims to be a review (see reviewish).
func isReviewComment(body string) bool {
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		return reviewish.MatchString(l)
	}
	return false
}
