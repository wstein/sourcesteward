# Validation Guide

Use only disposable repositories. Install the manifest-pinned Git/jj versions and Go prerequisites; run from this worktree with the documented Go caches.

1. Run `make check` and `go test -race ./...`; record actual versions and any baseline failures.
2. Build `bin/stw` with `make build`. Inspect an ordinary temporary Git repository: existing schema1 output and human identity must match prior behavior even with jj installed.
3. Create disposable standalone and colocated jj repositories using the supported native `jj git init` modes. Inspect root and nested paths; compare full workspace/change/commit/operation identity with independently queried native evidence.
4. Create a secondary jj workspace with `jj workspace add` and inspect it. Confirm its distinct workspace root/name and shared repository path.
5. Modify tracked/untracked files; create stale workspace and divergent operation-head fixtures through native setup commands. Snapshot fixture files and metadata before invoking `stw inspect`; compare afterward. Native setup mutations are fixture preparation, never part of inspection.
6. Exercise missing/incompatible jj, malformed markers, ambiguous GitButler evidence, hostile config/env/helper sentinels and runner cancellation/concurrency cases. Expect one documented failure, no fallback and no effects.
7. Run required conformance mode via the repository script in both pinned version environments; absence of a tool or mandatory case must fail.
8. Validate real JSON outputs and independent negatives offline; package Pages and verify both v1 schemas remain byte-identical and v2 is served at its stable project path.

The tests/scripts implementing these checks are task deliverables; this guide does not claim they already pass. No live publication claim follows from local packaging. Independent review receives exact commit and complete evidence.
