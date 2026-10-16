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
- **V. Tier 1 peers**: Git and jj are both supported with stated, different semantics and evidence levels; a jj-managed workspace, colocated included, uses the jj adapter; the Git reads in a colocated workspace are read-only configuration facts, not a substitute for jj and not a write; jj unavailability is an actionable error; GitButler refuses explicitly; no equivalence is claimed. The Git reads run outside the jj sandbox and are disclosed.
- **Scope**: no network; hooks are not executed, and the contract says so. Installing, repairing or bypassing hooks, server-side hooks and a hook-compatibility layer are separate specifications. No amendment or exception is required.

## Project Structure

```text
internal/inspection/hooks.go          # entry: ownership dispatch (ancestors, then Inspect or the jj filesystem detection), refusals, jj version, resample
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

1. **Ownership decision.** The path is first checked with the existing filesystem-only `ancestors` for a `.jj` marker. With none, the operation calls `Inspect` and branches on its `Backend` and `Status`: `git` ok proceeds (`Repository.Bare` refuses `bare-repository`); `unsupported` for `gitbutler` refuses `unsupported-backend`; `not-repository` refuses; every other error code passes through verbatim (spec refusal table). With a `.jj` marker, the operation runs `detectJJGitButler` and `managed` (filesystem only): ambiguity refuses `ambiguous-backend` before any process; otherwise it proceeds with the jj adapter without calling the identity queries of `Inspect` (those run `operation log`, `workspace` and `log` twice and add failure codes unrelated to hooks). Existing `Inspect` behavior is unchanged. Alternative: call `Inspect` for jj too (rejected: extra jj queries and unrelated failure modes; the spec states jj processes precisely).
2. **The jj session.** `jjSession` already parses the jj version in `withJJSession`; the plan adds one field to the session and one internal function that runs only the sandbox probe and `--version` and returns the version. Nothing else runs under jj. Failures map to the existing jj codes; no unsandboxed retry.
3. **Git facts for a jj workspace.** A `.git` entry at the workspace root (directory, or gitfile of a worktree-backed secondary workspace, which jj documents as the default) means Git facts are read. `git version` is run and checked against the 2.46 minimum (`git-unavailable`, `git-failed` as `Inspect` uses them). Reads use the workspace root as working directory and discovery; the discovered `--absolute-git-dir` must equal the directory the `.git` entry designates (the gitfile target is resolved as data, not followed beyond that comparison), otherwise `hooks-failed`. With no `.git` entry no Git process runs and the guard set is not evaluated. The existing `detectJJGitButler` already follows gitfiles and `commondir`, so no new shape parsing is added.
4. **Ownership gate.** `git config` does not apply Git's ownership check (research measurement 13). Every configuration read is therefore bracketed by `rev-parse --path-format=absolute --absolute-git-dir`, which does; a failure is `hooks-failed`, and `config` exit status 1 means unset only after the gate passed (exit 3 or 128 is `hooks-failed`). The runner strips `GIT_*`, so the ownership test uses an injected runner that sets `GIT_TEST_ASSUME_DIFFERENT_OWNER=1` for the child; a fixture also covers the path where DD5 skips `rev-parse` for an empty value (the gate still runs).
5. **Configuration reads and levels.** `config --show-scope --show-origin -z --get-regexp '^core\.hookspath$'` yields records of scope, origin, key and value in precedence order; a valueless key has no value field and is `invalid`, distinct from an empty value (measurement 15). The parser is fail-safe: a record that does not parse is `hooks-failed`. Origins are normalized (relative to absolute against the worktree root; `file:` prefix stripped) and reported only for `local` and `worktree`; at most 16 records are kept (`levels_truncated`). The effective value is the last. An empty effective value skips `rev-parse --git-path hooks` (it exits 128 for it while Git runs no hook; measurement 7) and sets `empty`. Otherwise the absolute directory comes from `rev-parse --path-format=absolute --git-path hooks`, which applies `~` expansion, worktree-root resolution and the default common directory (measurements 3, 6); a failure for a non-empty value is `invalid` (measurement 15).
6. **State of the hooks path.** `null-device` when the resolved path is the null device (compared after resolution, not by string alone), `absent` when it does not exist, `not-directory`, `unreadable` (lacks search permission for the real user), `invalid`, `configured` or `default`. For every state except `configured` and `default`, no directory hook can run and no directory is listed; configuration hooks are independent of the path (measurement 10).
7. **File classification without opening a hook.** Each of the 28 catalog names is `lstat`ed in the resolved directory. Order: `access(X_OK)` through `syscall.Access` (real user, as Git) failing, including ELOOP and dangling, gives `ignored`; otherwise a non-regular entry or symlink target gives `not-runnable`; otherwise `active` (measurements 5 and 16). For a symlink one `readlink` and one `stat` classify the target as inside, outside or dangling. Owner and world-writability come from `lstat`; root-owned is safe. A test source scan forbids `os.Open`, `os.ReadFile`, `os.OpenFile` and `ioutil` in `hooks_*.go` except one allowlisted helper that opens a confined directory with `O_DIRECTORY|O_NONBLOCK|O_NOFOLLOW` (the pattern of `openMetadata`). Proof that no hook entry is opened: an inotify watch on the hooks directory (Linux) sees no `IN_OPEN` or `IN_ACCESS` for catalog names, and a FIFO at each catalog name never blocks; macOS is not proven.
8. **Confinement roots and listing.** The roots are the worktree root, the Git directory and the common Git directory (a linked worktree's default hooks are in the common directory; measurement 3). The directory is listed (`ReadDir(257)`, at most 256 reported) only when its resolved real path lies inside a root; otherwise only the fixed names are examined (US3-6). Unrecognized names exclude catalog names and `*.sample`; at most 16 reported, escaped. A hooks directory that is a symlink whose real path leaves the roots is `hook-symlink-outside`; a configured path outside the roots that is not a symlink is only info.
9. **Tracking.** For a directory inside the worktree, one `--literal-pathspecs ls-files -z --full-name` read from the worktree root with the catalog names under that directory as literal pathspecs marks hooks `tracked` or `untracked`; anything else is `not-applicable` or `unknown`.
10. **Configuration hooks.** Two reads, `^hook\..*\.(event|enabled)$` with values and `^hook\..*\.command$` with `--name-only`, are parsed into `{name, events, enabled, command_present, scopes}`; subsection names may contain dots and spaces (the variable is the last dotted segment). Command text is never read. `engine` is `supported` only when the claim `git-config-hooks` lists a measured range containing the detected Git version, else `unknown`; entries count as guards only when `supported`. An event without a command raises `config-hook-incomplete`.
11. **Catalogs are data in one file.** `hooks_result.go` holds the hook catalog, the evidence catalog and the findings table once; the schema enumerates the same codes, claim ids and levels and a test compares them, so spec, schema and code cannot drift. The evidence catalog stores the base level and the measured range per claim and backend tuple; the version-coverage rule lowers `measured` to `documented`, and disagreeing lanes leave a claim `unverified`. The catalog ships `documented` and `unverified` values until WI6 records a lane.
12. **Derivation.** Operation hooks are built from the catalog rows of each operation; `bypassed`, `guards_evaluated` and the bypass findings follow FR-009 and the contract's derivation rules; directory and file safety findings come from the classification. Findings are sorted deterministically; arrays are never null.
13. **Resample.** Before success the effective `core.hooksPath` value, the directory identity (device, inode, modification time) and each present hook's `lstat` metadata are read again; a difference yields `hooks-state-changed`.
14. **Caller environment.** The existing runner removes `GIT_*` from the child environment but keeps `HOME` and `XDG_CONFIG_HOME`. The operation separately reads the names of the closed list of FR-012 in the stw process environment and reports those present by name. Values are never read into the result.
15. **Exit statuses.** As in Spec 021: the typed strict-findings error exits 3 with one standard-error line and is checked before the fallback of 2. Whichever of 021 WI1 and 024 WI1 lands first adds the error type, `exitStatus` and the escaping helper; the other reuses them.
16. **Conformance and the catalog agree by test.** `scripts/smoke-hooks.py` builds the J0 to J7 fixtures, records outcomes, and compares them with the catalog extracted from the built binary's JSON. A measured outcome that contradicts a claim fails the lane; an invalid control or precondition exits 2 and concludes nothing; promotion to `measured` is a catalog edit made with the lane record, in the same change as the verification note.
17. **Hidden until complete.** The command is registered hidden, as `stw pins` is, and unhidden in the last documentation work item.

## Risks

- **jj behavior may contradict the catalog.** `jj commit` or, more likely, `jj git push` (which spawns `git`) could run a hook. The catalog states `documented` and `unverified` and the lane decides; a contradiction changes the catalog and the spec before release, not the schema.
- **Git output shapes.** `--show-origin` printed relative and absolute origins (measurement 8) and may differ on Git 2.46; the parser is fail-safe and a format-pinning test runs on both Git lanes. Configuration hooks may not exist on 2.46; `engine: unknown` covers that until measured.
- **Reading global configuration** crosses the repository boundary by design; only scope is reported for system and global values and the effects section says so.
- **Filesystem modes** can be misleading on some hosts (measurement 14); tests create fixtures and read modes back, and the unsafe-permission findings are Unix-only.
- **`access(X_OK)` mirrors Git for the real user**, not for other users who will commit from the same checkout; the report says "executable for the real user".
- **Non-colocated jj push.** jj's `git` subprocess may consult system and global Git configuration and the internal store's hooks; the report does not evaluate them (`guards_evaluated: false`) and J5 measures the case.
- **Bypass finding severity.** `guards-bypassed-on-backend` is an error, so `--strict` fails closed on documentation-level evidence; the contract states this and the maintainer may rate it down in review.
- **Status and code names** (`incomplete`, `refused`, `hooks-failed`) follow Spec 021 and differ from inventory's; recorded as a choice, consistent inside the contract.
