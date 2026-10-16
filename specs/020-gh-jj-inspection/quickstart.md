# Quickstart validation

This guide describes acceptance after implementation, not pre-existing feature availability. Use temporary repositories; no live mutation or installation is needed.

## Prerequisites

Use pinned project Go, supported Git/jj and existing native read-only sandbox tooling. gh is optional for local resolution. Deterministic tests use a fake gh binary, not production credentials. Existing minimum/current native CI lanes are mandatory acceptance evidence.

## Source checks

```sh
go test ./internal/hosting ./internal/cli ./internal/inspection
go test -race ./...
make check
```

Record actual results and native-conformance executions in `verification.md`; skipped mandatory native lanes are remaining gates, never passes. Validate complete success/error fixtures through the existing offline schema test runner and regress old published schemas.

## Local resolution

In temporary Git/jj fixtures, configure one HTTPS/SSH GitHub remote, then invoke the source entry points:

```sh
go run ./cmd/stw hosting repo --json PATH
go run ./cmd/gh-jj repo --json JJ_PATH
```

Expect schema_version1, selected native identity and locally resolved repository. Remove gh from PATH and repeat: resolution still succeeds. Git worktree and both colocated/non-colocated jj must resolve without changing refs, working copy or jj operation log. Ordinary Git through gh-jj refuses. Fork+upstream candidates refuse until explicitly selected with `--repo OWNER/NAME` or `--remote NAME`.

## PR and CI observation

Use fixture-controlled gh responses for these commands; live read smoke is optional and requires existing user authentication but never writes:

```sh
go run ./cmd/stw hosting pr 42 --repo example/project --json PATH
go run ./cmd/gh-jj ci 42 --repo example/project --json JJ_PATH
```

Verify explicit GET host/repository paths and exact PR-head checks/status requests. Simulate moved head, 403, malformed JSON, empty collections, multi-page data, output/record/page bounds, missing gh and cancellation. Only complete successfully fetched empty collections may produce empty success; all other incomplete cases return operational error with boundary.complete=false.

## Entry-point/effect equivalence

Compare normalized stw/gh-jj JSON on the same jj fixture, excluding observation timestamps. Capture repository refs/files, jj operation log and stored working-copy identity before/after. Helper/pager/network-write sentinels remain unexecuted. A private temporary GH_CONFIG_DIR fixture may install the locally built extension and verify real `gh jj` dispatch. No live user extension installation, release, push, merge, cleanup or CI rerun is performed by this guide.
