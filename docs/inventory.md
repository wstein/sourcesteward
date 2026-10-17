# Workspace, state and reference inventory

`stw inventory [PATH]` observes local Git worktrees or native jj workspaces, references,
base candidates and selected file-state scopes. It performs no target writes, repair,
snapshots or fetches. PATH defaults to the current directory. GitButler and ambiguous
ownership refuse execution; a jj-managed repository never falls back to Git.

```sh
stw inventory . --json
stw inventory . --query ref-exists --ref topic --json
stw inventory . --query ancestor --from SOURCE --to DESTINATION --json
stw inventory . --query dirty --scope disk --scope untracked --json
stw inventory . --scope stored --base BASE --revision REVISION --json
stw inventory . --scope untracked --path 'literal[*].txt' --json
```

These examples use explicit literal selectors. Git accepts unique local/reference names,
HEAD and full object IDs; jj accepts exact bookmarks (including native remote names),
`@`, full commit IDs and full change IDs. Expressions, arbitrary revsets and aliases are
not evaluated. Ambiguous or unavailable endpoints are unknown or an explicit failure.

## Results and exits

Human and JSON output describe the same observations. Inventory has its own schema v1,
independent of the existing inspect/version schema versions. Check `command` and
`schema_version`, then `status`, `boundary.complete` and each observation's qualification.

- `ok`, exit 0: requested observations completed. A known false predicate also exits 0.
- `partial`, exit 1: requested evidence is unavailable; unknown is never false or clean.
- `error`, exit 1: the observation failed, changed relevant state was detected or a limit was exceeded.
- Usage errors exit 2. Cancellation or output failures may prevent a complete JSON result.

Reference-existence and ancestry predicates collect only their relevant evidence, so
unrelated unknown disk state does not fail a known predicate. Predicate truth is not
encoded as process success/failure. No inventory result authorizes tidy or another mutation.
Task ownership remains unknown; manager detection does not establish that a task belongs to you.

## Scopes and paths

Default scopes are `disk`, `tracked` and `untracked`. Repeat `--scope` to select an
explicit set; repeat `--path` for literal paths or slash-bounded subtrees. Filters
match either side of a reported rename/copy. Names containing commas, spaces, newlines
or glob characters remain literal. `--anchor workspace` is the default;
`--anchor cwd` uses the caller's current directory, which must be inside the selected
workspace. A filter escaping the workspace is refused.

For Git, `staged` compares index to HEAD; `disk` compares tracked working files to index;
`tracked` combines those two scopes. `untracked` excludes ignored files; `ignored` is
opt-in. Stored comparisons use selected revision versus explicit base, otherwise the
sole parent; root commits use an empty tree. Multi-parent comparisons need an explicit
base. Native normalization, submodule and unavailable disk observations carry explicit
limitations; configured content helpers are never executed.

For jj, `stored` compares the chosen commit against explicit base or its native
merged-parent tree. This does not include unsnapshotted edits. Disk, tracked, untracked
and ignored freshness remain unknown; staging is unsupported. Use `--scope stored`
for stored evidence, never as a disk-cleanliness assertion. Enforced read-only
bubblewrap (Linux) or sandbox-exec (macOS) is mandatory; no unsandboxed retry occurs.
Private temporary isolation/probe files outside the target are created and cleaned up.

## References, bases and limits

`--base` selects ancestry-based integration evidence; `--revision` selects containment
and a stored comparison endpoint. `--remote NAME` selects only locally sampled native
remote references for local-only reachability. These samples are not fetched and do
not prove that a revision was never published. Ancestry, squash equivalence and forge
integration are distinct; this command does not invent the latter two.

Base candidates report provenance and native identities. No name such as main, master
or origin is assumed. jj configured aliases are excluded by configuration isolation;
unavailable configuration-derived trunk evidence remains explicitly qualified rather
than executed. Multiple candidates require explicit selection.

The total deadline is ten seconds, results are capped at 1 MiB and returned records at 1,024. For
Git, commands whose output grows with the repository (the index, content attributes, staged,
untracked and stored changes, references) are read incrementally, so a repository of thousands of
tracked files or hundreds of tags gives facts; the 64 KiB `command_bytes` value now names the
largest unit held at once (one record, or the whole output of a command that is not streamed).
Git raw file reads are confined to the workspace and hashed in chunks up to 16 MiB per file, with a
256 MiB read budget per run; larger files and an exhausted budget remain qualified `unknown`
rather than blocking a process. The jj adapter is unchanged and keeps its 64 KiB per-command limit.

### When a bound is reached

A bound that used to be an error degrades to a partial result with a verbatim disclosure in
`boundary.limitations` wherever an omission cannot create a false claim. More than the record share
of changed files returns the first records in path order and says `file listing truncated: N file
records omitted ...; omitted paths are not clean`; more references than the share omit tags first
(`reference listing truncated: ...`) while `ref-exists`, `ancestor`, bases and upstreams use every
reference; an index above 100,000 entries or a scan above 64 MiB makes that workspace's disk
comparison `unknown`; a result above 1 MiB is trimmed (`listing trimmed ...`). More than 20,000
references, or more workspaces and bases than the record limit, remain `inventory-limit` errors
because a listing cut at an unknown point could make a predicate lie. With `--query`, a known
predicate keeps `ok` and exit 0 and the truncation is only noted in `boundary.limitations`
(the listing is supporting evidence); `clean` and `dirty` are computed from every candidate, never
from the returned listing.

### Submodule checkouts, separate Git directories and gitlinks

For a checkout whose Git directory is elsewhere (a submodule, `git init --separate-git-dir`) the
workspace record is the checkout, not the Git directory, so a clean submodule is `tracked: false`
and its modified files are reported with checkout-relative paths. Where the checkout cannot be
identified (a linked worktree of a separate Git directory, a hostile `core.worktree`) the entry is
`present` with every observation `unknown` and the reason `Git directory listed as workspace;
checkout not identified from this worktree`. For a superproject with registered gitlinks, `tracked`
is `true` when a staged change or an ordinary tracked file differs; it stays `unknown` otherwise
because submodule worktrees are never opened (use `stw pins` for submodule state), so a plain run
stays `partial` and only `--query dirty` can answer with exit 0. Skip-worktree entries absent from
disk (sparse checkouts) are no longer reported deleted.
Results are sampled and nontransactional; races and ABA changes cannot all be detected.
See the [normative contract](../specs/009-workspace-inventory/contracts/inventory.md)
and [JSON contracts](json-contracts.md). See also the [Spec 027 amendment](../specs/027-inventory-limits/contracts/inventory-limits.md)
for the limits, degradation, checkout and gitlink tables and the compatibility notes. Published
schema/deployment and minimum/current acceptance evidence are recorded separately from source
implementation.
