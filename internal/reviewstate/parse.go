// SPDX-License-Identifier: EUPL-1.2

// Package reviewstate is the pure core of the review state of Spec 046: the strict grammar of the
// first line of a pull request comment, the review state derived from the comments of one pull
// request and the derived hints. It reads nothing and writes nothing. A review verdict is a claim of
// a comment, never proof (Spec 046, "Trust model"): the package checks only what the data allows.
package reviewstate

import (
	"regexp"
	"strconv"
	"strings"
)

// Kind classifies the first line of a comment.
type Kind int

// The kinds of a comment: not about a review, a strict review line, a strict carry-over line, or a
// line that talks about a review without following the strict grammar.
const (
	KindIgnored Kind = iota
	KindReview
	KindCarried
	KindUnparsed
)

// Line is the parsed first line of a comment. Verdict is CLEAR or FINDINGS for a review; Head is the
// reviewed head of a review; From and To are the heads of a carry-over.
type Line struct {
	Kind                    Kind
	Verdict                 string
	Head, From, To, PatchID string
	PR                      int
}

const (
	hexSHA   = `[0-9a-f]{40}`
	hexPatch = `(?:[0-9a-f]{40}|[0-9a-f]{64})`
	number   = `([1-9][0-9]{0,8})`
)

var (
	reviewLine  = regexp.MustCompile(`\AReview: (CLEAR|FINDINGS) head=(` + hexSHA + `) patch-id=(` + hexPatch + `) pr=` + number + `\z`)
	carriedLine = regexp.MustCompile(`\ACarried-over: from=(` + hexSHA + `) to=(` + hexSHA + `) patch-id=(` + hexPatch + `) pr=` + number + `\z`)
	// reviewWords marks a line that talks about a review: whole words, case-insensitive.
	reviewWords = regexp.MustCompile(`(?i)(?:\b(?:verdict|urteil|clear|findings|carried-over)\b|\bcarried over\b|\Areview\s*:)`)
)

// firstLine returns the first line of a comment body: up to the first "\n", one trailing "\r" dropped.
func firstLine(body string) string {
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		body = body[:i]
	}
	return strings.TrimSuffix(body, "\r")
}

// ParseLine classifies the first line of body for pull request pr. A strict line for another pull
// request is unparsed: the comment was not written for this one.
func ParseLine(body string, pr int) Line {
	line := firstLine(body)
	if m := reviewLine.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[4])
		if n != pr {
			return Line{Kind: KindUnparsed}
		}
		return Line{Kind: KindReview, Verdict: m[1], Head: m[2], PatchID: m[3], PR: n}
	}
	if m := carriedLine.FindStringSubmatch(line); m != nil {
		n, _ := strconv.Atoi(m[4])
		if n != pr {
			return Line{Kind: KindUnparsed}
		}
		return Line{Kind: KindCarried, From: m[1], To: m[2], PatchID: m[3], PR: n}
	}
	if reviewWords.MatchString(line) {
		return Line{Kind: KindUnparsed}
	}
	return Line{Kind: KindIgnored}
}
