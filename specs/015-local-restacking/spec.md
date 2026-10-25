# Feature Specification: Local Restacking

**Feature Branch**: `codex/spec-015-stack-toolbelt` (2026-10-25 documentation amendment; original draft branch `codex/lazyjj-inspired-roadmap`)
**Created**: 2026-10-15
**Status**: Draft amendment, 2026-10-25 — first slice (`stw restack plan`) implemented in source on 2026-10-21 and hidden, schema unpublished. Historical T001 design review is complete at its recorded SHA; T021 minimum/current Git conformance, T023 exact implementation-head review and T024 limitation emission remain open. Local apply/recovery and jj below are proposed, unimplemented slices. Push remains outside 015 in Spec 016.
**Input**: Add after the mutation/recovery contract.
**Priority rating**: 8/10 (user-selected product priority, not verification evidence)

## Motivating scenario and first slice (owner decisions, 2026-10-21)

The owner rebase-merges a parent pull request. GitHub rewrites the commits, so every child branch still carries the old parent commits under old identities. By hand the coordinator restacks each child and proves the result:

1. `git rebase --onto <new parent> <old parent> <branch>`, naming both full SHAs.
2. `git range-diff <old parent>..<old head> <new parent>..<new head>`: every row `=`.
3. `git cherry <new parent> <old parent>`: every row `-` (the parent commits were dropped because the new parent already contains them); where patch ids differ, tree equivalence of the touched paths.
4. Equality of the branch's own commit diffs, and an author and committer identity check over `base..head`.
5. `git push --force-with-lease=<ref>:<observed old full SHA> origin <full new SHA>:refs/heads/<ref>`.
6. Resync text for the owner: `git fetch --prune`, then `git rebase --onto origin/<branch> <old SHA> <branch>` if the owner has local commits, otherwise `git switch <branch> && git reset --hard origin/<branch>` (destructive for local-only work and uncommitted changes).

The first slice makes steps 1 to 4 and the texts of steps 5 and 6 a read-only, offline, reviewable `stw restack plan` for native Git. Later slices add `stw restack apply` (Git), a jj peer with a stated capability gap, and, outside this spec, the push (Spec 016). The owner decisions of 2026-10-21 that bind this slice:

| # | Decision |
| --- | --- |
| D4 | Spec 015 starts Git-only. jj follows later with a stated capability gap (FR-007 and the Tier 1 rule): `stw restack` on a jj repository returns `unsupported-backend` and never falls back to Git. |
| D5 | The read-only `restack plan` comes first; `restack apply` is a later slice with its own tasks. |
| D6 | The Spec 012 dependency is replaced by an explicit minimal input: old parent, new parent and the branch to restack, each as a full commit id or a fully qualified ref (the branch is specifically `refs/heads/...`). |
| D7 | Replay uses `merge-tree`/plumbing and a compare-and-swap ref update, never the porcelain `rebase` (it runs hooks and touches worktrees). The plan predicts with plumbing in a private object quarantine; apply is specified later. |
| D8 | Push with lease is not in 015; it belongs to a Spec 016 L2 amendment. The plan only prints the lease command text and never prints `--force` or a bare `--force-with-lease`. |
| D9 | The author and committer identity check over `base..head` is part of the plan evidence. The expected identities come only from the explicit `--identity` flag (a pass requires it; without it the check is `unknown`), and the replay commits use a fixed constant identity (implementation clarification 2026-10-21: the fixed identity is the committer; the original author is kept so `git range-diff` pairs `=`); stw never writes Git configuration. |
| D12 | "Keep-both" ledger conflicts are defined by an explicit path allowlist in stw configuration, default empty. No heuristic. (Classification is a later slice (WP6, with its `--keep-both-config` flag); the first-slice plan reports conflicting paths without classes.) |
| D13 | The plan includes predicted conflicts and the owner resync command text. |
| Later | Verify commands from configuration are a later slice: executed only if declared, in a sandbox, without network, never repository-controlled. |

Semantic breaks after a clean replay (for example a parent changed a signature the branch calls) are not detectable by stw in this slice. The plan states this in every result and reports instead the touched-paths overlap between the parent's merged changes and the branch's own changes as a risk indicator. An empty overlap is not evidence of safety.

## User Scenarios & Testing

### User Story 1 — Preview a local stack move (Priority: P1)

A caller understands a stack rewrite before applying it.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an explicit stack and destination base, **When** restacking is planned, **Then** affected revisions, descendants, references, workspaces and potential conflicts are listed.
2. **Given** ambiguous boundaries, immutable/shared revisions or unsupported topology, **When** restacking is planned, **Then** blocked scope and reasons are shown without guessing or widening the target.

### User Story 2 — Apply and recover a local rewrite (Priority: P1)

A caller authorizes a bounded rewrite with recovery evidence.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** an authorized unchanged plan, **When** restacking is applied, **Then** only declared native effects occur and old/new identities and reference outcomes are recorded.
2. **Given** a new conflict, cancellation or partial failure, **When** restacking stops, **Then** native conflict/interruption state and recovery options are reported without automatic discard.
3. **Given** changed refs, workspace owners or operation state, **When** apply is attempted, **Then** stale actions refuse before their mutations.

### User Story 3 — Rely on a bounded agent contract (Priority: P1)

A caller receives the same evidence in human and structured output without hidden effects.

**Why this priority**: Both humans and automation must recognize success, uncertainty and refusal.

**Independent Test**: Exercise normal, unsupported, malformed and cancelled cases for minimum/current supported Git and jj, with effect sentinels and published-contract fixtures.

**Acceptance Scenarios**:

1. **Given** supported success or failure, **When** either output mode is selected, **Then** native identities, qualifications and outcomes agree and complete structured results validate against their documented contract.
2. **Given** ambiguous ownership, a missing/unsupported backend or GitButler-managed state, **When** execution is requested, **Then** it refuses clearly without using another backend as a fallback.
3. **Given** cancellation, malformed data or an exceeded documented bound, **When** execution stops, **Then** incomplete evidence is explicit and never authorizes a consequential action; any completed effects are reported.
4. **Given** repository-controlled helpers, instructions or configuration, **When** the operation runs, **Then** no undeclared executable, hook, prompt or network action occurs and state outside declared effects is preserved.
5. **Given** a missing mandatory backend/version or absent acceptance execution, **When** conformance is evaluated, **Then** the missing evidence blocks acceptance rather than counting as a pass.

### Edge Cases

- Merge topology, empty/redundant changes, immutable jj commits, divergent operations and multiple workspaces.
- Hook failures, conflict creation, cancellation and concurrent reference/operation changes.

## Requirements

### Functional Requirements

- **FR-001**: Depend on the Spec 010 mutation/recovery contract and, for the first slice, on the explicit minimal input of FR-011 instead of Spec 012 stack observation (D6).
- **FR-002**: Require explicit source scope and destination identity; disclose descendant rewrites, reference movements, disk/index changes, snapshots and hook effects before apply.
- **FR-003**: Respect immutable/protected and shared work; refuse unsupported topology or ownership rather than falling back to another backend.
- **FR-004**: Retain recoverable pre-operation identities and report native old/new mappings, including jj stable change identity versus rewritten commit identity.
- **FR-005**: Handle Git interrupted operations and jj stored conflicts explicitly; neither backend may report a clean completed outcome when unresolved work remains.
- **FR-006**: Revalidate relevant state before consequential actions; expose partial failure, cancellation and recovery limitations. Never fetch, push or force-push as part of local restacking. The plan prints, as inert text only, the lease push command (`--force-with-lease=<ref>:<observed old full SHA>` with an explicit full new SHA, never `--force`, never a bare lease) and the owner resync commands; stw does not run them, and executing a push belongs to Spec 016 after its own amendment and authorization.
- **FR-007**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-008**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-009**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-010**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

#### First slice: `stw restack plan` (Git, read-only)

- **FR-011**: `stw restack plan` takes the branch to restack, the old parent and the new parent, each as a 40-hex commit id or a fully qualified ref (`refs/...`); short names, abbreviations, revision expressions and `HEAD` are syntax errors. It resolves each to one commit, records the observed branch tip (`old_head`), and may be pinned with `--expect-head`. The old parent must be an ancestor of the old head (`git merge-base --is-ancestor old_parent old_head`); otherwise `old_parent..old_head` would silently move the parent's own commits, and the plan refuses with `old-parent-not-ancestor`. The set of commits to move is then exactly `old_parent..old_head`.
- **FR-012**: The plan is read-only: no ref, index, worktree, configuration or repository-object change, no network, no hook, no helper, no editor, no pager. Predictions use plumbing in a private temporary object quarantine that is removed afterwards; a sentinel test proves the object store and refs are content-identical afterwards (Git may freshen the mtime of an already-existing object when it writes an identical one, so mtimes are not claimed).
- **FR-013**: The plan reports evidence, never a verdict that authorizes anything: (a) `git range-diff`-equivalent pairing of old commits against predicted replayed commits, (b) `git cherry` classification of the old parent's own commits against the new parent with tree equivalence for patch-id mismatches, (c) per-commit own-diff equality, (d) author and committer identity check over the moved commits, (e) predicted conflicts from `merge-tree` with the conflicting paths, (f) the touched-paths overlap risk indicator, (g) affected refs and worktrees, (h) hook and configuration disclosure, and (i) the lease push text and resync text.
- **FR-014** (a repository-local `core.hooksPath` is refused although the runner overrides it: the plan refuses what the Spec 029/031 observation refuses, so both agree on one repository): Refuse, with a stable diagnostic and no partial plan, a target whose worktree is dirty or mid-operation, a branch checked out in another worktree (reported, never touched), an empty moved range (`old_head` equals `old_parent`: `empty-moved-range`, nothing to restack), an old parent that is not an ancestor of the old head (`old-parent-not-ancestor`), unrelated histories (no merge base between old parent and new parent), missing objects, ambiguous or non-unique refs, merge commits in the moved range, shallow or replace/graft-affected history, symlinked or non-canonical repository paths, and configuration or hook hazards listed in the contract (Spec 029 and 031 safeguards: attributes, merge drivers, `core.hooksPath`, `core.fsmonitor`, `include` configuration).
- **FR-015** (the check covers only the existing moved commits; a by-hand `git rebase` sets the committer of the rewritten commits from the rebaser's own configuration or environment, which the plan cannot predict, so `pass` is no statement about the commits that a rebase will create, and the owner still runs the AGENTS.md check `git log --format='%an <%ae> | %cn <%ce>' origin/main..HEAD` before any push; limitation string `rewrite-committer-not-predicted`: reserved, not emitted until task T024 lands): The identity check uses expected identities only from `--identity "NAME <EMAIL>"` (repeatable, source `flag`); neither the environment nor any configuration file supplies them, because a placeholder identity leaked into the environment would otherwise be "expected". Without `--identity` the check is `unknown`, never `pass`. stw never writes configuration. The predicted replay commits keep the original author (name, email and date, as a real rebase does, so a clean replay pairs `=` in `git range-diff`) and use a fixed constant committer identity and committer date given through the environment, so no repository-local or global `user.*` is read; they exist only in the quarantine and are never pushed. A repository-local `user.name` or `user.email` is reported as a hazard diagnostic (`local-identity-config`), not refused.
- **FR-016**: The plan states the semantic-break limitation of the motivating scenario in the result (`limitations`) and the overlap indicator; it never reports a clean replay as "safe to push".
- **FR-017**: Results use the new `restack-plan` schema (`schemas/v1/restack-plan.schema.json`, `schema_version: 1`, `kind: "restack-plan"`): versioned, stable and additive; existing published contracts and fixtures do not change. Exit status: 0 for a complete plan (including predicted conflicts or differing evidence), 1 for refusals and errors, 2 for syntax errors, 3 for `incomplete`.
- **FR-018**: Bounds: at most 200 moved commits, 2,000 paths per list, 1 MiB result, 60 s total; exceeding any yields `incomplete` with the bound named, never a truncated success (FR-009).

#### Proposed local apply and recovery (2026-10-25; unimplemented)

These requirements do not change hidden v1 behavior or its frozen schema. They
require the reviewed [apply](contracts/restack-apply.md),
[recovery](contracts/recovery.md) and [jj](contracts/jj-restack.md) contracts and
resolution of the [owner decisions](#needs-owner-decision) before implementation.
FR-020–FR-024 describe the first Git apply capability; FR-027 requires separate
native jj semantics rather than Git ref/range-diff equivalence.

- **FR-019**: Default to no mutation and no prompt. A stored, immutable local apply plan must name exactly one owned topic branch (or selected native jj bookmark under its distinct capability), repository/backend, old parent, old head, new parent, declared effects and expected state. Applying requires that plan id and explicit local authorization; a v1 observation or Spec 012 advisory step cannot authorize it.
- **FR-020**: Replay exactly `old_parent..old_head` onto the full `new_parent` identity, oldest first, with the `--onto` semantics of the motivating example. Preserve D7: private quarantine/plumbing and an expected-old-value ref update; never execute porcelain `rebase`, automatic descendant updates or a fallback replay strategy. Refuse merges, empty/redundant moved commits and unavailable parent-integration proof in this slice.
- **FR-021**: Before effects and again before the target ref update, revalidate repository/common-directory identity, backend ownership, task attribution, exact selected ref and parent/head identities, protections, linked worktree registrations, operation state and the recorded configuration/hook policy. Refuse missing, foreign, shared or ambiguous ownership, a protected/default branch or a branch held by any worktree. Caller/owner worktrees, HEADs, indexes, files and config stay unchanged.
- **FR-022**: Require complete post-replay evidence over the actual candidate commits: every range-diff row `=`, one paired commit per moved commit, every own diff equal, original author identities/dates preserved, and actual author AND committer identities matching exactly the owner identity from a trusted policy source (OQ-2/OQ-3). A caller cannot widen that identity through repeatable flags. An old-plan identity pass, fixed prediction committer, aggregate patch id or changed/unknown row cannot satisfy this gate. Expected actual identities are not inferred from repository/global config or inherited identity environment. Before push, 016 must check every actual author/committer in `<observed remote default branch>..<new head>`, including still-open parent commits, and report `git config --show-origin --get-regexp '^user\.'` origins/values as bounded evidence with no foreign `user.name`/`user.email`; configuration is never written. The historical v1 D9 flag-only prediction check remains unchanged.
- **FR-023**: Reuse Spec 032 journal/recovery conventions through a proposed typed restack integration, with an immutable plan/run/action binding and create-only recovery root for the old head before the target ref move. Durably record the known candidate SHA before compare-and-swap, then actual outcome; refuse journal, durability or recovery-root failure. No second unrelated recovery subsystem is introduced.
- **FR-024**: Report each completed, refused, failed or unknown action after conflict, race, cancellation or crash. Retry reconciles the exact recorded old/new ref values and never blindly replays or overwrites later work. Restore is a separately authorized plan with a CAS back to the retained old identity only when the target is still at the recorded new identity and all ownership/worktree guards pass; otherwise retain and report `changed-since` or `journal-incomplete`.
- **FR-025**: Hand Spec 016 only actual old/new identities, post-replay evidence, journal/recovery pointers and an explicit review-gate input bound to the EXACT new head SHA. The receipt records `change_class` (`docs-only` or `code`, computed from actual changed paths; unknown becomes `code`) and `pure_rebase` (all range-diff rows `=` with the same commit count). Spec 016 follows AGENTS.md: docs/spec-only rewrites need green local checks and, for a pure rebase, range-diff equality without pre-push review; code rewrites require independent review of the exact new SHA before push, with middle-tier review allowed for a pure rebase with equal range-diff. Ready always requires independent exact-head CLEAR and green required checks. Push-time owner identity covers the full observed remote-default-to-new-head range, including open-parent commits, against the trusted owner alone, with user-configuration origins reported and no caller-widened allowlist. Current attribution and full observed remote SHA are separately validated under each remote plan/action authorization. Local apply neither obtains nor grants remote permission; a head change invalidates review evidence.
- **FR-026**: Keep push with `--force-with-lease=<full ref>:<observed full remote SHA>` and PR linking/retargeting solely in separately authorized Spec 016. Print owner resync alternatives as bounded inert DATA only, marking `reset --hard` destructive; stw never runs an owner reset, switch or resync. Agent merge/default-branch push/tag/release/settings changes remain forbidden; Spec 043 landing is a separate human invocation.
- **FR-027**: Specify native jj as a separate capability with commit/change/operation ids, explicit bookmark/descendant scope, immutable/shared/conflicted-state checks and operation-bound stale validation. No Git fallback in colocated or standalone jj. Enable apply only with measured native operation/bookmark semantics, isolation and minimum/current conformance; otherwise refuse `capability-unavailable` with no mutation.
- **FR-028**: Keep any Git conflict or failed equality check in owned disposable replay state, never create an interrupted operation in an original worktree, and never resolve, continue/abort, auto-stash or silently discard work. jj stored conflicts remain native conflicts; this clean-only apply slice refuses them. Keep-both classification follows D12's explicit allowlist, default empty, and never resolves a conflict.
- **FR-029**: Keep human/JSON results equivalent and bounded. Proposed apply/recovery uses 0 for complete selected success (including proven already-applied/restored), 1 for refusal, incomplete, partial, cancellation or operational failure and 2 for usage errors. Preserve actual v1 plan exit 3 for incomplete and all schema bytes; a future plan 0/1/2 protocol requires explicit version/migration acceptance, not a silent change.
- **FR-030**: Bind optional Spec 012 observations and Spec 014 isolated preflight/replay evidence by exact inputs, backend, semantics, completeness and freshness; resample before apply. Preserve the D6 direct-input path without depending on either implementation. Native-stack-managed or unknown remote management never licenses racing GitHub's rewrite; require explicit provenance that this is a local/plain-chain action or refuse until observed separately.
- **FR-031**: Treat plan/journal records, repository instructions, filenames and diagnostics as untrusted bounded data; no shell evaluation, config-driven helper, verify command, hook or network action. Reject symlink/replaced state paths, malformed refs/ids, unknown record types/versions, duplicate ids and forged cross-repository bindings. Implement only declared fixed native argument vectors; tests plant executable and credential sentinels.
- **FR-032**: Bound each proposed local plan/apply/recovery invocation to one branch/bookmark action, 200 moved commits, 2,000 paths per list, 1 MiB rendered result and 60 seconds total including owned teardown/result preparation. On exhaustion stop before the next effect, retain durable partial evidence and report the bound. Never publish a truncated success; no sleeps for concurrency tests (Spec 040).
- **FR-033**: Preserve committed-history recovery only within retained roots and journal evidence. It cannot restore untracked/ignored files, unsaved buffers, hooks/config side effects, arbitrary worktree files, expired roots or any remote effect; no automatic GC or recovery retention deletion in this slice. Integrate checkpoint/restore/selective undo through Spec 013 instead of duplicating it.
- **FR-034**: Require reviewed spec/plan/tasks, failing behavior fixtures before implementation, effect sentinels and native minimum/current conformance for each enabled capability. New machine contracts remain Markdown proposals, UNPUBLISHED until implemented and conformance-accepted; no dependency, schema-file or published-contract change is authorized by this documentation amendment.

### Key Entities

- Restack plan: native source scope, destination, affected descendants and declared effects.
- Rewrite outcome: old/new identities, conflicts and recovery evidence.
- Evidence bundle (first slice): the read-only observations of FR-013, see [data model](data-model.md) and [contract](contracts/restack-plan.md).

## Known limits

- Semantic breaks after a clean replay are not detected; only the overlap indicator is reported.
- The prediction can differ from a real rebase (renames, rerere, whitespace options); apply recomputes it.
- Exotic traps (author-date edge cases, unusual attributes beyond the hazard gate, other Git versions than the CI lanes, pack-threshold packing in the quarantine) are not covered by fixtures; the plan then refuses or reports `unknown` where it can detect them, and otherwise the limit stands.
- Mutation controls are required for the safety-critical tasks only (refusals, identity, quarantine, texts, outcome table); other tasks rely on their tests.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

- **SC-005**: Every selected Git apply fixture moves only its named owned ref from exact old head to a candidate with all `=` rows and matching actual author/committer identities; every negative gate fixture leaves that ref unchanged.
- **SC-006**: Crash fixtures at every journal/ref boundary reconcile to an explicit old/new/changed-since outcome; separately authorized restore never overwrites intervening work and retains incomplete recovery roots.
- **SC-007**: Every attempted remote handoff with a missing class-dependent pre-push gate, stale/exact-other-SHA review where required, missing authorization or changed observed remote head is refused by the consuming Spec 016 contract; docs-only pure rebase succeeds with green local checks plus equal range-diff without pre-push review, while code requires exact-new-SHA review (middle tier allowed for pure rebase); all 015 effect recorders show zero remote writes and zero owner-worktree resets.
- **SC-008**: Every enabled jj capability passes its own minimum/current native identity, operation/bookmark, conflict and effects fixtures; until then capability refusals preserve native state and never run Git writes.

## Needs owner decision

Recommendations below are proposals, not settled answers or permission to implement.
The 2026-10-21 D4–D9/D12/D13 decisions above remain settled.

| Id | Choice still needed | Recommendation and blocked work |
| --- | --- | --- |
| OQ-1 | Brief convention 0/1/2 conflicts with implemented v1 incomplete exit 3. Which explicit version selector and migration policy? | Keep hidden v1 unchanged; use a separately opted-in future plan/apply protocol with incomplete exit 1 and compatibility fixtures. `--contract 2` below is a proposed selector only. Blocks future protocol implementation/publication, not historical evidence. |
| OQ-2 | Which trusted ownership evidence binds a topic ref to this task, and which independent review/check issuer, format and freshness policy may 016 accept? | Bind private complete task attribution plus explicit ref association, repository identity and native registration; names/owner labels alone are insufficient. Fail closed when absent. Where the agent pre-push policy requires review, bind independent CLEAR to the exact new SHA and an allowed issuer, preserving the FR-025 docs-only exemption and middle-tier pure-rebase allowance. Spec 043 `--reviewed SHA` is explicitly the human owner's statement under its separate landing policy; this agent receipt policy neither overrides nor strengthens that human contract. Ready still needs independent exact-head CLEAR and green checks for every change class. Blocks trusted input implementation and 016 remote consumption. |
| OQ-3 | Which actual replay committer identity/date policy should persist commits (D9 fixes the prediction committer only)? | Require explicit `--committer` equal to the trusted owner identity (source still an owner decision), preserve original author/date and capture a committer timestamp once in the immutable plan. Proposed actual-apply `--identity` flags are assertions checked against that owner only, never an extendable allowlist; report local/global config origins as evidence without treating them as caller-granted authority. Refuse missing or foreign identity; never infer expected actual identity from repository config/inherited environment. Blocks actual candidate creation; does not change v1 quarantine identity. |
| OQ-4 | Which jj versions/native interfaces prove exclusive operation expectation, exact bookmark/descendant effects and recoverable operation retention? | Keep native plan/apply disabled with `capability-unavailable` until a scratch capability spike records all effects and failure paths. No Git fallback or claimed Git-style CAS. Blocks jj enablement. |
| OQ-5 | How should typed restack recovery participate in the Spec 032 ten-completed-run retention and Spec 013 restore index? | Reuse durability/ref conventions through a reviewed neutral seam; protect incomplete/partial restack runs and do no automatic pruning in the first apply slice. Decide cross-operation retention before enabling any deletion. Blocks retention deletion, not a retained-root apply design. |
| OQ-6 | May apply target a branch checked out in its own task worktree? | First apply slice refuses all occupied branches, including the caller's; no worktree update/checkout permission. A later worktree-aware capability needs its own atomicity/recovery design and authorization. Blocks widening the narrow unoccupied-ref capability. |
| OQ-7 | The brief's literal no-network read-only phrase conflicts with settled Spec 012 explicit `--pull-requests` reads. | Preserve 012 offline default and explicit bounded/cached online evidence; 015 stays offline and consumes supplied evidence only. Clarify wording without deleting the owner-approved opt-in contract. No new network effect here. |

## Assumptions

- Depends on Spec 010. The first slice no longer depends on Spec 012 or 014 (D6, 2026-10-21): its explicit input is old parent, new parent and branch. Later slices still reuse Spec 012 stack observation (revalidating its `expected` ids) and Spec 014 conflict diagnosis where they land; optional checkpoints reuse Spec 013.
- Spec 012 (reworked 2026-10-17) supplies the stack observation and an advisory, print-only plan; this spec owns executing a local rewrite and its recovery evidence and must revalidate the Spec 012 `expected` ids. A rewrite that GitHub performs on a native stack is not restacked here.
- The first-slice plan follows the Spec 029/031 proof ideas (patch id, range-diff pairing) and native Git runner discipline with its own small helpers in `internal/restack` (no extraction from `internal/workspace`, T004 skipped); it does not reuse tidy plans or journals. Proposed apply separately reuses Spec 032 durability/recovery conventions; no shared API is implemented by this amendment.
- No automatic restacking of every stack, remote history rewrite or implicit base synchronization.
- This draft does not amend the constitution or authorize execution. Owner review, plan, tasks and consistency analysis precede implementation; later mutation/forge scope must be reconciled with constitutional constraints during planning.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.
