# Research: fast-forward landing of a pull request

**Measured**: 2026-10-24, on a private throwaway repository on github.com, by the owner's session. Rebase-merge was **not** used. The raw
result file is `ff-experiment-results.md` in the session scratchpad. This document has three labelled parts: the copy of that file
(Setup, Results, Caveats from the file), the author's notes (not measurements), and the read-only observations of the real repositories
(read configuration, not measurements of any push).

## Setup

`main` with one commit; stacked pull requests A (base `main`) and B (base `topic-a`); a direct push of the reviewed PR head SHA to `main`
(no branch protection).

## Results (copy of the measurement)

- **R1** Pushing A's head SHA to `main` as a fast-forward: GitHub marked pull request A MERGED automatically; `mergeCommit` equals the PR
  head SHA (no new SHA).
- **R2** Pull request B stayed OPEN, head unchanged, base still `topic-a`, mergeable CLEAN.
- **R3** Deleting `topic-a` (A's branch) **before** retargeting B: B was CLOSED automatically (base gone, CONFLICTING/DIRTY). It cannot be
  reopened with the old base.
- **R4** Correct order: after A landed, `gh pr edit B --base main` (state OPEN, MERGEABLE CLEAN, head unchanged), **then** delete A's
  branch: B stays OPEN. Then pushing B's head SHA to `main` as a fast-forward: B MERGED, `mergeCommit` equals B's head SHA. `main` shows
  the commits with the original SHAs; the history is linear.

## Caveats of the measurement file (copy: C1, C2)

- **C1** Direct push to `main` was allowed because the throwaway repository has no branch protection or ruleset. Real repositories may
  refuse. Not measured on `wstein/anchorage-workspace`, `wstein/sourcesteward` or any other real repository.
- **C2** This is an owner-only operation; agents never push `main` (AGENTS.md).
## Author's notes (not measurements)

- **N1** One private repository, one run. Not measured: `delete_branch_on_merge` on, required status checks, required reviews, signed
  commits, merge queue, forks, several children of one bottom, Draft pull requests, `mergeable` computation delay after the push.
- **N2** The time between the push and GitHub's MERGED mark was not recorded; the bounded wait in FR-022 is a design choice.
- **N4** GitHub documents that when a head branch is deleted after a merge, dependent pull requests may be retargeted automatically. The
  measured run (R3) closed the child when the branch was deleted by hand before any merge-time retarget; whether `delete_branch_on_merge`
  auto-retargets a child of the landed branch is not measured. Both outcomes are possible and T002 must record whichever occurs.
- **N3** The results file does not say whether R3 and R4 were separate runs. B was closed in R3 and cannot be reopened with the old base,
  so R4 necessarily used a second stack or a recreated B; how it was set up is not recorded.

## Real repositories, read-only observations (2026-10-24; configuration read, NOT a push, NOT measured)

Calls (all read-only `gh api`, no write):

```text
gh api repos/wstein/sourcesteward --jq '{delete_branch_on_merge,default_branch,allow_rebase_merge,allow_squash_merge,allow_merge_commit}'
gh api repos/wstein/sourcesteward/branches/main/protection
gh api repos/wstein/sourcesteward/rulesets
gh api repos/wstein/sourcesteward/rules/branches/main --jq '[.[]|.type]'
gh api repos/wstein/sourcesteward/rulesets/<id> --jq '{bypass_actors,rules:[.rules[]|.type]}'
(the same first three for wstein/anchorage-workspace)
```

| Repository | Observed |
| --- | --- |
| `wstein/sourcesteward` | `delete_branch_on_merge` true; default branch `main`; merge commit and squash disabled, rebase-merge enabled; classic branch protection: HTTP 404 "Branch not protected"; one active ruleset `main` (`~DEFAULT_BRANCH`) with rules `deletion`, `non_fast_forward`, `required_linear_history`, `pull_request` (allowed merge methods: rebase), `required_status_checks` (contexts only in the ruleset); only bypass actor RepositoryRole 5 with `bypass_mode: pull_request` |
| `wstein/anchorage-workspace` | `delete_branch_on_merge` true; default branch `main`; rebase-merge only; branch protection and rulesets endpoints: HTTP 403 "Upgrade to GitHub Pro or make this repository public", so the shape is unreadable |

**Expected, not measured:** a direct push of a reviewed head to `main` on `wstein/sourcesteward` is expected to be refused, even for the
owner: the `pull_request` rule requires a pull request and the only bypass mode is `pull_request`. If so, the owner must change the
ruleset (owner-only) for `stw land` to be usable there. Because `delete_branch_on_merge` is true on both repositories, the measured
order R4 (land, then retarget) would let GitHub delete the bottom branch first and close the child (R3); the pre-retarget order
(Spec 043 FR-031) is the conservative order for real stacks, and it is unmeasured; GitHub may also auto-retarget the child (N4), in which
case it would not be needed. A private repository on the free plan cannot serve as the scratch repository for the ruleset shape (403, as
observed on `wstein/anchorage-workspace`).

## What the spec takes from this

R1, R2: the landed SHA equals the reviewed SHA, so exact-head review stays valid. R3, R4: the order of FR-030 to FR-032. C1, N1 and the
read-only observations: OQ2, OQ4 as hard gates (`land-unmeasured`) and the first dogfood step (a scratch repository with a copy of the real
ruleset).
