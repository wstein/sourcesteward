# Implementation Plan: Hook and bypass disclosure

**Branch**: `codex/024-hook-disclosure` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw hooks`, a read-only report of which commit and push hooks Git would run from the effective `core.hooksPath` and whether the owning backend runs them. One bounded operation in `internal/inspection` reuses the existing ownership decision (`Inspect` for Git-owned paths, the filesystem-only jj detection for jj), reads `core.hooksPath` through allowlisted isolated Git reads, classifies six hook names with `lstat`, `readlink` and `access` only, joins the result with an evidence catalog that states, per backend and operation, whether hooks run and how well that is established, derives stable findings, and returns one typed result that the CLI renders as text or as versioned JSON. jj facts come only from the existing sandboxed session; the jj hook behavior is a catalog claim with an evidence level, and a reduced conformance script measures it on the existing lanes. The scope was reduced after five review rounds (analysis.md, "Scope reduction applied").

## Technical Context

**Language/Version**: Go 1.27.2; Git 2.46 or newer; jj 0.45.1 and 0.46.0 (existing lanes).
**Primary Dependencies**: Existing Cobra v1.10.2, the standard library and the existing JSON Schema test dependency. No new dependency.
**Storage**: None.
**Testing**: Go table tests with real temporary Git repositories and, on the lanes, real jj workspaces; schema fixtures; `scripts/smoke-hooks.py` with its unit test; effect sentinels; existing architecture and documentation checks.
**Target Platform**: linux amd64/arm64 and darwin arm64 in code; CI evidence for ubuntu-24.04 amd64 only.
**Project Type**: CLI with a shared operation layer.
**Performance Goals**: limits table of the spec (10 seconds total, 5 per Git process, 256 KiB result).
**Constraints**: No hook or configuration-hook execution; no hook entry or directory opened; no write; no network; configuration values are untrusted.
**Scale/Scope**: One worktree or workspace, six hook names.

## Constitution Check

Pre-research assessment: PASS. Post-design assessment: PASS.

- **I. Shared contract**: one typed result renders text and JSON; non-interactive; exit statuses 0, 1, 2.
- **II. Evidence before actions**: read-only; every claim carries its evidence level; the result authorizes nothing and recommends no bypass.
- **III. Ownership and recovery**: no mutation and no recovery claim. The command never skips, installs or edits hooks; it reports that a backend may skip them. Ownership comes from the existing detection and Git's own ownership check, applied through the `rev-parse` gate around every configuration read.
- **IV. Specify and verify**: failing tests first; independent analysis before code; real temporary Git repositories and jj workspaces; hostile fixtures, concurrent change, limits; effect sentinels for hooks, writes, opened entries and the jj working copy and operation log; a reduced conformance procedure that measures the jj claim on the minimum and current lanes.
- **V. Tier 1 peers**: Git and jj are both supported with stated, different semantics and evidence levels; a jj-managed workspace, colocated included, uses the jj adapter; the Git reads in a colocated workspace are read-only configuration facts, not a substitute for jj and not a write; jj unavailability is an actionable error; GitButler refuses explicitly; no equivalence is claimed. The Git reads run outside the jj sandbox and are disclosed.
- **Scope**: no network; hooks are not executed, and the contract says so. Hook hygiene, configuration-hook evaluation, further operations and further conformance are separate specifications. No amendment or exception is required.

## Project Structure

```text
internal/inspection/hooks.go          # entry: ownership dispatch, refusals, jj version, resample
internal/inspection/hooks_result.go   # result types, hook and evidence catalogs, findings table, derivation
internal/inspection/hooks_config.go   # allowlisted Git reads, hooks path states, hooks-directory resolution
internal/inspection/hooks_files.go    # lstat/readlink/access classification of six names (no open)
internal/inspection/hooks_files_unix.go   # access(X_OK) and mode for linux and darwin
internal/inspection/hooks_files_other.go  # explicit unsupported-platform stub
internal/inspection/jj.go             # one added field: the jj version the session already parses
internal/inspection/hooks_*_test.go   # temporary-repository tests, hostile fixtures, sentinels
internal/cli/hooks.go                 # command, flags, text rendering; registered hidden until WI5
schemas/v1/hooks.schema.json          # new contract; existing contracts unchanged
internal/cli/testdata/hooks-*.json    # schema-valid success, incomplete, refused and error observations
internal/contracts/hooks_test.go      # fixtures against the schema, existing fixtures still valid
scripts/smoke-hooks.py                # conformance: J0, J2, J2b, J3, J4, J7, report assertions, sentinels
scripts/test_hooks_smoke.py           # unit test of the smoke script
docs/hooks.md, docs/backends.md       # guide and capability matrix with evidence levels
docs/agent-guide.md, docs/json-contracts.md  # the new command and contract
```

The Git adapter stays inside `internal/inspection`, as for inventory, so the import and effects boundary (`scripts/check-architecture.py`) needs no exception: `internal/cli` renders and never starts a process.

## Design Decisions

1. **Ownership decision.** The path is first checked with the existing filesystem-only `ancestors` for a `.jj` marker. With none, the operation calls `Inspect` and branches on its `Backend` and `Status`: `git` ok proceeds (`Repository.Bare` refuses `bare-repository`); `unsupported` for `gitbutler` refuses `unsupported-backend`; `not-repository` refuses; every other error code passes through verbatim (spec refusal table). With a `.jj` marker, the operation captures the innermost marker as the workspace root, then runs `detectJJGitButler` and `managed` (filesystem only; `managed` sorts the evidence, so the root is captured first): ambiguity refuses `ambiguous-backend` before any process; otherwise it proceeds with the jj adapter without the identity queries of `Inspect`. The same path validation as `Inspect` (UTF-8, `Abs`, `EvalSymlinks`, directory check) runs before `ancestors`, shared rather than copied, so `invalid-path` is raised on both paths. If a `.jj` marker appears between this check and the `Inspect` call and `Inspect` returns backend `jj`, the result is `changed-repository` (tested with a fake runner). With nested `.jj` markers the probe-only session receives a Result whose `Evidence[0]` is the innermost marker (the evidence is copied before `managed` sorts it; `withJJSession` and `detectJJGitButler` both read `Evidence[0]`). Existing `Inspect` behavior is unchanged.
2. **The jj session.** `jjSession` already parses the jj version in `withJJSession`; the plan adds one field to the session and one internal function that runs only the sandbox probe and `--version` and returns the version. Nothing else runs under jj. Failures map to the existing jj codes; no unsandboxed retry.
3. **Git facts for a jj workspace.** A `.git` entry at the workspace root (directory, or gitfile of a worktree-backed secondary workspace, which jj documents as the default) means Git facts are read. `git version` is run (one read on every path that uses Git, since `Inspect` does not return the version) and checked against the 2.46 minimum (`git-unavailable`, `git-failed`). Reads use the workspace root as working directory and discovery; the two gate outputs (`--absolute-git-dir` before and after the reads) must be equal to each other and, for jj, to the directory the `.git` entry designates; on a Git-owned path they must equal `Inspect`'s `git_dir` (a gitfile target is resolved as data), otherwise `hooks-failed`. With no `.git` entry no Git process runs and the guard set is not evaluated. The detected Git version for `jj-push` is the Git on stw's `PATH`, which may differ from the Git jj spawns; the notes say so.
4. **Ownership gate.** `git config` does not apply Git's ownership check (research measurement 13). Every configuration read is bracketed by `rev-parse --path-format=absolute --absolute-git-dir`, which does; a failure is `hooks-failed`, and `config` exit status 1 means unset only after the gate passed (exit 3 or 128 is `hooks-failed`). The runner strips `GIT_*`, so the ownership test uses an injected runner that sets `GIT_TEST_ASSUME_DIFFERENT_OWNER=1` for the child; a fixture also covers the empty-value path, where `rev-parse --git-path hooks` is skipped but the gate still runs.
5. **Reads and states.** `config --show-scope -z --get-regexp '^core\.hookspath$'` (NUL-separated records `scope`, `key`, optional LF and value): a valueless key has no value and is `invalid`, distinct from an empty value (measurement 15). The parser is fail-safe: a record that does not parse is `hooks-failed`. The last record gives the effective value and scope. An empty effective value skips `rev-parse --git-path hooks` (it exits 128 for it while Git runs no hook; measurement 7) and sets `empty`; otherwise the absolute directory comes from `rev-parse --path-format=absolute --git-path hooks` (measurements 3 and 6), and failure for a non-empty value is `invalid`. `null-device` is decided after resolution, not by string; then `absent`, `not-directory`, `unreadable` (no search permission for the real user), `configured` or `default`. For every state except `configured` and `default` no directory hook can run.
6. **Classification without opening.** Each of the six names is `lstat`ed in the resolved directory. Order: `access(X_OK)` through `syscall.Access` (real user, as Git) failing, including ELOOP and dangling, gives `ignored`; otherwise a non-regular entry or symlink target gives `not-runnable`; otherwise `active` (measurements 5 and 16). A test source scan forbids every open (`os.Open`, `os.OpenFile`, `os.ReadFile`, `os.ReadDir`, `ioutil`) in the non-test `hooks*.go` files. Proof that no entry is opened: on Linux an inotify watch on the hooks directory sees no `IN_OPEN` or `IN_ACCESS` of the six names, and a FIFO at each name never blocks; macOS is not proven.
7. **Configuration hooks are detected, not evaluated.** One read, `config -z --get-regexp '^hook\..*\.event$'`, returns event names (no command text, no enabled state). If a value names one of the six operation hooks (or the key is valueless), `config_hooks_detected` is true and `config-hooks-not-evaluated` is raised. Evaluating them (engine, enabled state, version coverage) is deferred.
8. **Catalogs are data in one file.** `hooks_result.go` holds the hook catalog (six names), the evidence catalog (four claims) and the findings table once; the schema enumerates the same codes, claim ids and levels and a test compares them, so spec, schema and code cannot drift. The evidence catalog stores the base level and the measured range per claim and backend tuple; the coverage rule lowers `measured` to `documented`, and disagreeing lanes leave a claim `unverified`. The catalog ships `documented` and `unverified` values until WI6 records a lane.
9. **Derivation.** Operation hooks are built from the catalog rows of each operation; `bypassed` and findings follow FR-007 and the spec's findings section; arrays are never null; findings are sorted deterministically.
10. **Resample.** Before success the effective `core.hooksPath` value and each present hook's `lstat` metadata are read again; a difference yields `hooks-state-changed`.
11. **Exit statuses.** 0, 1 and 2 only; no new error type and no change to `internal/cli/root.go` beyond command registration.
12. **Conformance and the catalog agree by test.** `scripts/smoke-hooks.py` builds the fixtures J0, J2, J2b, J3, J4 and J7 of the spec, records outcomes, asserts in the colocated fixture that `stw hooks --json` reports the three guards and the claims at the catalog levels, and applies the exit rules of the spec's procedure (2 invalid control or precondition, 1 a deciding outcome that contradicts a `yes` or `no` claim or disagrees unless the catalog already states `unverified`/`unknown`, otherwise 0). Promotion to `measured` is a manual catalog edit with the lane record, in the same change as the verification note.
13. **Hidden until complete.** The command is registered hidden and unhidden in WI5.

## Risks

- **jj behavior may contradict the catalog.** `jj commit` or, more likely, `jj git push` (which spawns `git`) could run a hook. The catalog states `documented` and `unverified` and the lane decides; a contradiction changes the catalog and the spec before release, not the schema.
- **Git output shapes.** The `--show-scope` record layout may differ on Git 2.46; the parser is fail-safe and a format-pinning test runs on both Git lanes.
- **Reading global configuration** crosses the repository boundary by design; only the winning scope is reported and the effects section says so.
- **Filesystem modes** can be misleading on some hosts (measurement 14); tests create fixtures and read modes back.
- **`access(X_OK)` mirrors Git for the real user**, not for other users who will commit from the same checkout.
- **Non-colocated jj push.** jj's `git` subprocess may consult Git configuration and the internal store's hooks; the report does not evaluate them (`guards_evaluated: false`); this is deferred together with the non-colocated conformance steps.
- **Configuration hooks are not evaluated**, so a repository whose guards are only `hook.*` entries reports `config-hooks-not-evaluated` instead of guards; the warning says so.
- **Bypass finding severity.** `guards-bypassed-on-backend` is an error at documentation-level evidence; the maintainer confirmed it stays an error. Without a strict mode a caller reads the JSON.
- **Status and code names** (`incomplete`, `refused`, `hooks-failed`) follow Spec 021 and differ from inventory's; recorded as a choice.
