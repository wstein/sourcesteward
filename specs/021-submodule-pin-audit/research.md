# Research: Submodule pin audit

## Why this feature

A superproject can record a submodule commit that exists only in one local submodule checkout. Local checks pass, a fresh clone cannot fetch the pinned commit, and nothing in `git status` says so. The incident that motivated this spec was a recorded gitlink that was not contained in the submodule remote's `main`. Existing SourceSteward capabilities retain gitlinks and refuse to manage them; none reports their publication state.

## Decisions and alternatives

**Remote-tracking refs as publication evidence (chosen).** `refs/remotes/*` are the local record of what a remote last advertised. Containment there is the strongest read-only, offline evidence. *Alternatives rejected*: `git ls-remote` or the hosting API (network effect, separate specification); `git branch -r --contains` through porcelain (output format and locale risk); counting local tags or branches (they say nothing about publication).

**No fetch (chosen).** A fetch writes refs and uses the network, which the read-only contract forbids. The result states that evidence may be stale and gives an advisory freshness time.

**Plumbing and fixed formats (chosen), and the normative process allowlist.** The audit starts only these Git invocations, each with the isolation prefix, explicit `--git-dir` and `--work-tree` where a submodule is involved, and optional locks disabled. Contracts and tests refer to this list.

- Superproject: `rev-parse --path-format=absolute --git-dir --git-path modules` (discovery from the validated root), `config --no-includes --bool --get extensions.submodulePathConfig`, `rev-parse --verify --quiet HEAD`, and only when that exits 1, `symbolic-ref -q HEAD` and `for-each-ref --format=%(refname) -- <ref>` to confirm an unborn HEAD, `ls-tree -r -z --full-tree HEAD` (gitlinks and `.gitmodules`), `ls-files --stage -z`, `config --blob HEAD:.gitmodules --null --list --no-includes` and the same for `:.gitmodules`, `config --file - --null --list --no-includes` (the worktree `.gitmodules` bytes on standard input), and `config --local --null --list --no-includes` filtered to `submodule.*.url|active|gitdir`, `submodule.active` and the extension flag.
- Per submodule, validation: `config --file <gitdir>/config --null --list --no-includes` and the same for `<gitdir>/config.worktree` (an include key or a core.worktree differing from the validated worktree refuses the submodule; no repository is read), run only after ownership and content checks. Remotes: `config --local --null --list --no-includes` filtered to `remote.*.url`.
- Per submodule, evidence (WI3): `rev-parse HEAD`, `symbolic-ref -q HEAD`, `cat-file -e <pin>^{commit}`, `merge-base <pin> HEAD`, two `rev-list --count --max-count=10001` calls (one per side), `for-each-ref --contains <pin> --count=64 --format=... refs/remotes/`, `ls-tree HEAD .gitmodules` (nested detection).
- Per submodule, evidence added by WI3 (documented here as WI2 did): `config --local --null --list --no-includes` filtered to `remote.*.url|promisor|partialclonefilter`, `extensions.partialclone*`, `core.filemode` and `core.symlinks` (one read for remotes, partial clone and the dirty comparison), `for-each-ref --format=%(refname) -- <ref>` to confirm an unborn submodule HEAD, `symbolic-ref -q refs/remotes/<remote>/HEAD` and `rev-parse --verify --quiet refs/remotes/<remote>/<branch>^{commit}` for the tracking ref (the branch text is checked against a plain-name pattern first), and the resample `rev-parse --verify --quiet HEAD`.
- Dirty state (WI3): `diff-index --cached --no-ext-diff --no-textconv HEAD`, `ls-files --stage --debug -z` (streamed, compared with `lstat` in Go) and a bounded `ls-files --others --exclude-standard -z`.

Every invocation runs with the isolation prefix of `pinsIsolation()` (replace objects, optional locks, literal pathspecs, fsmonitor, hooks path, untracked cache, credential helper, ssh command, every transport switched off by name, gc and maintenance off), `GIT_CONFIG_NOSYSTEM=1`, no inherited `GIT_*` variable and a 5 second deadline. `pins_git_test.go` compares every recorded invocation with these argument shapes.

**Operational note (umask).** Git creates submodule directories with the process umask. Under umask 000, as in this devcontainer, every submodule worktree and Git directory is world-writable and the audit refuses it with `submodule-unsafe-owner`, although the owner is correct; the check covers the worktree, the modules directory and every directory walked inside the Git directory, and the message names the mode and recommends `chmod -R o-w` or re-cloning under a stricter umask. A group-writable directory is accepted only for the effective group. `stw pins` documentation (T018) must repeat this.

**Owner check coverage (T007).** The environment runs unprivileged and cannot chown, so foreign ownership is exercised through the injected owner seam only; the real extraction is checked against a file the test creates (the effective user) and against `/` (root). Real foreign-owned submodules, macOS and root with `SUDO_UID` are not exercised and are recorded for the verification note of T021.

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

**Alternates.** A submodule Git directory with a non-empty `objects/info/alternates` is refused with `submodule-path-unsafe`, never audited: Git resolves entries by realpath and keeps a trailing carriage return, which the audit does not re-implement, so no confinement check could be trusted. Users who share objects through alternates must use another mechanism for the audit (for example a separate clone).
