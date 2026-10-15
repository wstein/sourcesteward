# Implementation Plan: Bounded Repository Inspection

**Branch**: `codex/005-repository-inspection` | **Date**: 2026-10-15 | **Spec**: [spec.md](spec.md)

## Summary
Introduce only local identity inspection and conservative backend detection. Keep command parsing/rendering in CLI and place filesystem discovery and fixed Git plumbing in one operation package. Managed workspaces produce evidence-bearing unsupported results, not native adapter claims.

## Technical Context
**Language/Version**: Go 1.27.2; existing Cobra 1.10.2; Python checks.
**Dependencies**: Standard library plus existing Cobra; no new module dependency. Git 2.46+ is required for the identity query contract; older incompatible plumbing returns git-failed without a fallback.
**Storage**: Read-only local metadata; no caches, logs or result files.
**Testing**: Table-driven unit tests, temporary Git repositories, command injection seam, real-process CLI smoke tests, byte snapshots and sentinels.
**Target Platform**: Existing Go platforms with installed Git; test portability explicitly, no shell execution.
**Performance**: A fixed bounded number of plumbing invocations; output capped at 64 KiB per invocation. No history/status scans.
**Constraints**: No index refresh, repository mutation, hook/network/process prompt or backend snapshot. Normal safe.directory protection retained.
**Scope**: Identity, HEAD state, object format, managed-backend detection and explicit failures only.

## Constitution Check
I: One typed result drives human/JSON, schema1 and fixed exit classes, no interactions.
II/III: No consequential mutation; explicit effects and preserved worktree ownership; no fallback for managed workspaces.
IV: Reviewed artifacts precede failing tests and implementation; temporary fixtures, unusual paths, concurrency/cancellation and independent review.
V: Fixed documented Git plumbing only. No speculative universal backend interface; no jj/GitButler invocation.
All gates pass before and after design; architecture boundary deliberately evolves from foundation to CLI → inspection operation.

## Structure
```text
internal/cli/{root,inspect}.go       parsing and rendering only
internal/inspection/               typed result, discovery, Git runner and operation tests
scripts/check-architecture.py       allow only CLI → inspection; forbid adapters in parsing
specs/005-repository-inspection/    specification, analysis and contracts
```
The CLI receives a typed result and operational error from inspection.Inspect(context, path). The operation owns os.Getwd/path resolution. Keep os/exec, filesystem and subprocess environment construction out of CLI. Command entry point remains unchanged. A small unexported runner seam permits bounded output/cancellation/concurrency tests; do not build a general backend framework.

## Detection and Git execution design
1. Resolve an absolute existing input directory, following the input path's symlinks, and reject invalid UTF-8. Walk from it toward the filesystem root; at each level inspect `.jj` before checking `.git`. Stop after the nearest `.git` entry (directory or gitfile): an inner Git repository is an ownership boundary, so outer `.jj` markers do not own it. `.jj` at that same level still counts. Without a `.git` entry, walk to root, allowing standalone jj detection. A `.jj` directory is recognized evidence; symlink, wrong-type or inaccessible `.jj` is invalid/unreadable metadata, not absence. Do not follow marker symlinks. This is a conservative metadata heuristic, not an arbitrary manager detector.
   An early jj match forbids HEAD inspection, but does not prevent bounded Git identity discovery when a Git repository is discoverable: collect GitButler evidence too and classify both families as ambiguous. If Git is unavailable or no Git repository is discovered, retain jj unsupported detection. Other discovery errors (unsafe ownership, malformed metadata) remain operational errors with retained evidence.
2. For a Git discovery path, use fixed built-in rev-parse plumbing with `--path-format=absolute` to discover git-dir/common-dir/worktree/bare/object format. Parse each output as a single value by removing only Git's one terminal LF, never TrimSpace or line splitting for paths. Validate returned absolute paths and UTF-8. Do not concatenate user path into Git arguments; use Cmd.Dir.
3. Examine `gitbutler` entries at both resolved private and common Git directories. A directory is evidence; marker symlinks/wrong types are invalid metadata and inaccessible entries are unreadable metadata. Query packed and loose reference evidence through fixed `for-each-ref --format=%(refname) refs/gitbutler/ refs/heads/gitbutler/workspace`; retain only full names beginning `refs/gitbutler/` or exactly `refs/heads/gitbutler/workspace` (filter out prefix collisions). This query also recognizes a workspace branch when it is not current HEAD. Do not include undocumented legacy branch names. Any recognized marker/reference yields suspected GitButler ownership, not a health assertion. If detected, stop before HEAD inspection and report unsupported; both families mean ambiguous. Unknown future marker conventions and GitButler's experimental lazy registration can remain undetected; absence never proves unmanaged ownership.
4. For ordinary Git, sample complete HEAD state twice around remaining identity work. `symbolic-ref --quiet HEAD` exit0 means symbolic; exit1 means detached, while any other exit is a failure. A symbolic HEAD must name a full `refs/heads/...` branch; other targets are outside this slice and fail invalid-metadata. For symbolic HEAD, `show-ref --exists <full-ref>` distinguishes absent branch (exit2) from lookup/corrupt-reference errors (exit1 or other nonzero); only absence qualifies as unborn. For a present branch and for detached HEAD, compare full `rev-parse --verify HEAD` with full `rev-parse --verify HEAD^{commit}`: both must succeed with identical valid object IDs. Missing/noncommit objects or a peeling tag fail invalid-metadata rather than becoming unborn. Use fixed argv with only the validated full branch ref passed to show-ref. Git 2.46+ is required for --exists; no missing/corrupt fallback based on --verify is allowed. SHA-1 and SHA-256 IDs are validated by reported format.
   Mismatch between complete HEAD samples returns changed-repository. Concurrency sampling covers HEAD only: gitfile/worktree/common-dir routing or backend marker races are not transactionally detected, and ABA changes may be invisible. Disclose this limitation. Do not read status, index, tracked/untracked files, history or remotes.
5. Every Git subprocess uses CommandContext with no shell, empty stdin, bounded stdout/stderr, fixed built-in argv and finite wait after cancellation. Strip all inherited GIT_* variables (routing, config injection, external diff, trace) before setting GIT_OPTIONAL_LOCKS=0, GIT_TERMINAL_PROMPT=0, GIT_NO_LAZY_FETCH=1, GIT_PAGER=cat. Fixed `-c core.fsmonitor=false` prevents configured monitoring effects. Preserve ordinary system/global safe.directory configuration and never set safe.directory=*.
6. Git may read trusted normal configuration, but no selected plumbing command invokes hooks, external tools or network. No user-supplied Git option is accepted. Captured stderr is discarded from public diagnostics; classify explicit local states and otherwise return bounded generic git-failed.

## Output and error design
See [CLI contract](contracts/inspect.md). JSON operational errors remain structured and also pass once through the existing diagnostic boundary. Detect context cancellation before emitting success. A broken output writer is runtime1; avoid emitting an additional result after any attempted write. No output stream can promise atomicity against arbitrary writer failures.

## Complexity Tracking
No new dependencies, general adapter framework or constitution amendment. Native jj/GitButler capabilities are deferred.
