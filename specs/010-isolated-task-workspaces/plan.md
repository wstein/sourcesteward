# Implementation Plan: Isolated Task Workspaces

**Branch**: codex/010-isolated-task-workspaces | **Date**: 2026-10-15 | **Spec**: [spec.md](spec.md)

## Summary
Implement Spec 010 in one bounded local operation layer: inspectable stored creation plans,
explicit authorized apply, durable task ownership/recovery records, handoff and retirement
preparation. Native Git worktrees and native jj workspaces are peers. This work does not
remove workspaces, publish, launch agents or execute setup scripts. Existing inventory and
identity contracts stay unchanged.

## Technical Context
- Go 1.27.2; Cobra v1.10.2 and existing pinned schema validator; standard library first.
- Linux/macOS; Git minimum 2.46.0/current 2.56.0, jj minimum 0.45.1/current 0.46.0.
- Storage: explicit caller-owned existing private `--state-dir`; immutable plans, durable
  pre-action recovery journals and task records. No source configuration ownership markers.
- Tests: failing Go behavior tests first, schema fixtures, mandatory real CLI minimum/current
  native conformance including failure/concurrency/interruption/effect sentinels.
- Bounds: 10-second read-only observation deadline, 60-second apply deadline, 64 KiB command
  output, 1 MiB plan/result/journal, 1,024 registrations/records, 4,096-byte paths/base,
  128-byte ASCII identifiers. Enforce before traversal/allocation and preserve partial effects.
- Offline and noninteractive; no undeclared hooks, filters, helpers, editor, pager or network.

## Constitution Check
Pre-design PASS: this user-requested later specification supplies the native mutation contract
required by constitution 1.1.0. The historical implemented-scope/jj-pending paragraph is not a
prohibition on implementing a later reviewed specification. No normative amendment is needed.
Post-design PASS subject to pre-implementation analysis: plans are not authorization, apply
requires explicit authorization and stale-state validation, source state is retained, recovery
precedes native writes, unknown outcomes retain evidence, both backend lanes are mandatory.
Git configuration capable of hooks/filters/helpers must be refused before execution, not
silently disabled. jj must use native writes with reviewed configuration isolation and an
explicit least-write/network-denying sandbox. Missing enforcement refuses. No exceptions.

## Project Structure
- `internal/workspace/workspace.go`, `result.go`: public operation/model contract; no CLI effects.
- `internal/workspace/state.go`, `paths.go`: private secure store, locking, journal, path identity.
- `internal/workspace/git.go`, `jj.go`: native adapter observations/actions.
- `internal/workspace/*_test.go`: real temporary repositories, failure/effect tests.
- `internal/cli/workspace.go`, `workspace_test.go`: Cobra syntax and shared human/JSON result.
- `schemas/v1/workspace.schema.json`, `internal/contracts/workspace_test.go`: new independent schema.
- `scripts/smoke-workspaces.py`, `.github/workflows/ci.yml`: mandatory native process acceptance.
- `docs/workspaces.md`, `docs/backends.md`, `docs/json-contracts.md`, architecture/progress/status:
  honest capability, security and evidence documentation; explicit Pages guide/schema allowlist.

## Design and delivery
See [research](research.md), [model](data-model.md), [contract](contracts/workspace.md) and
[validation guide](quickstart.md). Store operations serialize cooperating processes, but do
not pretend to lock external Git/jj/editor processes. Re-sample before and after action;
changes outside the owned destination are never automatically repaired. Check native and
filesystem identities, not only names. A matching basename is no ownership proof.

Plan → tasks → independent cross-artifact analyze → failing tests → implement → independent
review → verify → focused Conventional Commits → PR. Parallel backend authors own separate
worktrees/files after shared contract freeze; integration is sequential. All three stories
are required before handoff; creation is an internal MVP gate rather than a partial delivery.

## Complexity Tracking
No constitution exceptions. The explicit store avoids hidden source metadata writes and makes
recovery independently inspectable; platform-native sandboxing is required for safe Git/jj effects.
