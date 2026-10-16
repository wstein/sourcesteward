# Multi-repository inventory

`stw repos PATH... [--submodules] [--json]` inventories up to 16 repositories in one bounded,
read-only run and adds a small cross-repository summary. Each member is inspected and
inventoried by its own native backend: Git members with the default `stw inventory` scopes,
jj members with the `stored` scope only. The command writes to no repository, starts no
editor, prompt or pager, reads no standard input and contacts no network. The normative
text is the [contract](../specs/023-multi-repo-inventory/contracts/repos.md) and the
[specification](../specs/023-multi-repo-inventory/spec.md); the result is validated by
[`repos.schema.json`](../schemas/v1/repos.schema.json).

```sh
stw repos ~/work/app ~/work/lib ~/work/tools
stw repos ~/work/umbrella --submodules --json
```

`--submodules` adds the direct, registered submodules of every Git-owned explicit path as
entries after all explicit ones (sorted by path bytes). Submodules of submodules are not
traversed, and a jj, GitButler or ambiguous path has nothing to discover (`not-applicable`).

## What you get

One result with one entry per path, in argument order, then the discovered submodules.
Each entry carries its status, the backend, an error object when it is not clean, the
embedded inventory exactly as `stw inventory PATH --json` returns it, and a few derived
facts (head, dirty, conflicted references, stale, foreign and unverified workspaces).
A summary counts entries by status and backend and sums the facts. Human text and JSON
come from the same result.

Dirty state is a fact, not a status: a dirty repository is still `ok`. Dirty is `unknown`
for jj members (disk freshness needs a snapshot) and for submodule checkouts (Git reports
a submodule's Git directory, not its checkout, as its worktree).

## Exit status

| Exit | Meaning |
| --- | --- |
| 0 | every entry is `ok` or `skipped` (a skipped entry is an uninitialized submodule) |
| 1 | the overall result is `partial` or `error`, the run was canceled, or the output failed |
| 2 | usage error: no path, more than 16 paths, or an empty, NUL-containing, invalid UTF-8 or over-long (4,096 bytes) path; no JSON is printed |

Check each entry's `status` and `error`, not only the exit code. One bad member never hides
or damages the others.

## Limits that matter in practice

These are inherited from the single-repository inventory that `repos` reuses unchanged, and
they decide how often you will see exit status 1:

- **Content attributes.** Git inventory reads attributes for every tracked path with one
  `check-attr` call whose output is capped at 64 KiB. A repository with roughly 150 to 400
  tracked files (depending on path length) therefore fails with `git-inventory-failed`
  ("cannot safely observe content attributes"), even without any `.gitattributes`.
- **Record limit.** More than 1,024 aggregate inventory records fails with
  `git-inventory-failed` ("inventory record limit exceeded") or `inventory-limit`.
- **Always partial.** A repository with registered gitlinks, a bare repository and a
  repository with a stale linked worktree are `partial` (and exit 1).
- **jj output.** A jj inventory fails with `jj-output-limit` when one jj command prints
  more than 64 KiB (read from the code, not yet measured).

So a workspace of large or submodule-bearing repositories mostly ends with exit status 1 and
the cause on each entry; exit 0 means every member was fully observed. Lifting these limits is
a change to single-repository inventory and a separate specification.

Other bounds of `repos` itself: 16 entries in total (explicit plus discovered), 64 candidate
paths per `.gitmodules`, 4 workers, 10 seconds per call, 60 seconds in total (unfinished
entries report `inventory-timeout` or `inventory-not-started`) and 8 MiB of JSON (later
entries drop their embedded inventory and carry `output-limit`).

## Submodule discovery

Registration is read from the superproject's HEAD `.gitmodules` blob (a regular file of at
most 64 KiB) and confirmed as a gitlink in the index; a newly staged submodule is not found
until committed, and a staged removal is ignored. Paths that cannot be inventoried safely
(absolute, `..`, leading dash, not UTF-8) are counted in `discovery.ignored` and named,
escaped, in the parent's notes.

Everything a submodule says about itself is untrusted. Before any process runs in it, its
location is confined by the same checks as the submodule pin audit: no symlink in the path,
a `.git` pointer that names exactly the expected Git directory, ownership by the effective
user, no write access for others, and consistent `core.worktree`. A submodule that fails
is `path-unsafe`; an uninitialized one is `skipped` and does not change the exit status.
A submodule whose Git directory has `objects/info/alternates` or a `commondir` entry is
refused too, so a submodule that borrows objects through alternates is not inventoried. Directories writable by others are refused everywhere, including an empty uninitialized submodule directory: Git creates such directories under `umask 000`, and they are `path-unsafe` (the message names the `chmod -R o-w` fix) instead of `skipped`.

## Not provided

No ahead/behind counts, publication state or task ownership (single-repository inventory has
none), no discovery from a jj superproject, no recursion into nested submodules, no strict
exit mode and no path lists from standard input or files. The results are sampled and not
transactional across repositories.

## Effects

Read-only: no network, no fetch, no repository writes and no hooks. Hooks, clean and process
filters, external diff, textconv, fsmonitor, credential helpers and aliases are neutralized
as `stw inventory` does, and discovery commands run through the submodule audit's isolation
runner. jj inventories create and remove private temporary files outside the target
(inherited and disclosed in `boundary.effects`).

See also [inventory](inventory.md), [JSON contracts](json-contracts.md) and the
[commands](commands.md) overview.
