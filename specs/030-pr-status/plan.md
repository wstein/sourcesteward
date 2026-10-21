# Implementation Plan: PR status

**Base**: `2e9b9fc99888b605bcce08afa6ef98846359e06a` | **Status**: Re-scoped 2026-10-21. Slice 1 (one-shot v2, expected head, compact, head lookup) awaits its own independent design review; runtime gated on it. The watch design (D2, D3 and work items WI2 watcher/WI3 delivery) is a deferred slice that keeps its three DESIGN NOT CLEAR results and pending round4.

## Slice split (owner decisions, 2026-10-21)

Slice 1 = WI1 (work package A: v2 schema and decoder), the one-shot half of WI2 (B: one GET, expect-head, minimal shared prelude extraction with characterization), the flag/render half of WI3 (C: `--json-version 2`, `--compact`, `--expect-head`, both entry points; no `--watch`, no delivery arbiter) and the WI4 docs/conformance for those, plus the head lookup `hosting prs SHA` ([contract](contracts/pr-lookup.md)). Deferred slice = the watcher (D), the delivery arbiter and process guard (E), D2's 30s/8 GET/backoff/reserve constants as watch parameters, D3 and descendant verification; they are untouched design history. A separate small spec later adds a checks class reusing `hosting ci`; `scripts/ci-wait.sh` stays the long-wait tool until a watch exists.

Slice 1 and the design blockers: the blockers of rounds 1 to 3 and the pending round 4 concern only watch final delivery; one-shot keeps the existing 30 second operation context and writer contract, adds no teardown phase, helper, process exit or descendant claim, so they do not apply to slice 1 (see the spec). Slice 1 still needs its own exact-head review.

Head lookup design: new leaf kind `prs` (not a `pr` flag; reasons in the contract). One additive allowlist route `commits/SHA/pulls?per_page=100&page=1` in `internal/hosting/transport.go` (`fixedEndpoint`), one typed reader and list decoder reusing the lifecycle table with `merged` derived from `merged_at`; no pagination (truncation disclosed), no follow-up request, GET only, same 401/403/404/429 stop with no retry. The new route is the only transport change; existing `tidyRoute` is untouched.

Slice-1 file scope (still not runtime ownership authorization): new `internal/hosting/pr_status_result.go`, `pr_status_decode.go`, `pr_status.go`, `pr_lookup.go` and tests; new `schemas/v2/hosting-pr.schema.json` and `hosting-prs.schema.json`; minimal edits to `operations.go`/`result.go`/`transport.go` (shared with Ready/tidy consumers: dispatch hands off current heads first), `internal/cli/hosting.go`, both cmd entry points for the new flags and leaf. The held runtime branch `origin/codex/hold-spec030-runtime` (typed lifecycle model and decoder, one-shot assembly) may be reused for A and B after review.

## Context and inspected evidence

Go1.27.2, Cobra1.10.2, pinned go-gh/v2 2.16.2 and the existing JSON Schema library; standard library first, no added dependency/storage. Linux/macOS remain the supported platform scope. `internal/hosting/result.go` Options/Result/PullRequest serve repo/pr/ci. `operations.go` readPR is shared with CI/resampling; `fullSHA` is exactly 40 lowercase hex. Existing inspect owns a 30-second context through operation return; CLI delivery follows outside it. Existing v1 schema permits future properties, confirmed by `internal/contracts/hosting_test.go`; checkpoint claims of strictness were incorrect. V1 guarantees its current required fields and version const, not rejection of extra fields.

`transport.go` authenticates with bounded gh in a neutral directory and owns typed destination/body/redirect guards. Do not duplicate that transport or introduce arbitrary HTTP URLs, new token access, retrying clients or global environment changes. The new enhanced decoder accepts explicit null head repository; legacy/CI readers keep their behavior. Source-side fixture seams are test-only; production gains no endpoint override.

## Decisions and alternatives

D1 (proposed): v1 remains default JSON; explicit `--json-version 2` selects the enhanced operation. Default human detail is retained with added evidence; compact is opt-in. Rejected default-v2 silently changes existing machine callers. Rejected v1 additive-only delivery does not provide a separately guaranteed lifecycle/watch contract. Tradeoff: legacy JSON intentionally cannot access the new watch/expect flags without explicit migration.

D2 (proposed): one final observation, fixed 30s global upper bound, 8 GETs, 1/2/4s capped backoff, 1s internal delivery reserve. No cap raise or new duration flags. These values make explicit short observation useful while keeping existing total bounds; they are reviewable conservative choices, not measured product-performance targets. A longer-running watch requires a separately reviewed contract, not a loop of unbounded 30s sessions.

D3 (final-delivery C component previously CLEAR; lifetime replacement proposed): [accepted-baseline lifetime](baseline-lifetime.md) retains direct native/auth CommandContext/Run/Wait/capture completion, full authentication and actual bwrap/sandbox-exec flags. It withdraws the author's exhaustive process-tree certificate and discloses inherited descendant limits. Bounded owned HTTP abort/close and private leaf-helper completion precede immutable prepared output; cmd-only C covers formatting/stdout/root stderr until the original D without a disarm gap. No library exit, arbitrary-writer goroutine, process metadata scan, added group control, guardian, token-only or platform restriction. Completion means the enumerated owned calls returned; it does not certify every inherited descendant stopped. New helper/residue/transport/entry surfaces and any concrete accepted-contract conflict were separately assessed as technically honest, with no material new descendant risk/accepted-contract conflict requiring an owner question. That assessment was not formal DESIGN CLEAR; helper/residue, HTTP/late-dial/timing/platform and both-entry conformance remain implementation acceptance obligations. All three prior negative gates remain counted. Runtime remains gated on complete formal DESIGN CLEAR and explicit file handoff.

D4: lifecycle comes only from coherent state/merged/timestamp evidence, result identity stays separate from head, and errors retain last samples only as incomplete timestamped evidence. No cross-endpoint or review/attestation inference.

## Constitution check

I: shared enhanced human/v2 model, explicit legacy format path, noninteractive one final output; D3 requires separate command-host delivery capability and guarded process exit, with no claim for arbitrary library writers. II/III: read-only observations, no plans/actions or target writes; preserve existing ownership/config/auth refusal. IV: complete specification/plan/tasks precede code and independent review; TDD, real native sentinels and explicit source-vs-acceptance limits. V: both Tier 1 backends share hosting operations without substituting Git inside jj. Existing native versions, sandbox, snapshot suppression and operation pinning stay. No constitutional amendment is proposed. Revisions to the unapproved inclusive-output proposal are engineering choices; escalate only a material accepted-contract change or independently assessed unresolved risk.

## Work-item and dependency ledger

| Item | Proposed file scope | Prerequisite / owner |
| --- | --- | --- |
| WI0 design | `specs/030-pr-status/**` only | This author; strongest independent complete-artifact review via dispatch |
| WI1 result/decoder | New `internal/hosting/pr_status_result.go`, `pr_status_decode.go`, tests; new `schemas/v2/hosting-pr.schema.json`, contract fixtures | WI0 CLEAR + explicit D3 acceptance/file handoff; dispatch grants runtime files |
| WI2 orchestration | New `internal/hosting/pr_status.go`, watch tests; minimal shared prelude extraction only with characterization | WI1; reconcile F tidy/Ready transport/prelude consumers first |
| WI3 CLI/delivery | New enhanced PR renderer/parser seam; `internal/cli/hosting.go`, root boundary, both cmd entry points and a cmd-owned final-delivery arbiter | WI2 + explicit file handoff; immutable prepared output after enumerated owned completion, bounded cleanup and process-guard acceptance |
| WI4 docs/acceptance | hosting/JSON/agent documentation, progress/status/evidence and source smoke observer only as needed | WI3, exact implementation review and actual checks |

This plan names likely files, not runtime ownership authorization. Shared `operations.go`, `result.go`, `transport.go` and CLI root have current F tidy and Ready consumers. Before WI1/2, dispatch supplies their current exact heads, accepted interfaces and integration order; re-inspect them before any shared edit. Prefer extracting a shared resolution/auth prelude with existing `Inspect` characterized green; never couple hosting PR status to tidy or Ready. No transport endpoint expansion or classification dependency is needed: every HTTP failure stops using existing safe messages. Specs 028/029 are excluded. Old review clearance never transfers across head changes.

## Design verification and acceptance boundary

For this spec-only revision: offline documentation checker, JSON syntax/schema compilation and fixture validation with the already pinned library, targeted schema-invalid mutants, temporary source-only output-deadline probe and whitespace. No full Go suite/race is needed for design-only edits. Future runtime work requires `make check`, `go test -race ./...`, module verification, architecture checks and minimum/current native conformance per repository instructions. No CLI/site/image/artifact build or installation is authorized. Generated command reference and real binary smoke remain blocked on separately authorized builds; do not use `go run` as a build bypass.
