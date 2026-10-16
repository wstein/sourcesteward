# Research: Submodule pin audit

## Why this feature

A superproject can record a submodule commit that exists only in one local submodule checkout. Local checks pass, a fresh clone cannot fetch the pinned commit, and nothing in `git status` says so. The incident that motivated this spec was a recorded gitlink that was not contained in the submodule remote's `main`. Existing SourceSteward capabilities retain gitlinks and refuse to manage them; none reports their publication state.

## Decisions and alternatives

**Remote-tracking refs as publication evidence (chosen).** `refs/remotes/*` are the local record of what a remote last advertised. Containment there is the strongest read-only, offline evidence. *Alternatives rejected*: `git ls-remote` or the hosting API (network effect, separate specification); `git branch -r --contains` through porcelain (output format and locale risk); counting local tags or branches (they say nothing about publication).

**No fetch (chosen).** A fetch writes refs and uses the network, which the read-only contract forbids. The result states that evidence may be stale and gives an advisory freshness time.

**Plumbing and fixed formats (chosen), and the normative process allowlist.** The audit starts only these Git invocations, each with the isolation prefix, explicit `--git-dir` and `--work-tree` where a submodule is involved, and optional locks disabled. Contracts and tests refer to this list.

- Superproject: `rev-parse --path-format=absolute --git-dir --git-path modules`, `rev-parse HEAD`, `ls-tree -r -z HEAD` (gitlinks), `ls-files --stage -z`, `config --blob HEAD:.gitmodules --null --list --no-includes` and the same for `:.gitmodules`, `config` reads of `extensions.submodulePathConfig` and `submodule.<name>.gitdir`.
- Per submodule: `rev-parse HEAD`, `symbolic-ref -q HEAD`, `cat-file -e <pin>^{commit}`, `merge-base <pin> HEAD`, two `rev-list --count --max-count=10001` calls (one per side), `for-each-ref --contains <pin> --count=64 --format=... refs/remotes/`, `ls-tree HEAD .gitmodules` (nested detection), `config` reads of the remote URLs.
- Dirty state: `diff-index --cached --no-ext-diff --no-textconv HEAD`, `ls-files --stage --debug -z` (streamed, compared with `lstat` in Go) and a bounded `ls-files --others --exclude-standard -z`.

Not used: `git status`, which can run clean and process filters; the inventory comparison, which is a poor fit; `git submodule` subcommands and `foreach`, which can run configured commands and mix human output with state; any fetch, ls-remote or clone.

**Names (recorded choice).** Result statuses `incomplete` and `refused` and the code `pins-failed` differ from inventory's `partial`, inspect's `unsupported` and `git-inventory-failed`. They describe an audit that must say whether it examined everything and are kept consistent within this contract. Refusal and finding codes follow the existing kebab-case vocabulary.

**jj refuses (chosen).** The jj documentation lists submodules as unsupported: they do not appear in the working copy and are preserved. The design document describes future phases and none is stated as implemented. A jj-owned workspace therefore gets an explicit refusal under constitution principle V instead of a Git fallback.

## Open measurements for the plan's tests

- Whether the isolated invocations leave the index file byte-identical and its modification time unchanged on the minimum and current Git versions (sentinel test, expected yes).
- Whether `merge-base --is-ancestor` on a shallow or partial submodule reports unknown cleanly (fixture).
- Behavior of Git's `safe.directory` check inside a foreign-owned submodule on Linux and macOS (fixture where permissions allow; otherwise recorded as not exercised).
- How Git 2.46 and the current release differ in `.gitmodules` parsing of unusual names; the fixtures run on both.

## Measurements recorded by the independent review (round 2, Git 2.47.3 and 2.56.0; 2.46 not available to the reviewer)

- An explicit `--git-dir`/`--work-tree` or `GIT_DIR` skips the ownership check that discovery applies: with `GIT_TEST_ASSUME_DIFFERENT_OWNER=1` a discovered submodule fails with "dubious ownership" while the explicit forms succeed on both versions.
- `rev-parse --git-path modules` is relative in a main worktree; `--path-format=absolute` is needed.
- Git 2.56 `extensions.submodulePathConfig` stores the Git directory under an encoded name (`modules/a%2fb`).
- `for-each-ref --contains` exits 129 for an absent object or a non-commit; `--count` applies after sorting; a remote's `HEAD` symref is listed and uses a slot.
- `config --blob HEAD:.gitmodules --null --list --no-includes` and `rev-list --count --max-count=10001 A..B` work on both versions; an absent end exits 128.
