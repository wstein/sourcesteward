# Tasks: Bounded Repository Inspection

**Input**: [spec](spec.md), [plan](plan.md), [research](research.md), [model](data-model.md), [contract](contracts/inspect.md)

## Phase 1: Setup and design gate
- [x] T001 Prepare feature artifacts in specs/005-repository-inspection using installed Spec Kit flow.
- [x] T002 Obtain independent artifact analysis/research review and resolve findings in specs/005-repository-inspection before root releases implementation.
- [x] T003 Establish baseline `make check` and record results in docs/inspection-verification.md.

## Phase 2: Operation boundary
- [x] T004 Write failing architecture tests in scripts/test_architecture.py allowing only internal/cli → internal/inspection while forbidding os/exec/network/filesystem access in CLI.
- [x] T005 Define typed result/errors in internal/inspection/result.go and evolve scripts/check-architecture.py plus ARCHITECTURE.md deliberately.

## Phase 3: US1 — Ordinary identity
**Independent test**: temporary normal/nested/linked/bare/unborn/detached/SHA-256/corrupt-HEAD/malformed-branch-ref/noncommit-HEAD repositories match typed identity, unchanged repository bytes.
- [x] T006 [US1] Add failing fixtures and expected identity/HEAD assertions in internal/inspection/inspection_test.go before implementation.
- [x] T007 [US1] Implement absolute path discovery, fixed plumbing, result validation and HEAD sampling in internal/inspection/inspection.go and git.go.
- [x] T008 [US1] Add failing parsing/JSON/human/output-error tests in internal/cli/inspect_test.go; implement internal/cli/inspect.go and register in root.go.

## Phase 4: US2 — Managed ownership
**Independent test**: recognized jj/GitButler/ambiguous fixtures refuse inspection with explicit evidence and no native backend invocation.
- [x] T009 [US2] Add failing marker/private-common-dir/packed-ref/prefix-collision/symlink/nested-boundary/malformed/inaccessible tests in internal/inspection/detection_test.go.
- [x] T010 [US2] Implement conservative bounded detection in internal/inspection/detection.go, stop managed inspection before HEAD work, retain sorted evidence.
- [x] T011 [US2] Test unsupported/ambiguous human+JSON diagnostics and absence of partial-success stdout (JSON error object remains required) in internal/cli/inspect_test.go.

## Phase 5: US3 — Effects and failures
**Independent test**: snapshots and sentinels show no writes/hooks/network/trace; invalid paths/env/cancellation/concurrent HEAD fail correctly.
- [x] T012 [US3] Write failing hostile GIT_* environment, changed-HEAD, bounded output, missing executable, unsafe ownership, invalid UTF-8 and cancellation tests in internal/inspection/git_test.go and inspection_test.go.
- [x] T013 [US3] Implement sanitized environment, bounded process capture, no-lazy-fetch/no-lock/no-prompt controls and contextual diagnostics in internal/inspection/git.go.
- [x] T014 [US3] Add real-process CLI smoke scenarios and stderr cardinality assertions in scripts/smoke-cli.py or a documented new inspection smoke script.

## Phase 6: Analysis, verification and handoff
- [x] T015 Update AGENTS.md, README.md, docs/{agent-guide,backends,roadmap,harness,README}.md and llms.txt to distinguish implemented identity inspection from deferred adapters/mutations.
- [x] T016 Run gofmt, make check, go test -race ./..., real-process acceptance, snapshots/sentinels and git diff --check; record actual evidence and limitations in docs/inspection-verification.md.
- [x] T017 Perform final Spec Kit cross-artifact analyze in specs/005-repository-inspection/analysis.md; fix consistency findings.
- [x] T018 Obtain independent exact-revision source review, fix concrete findings, update docs/progress.md and docs/feature-status.json, and create focused Conventional Commits without pushing/merging.

## Dependencies and execution
T002 blocks source implementation. T003–T005 precede US1. US2 detection fixtures can be designed independently, but detection integrates before ordinary HEAD inspection. US3 safety controls must pass before any handoff; US1 is not publishable alone. Each behavior test must fail before its implementation. No author publication.

## Parallel opportunities
Independent reviewer analyzes artifacts/source while author works only on released tasks. No concurrent edits to shared operation files. US1 fixture design and US2 marker research may proceed in separate files; integrate sequentially.

## Implementation strategy
Deliver one bounded feature, never expand into status, cleanup or native backend adapters. Validate each story locally, then all stories together, analyze artifacts and obtain independent review before root external handoff.
