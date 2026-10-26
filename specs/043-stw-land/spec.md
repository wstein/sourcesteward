# Feature Specification: `stw land`, the owner's fast-forward landing of a reviewed pull request

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Human-only landing and measured-shape/retarget gates remain; trusted workspace policy is authority.
> This is a documentation target, not implementation or native acceptance.
>
> **Owner-decision amendment — 2026-10-26 (stack landing):** `stw land --stack` lands a whole linear chain of reviewed pull
> requests with **one** fast-forward push of the tip; see [Stack landing](#stack-landing---stack-owner-decision-2026-10-26).
> Dependent parts are by default ONE pull request with a commit series (workspace `AGENTS.md`), which the single-PR landing
> already covers; `--stack` is for the separate stacked PRs that remain. First feature after v0.2.0; first measured in the spike
> repository `wstein/stw-release-spike` (research.md, "Stack landing: to be measured in the spike").

**Branch**: `docs/spec-043-stw-land` | **Created**: 2026-10-24
**Status**: Draft specification; no runtime implementation. Requires an independent review of the exact head before tasks start.
**Input**: Owner decision 2026-10-24: yes to `stw land`, the fast-forward landing of a reviewed PR, as an owner-only command. Measured basis: [research.md](research.md).

## Scope and decisions

`stw land` pushes the exact, already reviewed head commit of one GitHub pull request to the default branch as a **fast-forward** and
lets GitHub mark the pull request merged. No merge commit and no rebase happens, so the landed commits keep their original SHAs and the
history stays linear. It is for the one operation the workspace rules reserve to the owner (the PR merge); it does not replace the
owner's choice of the GitHub merge button.

1. **Owner-only.** The command refuses in agent mode and never runs from an agent session (FR-001 to FR-004). An agent may run only
   `stw land --plan` (read-only), see the rule for `--plan` below.
2. **One pull request per run, or one chain with `--stack`.** Without `--stack` a stack is landed by one run per pull request, bottom
   first, and the run for a child performs the retarget step of the measured order (decision 6). With `--stack` (owner decision
   2026-10-26) one run lands a whole linear chain with a single push of the tip and no retarget at all (FR-060 to FR-079); no other
   run lands two pull requests.
3. **Fresh preconditions.** Every precondition is read again immediately before the effect; nothing from `--plan` is trusted by the apply.
4. **Git decides.** Fast-forwardness is computed with Git over local objects (`merge-base --is-ancestor`); the GitHub API is evidence for
   state, never the proof of an ancestry.
5. **Two network writes only**, each from a closed set: `git push <remote> <sha>:refs/heads/<default>` and the base edit of a child
   (`PATCH /repos/{o}/{r}/pulls/{n}` with `base`, run as `gh pr edit <child> --base <default>`), an explicit extension of the Spec 022
   transport (see Reconciliation). Optional third, only on request: delete of the landed bottom branch (decision 7).
6. **Order for a stack.** With `delete_branch_on_merge` readable and off, the measured order (R3, R4): land the bottom; for every child
   whose base is the landed branch run `gh pr edit CHILD --base DEFAULT`; only then, and only if decision 7 allows, delete the landed
   branch; then land the child. With the setting on or unreadable (both real repositories), FR-031 applies instead: retarget the
   children, re-read (FR-034), land the bottom; this order is unmeasured and gated (decision 9). Deleting the bottom branch before
   retargeting closed the child in the measured run (R3); GitHub documents an automatic retarget in some deletion cases, which is not
   measured here (research.md N4), so the spec never relies on either outcome.
7. **Branch deletion.** Only on request (`--delete-branch`), or never by stw when GitHub's `delete_branch_on_merge` is on (GitHub does it,
   and the retarget step then has to precede the landing push of the bottom, see FR-031). stw never deletes a branch it cannot show was
   pushed for this pull request by its head repository owner (head repository equals the base repository, branch name equals the PR head
   ref), never a branch other than the landed PR's head ref, and never when a different open pull request still uses it as base after the
   retarget step.
8. **Out of scope for v1.** jj repositories (refused, `unsupported-vcs`), non-GitHub forges (refused, `unsupported-forge`), pull requests
   from forks, landing as anything other than a fast-forward (no squash, no rebase-merge, no merge commit), tags, releases, submodule
   gitlinks, auto-restack of a child that is not a fast-forward (Spec 015/016 remain the tools for that), and landing several pull
   requests in one run other than the linear chain of `--stack`.
9. **Hard gate: unmeasured repository shapes fail closed.** Rulesets and branch protection are **not measured**. Read-only
   observations (research.md, "Real repositories, read-only observations") show that `wstein/sourcesteward` has `delete_branch_on_merge`
   true and an active ruleset `main` with `deletion`, `non_fast_forward`, `required_linear_history`, `pull_request` (rebase only) and
   `required_status_checks`, whose only bypass is `bypass_mode: pull_request`; a direct push of the owner is therefore **expected to be
   refused** (expected, not measured). `wstein/anchorage-workspace` has `delete_branch_on_merge` true and its ruleset and protection
   endpoints answer 403 (plan limit), so its shape is unreadable. **Apply fails closed** with the typed refusal `land-unmeasured` (exit
   1) for a repository until research.md records a measured scratch run for that repository's ruleset shape (ruleset rule types and
   bypass modes, `delete_branch_on_merge`, children present or not); `--plan` still works and reports `land-unmeasured` among its
   preconditions. The measured shapes are the **single allowlist** `internal/land/shapes.json` (one data file reviewed in a PR, the only place; every other
   mention refers to it), keyed by the normalised shape, never by repository name. A shape stw cannot read (a 403 on rulesets or
   protection, as on `wstein/anchorage-workspace`) never matches the allowlist and is always `land-unmeasured`. If the ruleset forbids the push, the owner must change the ruleset (owner-only); this
   spec and the command never do, and never suggest a workaround.

## Mode: owner-only

The command reuses the mode detection of Spec 041 (terminal facts per stream, `STW_AGENT`, the agent markers `CLAUDECODE`, `CLAUDE_CODE_*`,
`CODEX_*`, `AGY_*`, `internal/tidywizard/owner`) and the attribution gate of Spec 042 (D5). It is an accident guard, not a security
boundary against a deliberately evasive process of the same user; the repository rules (AGENTS.md: agents never push the default branch
and never merge) are the policy, the guard only catches honest mix-ups.

- **Human mode** (stdin and stdout terminals, `STW_AGENT` unset, no marker): `stw land` may apply.
- **Agent mode**: apply is refused with exit 1 and reason `agent-mode`, before any network read that needs credentials is made. `--plan`
  is allowed in agent mode but never performs a write and is labelled `mode: agent` in its output; it exists so an agent can show the
  owner the exact commands.
- **Typed confirmation, always.** Apply reads from `/dev/tty` (never stdin) the confirmation `land <PR number> <first 8 characters of the
  head SHA>`, for example `land 123 3f9c2a41`. `--yes` is **never** honoured and has no effect; passing it is a usage error (exit 2).
  No controlling terminal: the confirmation fails and nothing is pushed.

## User scenarios and acceptance

### US1 (P1) Show what landing would do

The owner, or an agent preparing the hand-over, wants every effect and the exact commands before anything happens.

1. **Given** a reviewed pull request whose head fast-forwards the default branch, **When** `stw land --pr 123 --head SHA --reviewed SHA
   --plan` runs, **Then** it prints the preconditions with their fresh values, the ordered effects and the exact `git` and `gh` argument
   vectors, performs no write to any branch, worktree, index, config or remote (only the private fetch of FR-002) and exits 0.
2. **Given** any failing precondition, **When** `--plan` runs, **Then** it lists every failing precondition (not only the first), the
   plan is marked `landable: false` and the exit status is 1.
3. **Given** agent mode, **When** `--plan` runs, **Then** the plan is shown with `mode: agent`, and its JSON `apply_allowed` is false (FR-051).

### US2 (P1) Land one reviewed pull request

1. **Given** human mode, a CLEAR review of the exact head (stated with `--reviewed`), green required checks and a fast-forward, **When**
   `stw land --pr 123 --head SHA --reviewed SHA` runs and the typed confirmation is correct, **Then** the preconditions are read again,
   `SHA` is pushed to the default branch as a fast-forward, GitHub reports the pull request
   `MERGED` with merge commit `SHA` within the bounded wait, and the exit status is 0.
2. **Given** the default branch moved after the plan, **When** apply runs, **Then** the fresh read or the push (a compare-and-swap for a fast-forward) detects it,
   nothing changes and the exit status is 1 (`base-moved`).
3. **Given** the pull request head is not `--head`, **When** apply runs, **Then** it refuses (`head-changed`) and pushes nothing.

### US3 (P1) Land a stack, bottom first

1. **Given** PR A (base main), child PR B (base `topic-a`) and `delete_branch_on_merge` off or readable-off, **When** the owner lands A,
   **Then** the push comes first, then the run retargets B to main (`gh pr edit B --base main`), and with `--delete-branch` deletes
   `topic-a` only afterwards; B stays open with its head unchanged. With `delete_branch_on_merge` on or unknown the order is the one of
   FR-031 (retarget, re-read, push), and only for a repository shape recorded as measured (decision 9).
2. **Given** A is landed and B still targets `topic-a`, **When** `stw land --pr B` runs, **Then** it sees that the stack's bottom is
   landed, performs the retarget as its first effect if `topic-a` still exists, re-reads every precondition (FR-034), and then lands B.
3. **Given** `topic-a` was deleted while B still targeted it, **When** B is looked up, **Then** B is closed (measured R3); stw reports
   `child-closed` and does not try to reopen or recreate anything (Spec 015/016 and the owner decide).
4. **Given** several children on the landed branch, **When** A is landed, **Then** every child is retargeted before any deletion; if one
   retarget fails, no deletion happens and the failure is reported per child.

### US4 (P2) Refusals are specific and change nothing

For each precondition of FR-010 to FR-019 a test shows the typed reason, exit status 1 and no push and no `gh pr edit`.

### US5 (P2) Replay after a partial success

1. **Given** the push succeeded but the run was interrupted, **When** the same command is rerun, **Then** it finds the pull request merged
   with the same SHA (or the default branch containing `SHA` as an ancestor and the pull request still open within the bounded wait),
   skips the push, performs the remaining retargets and exits 0 (`already-landed`).
2. **Given** the retarget succeeded and the landing failed, **When** the command is rerun, **Then** the retarget is skipped (child base is
   already the default branch) and the landing proceeds.

## Command

```text
stw land --pr NUMBER --head SHA --reviewed SHA [--plan] [--repo OWNER/NAME | PATH]
         [--remote NAME] [--delete-branch] [--ack-draft] [--wait DURATION]
         [--json | --human]
stw land --stack NUMBER[,NUMBER...] --reviewed NUMBER:SHA [--reviewed NUMBER:SHA ...] [--plan] [same options]
```

The second form is the stack landing (FR-060 and following). `--stack`, `--pr` and `--head` exclude each other (exit 2).

| Flag | Meaning |
| --- | --- |
| `--pr NUMBER` | the pull request to land (required) |
| `--head SHA` | the full 40 hex object id to land (required); must equal the PR's current head |
| `--reviewed SHA` | the owner's statement that an independent review of exactly this head was CLEAR (required for plan and apply, so a plan shows what apply will do; see OQ1); must equal `--head` |
| `--plan` | read-only: preconditions, effects, exact commands; no effect; allowed in agent mode |
| `--stack LIST` | land the listed pull requests as one linear chain with one push of the tip, bottom first, tip last (FR-060); needs one `--reviewed NUMBER:SHA` per listed pull request, no more and no fewer |
| `--delete-branch` | delete the landed PR's head branch on the remote after the retargets (decision 7); with `--stack`, each landed PR's own head branch (FR-075) |
| `--ack-draft` | the owner acknowledges the pull request is a Draft (FR-013); recorded in the output |
| `--wait DURATION` | bounded wait for GitHub to report the pull request merged; default `30s`, maximum `2m` |
| `--remote NAME` | remote; default `origin` if present, else the single remote, else `remote-ambiguous` (exit 1) |
| `--repo OWNER/NAME`, `PATH` | repository selection as `stw hosting pr` does (Spec 022/030) |
| `--json` / `--human` | as `stw status`; they exclude each other (exit 2) |
| `--yes` | **not accepted** (usage error, exit 2) |

## Requirements

### Mode and confirmation

- **FR-001** Apply runs only in human mode as defined by Spec 041; otherwise it exits 1 with `agent-mode` before any write and before
  any credentialed read.
- **FR-002** `--plan` never writes a branch, worktree, index, config or remote and never prompts. Its one disclosed local effect is the
  fetch of FR-016 into a private namespace (`refs/stw/land/...`: objects and that namespace only; `FETCH_HEAD` is not written because the
  fetch runs with `--no-write-fetch-head`; as Spec 032 discloses its fetch); no
  user ref and no remote-tracking ref changes; it is allowed in agent mode and states the mode.
- **FR-003** Apply requires the typed confirmation on `/dev/tty` (`land NUMBER SHA8`); anything else is re-asked up to 3 times, then exit 1
  `confirmation-failed`. No terminal: exit 1 `confirmation-unavailable`. `--yes` is a usage error (exit 2).
- **FR-004** The Spec 042 attribution gate is not applied (the command is not a task push); the mode evidence (terminal facts, markers
  seen, derived mode) is part of the result and the journal line.

### Preconditions (all read fresh, immediately before the first effect, and re-read for the push)

- **FR-010** The pull request is open, in the selected repository, from the same repository (not a fork), not merged.
- **FR-011** Its current head SHA equals `--head` (`head-changed` otherwise) and `--reviewed` equals `--head` (`head-not-reviewed`).
- **FR-012** Required checks for the head are green. Required contexts are the **union** of the rules of the **default branch** (not the PR's base,
  which for a child is the bottom's branch) from the repository rulesets (`GET /repos/{o}/{r}/rules/branches/{default}`, rule type
  `required_status_checks`) and, when present, classic branch protection (`GET /repos/{o}/{r}/branches/{default}/protection`; a 404
  `Branch not protected` means none, not an error; observed on `wstein/sourcesteward`, where the required contexts exist only in the
  ruleset). The head's check runs and statuses come through the Spec 022 transport. Every required context present and successful: ok;
  one missing, pending or failing: `checks-not-green`; both sources readable and no required context: ok with note
  `no-required-checks`; either source unreadable (403, plan limit, permission): `checks-unknown`, apply refuses and the owner may not
  override it in v1 (OQ3). Fixtures cover each case, including a 404 protection with a ruleset list and a 403 ruleset read.
- **FR-013** The pull request is not a Draft, or `--ack-draft` is given.
- **FR-014** Mergeability: GitHub's `mergeable` is not `CONFLICTING` and not `null`-after-bounded-retry (`mergeability-not-computed`, after one bounded re-read; this
  spec's own design, Spec 025 itself does not poll or retry). `mergeable_state` is echoed as GitHub's report under a labelled field and
  drives no conclusion except `dirty` and `blocked` with an explained cause, which refuse. A GitHub `BEHIND` is not a refusal; the Git
  check of FR-016 decides. The repository's `rebaseable` flag is recorded but is not a precondition for a fast-forward push.
- **FR-015** The base is the default branch, or the PR's base is the head branch of a pull request that is **already merged** and whose
  merged SHA is an ancestor of the default branch (the stack's bottom is landed). In the second case the retarget effect (FR-030) is
  part of the plan. Any other base refuses `base-not-default`.
- **FR-016** The object `--head` and the old tip exist locally (after one `git fetch --no-write-fetch-head` of the PR head ref and the default branch into a private
  namespace, never into a user ref)
  and `git merge-base --is-ancestor <old-tip> <head>` holds (exit 0), where `<old-tip>` is the remote default branch tip read with `git ls-remote`
  in this run. Exit 1: `not-fast-forward` (the owner restacks per Spec 015/016; stw never rewrites). Exit 128 (an object, for example the old tip, is absent after the fetch) is `object-missing` or `ancestry-unproven`, never a fast-forward verdict.
- **FR-017** The commits old-tip..head must have author AND committer name/email equal to the exact trusted workspace-policy owner identity. Read-only global Git identity corroborates only; missing policy refuses. Bounded config-origin evidence must show no foreign identity, with only policy-needed name/email values rendered and other user.* values omitted/redacted; never write config. Report at most ten offending commits with escaped fields. Apply the decided default signature policy to this exact reviewed head: unreadable/unmet requirements or unverified signer evidence refuse; any optional host re-sign must already have occurred as a separately authorized 015 local operation and the final SHA must be independently reviewed. 043 runs no signer and creates no commit.
- **FR-018** The number of commits in `old-tip..head` is at most the configured bound (default 200), else `range-too-large`; the
  list is part of the plan.
- **FR-019** Every refusal is typed; the plan lists all failing preconditions, the apply stops at the first and reports it.

### Effects

- **FR-020** The landing push is exactly `git push --porcelain <remote> <head>:refs/heads/<default>`. A plain push of a commit to a
  branch is already a compare-and-swap for a fast-forward: the remote updates the ref only if it still has the advertised old value and
  refuses anything that is not a fast-forward (and a ruleset `non_fast_forward` rule refuses it too). stw therefore uses **no**
  `--force`, no `--force-with-lease` (a force mode, which would allow a rewrite), no `+` refspec, no `--tags`, `--mirror`, `--delete`
  or `--all`, and no other ref. The ref is fully qualified. The ancestry check of FR-016 stays, so the refusal is typed before the push.
  A rejection with `fetch first` or `non-fast-forward` after a passed ancestry check is `base-moved`; a mutation test fails on any
  `--force*`, lease or `+` refspec in the argument vector.
- **FR-021** The pushing process runs with the repository's own hooks as plain Git does (as in Spec 032 decision 8) with an argument
  vector fixed by stw, a bounded timeout and bounded output capture.
- **FR-022** After a successful push the run waits for GitHub to report the pull request `MERGED` with `merge_commit_sha` equal to
  `--head`, with a bounded wait (`--wait`, default 30s, bounded polling of this spec's own design, a fixed backoff of at most 6 single GETs, tested with a fake clock under `testing/synctest`
  per Spec 040).
  `MERGED` with a different SHA is `merged-other-sha` (exit 1; nothing is undone).
- **FR-023** If GitHub did not report `MERGED` within the wait but the remote default branch contains `--head`, the result is
  `landed-not-marked` (exit 1): the push is done and is not retried, the pull request state is reported as read, and the owner can rerun
  to observe it later (idempotent, US5).
- **FR-030** For every open child pull request whose base ref is the landed head ref (list through the Spec 022 transport, bounded
  pagination), after the landing and before any deletion: `gh pr edit CHILD --base <default>` through the bounded transport. A child
  that is not open, or whose base was already the default branch, is skipped. A child's head SHA is read before and after and must not
  change (else `child-changed`, reported).
- **FR-031** If `delete_branch_on_merge` is on, or unreadable, GitHub may delete the landed branch immediately after the merge, before
  stw retargets, and a child would close (R3). Then, when children exist, stw retargets them **before** the landing push (plan order:
  retarget, re-read per FR-034, push). This order is **not measured** (OQ4) and is available only for a repository shape recorded as
  measured (decision 9); otherwise apply refuses `land-unmeasured`. With the setting readable and off, the measured order is used
  (push, then retarget). A refused push after a pre-retarget (the expected case on a protected default branch) leaves the child
  targeting the default branch with the bottom's commits in its range, which changes the diff that was reviewed. The failure text
  prints the restore command for each retargeted child, `gh pr edit CHILD --base OLD_BASE` (the old base still exists because nothing
  was deleted), and stw does not run it on its own.
- **FR-034** After every retarget and before the landing push, stw re-reads the pull request and every child (state, head SHA, base,
  draft, mergeable fields) and re-evaluates FR-010 to FR-018 completely, because a base edit recomputes `mergeable` (often `null` at
  first) and does not rerun checks (the event is `edited`, not `synchronize`). A result that is not green is refused with a typed reason
  (`checks-pending`, `mergeability-not-computed`, or the failing precondition) and the push does not happen; evidence from the old
  base is never reused.
- **FR-032** Deletion of the landed branch (`git push <remote> --delete refs/heads/<ref>`; the only permitted lease is the delete guard
  `--force-with-lease=refs/heads/<ref>:<head>`) happens only with `--delete-branch`, only after FR-030 completed without failure for all
  children, only when decision 7 holds, and never when `delete_branch_on_merge` already removed it (then it is a no-op `already-absent`).
- **FR-033** No other write exists: no comment, label, ready flag, merge call, review call, reopen, branch creation, tag, worktree, index
  or config change, and no local ref change other than objects and `refs/stw/land/*` from the FR-016 fetch. A static test pins the argument vectors (allowlist) of the git and gh invocations.

### Failure, replay and refusal text

- **FR-040** A ruleset or protection refusal of the push (git output containing a remote rejection) is reported with reason
  `push-refused` and the remote's text **verbatim, escaped and bounded** (at most 2 KiB, control characters escaped, secrets pattern
  redacted by the existing output sanitizer). Nothing else is changed: no retarget after a refused landing push (except the FR-031
  pre-retarget already done and reported), no deletion. Exit 1.
- **FR-041** Idempotence: every step is checked against fresh state before it runs. Already merged with `merge_commit_sha == head` and
  all children retargeted: exit 0 `already-landed` with no effect. A different state is classified, never guessed.
- **FR-042** A lost race (the remote rejects the push as `fetch first` or `non-fast-forward`) is `base-moved`, exit 1, no retry; the owner reruns after re-review.
- **FR-043** Before each effect, durably create an immutable intent fact in the validated private land-action namespace; after the effect create a separate linked outcome fact. Bind run/action/attempt/previous digest, PR/head/old tip/effect/result/mode evidence under the shared profile; reserve count/bytes for bounded intent and outcome before effects, never overwrite old facts. Missing completion is unknown, never success or blind retry. The journal is audit evidence; replay revalidates FR-041 native state. stw never stages/commits/pushes journal bytes; ignored/untracked validation and residual export risk remain explicit, not a promise that mode 0700 prevents owner export.
- **FR-044** Cancellation (signal) between effects stops before the next effect and reports what was done; an in-flight `git push` is
  allowed to finish or is waited on, never killed mid-protocol by stw.

### Output

- **FR-050** Human output: one line per precondition, one per effect with outcome, a summary line. Colour only on a terminal with
  `NO_COLOR` unset. Untrusted text (branch names, titles, remote messages) is escaped and bounded.
- **FR-051** `--json` prints exactly one final object conforming to `schemas/v1/land.schema.json` (unpublished until accepted,
  listed in `UNPUBLISHED_SCHEMAS`): `schema_version`, `command: "land"`, `operation` (`plan` or `apply`), `mode` (`human` or `agent`) with
  the mode evidence as in Spec 041, `repository`, `pull_request` (number, state as read, head sha, base ref, `github_reported` mergeable fields,
  draft), `inputs` (`head`, `reviewed`, `ack_draft`, `delete_branch`), `preconditions` (array of `{id, ok, value, reason}`),
  `chain` (only with `--stack`, FR-079), `landable`, `apply_allowed` (false in agent mode or while any precondition including `land-unmeasured` fails), `effects` (array of `{id, kind, argv, outcome, detail}` where `kind` is `fetch`, `retarget`, `push`, `delete-branch`;
  argv is the exact vector), `children`, `result` (`landed`, `already-landed`, `landed-not-marked`, `refused`, `failed`), `reasons`,
  `truncated`.
- **FR-052** `--json` and `--human` are one result model; the schema carries valid and invalid fixtures.

### Exit codes (consistent with `stw tidy` and `stw sync`)

| Code | Meaning |
| --- | --- |
| 0 | `--plan` shows a landable pull request; or the apply ended `landed` or `already-landed` |
| 1 | refusal (any typed reason above, agent mode, failed confirmation), failed precondition in `--plan`, `push-refused`, `base-moved`, `landed-not-marked`, `merged-other-sha`, a failed retarget or deletion, cancellation, operational or output failure |
| 2 | invalid usage (missing `--pr`, `--head` or `--reviewed`; `--head` not 40 hex; `--yes`; `--json` with `--human`; bad `--wait`; outside a repository) |

## Stack landing (`--stack`), owner decision 2026-10-26

Owner decision 2026-10-26, answer "A yes" (German source in the owner-decision record of the workspace): `stw land --stack` lands a
whole stack with ONE fast-forward push of the TIP. Every pull request in the chain must be CLEAR, the chain linear, the CI on the tip
green and the ruleset must allow the owner's direct push. GitHub marks every pull request whose head is in the default branch as
merged. No rebase, no new SHAs, no restack, no retarget. This is the owner's way out of the bottom-up merge, whose rebase-merge
rewrites every SHA and forces a restack and a new CI run per pull request (spike observation D and E in research.md).

Companion rule (answer "B yes"): dependent parts are by default ONE pull request with a commit series (a "virtual stack"; the rule is in the
workspace `AGENTS.md`). Such a pull request needs no `--stack`: the single landing of FR-016 and FR-018 already pushes its whole range
once. `--stack` serves the separate stacked pull requests that must be merged independently or were opened before the rule.

**Interface.** The explicit form is `stw land --stack 12,13,14 --reviewed 12:SHA --reviewed 13:SHA --reviewed 14:SHA` (bottom first). The
explicit list is the owner's statement of the chain, so no discovery can add a pull request nobody named. A flagless wizard
(`stw land` in human mode, listing the chains it finds, as the Spec 045 release wizard does for its steps) is **not** part of this
slice; see Open question Q1. The conventions are the ones of Specs 044 and 045: human mode only, the typed approval read from `/dev/tty`
and never stdin (as Spec 044 D16), refusal in agent mode, `--plan` read-only and allowed for agents.

### Preconditions of a chain

Each is read fresh immediately before the effect and again before the push (FR-034 applies to the whole chain). `--plan` lists every failing
one.

- **FR-060 Named chain.** The list has 2 to 8 distinct pull requests (bound, default 8; a single number is a usage error: use
  `--pr`). Each is open, from the same repository (not a fork), not merged, and the `--reviewed` set equals the list exactly (a missing,
  extra or duplicate entry is a usage error, exit 2).
- **FR-061 Heads equal the recorded reviewed heads.** For each pull request the current head SHA equals the SHA in its `--reviewed
  NUMBER:SHA` (`head-changed` with the number). A head that GitHub changed itself (automatic rebase after a merge, events
  `head_ref_force_pushed` or `automatic_base_change_succeeded`, spike observation E) is the same refusal and also reports FR-064.
- **FR-062 Review evidence: what stw checks and what it does not.** stw has no review ledger and does not invent one. For each pull
  request it checks (a) the exact head equals the SHA the owner states as reviewed (FR-061), (b) the check evidence of FR-012 and
  FR-064, and (c) it reads and **displays** the reviews on that exact head (`GET /repos/{o}/{r}/pulls/{n}/reviews`: `commit_id` equal to the
  head, state, reviewer) filtered by the trusted workspace reviewer policy of OQ1 where the policy exists, as the field `review_evidence`
  (`found`, `none`, or `unknown`). `review_evidence` is information, never a pass or a refusal by itself, because the independent
  review of this workspace may be recorded as a comment or a report outside GitHub's review objects. The owner's confirmation of the review state
  for the whole chain is the typed approval word of FR-072, shown after a table of every pull request, its head, its
  `review_evidence` and its checks. What stw does not verify: that the independent review was CLEAR.
- **FR-063 Chain is linear.** For every consecutive pair the lower head is an **ancestor** of the next head
  (`git merge-base --is-ancestor <head_i> <head_i+1>` exit 0, over local objects after the private fetch of FR-016) and the heads
  differ; exit 1 is `chain-not-linear`, exit 128 is `object-missing`. Each pull request's GitHub base is the default branch or the head
  ref of the previous pull request in the list; any other base is `base-not-in-chain`. Git decides, not the API (decision 4).
- **FR-064 Checks on every head, distinct states.** Required contexts (FR-012, default branch union) are green on the **tip** head
  (`checks-not-green` otherwise). No other pull request of the chain has a failing or missing required context on its own head
  (a pending one is `checks-pending`); with Spec 039's tiered CI the non-tip heads run the light lane only, so the heavy shards are
  required on the tip alone and the required contexts are read from the rules, not assumed. A head with an **empty check rollup after
  a change made by GitHub** (an event `head_ref_force_pushed` or `automatic_base_change_succeeded` not caused by the owner's push, spike
  observation E, gap S9) is a distinct state `head-changed-no-checks`, not `checks-not-green`. The refusal text offers the owner's way out
  as text only: `gh pr close N && gh pr reopen N` starts the `pull_request` run without changing the head (measured on #6 of the spike);
  stw never runs it (FR-033).
- **FR-065 Bottom fast-forwards the default branch.** The remote default tip read by `git ls-remote` in this run is an ancestor of the
  bottom head (equivalently, the merge base of the bottom and the default tip equals the default tip). If the default branch moved,
  the reason is `not-fast-forward` with the detail `rebase needed`: the owner restacks per Spec 015/016 and re-reviews; stw never
  rewrites. The identity check of FR-017 and the range bound of FR-018 apply to the range old-tip..tip (all commits of the chain, up to
  the configured bound).
- **FR-066 No side child.** A pull request that is not in the list but whose base ref is the head ref of a listed pull request
  (a "side child") is `stack-side-child`: the landing would merge its base away. The owner lands it separately first or retargets it by
  hand. The refusal lists it; `--stack` never retargets (FR-070).
- **FR-067 Ruleset allows the owner's direct push (read-only check).** The rules for the default branch and the active rulesets are read
  (FR-012 sources plus `GET /repos/{o}/{r}/rulesets/{id}` for the bypass list). The owner's push is expected to be accepted only if the
  repository shape has a bypass for the owner's role with `bypass_mode: always`. In mode `pull_request` the bypass applies to pull
  requests only, so the direct push is refused by the `pull_request` rule even for the owner (spike settings notes 2026-10-26; expected,
  not measured). Result: `ruleset-forbids-push` for a readable ruleset that cannot allow it, `land-unmeasured` for a readable one not
  yet measured, and `land-unmeasured` for an unreadable one (decision 9). The mode of the bypass is part of the normalized shape key
  in `internal/land/shapes.json`, which gains the field `stack` (measured for the chain landing or not). Only the owner changes a ruleset;
  this spec never proposes a workaround. Note for the owner's decision, not for stw: with bypass mode `always` any token of the same
  account can push the default branch, including an agent's `gh` login; "agents cannot" then holds by the workspace policy and the mode
  guard only (SI8), unless a separate agent identity without the bypass exists.
- **FR-068 Draft, mergeability, fork, mode.** FR-013 (a Draft needs `--ack-draft`, which then names the numbers), FR-014 and FR-010
  apply to every pull request of the chain; agent mode refuses apply (FR-001).

### Effects

- **FR-070 One push, no base edit.** The landing effect is exactly `git push --porcelain <remote> <tip-sha>:refs/heads/<default>` (FR-020
  unchanged: no force mode, no lease, no `+`, fully qualified ref). **No** `gh pr edit` and no other write exists in chain mode, because
  every listed pull request lands with the push and side children are refused (FR-066). The Spec 022 transport extension is therefore
  smaller than for the single landing: the chain mode adds no endpoint; the argument-vector allowlist of FR-033 gains the chain push
  and, on request, the branch deletes of FR-075 (the single landing's base edit stays what it is).
- **FR-071 Atomicity.** One ref update is atomic on the remote: either the whole chain lands, or nothing does (a rejection leaves the
  default branch untouched). The fast-forward compare-and-swap of FR-042 applies: a moved default branch is `base-moved`, exit 1, no retry.
  The marking of the pull requests as merged is GitHub's own, asynchronous step after the push and is not atomic (FR-073).
- **FR-072 Typed approval.** After the table of FR-062 the owner types `land stack TIPNUMBER SHA8 COUNT` (the tip pull request number, the
  first 8 characters of the tip head and the number of pull requests; for example `land stack 14 3f9c2a41 3`) read from `/dev/tty`; three
  tries, then `confirmation-failed`; no terminal `confirmation-unavailable`; `--yes` is a usage error (FR-003). Never in agent mode.
- **FR-073 Marking, bounded wait.** After the push the run waits (FR-022 bound) for **each** listed pull request to be reported `MERGED`
  with `merge_commit_sha` equal to its own head (measured for a single pull request, R1; for a chain this is **to be measured in the spike**,
  research.md E1). Per pull request outcomes are `landed`, `landed-not-marked` (the default branch contains its head, GitHub did not mark it
  within the wait; exit 1, no retry, no repair), or `merged-other-sha`. The tip's own marking is checked as FR-022 does. Which
  pull requests GitHub marks (all with an ancestor head, or only the tip) is unmeasured; until E1 is recorded, `--stack` stays
  `land-unmeasured`.
- **FR-074 Replay.** Idempotent as FR-041: if the default branch already contains the tip head, the push is skipped and the per pull request
  states are classified and reported (`already-landed` when all are `MERGED` with their own head). An interrupted run (signal, lost
  terminal) before the push changed nothing; after the push the rerun only observes (FR-044 applies: a running `git push` is waited on).
- **FR-075 Branches.** stw deletes nothing unless `--delete-branch`: then, after all pull requests are `MERGED` and only for each landed
  pull request's own head ref (decision 7: same repository, name equals the PR head ref, `--force-with-lease=refs/heads/<ref>:<head>`),
  one push per branch so each outcome is its own. With `delete_branch_on_merge` on, GitHub may delete the heads itself after marking
  (then `already-absent`, FR-032); whether it does so for pull requests marked by a push and not by the merge button is **to be measured in
  the spike** (E2). No child exists whose base is a landed branch (FR-066), so the closing of R3 cannot occur.
- **FR-076 Pull request bodies.** stw never edits bodies, titles, labels, comments or state of any pull request (FR-033), nor does it
  close a pull request that GitHub left open (FR-073); it reports each state and the owner decides.
- **FR-077 Failure semantics.** `push-refused` (FR-040), `base-moved` (FR-042), a precondition refusal and `confirmation-failed` leave the
  repository, the default branch and every pull request exactly as before. Cancellation before the push: nothing happened. Journal:
  one immutable intent fact before the push and one outcome fact after it (FR-043), plus one outcome fact per branch delete.
- **FR-078 Not a restack.** A chain that is not linear or whose bottom is not a fast-forward is refused with the typed reason. The way
  forward is the bottom-up landing of `stw land --pr` (one per run) or a restack (Spec 015/016), each with its own review rules.
- **FR-079 JSON.** `--json` adds `chain`: an array in bottom-first order of `{number, head, base_ref, review_evidence, checks, state,
  outcome}` and the typed `reasons` of every failing precondition with the pull request number. The schema stays unpublished (FR-051).

### Scenarios

- **US6 (P1) Show the chain landing.** *Given* three CLEAR pull requests stacked on `main`, *when* `stw land --stack 12,13,14 --reviewed
  12:S1 --reviewed 13:S2 --reviewed 14:S3 --plan` runs (also in agent mode), *then* it prints the chain table, every precondition of FR-060 to
  FR-068, the one push `git push --porcelain origin S3:refs/heads/main`, no write, exit 0 (or exit 1 with all failing preconditions).
- **US7 (P1) Land the chain.** *Given* human mode, all preconditions green and the typed approval, *when* apply runs, *then* the preconditions are
  read again, S3 is pushed once, GitHub reports 12, 13 and 14 `MERGED` with their own heads, no base edit and no branch deletion took place,
  exit 0.
- **US8 (P2) Refusals change nothing.** Per FR-060 to FR-068 one test shows the typed reason, no push; among them `chain-not-linear`,
  `not-fast-forward` (default branch moved), `head-changed-no-checks` with the close-and-reopen text, `checks-not-green` on the tip,
  `stack-side-child`, `ruleset-forbids-push`, `land-unmeasured`.
- **US9 (P2) Replay.** After a push that was interrupted before the marking, a rerun skips the push and reports the per pull request states.

## Safety

SI1 only the owner in human mode with the typed `/dev/tty` confirmation applies; SI2 nothing is pushed except the exact reviewed head
as a plain fast-forward push (no force mode); SI3 the default branch is never rewritten; SI4 no branch is deleted before the
children are retargeted, and none that the PR did not own; SI5 the Git ancestry computation decides, not the API; SI6 untrusted text is
escaped and bounded; SI7 credentials come only from the Spec 022 bounded lookup and are never printed; SI8 mode detection is an accident
guard, not a boundary; SI9 an unmeasured forge behaviour (rulesets) is reported verbatim and never worked around; SI10 `--stack` pushes exactly one ref update (the tip), edits no
pull request and deletes no branch unless asked.

## Edge cases

- The pull request head moved between plan and apply: `head-changed`, nothing pushed.
- The default branch has new commits not in the PR: not a fast-forward, `not-fast-forward`; the owner restacks (Spec 015/016) and
  re-reviews the new head. stw does not rebase.
- The head contains a merge commit or the range has several commits: allowed if fast-forward and identity checks hold.
- A fast-forward whose commits are authored by a co-author trailer only: identity is author/committer fields only.
- `--stack` with a head that GitHub force-pushed after a merge below it: `head-changed` plus `head-changed-no-checks` when the rollup is
  empty (FR-064); the owner closes and reopens that pull request or pushes, and re-reviews.
- `--stack` where the pull requests all have base `main` but their heads are a linear chain (the repository convention before the
  rule of one PR with a commit series): accepted, FR-063 allows both base shapes; the diff each PR shows then contains its parents'
  commits, which changes nothing about the landing.
- `--stack` with one pull request left of the chain already `MERGED`: `already-merged-elsewhere` for that number; the owner names the rest.
- Stack: child head is based on the bottom's head; after the bottom lands, the child is a fast-forward of the new default branch only if
  the bottom landed as exactly its head (always true for this command).
- Two children of one bottom: both retargeted; each child later lands only if still a fast-forward (the second child usually is not;
  `not-fast-forward`, restack).
- Submodule gitlinks: this repository's range is checked only for its own commits; gitlink updates in other repositories are separate
  runs.
- A pull request opened from a fork: `fork-unsupported`.
- Detached HEAD, shallow clone or sparse checkout: only objects are used; shallow history that cannot prove ancestry gives
  `ancestry-unproven`, exit 1.
- The PR was merged by someone else with another SHA: `merged-other-sha`/`already-merged-elsewhere`, nothing changes.

## Interaction with other specs

- **Spec 015/016.** Restack and stack coordination remain the tools for chains that cannot be landed this way (`not-fast-forward`,
  diverged children). `stw land` never restacks, retargets only the child base, and never publishes a rewritten branch.
- **Spec 025.** The merge-state evidence format and its `not-asserted` verdict are reused for reading mergeability; `stw land` itself does
  assert nothing about mergeability beyond its own preconditions, and the Spec 025 rule against claiming a PR "can merge" applies to
  `stw hosting merge-state`, not to this command's explicit refusals.
- **Spec 022.** Reads use the bounded transport. Proposed child-base editing through `gh pr edit` needs the separately reviewed write allowlist below; no endpoint is admitted by this draft. The push uses the native Git runner only after the human landing capability gates pass.
- **Spec 041/042.** Mode detection and the typed confirmation pattern are reused; the agent-attribution gate is not applicable.
- **Spec 016 / 015.** The chain landing replaces the bottom-up merge plus restack for the cases where every pull request of the chain is
  CLEAR and the bottom still fast-forwards; it never replaces a restack for a moved default branch. Reconciliation item 2 below is
  widened (the one push of a linear chain).
- **Spec 045.** The release wizard and `stw land --stack` share the conventions (human mode, typed approval on `/dev/tty`, no agent
  writes). The flagless land wizard of Q1 would follow Spec 045; the release wizard does not call `stw land`.
- **Spec 039 (tiered CI).** The heavy conformance gate runs only on the tip of a stack (owner decision 2026-10-26, answer C), which is the
  head `--stack` pushes; FR-064 reads the required contexts from the rules and does not name a shard.
- **Spec 032.** After a landing the owner runs `stw sync`; `stw land` does not touch local branches or worktrees.

## Reconciliation (owner decisions, not assumed)

The dated OQ-WRITES and OQ-POLICY answers adopt measure-before-enable and
trusted-policy direction only. They do not approve any endpoint, ownership
amendment or constitution change listed below. Each reconciliation item remains
OPEN for a separate owner decision after its proposed amendment and evidence are
reviewable; these recommendations grant no landing/settings authorization.

- **Spec 022** ("no write API is added"; read-only transport). The chain mode (`--stack`) adds no write endpoint (one `git push` of the tip
  and, on request, branch deletes; FR-070). `stw land` proposes one write endpoint, `PATCH /repos/{o}/{r}/pulls/{n}` with
  `base`, through `gh pr edit`. Proposed amendment: "Spec 043 adds exactly one write through this transport, the base edit of an open pull
  request, from a closed allowlist, in human mode only."
- **Spec 016 FR-006** ("do not merge PRs, delete remote refs, force-push ... in this slice") and the **Spec 012 later-effects catalog L5**
  (`stack merge`, owned by 016). `stw land` is a merge-equivalent and, with `--delete-branch`, a remote deletion. Proposed amendment: in
  016 and in L5, "The fast-forward landing of one reviewed pull request, or of one linear chain of reviewed pull requests by one push of
  its tip, with the retarget of the children of a single landing and optional deletion of the landed head branches, is owned by Spec
  043; Spec 016 and L5 keep the native-stack merge."
- **Constitution** ("History editing, remote deletion, push/merge automation ... require later specifications"). Spec 043 is that later
  specification only if accepted by the owner. Proposed amendment: "Push/merge automation is limited to Spec 043 `stw land`, owner-only,
  human mode, typed confirmation, never by agents or schedules."
- **AGENTS.md** ("agents never push the default branch ... never merge"; "the owner merges"). Unchanged: `stw land` is the owner's tool and
  agents only run `--plan`.

## First dogfood step (unmeasured area)

Rulesets and branch protection are not measured on the real repositories, and the read-only observations predict a refusal. The first use
is, in order: (1) `stw land --plan` against a real pull request, which writes nothing and reports `land-unmeasured`; (2) create a
**scratch repository configured with a copy of the real ruleset** (same rule types and bypass modes, rebase-only `pull_request`,
required checks, `delete_branch_on_merge` true), with a stacked pair of trivial docs pull requests, and land them there with the owner at
the terminal, measuring both the direct-push refusal or acceptance and the pre-retarget order (FR-031); (3) record the outcome (accepted
or the verbatim refusal, with the shape) in research.md, which is what lifts `land-unmeasured` for that shape. If the ruleset forbids the
push, the owner must change the ruleset, an owner-only action this spec does not perform or propose to automate.

## Owner decisions (2026-10-25)

All listed former OQ IDs have a **dated disposition on 2026-10-25**.
IDs without an explicit answer adopt the source research recommendations. The
source column distinguishes direct owner answers, adopted recommendations and
cross-spec owner-answer overrides; implementation/native measurement and
review gates remain, never inferred as passing. The concrete [owner profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md)
distinguish saved-plan metadata, external capture payloads, trusted policy,
optional host-only signing and CLI exit-contract revision 2.

| Decision ID | Dated disposition | Source | Remaining implementation/verification gate |
| --- | --- | --- | --- |
| OQ1 | DECIDED: adopt trusted workspace reviewer/check-source policy; retain human --reviewed SHA statement and typed confirmation, no invented ledger. | adopted recommendation; owner-answer override: 016 OQ-POLICY | Independent source/check policy implementation; no agent landing. |
| OQ2 | DECIDED: retain measured-shape scratch gate for ruleset/direct-owner-push acceptance. | adopted recommendation | Unmeasured/unreadable shapes remain land-unmeasured; no owner settings workaround. |
| OQ3 | DECIDED: no checks-unknown override in this v1 capability. | adopted recommendation | Unknown required checks refuse owner apply. |
| OQ4 | DECIDED: preserve conditional readable-off vs on/unreadable order; measure pre-retarget shape before apply. | adopted recommendation | Unmeasured stack shape stays land-unmeasured; fresh post-retarget checks remain. |
| OQ5 | DECIDED: trusted workspace policy is identity authority; global Git identity corroborates, never supplies authority. | adopted recommendation; owner-answer override: 016 OQ-POLICY | Missing policy/source or foreign config/commit fields refuse. |
| OQ6 | DECIDED: refuse unmet/unknown signature policy; optional re-sign is separate local 015 capability before fresh final-SHA review. | adopted recommendation; owner-answer override: 016 OQ-SIGN | 043 neither creates commits nor signs; it lands only the exact already reviewed head. |
| OQ7 | DECIDED: forks/other forges/native jj landing remain deferred/unavailable in this first human-only slice. | adopted recommendation | No native or forge fallback. |
| OQ8 | DECIDED: use typed immutable durable private action record/profile with fsync and no unresolved expiry, distinct land namespace. | adopted recommendation; owner-answer override: 016 OQ-STORE | Journal integration/conformance before owner apply; sync v1 unchanged. |
| ST1 (2026-10-26) | DECIDED "A yes": `stw land --stack` lands a linear chain with one push of the tip (FR-060 to FR-079); first feature after v0.2.0; first measured in `wstein/stw-release-spike` with a second stack. | direct owner answer 2026-10-26 | Measurements E1 to E4 (research.md) and the security review task before code; stays `land-unmeasured` until recorded. |
| ST2 (2026-10-26) | DECIDED "B yes": dependent parts are by default one PR with a commit series; separate stacked PRs only when parts must merge independently (depth at most two). | direct owner answer 2026-10-26 | Workspace `AGENTS.md` (anchorage-workspace PR); no change to this command. |

## Open questions (not decided here)

- **Q1 (owner, not blocking).** Flagless wizard (`stw land` in human mode listing chains it finds) in this slice or later? Recommended: later, on
  the same engine; the explicit `--stack` list is the safer first form because no discovery can add a pull request nobody named.
- **Q2 (owner).** Ruleset bypass mode `always` for the owner's role is needed for any direct push (FR-067); it also lets an agent session
  that holds the owner's token push the default branch. Accept the policy-only separation (AGENTS.md and the mode guard) or set up a
  separate agent identity first?

Compatibility anchor only: the IDs above are settled owner decisions, not open
questions. Remaining unavailable capabilities require implementation/measurement
evidence, not an invented additional answer to these eight IDs. The separate
reconciliation amendments above remain OPEN for owner decisions.
