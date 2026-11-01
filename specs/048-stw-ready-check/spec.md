# Specification: `stw ready-check`, the read-only preconditions for marking a pull request Ready

**Created**: 2026-11-01 | **Status**: proposed (spec-light: this page, [contracts/ready-check.md](contracts/ready-check.md) and
[plan.md](plan.md) are the whole design). Issue #343. It uses the review state of Spec 046 (pull request #349, not merged yet, so
not linked) and the guarded reader of [Spec 036](../036-stw-pulls/spec.md). It is the cheap, local half of the
read-only Ready inspection of issue #64; the trusted `review-evidence` check of that issue stays separate.

## Why

A pull request was set Ready with an unchanged body that still named a blocker. Helpers assemble the Ready preconditions from several
`gh` calls. `stw ready-check NUMBER` lists them in one place and exits non-zero when one fails. **Ready means the owner can merge**
(workspace rule), so the check is the last look before an agent flips the Draft.

## Behavior

`stw ready-check NUMBER [--repo OWNER/NAME] [--host HOST] [--human | --json]` reads one pull request and prints seven conditions in a
fixed order. Each is `pass`, `fail` or `unknown` and carries its evidence. `ready` is true only when all seven pass.

| Condition | Passes when | Unknown when |
| --- | --- | --- |
| `open` | the pull request is open (not closed, not merged) | `pulls/N` could not be read |
| `review` | the Spec 046 state is `clear` and names the current head, or `clear-carried` through a valid carry-over chain | the review state could not be read, or is `unparsed` |
| `checks` | the check class of all check runs and commit statuses at the exact head is `green` | the checks could not be read |
| `base` | the base is the default branch, or the head branch of another open pull request of the repository (a stack child; the parent is named in the evidence) | the default branch or, for another base, the open pull requests could not be read |
| `blocker` | no Blocker line or Blocker section of the text names a blocker (see below); the none spellings are `none`, `keiner`, `keine`, `n/a`, `no`, `-` | the text could not be read |
| `conflict` | GitHub reports `mergeable` true and a state other than `dirty` | GitHub has not computed `mergeable` yet |
| `rebaseable` | GitHub reports `rebaseable` true | GitHub has not computed `rebaseable` yet |

A failed read or a value GitHub has not computed makes its conditions `unknown`; `unknown` never passes. Without a valid head neither
the checks nor the review are read. Being a Draft is reported (`draft`) and is not a condition: the check runs before the flip.

Decisions and limits:

- `checks` looks at every check run and commit status of the head, not at the list of required checks (not read; Spec 046 lists it as a
  non-goal). It is at least as strict as "required checks green". The check class is the one of `stw pulls` (Spec 036).
- `review` is a claim of the comment, not proof (Spec 046, Trust model); the output says "per comment" and never "independent".
- `base` is derived from GitHub data, not from the text (owner decision 2026-11-01, review of PR 359): a base other than the default
  branch passes only when another open pull request of the same repository has that branch as head branch (the lowest number is named,
  and "merge #N first" is stated as evidence). The list of open pull requests does not give the head repository, so a fork branch with the
  same name is not told apart. A text line "Merge #N first" is no evidence.
- A Blocker line is any line in which the word "blocker" or "blockers" is followed, after an optional parenthesis, by a colon (ASCII or
  fullwidth), a pipe or a dash (any Unicode dash, the minus sign, two hyphens, or a hyphen followed by a space; Unicode spaces count as spaces),
  wherever it stands in the line. The value is the text after it; quote,
  list, number and heading marks and emphasis are ignored. Matching forms: `Blocker: x`, `**Blocker (Draft):** x`, `1. Blocker: x`,
  `> Blocker: x`, `## Blocker: x`, `Status: Draft. Blocker: x`, `Blocker - x`, `Blocker — x`, `| Blocker | x |`. A line with several
  labels is checked per label. A heading with the word, a bare `Blocker` word, or a label without a value takes the lines below as value:
  below a heading all lines up to the next heading, below any other line up to the first blank line after a content line. Every value
  must be a none spelling (list mark, emphasis, table pipes and a trailing period or comma ignored), else the condition fails; no
  value fails. Prose that only uses the word (`a blocker for CI was removed`, `Blocker-free`) is not a Blocker line. The whole body is
  scanned. A Blocker line in a code block or a quote is read like any other (fail-closed).
- Fail-open limits of the label rule: free text between the word and the separator hides the label (`Blocker is: x`, `Blocker (a)(b): x`,
  `The blocker list: x` pass). Widening the rule would flag prose, so the shared Blocker parser of the PR-text lint takes over; this check
  does not widen it further.
- Evidence is cut to 400 bytes (the schema allows 400 characters); control characters are escaped first.
- The command reads the head once. A push between the read and the flip is not detected; `--expect-head`-style resampling is the
  trusted check of issue #64.
- Until the Spec 046 parser is merged, the review source of the command is a placeholder that fails, so `review` is `unknown` and the
  command never reports ready. The seam is one function type (`ReviewFunc`); wiring the parser changes one line of the command.

## Reads and effects

GET only, through the guarded `stw pulls` reader: `repos/OWNER/NAME/pulls/N`, `repos/OWNER/NAME`, and the check runs and statuses of
the head (`commits/SHA/check-runs`, `commits/SHA/statuses`) and, for a base other than the default branch, the list of open pull requests
(`pulls?state=open`, Spec 036), all existing allowlist shapes (Spec 022 FR-005); no new shape. A default-branch base needs four requests.
The open list is paginated (25 per page, 12 after a size halving, at most 80 pages for 2000 open pull requests, the bound of the watch
reader); a larger repository gives `base` unknown. Comment reads of Spec 046 come on top once the parser is wired. The command writes
nothing to GitHub, to disk or to git, has no `apply` mode and never marks, labels or merges.

## Exit statuses

0 all seven conditions pass, 1 a condition failed or is unknown or a read failed (the document is still printed), 2 usage error.

## Non-goals

No Ready transition, no `ready apply`, no label, no merge advice, no list of required checks, no check of session links or of the
commit identity (a different check, local to the checkout), no stack verification beyond the text line.

## Verification

Fake forge and in-process HTTP fixtures only. Every condition has a pass test and a fail test, every unknown path a test, and each has a
mutation that makes it fail (PR text). Acceptance against a real pull request is separate and not part of the first pull request.
