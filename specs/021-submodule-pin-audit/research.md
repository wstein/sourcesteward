# Research: Submodule pin audit

## Why this feature

A superproject can record a submodule commit that exists only in one local submodule checkout. Local checks pass, a fresh clone cannot fetch the pinned commit, and nothing in `git status` says so. The incident that motivated this spec was a recorded gitlink that was not contained in the submodule remote's `main`. Existing SourceSteward capabilities retain gitlinks and refuse to manage them; none reports their publication state.

## Decisions and alternatives

**Remote-tracking refs as publication evidence (chosen).** `refs/remotes/*` are the local record of what a remote last advertised. Containment there is the strongest read-only, offline evidence. *Alternatives rejected*: `git ls-remote` or the hosting API (network effect, separate specification); `git branch -r --contains` through porcelain (output format and locale risk); counting local tags or branches (they say nothing about publication).

**No fetch (chosen).** A fetch writes refs and uses the network, which the read-only contract forbids. The result states that evidence may be stale and gives an advisory freshness time.

**Plumbing and fixed formats (chosen).** `ls-tree -z`, `ls-files -s -z`, `config --blob HEAD:.gitmodules --null --list --no-includes`, `rev-parse --git-path modules`, `for-each-ref --contains <pin> --count=17 refs/remotes/`, and two `rev-list --count --max-count=10001` calls (one per side), all with the isolation prefix. Tracked-change detection reuses the inventory comparison instead of `status`, which can run clean and process filters. *Rejected*: `git submodule status` and `git submodule foreach`, which can run configured commands and mix human output with state.

**Validated Git directory, not the pointer (chosen).** A hostile `.git` file can point anywhere. Reading it as data and requiring the target at the path Git itself reports for the worktree in use removes that class of attack. Git keeps `modules` per worktree: measured with a linked worktree, `rev-parse --git-path modules` is `.git/modules` in the main worktree and `.git/worktrees/<id>/modules` in the linked one, and the submodule's `.git` file read `gitdir: ../../super/.git/worktrees/wt/modules/member`. An explicit `--git-dir` also makes Git skip its own `safe.directory` check, so the audit checks ownership itself. *Rejected*: trusting `git -C submodule` and its own discovery.

**Untracked scan bounded to presence (chosen).** A full untracked listing can be unbounded and slow. Presence is what a reviewer needs; counts are not promised.

**Direct submodules only (chosen).** Recursion makes the bounds multiplicative. Nested submodules are reported and left for a later specification.

**jj refuses (chosen).** The jj documentation lists submodules as unsupported: they do not appear in the working copy and are preserved. The design document describes future phases and none is stated as implemented. A jj-owned workspace therefore gets an explicit refusal under constitution principle V instead of a Git fallback.

## Open measurements for the plan's tests

- Whether the isolated invocations leave the index file byte-identical and its modification time unchanged on the minimum and current Git versions (sentinel test, expected yes).
- Whether `merge-base --is-ancestor` on a shallow or partial submodule reports unknown cleanly (fixture).
- Behavior of Git's `safe.directory` check inside a foreign-owned submodule on Linux and macOS (fixture where permissions allow; otherwise recorded as not exercised).
- How Git 2.46 and the current release differ in `.gitmodules` parsing of unusual names; the fixtures run on both.
