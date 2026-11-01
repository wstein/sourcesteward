// SPDX-License-Identifier: EUPL-1.2
package lintpr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Location kinds of a finding.
const (
	LocPRTitle = "pr-title"
	LocPRBody  = "pr-body"
	LocComment = "comment"
	LocCommit  = "commit"
)

// Commit is the part of a commit the rules read.
type Commit struct {
	SHA, Message                  string
	AuthorName, AuthorEmail       string
	CommitterName, CommitterEmail string
}

// Comment is one conversation comment of the pull request.
type Comment struct {
	ID   int64
	Body string
}

// Input is everything the rules read.
type Input struct {
	PR       int
	Title    string
	Body     string
	Commits  []Commit
	Comments []Comment
}

// Identity is a name and an email address.
type Identity struct{ Name, Email string }

// Config is the policy of a run. Without owner identities the two identity rules do not run.
type Config struct{ Owners []Identity }

// Location says where a finding is. ID is the commit SHA or the comment ID (empty for the PR title and
// text); Line is the 1-based line, 0 when the finding concerns the whole text.
type Location struct {
	Kind string
	ID   string
	Line int
}

// Finding is one violation.
type Finding struct {
	Rule     string
	Severity Severity
	Location Location
	Message  string
}

var (
	// The session patterns of the core, assembled so that this source does not hold the trailer in plain text.
	sessionPattern = regexp.MustCompile(`(?i)claude` + `-session:|claude\.ai/code/session_`)
	coAuthored     = regexp.MustCompile(`(?im)^co-authored-by:[ \t]*[^<\r\n]+<[^<>\s]+@[^<>\s]+>[ \t]*\r?$`)
	conventional   = regexp.MustCompile(`\A(?:feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(?:\([A-Za-z0-9._/-]+\))?!?: \S`)
	blockerLine    = regexp.MustCompile(`(?im)^[\s*_#>-]*blockers?\b`)
	authorAgent    = regexp.MustCompile(`(?im)^[\s*_#>-]*author agent\b`)
)

func normalize(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func lineCount(s string) int {
	s = strings.TrimRight(normalize(s), "\n \t")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func sameIdentity(owners []Identity, name, email string) bool {
	for _, o := range owners {
		if o.Name == name && strings.EqualFold(o.Email, email) {
			return true
		}
	}
	return false
}

// Lint applies the rule table. The result is ordered by rule (table order), then by location order of the
// input; it is empty when nothing is violated.
func Lint(in Input, cfg Config) []Finding {
	var out []Finding
	add := func(rule string, sev Severity, loc Location, format string, args ...any) {
		out = append(out, Finding{Rule: rule, Severity: sev, Location: loc, Message: fmt.Sprintf(format, args...)})
	}
	for _, r := range Rules {
		switch r.ID {
		case RuleSessionLink:
			scan := func(kind, id, text string) {
				for i, l := range strings.Split(normalize(text), "\n") {
					if sessionPattern.MatchString(l) {
						add(r.ID, r.Severity, Location{kind, id, i + 1}, "session link")
					}
				}
			}
			scan(LocPRTitle, "", in.Title)
			scan(LocPRBody, "", in.Body)
			for _, c := range in.Comments {
				scan(LocComment, strconv.FormatInt(c.ID, 10), c.Body)
			}
			for _, c := range in.Commits {
				scan(LocCommit, c.SHA, c.Message)
			}
		case RuleAuthorIdentity, RuleCommitterIdentity:
			if len(cfg.Owners) == 0 {
				continue
			}
			for _, c := range in.Commits {
				name, email, role := c.AuthorName, c.AuthorEmail, "author"
				if r.ID == RuleCommitterIdentity {
					name, email, role = c.CommitterName, c.CommitterEmail, "committer"
				}
				if !sameIdentity(cfg.Owners, name, email) {
					add(r.ID, r.Severity, Location{LocCommit, c.SHA, 0}, "%s %q <%s> is not an owner identity", role, name, email)
				}
			}
		case RuleCoAuthoredBy:
			for _, c := range in.Commits {
				if !coAuthored.MatchString(c.Message) {
					add(r.ID, r.Severity, Location{LocCommit, c.SHA, 0}, "no Co-Authored-By trailer with name and email")
				}
			}
		case RuleConventionalCommit:
			for _, c := range in.Commits {
				if s := strings.TrimSuffix(firstLine(c.Message), "\r"); !conventional.MatchString(s) {
					add(r.ID, r.Severity, Location{LocCommit, c.SHA, 1}, "subject is not type(scope): description")
				}
			}
		case RuleBodyLength:
			n := lineCount(in.Body)
			switch {
			case n > r.Hard:
				add(r.ID, SeverityError, Location{LocPRBody, "", 0}, "%d lines, more than %d", n, r.Hard)
			case n > r.Limit:
				add(r.ID, SeverityWarning, Location{LocPRBody, "", 0}, "%d lines, about %d expected", n, r.Limit)
			}
		case RuleBlockerLine:
			if !blockerLine.MatchString(normalize(in.Body)) {
				add(r.ID, r.Severity, Location{LocPRBody, "", 0}, "no Blocker line")
			}
		case RuleAuthorAgentLine:
			if !authorAgent.MatchString(normalize(in.Body)) {
				add(r.ID, r.Severity, Location{LocPRBody, "", 0}, "no Author agent line")
			}
		case RuleReviewLine:
			for _, c := range in.Comments {
				if !isReviewComment(c.Body) {
					continue
				}
				id := strconv.FormatInt(c.ID, 10)
				l, ok := ParseReviewLine(firstLine(c.Body))
				switch {
				case !ok:
					add(r.ID, r.Severity, Location{LocComment, id, 1}, "first line does not follow the review line grammar")
				case l.PR != in.PR:
					add(r.ID, r.Severity, Location{LocComment, id, 1}, "pr=%d, this is pull request %d", l.PR, in.PR)
				}
			}
		case RuleReviewLength:
			for _, c := range in.Comments {
				if !isReviewComment(c.Body) {
					continue
				}
				if n := lineCount(c.Body); n > r.Limit {
					add(r.ID, r.Severity, Location{LocComment, strconv.FormatInt(c.ID, 10), 0}, "%d lines, at most %d", n, r.Limit)
				}
			}
		}
	}
	return out
}
