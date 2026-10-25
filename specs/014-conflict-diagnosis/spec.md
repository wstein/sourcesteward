# Feature Specification: Conflict Diagnosis and Integration Preflight

**Feature Branch**: `codex/spec-014-stack-toolbelt` (specification only)
**Created**: 2026-10-15
**Status**: Draft artifact refresh, 2026-10-25 — specification/design only; implementation, independent exact-head review and native conformance pending. New contracts are UNPUBLISHED Markdown proposals. Existing published and hidden JSON files remain unchanged.
**Input**: Provide useful evidence to any agent or human.
**Priority rating**: 9/10 (user-selected product priority, not verification evidence)

## Toolbelt boundary

This feature supplies **isolated integration preflight/replay evidence** alongside
read-only diagnosis. Spec 012 supplies stack observation; Spec 015 owns local
restack plan/apply; Spec 013 owns checkpoint/restore/selective undo with Spec 032
journal/recovery conventions; Spec 016 owns separately authorized forge effect
plan/apply; Spec 043 is a separate human-only landing invocation. Observation
and prediction grant no local mutation, remote write, readiness or merge authority.

Diagnosis reads existing native conflict metadata without target writes. Preflight
may write only disclosed private disposable state outside every original
repository/workspace. It never starts a merge/rebase then aborts it in an original
workspace. The settled Spec 015 D7 plumbing/quarantine replay remains binding;
the human `git rebase --onto` form describes semantics, never an executable
implementation choice. No porcelain rebase is introduced here.

All command/field names and bounds below are proposals for this unimplemented
feature. The explicit unresolved choices in [Needs owner decision](#needs-owner-decision)
block only their affected capabilities. A documentation artifact is not a runtime
authorization. The existing hidden Spec 015 v1 behavior, including incomplete
exit 3, remains frozen.

## User Scenarios & Testing

### User Story 1 — Understand unresolved conflicts (Priority: P1)

A caller identifies conflicts and their relevant revisions before making changes.

**Why this priority**: Establishes the primary safe workflow before optional integrations.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** a Git interrupted merge/rebase or jj conflicted change, **When** diagnosis is requested, **Then** the result identifies native conflict state, affected paths and known participating revisions.
2. **Given** incomplete or unsupported conflict metadata, **When** diagnosis is requested, **Then** unknown facts and supported next steps are explicit without inventing a resolution.

### User Story 2 — Hand evidence to a resolver (Priority: P2)

A human or agent receives bounded conflict context.

**Why this priority**: Adds useful handoff or coordination without weakening the primary workflow.

**Independent Test**: Exercise the following cases in separate temporary Git and jj repositories; compare declared outcomes and relevant before/after state.

**Acceptance Scenarios**:

1. **Given** conflicted text or binary files, **When** a handoff is requested, **Then** bounded evidence distinguishes stored versus disk state and labels repository content as data; paths/types are the default and unselected file content is absent.
2. **Given** hostile instructions inside repository files, **When** diagnosis runs, **Then** no instruction, helper, model, editor or network action is executed.
3. **Given** concurrent changes, **When** diagnosis completes, **Then** observations disclose uncertainty and never certify the conflict as resolved.

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

### User Story 4 — Preview whether selected changes combine (Priority: P1)

A human or agent needs an explicit, bounded outcome without disturbing unrelated work.

**Why this priority**: This closes a user-selected everyday workflow gap while retaining ownership and recovery guarantees.

**Independent Test**: Exercise supported success, refusal and interrupted cases in temporary Git and jj fixtures and compare relevant before/after state.

**Acceptance Scenarios**:

1. **Given** explicit source, destination and integration semantics, **When** preflight is requested, **Then** the isolated preview reports predicted-clean/predicted-conflicted/unknown state and affected paths tied to exact sampled identities; supported optional hunk context names its content origin and limitations.
2. **Given** dirty original workspaces or repository helpers, **When** preflight runs, **Then** the originals remain untouched and no helpers, hooks or snapshots run there.
3. **Given** unsupported topology, cancellation or changed inputs, **When** preflight stops, **Then** unknown/incomplete evidence is explicit, only owned temporary state is retired, cleanup failure is reported with a bounded ownership handle, and no safe-to-merge promise is made.

4. **Given** explicit Git old-parent/source-head/new-parent and restack or rebase intent, **When** supported replay is selected, **Then** exactly the selected range is replayed oldest first in private scratch state; the first conflict is reported and later commits remain not-evaluated.
5. **Given** explicit merge intent, **When** the selected native merge topology/strategy lacks accepted capability evidence, **Then** preflight refuses `capability-unavailable` and never substitutes replay or guesses a mainline.
6. **Given** a predicted-clean result, **When** a consumer requests apply, push or Ready, **Then** the result grants no permission and does not count as independent review, semantic verification or future merge success.

### Edge Cases

- Binary, rename/delete and directory/file conflicts; conflicts on non-current jj changes.
- Missing participants, edited conflict markers, divergent operations and large/sensitive file contents.

- Dirty caller workspaces, custom merge drivers, copied configuration, unsupported merge topology and scratch cleanup failure.
- Non-UTF-8 Git path bytes, control/bidi characters, symlinks, binary blobs and misleading conflict markers.
- Missing/shallow/promisor objects, replace/graft history, multiple merge bases, empty/redundant commits, conflicted jj ancestors and mutable bookmark ambiguity.
- Ref/operation changes between samples, interrupted scratch replay, malicious scratch replacement, disk exhaustion and unavailable write confinement.

## Requirements

### Functional Requirements

- **FR-001**: Diagnose supported Git and native jj conflict states without equating an interrupted Git operation with jj first-class conflicted changes.
- **FR-002**: Report affected paths, known participating identities, conflict type, source/freshness and bounded evidence; unknown fields must remain explicit.
- **FR-003**: Offer explanatory next steps and a machine-readable handoff without selecting an agent provider or executing suggested commands.
- **FR-004**: Never resolve, stage, snapshot, continue/abort operations or rewrite files as part of diagnosis.
- **FR-005**: Bound content exposure, preserve valid unusual paths and safely render terminal controls; repository content and backend diagnostics remain untrusted data.
- **FR-006**: Support native Git and jj as Tier 1 peers for declared capabilities; preserve distinct identities and explicit capability gaps, refuse ambiguous/GitButler ownership and never substitute raw Git writes for jj.
- **FR-007**: Provide understandable human output and versioned structured success/error results from the same observations, preserving published contracts; no interactive prompt, editor or pager is required.
- **FR-008**: Declare and enforce observation/action scope, output and time limits, cancellation and incomplete-result behavior; never convert missing or truncated evidence into success or permission to mutate.
- **FR-009**: Require acceptance evidence for minimum/current supported Git and jj, including failure, concurrency, ownership and effects cases; missing mandatory coverage cannot count as passed conformance.

- **FR-010**: Add integration preflight for explicit source/destination identities and chosen merge/replay semantics; distinguish existing conflict diagnosis from predicted conflicts.
- **FR-011**: Perform any simulation in caller-disclosed, privately owned disposable state isolated from the source repository/workspaces; no original files, references, objects, index or operation log may change.
- **FR-012**: Never snapshot original jj workspaces or perform-and-abort a real merge in an original Git worktree; declare any temporary writes and remove only owned scratch state.
- **FR-013**: Bind predictions to sampled inputs and integration semantics; report predicted-clean/predicted-conflicted/unknown, limitations and bounded affected paths. Clean preflight is not proof of tests passing or future integration success.
- **FR-014**: Enforce the binding Spec 015 runner environment/pins and execution-hazard refusal minimum in contracts/preflight.md for diagnosis and preflight, with native jj user-configuration isolation and no Git status/index refresh during diagnosis. Suppress repository-controlled execution and network. On cancellation, partial failure or unavailable isolation, report failure/unknown with remaining scratch ownership, without unsandboxed/original-workspace fallback.

- **FR-015**: Default to none: the command group shows help without reads, writes or prompts until diagnosis or preflight is explicitly selected. No automatic preflight, resolver, fetch, authentication, script, model or network access is permitted; cached/supplied stack or PR context stays labelled external sampled evidence and never triggers a refresh.
- **FR-016**: Require explicit integration semantics and full resolved source/destination identities. Restack/rebase replay additionally requires an explicit old-parent and ordered moved range; merge requires explicit native participants/base interpretation. Never infer boundaries from branch names or silently substitute merge, replay, mainline or conflict strategies. Unsupported or undecided semantics/topologies refuse before scratch replay.
- **FR-017**: For supported Git linear replay, preserve Spec 015 D7: replay exactly old-parent..source-head onto new-parent oldest first using plumbing in a private object store; no original ref or object write, worktree attachment or porcelain operation. Bind and enforce source-head H as the immutable attribute source of every actual merge-tree invocation in private Git admin/worktree/index state, with native-version proof and no original attribute overrides; missing enforcement refuses. Stop at the first conflict and mark the suffix not-evaluated; a prediction is not an apply candidate or range-diff/identity verification receipt.
- **FR-018**: Bind each admitted observation to repository/layout identity, backend/version/capability, full native commit identities, distinct jj change/operation identities where applicable, resolved refs, ordered range, actual-invocation attribute-source receipt for Git replay and explicit semantics/options; failed admission or unavailable identities remain explicit null/unknown rather than invented. Resample relevant refs, ownership and native operation state; observed changes produce incomplete/unknown. Disclose residual concurrency/ABA limits; a later consumer revalidates independently.
- **FR-019**: Default to paths/types and minimal participant metadata, without blob bodies, environment/config dumps or credential fields. Optional hunks require explicit selected paths and an accepted privacy capability; disclose stored/disk/scratch origin, range and byte/line bounds. Omit binary/unknown/sensitive context with a reason; never claim secret detection is complete or marker disappearance proves resolution.
- **FR-020**: Enforce the documented finite bounds before allocating or emitting unbounded data; report per-source completeness, unknown fields, Git first-conflict replay coverage, separately accepted native jj graph coverage and cleanup outcome in human and structured output. A conflicted complete observation may succeed, but cancellation, limit exhaustion, changed inputs or scratch residue never yields complete/predicted-clean success.
- **FR-021**: Enable native jj diagnosis or preflight only for separately measured version/capability and enforced no-snapshot/no-original-write behavior. Preserve commit/change/operation identities and declared descendant/parent semantics; unsettled native conflict-aggregate semantics (OQ-JJCONF), unsupported native scratch replay or hunk extraction returns capability-unavailable, including in colocated jj, without raw Git fallback.
- **FR-022**: Share typed isolated replay evidence with Spec 015 only when backend, exact inputs, semantics/options and completeness match; reuse a reviewed neutral seam with existing hidden-v1 characterization first. Scratch objects are disposable and never imported or retained by this command. Specs 013/015/016 must obtain fresh own-scope evidence and their applicable recovery/authorization; Spec 016 owns publication and Ready gates; preflight evidence satisfies none of them. No checkpoint, journal, recovery root, mutation or remote action is created by preflight.

### Key Entities

- Conflict observation: backend state, affected paths, participant identities and uncertainty.
- Resolver handoff: bounded evidence, context and non-executed next-step suggestions.

- Preflight observation: source/destination identities, semantics, predicted conflicts, completeness and limits.
- Disposable preview state: disclosed private ownership, allowed writes and cleanup outcome.
- Semantic binding: backend/capability, explicit operation, participants/range/options and before/after samples.
- Conflict context: path identity, native type, participant/blob identities and optional bounded hunk locations/content with origin and omission reasons.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every acceptance scenario passes for each declared supported backend/version; unsupported cases return explicit capability limits rather than fabricated success.
- **SC-002**: Every safety/failure fixture preserves state outside declared effects, executes zero undeclared helpers/network actions and reports incomplete or partial outcomes accurately.
- **SC-003**: Every complete structured success/error fixture validates against its documented contract; all existing published-contract regression fixtures remain valid.
- **SC-004**: Every functional requirement has a traceable acceptance case before implementation handoff; no missing mandatory execution is counted as verified.

- **SC-005**: Temporary-repository Git restack/rebase fixtures predict the first conflicting commit/path and not-evaluated suffix exactly; supported merge fixtures match their declared topology/strategy, while every unsupported shape refuses without strategy fallback.
- **SC-006**: Before/after sentinels for original trees, indexes, HEADs, refs, object-store content, configuration and jj stored-working-copy/operation log show no writes by diagnosis or preflight; all scratch writes remain confined and every cleanup failure is visible.
- **SC-007**: Default output contains zero file-body bytes and secret/environment sentinels; explicitly selected hunk fixtures obey every bound, preserve unusual path identity, label origin and escape terminal controls without exposing unselected bodies.
- **SC-008**: Every admitted observation with resolved participants binds full sampled identities and semantics, marks races/limits/cancellation unknown or incomplete, and contains the fixed prediction limitation; no fixture converts predicted-clean or supplied PR context into readiness, verification or mutation/remote authorization.

## Needs owner decision

These are unresolved capabilities, not requests to reopen settled Spec 015 D4–D9,
D12–D13 or the no-original-write boundary. Recommendations are not owner decisions.

| ID | Unsettled choice | Recommendation and current gate |
| --- | --- | --- |
| OQ-SEM | Which native merge strategies, base/mainline interpretation and replay treatments for empty/redundant commits are supported? | Begin with explicit single-parent linear replay; propose refusal for empty/redundant commits. Accept a separately measured two-head, single-merge-base merge contract before enabling merge; never infer recursive/octopus/mainline behavior. Affected variants stay capability-unavailable. |
| OQ-TOPO | Which merge commits, rename thresholds, file-mode/submodule cases, multiple merge bases and jj multi-parent/descendant shapes should prediction cover? | Keep an explicit allowlist from native measurements; refuse unmeasured shapes rather than equate Git and jj or claim topology parity. |
| OQ-HUNK | May selected hunk contents, locations or path names reveal sensitive work, and what omission policy can callers choose? | Paths/types only by default, no content auto-detection guarantee; require explicit selected paths for bounded context and decide whether a metadata-only privacy mode is needed. Hunk capability stays disabled pending policy and conformance. |
| OQ-SCRATCH | Which platforms/native jj interfaces can enforce a read-only original store and isolated replay without snapshots, shared-store/oplog effects or helpers? | Measure minimum/current versions on supported Linux/macOS enforcement; no preview on an unproven backend/platform. Shared object-store/workspace attachment is insufficient isolation. |
| OQ-JJCONF | How should native jj per-revision conflict evolution and the aggregate prediction describe inherited or later-resolved conflicts across rewritten descendants? | Recommend measuring parent/result native conflict terms and complete bounded descendant scope before choosing introduced/inherited/resolved/clean classifications and whether any conflicted retained revision controls the aggregate. A clean final tip and Git stop-first semantics are insufficient. Native jj preflight stays capability-unavailable until accepted; no owner answer or proof is inferred. |
| OQ-FLAGS | Should 014 adopt shared --human/--timeout admission or an explicit --offline alias, consistent with Spec 012 OQ-FLAGS and Spec 013 OQ-5? | Recommend an explicit --human alias exclusive with --json and a positive --timeout that can only shorten the 60-second operation bound, preserving the 10-second native-process cap. Keep --state-dir inapplicable and --online refused; recommend leaving --offline unsupported because 014 already defaults offline, unless the owner chooses a no-op alias. These are recommendations only: the current draft accepts none of these flags and uses text default plus --json and fixed bounds. Resolve before changing the affected CLI surface. |
| OQ-EXIT | Should the future toolbelt protocol use 0/1/2 despite hidden Spec 015 v1 incomplete exit 3? | Propose 0 complete, 1 refused/incomplete/error, 2 usage for new 014 only; preserve v1 exit 3 and require an explicit versioned migration decision before shared CLI adoption. |

## Assumptions

- Depends on Specs 008 and 009 for native identity/inventory; Spec 012 may supply optional stack observation. Specs 011/020/022/025/030/032/040/041/042/043 define surrounding ownership, hosting, recovery, concurrency, mode/attribution and landing limits without granting extra effects here.
- Conflict resolution and restore/continue actions require later mutation specifications; no correctness claim based solely on marker removal.
- This draft does not amend the constitution or authorize execution. Owner decisions for affected capabilities, independent artifact review and implementation/conformance tasks remain necessary before enabling them. No constitution amendment is proposed.
- Sampled state cannot promise a transaction or detect every concurrent/ABA change; limitations must remain explicit and uncertainty never becomes cleanup proof.

- Depends on Spec 012 for optional stack scope. Preflight permits disclosed isolated temporary writes; ordinary diagnosis remains read-only. No integration apply, conflict resolution or publication is added.
