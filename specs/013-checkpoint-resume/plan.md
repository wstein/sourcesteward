# Implementation Plan: Checkpoint, Resume and Selective Undo

**Branch**: `codex/spec-013-stack-toolbelt` | **Date**: 2026-10-25 | **Spec**: [spec.md](spec.md)
**Status**: proposed design only; no implementation, schema publication or runtime authorization.

## Summary

Provide explicit local capture, offline resume comparison and separately planned
restore/selective undo. Keep captured bytes distinct from recovery metadata.
Reuse Spec 032's durability and recovery-root mechanisms through a reviewed typed
integration, preserving the sync v1 reader/writer and recovery behavior; do not
copy a second journal engine. Native Git index/worktree state and native jj
working-copy/change/operation state retain separate capability contracts.

## Technical Context

**Language/version**: existing Go 1.27.2 and Cobra architecture, standard library
first; no new dependency is selected. Minimum/current native lanes follow the
repository backend policy; exact capture/undo capability admissions require OQ-1
and OQ-2 evidence before enablement. Git 2.46+ and jj 0.45.1+ inspection admission
alone is insufficient proof for these new mutations.

**Storage**: existing sync records in `<git-common-dir>/stw/sync` and roots under
`refs/stw/recovery/sync/RUN_ID/N` remain unchanged. Proposed typed recovery
integration, private payload store, ownership binding and retention are OQ-3 to
OQ-5. Payload bytes never live in sync journal fields or public results. OQ-5 reserves distinct checkpoint/undo Git prefixes with exact typed ownership; a jj retained-root mechanism needs native proof and cannot silently use visible exportable bookmarks.

**Testing**: red-first behavior tests in temporary repositories; pure decision
and result tests plus native adapter conformance, crash injection and before/
after effects. Missing required native/platform execution blocks acceptance.

**Platform**: existing native Linux/macOS runner protections; missing isolation
refuses. No product/platform claim is added by these Markdown artifacts.

**Constraints**: offline, noninteractive, no helpers/hooks/editors/pagers/model,
no config-selected commands, no automatic stash/reset/rollback/GC. Suggested
protocol bounds appear in [checkpoint contract](contracts/checkpoint.md#bounds)
and must be enforced before effects. No new background process or installation.

**Scope**: one explicitly identified owned repository/workspace per plan, exact
path/state selection, one chosen restore or undo action set. No recursive
submodule capture or cross-repository transaction. Bounded optional task text
is data. Default action/capture selection is none.

## Constitution Check

| Gate | Design disposition |
| --- | --- |
| I: shared understandable contract | One bounded typed human/JSON result; escaped untrusted values; no prompt or cloud requirement |
| II: inspectable consequence | Stored plan with selection, expected state, ownership, effects, limits and digest; fresh checks before every action; authorization plus plan id |
| III: ownership/recovery | Native identity binding and all linked-workspace guards; durable intent/root and any required selected bytes before effects; no inferred rollback |
| IV: reviewed/test first | All future tasks unchecked; red tests precede behavior; independent exact-head review and required native conformance before acceptance |
| V: native peers | Capability matrix distinguishes Git and jj; colocation uses jj; unavailable/ambiguous/GitButler ownership refuses; no Git fallback |

Pre-design and post-design result: no deliberate constitutional exception.
Unresolved owner decisions block the corresponding mutations rather than weaken
the gates. A complete design artifact set does not mean implementation-ready.

## Architecture and project structure

Proposed operation package `internal/checkpoint` owns pure selection,
comparison, guard and undo decisions, with small native/effect adapters;
`internal/cli/checkpoint.go` only parses/renders through that typed operation.
These paths are future task targets, not directories created in this amendment.

The recovery seam is designed jointly with `internal/stwsync/journal.go` and
`restore.go`; a proposed `internal/recovery` package name is provisional (OQ-5).
Characterize sync first, then extract/reuse durable atomic writes, create-only
native roots, typed record validation and expected-state reconciliation.
Operation-specific adapters keep sync, restack and checkpoint semantics apart.
Do not pass a `sync` record into checkpoint/undo execution. A future Spec 015
restack restore adapter may accept its own exact run/action; 013 owns checkpoint
comparison and selective undo orchestration, without another generic writer.

Payload storage behind `internal/checkpoint/store.go` (proposed) handles selected
bytes, content digest, native mode/type, missing/deleted state and Git staged vs
disk layers. Root-preserved committed objects and jj operation identities cannot
stand in for uncaptured bytes. The store validates trusted ownership, no-follow
paths and integrity; journal metadata references the payload receipt. Explicit 0700 directories/0600 files and external-location proof protect selected bytes independently of umask. Enforce a checkpoint cap and approved physical-store quota; separately planned/authorized/confirmed drop removes only exclusive owned bytes/roots with partial receipts, never automatic eviction or secure-erasure promises.

Documentation lives only in `specs/013-checkpoint-resume/`: spec, plan, research,
data model, three contracts, quickstart, tasks, checklist and analysis. No source,
schema, dependencies, docs/commands.md or docs/progress.md changes belong here.

## Phases and dependencies

1. Resolve OQ-1 through OQ-5, record exact native capability evidence, and review
   the design. Characterize existing sync recovery before extracting the seam.
2. Implement bounded selection and selected-payload capture with durable receipt;
   admit Git and jj capabilities independently. A failed or partial capture
   cannot advertise complete recovery. This is the US1 MVP.
3. Add offline resume comparison, then separately authorized restore with native
   per-action guards and crash reconciliation (US2). Comparison remains useful
   even when restore is unsupported or payload unavailable.
4. Add common CLI/result/error/escaping/bounds coverage (US3) across all paths;
   these are foundational requirements despite their story ordering.
5. Add explicit inverse-change vs history/operation-restoration plans (US4),
   explicit merge parent, descendant scope and shared/foreign refusal. Capability
   selection requires owner decision and measured native semantics first.
6. Complete each required native lane and failure suite, published-contract
   regressions, independent review and acceptance evidence before exposing the
   capability. Source implementation, release, activation and remote effects
   are separate gates.

US3 foundations precede all effects; its end-to-end rendering tests follow each
story. Capture tests can run independently by backend after the common model;
shared recovery extraction and CLI wiring are sequential. No concurrent
mutation is needed. If bounded read fanout becomes necessary, reuse Spec 040
`internal/fanout`, deterministic indexed results and injected/fake clocks.

## Risks and limits

Concurrent edits/ABA cannot be ruled out by sampling; refuse detected or unknown
relevant state. Multi-path disk restoration is not an atomic transaction with
native metadata. Crash outcomes distinguish known completed effects from unknown
ones and retain incomplete records. Corrupt/missing/expired bytes block the
promised restore instead of triggering a broader native rollback. No automatic
retention deletion or broad jj operation restore is permitted by this plan.
The [research decisions](research.md#needs-owner-decision) gate unresolved policy.
