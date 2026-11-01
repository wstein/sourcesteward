# Checking a pull request with `stw lint pr`

`stw lint pr` ([Spec 050](../specs/050-stw-lint-pr/spec.md)) reads one pull request and reports every violation of the machine-checkable
working rules in its commits, its text and its comments. It is read-only (`GET` only) and keeps no state. The JSON document is described
by [`lint-pr.schema.json`](../schemas/v1/lint-pr.schema.json) (unpublished, byte-frozen, see
[JSON contracts](json-contracts.md#lint-pr-schema-v1-unpublished)).

## Use

```sh
stw lint pr 344                                     # repository of the workspace, owner from the Git configuration
stw lint pr 344 --repo wstein/sourcesteward --owner-identity "Werner Stein <werner.stein@gmail.com>"
stw lint pr 344 --json | jq '.findings[] | .rule + " " + .location.kind'
```

The repository is `--repo` or the single GitHub repository of the workspace of `PATH` (default: the current directory); several
repositories are a usage error. The owner identities for the author and committer rules are `--owner-identity` (repeatable) or `user.name`
and `user.email` read from the Git configuration of that directory; without any the command refuses to run. Output is human on a
terminal and one JSON document otherwise (`--human`, `--json`).

## Rules

| Rule ID | Severity | Location | Checks |
| --- | --- | --- | --- |
| `session-link` | error | PR title, PR text, comment, commit | a Claude session trailer or a code-session URL of the Claude web host |
| `author-identity`, `committer-identity` | error | commit | author and committer are an owner identity |
| `co-authored-by` | error | commit | a `Co-Authored-By: Name <email>` trailer line |
| `conventional-commit` | error | commit | subject `type(scope)!: description` |
| `pr-body-length` | warning above 25 lines, error above 30 | PR text | about 25 lines |
| `pr-blocker-line` | error | PR text | a line starting with `Blocker` |
| `pr-author-agent-line` | error | PR text | a line starting with `Author agent` (repository convention) |
| `review-line-format` | error | comment, line 1 | `Review: <CLEAR\|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n>` or the `Carried-over` form, for this pull request |
| `review-comment-length` | error | comment | at most 15 lines |

A finding has `rule`, `severity`, `location` (`kind`, `id` = comment ID or commit SHA, `line`) and `message`. The rules are a table in
`internal/lintpr/rules.go` that cites the agent-rules core; see the spec for how it is kept in step.

## Limits and exit statuses

Up to 300 commits (the forge lists at most 250; the commit count of the pull request is compared) and 300 comments are read. A list that was not
read in full is reported as `truncated`, `summary.passed` is false and the exit status is 3: unread text is not a pass. Exit statuses: 0 no error
finding and a complete read (warnings do not fail), 3 at least one error finding or an incomplete read (the output is printed), 1 a failed read or missing authentication (no output), 2 usage error. A
comment that does not claim to be a review is checked for session links only.
