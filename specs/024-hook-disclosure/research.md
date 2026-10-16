# Research: Hook and bypass disclosure

## Why this feature

WorkHarbor enforces secret scanning, commit-message linting and formatting through Git hooks: `core.hooksPath` points at a tracked `.githooks` directory holding `pre-commit`, `commit-msg` and `pre-push`. A maintainer is considering jj. jj does not run Git hooks as far as its documentation says, so a commit made with jj could bypass every guard without any warning, and nothing in `jj status` or `git status` says so. SourceSteward can answer, read-only, which guards exist, whether they are safe, and whether the owning backend's operation runs them, and it must say how well established that answer is.

## Decisions and alternatives

### Command name (recorded choice)

Ratings from 1 (poor) to 5 (good) for discoverability, accuracy of the scope, a contract that leaves existing schemas unchanged, and brevity.

| Candidate | Discoverability | Scope accuracy | Existing contracts unchanged | Brevity | Total | Verdict |
| --- | --- | --- | --- | --- | --- | --- |
| `stw hooks` | 5 | 4 (reports hooks and bypass) | 5 | 5 | 19 | chosen |
| `stw guards` | 3 | 4 (the concept, not the word people search) | 5 | 5 | 17 | rejected: users and agents look for "hooks" |
| `stw inspect --hooks` | 4 | 3 | 1 (extends `inspect`, whose schema 1 and 2 must stay unchanged) | 4 | 12 | rejected |
| `stw bypass` | 2 | 2 (narrower than the disclosure) | 5 | 5 | 14 | rejected |
| `stw policy` | 2 | 1 (too broad) | 5 | 5 | 13 | rejected |

### Operations are fixed to `commit` and `push` (chosen)

The motivating guards run on those two operations, and a closed pair keeps the report bounded and the claims measurable. Alternative: an `--operation` flag or the full hook-to-command matrix (merge, rebase, checkout hooks). Rejected for this slice: each added operation needs its own measurement on both backends. Other client hooks are listed when present, with state only.

### Global and system configuration are in scope as read-through (chosen)

Git applies system and global `core.hooksPath` and `hook.*` to the user's later command, and a global hooks directory is a common setup. Omitting them would report a guard as absent that Git runs, which is the wrong kind of mistake for a disclosure. The command therefore reads configuration through `git config --show-scope --show-origin`, lists every level, and marks the effective value. It reports the origin file path only for repository-level values and only the scope for system and global values; the values themselves are reported as read and can contain home-directory paths. Alternatives rejected: ignoring non-repository levels (silently wrong); reading configuration files directly in Go (include and `includeIf` semantics would drift from Git); applying the caller's `GIT_CONFIG_*` environment (the existing runner removes `GIT_*` so reports are reproducible; the command only names the variables it did not apply).

### Evidence model (chosen)

Every claim has a level: `measured`, `documented` or `unverified`, and the report lowers `measured` to `documented` when the detected backend version lies outside the inclusive range the conformance lane measured. This mirrors `docs/backends.md`: an untested release is not equivalent to recorded conformance. jj-commit is `no` at `documented` and jj-push is `unknown` at `unverified`, because the official configuration documentation says "Git remote interactions are handled by spawning a `git` subprocess", and the documentation does not say whether that subprocess runs `pre-push`. Claiming `no` for push from the one-line "Hooks: No" would be a guess presented as a fact.

### Git claims reach `measured` only through the lane

The measurements below were made once, with Git 2.56.0, in scratch repositories. The spec ships the Git claims as `documented` and promotes them when the conformance lane (FR-017) has recorded them on 2.46.0 and 2.56.0. Alternative: label them `measured` now. Rejected: the constitution requires conformance CI and recorded acceptance evidence, and the minimum lane was not measured.

### Reading hooks: lstat of fixed names, never open (chosen)

Only the 28 catalog names are examined with `lstat`, `readlink` and `access(X_OK)`; no hook path is opened. Directory listing (bounded, 256 entries) happens only for a directory inside the worktree or the Git directory, to find misnamed hooks, because listing a hostile `core.hooksPath` such as `/` would disclose unrelated names. Alternatives rejected: `git hook list` (needs a newer Git than the minimum lane and prints advice to standard error; not measured on 2.46; it also depends on executable checks the command performs itself); reading shebang lines to detect missing interpreters (violates "never read hook contents"); dry-running a hook (executes it).

### Configuration hooks (chosen)

Git 2.56.0 honors `hook.<name>.event` and `hook.<name>.command`; they run in addition to directory hooks and still run when `core.hooksPath` is the null device (measurements 10). The command reads names, events and enabled state with `git config -z --show-scope --get-regexp '^hook\.'` and never reports command text, which is configuration that may hold secrets. The Git version that introduced the feature is unverified; below the measured range the `engine` field is `unknown` rather than guessing.

### jj in a colocated workspace: Git facts through read-only Git (chosen)

The hook configuration of a colocated workspace lives in the Git repository jj shares. Alternatives: refuse for jj (loses the motivating case); parse `.git/config` in Go (include semantics drift, hostile input); ask jj (jj's configuration interface describes jj configuration, not Git's; unverified that any `jj` command exposes `core.hooksPath`). The chosen design runs only the allowlisted read-only `git config` and `rev-parse` reads, brackets them with the `rev-parse --absolute-git-dir` ownership gate (measurement 13: `git config` does not apply Git's ownership check), and verifies that the discovered Git directory is the one the workspace's `.git` entry designates. These reads run outside the jj sandbox, and the contract says so. This is allowed by `docs/backends.md` rule 5 because the effects are specified here and tested; it is not a fallback for jj ownership and it writes nothing. A jj workspace without a `.git` entry is not evaluated: its guard set is reported as not evaluated, because jj's `git` subprocess (used for push) may read system and global Git configuration and the internal store repository's hooks directory, and examining those would mean guessing which Git configuration jj's subprocess sees. The conformance step J5 measures that case instead. A secondary workspace of a colocated repository is a Git worktree by default according to the jj documentation (`jj workspace add`, reported by the independent review from docs.jj-vcs.dev/latest, not re-fetched by the author), so it has a `.git` gitfile and is handled as colocated; J6 measures the default on both lanes.

### GitButler is refused, explicitly (chosen)

GitButler ownership is detected; its hook behavior is not specified anywhere this feature relies on. The command refuses with `unsupported-backend` and claims nothing.

### Command visibility

As in Spec 021, the command is registered hidden until its final work item so a partial stack never advertises a half feature.

## Documentation evidence for jj (accessed 2026-10-16)

Recorded with WebFetch; the answers below are the fetch tool's extraction of the pages, quoted as returned, and are not a measurement.

- https://docs.jj-vcs.dev/latest/git-compatibility/ , section "Supported features": "**Hooks: No.** There's [#405] specifically for providing the checks from [https://pre-commit.com]." The page does not mention `pre-commit`, `pre-push`, `commit-msg` or `core.hooksPath`. The same section lists "Submodules: No" and "git-worktree: No".
- Same page, "Colocated Jujutsu/Git workspaces": "A colocated Jujutsu workspace is a hybrid Jujutsu/Git workspace." "The Git repo and the Jujutsu workspace then share the same working copy." "Jujutsu will import and export from and to the Git repo on every `jj` command automatically."
- https://github.com/jj-vcs/jj/issues/405 "Integrate with https://pre-commit.com/", open. Summary: the maintainer notes pre-commit assumes a Git repository; a `jj run` command exists; commenters point at `jj fix`; a configurable jj hook mechanism (events such as before-push) is only a proposal; third-party tools are mentioned. No statement that jj runs Git hooks.
- https://docs.jj-vcs.dev/latest/config/ , "Git subprocessing behavior": "Git remote interactions are handled by spawning a `git` subprocess. If `git` is not on your OS path, or you want to specify a particular binary, you can: [git] executable-path = ..." The page does not say which operations use it and does not mention hooks. Only the first 100,000 of 114,529 characters were read.

Conclusion for the spec: the documentation supports `jj-commit: no` at the level `documented`. It does not establish `jj-push`, because push spawns `git`. Neither is measured; jj is not installed in the author's environment.

## Git measurements (Git 2.56.0, scratch repositories, 2026-10-16)

All run with `GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1` and a private `HOME`, using hook scripts that append their name to a log, under `/tmp/claude-1000/.../scratchpad`. Git 2.46.0 (the minimum lane) was not available; nothing below is claimed for it.

1. `core.hooksPath=.githooks` with `pre-commit`, `prepare-commit-msg`, `commit-msg`, `post-commit`, `pre-push`, `reference-transaction`: `git commit` ran `pre-commit`, `prepare-commit-msg`, `commit-msg`, several `reference-transaction` calls and `post-commit`. `git commit --no-verify` ran `prepare-commit-msg`, `reference-transaction` and `post-commit` and skipped `pre-commit` and `commit-msg`.
2. `git push origin main` ran `pre-push` (arguments: remote name and URL) and `reference-transaction`; `git push --no-verify` skipped `pre-push` and still ran `reference-transaction`.
3. A relative `core.hooksPath` ran from a subdirectory and resolves against the worktree root: `rev-parse --git-path hooks` printed `../.githooks` from the subdirectory but `--path-format=absolute` printed the worktree-root path; in a linked worktree the absolute form resolves under that worktree's own root. Without `core.hooksPath`, the absolute form is the common `<git-dir>/hooks`, shared by linked worktrees.
4. `core.hooksPath=/dev/null` and `core.hooksPath=nonexistent` ran no hook; `rev-parse --git-path hooks` printed `/dev/null` for the first.
5. A non-executable hook was skipped with a hint on standard error and the commit succeeded; a dangling symlink hook was skipped and the commit succeeded; a symlink to a script outside the repository ran; a hooks directory that is a symlink to another directory ran; a world-writable hooks directory ran; a directory named `pre-commit` made `git commit` fail ("cannot exec ... Permission denied"); an executable with a missing interpreter made it fail ("cannot exec ... No such file or directory").
6. `core.hooksPath='~/hk'` ran the hook; `rev-parse --git-path hooks` printed the expanded path.
7. `core.hooksPath=''` (empty): `rev-parse --git-path hooks` exited 128 ("The empty string is not a valid path") while `git commit` succeeded and ran no hook.
8. `extensions.worktreeConfig=true` with `git config --worktree core.hooksPath /dev/null` in a linked worktree: that worktree ran no hook; `git config --show-origin --show-scope --get-all core.hooksPath` printed a `local` line and a `worktree` line (file `.git/worktrees/<id>/config.worktree`) in precedence order; from the main worktree only the local value. The origin was printed relative (`file:.git/config`) in one invocation and absolute in another, so origins are data to normalize, not a contract.
9. A global-config `core.hooksPath` is listed with scope `global` and applied by `rev-parse --git-path hooks`; a value from an included file is listed with that file as its origin and applied. A repository configuration that includes a file that is not valid configuration makes every Git command in that repository fail with exit 128.
10. Configuration hooks: `hook.mine.event=pre-commit` and `hook.mine.command=...` ran in addition to the directory hooks, still ran with `core.hooksPath=/dev/null`, and `hook.mine.enabled=false` stopped them. `git config -z --show-scope --get-regexp '^hook\.'` printed scope, key, value records. `git hook list pre-commit` did not execute anything and exited 1 with "no hooks found" for a non-executable or absent hook; an unknown event exited 1. A section name containing a space was accepted.
11. `git init --template=<dir>` copied `<dir>/hooks/pre-commit` into `.git/hooks`, where it ran like any hook; the default template installs only `*.sample` files (14 listed), which never run.
12. `git -c core.hooksPath=/dev/null commit` ran no hook.
13. With `GIT_TEST_ASSUME_DIFFERENT_OWNER=1`, discovery-based `git rev-parse --git-path hooks` failed with "dubious ownership" while the same query with explicit `--git-dir` and `--work-tree` succeeded. Re-measured after the first review: `git config --show-scope -z --get-all core.hooksPath` and `git config --get extensions.worktreeConfig` exited 1 with no output under the same variable, that is, `git config` does not apply the check and its exit status 1 looks like "unset". The design therefore requires the `rev-parse --absolute-git-dir` gate around the reads. (The `rev-parse` behavior was also recorded by the review of Spec 021.)
14. The scratchpad filesystem of the author's environment reported mode 0777 for a file created with mode 0755; a file under `/tmp` reported 0755. Fixtures must create files and read the resulting mode back; tests must not assume the host filesystem preserves modes.

15. A valueless `[core] hooksPath` key: `config -z --get-all core.hooksPath` printed a single NUL (the same bytes as an empty value), `config -z --get-regexp '^core\.hookspath$'` printed `core.hookspath` NUL (an empty value prints a newline before the NUL), and `git commit` failed ("missing value for 'core.hookspath'"). Reported by the review for `~nosuchuser/hk` as well: `rev-parse --git-path hooks` and `git commit` fatal.
16. A directory named `pre-commit` without the executable bit was skipped with the not-executable hint and the commit succeeded (measured with mode 0644 and 0000); the same directory with the executable bit made `git commit` fail (measurement 5). Classification therefore decides `ignored` by `access(X_OK)` first.
17. Reported by the review and not re-measured by the author: a `hook.<name>.event` without `hook.<name>.command` makes `git commit` fail; a hooks directory with mode 0000 ignores its hooks while mode 0711 runs them (search permission is what matters).

## Process allowlist (normative)

The command starts only these Git invocations, each through the existing runner (isolation prefix, `GIT_*` removed, optional locks, prompts, lazy fetch and pager disabled; `HOME` and `XDG_CONFIG_HOME` kept), with a 5-second bound and the existing 64 KiB stream cap. A runner-recording test pins this list. Contracts and tests refer to it.

- Ownership decision, reused unchanged from `Inspect` for Git-owned and GitButler paths: `version`, `rev-parse --path-format=absolute --absolute-git-dir`, `--git-common-dir`, `--is-bare-repository`, `--show-toplevel`, `--show-object-format`, the GitButler `for-each-ref` probe, `symbolic-ref --quiet HEAD` and the other HEAD reads `Inspect` makes (`readHead`, run twice). The review reported the exact `readHead` argument list; the implementation test records whatever it is, and nothing outside this list may be added.
- jj-owned workspaces: no Inspect Git reads. `git version` and the Git reads below run only when a `.git` entry exists.
- Gate: `rev-parse --path-format=absolute --absolute-git-dir` before and after the configuration reads (it applies Git's ownership check).
- Configuration: `config --show-scope --show-origin -z --get-regexp '^core\.hookspath$'`; `config --type=bool --get extensions.worktreeConfig`; `config -z --show-scope --show-origin --get-regexp '^hook\..*\.(event|enabled)$'`; `config -z --name-only --get-regexp '^hook\..*\.command$'` (key names only, so command text is never read). Exit status 1 means unset only after the gate passed.
- Resolution: `rev-parse --path-format=absolute --git-path hooks` (not run for an empty effective value; failure for a non-empty value is state `invalid`).
- Tracking: `--literal-pathspecs ls-files -z --full-name -- <catalog names under the hooks directory, relative to the worktree root>`, run from the worktree root, only when the directory is inside the worktree.

Not used: `git hook` (any subcommand), `git commit`, `git push`, `git status`, `git config --edit`, `git var`, any command that can run a hook, filter, helper or alias, and any fetch. For jj only the existing sandbox probe and `--version` run (no identity queries).

## Not measured, therefore unverified

- The 28-name hook catalog against the 2.46.0 and 2.56.0 documentation (the only manual on the author's host is a Git 2.47.3 `githooks(5)`); everything about jj: whether `jj commit` or `jj git push` runs any Git hook in a colocated or non-colocated repository, on 0.45.1 or 0.46.0; whether a `reference-transaction` hook fires on export.
- All Git behavior on 2.46.0, including the version that introduced configuration hooks and `config --show-scope` output format differences.
- Behavior on macOS and on filesystems without an executable bit.
- Whether `reference-transaction` aborts an operation when it exits non-zero (documented in `githooks(5)`, not measured).
- Behavior of `git rebase`, `git merge`, `git checkout` and `git worktree add` hooks.
- A raw `git commit` inside a colocated jj workspace (outside SourceSteward's policy; the lane does not exercise it).
