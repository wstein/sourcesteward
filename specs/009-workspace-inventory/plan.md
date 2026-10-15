# Implementation Plan: Workspace, State and Reference Inventory

**Branch**: codex/009-inventory-implementation | **Date**: 2026-10-15 | **Spec**: [spec.md](spec.md)

## Summary
Implement all FR-001–015 in a bounded read-only `stw inventory` operation with optional three-valued predicates, native Git/jj adapters, separate inventory v1 schema and per-backend conformance. Preserve existing inspect/version contracts. Unsupported observations remain explicit; they are not silently deferred or fabricated.

## Technical Context
- Language: Go 1.27.2, Cobra v1.10.2; existing pinned dependencies only, standard library first.
- Storage: no persisted inventory or target writes; temporary isolated jj config outside target only.
- Testing: Go unit/table-driven real temporary-repository tests, schema fixtures, Python real CLI smoke, mandatory minimum/current Linux CI; macOS native sandbox local checks where available.
- Target: Linux/macOS supported inspection platforms; existing Git 2.46+ and jj 0.45.1+ contracts.
- Project type: CLI; performance/scope: ten-second total deadline, 64 KiB per command, 1 MiB serialized result, 1,024 total records. Collection loops and filesystem reads must enforce bounds before allocation/unbounded traversal.
- No interactive process, arbitrary source execution, helper, network, repair, jj snapshot or implicit fetch.

## Constitution Check
Pre-design: PASS — human/machine shared model, explicit unknown, native backend selection, no consequential actions, tests before implementation, no policy changes required. Existing constitutional scope paragraph has historical jj-pending text; acceptance remains governed by implemented Spec 008 evidence, not an invented new policy.
Post-design: PASS — safe execution reused, temporary effects disclosed, old schemas untouched, every FR mapped to tasks and mandatory native conformance. Analysis must clear before source implementation; final independent review/test evidence required before handoff. No complexity exceptions.

## Project Structure
- internal/inspection/inventory.go: options, orchestration and common models (or inventory_result.go).
- internal/inspection/inventory_git.go, inventory_jj.go: native observations; jj.go: reusable safe session extraction.
- internal/inspection/inventory*_test.go: failing behavior/effect tests before implementation.
- internal/cli/inventory.go, inventory_test.go: Cobra parsing, same-model human/JSON rendering.
- schemas/v1/inventory.schema.json; internal/contracts/inventory_test.go: new fixtures/validation, existing contracts preserved.
- scripts/smoke-inventory.py; .github/workflows/ci.yml: real-process minimum/current gates.
- docs/backends.md, docs/json-contracts.md, docs/agent-guide.md, docs/progress.md, docs/feature-status.json: honest scope and evidence.
- specs/009-workspace-inventory/: research, model, contracts, quickstart, tasks, analysis, verification.

## Native implementation strategy
Git: NUL-safe registered worktree, index/reference/file plumbing; object/index-only diffs plus confined Go raw blob hashing for selected disk files. Never run Git status or worktree diff content hashing, which may execute configured filters. File reads use os.Root confinement, nonblocking/no-follow opens and a 64 KiB per-file cap; oversized or normalization-dependent observations remain qualified unknown. Exact literal path handling, optional locks/fsmonitor/untracked-cache suppression, no lazy fetch or diff helpers. Enumerate registered worktrees only; inspect available registered paths without assuming ownership. Resolve operands to unique full OIDs and sample refs/registration before and after. Scope absence in bare/unborn states is explicit.
jj: extract reviewed jjSession setup callback; all observations fixed native commands, isolated config, enforced sandbox, no snapshot and pinned operation. Enumerate workspace list and native bookmarks/remote bookmarks with JSON-safe templates; use native stored diff and revsets built only from safely escaped literals/full identities. Disk scopes remain unknown if unavailable without snapshots. Configured base expression text is data; unsafe/unresolvable expressions remain qualified candidates. No raw Git fallback.
Common: bound all loops and aggregate records/output; cancellation propagates; unknown required evidence produces partial and operational failure produces error. Explicit-base integration and selected-remotes reachability are observations, not cleanup authorization. See [contract](contracts/inventory.md).

## Dependencies and delivery
Plan → tasks → non-destructive cross-artifact analysis → failing tests → adapters/CLI/schema → conformance/review → PR. Parallel adapter work uses isolated worktrees after shared contract freeze; root integrates focused commits without overwriting concurrent work. All FRs are in this delivery; later task workspaces/tidy/mutations remain excluded.
