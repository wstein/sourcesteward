# Feature Specification: `stw land`, the owner's fast-forward landing of a reviewed pull request

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
2. **One pull request per run.** A stack is landed by one run per pull request, bottom first. The run for a child performs the retarget
   step of the measured order (decision 6); nothing lands two pull requests at once.
3. **Fresh preconditions.** Every precondition is read again immediately before the effect; nothing from `--plan` is trusted by the apply.
4. **Git decides.** Fast-forwardness is computed with Git over local objects (`merge-base --is-ancestor`); the GitHub API is evidence for
   state, never the proof of an ancestry.
5. **Two network writes only**, each from a closed set: `git push <remote> <sha>:refs/heads/<default>` and the base edit of a child
   (`PATCH /repos/{o}/{r}/pulls/{n}` with `base`, run as `gh pr edit <child> --base <default>`), an explicit extension of the Spec 022
   transport (see Reconciliation). Optional third, only on request: delete of the landed bottom branch (decision 7).
6. **Order for a stack (measured, research.md R3 and R4).** Land the bottom; for every child pull request whose base is the landed
   branch run `gh pr edit CHILD --base DEFAULT`; only then, and only if decision 7 allows, delete the landed branch; then land the child.
   Deleting the bottom branch before retargeting closes the child and cannot be undone by reopening (R3).
7. **Branch deletion.** Only on request (`--delete-branch`), or never by stw when GitHub's `delete_branch_on_merge` is on (GitHub does it,
   and the retarget step then has to precede the landing push of the bottom, see FR-031). stw never deletes a branch it cannot show was
   pushed for this pull request by its head repository owner (head repository equals the base repository, branch name equals the PR head
   ref), never a branch other than the landed PR's head ref, and never when a different open pull request still uses it as base after the
   retarget step.
8. **Out of scope for v1.** jj repositories (refused, `unsupported-vcs`), non-GitHub forges (refused, `unsupported-forge`), pull requests
   from forks, landing as anything other than a fast-forward (no squash, no rebase-merge, no merge commit), tags, releases, submodule
   gitlinks, auto-restack of a child that is not a fast-forward (Spec 015/016 remain the tools for that), and landing several pull
   requests in one run.
9. **Hard gate: unmeasured repository shapes fail closed.** Rulesets and branch protection are **not measured**. Read-only
   observations (research.md, "Real repositories, read-only observations") show that `wstein/sourcesteward` has `delete_branch_on_merge`
   true and an active ruleset `main` with `deletion`, `non_fast_forward`, `required_linear_history`, `pull_request` (rebase only) and
   `required_status_checks`, whose only bypass is `bypass_mode: pull_request`; a direct push of the owner is therefore **expected to be
   refused** (expected, not measured). `wstein/anchorage-workspace` has `delete_branch_on_merge` true and its ruleset and protection
   endpoints answer 403 (plan limit), so its shape is unreadable. **Apply fails closed** with the typed refusal `land-unmeasured` (exit
   1) for a repository until research.md records a measured scratch run for that repository's ruleset shape (ruleset rule types and
   bypass modes, `delete_branch_on_merge`, children present or not); `--plan` still works and reports `land-unmeasured` among its
   preconditions. The measured shapes are an explicit allowlist compiled into the command (data file reviewed in a PR), keyed by the
   normalised shape, never by repository name. If the ruleset forbids the push, the owner must change the ruleset (owner-only); this
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
   vectors, performs no write and exits 0.
2. **Given** any failing precondition, **When** `--plan` runs, **Then** it lists every failing precondition (not only the first), the
   plan is marked `landable: false` and the exit status is 1.
3. **Given** agent mode, **When** `--plan` runs, **Then** the plan is shown with `mode: agent` and `apply_allowed: false`.

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
```

| Flag | Meaning |
| --- | --- |
| `--pr NUMBER` | the pull request to land (required) |
| `--head SHA` | the full 40 hex object id to land (required); must equal the PR's current head |
| `--reviewed SHA` | the owner's statement that an independent review of exactly this head was CLEAR (required for plan and apply, so a plan shows what apply will do; see OQ1); must equal `--head` |
| `--plan` | read-only: preconditions, effects, exact commands; no effect; allowed in agent mode |
| `--delete-branch` | delete the landed PR's head branch on the remote after the retargets (decision 7) |
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
  fetch of FR-016 into a private namespace (`refs/stw/land/...`: objects and that namespace only, as Spec 032 discloses its fetch); no
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
- **FR-016** The object `--head` and the old tip exist locally (after one `git fetch` of the PR head ref and the default branch into a private
  namespace, never into a user ref)
  and `git merge-base --is-ancestor <old-tip> <head>` holds (exit 0), where `<old-tip>` is the remote default branch tip read with `git ls-remote`
  in this run. Exit 1: `not-fast-forward` (the owner restacks per Spec 015/016; stw never rewrites). Exit 128 (an object, for example the old tip, is absent after the fetch) is `object-missing` or `ancestry-unproven`, never a fast-forward verdict.
- **FR-017** The commits `old-tip..head` have owner-only identities: author and committer name and email, read from the commits themselves, each equal the owner's **global**
  identity (`git config --global user.name` and `user.email`, read only, never written). The effective configuration with local or
  worktree scope is never the expectation (AGENTS.md identity-leak rule). Also `git config --show-origin --get-regexp '^user\.'` must
  show no `user.name` or `user.email` outside the global scope, else `identity-leak`. Any other commit value refuses
  `identity-mismatch` and lists at most 10 offending commits with escaped fields. The check catches leaked throwaway identities, not
  agent authorship: agent commits carry the owner identity.
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
- **FR-033** No other write exists: no comment, label, ready flag, merge call, review call, reopen, branch creation, tag, local ref or
  worktree change. A static test pins the argument vectors (allowlist) of the git and gh invocations.

### Failure, replay and refusal text

- **FR-040** A ruleset or protection refusal of the push (git output containing a remote rejection) is reported with reason
  `push-refused` and the remote's text **verbatim, escaped and bounded** (at most 2 KiB, control characters escaped, secrets pattern
  redacted by the existing output sanitizer). Nothing else is changed: no retarget after a refused landing push (except the FR-031
  pre-retarget already done and reported), no deletion. Exit 1.
- **FR-041** Idempotence: every step is checked against fresh state before it runs. Already merged with `merge_commit_sha == head` and
  all children retargeted: exit 0 `already-landed` with no effect. A different state is classified, never guessed.
- **FR-042** A lost race (the remote rejects the push as `fetch first` or `non-fast-forward`) is `base-moved`, exit 1, no retry; the owner reruns after re-review.
- **FR-043** The run writes a **journal line** to the stw state store before each effect and completes it after (run id, PR, head, old
  tip, effect, result, mode evidence); the journal is local only and is never committed or pushed. A run interrupted between effects
  is replayed through FR-041, not through the journal (the journal is the audit record).
- **FR-044** Cancellation (signal) between effects stops before the next effect and reports what was done; an in-flight `git push` is
  allowed to finish or is waited on, never killed mid-protocol by stw.

### Output

- **FR-050** Human output: one line per precondition, one per effect with outcome, a summary line. Colour only on a terminal with
  `NO_COLOR` unset. Untrusted text (branch names, titles, remote messages) is escaped and bounded.
- **FR-051** `--json` prints exactly one final object conforming to `schemas/v1/land.schema.json` (unpublished until accepted,
  listed in `UNPUBLISHED_SCHEMAS`): `schema_version`, `command: "land"`, `operation` (`plan` or `apply`), `mode` (`human` or `agent`) with
  the mode evidence as in Spec 041, `repository`, `pull_request` (number, state as read, head sha, base ref, `github_reported` mergeable fields,
  draft), `inputs` (`head`, `reviewed`, `ack_draft`, `delete_branch`), `preconditions` (array of `{id, ok, value, reason}`),
  `landable`, `effects` (array of `{id, kind, argv, outcome, detail}` where `kind` is `fetch`, `retarget`, `push`, `delete-branch`;
  argv is the exact vector), `children`, `result` (`landed`, `already-landed`, `landed-not-marked`, `refused`, `failed`), `reasons`,
  `truncated`.
- **FR-052** `--json` and `--human` are one result model; the schema carries valid and invalid fixtures.

### Exit codes (consistent with `stw tidy` and `stw sync`)

| Code | Meaning |
| --- | --- |
| 0 | `--plan` shows a landable pull request; or the apply ended `landed` or `already-landed` |
| 1 | refusal (any typed reason above, agent mode, failed confirmation), failed precondition in `--plan`, `push-refused`, `base-moved`, `landed-not-marked`, `merged-other-sha`, a failed retarget or deletion, cancellation, operational or output failure |
| 2 | invalid usage (missing `--pr`, `--head` or `--reviewed`; `--head` not 40 hex; `--yes`; `--json` with `--human`; bad `--wait`; outside a repository) |

## Safety

SI1 only the owner in human mode with the typed `/dev/tty` confirmation applies; SI2 nothing is pushed except the exact reviewed head
as a plain fast-forward push (no force mode); SI3 the default branch is never rewritten; SI4 no branch is deleted before the
children are retargeted, and none that the PR did not own; SI5 the Git ancestry computation decides, not the API; SI6 untrusted text is
escaped and bounded; SI7 credentials come only from the Spec 022 bounded lookup and are never printed; SI8 mode detection is an accident
guard, not a boundary; SI9 an unmeasured forge behaviour (rulesets) is reported verbatim and never worked around.

## Edge cases

- The pull request head moved between plan and apply: `head-changed`, nothing pushed.
- The default branch has new commits not in the PR: not a fast-forward, `not-fast-forward`; the owner restacks (Spec 015/016) and
  re-reviews the new head. stw does not rebase.
- The head contains a merge commit or the range has several commits: allowed if fast-forward and identity checks hold.
- A fast-forward whose commits are authored by a co-author trailer only: identity is author/committer fields only.
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
- **Spec 022.** All reads and the `gh pr edit` go through the bounded transport; the push uses the native Git runner.
- **Spec 041/042.** Mode detection and the typed confirmation pattern are reused; the agent-attribution gate is not applicable.
- **Spec 032.** After a landing the owner runs `stw sync`; `stw land` does not touch local branches or worktrees.

## Reconciliation (owner decisions, not assumed)

This spec clashes with existing text. It edits none of it; each item needs an owner decision and the proposed amendment is given.

- **Spec 022** ("no write API is added"; read-only transport). `stw land` adds one write endpoint, `PATCH /repos/{o}/{r}/pulls/{n}` with
  `base`, through `gh pr edit`. Proposed amendment: "Spec 043 adds exactly one write through this transport, the base edit of an open pull
  request, from a closed allowlist, in human mode only."
- **Spec 016 FR-006** ("do not merge PRs, delete remote refs, force-push ... in this slice") and the **Spec 012 later-effects catalog L5**
  (`stack merge`, owned by 016). `stw land` is a merge-equivalent and, with `--delete-branch`, a remote deletion. Proposed amendment: in
  016 and in L5, "The fast-forward landing of one reviewed pull request, with the retarget of its children and optional deletion of its
  own head branch, is owned by Spec 043; Spec 016 and L5 keep the native-stack merge."
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

## Open questions (not decided here)

- **OQ1 Review evidence.** `--reviewed SHA` is only the owner's statement. Whether stw should read a machine-checkable CLEAR (a ledger
  line, a signed-off PR review by an allowed reviewer on the exact head, a status/check named by the owner) is open. No ledger format is
  invented here.
- **OQ2 Rulesets (hard gate).** Whether the default branches of the real repositories accept an owner push of a reviewed head is
  unknown and, for `sourcesteward`, expected to be refused (`pull_request` rule, bypass mode `pull_request`). Apply stays
  `land-unmeasured` until a scratch run for the shape is recorded; no default accepts it.
- **OQ3 Unknown required checks.** Should the owner be able to override `checks-unknown` explicitly? Not in v1 unless asked.
- **OQ4 `delete_branch_on_merge` ordering (hard gate).** The pre-retarget order (FR-031) is not measured and both real repositories have
  the setting on. A scratch run must measure it; until then apply refuses `land-unmeasured` for stacks on such shapes.
- **OQ5 Identity source.** Which value is "the owner identity": the global Git configuration read, a workspace setting, or
  `CODEOWNERS`. AGENTS.md rule is a pre-push check against the global identity; this spec follows it and asks to confirm.
- **OQ6 Signed commits.** Should `--require-signed` be a later option for repositories that require signatures on the default branch?
- **OQ7 Fork pull requests and other forges.** Left to a later spec; jj colocated repositories are refused in v1.
- **OQ8 Journal location.** Reuse of the Spec 029/031 journal record type versus a plain audit file.
