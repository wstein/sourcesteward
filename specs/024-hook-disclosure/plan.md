# Implementation Plan: Hook and bypass disclosure

**Branch**: `codex/024-hook-disclosure` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw hooks`, a read-only report of a repository's commit and push guards and of whether the owning backend runs them. One bounded operation in `internal/inspection` reuses the existing ownership decision (`Inspect`), reads `core.hooksPath` and configuration hooks through allowlisted isolated Git configuration reads, classifies the fixed catalog of hook names with `lstat`, `readlink` and `access` only, joins the result with an evidence catalog that states, per backend and operation, whether hooks run and how well that is established, derives stable findings, and returns one typed result that the CLI renders as text or as versioned JSON. jj facts come only from the existing sandboxed session; the jj hook behavior is a catalog claim with an evidence level, and a conformance script measures it on the existing lanes.

## Technical Context

**Language/Version**: Go 1.27.2; Git 2.46 or newer (existing minimum and current lanes); jj 0.45.1 and 0.46.0 (existing lanes).  
**Primary Dependencies**: Existing Cobra v1.10.2, the standard library and the existing JSON Schema test dependency. No new dependency.  
**Storage**: None.  
**Testing**: Go table tests with real temporary Git repositories and, on the lanes, real jj workspaces; schema fixtures; `scripts/smoke-hooks.py` with its unit test; effect sentinels; existing architecture and documentation checks.  
**Target Platform**: linux amd64/arm64 and darwin arm64 in code; CI evidence for ubuntu-24.04 amd64 only.  
**Project Type**: CLI with a shared operation layer.  
**Performance Goals**: 10 seconds total, 5 seconds per Git process, 256 KiB result.  
**Constraints**: No hook, configuration hook, helper, filter or alias execution; no hook path opened; no write; no network; configuration and hook names are untrusted.  
**Scale/Scope**: One worktree or workspace, 28 catalog names, at most 256 directory entries.

## Constitution Check

Pre-research assessment: PASS. Post-design assessment: PASS.

- **I. Shared contract**: one typed result renders text and JSON; non-interactive; statuses 0, 1, 2 and the documented `--strict` status 3 with a stated precedence.
- **II. Evidence before actions**: read-only; every claim carries its evidence level; the result authorizes nothing and recommends no bypass.
- **III. Ownership and recovery**: no mutation and no recovery claim. The command never skips, installs or edits hooks; it reports that a backend may skip them. Ownership comes from the existing detection; Git's own ownership check applies because the Git reads are discovery-based.
- **IV. Specify and verify**: failing tests first; independent analysis before code; real temporary Git repositories and jj workspaces; hostile fixtures, concurrent change, limits; effect sentinels for hooks, writes, opened paths and the jj working copy and operation log; a conformance procedure that measures the jj claim on the minimum and current lanes.
- **V. Tier 1 peers**: Git and jj are both supported with stated, different semantics and evidence levels; a jj-managed workspace, colocated included, uses the jj adapter; the Git reads in a colocated workspace are read-only configuration facts, not a substitute for jj and not a write; jj unavailability is an actionable error; GitButler refuses explicitly; no equivalence is claimed.
- **Scope**: no network; hooks are not executed, and the contract says so. Installing, repairing or bypassing hooks, server-side hooks and a hook-compatibility layer are separate specifications. No amendment or exception is required.

## Project Structure

```text
internal/inspection/hooks.go          # entry: Inspect-based ownership dispatch, refusals, jj version, resample
internal/inspection/hooks_result.go   # result types, hook and evidence catalogs, findings table, derivation, option validation
internal/inspection/hooks_config.go   # allowlisted Git configuration reads, level parsing, hooks-directory resolution
internal/inspection/hooks_files.go    # lstat/readlink/access classification, bounded confined listing (shared logic)
internal/inspection/hooks_files_unix.go   # owner, mode and access(X_OK) for linux and darwin
internal/inspection/hooks_files_other.go  # explicit unsupported-platform stub
internal/inspection/jj.go             # one added field: the jj version the session already parses
internal/inspection/hooks_*_test.go   # temporary-repository tests, hostile fixtures, sentinels
internal/cli/hooks.go                 # command, flags, text rendering; registered hidden until the last work item
internal/cli/root.go                  # strict-findings error shared with Spec 021 (status 3)
schemas/v1/hooks.schema.json          # new contract; existing contracts unchanged
internal/cli/testdata/hooks-*.json    # schema-valid success, incomplete, refused and error observations
internal/contracts/hooks_test.go      # fixtures against the schema, existing fixtures still valid
scripts/smoke-hooks.py                # native conformance: hook sentinels, Git and jj lanes, catalog agreement
scripts/test_hooks_smoke.py           # unit test of the smoke script
docs/hooks.md, docs/backends.md       # guide and capability matrix with evidence levels
docs/agent-guide.md, docs/json-contracts.md  # exit status 3 and the new contract
```

The Git adapter stays inside `internal/inspection`, as for inventory, so the import and effects boundary (`scripts/check-architecture.py`) needs no exception: `internal/cli` renders and never starts a process.

## Design Decisions

1. **Ownership is `Inspect`'s decision.** The operation calls `Inspect` and branches on its `Backend` and `Status`: `git` ok proceeds (a `Repository.Bare` result refuses `bare-repository`); `unsupported` for `gitbutler` refuses `unsupported-backend`; `ambiguous` refuses `ambiguous-backend` (decided from filesystem markers before any process, as `Inspect` already does); `not-repository` refuses; a jj `ok` result proceeds with the jj adapter; every other error maps to `hooks-failed` except the jj codes of the refusal table, which are passed through. No new ownership logic is written. Alternative: re-implement detection (rejected: drift from the shared decision).
2. **The jj version.** `Inspect` does not return the jj version, and its schema 2 result must not change. `jjSession` already parses the version in `withJJSession`; the plan adds one field to the session and one internal function that runs the session only to return it (sandbox probe and `--version`, nothing else). This repeats the sandbox probe once; the cost is accepted under the 10-second deadline. No new jj command.
3. **Colocated Git facts.** For a jj workspace, `.git` at the workspace root decides colocation (a directory; the existing jj metadata validation already refuses odd shapes). Git configuration reads run with the workspace root as the working directory and discovery, then compare `rev-parse --absolute-git-dir` with the workspace's `.git`; a mismatch is `hooks-failed`. A non-colocated or secondary workspace starts no Git process and reports `workspace-has-no-git-configuration`. Git's ownership failure surfaces as `hooks-failed`.
4. **Configuration reads and levels.** `config --show-scope --show-origin -z --get-all core.hooksPath` yields records of scope, origin and value in precedence order (measurement 8). The parser is fail-safe: a record that does not parse is `hooks-failed`, not a guess. Origins are normalized (relative to absolute against the worktree root; `file:` prefix stripped) and reported only for `local` and `worktree`. The effective value is the last. An empty effective value skips `rev-parse --git-path hooks` (it exits 128 for it while Git runs no hook; measurement 7) and sets state `empty`. Otherwise the absolute directory comes from `rev-parse --path-format=absolute --git-path hooks`, which already applies `~` expansion, worktree-root resolution and the default common directory (measurements 3, 6).
5. **State of the hooks path.** `null-device` when the resolved path is the null device (compared after resolution, not by string alone), `absent` when it does not exist, `not-directory`, `unreadable`, `configured` or `default`. For `null-device`, `empty` and `absent` no directory hook can run and no directory is listed; configuration hooks are independent of the path (measurement 10).
6. **File classification without opening.** Each of the 28 catalog names is `lstat`ed in the resolved directory. `kind` and mode come from `lstat`; for a symlink, one `readlink` and one `stat` classify the target as inside, outside or dangling; `access(X_OK)` for the effective user decides `executable_for_user`, mirroring Git's own check (measurement 5). `ignored` and `not-runnable` follow measurement 5. Owner and world-writability come from `lstat`. A test source scan forbids `os.Open`, `os.ReadFile`, `os.OpenFile` and `ioutil` in the `hooks_*.go` files, and a FIFO fixture proves nothing blocks.
7. **Confined listing only.** The hooks directory is listed (at most 256 entries) only when its resolved real path lies inside the worktree or the Git directory; otherwise only the fixed names are examined (US3-6). Unrecognized names are those that are neither catalog names nor end in `.sample`; at most 16 are reported, escaped.
8. **Tracking.** For a confined directory inside the worktree, one `--literal-pathspecs ls-files -z --full-name -- <dir>` read marks catalog hooks `tracked` or `untracked`; anything else is `not-applicable` or `unknown`.
9. **Configuration hooks.** `config -z --show-scope --get-regexp '^hook\.'` is parsed into `{name, events, enabled, scope}`; subsection names may contain dots and spaces (the variable is the last dotted segment). Command text is parsed past and discarded, never stored. `engine` is `supported` only when the evidence catalog lists a measured range containing the detected Git version, else `unknown`.
10. **Catalogs are data in one file.** `hooks_result.go` holds the hook catalog, the evidence catalog and the findings table once; the schema enumerates the same codes, claim ids and levels and a test compares them, so spec, schema and code cannot drift. The evidence catalog stores the base level and the measured range per claim; the version-coverage rule lowers `measured` to `documented`. The catalog ships `documented` and `unverified` values until WI6 records a lane.
11. **Derivation.** Operation hooks are built from the catalog rows of each operation; `bypassed` and the bypass findings follow FR-009 and the contract's derivation rules; directory and file safety findings are derived from the classification. Findings are sorted deterministically; arrays are never null.
12. **Resample.** Before success the effective `core.hooksPath` value, the directory identity (device, inode, modification time) and each present hook's `lstat` metadata are read again; a difference yields `hooks-state-changed`.
13. **Caller environment.** The existing runner removes `GIT_*` from the child environment. The operation separately reads the names of a closed list (`GIT_CONFIG_COUNT`, `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM`, `GIT_CONFIG_NOSYSTEM`, `GIT_DIR`, `GIT_WORK_TREE`) in the stw process environment and reports those present by name. Values are never read into the result.
14. **Exit statuses.** As in Spec 021: the typed strict-findings error exits 3 with one standard-error line and is checked before the fallback of 2. Whichever of 021 WI1 and 024 WI1 lands first adds the error type, `exitStatus` and the escaping helper; the other reuses them.
15. **Conformance and the catalog agree by test.** `scripts/smoke-hooks.py` builds the J0 to J6 fixtures, records outcomes, and compares them with the catalog extracted from the built binary's JSON (`stw hooks --json` in a fixture reports each claim). A measured outcome that contradicts a claim fails the lane; promotion of a claim to `measured` is a catalog edit made with the lane record, in the same change as the verification note.
16. **Hidden until complete.** The command is registered hidden, as `stw pins` is, and unhidden in the last documentation work item, so a partial stack never advertises it.

## Risks

- **jj behavior may contradict the catalog.** `jj commit` or, more likely, `jj git push` (which spawns `git`) could run a hook. The catalog states `documented` and `unverified` and the lane decides; a contradiction changes the catalog and the spec before release, not the schema.
- **Git output shapes.** `--show-origin` printed relative and absolute origins (measurement 8) and may differ on Git 2.46; the parser is fail-safe and a format-pinning test runs on both Git lanes. Configuration hooks may not exist on 2.46; `engine: unknown` covers that until measured.
- **Reading global configuration** crosses the repository boundary by design; only scope is reported for system and global values and the effects section says so.
- **Filesystem modes** can be misleading on some hosts (measurement 14); tests create fixtures and read modes back, and the unsafe-permission findings are Unix-only.
- **`access(X_OK)` mirrors Git for the effective user**, not for other users who will commit from the same checkout; the report says "executable for the effective user".
- **Bypass finding severity.** `guards-bypassed-on-backend` is an error, so `--strict` fails closed on documentation-level evidence; the contract states this and the maintainer may rate it down in review.
- **Status and code names** (`incomplete`, `refused`, `hooks-failed`) follow Spec 021 and differ from inventory's; recorded as a choice, consistent inside the contract.
