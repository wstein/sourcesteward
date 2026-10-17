# Submodule pin audit

`stw pins [PATH] [--json] [--strict]` is a read-only audit of the submodules of a Git
superproject. For each submodule it reports what the superproject records (the pin), what
is staged, what is checked out, and whether the pinned commit is contained in a
remote-tracking ref that the local submodule repository already holds. It answers one
question that no local check answers: could the next person or agent fetch this pin?

**Status.** Specified in [Spec 021](../specs/021-submodule-pin-audit/spec.md). The command
and its schema are merged but hidden until the conformance work item (WI5) completes, and
[feature status](feature-status.json) keeps it `planned`. This guide describes the
approved behavior; an installed `stw` may predate the merge and lack the command. The
[specification](../specs/021-submodule-pin-audit/spec.md) is normative; if this page and
the specification differ, the specification wins and this page is wrong.

```sh
stw pins
stw pins ../superproject --json
stw pins . --json --strict
```

`PATH` defaults to the current directory and may be inside the superproject worktree. The
command accepts no other flags and never prompts, opens an editor or pager, or contacts a
network. Human output and JSON come from one result.

## What it does not do

No network access, fetch, clone, `submodule init` or `submodule update`. No write to the
superproject, any submodule, their Git directories, index, refs, reflogs or configuration,
and no lock file is left behind. No hook, clean or process filter, external diff, textconv,
fsmonitor, credential helper or alias runs, and no configured URL is ever contacted. Nested
submodules are detected but not traversed. A jj, GitButler, ambiguous, bare or non-repository
target is refused; see [Refusals](#refusals).

## Evidence for each submodule

- **Registration.** Gitlinks come from the superproject HEAD tree (recursively) and the staged
  index; `.gitmodules` is read from the HEAD blob without includes and compared with the staged
  blob and the worktree file. A worktree symlink is never followed. A difference is reported as
  `gitmodules-modified`, never silently preferred. With an unborn HEAD the registration comes
  only from the staged blob and the HEAD pin is null. A HEAD `.gitmodules` that is a symlink or
  gitlink is not read: registration is unknown and no `gitlink-without-entry` is claimed.
- **Pin.** The commit recorded at HEAD, the commit staged in the index, and whether they differ.
  Commit identities are always full object names.
- **URL.** The registered URL with credentials removed, and the submodule's own remote URL. The
  match is `match`, `mismatch` or `unknown`. "The remote the submodule uses" is the remote whose
  normalized URL equals the registered URL; when none or several match, the match is `unknown`.
- **Checkout.** Initialized, uninitialized or refused; the checked-out commit; branch or
  detached; and the relation to the HEAD pin: `same`, `ahead`, `behind`, `diverged`,
  `unrelated` or `unknown` (objects needed to decide are missing). Each side is counted
  separately and capped at 10,000, with a `capped` flag.
- **Dirty state.** Tracked changes and untracked files, each `present`, `absent` or `unknown`,
  decided from the index and file metadata and never by hashing content. No file content is
  reported. A staged difference, a different size or mode, or a missing file is `present`;
  identical metadata is `absent`. A modification-time, change-time or inode-only difference, a
  racily clean entry, skip-worktree, assume-unchanged, intent-to-add or non-zero stage entries,
  gitlink entries, a size-0 smudged entry, an unparseable index record, or an exhausted dirty
  scan (3 seconds and 20,000 index entries per submodule) are `unknown` with a reason.
  Exceeding the dirty-scan budget only makes that value `unknown`; it never makes the result
  incomplete.
- **Publication.** The central evidence, below.
- **Tracking.** The pin's relation to the configured tracking ref (the `.gitmodules` `branch` or
  the remote's default ref): `equal`, `behind`, `ahead`, `diverged` or `unknown`, with bounded
  counts. A pin behind its tracking ref is information, not an error.
- **Nested.** Whether the submodule's HEAD tree contains a `.gitmodules` blob. Not traversed.

### Publication evidence

| Status | Meaning |
| --- | --- |
| `reachable` | the pinned commit is contained in at least one remote-tracking ref (`refs/remotes/*`) of the submodule repository; at most 16 refs are named, each with its remote, and truncation is flagged |
| `unreachable` | the commit object exists locally and no remote-tracking ref contains it; never reported for a shallow submodule |
| `unknown` | anything else, with a reason: an absent object, a non-commit, an uninitialized submodule, or a missing containment in a shallow submodule |

In a shallow submodule a missing containment is `unknown` ("shallow repository: the history is
cut"), never `unreachable`, because the cut history can hide the connection (a pin fetched alone
by `git submodule update --depth 1`); a positive stays `reachable`, so `pin-unpublished` is never
raised for a shallow submodule. Publication is evaluated only for the HEAD pin, or for the staged
pin only when HEAD records none (an unborn superproject HEAD); the staged pin of a path whose HEAD
pin differs is not evaluated, and `staged-pin-differs` says so.

Symbolic refs such as a remote's `HEAD` never count. Local branches and tags never count as
publication evidence. When several remotes exist, every remote-tracking ref is considered.

`unreachable` means exactly this: the local remote-tracking refs do not contain the pin, and
nothing more. It does not prove that the remote lacks the commit, because remote-tracking refs
are a local record of what a remote last advertised, and the audit makes no fetch. They may be
stale, or a remote may hold the commit on a branch that was never fetched. `reachable` is
likewise evidence only as fresh as the last fetch. When it can be read without writing, an
advisory last-fetch time (from `FETCH_HEAD` metadata) is reported; the result always carries
the staleness note and the finding `remote-refs-may-be-stale` whenever publication was
evaluated. Fetch deliberately, outside `stw pins`, and run the audit again to refresh the
evidence.

## Findings

Codes are append-only within `schema_version: 1`; severities are fixed. This table restates the
normative table of the [specification](../specs/021-submodule-pin-audit/spec.md) and must match it.

| Code | Severity | Meaning |
| --- | --- | --- |
| `pin-unpublished` | error | pin exists locally and no remote-tracking ref contains it |
| `gitlink-without-entry` | error | gitlink with no `.gitmodules` entry |
| `url-has-credentials` | error | registered URL embeds credentials |
| `submodule-path-unsafe` | error | submodule refused: path, Git directory, pointer or encoding unsafe |
| `submodule-unsafe-owner` | error | submodule refused: worktree or Git directory not owned by the effective user, or world-writable (or writable by a group other than the effective group) |
| `submodule-unsupported-backend` | warning | submodule is itself jj- or GitButler-managed, not audited |
| `pins-limit` | error | a bound was exhausted; result incomplete |
| `pins-state-changed` | error | state changed during the audit; result incomplete |
| `staged-pin-differs` | warning | staged pin differs from the HEAD pin |
| `checkout-differs-from-pin` | warning | checkout is ahead, behind, diverged or unrelated |
| `submodule-dirty` | warning | tracked changes or untracked files present |
| `url-mismatch` | warning | registered URL differs from the submodule's own remote |
| `entry-without-gitlink` | warning | `.gitmodules` entry with no gitlink |
| `gitmodules-modified` | warning | worktree or staged `.gitmodules` differs from the HEAD blob |
| `pin-object-absent` | error | submodule is initialized but the pinned commit object is not in its repository |
| `submodule-uninitialized` | info | submodule has no checkout |
| `pin-behind-tracking` | info | pin is behind its tracking ref |
| `nested-submodules-present` | info | nested submodules exist and were not traversed |
| `remote-refs-may-be-stale` | info | global, present whenever publication was evaluated |

### What a finding proves and does not prove

- A finding is evidence from one local sample, not a verdict. `pin-unpublished` says the local
  remote-tracking refs do not contain the pin (see above); it does not say the commit is gone
  from the remote.
- No error finding does not prove publication. An uninitialized or shallow submodule has
  publication `unknown` and no `pin-unpublished` error, and a staged pin that differs from the HEAD
  pin is not evaluated, so a passing `--strict` run does not prove that these pins are published. An `unknown` publication is not a pass.
- `submodule-dirty` and `checkout-differs-from-pin` describe the local checkout. They do not say
  which side is right and do not suggest a repair; the audit never repairs registration.
- `url-has-credentials` is raised without contacting the URL; the credentials never appear in
  any output.
- A refused submodule (`submodule-path-unsafe`, `submodule-unsafe-owner`,
  `submodule-unsupported-backend`) still appears with only the fields that were safe to read. Its
  refusal does not by itself make the result incomplete; the other submodules are still reported.
- A superproject whose current branch name is not valid UTF-8 cannot be identified, so the whole
  result is `pins-failed` with a null identity (a recorded exception to per-field escaping, which
  covers everything else the audit reads).
- A non-UTF-8 submodule path cannot be represented in JSON: the entry is refused with
  `submodule-path-unsafe` and shown escaped (`\xNN`) with `path_encoding: "escaped"`.

## Exit statuses

| Status | Meaning |
| --- | --- |
| 0 | complete result; findings do not change this without `--strict` |
| 1 | refusal, operational failure, cancellation or an incomplete result, with or without `--strict` |
| 2 | usage error |
| 3 | `--strict`, a complete result and at least one `error` finding |

Precedence: 2, then 1, then 3, then 0. An incomplete result exits 1 even when it has error
findings, so `pins-limit` and `pins-state-changed` never reach status 3. On status 3 one line
on standard error names the number of error findings, and standard output still carries the
full result.

## Refusals

| `error.code` | `status` | `backend` | When |
| --- | --- | --- | --- |
| `unsupported-backend` | `refused` | `jj` or `gitbutler` | jj-owned (including colocated and secondary) or GitButler-managed workspace |
| `ambiguous-backend` | `refused` | `ambiguous` | ownership markers conflict |
| `bare-repository` | `refused` | `git` | no worktree to compare |
| `not-repository` | `refused` | `unknown` | no repository at the path |
| `pins-failed` | `error` | `git` or `unknown` | an operational failure such as a failed Git invocation, an unreadable path or cancellation; `git` only once Git ownership is established, `unknown` for a failure before that (for example a superproject branch name that is not valid UTF-8, with a null identity) |

Git-owned worktrees, including linked worktrees, are audited; a linked worktree's submodules
live under that worktree's own `modules` directory. jj has no Git submodule model, so a jj-owned
workspace, even a colocated one, is refused with `unsupported-backend` and the audit does not
fall back to Git; run it in a Git-owned checkout. A jj or ambiguous refusal is decided from
filesystem markers and starts no process of any kind. The other refusals use only the existing
detection's Git processes and never start jj or a submodule command. A submodule that is itself
jj- or GitButler-managed is refused individually and the others are still audited.

## Limits and completeness

| Limit | Value |
| --- | --- |
| submodules | 256 |
| time per submodule / in total | 5 seconds / 30 seconds |
| retained output | 1 MiB |
| remote-tracking refs named per pin | 16 |
| ahead and behind counts | 10,000 each, with a `capped` flag |
| dirty scan per submodule | 3 seconds and 20,000 index entries (yields `unknown`, never `pins-limit`) |

Exhausting a bound marks the result `incomplete` with `pins-limit`; beyond 256 submodules only the first 256 are examined and a global `pins-limit` finding reports the total. The audit also resamples the
superproject HEAD and index identity and each submodule HEAD before success; a change marks the
result `incomplete` with `pins-state-changed`. An incomplete result is never a complete success.
An `unknown` observation, an uninitialized submodule or an absent pinned object is a valid
observation and does not make the result incomplete.

## Safety

`.gitmodules`, submodule paths, `.git` pointer files, URLs, refs and Git-directory files are
untrusted data and are rendered with control characters escaped. A submodule's working directory
and Git directory are resolved without following symlinks and accepted only at the locations
Git itself reports for the worktree in use (or an in-place `.git` directory); anything that
escapes, including `commondir`, alternates or `core.worktree`, is refused. Because an explicit Git
directory bypasses Git's own ownership check, the audit verifies that each submodule worktree and
Git directory is owned by the effective user and is not world-writable (nor writable by a group
other than the effective group) before any Git process runs there; otherwise the submodule is
refused with `submodule-unsafe-owner`. Git creates world-writable directories under umask 000, so
the remedy is `chmod -R o-w` on the submodule or a re-clone under a stricter umask. A `commondir`
entry or any non-empty `objects/info/alternates` is refused as `submodule-path-unsafe`.

The Git isolation (replace objects, optional locks, fsmonitor, hooks path, credential helper, ssh
command, every transport, gc, commit-graph, the graft file, no inherited `GIT_*` variable) is
specified in [Spec 021 research](../specs/021-submodule-pin-audit/research.md), which also lists
the fixed Git invocations, and exercised by the hostile-repository tests; this page does not
restate it as a guarantee.

## JSON

`--json` emits one `PinsResult` object with `schema_version: 1` and `command: "pins"`, validated by
`schemas/v1/pins.schema.json`. It includes refusals and operational errors, explicit `effects`
(`network`, `fetch`, `writes`, `hooks` all false), `limits`, completeness, the staleness note, a
summary of counts and the findings. See [JSON contracts](json-contracts.md) and the
[contract](../specs/021-submodule-pin-audit/contracts/pins.md).

## Backends

Git is supported. jj and GitButler are explicitly unsupported for submodules and no equivalence is
claimed. See [backends](backends.md).

## Out of scope

Bumping a pin, fetching or pushing a submodule, traversing nested submodules, repairing
registration, comparing a pin with a hosted repository (through [hosting](hosting.md)) and CI lanes
for further platforms each need their own specification.
