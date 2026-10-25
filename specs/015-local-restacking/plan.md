# Implementation Plan: Local restack evidence and proposed apply

**Branch**: `docs/015-restack-plan-first-slice` (specification); implementation branches follow the work packages | **Date**: 2026-10-21 | **Spec**: [spec.md](spec.md)

## Amendment boundary (2026-10-25)

Historical sections below retain the 2026-10-21 first-slice design and measured
experiment. Hidden plan source exists in `internal/restack` and
`internal/cli/restack.go`; the v1 schema exists but remains UNPUBLISHED and frozen
by digest. T001 is historical design CLEAR, T004 deliberately skipped, and
T021/T023/T024 remain open. The new work packages at the end are proposals only;
no implementation, schema-file, dependency or runtime action is added here.

## Summary

Add the read-only `stw restack plan` for native Git: one operation in a new `internal/restack` package that resolves an explicit branch, old parent and new parent, replays the moved range with `merge-tree` plumbing in a private object quarantine, and returns one typed `restack-plan` result (text or JSON) with the evidence bundle of FR-013, the effects disclosure and the inert lease push and resync texts. It follows the Spec 029/031 proof ideas (patch id, range-diff pairing) with its own helpers and the isolated native runner discipline. No apply, no jj, no push, no network, no verify commands in this slice.

## Technical Context

**Language/Version**: Go 1.27.2; existing supported minimum and current Git; jj and GitButler repositories are refused (`unsupported-backend`), the stated capability gap of D4.
**Dependencies**: Spec 010 contract vocabulary only; no Spec 012/014 code (D6).
**Testing**: real temporary Git repositories, test first; fixtures built with `git` plumbing under a throwaway identity passed by environment, never written to configuration. Mutation controls are listed in [tasks.md](tasks.md).
**Constraints**: offline; zero repository writes (sentinel over refs, config, index, worktree and object store); bounds in FR-018; cancellation honoured; no hooks, helpers, pager or prompt.

## Constitution and policy check

| Gate | Result |
| --- | --- |
| Git/jj Tier 1 peers | Git only with a stated capability gap and an explicit refusal for jj (D4); jj is Work package WP8 |
| Read-only first, inspectable plans | Yes (D5); apply is a later slice with its own plan and analysis |
| No undeclared executable/network | Fixed Git plumbing only; `internal/restack` joins `NETWORK_FREE_PACKAGES` in `scripts/check-architecture.py`; process starts only in reviewed files |
| Published contracts preserved | New schema file only; allowlist/copy tests extended; no existing byte changes |
| No Git configuration write | Expected identity from the `--identity` flag only; fixed constant replay identity; nothing written (D9, FR-015) |
| Push/force | Text only, `--force-with-lease=ref:sha` form, never run (D8) |

## Design

1. **Resolve** (`internal/restack/input.go`): validate syntax (FR-011), resolve through the isolated runner to commit ids, observe `old_head`, compute both merge bases, collect the moved range with `rev-list --topo-order --reverse`; refuse per FR-014 before any evidence work.
2. **Hazards** (`hazards.go`): apply the Spec 029/031 repository, config, attribute and hook safeguards (re-implemented by name in `hazards.go`); report only names. Any hazard on a path the replay touches refuses (`repository-hazard`), because `merge-tree` may run merge drivers.
3. **Replay** (`replay.go`): for each moved commit, `merge-tree --write-tree --merge-base=<commit^> <tip> <commit>`; on a clean result `commit-tree` with the original author (`GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL` and `GIT_AUTHOR_DATE=@<unix> <zone>` of the moved commit, so `git range-diff` pairs it `=`) and a fixed constant committer given only through the environment (`GIT_COMMITTER_NAME=stw-restack-plan`, `GIT_COMMITTER_EMAIL=restack-plan@invalid`, `GIT_COMMITTER_DATE=1970-01-01T00:00:00Z`; process `HOME`, global and system configuration neutralized), so no repository-local or global `user.*` is ever read; the replay commits live only in the quarantine and are never pushed, so their committer identity is never evidence about the branch. The quarantine is a private object directory (`GIT_OBJECT_DIRECTORY` private, repository objects as alternates). The first conflict stops replay and is reported with its paths and kinds (no allowlist class in this slice); later commits are `unpaired`. The quarantine is removed on every path and a sentinel test compares the object store.
4. **Evidence** (`evidence.go`): `git range-diff` and `git cherry` through the runner; tree equivalence of touched paths for `+` rows (own small helpers; the tidy patch-id code was not extracted, T004 skipped); own-diff patch-id equality; identity check; overlap indicator from `old_parent..new_parent` paths against moved paths.
5. **Effects and texts** (`effects.go`, `texts.go`): affected refs and worktrees from `for-each-ref --contains` and `worktree list --porcelain`; hook names by file presence only; lease and resync texts with strict ref-name validation and quoting.
6. **Result and CLI** (`result.go`, `internal/cli/restack.go`): one typed result, human rendering from it, `schemas/v1/restack-plan.schema.json`, exit mapping. The CLI imports only the reviewed `restack` operation (architecture check updated).

Implementation note (2026-10-21): the helpers were not extracted; `internal/restack` has its own small proof helpers behind its own reviewed runner and `internal/workspace` is unchanged (T004). Original decision, not taken: extract the pure proof helpers of the tidy integration run into a neutral package or exported helpers with unchanged characterization tests first (refactor commit, no behavior change); do not make `restack` import tidy plan types.

## Work packages and effort

S about 0.5 author day, M about 1, L about 2 to 3 (each includes the repo's own tests, `make check` and one independent review; review rounds and CI waits are extra). Numbers are those of the 2026-10-21 estimate where valid.

| WP | Content | Effort | Depends on |
| --- | --- | --- | --- |
| WP0 | This design: plan, tasks, contracts, data model, independent design review, analysis (estimate 015-0) | L (done as draft in this PR; review rounds extra) | owner decisions D4-D13 |
| WP1 | Minimal explicit input and resolver, refusals, bounds (estimate 015-1 plus refusals of 015-3) | M | WP0 |
| WP2 | Evidence library: own proof helpers (no extraction from `internal/workspace`, T004 skipped), range-diff pairing, cherry, tree equivalence, own-diff equality, identity check (015-2) | M | WP1 |
| WP3 | Replay and conflict prediction in quarantine, effects, hazards, texts (015-3 remainder) | L | WP2 |
| WP4 | Result, schema, CLI, human output, architecture check | M | WP3 |
| WP5 | Conformance: minimum/current Git, effect sentinels, mutation controls, quickstart evidence (015-9, plan part) | M | WP4 |
| WP6 | Keep-both ledger classification from stw configuration (015-4), including the `--keep-both-config` flag and its tasks; the plan slice has no such flag and classifies nothing | M | WP3 |
| WP7 | `restack apply` Git: recovery refs and journal, compare-and-swap ref update, revalidation, stop on conflict (015-5); own spec amendment and review | L | WP5; Spec 010 |
| WP8 | jj peer with change/commit identity mapping (015-6) | L | WP7 |
| WP9 | Push with lease: Spec 016 L2 amendment (015-7), outside Spec 015 | M | Spec 016 FR-006, Spec 024 |
| WP10 | Declared verify commands in a sandbox (015-8) | M | owner decision, configuration contract |

First slice (plan only, WP1 to WP5, without the keep-both flag): about 4 to 5 author days with one editing author, sequential on `internal/restack` and the shared CLI root; WP6 may follow in parallel. Together with WP0 the estimate's slice 2 of 6 to 8 days stands. Git-only plan plus apply (WP1 to WP7): about 8 to 10 days; with jj (WP8) 14 to 16 days.

## Quarantine evidence (reviewer experiment, not a guarantee)

An independent reviewer measured with Git 2.56.0 in a scratch repository, global and system configuration set to `/dev/null`: with `GIT_OBJECT_DIRECTORY=<quarantine>` and `GIT_ALTERNATE_OBJECT_DIRECTORIES=<real objects>`, `merge-tree --write-tree` (clean and conflicting), `commit-tree`, `range-diff`, `cherry` and `patch-id` left the content of the real `.git` identical (sha256 listing); Git may freshen the mtime of an already-existing object when it writes an identical one, so the `find -newer` observation is not a claim about mtimes. A plain `git merge-tree --write-tree` without the quarantine writes objects into the real store (9 to 11 objects). Not verified on other Git versions or for pack-threshold triggered packing. It matches the runner environment of `internal/workspace/tidy_v3_integration_run.go` (`gc.auto=0`, `core.hooksPath=/dev/null`, `GIT_OPTIONAL_LOCKS=0`). The same experiment becomes a test on the CI Git versions (T012). It also showed that `commit-tree` reads a repository-local `user.*` when the environment has no identity, which is why the replay identity is fixed (FR-015).

## Known limits

- Semantic breaks after a clean replay are not detected; only the overlap indicator is reported.
- The prediction can differ from a real rebase (renames, rerere, whitespace options); apply recomputes it.
- Exotic traps (author-date edge cases, unusual attributes beyond the hazard gate, other Git versions than the CI lanes, pack-threshold packing in the quarantine) are not covered by fixtures; the plan then refuses or reports `unknown` where it can detect them, and otherwise the limit stands.
- Mutation controls are required for the safety-critical tasks only (refusals, identity, quarantine, texts, outcome table); other tasks rely on their tests.

## Risks

| Risk | Handling |
| --- | --- |
| `merge-tree` runs a configured merge driver | hazard gate refuses before any call, as Spec 031 does |
| Quarantine objects leak into the repository | private object directory, alternates only for reads, sentinel test, removal on every path |
| Predicted tree differs from a real rebase (renames, whitespace, rerere) | stated in `limitations` (`prediction-is-not-apply`); apply (WP7) recomputes and revalidates |
| False confidence from `evidence-clean` | notice and `limitations` in every result; overlap indicator; verify commands are a later slice |
| Identity unknown in a sandbox | check is `unknown`, never `pass`; `--identity` is required for a pass; the environment never counts |
| Shared files `cli/root.go`, schema allowlists with other authors | rebase before CLI wiring (WP4); keep each commit to one logical change |

## Open questions for review

1. (Resolved by the review) No `user.*` configuration feeds the expected identity; repository-local `user.*` is reported as a hazard.
2. (Resolved by the review) `--keep-both-config` moves to WP6; the first slice has no configuration input.
3. Whether `range-diff` should also be available as post-check text after a by-hand rebase (printed in quickstart only).


## Proposed next slices: design through tasks, not implementation authorization

[Research](research.md) records source facts, decisions and unresolved choices.
[Apply](contracts/restack-apply.md), [recovery](contracts/recovery.md) and
[jj](contracts/jj-restack.md) are UNPUBLISHED Markdown contracts. Proposed fields,
selectors and refusals must not be added to frozen v1 by documentation alone.

### Technical context and gates

Stay on the current Go/Cobra/native runner architecture and standard library;
no new dependency is proposed. Keep decisions/evidence derivation pure and
place filesystem/process/ref effects at a small reviewed operation boundary.
If concurrency is needed, follow Spec 040 `internal/fanout` and deterministic
indexed results; a one-branch apply needs no parallel mutation. No repository
script, hook, model, config-selected verify command or network is permitted.
The installed Spec Kit plan and task setup are used with explicit existing
feature selection and no persistence; existing templates/history are not reset.

| Constitution gate | Proposed disposition |
| --- | --- |
| I shared contract/noninteractive | One typed human/JSON result, no prompt without an explicitly selected step; bounded future 0/1/2 proposal, unchanged legacy exit3 |
| II evidence and expected state | Immutable one-action plan, exact old/new parent/head, post-replay equality and identity, CAS; missing proof refuses |
| III ownership and recovery | Own unoccupied topic refs only as an OQ-6 proposal; private task binding and pre-action durable journal/root; reverse action separately authorized |
| IV test first and truthful evidence | New work starts only after design decisions/review; named red tests, native lanes, sentinels and mutations; no product tests run in this amendment |
| V native peers | Git first under D4; jj enabled only after native operation/bookmark proof, no raw Git writes or false parity |
| Publication boundary | D8: every remote step is a 016 plan/action with exact actual-new-SHA CLEAR and fresh checks; 043 remains the human owner's landing invocation |

Open OQ-1 to OQ-6 block the affected implementations. OQ-7 preserves 012's
explicit opt-in reads without adding network here. Recommendations are not
settled policy. This plan is ready for independent documentation review, not
blanket runtime implementation or product acceptance.

### Architecture and integration

1. Preserve `Plan` and v1 result/CLI/schema tests unchanged. A future opt-in
   plan protocol builds a stored local apply plan; an adapter may consume 012
   `expected` evidence but direct D6 inputs remain sufficient. No authentication
   or 012/014 implementation dependency enters the existing plan slice.
2. Proposed `internal/restack/apply_plan.go` validates immutable typed inputs,
   owner/task/ref association, exact native layout and effect bounds. Proposed
   `apply.go` coordinates quarantined candidate replay, evidence and the commit
   point; pure gate functions accept values and return bounded typed reasons.
3. Reuse 014 isolated integration preflight/replay semantics once accepted;
   identical backend/input/semantics/completeness is necessary for reuse and
   every mutation guard is still sampled fresh. Neither a clean 014 preflight
   nor 012 observation is an authorization token. Native-stack-managed/unknown
   evidence blocks automatic competition with GitHub; the direct-input path
   needs explicit local/plain-chain provenance.
4. D7 remains plumbing plus CAS. Candidate replay preserves original author/date,
   uses the OQ-3 explicit actual committer policy, and exposes actual new SHA.
   Verify one-to-one all `=` range-diff and actual identities before target CAS.
   Initial apply refuses occupied refs; changing a caller's own attached
   worktree is a separate future capability requiring OQ-6 resolution.
5. Extract/reuse a reviewed neutral durability seam from
   `internal/stwsync/journal.go` with unchanged characterization tests. Typed
   restack records/root namespace extend the 032 recovery architecture; no
   duplicate writer, cross-type interpretation or API availability claim.
   Sync retention stays unchanged; restack roots stay retained pending OQ-5.
   Spec 013 consumes action/native identities for checkpoint/restore/undo.
6. Expose actual new SHA plus derived `change_class`/`pure_rebase` policy input.
   Spec 016 preserves AGENTS.md: docs-only rewrite needs green local checks and
   equal range-diff for pure rebase without pre-push review; code rewrite needs
   independent exact-new-SHA review before push (middle tier for equal pure
   rebase allowed). Ready always needs independent exact-head CLEAR and green
   required checks. Spec 043 human `--reviewed` is a separate owner statement.
   The observed remote SHA belongs to 016 and is never inferred from local old
   head, upstream config or an inert v1 text. Retarget/link are separate effects.
7. Native jj begins with a capability spike. No adapter may claim Git CAS,
   automatic descendant scope safety, operation undo or snapshot suppression
   for writes until measured. Unsupported capability remains explicit.

### Dependency-ordered implementation units

Sizing: S about half an author day, M about one day, L two to three days;
these are planning estimates, not delivery promises. Each unit includes its
own tests/format/source checks and independent exact-head review when built.
Do not run those checks or builds for this Markdown-only amendment.

| Unit | Proposed tasks | Size | Prerequisites and independently reviewable result |
| --- | --- | --- | --- |
| A0 unresolved decisions | T025–T026 | S | Owner resolves affected OQs; independent complete-artifact design review. Historical T001 is not CLEAR for this amendment |
| A1 frozen v1 and apply-plan contract | T027–T028 | M | A0, T024 and unchanged v1 compatibility; US1 stored plan only, no apply |
| A2 actual candidate evidence | T029–T030 | L | A1, T021; US2 quarantine replay, actual identity/equality and no original ref movement |
| A3 shared durability seam | T031–T032 | M | A0, sync characterization and OQ-5; typed recovery intent with old/new roots, no target move |
| A4 guarded local CAS | T033–T034 | M | A2+A3; own unoccupied topic ref only; partial outcomes and receipt |
| A5 retry and reverse plan | T035–T036 | L | A4; US2 crash reconciliation and separately authorized conditional restore |
| A6 native jj capability | T037–T039 | L | A0/OQ-4; US2 explicit unsupported gap until proven native semantics and conformance |
| A7 human/JSON and remote handoff | T040–T041 | M | A4+A5, 016 consumer design, OQ-1/OQ-2; US3 actual SHA and class-dependent pre-push/Ready gates, zero remote writes |
| A8 conformance/evidence | T042–T044 | M | Each selected unit; native minimum/current, sentinels/mutations, exact-head independent review and honest verification |

MVP is A0–A5 plus A7 and Git acceptance in A8. A6 may be specified/tested
separately but must stay disabled until its own evidence passes. A3 can be
authored in parallel with A2 only with disjoint files and after its own gates;
shared CLI, journal seam and schema wiring serialize. No text-only PR stacking
is implied. Remote work is outside these units in 016; no merge, push, Ready,
settings or deployment command is executed by this feature.

### Risks and observable controls

| Risk | Control and proposed evidence |
| --- | --- |
| Candidate changes source history or drops commits | T029 negative scope fixtures, T030 all `=`/count/mapping gates; never substitute a tree-only proof |
| Identity leak reaches real commits | T030 reads actual authors/committers, explicit-only policy; fixed prediction committer is rejected |
| CAS succeeds but journal/output fails | T035 fault injection at every boundary; report partial, reconcile exact old/new, never reset |
| A worktree attaches after sampling | T033 resample registration and refuse observed changes; disclose residual ABA/native lock limit |
| Malicious state file/record widens effects | T031/T040 symlink, replacement, cross-repository/type/id fixtures; bounded typed data and no shell |
| Generic restore silently undoes later edits | T036 requires current==recorded-new and retained root==old, otherwise changed-since |
| jj native rebase rewrites foreign descendants | T037 spike must enumerate effects and prove operation expectation; otherwise capability-unavailable |
| Old review or local head reused for push | T041 exact actual-new-SHA binding and separate full observed remote SHA; 016 owns remote gate |
