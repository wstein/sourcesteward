# Implementation Plan: Workspace and Reference Tidy

**Branch**: codex/011-tidy-plans | **Date**: 2026-10-15 | **Spec**: [spec.md](spec.md)

## Summary
Deliver three sequential work items, each independently analyzed, reviewed, tested and handed
 off as a PR: [WI1 plans](wi1-plan.md), WI2 reference cleanup, and
WI3 owned retirement. Planning defaults to retaining uncertainty. Plans never
authorize apply. Native Git/jj are Tier 1 peers for their declared capabilities.

## Technical Context
Go 1.27.2, Cobra v1.10.2, existing pinned schema validator; no new runtime dependency.
Linux/macOS; Git 2.46.0/2.56.0 and jj 0.45.1/0.46.0 remain mandatory minimum/current lanes.
Reuse internal/workspace secure store, canonical path identities, private journals, native
sandbox and inspection; no duplicate state implementation or mutable global adapter injection.
New independent tidy schema v1; existing inspection, inventory and workspace contracts unchanged.
Bounds: 10-second planning/observation and 60-second apply; native output64KiB, each encoded
record/result1MiB, aggregate1024 records, input paths/selectors4096bytes and IDs128bytes.
Enforce before traversal/allocation; bounded results preserve partial effects and recovery pointer.

## Constitution Check
Pre-design PASS: later reviewed specification supplies required mutation contract; user explicitly
requests implementation and PRs, not cleanup of their real repositories. All destructive tests use
disposable fixtures. Post-design PASS pending independent analyze: inspectable explicit targets,
durable recovery before mutation, per-action stale validation, ownership retention, native jj writes,
no remote effects or helpers, mandatory dual-backend/version acceptance. No normative amendment.
External races/ABA remain sampled; Git target CAS is stronger than jj operation sampling, never
claim equivalence or transactions. Missing enforcement/capability refuses. No exceptions.

## Project Structure
- internal/workspace/tidy*.go: shared coordinator/models, secure store extension and native adapters.
- internal/inspection/jj_tidy.go: bounded native jj observations/writes within existing session boundary.
- internal/cli/tidy.go: Cobra parsing and same-result human/JSON rendering only.
- schemas/v1/tidy.schema.json and internal/contracts/tidy_test.go: independent versioned contract.
- scripts/smoke-tidy.py and its tests, .github/workflows/ci.yml: mandatory native case manifest.
- docs/tidy.md, backend/JSON/agent guides, schema/guide publication allowlists and evidence.

## Delivery and Dependencies
Roadmap/progress reconciliation precedes these artifacts (763a502). WI1 → WI2 → WI3; each
PR owns focused commits and truthful scoped evidence. Shared [research](research.md),
[model](data-model.md), [CLI](contracts/tidy.md), [API](contracts/interface.md),
[quickstart](quickstart.md), [tasks](tasks.md) apply throughout. Each WI must rerun analyze
before source work, fail behavior tests first, then implement, independently review and verify.
Git/jj authors work on disjoint files in worktrees after interface freeze; root integrates serially.

## Complexity Tracking
Secure lazy tidy store directories preserve Spec010 strict records; a new result avoids breaking
published workspace schemas. No forge client, squash inference, garbage collection, automatic
recovery, arbitrary revsets or remote writes. Future integrations remain explicit unsupported proof.

## Issue #62: shared Git layout consumers

This candidate amendment depends on the independently analyzed
[Spec 010 layout contract](../010-isolated-task-workspaces/contracts/workspace.md#git-checkout-layout-admission-issue-62)
and its bounded shared implementation. It is not covered by historical WI1/WI2/WI3 clearance.
`gitTidyAdapter.Observe` delegates to `gitAdapter.Observe`; reference apply revalidates that
snapshot, while `gitRetirementAdapter.Observe` also checks target safety. All must consume
the same fingerprint and reject changed mappings before the next effect, including recovery
creation. Post-action fingerprints may advance only for already specified exact native deltas.

Scope is consumer tests and any strictly necessary plumbing in `tidy_git.go`,
`tidy_git_apply.go` and `retirement_git.go`; shared mapping proof stays with Spec 010.
Preserve complete workspace protection, durable recovery/CAS, physical manifests, exact
owned lock/removal and completed-retry validation. No additional filesystem grants, raw Git
fallback, bookmark abandonment or remote deletion. Existing native jj bookmark conformance
is a mandatory regression gate, not a new behavior to implement.
The [follow-up tasks](tasks.md#issue-62-shared-layout-consumers) gate exact-source review and
minimum/current conformance. Public schemas and legacy strict record decoding stay unchanged.

## Narrow prune amendment delivery

The [narrow prune contract](contracts/narrow-registration-prune.md) proposes a distinct
Git-only operation over one named missing-directory registration. Reuse private-store,
original creation ownership and native recovery mechanisms where valid; add a complete
bounded private registration payload and a separately authorized restoration path.
Do not reuse present-checkout retirement by inventing disk evidence or clear the stw lock.

Deliver this specification PR first. Source work waits for independent CLEAR, owner merge,
and owner resolution of the contract's frozen-schema/interface choice. The defined
absence predicate is not another owner gate. Then
implement expert planning/apply/recovery test-first in a separate PR; only after its accepted
capability exists, implement Spec 041 T012b classification/plan-set wiring in the final PR.
No task/progress completion is asserted here. T501–T508 define the dependencies and evidence.
