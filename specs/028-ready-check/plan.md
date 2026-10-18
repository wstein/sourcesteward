# Implementation Plan: Ready inspection and review evidence

**Branch**: `codex/64-ready-evidence-contract` | **Date**: 2026-10-18 | **Spec**: [spec.md](spec.md)

**Status**: Proposed plan; specification gate CLEAR at `4dbfd8e557148f315daf8c00c932d9f5a6b5aa46`; plan gate pending. No tasks, implementation or deployment.

## Summary

Add a separate read-only Ready operation beside existing hosting inspection. Typed bounded readers supply immutable policy and sampled evidence to a pure evaluator. The evaluator exposes review, CI, constraints and deployment components; the hosted publisher uses only its review component. This avoids self-dependency while retaining strict final CLI eligibility. Spec 025 remains explanatory and is not a prerequisite.

The [command](contracts/ready-check.md), [policy](contracts/project-policy.md), [report](contracts/review-report.md) and [publisher](contracts/github-check.md) contracts are normative for this proposed design. [Research](research.md) distinguishes measured facts from documented capabilities and unresolved deployment acceptance. No deployment option is selected by this plan.

## Technical Context

**Language/Version**: Existing Go 1.27.2 module; Go standard library for parsing, bounds and evaluation.

**Primary Dependencies**: Existing Cobra and go-gh pins; existing jsonschema/v6 for offline contract tests. No new runtime library.

**Storage**: No CLI persistence. Caller-owned policy lock; immutable remote policy bytes; public digest-bound report comments. Publisher idempotency uses hosted check IDs and controller concurrency, not CLI state files. Publication serialization uses the actual repository/head/context/App key shared by all PRs on that commit.

**Testing**: Table-driven evaluator/parser tests; HTTP fixtures; temporary Git/jj effects fixtures; CLI human/JSON fixtures; existing source/race checks. Hosted security acceptance separately authorized.

**Target Platform**: Existing Linux/macOS native hosting support. First deployed GitHub profile is selected only after its capability/security acceptance. Unsupported Enterprise behavior fails closed.

**Performance Goals**: 30 seconds, 80 requests, 1 MiB response/result, bounded collection/report sizes in the command contract. No retries/polling.

**Constraints**: CLI GET-only; no inspected-target write, native ownership bypass, model invocation, untrusted PR execution or arbitrary endpoint. Publisher writes only its own authenticated check records.

**Scale/Scope**: One repository/PR, full-PR review, one base branch and one approved policy generation. Rebase is the only v1 merge method. GitHub-native review eligibility/conversation proof and merge queues are unsupported, explicitly blocking final eligibility when required.

## Constitution Check

| Principle | Before research and after design |
| --- | --- |
| I shared contract | One evaluator/result; human and versioned JSON; bounded noninteractive operation. Local features retain no cloud requirement. |
| II evidence | Read-only command grants no mutation permission; publication resamples bindings; no atomic-merge claim. |
| III ownership | Existing native discovery preserved; private native isolation effects disclosed; no silent stash/reset/checkout. |
| IV reviewed specification and tests | Specification gate cleared; plan gate precedes tasks; independent full-artifact CLEAR precedes test-first implementation. |
| V Tier 1 | Shared hosting decisions on Git/jj, existing jj sandbox/pinning and identity evidence retained; gh jj remains jj-only. |
| Workflow effects | No build, settings, credential, App or activation authority. Deployment security profile remains unselected. |

No constitutional exception is requested. Proposed publisher writes are a separately specified hosting-check capability, not push/merge automation. They require explicit deployment authorization and never enter `stw hosting`'s read-only transport.

## Project Structure

Design artifacts are this plan, research, data model, four contracts, three unpublished input schemas/examples, quickstart and analysis record. The installed plan review gate precedes creation of `tasks.md`.

Future source allocation (not created by this plan):

```text
internal/readiness/policy.go, report.go, evaluate.go + matching tests
internal/hosting/ready.go, ready_sources.go, ready_policy.go + matching tests
internal/cli/ready.go + matching tests and testdata/ready-*.json
scripts/review-evidence.py + scripts/test_review_evidence.py
schemas/v1/ready-check.schema.json, ready-policy.schema.json, review-report.schema.json
```

`internal/readiness` is pure: no filesystem, process, time or network reads. Hosting owns lock loading and typed HTTP readers. CLI imports hosting operations only; hosting returns the public result. The publisher script runs an already-approved immutable `stw` executable in a trusted checkout, validates its JSON and writes its review projection. It never implements review predicates itself. Its eventual workflow belongs to the selected deployment profile; no active workflow is created merely by implementing the parser/evaluator.

Architecture checks extend their pure-package/import enforcement. Existing hosting functions/results stay compatible; richer decoders are separate. Any shared transport extension is additive with unchanged old endpoint tests. Coordination with future Spec 025 reader work must avoid parallel ownership of transport files.

## Design Decisions

1. **Separate projections.** `review` depends on policy, report/status authenticity, full changed-file scope, exactly one open PR associated with the head, and stable review/full-activation bindings. `ci`, `constraints` and `deployment` have separate evidence dependencies. Final CLI readiness requires all four. Publisher validates the entire result's integrity and complete passing review component; a complete review may be published while unrelated CI is pending. Cancellation, changed bindings or exhausted global budgets invalidate review publication.
2. **Caller trust versus deployed trust.** The lock is an explicit caller trust input, not discovered in the PR. Undeployed evaluation may assess policy locally with `deployment=not-required` only if the policy says so; output labels this caller-policy eligibility. Installed profiles require an authenticated active-generation read and an exact matching deployed check. Never label an old caller lock current merely because its JSON is valid.
3. **Approved baseline.** Fixed policy path at a full pinned source SHA plus raw-byte SHA-256; owner-controlled lock carries checker/source identity. No PR policy autoload. The old baseline checks proposed policy/root changes; activation is independent. See the transition state machine in the policy contract.
4. **Public review input.** The newest selected review status supplies the expected report digest and a strictly parsed same-repository issue-comment locator. Read creator and comment-author numeric IDs from authenticated API records. No transcript/session ID required. v1 accepts accountable declared independent execution and configured model metadata only under explicit policy; runtime attestation remains unsupported.
5. **Full scope, deterministic rules.** Read the complete changed-file list, including previous filenames, and require a full-PR review. Prefix rules may raise the required model strength. A report cannot self-classify its paths or downgrade tier. No complex glob engine and no inherited review exception.
6. **Fail closed on effective policy.** Classic and effective rulesets must be known. Unknown permission outcomes never mean absent. Exact-head required-check success, explicit allowed rebase and REST rebaseability are distinct predicates. Unsupported effective requirements remain indeterminate, even when GitHub reports a clean aggregate state.
7. **Blocking publisher lifecycle.** Before reevaluation, revoke previous owned successes and confirm blocking state. Every observed failure/unknown/read error or collision must produce failure, never neutral/skipped or a retained old success; unconfirmed writes produce enforcement-unknown. A complete singleton open-PR set is resampled around publication. Full typed activation bytes, including transition/null, are authenticated and bound to the output; unchanged generation does not excuse transition drift.
8. **Versioned inputs, strict semantics.** Closed JSON input schemas, duplicate-key/depth/size checks, exact version matching and separate semantic checks. Schema validation alone cannot prove authorization or completeness. All input examples are fictitious structure fixtures, not live policy.
9. **Publication isolation.** The generic script has a fixed check-write adapter, distinct from the CLI GET client. App credentials never enter untrusted PR jobs; no arbitrary command or URL from evidence reaches a process/API write. Platform-native required-workflow or human-mediated profiles are considered before asserting any new App is necessary.
10. **No automatic build.** A deployed wrapper consumes an independently authorized immutable artifact/digest. It does not build on events, run `go run`, install extensions, or activate a development binary. Source test fixture compilation remains routine verification.

## Verification design

Write failing tests for each failure-matrix row before behavior. Exercise public-input shape and semantic validation separately; table-test deterministic evaluation and projection isolation. Include source/app spoofing, old-success selection, duplicate identities, policy transitions (including transition-only tampering), stale-success revocation, same-head multi-PR collisions, test-merge/head separation, missing effective constraints, limits and final resample races. Test full Git/jj effects through the existing real-native/fake-HTTP seam. Preserve old hosting schema/fixture bytes. Test publisher event idempotency and concurrency using a fake write adapter without credentials; hosted source-spoof/race acceptance requires a selected authorized profile.

Current design validation is documented in [analysis.md](analysis.md). The eventual implementation gate requires complete source/race checks and independent exact-SHA review. Artifact builds, hosted acceptance and rollout remain separate named operations. No task is considered implemented by a schema example or design check.

## Complexity Tracking

Three input documents are necessary to separate caller/owner authority, project rules and review claims. A separate publisher is necessary because the CLI remains read-only. No service, database, generic policy language, external signing system or new model runner is introduced. Infrastructure selection is deferred to an owner decision on concrete accepted alternatives, not silently selected here.
