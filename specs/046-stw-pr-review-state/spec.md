# Specification: review state, owner queue and stack view of open pull requests

**Created**: 2026-11-01 | **Status**: proposed (spec-light: this page, [contracts/review.md](contracts/review.md), [plan.md](plan.md) and
[tasks.md](tasks.md) are the whole design). Issue #348. It amends [Spec 036](../036-stw-pulls/spec.md) (`stw pulls`),
[Spec 037](../037-stw-status-overview/spec.md) (`stw status`) and the allowlist of [Spec 022](../022-bounded-go-gh/spec.md) FR-005.

**Why a new spec and not an amendment of 037.** The change adds a new data source (pull request comments), a trust model, a comment
grammar, three views and new allowlisted read shapes. Spec 036 owns the list and Spec 037 the status screen; both only receive pointers
and the additive output. A number of its own keeps the rules in one place for the later `stw ready-check` (#343).

Agents review pull requests and post the verdict as a comment. The owner needs to see, without opening each pull request, which pull
requests are CLEAR but still Draft, which have no final review comment, which have findings, what can be merged now, and how stacked
pull requests depend on each other. `stw` reads and shows; it never writes, labels, marks Ready or merges.

## Trust model

Every comment, including the agents' review comments, is posted by the GitHub account of the owner. A CLEAR is therefore a **claim**,
not proof, and `stw` never says "independent", "verified" or "approved". It shows `review: clear` with the basis `comment` (human text:
"per comment") and checks only what can be checked from GitHub data:

1. the head named in the comment equals the current head SHA of the pull request (otherwise `clear-stale`);
2. the comment was created at or after the committer date of the head commit (a comment that names a SHA before that commit existed
   cannot be about it and is ignored);
3. a `Carried-over:` line binds an old head to a new one (`from` to `to`) and the chain must end at the current head;
4. the `patch-id` of a carry-over equals the `patch-id` of the CLEAR it carries, and the carry-over was posted after that CLEAR;
5. `pr=` equals the number of the pull request that carries the comment.

Comment edits are visible (`edited`) but not refused. Nothing else is inferred: the author of a comment is not interpreted, and a text
that is not strictly parseable is never guessed (`unparsed`).

## Comment grammar (strict)

The **first line** of a comment body (up to the first `\n`; one trailing `\r` is ignored; no leading space) is one of:

```
Review: <CLEAR|FINDINGS> head=<40-hex> patch-id=<hex> pr=<n>
Carried-over: from=<40-hex> to=<40-hex> patch-id=<hex> pr=<n>
```

Single spaces, lower-case hex, `patch-id` is 40 or 64 hex digits (git patch-id), `n` a positive integer without leading zeros. Anything
else is not strict. The format is fixed by `wstein/agent-rules`; a change there is an amendment of this spec. A not strict first line that
contains one of the words `verdict`, `urteil`, `clear`, `findings`, `carried-over` or `carried over` (case-insensitive, whole word), or
starts with `review` followed by `:` , is **unparsed** (old forms such as `Verdict: CLEAR`, `VERDICT: CLEAR for head <sha>`,
`Urteil: CLEAR`). A strict line whose `pr=` differs from the pull request is unparsed too. Other comments are ignored.

## Review state (exactly one per pull request whose comments were read)

Let H be the current head, T the committer date of H. Comments are ordered by `created_at`, then id.

| State | Meaning |
| --- | --- |
| `none` | no strict verdict applies to H, no CLEAR for an older head exists and no unparsed comment is newer than the latest strict comment |
| `findings` | the decisive comment for H is `Review: FINDINGS` |
| `clear` | the decisive comment for H is `Review: CLEAR` |
| `clear-carried` | the decisive comment for H is a valid `Carried-over` chain from a CLEAR |
| `clear-stale` | no decisive comment for H, but an older head X has `Review: CLEAR` as its latest `Review` verdict (carried to a head other than H or not carried) |
| `unparsed` | an unparsed comment is newer than the decisive comment (or than the latest strict one when there is none); it is not read, so the state is unknown |

The **decisive comment** for H is the latest (by `created_at`, then id) of: a `Review:` line with `head=H` created at or after T, and a
valid carry-over with `to=H` created at or after T. A carry-over `from=F to=H` is valid when a chain exists from a `Review: CLEAR head=F`
that was the latest `Review` for F up to the carry-over, with equal `patch-id`, created before the carry-over, and each link is created
at or after the one before it (at most 8 links; a repeated head ends the chain). A carry-over of `FINDINGS` is not valid. A decisive comment is never overridden by an
older unparsed comment. A strict comment ignored by rule 2 or 4 does not count: it can leave the state `none`.

Evidence per state: the verdict, the head named in the decisive comment (for a carry-over the head of the CLEAR it carries), the
`patch-id`, the time and `https` URL of the decisive comment, `carried`, `edited` (its `updated_at` is after `created_at`) and
`truncated` (a long thread is read as its first page and its last five pages of 50 comments, so a comment in the middle can be missed).

## Hints (derived, a pull request may have several)

| Hint | Rule |
| --- | --- |
| `clear-not-ready` | state is `clear` or `clear-carried` and the pull request is a Draft |
| `no-final-review` | comments were read and the state is `none`, `clear-stale` or `unparsed` (no strict comment for H) |
| `base-not-default` | the base branch is not the default branch of the repository |
| `parent-merged-retarget` | `base-not-default`, and a merged pull request of the same repository had that branch as its head; the child needs a retarget or restack |
| `ready-mergeable` | not a Draft, state `clear` or `clear-carried`, verdict group `ready` (Spec 036: head checks green, `mergeable` true, state `clean`, rebaseable), base is the default branch |

`parent-merged-retarget` looks at the 100 most recently updated closed pull requests of the repository (one read). Evidence outside that
window is not seen, so the hint can be absent for an old parent (a documented limit, never a guess). `ready-mergeable` says: per
comment CLEAR, and GitHub reports the head mergeable and green. It does not say "the required checks are green" separately: GitHub's
`clean` state already includes them, as in Spec 036.

## Views

1. **List and status (this wave, PR 1).** `stw pulls` shows `review` and `hints` per row (human: a `REVIEW` column and `{hint, ...}`;
   JSON: see the contract) and the counts in `summary`. `--no-review` skips the comment reads. `--review STATE` (repeatable) and
   `--hint HINT` (repeatable) keep the matching rows (OR within a flag, AND across both); they are applied after all reads, before the
   summary, and a row whose comments were not read matches neither flag. Both flags with `--no-review` are a usage error (exit 2).
   `stw status` reads reviews with the same cycle and shows them in the `pulls` source data (counts per state and per hint) and in the
   `remote:` line; it adds no action category.
2. **Owner list (PR 2).** `stw pulls --mergeable` ("what can I merge now"): only rows with `ready-mergeable`, per repository in
   merge order (a stack's lower pull request first; otherwise oldest `created_at`), each with a reason line built from facts ("CLEAR
   for head 3f2a9c1 per comment 2026-11-01T09:00Z, checks green, mergeable clean, base main") and the open descendants that follow
   after it (`then: #12, #13`). Pull requests whose parent is not merged yet are not listed; the footer counts them. `--mergeable` is
   a view over the same reads and cannot be combined with `--stack`, `--review` or `--hint`. (`--owner` already selects another
   GitHub owner for discovery in `stw pulls`, so the owner list has its own flag.)
3. **Stack view (PR 3).** `stw pulls --stack`: forest of open pull requests of one repository, a node `B` is the child of `A` when
   `B.base == A.head_ref`, both are non-fork pull requests of the repository. Roots are pull requests whose base has no open pull
   request. Each node shows number, group, checks, review state and hints. Only trees with a child and roots with `base-not-default`
   are printed; the footer counts the single pull requests left out. A cycle (not possible on GitHub) is cut at the repeated node.

## Reads, cost and the allowlist amendment

Added GET shapes, each produced by one generator and matched exactly (Spec 022 FR-005, admitted in the guard modes `pulls` and
`status` only; `watch`, `issues` and the one-shot modes refuse them):

- `repos/OWNER/NAME/issues/N/comments?per_page=50&page=P`, N a positive integer, P 1 to 999;
- `repos/OWNER/NAME/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=1`.

Existing shapes are reused: `repos/OWNER/NAME` (default branch) and `commits/SHA` (committer date of H). Cost per run: for each row
within `--limit` (drafts included; newest first, the budget of Spec 036 counted separately for reviews) one comment page (more only when
GitHub says there are more, at most 6 reads), one `commits/SHA` read only when a strict comment names H, one `repos/OWNER/NAME` read per
repository with an open pull request, one closed-list read per repository with a pull request whose base is not the default branch.
A failed read leaves the row's `review` null and is reported like any read failure (`repos[].error`, exit 1); a rate limit stops
further reads. No search, no GraphQL, no write.

## JSON and compatibility

Additive, in place: the `stw pulls` schema 1 and the `stw status` schema 2 are unpublished (Spec 036, Spec 037) and byte-frozen by
digest; both gain optional members and their digests are updated in the same commit, as the freeze test requires. `schema_version`
stays 1 and 2: consumers must ignore unknown members ([JSON contracts](../../docs/json-contracts.md#compatibility)). A later
publication freezes the result. The owner list and the stack view add document members in their own pull requests.

## Non-goals

No write of any kind, no verification of who wrote a comment, no interpretation of free-form comments, no PR review API
(`pulls/N/reviews`), no required-check list, no Ready or merge advice beyond `ready-mergeable`, no new status action category,
`stw ready-check` (#343) and `stw review carry-over` (#345) are separate.

## Verification

Fake forge and transport fixtures only. Every state, every hint, every rule of the trust model and the filters have a test and a
mutation that makes it fail (plan). A drift test keeps the status copy of the state and hint names equal to this package. Acceptance
against a real repository is separate and not part of the first pull request.
