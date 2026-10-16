# Implementation Plan: Submodule pin audit

**Branch**: `codex/021-submodule-pin-audit` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw pins`, a read-only audit of a Git superproject's submodule pins. One bounded operation in `internal/inspection` detects ownership from markers, reads registration from the HEAD blob of `.gitmodules` plus the gitlinks of HEAD and the index, validates each submodule's worktree and Git directory, gathers checkout, dirty and publication evidence through an isolated streaming Git runner, derives stable findings, and returns one typed result that the CLI renders as text or as versioned JSON. jj, GitButler, ambiguous and bare cases refuse with an explicit capability statement before any process starts.

## Technical Context

**Language/Version**: Go 1.27.2; Git 2.46 or newer (the existing minimum and current lanes).  
**Primary Dependencies**: Existing Cobra v1.10.2, the standard library and the existing JSON Schema test dependency. No new dependency.  
**Storage**: None. No plan files, recovery records or cache.  
**Testing**: Go table tests with real temporary Git repositories, schema fixtures, `scripts/smoke-pins.py` with its unit test, effect sentinels, existing architecture and documentation checks.  
**Target Platform**: Code supports linux amd64/arm64 and darwin arm64. CI evidence exists for ubuntu-24.04 amd64 only; arm64 and darwin results are recorded as local evidence.  
**Project Type**: CLI with a shared operation layer.  
**Performance Goals**: 5 seconds per submodule, 30 seconds total, 1 MiB retained output, at most 256 submodules.  
**Constraints**: No network, fetch, prompt, pager, hook, filter, helper, alias or write; submodule metadata is untrusted; nested submodules are not traversed.  
**Scale/Scope**: One superproject worktree and its direct submodules.

## Constitution Check

Pre-research assessment: PASS. Post-design assessment (after analysis findings were resolved): PASS.

- **I. Shared contract**: one typed result renders text and JSON; non-interactive; exit statuses 0, 1, 2 and the documented `--strict` status 3 with a stated precedence.
- **II. Evidence before actions**: read-only; the result states what it observed and could not, and that remote-tracking refs may be stale. It authorizes nothing.
- **III. Ownership and recovery**: no mutation and so no recovery claim. Ownership is verified from markers first. The audit checks submodule ownership itself because an explicit Git directory bypasses Git's `safe.directory` check.
- **IV. Specify and verify**: failing tests first; independent analysis before code; real temporary repositories; hostile fixtures, unusual names, concurrent change, interruption and limits; effect sentinels including a network sentinel and, on the refusal path, jj working-copy and operation-log sentinels.
- **V. Tier 1 peers**: Git is supported and conformance-tested. jj and GitButler are explicitly unsupported for submodules; the refusal is tested on the jj lanes and recorded in the backend contract. No equivalence is claimed.
- **Scope**: network is not used, and the result says so. Pin updates, fetch, push, hosting comparison and CI lanes for more platforms are separate specifications. No amendment or exception is required.

## Project Structure

```text
internal/inspection/pins.go          # entry, marker-based ownership dispatch, refusals, resample
internal/inspection/pins_result.go   # result types, finding table, option validation
internal/inspection/pins_git.go      # registry, per-submodule evidence, publication, findings
internal/inspection/pins_safe.go     # worktree and Git-directory validation, ownership check
internal/inspection/pins_run.go      # streaming isolated Git runner with scan and retained caps
internal/inspection/pins_*_test.go   # temporary-repository tests, hostile fixtures, sentinels
internal/cli/pins.go                 # command, flags, text rendering
internal/cli/root.go                 # typed exit error for --strict findings (status 3)
schemas/v1/pins.schema.json          # new contract; existing contracts unchanged
internal/cli/testdata/pins-*.json    # schema-valid success, incomplete, refused and error observations
internal/contracts/pins_test.go      # fixtures against the schema, existing fixtures still valid
scripts/smoke-pins.py                # native conformance smoke (Git lanes, jj refusal lane)
scripts/test_pins_smoke.py           # unit test of the smoke script
docs/pins.md, docs/backends.md       # guide and capability matrix
docs/agent-guide.md, docs/json-contracts.md  # exit status 3 and the new contract
```

The Git adapter stays inside `internal/inspection`, as for inventory, so the import and effects boundary (`scripts/check-architecture.py`) needs no exception: `internal/cli` renders and never starts a process.

## Design Decisions

1. **Registry from three sources.** HEAD tree gitlinks and index entries (mode 160000) give pins; the HEAD blob of `.gitmodules` (read with includes disabled) gives names, URLs and tracking branches; the staged blob and the worktree file are compared and differences become `gitmodules-modified`. A path present in one source and not another produces a finding, not a refusal. A worktree symlink is never followed.
2. **Validated worktree and Git directory.** The plan does not assume a shared `modules/` directory. Git keeps `modules` per worktree: in a main worktree it is `<git-dir>/modules`, in a linked worktree `<git-dir>/worktrees/<id>/modules`, and this was measured (`rev-parse --git-path modules` returned different paths, and the submodule pointer read `gitdir: ../../super/.git/worktrees/wt/modules/member`). The operation obtains the expected location from `rev-parse --git-path modules` for the worktree in use and accepts only a Git directory equal to that path joined with the submodule name, or an in-place `.git` directory inside the submodule. It reads the `.git` pointer as data, then refuses a submodule whose Git directory contains a `commondir` file or `objects/info/alternates` entry that leads outside the superproject's Git directory tree, or whose `core.worktree` differs from the validated worktree. Git always runs with explicit `--git-dir` and `--work-tree`. Directory identity (device and inode) is recorded and compared again before success.
3. **Ownership checked by the audit.** An explicit `--git-dir` makes Git skip its own `safe.directory` check, so `pins_safe.go` compares the owner of each submodule worktree and Git directory with the effective user before any Git process runs there and refuses with `submodule-unsafe-owner`. The check is Unix-only, matching the supported platforms.
4. **Publication through containment.** One bounded call per pin, `for-each-ref --contains <pin> --count=17 --format=... refs/remotes/`, names at most 16 refs and detects truncation. Local branches and tags are never consulted. The advisory freshness value is the modification time of `FETCH_HEAD` in the submodule Git directory when it can be read.
5. **Dirty state without asking Git to hash content.** `git status` can run clean and process filters on stat-dirty entries. The inventory implementation already avoids it ("never asks Git to hash worktree contents") and keeps the filter-neutralizing prefix. Pins reuses that comparison for tracked state, reports `unknown` where deciding would need a filter, and finds untracked presence with a bounded `ls-files --others --exclude-standard` that stops at the first record. Both are tested with filter sentinels and an index sentinel.
6. **Streaming runner and caps.** The shared runner caps each stream at 64 KiB, which is too small for `ls-files --stage` on a large superproject. `pins_run.go` consumes output incrementally, keeps only the records it needs (gitlink entries, `.gitmodules` keys), bounds retained bytes to the 1 MiB budget and the scan to a stated allowance, and reports `pins-limit` instead of truncating. Counts use two `rev-list --count --max-count=10001` calls (one per side), so each side is capped separately.
7. **Detection from markers.** Ownership detection reads `.jj`, `.git` and GitButler markers and starts no process. The existing `Inspect` path may run sandboxed jj and fail with `jj-sandbox-unavailable`, which would make a refusal depend on a sandbox. If marker detection cannot decide, the result is `ambiguous-backend`.
8. **Resample.** Superproject HEAD, index identity (size, modification time and object hash of the index file, read without refreshing) and each submodule HEAD are read again before success; a difference yields `pins-state-changed`.
9. **Findings are derived, not stored.** A single table in the spec maps evidence to stable codes and severities; the schema pins the code list.
10. **Exit statuses.** `internal/cli/root.go` gains one typed error for strict findings (status 3). Precedence: usage 2, then refusal, operational failure, cancellation or incomplete result 1, then strict findings 3, then 0.
11. **In-place `.git` directories** inside a submodule are supported (old-style), validated by the same containment and ownership rules.

## Risks

- Git read paths can update the index opportunistically; sentinels on index bytes and modification time guard this, and every invocation runs with optional locks disabled.
- Reusing the inventory comparison for dirty state may need a small refactor to run against an explicit Git directory; the work item records the refactor as its own commit with the existing inventory tests as the safety net.
- `FETCH_HEAD` freshness is advisory and may be absent; the schema models it as nullable.
- Non-UTF-8 paths cannot be represented in JSON; the entry is refused with `submodule-path-unsafe` rather than encoded.
