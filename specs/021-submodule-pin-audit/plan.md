# Implementation Plan: Submodule pin audit

**Branch**: `codex/021-submodule-pin-audit` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw pins`, a read-only audit of a Git superproject's submodule pins. One bounded operation in `internal/inspection` enumerates gitlinks, `.gitmodules` entries and staged pins, validates every submodule path and Git directory, gathers checkout, dirty and publication evidence through the existing isolated Git runner, derives stable findings, and returns one typed result that the CLI renders as text or as versioned JSON. jj, GitButler and bare repositories refuse with an explicit capability statement.

## Technical Context

**Language/Version**: Go 1.27.2; Git 2.46 or newer (minimum and current conformance lanes).  
**Primary Dependencies**: Existing Cobra v1.10.2, the standard library and the existing JSON Schema test dependency. No new dependency.  
**Storage**: None. No plan files, no recovery records, no cache. Existing private temporary files used by native jj inspection are not involved because the jj path refuses before any read.  
**Testing**: Go table tests with real temporary Git repositories, schema fixtures, `scripts/smoke-pins.py` with its unit test, effect sentinels, existing architecture and documentation checks.  
**Target Platform**: linux amd64, linux arm64, darwin arm64. Other platforms are out of scope and not claimed.  
**Project Type**: CLI with a shared operation layer.  
**Performance Goals**: 5 seconds per submodule, 30 seconds total, 1 MiB of subprocess output, at most 256 submodules.  
**Constraints**: No network, fetch, prompt, pager, hook, filter, helper, alias or write; submodule metadata is untrusted; nested submodules are not traversed.  
**Scale/Scope**: One superproject worktree and its direct submodules.

## Constitution Check

Pre-research assessment: PASS. Post-design assessment: PASS.

- **I. Shared contract**: one typed result renders text and JSON; non-interactive; exit 0, 1, 2 and the documented `--strict` status 3.
- **II. Evidence before actions**: read-only; the result states what it observed, what it could not, and that remote-tracking refs may be stale. It authorizes nothing.
- **III. Ownership and recovery**: no mutation, so no recovery claim. Linked worktrees are read, never changed; ownership is verified first.
- **IV. Specify and verify**: failing tests first; independent analysis before code; real temporary repositories; hostile fixtures, unusual names, concurrent change, interruption and limits; effect sentinels.
- **V. Tier 1 peers**: Git is supported and conformance-tested. jj and GitButler are explicitly unsupported for submodules; the refusal is tested on the minimum and current jj lanes and recorded in the backend contract. No equivalence is claimed.
- **Scope**: network is not used, and the result says so. Pin updates, fetch, push and hosting comparison are separate specifications. No amendment or exception is required.

## Project Structure

```text
internal/inspection/pins.go          # entry, ownership dispatch, refusals, resample
internal/inspection/pins_result.go   # result types, finding catalog, option validation
internal/inspection/pins_git.go      # registry enumeration, per-submodule evidence, publication
internal/inspection/pins_safe.go     # path and Git-directory validation (no symlinks, inside root)
internal/inspection/pins_*_test.go   # temporary-repository tests, hostile fixtures, sentinels
internal/cli/pins.go                 # command, flags, text rendering, strict exit status
internal/cli/root.go                 # one new exit status (3) for --strict findings
schemas/v1/pins.schema.json          # new contract; existing contracts unchanged
internal/cli/testdata/pins-*.json    # schema-valid success and error observations
internal/contracts/pins_test.go      # fixtures against the schema, existing fixtures still valid
scripts/smoke-pins.py                # native conformance smoke (Git lanes, jj refusal lane)
scripts/test_pins_smoke.py           # unit test of the smoke script
docs/pins.md, docs/backends.md       # user guide and capability matrix
```

The Git adapter stays inside `internal/inspection`, as for inventory, so the existing import and effects boundary (`scripts/check-architecture.py`) needs no exception: `internal/cli` renders and never starts a process.

## Design Decisions

1. **Registry from three sources.** The HEAD tree gives recorded pins, the index gives staged pins, `.gitmodules` gives names, URLs and tracking branches. A path present in one source and not another produces a finding, not a refusal.
2. **Validated Git directory only.** For each submodule the operation resolves the working directory without following symlinks and reads the `.git` pointer as data. A Git directory outside the superproject's common directory `modules/` (or an in-place `.git` directory inside the submodule) refuses that submodule. Git then runs with an explicit validated `--git-dir`, never with a pointer taken from the submodule.
3. **Publication is remote-tracking evidence.** `merge-base --is-ancestor` against each `refs/remotes/*` tip with an upper bound of refs examined. Local branches and tags never count. The advisory freshness value is the modification time of `FETCH_HEAD` in the submodule Git directory when it can be read.
4. **Dirty state without refresh.** `git status --porcelain=v2 -z --untracked-files=no --ignore-submodules=all` with optional locks disabled for tracked changes, and a bounded `ls-files --others --exclude-standard` stop at the first record for untracked presence. Both are tested not to touch the index.
5. **Resample.** Superproject HEAD, index identity and each submodule HEAD are read again before success; a difference yields `state_changed_during_audit` and an incomplete result.
6. **Findings are derived, not stored.** A single table maps evidence to stable codes and severities; the schema pins the code list so agents can depend on it.
7. **Strict status.** `--strict` adds exit status 3 when an error finding exists. The result is still emitted in full.

## Risks

- Git's read paths can update the index opportunistically; sentinels on index bytes and modification time guard this, and the dirty check uses the isolated, optional-lock-free form.
- A submodule that Git refuses (foreign owner) cannot be audited; the result says so rather than overriding `safe.directory`.
- `FETCH_HEAD` freshness is advisory and may be absent; the schema models it as nullable.
