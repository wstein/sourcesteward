# Contract: review members of `stw pulls --json` and `stw status --json` (Spec 046)

Additive members of the unpublished `schemas/v1/pulls.schema.json` and `schemas/v2/status.schema.json`; no key is removed or retyped,
`schema_version` stays 1 and 2, both digests in `internal/contracts/schema_freeze_test.go` change in the commit that edits the schema.
Keys are never omitted; absent values are `null`.

## `stw pulls` (schema 1)

- `options.review` (bool, false with `--no-review`), `options.review_states` and `options.hints` (arrays of the filter values).
- Row member `review`: `null` when the comments were not read (`--no-review`, past `--limit`, a failed read, a reader without
  comment support), else `{basis, state, verdict, head, patch_id, at, url, association, ignored, carried, edited, truncated}`:
  `basis` const `comment` (the claim is the comment's, Trust model); `state` enum `none|findings|clear|clear-carried|clear-stale|unparsed`;
  `verdict` `CLEAR`, `FINDINGS` or null (null for `none` and `unparsed`); `head` (sha of the decisive comment, for a carry-over the CLEAR it
  carries) and `patch_id` or null; `association` the `author_association` of the decisive comment or null (`OWNER`, `MEMBER` or `COLLABORATOR`); `ignored` the number of ignored review-like comments of other authors (integer, 0 or more); `at` RFC 3339 and `url` (`https`) of the decisive comment or null; `carried`, `edited`, `truncated` bool.
- Row member `hints`: array of `clear-not-ready`, `no-final-review`, `base-not-default`, `parent-merged-retarget`, `truncated-review-unknown`, `foreign-review-ignored`, `ready-mergeable`, in
  that order, without duplicates.
- `summary.review`: `{none, findings, clear, clear_carried, clear_stale, unparsed, not_read}` (not_read counts rows with `review` null)
  and `summary.hints`: `{clear_not_ready, no_final_review, base_not_default, parent_merged_retarget, truncated_review_unknown, foreign_review_ignored, ready_mergeable}`.

## Owner list (`--mergeable`, PR 2)

`options.mergeable` (bool) and the top-level member `owner_queue`: `null` without `--mergeable`, else an array of entries
`{position, repo, number, title, url, head, review, reason, then}` in merge order: `position` from 1, `head` the sha, `review` `clear` or
`clear-carried`, `reason` the fact text of the spec, `then` the numbers of the open descendants. `summary.waiting_for_parent` counts the
non-draft rows with `base-not-default` (always present, 0 without the view).

## `stw status` (schema 2)

`pullsData` gains the optional member `review` with the same two count objects: `{states: {...}, hints: {...}}`, present exactly when the
pulls source was read with reviews. Counts equal those of `stw pulls` for the same repository.

## Human output

`stw pulls` adds a `REVIEW` word after the mergeable state (`clear`, `clear(carried)`, `findings`, `clear-stale`, `unparsed`, `none`, `-`
when not read) and `{hint, hint}` at the end of the line when hints exist. Untrusted text is escaped as before. `stw status` adds one line
`reviews: N clear, M findings, ...` to the `remote:` block of a repository with open pull requests.
