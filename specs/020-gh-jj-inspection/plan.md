# Implementation Plan: Shared GitHub inspection and gh jj entry point

**Branch**: `codex/020-gh-jj-inspection` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Introduce a jj-focused `gh jj` entry point and backend-neutral `stw hosting repo|pr|ci` commands backed by one Go operation layer. Resolve local hosting candidates through existing native Git/jj adapters. Only explicitly requested PR/CI observations launch fixed authenticated `gh api` GET requests. No network call is needed for repository resolution, and no remote or local mutation is introduced.

## Technical Context

**Language/Version**: Go 1.27.2, existing supported native Git/jj versions.  
**Primary Dependencies**: Existing Cobra v1.10.2; standard library subprocess/JSON/time/net URL tools; gh 2.102.0 initial tested compatibility baseline, optional for PR/CI only. No new library dependency.  
**Storage**: No persistent application records or credential storage; existing native inspection may create private temporary isolation files outside target.  
**Testing**: Go table tests, fake gh process fixtures, schema fixtures, real temporary Git/jj repositories, existing Python source/conformance checks.  
**Target Platform**: Existing supported Linux/macOS read-only native execution, minimum/current backend CI lanes.  
**Project Type**: CLI with two thin entry points and shared operations.  
**Performance Goals**: Overall hosting deadline 30 seconds; at most 10 pages per collection, 1000 combined CI records, 1 MiB per subprocess output and final structured output. Existing tighter native inspection bounds still apply.  
**Constraints**: No prompts/pagers/helpers, no mutation HTTP methods, no arbitrary endpoint forwarding, no raw credential/response diagnostic exposure; ambiguous discovery refuses.  
**Scale/Scope**: One selected repository, one explicit PR, its exact head checks/statuses; no stack traversal or whole-organization enumeration.

## Constitution Check

Pre-research assessment: PASS. Post-design assessment: PASS.

- Shared human/agent contract: shared typed hosting result and versioned independent schema; usage exit 2, operational failure exit 1, success exit 0.
- Evidence before actions: this slice supplies sampled read evidence only; no publication/mutation authorization is inferred.
- Ownership/recovery: existing native ownership and enforced jj read-only isolation are reused; explicit repository override cannot bypass ownership. No recovery guarantee is needed for reads.
- Specify/verify: failing tests precede implementation; independent analysis precedes implementation; mandatory native backend/effect/schema evidence is recorded at handoff.
- Tier 1: `stw hosting` supports native Git and jj through the same operations. The deliberately jj-specific extension refuses ordinary Git. Colocated and standalone jj remain native jj.
- Scope: explicit network reads were requested and disclosed. Spec 016 remote mutations, installation and release remain deferred. No constitution amendment or exception is required.

## Project Structure

```text
cmd/gh-jj/main.go           # cancellation and shared CLI entry only
internal/cli/hosting.go    # shared command parsing/rendering, no process/network work
internal/hosting/          # options/result, resolution, bounded gh API reads
internal/inspection/       # bounded native remote discovery with existing isolation
schemas/v1/hosting.schema.json
internal/cli/testdata/      # schema-valid success/error observations
specs/020-gh-jj-inspection/ # specification, design, analysis, acceptance evidence
```

Existing architecture checks must admit the narrow hosting operation boundary without relaxing CLI process/network prohibitions. Both command trees call `hosting.Inspect`; no extension-only business logic. A shared `inspection.Remotes(ctx, identity Result) RemoteResult` observation must remain native/configuration-isolated and return identity plus remote candidates without invoking git transport or credential helpers.

## Phase 0: Research

Resolved decisions are in [research.md](research.md). Fixed REST endpoints avoid gh's context placeholders and field-dependent method selection. Manual bounded pagination avoids unbounded `--paginate`; error codes and incomplete collections are never converted into passing CI. No third-party extension implementation is copied.

## Phase 1: Design

[Data model](data-model.md), [CLI/operation contract](contracts/hosting.md), [transport contract](contracts/transport.md) and [quickstart](quickstart.md) define concrete interfaces and acceptance. Repository selection defaults to `github.com`; enterprise hostname requires explicit caller selection and never follows a host extracted from an untrusted remote automatically. Repo resolution reports local association, not proof a GitHub repository currently exists.

## Implementation and review sequence

1. Commit reviewed specification/design/task artifacts.
2. Implement test-first native remote discovery, hosting core/API and CLI/schema in separate worktrees after cross-artifact analysis clears.
3. Integrate, independently review source/security/contracts, repair findings and run all checks plus minimum/current native CI.
4. Record actual evidence and remaining gates; create a PR. No installation/release or live remote mutation.

## Complexity Tracking

No constitution violations or new dependency exceptions. A separate hosting package is justified by process/network isolation from parsing and reuse across both entry points.
