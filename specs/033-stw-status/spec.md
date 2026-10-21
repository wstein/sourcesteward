# Specification: `stw status`, the workspace overview

**Created**: 2026-10-21 | **Status**: implemented in source (spec-light: code and tests first; this page is the contract only).

`stw status [PATH] [--fetch] [--worktrees] [--json]` answers "where am I" for the whole workspace in one screen:
what `git status`, `git worktree list` and `git branch -vv` tell, for the top-level superproject and every
initialized submodule (nested ones too). Git only; a jj-owned path is refused (`status-unsupported-backend`).

## Rules

1. **Workspace.** Found from any directory inside it (a subdirectory, a submodule, a nested submodule or a linked
   worktree) exactly as `stw sync` finds it (`workspaceRoot`, `submodules`); repositories are listed root first, then
   submodules depth-first. An uninitialized submodule is listed with `state` `uninitialized` and never cloned. At most
   256 repositories (`status-limit`); worktrees, branches and files are not limited.
2. **Unit.** The main worktree of each repository. Linked worktrees are only counted (and listed with `--worktrees`).
3. **Read-only.** Nothing is changed. `--fetch` runs one `git fetch --no-tags --no-write-fetch-head
   --no-recurse-submodules --no-prune` per repository (remote-tracking refs only); a failure is `fetch_error`, not an error.
4. **Facts per repository**: `branch` or `detached`; `upstream` with `ahead`/`behind` (null without an upstream or when it
   is gone, `upstream_gone`); `changes` (`staged`, `unstaged`, `untracked` file counts, a file changed in index and tree
   counts in both; `conflicts` = unmerged files); `operation` (`rebase`, `merge`, `cherry-pick`, `revert`, `bisect`,
   `unreadable`); `stash` count; `branches` (`local` count; `integrated` = local branches merged into the default branch of
   the upstream remote, `not_integrated` the rest, both excluding the default branch itself; null when no default branch
   is known, which is `refs/remotes/REMOTE/HEAD`, else `main`, else `master`); `worktrees` (`linked`, `dirty` = any change
   including untracked files, `locked`, `prunable`, `missing` (directory gone, not claimed dirty), `list` only with `--worktrees`); and for a submodule `gitlink`: the
   commit the superproject records (`HEAD:PATH`), the commit checked out, `state` `ok`/`drift`/`uninitialized` and, on
   drift, `relation` `ahead`/`behind`/`diverged`/`unrelated` (no common ancestor)/`unknown` of the checked-out commit. A changed submodule in the superproject's `git status` (the gitlink change) is not counted in `changes`; the gitlink check reports it.
5. **Attention** (the exact rule): `attention` is true, and `reasons` is non-empty, when the repository has one of these, listed in this order: `operation`, `conflicts`, `staged`, `unstaged`, `untracked`, `behind`, `diverged` (ahead and behind), `no-upstream` (on a branch), `detached` (HEAD detached in the main worktree), `gitlink-drift` (the checked-out commit is behind, diverged from or unrelated to the recorded one), `error`. Everything else is information only and never counts: commits ahead, a gone upstream, stashes, branches not integrated, linked worktrees (dirty, locked, prunable), a failed fetch, an uninitialized submodule, and a gitlink whose checked-out commit is *ahead* of the recorded one (shown as `gitlink: recorded A, checked out ahead (B)`, the normal state while gitlinks are updated in batches).
6. **Exit status.** 0, also when repositories need attention; 1 when a repository cannot be read (`state` `error`) or the
   run fails (`error` set, `repos` empty; `--json` still prints it); 2 for usage errors with no output.
7. **Output.** Text: one block per repository (marker `!` when it needs attention, name, branch, upstream state, change
   counts, then one indented line per non-trivial fact) and a final line `N of M repositories need attention` or
   `M repositories, nothing needs attention`. Colour only on a terminal and with `NO_COLOR` unset; repository text is escaped.

## JSON contract

[`schemas/v1/status.schema.json`](../../schemas/v1/status.schema.json) (unpublished like the sync schema): one object
with `schema_version` 1, `command` `status`, `options` (`fetch`, `worktrees`), `root`, `note` (set when started inside a
linked worktree), `repos`, `summary` (`total`, `attention`, `errors`) and `error`. Keys are never omitted; absent values
are null. Repository members are those of rule 4 plus `name`, `path`, `kind` (`root`/`submodule`), `state`
(`ok`/`uninitialized`/`error`), `head`, `fetch_error`, `attention` (boolean), `reasons` (list) and `error`. Worktree members: `path`, `branch`,
`detached`, `dirty`, `locked` (the lock reason, `""` if none), `prunable`. Error codes: `status-invalid-options`,
`status-git-unavailable`, `status-unsupported-backend`, `status-not-a-repository`, `status-read-failed`, `status-limit`.
