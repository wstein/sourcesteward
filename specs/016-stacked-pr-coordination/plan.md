# Implementation Plan: Stacked PR Coordination

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Default saved-plan metadata is .work; remote authority never runs local signing.
> This is a documentation target, not implementation or native acceptance.

**Branch**: `codex/spec-016-stack-toolbelt` | **Date**: 2026-10-25 | **Spec**: [spec.md](spec.md)
**Status**: Proposed design, no implementation or native publication acceptance.

## Summary

Consume verified stack/PR evidence, derive a pure selected-action plan, and run
only individually authorized own-topic/own-PR effects through reviewed native and
forge edges. Offline consumption works without authentication. Online reads and
remote writes are distinct disclosed selections; local apply never grants either.
Spec 015 owns local rewrite, including its actual candidate receipt; this feature
owns the later publication gates. Spec 043 remains the owner's separate landing
operation. No stack merge or remote deletion is added here.

## Technical Context

| Item | Proposed boundary |
| --- | --- |
| Language/runtime | Existing Go 1.27.2+ and Cobra; standard library first |
| Dependencies | Existing pinned official go-gh/v2, native Git/jj adapters; no new dependency, provider SDK or extension installation |
| Functional core | Proposed `internal/stackpublish` typed evidence, validation, planning, gate evaluation and outcomes; no I/O, clock or goroutines in derivation |
| Imperative edges | Existing inspection/hosting/private-state seams, extended only by reviewed closed capabilities; native Git own-topic push, separately measured native jj publication |
| Storage | Immutable plans and action intent/outcome records in validated workspace .work METADATA (captured 013 bytes remain external); typed integration with existing durability conventions, not reuse of sync JSON as though generic |
| Target | Existing supported Linux/macOS native enforcement lanes; exact minimum/current versions from backend policy, with explicit capability evidence |
| Testing | Temporary repositories/bare remotes, fake HTTP/processes/clocks, effect sentinels, proposed offline contract fixtures; failing tests before behavior |
| Performance/scope | One repository per plan, at most two open stack levels and 16 selected actions; fixed operation budgets in the contract |
| Network | Off by default; explicit `--online` proposal, bounded caller-owned authentication, no implicit fetch/cache |
| Unknowns | Owner decisions in research block affected writes, rather than silently resolve trust, endpoint, signing, jj or storage policy |

## Constitution Check

| Principle | Pre-design gate | Post-design assessment |
| --- | --- | --- |
| I Human/agent contract | One human/JSON model, no prompt/editor/pager/provider requirement | Proposed result/exit contract and T007/T008/T033; acceptance not yet executed |
| II Evidence before effects | Stored immutable plan, individual authorization and fresh exact-state guards | T006/T009/T010/T013 and sequential action edge; uncertain state refuses |
| III Ownership/recovery | Own task/branch/PR only; durable intent/outcome; no owner checkout mutation | T011/T012/T025/T026; remote effects are not reversible through local recovery |
| IV Specify/verify | Reviewed artifacts before implementation, failing tests and native conformance | T001/T035/T036/T038; no new runtime evidence claimed |
| V Native peers | Preserve native ids; Git/jj capabilities measured separately, no raw Git in jj | T004/T005/T034 and explicit jj publication gap |

No principle is amended. The constitutional requirement for later specifications
is addressed by this proposed effect contract only after owner decisions and
review; the document itself grants no write authority. Both design passes find
no intended exception; unresolved capabilities remain disabled.

## Project Structure

This directory contains `spec.md`, `plan.md`, `research.md`, `data-model.md`,
`contracts/forge-effects.md`, `contracts/receipts.md`, `quickstart.md`, `tasks.md`,
`checklists/requirements.md` and `analysis.md`.

Proposed future source paths (none is created by this design):

```text
internal/stackpublish/             pure plans, bindings, guards, action outcomes
internal/hosting/                  typed bounded read/write capability edges
internal/workspace/                reviewed private plan/journal integration
internal/cli/stack_publish.go      parser/render adapter only
internal/contracts/               contract fixtures and legacy freeze tests
internal/acceptance/               native Git/jj effect/conformance fixtures
```

Do not let CLI launch processes, inspect state files or import HTTP. The existing
architecture guard must be amended narrowly if the new operation package needs a
hosting edge; no wildcard/transitive network exemption. Use Spec 040 fanout for
independent reads, preserving input indexes and the one run-wide budget; action
writes and their dependent observations are sequential.

## Delivery phases and dependencies

1. **Characterize and decide** (T001–T005): pin existing 020/022/025/030/041/042
   behavior and 015 v1 freeze, owner decisions, native capability table. No write
   implementation starts before its method/effects/trust/signature gate is accepted.
2. **Foundations** (T006–T012): fail first for immutable typed plans, bounds,
   escaping, safe store and mode/ownership/attribution. Core logic must be tested
   without network. Independent test fixtures use no live credentials.
3. **US1** (T013–T016): consume producer-bound verified relationships and derive an empty-default
   plan, with native rewrite/depth/dependency blockers. This offline MVP has no
   write endpoint dependency and can ship alone after its own acceptance.
4. **US2** (T017–T024): tests before narrowly selected native push, PR effects,
   retarget and separately gated native link/unstack. Fresh per-action gate inputs
   and actual-SHA receipt precede any write. No parent rewrite is inferred.
5. **US3** (T025–T028): durable intent/outcome and fresh reconciliation, including
   lost responses and unknown outcome; owner resync remains inert data.
6. **US4** (T029–T032): independent exact-current-head Ready gates, accepted
   per-PR scope, explicit Ready action, conditional owner landing handoff data.
7. **US5 and acceptance** (T033–T038): rendering/escaping/native capabilities,
   frozen contract regression, minimum/current lanes, review and durable status.

T025/T026 are mandatory before any remote action may be enabled, even though
organized under US3. US2 implementation may be developed against fakes first;
no executable write capability ships without those durability/retry guards.
Likewise T029/T030 gate T031 Ready. Documentation PRs do not stack on one another:
these designs have no code dependency. Later runtime work may use a two-level
stack only for a named real code dependency and disjoint author file scopes.

## Admission and per-action execution

Resolve backend/layout and selected repository locally first. Reject ambiguity,
GitButler and unsupported native capabilities. Admit supplied evidence as bounded
untrusted data; do not authenticate or refresh merely because it has a PR URL.
Offline dependent remote guards remain unknown and actions blocked.
Consume current 012 `restack-advised` as a nested stack finding, never as the
published 035 event. Its SC-009 fanout is gated by OQ-FANOUT entry approval and
is not a new 016 batch surface. When supplied, validate 014's actual full-H
attribute-source/private-context profile binding; missing enforcement refuses
that evidence. The hidden-v1 replay implementation's attribute-source gap
(issue #241) is fixed for the hidden restack runner by PR #279 with an empty
attribute source (see `specs/015-local-restacking/verification.md`, section on
the attribute source of the replay); this is not the 014 full-H profile, which
stays unimplemented, and the Git 2.46 lane has not run.
Do not infer native jj aggregate conflict semantics from Git first-conflict
behavior. Preserve 013 independent safety-copy retention and no operation-head
CAS/invoker-working-copy-effect admission; no recovery receipt authorizes a write.

Online plan explicitly reads only accepted GET shapes within the operation
budget. Before apply, compare plan identity/action ids and all relevant fresh
local/remote expectations. Validate own task/branch/PR/body, Spec 041 mode and
Spec 042 D5 for push/Ready, trusted owner identity range/config origins, explicit
signing policy, publication review tier and per-action permissions. Record durable
intent before each write; reread the effect and record actual/unknown outcome.
Stop on a failed/unknown guard or cancellation before the next effect.

A native Git lease push is one exact full-SHA refspec and full-SHA expected-old
lease on an owned existing topic. Ref creation stays disabled until the
allowlist review accepts the measured absent-ref lease (W1) and the
hook/credential boundary. Retarget edits only the selected base and verifies head
unchanged. Link/unstack cannot use generic extension execution or inferred API
semantics; until accepted, they are blocked plan entries and inert reference data.

Ready always needs exact-current-head independent CLEAR, required CI green,
`rebaseable == true` and accepted own PR scope. These gates are stricter than the
docs-only before-push rule on purpose: they govern different actions. Unknown
checks or review source is not a pass. A PR can be Ready with out-of-scope future
work still open; its own unmet acceptance cannot be ignored.

## Shared vocabulary and compatibility

Owner decisions 2026-10-25 adopt --online only (no old --pull-requests alias),
--human/--json exclusivity and shortening --timeout. Local observation stays
offline; local --authorize and per-action remote --authorize-remote remain
separate. Rendering stays unsaved; explicit --save uses validated default
WORKSPACE_ROOT/.work/stw plan/action metadata or explicit --state-dir override.
No preview repairs/creates the namespace. Trusted workspace policy defines the
unique root, identity, reviewer/check sources; missing policy refuses. Metadata
needs explicit private modes, ignore/untracked export-risk and no-follow/race/
quota/fsync guards with no auto-ignore/config/index edit. Captured 013 bytes
and safety copies remain in explicit EXTERNAL --payload-dir, never .work.

The target CLI exit-contract revision 2 is unified 0/1/2, including 015 incomplete 1
without a legacy 3 selector; CURRENT hidden 015 code 3 remains until its red-first
mapping/help task. JSON schema 1 bytes are a separate version domain and stay
frozen. 013/014 remain helper/network-free. Optional re-sign is separately
planned/authorized local 015 work through a measured fixed HOST helper/verifier;
no stw keys or implicit invocation under remote authorization. 016 validates
the final signed chain/head before independent exact-SHA review/push. All native
measurement/transport gates remain; adopted decisions are not conformance.

## Risks and verification gates

- A sampled forge is not transactional: head/base/permissions can race, leases
  cannot detect all ABA and failed responses may hide success. Reconcile exact
  post-state; retain unknown and never duplicate a potentially completed write.
- Shared global identity/config or untrusted caller flags can hide foreign parent
  commits. Validate trusted owner over remote-default..actual-new-head and report
  bounded config origins, including the whole open parent's range.
- Signed source replay can lose signatures despite equal range-diff. Preserve
  015's explicit unsigned candidate/signature-dropped evidence and fail closed
  under decided default refusal; optional 015 host-only signing is disabled without accepted measured profile.
- Native GitHub rewriting and head deletion were measured together only with
  deletion on. No claim about independent causation or another setting is made.
  043's readable-off handoff and on/unreadable unmeasured pre-retarget gate stay
  conditional and owned by 043.
- Existing 022 is GET-only. New write methods/body limits are a reviewed additive
  transport amendment, never an assumption that an SDK permits arbitrary writes.

Acceptance requires all tasks for the declared capability, native minimum/current
lanes, meaningful mutation probes and independent review of the exact integrated
source. Build/install/live forge measurements and owner landing are separately
authorized operations; source tests or documentation checks do not stand in for
them. No product/documentation build is needed to review these Markdown artifacts.
