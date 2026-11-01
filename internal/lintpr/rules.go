// SPDX-License-Identifier: EUPL-1.2

// Package lintpr checks the commits, the text and the comments of one pull request against the
// machine-checkable working rules of the agent-rules core (Spec 050). It is pure: it reads nothing and
// writes nothing. The rules are the data table Rules; the engine in lint.go only interprets it.
package lintpr

// Severity of a finding. Only an error makes `stw lint pr` exit non-zero.
type Severity string

// The severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Rule IDs. They are part of the JSON contract and never change meaning.
const (
	RuleSessionLink        = "session-link"
	RuleAuthorIdentity     = "author-identity"
	RuleCommitterIdentity  = "committer-identity"
	RuleCoAuthoredBy       = "co-authored-by"
	RuleConventionalCommit = "conventional-commit"
	RuleBodyLength         = "pr-body-length"
	RuleBlockerLine        = "pr-blocker-line"
	RuleAuthorAgentLine    = "pr-author-agent-line"
	RuleReviewLine         = "review-line-format"
	RuleReviewLength       = "review-comment-length"
)

// Sources of a rule: a file of the agent-rules checkout (path relative to its root) or this repository.
const (
	SourceCore  = "claude/CLAUDE.md"
	SourceSkill = "claude/skills/agent-workflow/SKILL.md"
	SourceRepo  = "sourcesteward"
)

// Rule is one row of the table. Source and Anchor tie the row to the text it implements: Anchor is a phrase
// that must occur verbatim in Source (checked by TestRulesTableAnchorsInAgentRules against a checkout of
// agent-rules). Limit and Hard are the numeric limits of a length rule: above Limit a warning, above Hard an
// error (Hard equals Limit when the limit is strict).
type Rule struct {
	ID       string
	Severity Severity
	Summary  string
	Source   string
	Anchor   string
	Limit    int
	Hard     int
}

// Rules is the table, in the order findings are reported. The session pattern list is in lint.go next to
// its compiled form; the review line grammar is in review.go.
var Rules = []Rule{
	{ID: RuleSessionLink, Severity: SeverityError, Source: SourceCore,
		Summary: "No session link in commit messages, the PR title, the PR text or comments (a Claude session trailer or a code-session URL of the Claude web host).",
		// Assembled so that this file does not hold the trailer in plain text.
		Anchor: "claude" + "-session:|claude\\.ai/code/session_"},
	{ID: RuleAuthorIdentity, Severity: SeverityError, Source: SourceCore,
		Summary: "The author of every commit is an owner identity.",
		Anchor:  "Autor und Committer sind nur der Owner"},
	{ID: RuleCommitterIdentity, Severity: SeverityError, Source: SourceCore,
		Summary: "The committer of every commit is an owner identity.",
		Anchor:  "Autor und Committer sind nur der Owner"},
	{ID: RuleCoAuthoredBy, Severity: SeverityError, Source: SourceCore,
		Summary: "Every commit message has a Co-Authored-By trailer with a name and an email.",
		Anchor:  "atomare Conventional Commits (mit `Co-Authored-By`)"},
	{ID: RuleConventionalCommit, Severity: SeverityError, Source: SourceCore,
		Summary: "The subject of every commit is a Conventional Commit: type(scope)!: description.",
		Anchor:  "atomare Conventional Commits"},
	{ID: RuleBodyLength, Severity: SeverityWarning, Source: SourceSkill, Limit: 25, Hard: 30,
		Summary: "The PR text is about 25 lines: a warning above 25, an error above 30.",
		Anchor:  "PR-Text, zusammen ca. 25 Zeilen"},
	{ID: RuleBlockerLine, Severity: SeverityError, Source: SourceSkill,
		Summary: "The PR text has a Blocker line (the named blocker or none).",
		Anchor:  `Blocker oder "keiner"`},
	{ID: RuleAuthorAgentLine, Severity: SeverityError, Source: SourceRepo,
		Summary: "The PR text has an Author agent line naming the author agent (repository convention, no core text).",
	},
	{ID: RuleReviewLine, Severity: SeverityError, Source: SourceCore,
		Summary: "The first line of a review comment is `Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n>` or the Carried-over form, for this PR.",
		Anchor:  "`Review: <CLEAR|FINDINGS> head=<SHA> patch-id=<hex> pr=<n>`"},
	{ID: RuleReviewLength, Severity: SeverityError, Source: SourceCore, Limit: 15, Hard: 15,
		Summary: "A review comment has at most 15 lines.",
		Anchor:  "Review-Kommentar (max 15 Zeilen)"},
}

// ByID returns the rule with the given ID; it panics for an unknown ID (a programming error).
func ByID(id string) Rule {
	for _, r := range Rules {
		if r.ID == id {
			return r
		}
	}
	panic("lintpr: unknown rule " + id)
}
