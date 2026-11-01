# Specification: `stw lint pr`, the working rules checked on a pull request

**Created**: 2026-11-01 | **Status**: implemented in source (spec-light: this page and [tasks.md](tasks.md) are the whole design).
Issue #344. It adds one fixed read shape to the allowlist of [Spec 022](../022-bounded-go-gh/spec.md) FR-005.

## Purpose

A pull request contained session links in its text, in its comments and in 7 commits, and review comments of 58 to 98 lines. No tool
detected this. `stw lint pr PR` reads one pull request and reports every violation of the rules of the agent-rules core that a machine
can check. It is read-only: `GET` only, nothing is written to the forge or to disk.

## Rules (the table is data)

The rule table is `Rules` in `internal/lintpr/rules.go`: ID, severity, summary, source file and an **anchor** phrase, and the numeric
limits. The engine only interprets the table. Rule IDs are part of the JSON contract.

| Rule ID | Severity | Checks |
| --- | --- | --- |
| `session-link` | error | no Claude session trailer and no code-session URL of the Claude web host in a commit message, the PR title, the PR text or a comment |
| `author-identity`, `committer-identity` | error | author and committer of every commit are an owner identity (name exact, email case-insensitive) |
| `co-authored-by` | error | every commit message has a `Co-Authored-By: Name <email>` trailer line |
| `conventional-commit` | error | the subject is `type(scope)!: description` with a known type |
| `pr-body-length` | warning, error | the PR text has at most about 25 lines: a warning above 25, an error above 30 |
| `pr-blocker-line` | error | a line starting with `Blocker` |
| `pr-author-agent-line` | error | a line starting with `Author agent` (repository convention; the core has no text for it) |
| `review-line-format` | error | the first line of a review comment is `Review: <CLEAR\|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n>` or the `Carried-over` form, with `pr` equal to this pull request |
| `review-comment-length` | error | a review comment has at most 15 lines |

A comment is a **review comment** when its first non-blank line starts with `Review` or `Carried-over` (case-insensitive, blanks and markdown marks ignored) followed by a colon, the end of the line or a verdict word (`**Review**: CLEAR`, `Review CLEAR`, `## Review`); prose such as "Review of the approach" is not one. A comment that does not claim to be a review is not checked beyond the session-link rule. The
identity rules run only with at least one owner identity; the command refuses to run without one.

Not checked (no machine-checkable form): the list of at most 6 changes, "Tests with numbers", the "Not verified" line, atomicity of
commits, the language of the text, the line length of the rule files.

## Keeping the table in step with agent-rules

Every row except `pr-author-agent-line` cites its file (`claude/CLAUDE.md` or `claude/skills/agent-workflow/SKILL.md`) and a phrase of
it. `TestRulesTableAnchorsInAgentRules` reads the files from a checkout of agent-rules (`AGENT_RULES_DIR=<checkout>`) and fails when an
anchor is gone; without the variable it is skipped. A change of a limit or of the grammar in agent-rules therefore needs a matching
change of the table in the same step: run the test after every change of the core. A CI job with a checkout of agent-rules is a follow-up.

## Command

`stw lint pr PR [PATH] [--repo OWNER/NAME] [--host HOST] [--owner-identity "Name <email>"]... [--human | --json]`. The repository is
`--repo` or the single GitHub repository of the workspace of PATH (the current directory). Owner identities are `--owner-identity`
(repeatable) or `user.name` and `user.email` read from the Git configuration of that directory (two `git config --get` calls, never a write).
Reads: `pulls/N`, `pulls/N/commits` (pages 1 to 3, 100 per page; the forge lists at most 250 commits), `issues/N/comments` (pages 1 to 10, 30 per page).
**An incomplete read fails closed:** the commits are `truncated` when the last page still has a next page or when fewer commits were read than the `commits`
field of `pulls/N` reports; the comments when page 10 still has a next page. Then `summary.passed` is false and the exit status is 3 even without a finding,
because unread text is not a pass. Exit statuses: 0 no error finding and a complete read (warnings do not fail), 3 at least one error finding or an incomplete read (the output is printed), 1 a failed read
(no output), 2 usage error. JSON: [lint-pr.schema.json](../../schemas/v1/lint-pr.schema.json), a finding has `rule`, `severity`,
`location` (`kind` `pr-title`, `pr-body`, `comment` or `commit`; `id` the comment ID or commit SHA; `line`) and `message`.

## Reuse and duplication

The review line grammar is also parsed by `internal/reviewstate` (Spec 046, PR 349). That package is not on main, so `lintpr` carries its own
small parser with tests for the same grammar; the only difference is the `patch-id`, which the core text fixes at 40 hex digits and
`reviewstate` also admits as 64. Follow-up: when Spec 046 has landed, `ParseReviewLine` calls it.

## Hosting guard

A sixth guard mode, `lint`, admits the one-shot set, `pulls/N/commits?per_page=100&page=1..3` and exactly
`issues/N/comments?per_page=30&page=1..10`, produced by one generator and matched exactly. The shape differs on purpose from the comment
shape of Spec 046 (50 per page).
