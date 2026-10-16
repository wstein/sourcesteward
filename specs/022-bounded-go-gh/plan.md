# Implementation Plan: Bounded official go-gh transport

**Branch**: `codex/022-bounded-go-gh` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Replace only the hosting API subprocess transport with official go-gh/v2 HTTP middleware and owned bounded response handling. Keep native ownership, local repository resolution, schemas and both CLI entry points intact. Existing bounded gh execution remains for version/authentication, not API reads.

## Technical Context

- Language: Go 1.27.2; existing Cobra CLI.
- Dependency: pinned go-gh/v2 v2.16.2; go.mod/go.sum and license footprint reviewed.
- Runtime: selected-host gh >=2.102.0 remains required for remote reads; local reads stay independent.
- Storage: no new persistent state; existing neutral temporary directory is cleaned.
- Bounds: operation 30 seconds; raw and normalized HTTP bodies each 1 MiB plus one sentinel; subprocess buffers 1 MiB; existing pagination/record bounds.
- Tests: Go unit/integration/schema tests, native Git/jj fixtures, Python process smoke, race/module/docs checks; no live network fixtures.
- Unknowns: none; [research](research.md) records pinned evaluation decisions.

## Constitution Check

Pre-design: PASS. Shared versioned results and noninteractive bounds preserve Principle I; read-only fixed GETs preserve II/III; TDD, pre-code analysis, independent review and native conformance satisfy IV; native Git/jj adapters remain peers under V. No merge/release/mutation authorization is inferred.

Post-design: PASS with the same conditions. The official SDK is justified for GitHub conventions and HTTP middleware, not native backend ownership or discovery. Published schema bytes remain frozen. In-process conformance preserves real native operations; process smoke explicitly retains only behavior testable without endpoint overrides.

## Project Structure

- `internal/hosting/transport.go`: existing trusted gh/auth isolation plus SDK adapter.
- `internal/hosting/operations.go`: preserved operation boundary and `InspectWithTransport` seam.
- `internal/hosting/*_test.go`: HTTP/auth/adversarial tests and unchanged operation tests.
- `internal/cli/*hosting*`: unexported command injection seam and rendering tests.
- `internal/contracts/`, `scripts/smoke-hosting.py`: compatible schema/native/dispatch evidence.
- `go.mod`, `go.sum`: pinned official dependency.
- `docs/hosting.md`, `ARCHITECTURE.md`, progress/index/evidence: updated transport truth.

## Execution Phases

1. Design/research/tasks and independent pre-implementation consistency analysis.
2. Baseline plus failing auth/HTTP boundary tests; SDK implementation in isolated worktree.
3. Parallel adapter/schema/native fixture conversion after frozen interface; failing tests before edits.
4. Integrate, independent adversarial review, resolve findings, full source/race/native checks and durable acceptance record.
5. Focused Conventional Commits and separate PR. No live API, extension install, release or deletion.

## Interface

`apiReader.get(context.Context,string)` is unchanged. `Inspect(ctx,path,Options)` uses default SDK transport; `InspectWithTransport(ctx,path,Options,http.RoundTripper)` is an internal-package seam for deterministic fixture transport. CLI test constructors inject this shared operation, not fabricated results. No production flag/environment endpoint override exists. See [transport contract](contracts/transport.md).

## Complexity Tracking

No constitution exception. Direct SDK HTTP client avoids known unbounded REST error decoding. Retaining bounded gh authentication deliberately avoids importing SDK subprocess/auth convenience behavior into the trusted boundary.
