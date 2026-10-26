# Research: fast-forward landing of a pull request

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Human-only landing and measured-shape/retarget gates remain; trusted workspace policy is authority.
> This is a documentation target, not implementation or native acceptance.

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

## Stack landing: spike observations and what is still to be measured (2026-10-26)

Source: the notes of the `wstein/stw-release-spike` author (the first stack, a native `gh stack` stack of three pull requests, and the
ruleset settings of that repository; session scratchpad `spike-release-notes.md` sections D, E and "Stacking", and `spike-settings.md`).
These are observations of the spike repository, not of `wstein/sourcesteward` or `wstein/anchorage-workspace`.

**Observed (the facts FR-060 to FR-079 rely on)**

- **S-A** A pull request with `base=main` whose branch contains its parent's commits shows parent plus child commits (4 commits for the
  second pull request of a two-pull-request stack); with `base=<parent branch>` only the child's own commits. Two pull requests on the same
  head SHA doubled the `check` runs of that SHA. (Edge case in spec.md: both base shapes accepted.)
- **S-B** After the owner rebase-merged the bottom (#5), GitHub retargeted the next pull request (#6) to `main` and force-pushed new heads
  for **every** remaining pull request of the stack (events `automatic_base_change_succeeded`, `head_ref_force_pushed`); with
  `delete_branch_on_merge` on, the bottom branch was deleted. Rebase-merge gave every commit a new SHA (restack needed for a non-native
  child, D).
- **S-C** #6 had **no** `pull_request` check run on its new head (empty `statusCheckRollup`, merge state BLOCKED by the required check
  `check`); #7 got a run about 3.5 minutes late. So "automatic restack never triggers CI" is confirmed for #6 and refuted for #7: not
  conclusive. `gh pr close 6 && gh pr reopen 6` started the run at once with the head unchanged. This is the distinct state of FR-064.
- **S-D** Merging the top pull request of a native stack (base = the parent branch) is allowed by GitHub and merges into the parent's branch,
  not into `main`; nothing reaches `main`. A stack lands bottom-up only, one merge per pull request, each needing its CI again.
- **S-E** The spike ruleset `main` has `non_fast_forward`, `required_linear_history`, `pull_request` (rebase only), required check `check`, bypass
  actor RepositoryRole 5 with `bypass_mode: pull_request`. Bypass in that mode applies to pull requests only, so a direct push is **expected
  to be refused** (not measured). A direct push needs `bypass_mode: always`, which also lets any token of the same account push (so
  "agents cannot" would hold by policy only, no GitHub separation).
- **S-F** CI start delays of three minutes and more are common, so a plan can see `checks-pending` for a long time; the apply refuses it.

**Not measured; to be measured in the spike with a second stack (the spike author prepares it; the owner is at the terminal)**

Each test names the exact observation. The spike repository is configured as in S-E; for E1 to E3 the ruleset bypass mode is set to `always`
by the owner (owner-only settings) and the result recorded; with `pull_request` the first observation is the verbatim refusal (E0).

| ID | Test | Observation to record | Lifts |
| --- | --- | --- | --- |
| E0 | With the bypass mode as it is now (`pull_request`), the owner pushes the tip of the second stack: `git push origin <tip>:refs/heads/main` | the verbatim remote refusal, exact text | FR-040 text, FR-067 `ruleset-forbids-push` |
| E1 | Second stack of three pull requests (A base main, B base A's branch, C base B's branch; variant: all three base main with a linear chain of heads), all CLEAR, checks green on every head. Bypass `always`. Push the tip SHA to `main` once | after 2 minutes, then 10 minutes: state, `mergedAt`, `merge_commit_sha` of A, B and C; whether all are `MERGED` and each `merge_commit_sha` equals its own head (R1 measured this for one pull request only); the events of each pull request; the order and delay of the marking | FR-073, `land-unmeasured` for chains |
| E2 | Same run, `delete_branch_on_merge` on (the spike setting) | which of the three head branches still exist after the marking; whether GitHub deletes the heads of pull requests marked by a push | FR-075, FR-031 interplay |
| E3 | Same run | whether any pull request stays OPEN or is CLOSED (for example B or C with a base that vanished), its base and `mergeStateStatus` | FR-073 `landed-not-marked`, FR-066 |
| E4 | A chain whose bottom is not a fast-forward (main moved by an unrelated commit before the push) | the remote refusal text of the push (`fetch first`/`non-fast-forward`) with bypass `always` | FR-071, FR-042 |

Until E1 to E3 are recorded for a repository shape, `stw land --stack` stays `land-unmeasured` for it (decision 9), and the spike repository
is the first shape to be added with `stack: true` to `internal/land/shapes.json`. The real repositories follow only after the owner has
chosen the ruleset bypass question (spec.md Q2).
