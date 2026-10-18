# Implementation Plan: Complete local tidy with explicit integration evidence

**Branch**: `codex/tidy-dogfood-contract` | **Date**: 2026-10-18 | **Spec**: [spec.md](spec.md)
**Input**: Specification independently CLEAR at `99539328e696764ffb64b832fa8a3991a53bc4ae`; runtime baseline `c0832055dbd3f7c89355a39489a14a85231b605e`.
**Status**: Stacked-integration amendment candidate. Prior plan `5272dc1e` was independently CLEAR; updated specification/plan require renewed exact review before tasks are finalized. No runtime implementation.

## Summary

Implement four separate cleanup capabilities over one versioned evidence/recovery boundary: optional exact-head GitHub integration proof including intermediate stack branches with reported merge-result ancestry into the chosen base, explicit completed legacy Git worktree retirement, complete bounded tree/content reads, and local tracking-ref deletion after complete remote-absence evidence. Offline v1/v2 behavior and old stored-plan meaning remain unchanged. Shared tree-metadata admission expands for all existing consumers as the reviewed Spec 010 amendment; larger retirement user content applies only to selected Git v3 retirement.

Keep native effects in `internal/workspace` and HTTP/authentication in `internal/hosting`. Add a typed hosting evidence session with a shared per-operation budget; workspace imports its read-only interface without exposing arbitrary HTTP/native commands to the CLI. Existing native safety admission precedes online session creation. No network permission is added to native sandboxes.

## Technical Context

- Language/dependencies: repository Go 1.27.2, standard library first, existing Cobra 1.10.2, go-gh/v2 2.16.2 and jsonschema/v6 6.0.3; no new dependency.
- Storage: existing private no-follow/locked/fsynced state store, separate v3 plan/journal namespaces, native recovery refs retained. No database or ownership daemon.
- Platforms: current admitted Linux/macOS Git capabilities; required Git 2.46.0/2.56.0 lanes, existing jj 0.45.1/0.46.0 regression lanes as pinned by repository conformance configuration. Actual pins are verified when tasks are generated; unavailable lanes are gaps, not substitutes.
- Scope: GitHub.com/SHA-1 online evidence and admitted native Git legacy retirement. Native SHA-256 remains an offline capability where already supported; no invented SHA mapping. Existing jj v1/v2 remains native and offline.
- Performance: realistic 315–358-file/301,117-byte SVG retirement fixture must pass existing observation/application deadlines; bounded object batching removes per-file subprocess overhead. Maximum-size inputs may still hit the deadline and retain.
- Constraints: [stream/content contract](contracts/streams.md), [online contract](contracts/online.md), [CLI/effects contract](contracts/tidy-v3.md); no builds, installations or live mutation during plan validation.

## Constitution Check

| Principle | Design evidence |
| --- | --- |
| I: shared human/agent contract, useful offline | Explicit v3 command selection, one typed v3 result, v1/v2 unchanged; no network without online selection. |
| II: evidence before consequential actions | Exact targets/base/proof/version bound to immutable plan; final online/native resampling; recovery confirmed before removal. |
| III: ownership and recovery | Legacy completion is explicit one-operation cooperative authority, never invented original ownership; protected/current/dirty/unknown paths retain. |
| IV: reviewed specification/plan/tasks and test-first behavior | Prior specification99539328 was CLEAR; current stacked-integration specification/plan amendments await renewed review before task finalization, then cross-artifact analysis before implementation. Native/provider failure fixtures precede behavior code. |
| V: native backend capability boundaries | No Git writes for jj/GitButler; explicit new-capability gaps; native minimum/current and unchanged jj conformance. |

Pre-design and post-design assessment: aligned, with no constitution exception. This is author analysis, not independent clearance or acceptance. Provider attribution and legacy completion remain the explicit new trust inputs specified in FR-007/011.

## Project Structure

Artifacts here: `research.md`, `data-model.md`, `contracts/tidy-v3.md`, `contracts/online.md`, `contracts/streams.md`, `quickstart.md`. `tasks.md` is intentionally absent until the plan gate.

Affected source areas, not yet changed:

- `internal/workspace/native_sandbox.go` and new stream/session helpers: factor identical launch validation/grants/environment for bounded buffered, streaming and native-ref-transaction sessions; no generic caller-supplied commands.
- `internal/workspace/git.go`: complete tree/attribute streaming and incremental safety digest; reuse it in Git retirement stored-tree admission.
- New `internal/workspace/tidy_v3*.go`: typed v3 planning/apply/store, expected-state transitions and result rendering inputs. Existing v1/v2 types/dispatch stay valid; no reinterpretation of their JSON.
- `internal/workspace/retirement_manifest*.go`, `retirement_git.go`: explicit version/backend content policy, separate legacy identity admission, bounded hashes and timestamp identities. Old v2/jj call sites explicitly select their original policy.
- New `internal/hosting/tidy_evidence*.go`: typed repository/PR/branch reads over existing guarded transport and one non-resettable budget. Existing hosting result/schema stays unchanged.
- `internal/cli/tidy.go`, new v3 CLI helpers: flags and typed dispatch/rendering only. `internal/contracts`, relevant native/provider unit tests and smoke scripts: conformance fixtures.
- New `schemas/v3/tidy.schema.json`, schema allowlist/copy tests and command/docs/backend guides. Source allowlist tests do not authorize a site build. Apply [contract amendments](contract-amendments.md) only alongside implementation/evidence, with pending status retained until acceptance.

## Dependency-safe implementation units

These are planning boundaries, not executable tasks or permission to start workers. The coordinator assigns authors only after task/analysis gates. Shared files have one author at a time; independent work uses isolated worktrees.

| Unit | Own scope | Prerequisites | Exit evidence |
| --- | --- | --- | --- |
| A: bounded native reads | Sandbox launch factoring; complete tree/filter stream; raw-object stream and byte/deadline ledger; direct-ref transaction session | Reviewed contracts | Unchanged old-runner effects; exact/over bounds, binary framing, cancellation/reaping, helper/network sentinels; native minimum/current. |
| B: v3 model/store | v3 types/schema/options, strict store namespaces, plan/journal validation, policy-bound manifests and outcome transitions | Frozen contract; consumes A only for native fixtures | Old schema/plan bytes unchanged; complete/retained/error/partial examples and negative identity/effect/policy tests. |
| C: provider evidence | Repository identity, complete closed-PR discovery without original-base filtering/hints, selected PR resampling, complete branch advertisements, shared HTTP budget | Frozen online/model contracts | Strict provider fixtures including seven intermediate-base cases among nine exact merged heads, pagination/mutation/error/budget/auth/destination tests; old hosting tests unchanged. |
| D: reference planning/apply | v3 local branches and distinct tracking scope, proof selection, recovery, prepare/readback/CAS, final native/provider revalidation | A+B+C | Actual temporary refs/recovery/outcomes; workspace-use, symbolic swap, remote reappearance and interruption negatives. |
| E: completed retirement | Git v3 owned/legacy observations, attached branch support only in explicit legacy mode, full disk/content proof, coordinated 3/8 sampling and native removal | A+B+C; D's provider-proof helper frozen | Six realistic legacy fixtures; protected/foreign/current/dirty/ignored and concurrent-write negatives; 3/8 sample/read/process/time receipts; branch preserved until fresh D plan. |
| F: CLI/conformance/handoff | Expose reviewed modes, schemas/docs/effects, whole-flow temporary acceptance and fresh two-repository run guide | D+E integrated and independently reviewed | Source checks, minimum/current native lanes, old jj/v1/v2 regression, exact-head independent review; candidate build/install remains a separately authorized next operation. |

A/B/C are independently testable after interfaces freeze; D/E do not duplicate collectors or state-store logic. CLI exposure is last. The metadata fix is tested through creation and old tidy consumers before integration; it does not wait for provider access.

## Validation and delivery gates

1. Independently review this exact plan/artifact SHA. Address every finding before task generation.
2. Generate a traced task list, then independent cross-artifact/security analysis. That gate precedes runtime changes.
3. Implement each behavior from failing fixtures; use disposable repositories and deterministic HTTP doubles, never real cleanup as a test. Freeze schema/model/runner interfaces before dependent authors.
4. Source checks plus relevant race/native minimum/current lanes, effect sentinels, old schemas and jj regression. The real site-build unit test requires separate authorization or an explicit documented exclusion; missing Node/native lanes remain gaps.
5. Independent exact integrated-source review. Then prepare one exact candidate local build/install request for the coordinator; no PR merge or hosted CI is assumed necessary for authorized local dogfood, and no local review authorizes build/install itself.
6. After that separately authorized activation, use [quickstart](quickstart.md) for fresh plans, authorized applies and before/after analysis. Report original 54/13 baseline separately from current refs; do not reuse private RC proofs as fresh authority.

No unresolved design choice is delegated to the operator. Missing execution feasibility is an implementation/acceptance blocker, not permission to weaken these contracts. No runtime check was run for this plan-only change.

## Plan-artifact verification

Offline documentation check passed for263 authored documents; architecture and whitespace checks passed. For the current stacked-integration amendment, the specification, requirements checklist and dependent design files changed; runtime code, published schemas, scripts, dependencies and contract-amendments.md remain unchanged. No full Go/race suite was repeated for this plan-only change. Node and actual Hugo packaging remain outside this check receipt as previously disclosed. The installed planning workflow has no configured pre/post extension hooks. Independent review cleared the prior exact plan `5272dc1e`; the stacked-integration amendment requires renewed exact specification/plan review and is not a task or implementation gate clearance.
