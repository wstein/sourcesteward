# Development and compatibility

[SourceSteward 101](../README.md) · [Command reference](command-reference.md) · [Output for agents](status-and-sync.md#output-for-agents)

See the [command reference](command-reference.md) for public commands
and flags. After changing the CLI, regenerate it from the repository root.

```sh
cargo run --locked --quiet --bin stw -- dev command-reference > docs/command-reference.md
```

Implementation scope, feature decisions, dependencies, and acceptance gates: [Rust migration plan](migration-plan.md).

Planned milestones: [roadmap](../ROADMAP.md). Boundaries of the tool: [what `stw` does not do](what-stw-does-not-do.md).

Everyday git and GitHub workflows for agents and people, in one binary. Written in Rust on
[gitoxide](https://github.com/GitoxideLabs/gitoxide) (`gix`, pinned to `=0.90.0`) and `clap`.
stw starts the installed `git` in one place only, the runner (`src/git_runner.rs`; `tests/it/no_binaries.rs` allows a process
nowhere else): today for the fetch of `sync` with ssh and local-path remotes and for `sync restore` (`git reset --keep`,
`git update-ref`), and for workspace checkpoint, retirement preview, worktree entry, and resume. The recovery refs and the journal of a `sync` run are written through gix, so a run needs no `git` for them
(and the repository's own config cannot block them). `status`, `tidy` and `sync` start no
program for any repository content: gix reads only, filter drivers (`filter.<name>.clean/smudge/process`) are dropped
from the in-memory configuration (a file with a `filter=` attribute is then compared unconverted, so a content-rewriting
driver such as git-lfs can make it look modified, and `sync` refuses, before any write, a fast-forward that
would write such a path), https fetch uses no credential helper, and an empty `PATH` still serves
every gix read. There is no `gh` and no `ssh` of stw's own. Later `push`,
`range-diff` and conflict resolution in a worktree use the same runner. Each step that ran through it carries `"via": "git"`
in the JSON output; without a usable `git` (missing, older than the step needs, unknown option) the step reports
`unsupported` (exit 1, typed). See `chartroom/zettel/stw runs git only for push, fetch over ssh, range-diff and resolve.md`.

jj (Jujutsu) is not supported (owner decision 2026-11-11). Git is the standard; functions that jj offers (undo/oplog,
conflict resolution) are added to stw instead of maintaining a second repository model. jj is an officially unsupported
tool: it may work in a colocated Git repository, without a test and without a promise. stw has no jj special code (no
detection, no warning, no test).

## Command side effects

| Command | What it does | Writes |
| --- | --- | --- |
| `status` | counts per repository and in total, then per repository: branch, upstream ahead/behind, staged/unstaged/untracked/conflicts, stash, running operation, submodule pin drift, open pull requests, the CI of the default branch (`ci-main`, see "CI of the default branch in `stw status`") | nothing; with `--fetch` only the remote-tracking refs of `origin` (and FETCH_HEAD, objects) |
| `tidy plan` | the plan only: per local branch `delete`, `review` or `keep`, with the proof. `tidy` alone prints the usage (exit 2) | nothing |
| `tidy apply` | carries the plan out: deletes the local branches it marks `delete`, removes clean linked worktrees of such branches and dead worktree registrations. No confirmation flag; `--yes` is an unknown flag | local branches, worktrees, a manifest outside the repository |
| `tidy restore` | re-creates the branches of a run from its manifest | local branches |
| `tidy history` | lists the stored manifests (the journals of `tidy apply`), newest first, with the branches each recorded and how many `tidy restore` could create now | nothing |
| `add` | binds an EXISTING GitHub repository as a project (git submodule) of the workspace, see "stw add" | the clone directory, `.gitmodules`, the index, `.git/modules`, one local commit |
| `prove` | new, no Go counterpart: proves locally that a rewritten branch is the old one rebased and nothing else (eight checks, one `Carried-over:` line for `proved`), see "Local rebase proof" | nothing |
| `restack plan` | port of the single-branch prediction of the Go `restack plan` plus a whole-stack preview (new): which level of a stack of branches rebases cleanly onto a new base, where a conflict arises (files) and which level only inherits it, see "Stack preview" | nothing (no ref, no file, no object) |
| `restack prepare`, `apply`, `undo` | replay one direct local branch onto one direct local branch, move it, and move it back. See [restack apply](restack-apply.md) | the plan file, `refs/stw/restack-prepared/<id>`, `refs/stw/restack-applied/<id>`, `refs/stw/backup/<run>`, the branch. Apply runs no hooks |
| `absorb` | new, no Go counterpart: the PLAN of folding the staged changes into the unpushed commits that own the changed lines, see "`stw absorb plan`". `--apply --plan-sum SUM` folds that plan after an op log backup; `stw oplog undo` is the way back | without `--apply`: nothing. With it: fixup objects, `refs/stw/backup/<run>/1`, the branch, the op log |
| `ready-check` | the seven read-only conditions for a pull request | nothing |
| `lint pr` | the commits, the text and the comments of one pull request against the machine-checkable working rules, see "PR lint" | nothing |
| `lint review`, `lint pr-text` | new, no Go counterpart: the text of a review comment or of a PR text from a file against the same rules (and the patch-id of `BASE...HEAD` against a local repository), see "Text files: `lint review`, `lint pr-text`" | nothing |
| `sync restore` | moves the branches a `sync` run changed back to their journaled old tips, only where a branch still stands on the tip the run left (see "`sync restore`") | local branches (and the work tree of a checked-out one), the state of an incomplete journal record |
| `sync` | like Go it acts by default: fetch `origin` (any remote kind gix can serve, see "Remote kinds") and fast-forward the checked-out branch, index and working tree included. `--plan` (alias `--dry-run`, the same flag): like Go it fetches `origin` first (not with `--offline`) and says what would be fast-forwarded and why not | without `--plan`/`--dry-run`; with it only the remote-tracking refs of `origin` (and FETCH_HEAD, objects); `--apply` is a hidden alias without effect (see "Deviations from Go in these flags", 1) |

## Remote kinds and the git runner

`sync` (a run) fetches `origin` by remote kind:

| Remote | Fetched by | `via` | On failure |
| --- | --- | --- | --- |
| `ssh://`, `user@host:path`, local path, `file://` | installed `git fetch` through the runner | `git` | `fetch: failed`; `fetch: unsupported` when git is missing, too old or lacks an option |
| `https://` | gix, no program; with `sync --prune` the installed git through the runner (README "sync --prune") | `gix`, with `--prune` `git` | `fetch: failed` |
| `http://`, `git://` | gix, no program | `gix` | `fetch: unsupported-remote` (message ends with `use git directly for it`) |
| other transports (`ext::`, `fd::`) | nobody | none | `fetch: unsupported-remote` |

gix would start `ssh` or `git-upload-pack` itself for the first row; stw never hands such a URL to gix
(`tests/it/remote_kinds.rs` puts fake `ssh`, `git-upload-pack` and `git` first on `PATH` and checks that only the runner
starts `git`). `via`, and `git_error` (`not-found`, `version`, `unknown-option`, `timeout`, `output-limit`, `exit`,
`spawn`, `repository`, `refused-config`) appear in the repository entry of `sync` and in `agent_rules` only when a fetch ran. Credentials in a remote URL
and GitHub tokens are never printed. `status` without `--fetch`, `tidy plan` and `sync --plan --offline` never fetch; `sync --plan` fetches like a run. `git://` was not exercised
against a server (UNKNOWN beyond the error path).

Runner rules (`src/git_runner.rs`):

- Fixed argument vector per operation (`Op`), never composed from free text: `git -C <absolute repo> -c core.fsmonitor=false
  -c submodule.recurse=false -c fetch.recurseSubmodules=false -c gc.auto=0 -c maintenance.auto=false
  -c protocol.ext.allow=never -c core.hooksPath=/dev/null fetch --quiet --no-tags --no-recurse-submodules
  --upload-pack=git-upload-pack origin`. No shell, no repository hook, no repository-chosen upload-pack program. An empty,
  relative, missing or non-repository path is refused; the process working directory is never used.
- Repository config that runs commands is refused: `core.sshCommand`, `core.gitProxy`, `core.askPass`,
  `core.alternateRefsCommand`, any `credential.*`, `url.*.insteadOf`/`pushInsteadOf`, `remote.origin.receivepack`,
  `remote.origin.vcs`, and any `include`, `includeIf` or `hook` section (git follows include files, so a refused key
  could hide in one) in the local or worktree config give `fetch: unsupported` with `git_error: refused-config` and the
  key name; the same keys in the user's own config are allowed (a repository that sets `credential.helper` locally cannot
  be fetched over ssh by stw; use git directly).
- Environment allowlist: `PATH`, `HOME`, `USER`, `LOGNAME`, `XDG_CONFIG_HOME`, `TMPDIR`, `SSH_AUTH_SOCK`, `SSH_AGENT_PID`,
  `GIT_SSH`, `GIT_SSH_COMMAND`, `GIT_SSH_VARIANT`, `GIT_ASKPASS`, `SSH_ASKPASS`, `SSH_ASKPASS_REQUIRE`, `DISPLAY`, the proxy
  variables and `SSL_CERT_FILE`/`SSL_CERT_DIR`; `PATH` is passed on with its absolute entries only (an empty or relative entry would let git run `ssh` from the worktree). Everything else is dropped: all other `GIT_*` (`GIT_DIR`, `GIT_WORK_TREE`,
  `GIT_INDEX_FILE`, `GIT_CONFIG_*`, `GIT_EXEC_PATH`, `GIT_TRACE*`, author and committer variables), `EDITOR`, `VISUAL`,
  `PAGER`, `LANG*`, `LC_*`, `GH_TOKEN`, `GITHUB_TOKEN`. Fixed: `LC_ALL=C`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`,
  `GIT_PAGER=cat`, `GIT_EDITOR=true`, `GIT_ALLOW_PROTOCOL=file:ssh:git:http:https`, `GIT_CEILING_DIRECTORIES` = parent of
  the repository. The user's Git configuration and identity stay in effect; stw sets no author or committer.
- Time (120 s) and output (1 MiB per stream) are bounded; the process group depends on the terminal. Without a terminal on stdin or stderr (agents, CI) git runs in its own process group, and a timeout or output limit kills the whole group, helpers holding the pipes included. With a terminal git stays in stw's foreground group, as plain git: prompts (ssh host key, passphrase) and Ctrl-C work, and a timeout or output limit kills git only, so a helper it started can outlive it. ssh options are the user's (`BatchMode` is not forced): without a terminal a host-key prompt cannot be answered and blocks until the time limit. `git --version` is read once
  per run; the minimum per command is in "Git versions" below. A spawn that fails with ETXTBSY is retried.
- Errors are typed (`GitError`) and redacted: URL user info, GitHub token shapes, `Authorization:` values and the values of
  `GH_TOKEN`/`GITHUB_TOKEN` never reach output.
- Not done: Windows (executable lookup, path form), push.

## Git versions

Only `src/git_runner.rs` starts `git`; everything else (status, tidy, restack plan, lint pr, review lines, the merge
simulation, patch-id, the fast-forward and the journal of `sync`) runs in gix and needs no installed Git. `restack prepare` is the exception inside that command: the replay and the ref transaction are in process, and the printed range-diff is the one runner call. Every runner call
checks `git --version` against the minimum of its `Op` (`Op::min_version`) before it starts anything; an older Git, or a
version text that cannot be read, is refused (fail closed, `git_error: version`, result `unsupported`) with
`stw needs Git X.Y or newer for <command> (found Z)`.

| Command | Minimum Git | Deciding feature | Evidence (RelNotes) |
| --- | --- | --- | --- |
| `status`, `tidy`, `restack plan`, `lint pr`, `lint pr-text`, `review lines` | none (no Git call) | gix only | `tests/it/no_binaries.rs` |
| `lint review` with `--base` and `--head` | 2.30 | `git diff` and `git patch-id --stable`, the fail-closed value; without those two flags no Git call | `tests/it/lint_files.rs` |
| `status --fetch`, `sync` (default), `sync --prune`, `--no-prune` | 2.30 | `fetch --prune --no-prune-tags` (2.17); 2.30 is the fail-closed value, formally 2.17 | 2.17.0 |
| `sync --submodules`, `sync restore`, `add` | 2.36 | `-c core.fsmonitor=false` is a boolean only since 2.36 (before, the value names a hook command); these ops read the index | 2.36.0 (config/core) |
| `workspace` | 2.46 | `GIT_NO_LAZY_FETCH` prevents local checkpoint and resume from fetching missing objects | 2.46.0 |
| `absorb` | 2.46 | `GIT_NO_LAZY_FETCH` is honored since 2.46; an older git would fetch the missing objects of a partial clone; the one call is `git blame` | 2.46.0 (backends.md of the Go repository) |
| `review carry-over` (with and without `--fetch`), `prove` | 2.41 | `GIT_ATTR_SOURCE` (2.40 ignores it silently, so a `-diff` attribute could hide a change from `range-diff`) | 2.41.0 |
| `restack prepare` | 2.41 | the printed `range-diff` (`--no-color --no-patch --no-notes`); apply and undo call no git | 2.41.0 |

The 2.36 bound is documentary: with Git 2.35.3 no test of `add`, `sync --submodules` or `sync restore` failed (48 failures,
all range-diff or merge-tree tests that need 2.40/2.41; calls seen: check-attr 146, reset --keep 32, reset --hard 19,
submodule add 19, absorbgitdirs 17, commit 17, submodule sync 5, update 4), so no failure was observed there.
The 2.41 bound was shown: with 2.40.1 a worktree `.gitattributes` with `* -diff` made `range-diff` report "equal" for a
changed commit; with 2.41.0 and 2.43.0 it reported "not equal".

Test-only requirements (not needed at run time):

| Where | Requirement | Feature |
| --- | --- | --- |
| `tests/it/replay.rs` (oracle) | 2.40 | `git merge-tree --write-tree --merge-base` (2.38 for `--write-tree`) |
| `tests/it/carryover*.rs`, `prove` tests | 2.41 | real `range-diff` with `GIT_ATTR_SOURCE` |
| test repositories | 2.28 | `git init -b` |
| `tests/oracle/*.sh` | 2.23 | `git switch` |

Distributions (not checked against their packages): Ubuntu 24.04 ships Git 2.43.0 and covers every command. Debian 12 ships
2.39.x and loses `review carry-over` and `prove` (shown by the version check with a 2.39.2 build, not by the Debian package);
`sync --submodules`, `sync restore` and `add` stay available there. The suite ran green with 2.43.0 and 2.41.0, and with
2.40.1 the carry-over and prove tests fail at the version check (they need 2.41) and the rest passes.

## Build and test

```
cargo build --release          # target/release/stw
cargo test                     # unit and fixture tests, no network
cargo test --test it -- --ignored network   # clones a public repo, rewinds it, fetches and fast-forwards
bash tests/mutation/mutate.sh SCRATCH    # 1640 mutations, each must fail the named test
bash tests/oracle/shim_proof.sh stw ROOT SCRATCH   # fake git/gh/ssh first on PATH: 0 calls
bash tests/oracle/tidy_oracle.sh OUTDIR   # oracle of tidy plan against real repositories and GitHub (network; see Oracle of tidy plan)
bash tests/oracle/watch_scenarios.sh GO_SRC OUTDIR   # exports the watch scenarios from the Go sources (see Watch scenarios)
```

Integration tests live in one binary: new tests go to `tests/it/<name>.rs` with a `mod <name>;` line in `tests/it/main.rs` (one link instead of one per file; `slug_cases`, `review_states`, `snapshots_cli` and `snapshots_symlink` stay separate binaries).
`tests/oracle/*.sh` use the `git` binary **only as a reference** in tests (patch ID, fast-forward results).
Test helpers (dev-dependencies only, not in the binary): `proptest` (`tests/it/patchid_props.rs`, small case count),
`insta` (`tests/snapshots_cli.rs`; snapshots in `tests/snapshots/`, review with `cargo insta review` or
`INSTA_UPDATE=always cargo test --test snapshots_cli`), `datatest-stable` (`tests/slug_cases.rs`: one `.url` and
`.slug` file pair per case in `tests/slug_cases/`).
`cargo clippy --all-targets -- -D warnings` and `cargo fmt --check` are clean.

## Reflog identity and order of writes

Reflog entries (fast-forward of the branch and `HEAD`, update of the remote-tracking refs by the fetch) need a
committer. stw resolves it in three steps: `GIT_COMMITTER_NAME`/`GIT_COMMITTER_EMAIL`, then `committer.*` and
`user.name`/`user.email` of the Git configuration (as gix resolves them), and finally the fallback
`stw <stw@localhost>`. The fallback lives in memory only: it is never written to a configuration, and stw creates no
commits, so it is never an author or committer of a commit. `GIT_AUTHOR_*` is not used.
Rule for the environment variables: they count only when `GIT_COMMITTER_NAME` and `GIT_COMMITTER_EMAIL` are both
set and non-empty. Otherwise both are ignored (and the `EMAIL` variable with them) and the Git configuration, then
the fallback, decides; a name from the environment is never combined with an email from elsewhere.

A `sync` run writes in this order: all checks first (dirty paths, overlap, untracked files in the way), then the
index lock (`.git/index.lock`, taken here and held until the index is committed), then the branch ref (the write
that can still fail for reasons no check sees: permissions, locks, reflog), then working tree files, then the
index (written into the lock, which is renamed over the index last). There is no transaction across ref, files
and index:

- A refused check, a held `index.lock` (another git process or a stale lock file) or a failed ref update leaves
  index, working tree and `HEAD` untouched (`tests/it/sync.rs`). The foreign lock file is not removed.
- If writing files fails, the ref is put back to the old commit (reflog entry `stw: undo fast-forward ...`). Files
  the checkout already wrote stay; the index was not touched. `git status` shows those files as changes.
- If writing or committing the index fails, the ref is put back; all files are already new, the index is the old
  one.
- If the ref cannot be put back either, the error names both failures. `HEAD` then stays on the new commit while
  files and index are old or partly new.
- If the process is killed (or the machine loses power) after the ref moved and before the index was written,
  `HEAD` points to the new commit while files and index are old or partly new; stw does not detect or repair this
  on the next run. `git status` shows the update as local changes. A kill at any point after the index lock
  was taken (before the ref moved, during the checkout, during the index write) also leaves a stale
  `.git/index.lock`; no signal handler could prevent that for SIGKILL or a power loss. Remove that lock file
  first (only when no git process runs), otherwise git refuses every command that writes the index. If the
  ref had not moved yet, nothing else changed. To finish the update:
  `git reset --hard HEAD` (tested in a scratch repository; it also discards unrelated local changes). Without
  discarding anything else: `git checkout HEAD -- <paths>` with the paths of
  `git diff --name-only --diff-filter=d <old commit> HEAD`, then `git rm --cached` and remove the files of
  `--diff-filter=D`. A plain `git checkout HEAD -- <path>` fails for a path that the update deleted ("pathspec
  did not match"; tested). To go back: `git reset --hard <old commit>` (same caveat; not tested), where the old
  commit is the second-to-last entry of `git reflog show <branch>`.

## Own code where gix has no API

- Patch ID (`git patch-id --stable`, for squash/rebase-merge detection): `src/patch_id.rs` and `src/patch_rename.rs`; the section
  "Patch ID: the value of `git diff | git patch-id --stable`" below says what it equals and where it deviates.
  Paths are hashed as git writes them (quoted, raw bytes). Git's ID ignores white space in file names and in file
  content (`a b.txt` equals `ab.txt`; `\tcc x` equals `  cc   x`), so the `equal-patches` proof also requires an
  identical digest of the diff lines with their white space (file names included): a change that differs from main
  only in white space keeps the branch.
- Commit similarity (`src/tidy_similarity.rs`, wired into `tidy plan` as the proof `similar-patches` (README "Similar patches in `tidy plan`"); specification and accepted method in `spikes/stw/tidy-similarity/README.md`):
  a commit is the multiset of its changed lines (`<path> NUL <sign> <trimmed line>`, context 0, no rename detection), two commits are compared with
  Dice `2 * |A ∩ B| / (|A| + |B|)`, decided in integers (`200 * I >= pct * (A + B)`; the display rounds half up to one decimal and never shows the
  threshold for a value below it). Candidates on main: same subject, same subject without `type(scope):`, or at least half of the changed files in
  common, at most 7 days older (author time), at most 100 per commit. Classes per branch `landed` (all >= 95), `modified` (all >= 70), `partial`, `none`.
  Hunks come from gix-imara-diff (Myers), not from `git diff -U0`; whether the multisets differ from git's is UNKNOWN until the oracle. Limits: a commit with more
  than 20 000 keys or a blob over 1 MiB is `too_large` (no value, prevents `landed`). Reusable parts: `Keys::sum`, containment as (numerator, denominator),
  `tree_entries` (cumulative diff `base...tip`), `MainIndex::candidates` and `in_window` apart from the comparison. Mutations `TS1` to `TS51` and `TP1` to `TP31` (the plan).
- Rebase-merge proof (`src/tidy_rebase.rs`, wired into `tidy plan` as the proof `rebase-identity` (README "Rebase evidence in `tidy plan`"); specification in `spikes/stw/tidy-similarity/README.md`, section
  package 1b): a rebase merge writes a branch commit again with the same author, author time and subject. `RebaseIndex` walks the first-parent chain
  of main once; a branch commit maps to the chain commit with the same key (author mail in lower case, author seconds, subject) and the exact author
  name, after an equal patch. Several commits with one key are assigned as a multiset by their neighbours (`ambiguous` without one). `contiguous`,
  `in_order`, `gap`, `runs`: the targets form one block in branch order. Strong (class `rebase-identity`, decision `review`, never `delete`): all
  commits mapped, one ordered block, content at least 95 % per commit (at least 70 % and below 95 % is `modified`), no later version (amend after landing), at least one identity.
  The index stops by a run of 64 old author times, never by the committer time; the boundary commit of a shallow clone is skipped. The pull request (`evaluate_api`, on given inputs, no request) and
  `landed_via` (merge, rebase, squash, unknown with basis and strength). Mutations `RB1` to `RB68`; the plan wiring (rebase stage, pull request reads, `landed_via`, flag): `TR1` to `TR52`.
- Squash evidence, layer 1 (`src/tidy_squash.rs`, wired into `tidy plan` as `squash` per branch (README "Squash evidence in `tidy plan`"); specification `scratch/stw-tidy-squash/spec.md`, package 5a):
  the merged pull request (merge commit on the base with one parent, the head in the branch, commits after it) and the subject on the base (`(#N)`, title equality, list in the body, `Pull-request`
  trailer); no diff, no request, no class, no decision. Mutations `SQ11` to `SQ56` (5a part).
- `tidy plan --explain`: `src/tidy_explain.rs` (the text) and the flag in `src/cli.rs`; tests `tests/it/tidy_explain.rs`; mutations `TX1` to `TX38`.
- The oracle of `tidy plan`: `src/tidy_oracle.rs` (pure comparisons), `tests/it/tidy_oracle.rs`, the ignored tests `tests/it/oracle_tidy.rs`, the driver `tests/oracle/tidy_oracle.sh`; mutations `OR1` to `OR35`.
- Fast-forward with checkout: `src/sync/fast_forward.rs` (checks first, writes after; index tree cache removed by hand, gix issue 2421).
- Untracked collapsing for nested repositories: `src/status.rs`.
- Worktree registration HEAD (gix `Proxy` has none): `src/repo/worktrees.rs`.

gix has no API stability promise before 1.0: the pin is exact and updates are deliberate.


## Patch ID: the value of `git diff | git patch-id --stable`

The patch ID that `stw prove`, `stw review carry-over`, `tidy`, `restack` and `stw dev patch-id` print and compare is the first field
of `git patch-id --stable` on the diff of a commit against its first parent as `git diff` and `git show` print it with their built-in
defaults, except the indent heuristic of the hunks (deviation 3): renames on (50 %, `diff.renameLimit` 1000), copies off, no break detection, `core.quotePath` on. Before 2026-11-19 a rename was
a deletion plus an addition (`git diff-tree --no-renames`), a file diff without hunks was a unit of its own and a binary file
hashed full blob ids; that was not git's value, and `stw lint review` rejected a `Carried-over:` line that `stw prove` had printed for a commit
with a rename (debt ledger 2026-11-17, "prove/carry-over gix patch-id rename mismatch"). Measured then: 14 of 14 commits with a rename in
three real histories and 16 of 21 cases of the fixture oracle differed from git; now 14 of 14 and 21 of 21 are equal, and the 66 pinned fixtures of
`tests/it/patchid_git.rs` (48 rename and tie cases, 18 units cases) (units, binary files, type changes, gitlinks included) are equal. Sources: git `builtin/patch-id.c` (`get_one_patchid`), `diffcore-rename.c`, `diffcore-delta.c`, `diff.c`;
the behavior is checked against the installed git, not copied from the source text.

- Renames (`src/patch_rename.rs`): exact renames first (same blob id, the same base name preferred), then the base-name pass (a base name
  that occurs once among the remaining sources and once among the remaining destinations is scored first; such a pair needs 75 %, git's
  `min_basename_score` = 30000 + 0.5 * 30000, a pair below it is left to the matrix), then the similarity matrix
  (byte-span estimate of `diffcore-delta.c`: lines of at most 64 bytes hashed, a CR before an LF ignored, score `copied * 60000 / larger size`,
  threshold 30000 inclusive, a source is used once; the 4 candidate slots per destination are filled as git's `record_if_better` fills them,
  so equal candidates keep their slot order and the order of ties is git's, not the qsort of the C library; sources used by an exact or base-name
  rename are not candidates). A rename prints `similarity index N%`, `rename from`,
  `rename to` (the percentage is hashed, so it has to be git's integer), then the `---`/`+++` and hunks of the old against the new file; an exact
  rename has no hunks; the exact pass looks at no more than 100 identical candidates per destination, as git does. Skipped above `1000 * 1000` source/destination pairs (git warns on stderr and prints no renames). Symlinks and gitlinks
  are renames only when exact and only with equal modes (a symlink is never paired with a regular file of the same blob, a gitlink only with a
  gitlink; a regular file may change 100644 to 100755). A change of the file type (file, symlink, gitlink) is a deletion plus an addition, as git prints it.
- Units (`--stable`): a file diff without hunks (mode change, empty file, exact rename) is hashed together with the next file diff, a binary
  file adds the abbreviated blob ids of its `index` line and ends the unit, the `diff` line after it is not hashed, the last unit is added even
  when it is empty. The abbreviation length is the one gix gives the repository (`core.abbrev`, else from the object count, at least 7).
- The second digest of `PatchKey` (used with the patch ID by the `equal-patches` proof) follows the same units, keeps white space, hashes full
  blob ids for binary files and also the lines git skips.

Deviations from git, one by one:

1. The user's configuration is not read: `diff.renames=false` or `copies`, `diff.renameLimit`, `-M<n>`, `-C`, `-B` give another output in git.
   The result must not depend on a configuration (a review line carries it); `stw lint review --base/--head` still asks git itself.
2. `.gitattributes` (`binary`, `-diff`, a diff driver, a text conversion) is not read.
3. Hunk placement: git's default `diff.indentHeuristic` (on since 2.14) moves a hunk to the position that reads best; gix-imara-diff runs here without that
   heuristic, so a hunk that can slide differs. Measured against `git diff` (review of 2026-11-19): 25 of 300 commits of the sourcesteward path and 11 of 300
   commits of this repository differ, and all 25 of the first set equal `git diff --no-indent-heuristic`. It is a cause of different patch IDs for equal
   patches (a missed `equal-patches` proof), not of equal IDs for different patches.
4. The abbreviation length of a binary file's `index` line: gix computes the automatic length from its own object count, git from a sample of loose
   objects; near a power of 2 the lengths can differ.
5. Not covered by a mutation: a NUL byte after the first 8000 bytes of a text file
   (git stops hashing the line there; this code does as well, without a test).

The values are git's from version 2.31 on and were checked with git 2.47.3 only (UNKNOWN: 2.31 to 2.46). The base-name pass (`find_basename_matches`,
`min_basename_score`) is in `diffcore-rename.c` at tag v2.31.0 and not at v2.30.0 (commit bd24aa2f97, "diffcore-rename: guide inexact rename detection
based on basenames", an ancestor of v2.31.0); with git 2.30, which `stw lint review` accepts as its minimum, git itself gives other values for commits where
the base-name pass decides, while `stw lint review` still compares with the value of the installed git. Git also reads `GIT_BASENAME_FACTOR` (a test hook,
not read here).

Oracles (read-only on real repositories, throwaway repositories under `/tmp/stw-prove-patchid`): `tests/oracle/patch_id_rename_oracle.sh fixtures|repo`
(commits with renames), `tests/oracle/patch_id_oracle.sh` (sample of commits), the ignored tests `patchid_git::pinned::oracle_against_git` and
`patchid_git::units::oracle_against_git` (run git on the fixture repositories and compare); the ordinary tests pin the git values. Mutations `PI1` to `PI32`.

## Not verified

`stw add`: the gix clone over https was not run in the test suite (the tests replace the clone by a local copy; the
checks, the git steps and the rollback run for real); a run against a live GitHub repository is a manual check. Git
older than 2.47.3, macOS, a `url.<x>.insteadOf` rewrite (the clone ignores the user's config), concurrent runs on one
`PATH`, and a race between the checks and the removal of a clone directory are not covered. The clone's attribute
scan refuses a little too much (a line naming `filter` anywhere, also in an unrelated pattern); `git check-attr` is
trusted for the workspace (its `text`/`eol`/`working-tree-encoding` effects on `.gitmodules` are not asked). `GIT_ATTR_NOSYSTEM=1` keeps `/etc/gitattributes` out of every git step of `stw add`; that this works against a real system attributes file was not checked live (no root). The
`.git/modules` check before the absorb narrows the race window but does not close
it (no `dirfd`/`O_NOFOLLOW`).

Automated fetch of a private repository over https (the credential answer is unit-tested; one manual run against a private repository worked); `git://` behavior of gix; conflicted
index during a `sync` run; sparse checkout, `skip-worktree`, case-insensitive path collisions; a real git-lfs
during fast-forward (a `filter` attribute on a path the update writes and a `filter.*` driver in the config are refused, tested); Windows, and Linux beyond the test suite (the original measurements ran on macOS arm64; the M0 review and tests ran on Linux aarch64; keychain code is macOS-only); cold-cache timings.


## Documentation site

GitHub Actions builds the [public documentation site](https://wstein.github.io/sourcesteward/)
with Hugo. The build publishes selected guides from this repository,
including the generated command reference. Its line coverage badge uses
the LLVM report from the instrumented Rust tests.

With Hugo installed, preview the same documentation locally.

```sh
python3 scripts/build-pages.py /tmp/stw-preview/sourcesteward
python3 -m http.server --bind 127.0.0.1 --directory /tmp/stw-preview
```

Open <http://127.0.0.1:8000/sourcesteward/>.
