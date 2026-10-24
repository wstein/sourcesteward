# Research: fast-forward landing of a pull request

**Measured**: 2026-10-24, on a private throwaway repository on github.com, by the owner's session. Rebase-merge was **not** used. The raw
result file is `ff-experiment-results.md` in the session scratchpad; this document copies it faithfully.

## Setup

`main` with one commit; stacked pull requests A (base `main`) and B (base `topic-a`); a direct push of the reviewed PR head SHA to `main`
(no branch protection).

## Results

- **R1** Pushing A's head SHA to `main` as a fast-forward: GitHub marked pull request A MERGED automatically; `mergeCommit` equals the PR
  head SHA (no new SHA).
- **R2** Pull request B stayed OPEN, head unchanged, base still `topic-a`, mergeable CLEAN.
- **R3** Deleting `topic-a` (A's branch) **before** retargeting B: B was CLOSED automatically (base gone, CONFLICTING/DIRTY). It cannot be
  reopened with the old base.
- **R4** Correct order: after A landed, `gh pr edit B --base main` (state OPEN, MERGEABLE CLEAN, head unchanged), **then** delete A's
  branch: B stays OPEN. Then pushing B's head SHA to `main` as a fast-forward: B MERGED, `mergeCommit` equals B's head SHA. `main` shows
  the commits with the original SHAs; the history is linear.

## Caveats

- **C1** Direct push to `main` was allowed because the throwaway repository has no branch protection or ruleset. Real repositories may
  refuse. Not measured on `wstein/anchorage-workspace`, `wstein/sourcesteward` or any other real repository.
- **C2** This is an owner-only operation; agents never push `main` (AGENTS.md).
- **C3** One private repository, one run. Not measured: `delete_branch_on_merge` on, required status checks, required reviews, signed
  commits, merge queue, forks, several children of one bottom, Draft pull requests, `mergeable` computation delay after the push.
- **C4** The time between the push and GitHub's MERGED mark was not recorded; the bounded wait in FR-022 is a design choice, not a
  measurement.

## What the spec takes from this

R1, R2: the landed SHA equals the reviewed SHA, so exact-head review stays valid. R3, R4: the order of FR-030 to FR-032. C1, C3: OQ2,
OQ4 and the first dogfood step.
