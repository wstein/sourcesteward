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
5. **Two network writes only**, each from a closed set: `git push <sha>:refs/heads/<default>` and `gh pr edit <child> --base <default>`
   through the bounded transport of Spec 022. Optional third, only on request: delete of the landed bottom branch (decision 7).
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
9. **Unmeasured.** Rulesets and branch protection are **not measured** on the real repositories (research.md caveat C1). Whether a push
   of a reviewed head to the default branch is accepted there is unknown; the command must treat a refusal as a normal outcome (FR-040).

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
   `SHA` is pushed to the default branch as a fast-forward with a compare-and-swap on the old tip, GitHub reports the pull request
   `MERGED` with merge commit `SHA` within the bounded wait, and the exit status is 0.
2. **Given** the default branch moved after the plan, **When** apply runs, **Then** the fresh read or the compare-and-swap push detects it,
   nothing changes and the exit status is 1 (`base-moved`).
3. **Given** the pull request head is not `--head`, **When** apply runs, **Then** it refuses (`head-changed`) and pushes nothing.

### US3 (P1) Land a stack, bottom first

1. **Given** PR A (base main) and child PR B (base `topic-a`), **When** the owner lands A, **Then** after the push the run retargets B to
   main (`gh pr edit B --base main`), and with `--delete-branch` deletes `topic-a` only afterwards; B stays open with its head unchanged.
2. **Given** A is landed and B still targets `topic-a`, **When** `stw land --pr B` runs, **Then** it sees that the stack's bottom is
   landed, performs the retarget as its first effect if `topic-a` still exists, and then lands B.
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
| `--reviewed SHA` | the owner's statement that an independent review of exactly this head was CLEAR (required for apply; see open question OQ1); must equal `--head` |
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
- **FR-002** `--plan` never writes and never prompts; it is allowed in agent mode and states the mode.
- **FR-003** Apply requires the typed confirmation on `/dev/tty` (`land NUMBER SHA8`); anything else is re-asked up to 3 times, then exit 1
  `confirmation-failed`. No terminal: exit 1 `confirmation-unavailable`. `--yes` is a usage error (exit 2).
- **FR-004** The Spec 042 attribution gate is not applied (the command is not a task push); the mode evidence (terminal facts, markers
  seen, derived mode) is part of the result and the journal line.

### Preconditions (all read fresh, immediately before the first effect, and re-read for the push)

- **FR-010** The pull request is open, in the selected repository, from the same repository (not a fork), not merged.
- **FR-011** Its current head SHA equals `--head` (`head-changed` otherwise) and `--reviewed` equals `--head` (`head-not-reviewed`).
- **FR-012** Required checks for the head are green. Source: check runs and statuses of the head SHA through the Spec 022 transport and
  the required-check list of the base branch where readable. If the required list is unreadable (permission) the result is
  `checks-unknown` and apply refuses; the owner may not override it in v1 (open question OQ3).
- **FR-013** The pull request is not a Draft, or `--ack-draft` is given.
- **FR-014** Mergeability: GitHub's `mergeable` is not `CONFLICTING` and not `null`-after-bounded-retry (`mergeability-not-computed`,
  Spec 025 treatment: one bounded re-read, no polling loop). `mergeable_state` is echoed as GitHub's report under a labelled field and
  drives no conclusion except `dirty` and `blocked` with an explained cause, which refuse. A GitHub `BEHIND` is not a refusal; the Git
  check of FR-016 decides. The repository's `rebaseable` flag is recorded but is not a precondition for a fast-forward push.
- **FR-015** The base is the default branch, or the PR's base is the head branch of a pull request that is **already merged** and whose
  merged SHA is an ancestor of the default branch (the stack's bottom is landed). In the second case the retarget effect (FR-030) is
  part of the plan. Any other base refuses `base-not-default`.
- **FR-016** The object `--head` exists locally (after one `git fetch` of the PR head ref into a private namespace, never into a user ref)
  and `git merge-base --is-ancestor <old-tip> <head>` holds, where `<old-tip>` is the remote default branch tip read with `git ls-remote`
  in this run. Not an ancestor: `not-fast-forward` (the owner restacks per Spec 015/016; stw never rewrites). A local object that is
  absent after the fetch: `object-missing`.
- **FR-017** The commits `old-tip..head` have owner-only identities: author and committer name and email each equal the owner identity
  from the repository's effective Git identity read from the owner's global configuration (read only, never written). Any other value
  refuses `identity-mismatch` and lists at most 10 offending commits with escaped fields. A throwaway-identity commit is therefore refused.
- **FR-018** The number of commits in `old-tip..head` is at most the configured bound (default 200), else `range-too-large`; the
  list is part of the plan.
- **FR-019** Every refusal is typed; the plan lists all failing preconditions, the apply stops at the first and reports it.

### Effects

- **FR-020** The landing push is exactly `git push --porcelain --force-with-lease=refs/heads/<default>:<old-tip> <remote>
  <head>:refs/heads/<default>`, with `<old-tip>` the value read in FR-016. It is never `--force`, never without the lease value, never to
  any other ref, never with `--tags`, `--mirror`, `--delete` or `--all`. The ref must be fully qualified. The lease protects a moved
  default branch; the preceding ancestry check protects against a non-fast-forward; both are required, because a lease alone would
  allow a rewrite of the default branch.
- **FR-021** The pushing process runs with the repository's own hooks as plain Git does (as in Spec 032 decision 8) with an argument
  vector fixed by stw, a bounded timeout and bounded output capture.
- **FR-022** After a successful push the run waits for GitHub to report the pull request `MERGED` with `merge_commit_sha` equal to
  `--head`, with a bounded wait (`--wait`, default 30s, bounded polling with a fixed backoff of at most 6 reads, each a single GET).
  `MERGED` with a different SHA is `merged-other-sha` (exit 1; nothing is undone).
- **FR-023** If GitHub did not report `MERGED` within the wait but the remote default branch contains `--head`, the result is
  `landed-not-marked` (exit 1): the push is done and is not retried, the pull request state is reported as read, and the owner can rerun
  to observe it later (idempotent, US5).
- **FR-030** For every open child pull request whose base ref is the landed head ref (list through the Spec 022 transport, bounded
  pagination), after the landing and before any deletion: `gh pr edit CHILD --base <default>` through the bounded transport. A child
  that is not open, or whose base was already the default branch, is skipped. A child's head SHA is read before and after and must not
  change (else `child-changed`, reported).
- **FR-031** If `delete_branch_on_merge` is on (read from the repository settings when readable; unreadable counts as unknown), GitHub may
  delete the landed branch immediately after the merge, before stw retargets: a child would then close (R3). stw therefore, **before** the
  landing push and only when children exist, retargets them first when the setting is on or unknown (plan order: retarget, push), and
  otherwise lands first (the measured order, R4). This pre-retarget ordering is **not measured**; see OQ4. When it is used, a failure of
  the landing push after the retarget is reported and the retarget is not undone (the child then targets the default branch with the
  bottom's commits in its range; the owner is told this in the failure text).
- **FR-032** Deletion of the landed branch (`git push <remote> --delete refs/heads/<ref>` with the lease value of the PR head SHA, i.e.
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
- **FR-042** A lost race at the lease (`stale info`/rejected lease) is `base-moved`, exit 1, no retry; the owner reruns after re-review.
- **FR-043** The run writes a **journal line** to the stw state store before each effect and completes it after (run id, PR, head, old
  tip, effect, result, mode evidence); the journal is local only and is never committed or pushed. A run interrupted between effects
  is replayed through FR-041, not through the journal (the journal is the audit record).
- **FR-044** Cancellation (signal) between effects stops before the next effect and reports what was done; an in-flight `git push` is
  allowed to finish or is waited on, never killed mid-protocol by stw.

### Output

- **FR-050** Human output: one line per precondition, one per effect with outcome, a summary line. Colour only on a terminal with
  `NO_COLOR` unset. Untrusted text (branch names, titles, remote messages) is escaped and bounded.
- **FR-051** `--json` prints exactly one final object conforming to `schemas/v1/land.schema.json` (unpublished until accepted,
  listed in `UNPUBLISHED_SCHEMAS`): `schema_version`, `command: "land"`, `mode` (`plan` or `apply`), `run_mode` (`human` or `agent`) with
  the mode evidence, `repository`, `pull_request` (number, state as read, head sha, base ref, `github_reported` mergeable fields,
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
as a fast-forward with a lease on the observed tip; SI3 the default branch is never rewritten; SI4 no branch is deleted before the
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

## First dogfood step (unmeasured area)

Rulesets and branch protection are not measured on the real repositories. The first use is, in order: (1) `stw land --plan` against a
real pull request, which writes nothing; (2) land **one trivial docs pull request in a scratch repository** that mirrors the real
repository's ruleset settings (or has none, as in research.md) with the owner at the terminal; (3) record the outcome (accepted or the
verbatim refusal) as new measurements in research.md before any real repository is touched. If the real repositories refuse a direct
push, the command stays a plan printer there and this spec records the refusal (OQ2).

## Open questions (not decided here)

- **OQ1 Review evidence.** `--reviewed SHA` is only the owner's statement. Whether stw should read a machine-checkable CLEAR (a ledger
  line, a signed-off PR review by an allowed reviewer on the exact head, a status/check named by the owner) is open. No ledger format is
  invented here.
- **OQ2 Rulesets.** Whether the default branches of the real repositories accept an owner push of a reviewed head (bypass actor, no
  required-PR rule) is unknown. Possibly this command only applies where GitHub allows it.
- **OQ3 Unknown required checks.** Should the owner be able to override `checks-unknown` explicitly? Not in v1 unless asked.
- **OQ4 `delete_branch_on_merge` ordering.** The pre-retarget order (FR-031) is not measured; the measured run had the setting unknown
  or off. A scratch run must measure it before FR-031 is implemented.
- **OQ5 Identity source.** Which value is "the owner identity": the global Git configuration read, a workspace setting, or
  `CODEOWNERS`. AGENTS.md rule is a pre-push check against the global identity; this spec follows it and asks to confirm.
- **OQ6 Signed commits.** Should `--require-signed` be a later option for repositories that require signatures on the default branch?
- **OQ7 Fork pull requests and other forges.** Left to a later spec; jj colocated repositories are refused in v1.
- **OQ8 Journal location.** Reuse of the Spec 029/031 journal record type versus a plain audit file.
