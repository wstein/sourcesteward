# Worktrees and handoff

[SourceSteward 101](../README.md) · [Command reference](command-reference.md) · [Carry-over check](review-and-watch.md#carry-over-check-stw-review-carry-over-m4)

## Workspace checkpoints

Use `stw workspace worktree-plan REV DESTINATION --json` to resolve a local
revision to a commit, without creating a worktree. Save the result's `plan`
object outside the source worktree, then run
`stw workspace worktree-apply PLAN --json` in the same repository. Apply checks
that the plan identifies the Git common directory, that the pinned SHA-1
commit is available locally, and that the canonical destination is still
vacant. The destination's parent must already exist.

Apply checks out the saved commit in a native linked worktree with detached
HEAD. The plan pins that commit; apply never follows a live branch tip and
passes only the pinned SHA to `git worktree add`. If the pinned commit is
missing locally, apply refuses. Apply leaves the source checkout, its index,
and existing refs in place. If Git reports a checkout error, inspect the
reported destination and `git worktree list` for partial state before
retrying.

The plan also records the revision as typed (`revision`). Plans saved before
this field existed have no `revision` and apply them as before. Older stw
versions reject a plan that has it. At apply time stw resolves the revision
again and compares it with the pin:

- Same commit: the JSON report has `"tip_drift": false`.
- A different commit, or the revision no longer resolves: apply still creates
  the worktree at the pinned commit and exits 0. The report has
  `"tip_drift": true`, `revision`, `pinned_base` and, when the revision still
  resolves, `revision_tip`, plus a `notices` array with one sentence naming
  both commits. Text output prints the same notice and two ways to go on:
  run `worktree-plan` for the revision again to get a plan for its current
  commit, or continue at the pinned commit.
- Plan without `revision`: no check; `tip_drift` and `notices` are absent.

All of these fields are additive; `base`, `destination` and `created` keep
their meaning.

Use `stw workspace checkpoint --task NAME --plan --json` to inspect the selection,
then omit `--plan` to save it. Use `--include PATH` for each explicitly selected
non-ignored untracked file. Paths are relative to the worktree root. The
checkpoint records the source base, staged index contents, and working-tree
contents separately. Git publishes it under `refs/stw/tasks/NAME` only if the
ref still matches the value observed at the start. Earlier checkpoints remain
reachable through the task history. Source branch, index, and files are left as
they were. If the command stops around publication, `workspace inspect` reports
the checkpoint currently selected by the task ref.

`workspace retirement-plan --task NAME --json` compares the current HEAD,
index, and worktree with the task's published checkpoint. It reports staged
differences, worktree differences (including uncaptured untracked files), ignored
files, local-only commits, and active Git operations. On Unix it also reports
executable-bit changes even if the repository disables Git's file-mode checks.
Local-only commits are counted against all local remote-tracking refs, without
fetching. If none exist, every commit reachable from HEAD is local-only.
The preview returns successfully with `ready: false` if any of these checks finds
a blocker or if HEAD moved from the checkpoint base. It writes only a temporary
index. Keep the worktree idle during inspection and rerun the preview if its
state changes.

`workspace resume CHECKPOINT DESTINATION` creates a fresh detached linked worktree
at the recorded base. It restores the index and worktree separately, so selected
untracked files remain untracked. Choose a fresh destination with an existing
parent. A failed or interrupted restore leaves a sibling
`DESTINATION.stw-handoff-incomplete` marker. Use a fresh destination to retry.

This initial implementation uses the local Git runner (Git 2.46 or newer and a
configured Git identity for checkpoint commits). It supports SHA-1 repositories
with full worktrees and a regular index. Sparse checkouts, submodules, Git
operations in progress during capture or resume, worktree attributes, and
special index flags are refused.
Keep the source idle during capture. HEAD or index changes detected before
publication cause a refusal. Checkpoints survive loss of a worktree while their
Git objects remain available. Recovery after VM loss needs independent storage.

## Local rebase proof: `stw prove` (new)

New behavior without a Go counterpart (AGENTS.md "Scripts first, then product": the owner accepts it live; recipe in the
scratch directory of the workspace). `stw prove ALT NEU [--base BASIS] [--old-parent REV] [--pr N|none] [--json] [--root PATH] [--repo NAME]`
(read-only, local, no pull request and no network). `ALT` is the old branch or head, `NEU` the new one: one revision each (a
branch name, a tag, a commit id; not a range). It proves that `NEU` is `ALT` moved by a rebase and nothing else. The first line
of the output is the verdict, `proved` or `not proved`; then one line per check with its state (`ok`, `failed`, `unknown`),
its reason and `via`; for `proved` the last line is the finished line
`Carried-over: from=<ALT head> to=<NEU head> patch-id=<40 hex> pr=<n|none>` (`--pr N`; default `none`).

| # | Check | Reason when not `ok` | via |
| --- | --- | --- | --- |
| 1 | `range-diff`: `git range-diff OLD_BASE..ALT NEW_BASE..NEU` shows `=` for every commit at the same position | `range-diff-not-equal`, `range-empty` | `git` |
| 2 | `commit-patches`: the patch-id of every commit is equal, old against new at the same position (`range-diff` shows only "Binary files differ" for a binary change, so it cannot tell two binary contents apart); asked only when checks 1, 3 and 4 held, else `not-run` | `commit-patch-differs` | `gix` |
| 3 | `patch-id`: equal patch-id of `BASE...HEAD`, old and new | `patch-id-differs`, `patch-id-empty` | `gix` |
| 4 | `merge-commits`: no merge commit in `NEW_BASE..NEU` | `merge-commits` | `gix` |
| 5 | `files`: the same set of changed paths (name-only, without rename detection, both sides the same way) | `files-differ` | `gix` |
| 6 | `identity`: author and committer of every new commit are `user.name <user.email>` of the Git configuration (read only; the email is compared without regard to case) | `identity-mismatch`, `identity-unknown` | `gix` |
| 7 | `trailers`: every `Co-Authored-By` line of an old commit is in the new commit at the same position | `trailer-missing` | `gix` |
| 8 | `session-links`: no session link in the text of a new commit (the rule of `lint pr`) | `session-link` | `gix` |

`--old-parent REV` (same name as in `restack plan` and the Go `restack plan`) is the old parent of `ALT`, the old base: for a child branch whose parent was rebased (`git rebase --onto NEWPARENT OLDPARENT ALT`: `--old-parent OLDPARENT --base NEWPARENT`). `REV` must be an ancestor of `ALT` (`old-parent-not-ancestor`) and not `ALT` itself (`old-range-empty`); the old range is then `REV..ALT` instead of `merge-base..ALT`. Without the option nothing changes; a failed range-diff then hints at both options.

Checks 1 to 4 are the machine of `stw review carry-over` (`carryover::check`: range-diff, per-commit patch-ids, cumulative patch-id, merge commits), not a second implementation. The old base is
the merge base of `ALT` and `NEU`. Without `--base` the new base is the same commit (a rewrite on the same base: amend, reword,
reorder, squash); with `--base BASIS` it is `BASIS`, the base `NEU` was rebased onto, which must be an ancestor of `NEU`
(`base-not-below-new`, with a hint to name the new parent with `--base` when `--old-parent` made the old base the new base too; `ends.base_source` is then `old-parent`). An empty old or new range is `range-empty` (git range-diff needs two non-empty ranges). Without `--base` for a rebase onto a new base the new range holds the commits of the new base, the range-diff
fails and a hint names `--base`. Problems before any check: `unknown-revision` (a revision that names nothing or no commit),
`old-parent-not-ancestor`, `old-range-empty`, `range-empty`, `no-merge-base`, `same-commit` (`ALT` and `NEU` are one commit: nothing to prove, a deviation from the Go carry-over, which
accepts identical heads), `base-not-below-new`; the verdict is `not proved` and the checks do not run. Merge commits of the old range are left out of the pairing (range-diff and the per-commit patch-ids leave them out too); a linearized `NEU` can be proved, and the trailer line says how many were left out. A step that could not run is `unknown`
and the verdict is `not proved`; there is never a line then. `identity-unknown`: no `user.name` and `user.email` in the Git
configuration; stw never sets them. The identity check is as strict as `lint pr`: a rebased commit of another author is not proved.
Git boundary: only `range-diff` runs through the installed git (`Op::RangeDiff`, Git 2.41+, `via: git`; gix has no range-diff);
revisions, merge base, commit walk, trees, patch-id (`src/patch_id.rs`), identity and trailers are read in process. Commit
subjects, names and paths from the repository are untrusted: text and JSON carry them escaped (`escape_untrusted`).
The repository is the one of the current directory, which must belong to the workspace (`--root`, else the repository or
superproject of the current directory, else `$STW_ROOT`; the root and its checked-out submodules that `list_repos` accepts, never a
path that `.gitmodules` names outside the root), or `--repo NAME`; without a repository around the current directory the workspace must
have exactly one. Exit 0 `proved`, 1 `not proved` (result printed), 2 usage.
JSON (schema 2): `schema_version`, `command: "prove"`, `verdict`, `proved`, `pr` (number or `"none"`), `request{old,new,base,old_parent}`,
`ends{old_head,new_head,old_base,new_base,base_source,old_base_source}` (`old_base_source`: `merge-base` or `flag`; `base_source`: `merge-base`, `flag` or `old-parent`; null when the revisions did not
resolve), `input[]` and `checks[]` (each `{id,state,via,reasons[],detail,items[]}`), `reasons[]` (all reasons that apply),
`hint` (the `--base` hint or null) and `line` (the `Carried-over:` line for `proved`, else null).

## `stw absorb plan`

**Purpose.** After review feedback or a late fix, the staged hunks belong back into the commits that introduced the lines.
`stw absorb` computes the PLAN of that: for every staged hunk the commit that owns it (or why none does), the `fixup!`
commits that would fold the hunks in (one per target commit, hunks named) and how many commits would get new SHAs. It folds
nothing: no add, commit, rebase, ref, file, object or index refresh, no network, no hook, no editor. `--apply --plan-sum SUM`
folds that plan: the checksum is recomputed and a mismatch writes nothing (exit 1, `plan-sum-mismatch`). A match rewrites the commits in memory. Before the branch ref moves, the new tip tree must equal the
old HEAD tree plus exactly the absorbed hunks, and the index must already be the index of that tree
(absorbed paths no longer staged, every other staged path still staged). Apply does not write the
work tree, so an unstaged edit stays byte for byte. Then HEAD is copied to `refs/stw/backup/<run>/1`
and that update is appended to the op log, and the branch ref moves. The output names
`stw oplog undo <ref> --to <old> --expect <new> [--keep] --yes` (`--keep` only when the work tree
matched the index). `--apply` without `--plan-sum` is
`plan-sum-required` (exit 1), not an unknown flag. New behavior without a Go counterpart; the specification, the 64 counter-proofs
of the Python spike and the comparison with git-absorb are in `spikes/stw/absorb/README.md` (continuation of Spec 018,
"Fix up"); decisions D-20261119-07 to D-20261119-11 and D-20261119-12 to D-20261119-20 (`dayboard/DECISIONS.md`): git-absorb is parked (-12); the later apply is native in Rust, fixups written with gix, rewrite by `git rebase --autosquash` with `GIT_SEQUENCE_EDITOR=true`, an op log snapshot first, git-absorb is never called, the BSD-3 notice stays (-13); no flag is the read-only plan, writing only with `--apply` and the plan checksum (Q1, -14); nothing staged is exit 1 `nothing-staged` (Q2, -15); an ambiguous hunk is skipped, `--ambiguous-newest` takes the newest (Q4, -16); one fixup per target commit (Q8, -17); a partial clone with missing objects is `partial-clone-unsupported`, no lazy fetch (Q11, -18); Q3, Q5, Q6, Q7, Q9, Q10, Q12 and Q13 follow the recommendations of the spike README (-19); order: live acceptance of the spike first, this read-only port in parallel, the apply only after the owner accepted the spike and the answers (-20).

```
stw absorb [--plan] [--apply --plan-sum SUM] [--base REV] [--max-stack N] [--ambiguous-newest] [--human | --json] [--root PATH] [--repo NAME]
```

| Flag | Meaning |
| --- | --- |
| (none), `--plan`, `--human` | the plan as text, one line per fact (the default); `--plan` is accepted next to `--apply` |
| `--apply --plan-sum SUM` | fold the plan whose `plan_sum` is SUM. A mismatch writes nothing. The output names the backup ref and `stw oplog undo` |
| `--json` | the plan as one JSON document (not with `--human`) |
| `--base REV` | the stack is `REV..HEAD`; REV must be an ancestor of HEAD; the stack limit does not apply |
| `--max-stack N` | at most N commits in the stack, default 10 (the default of git-absorb; its `absorb.maxStack` is not read) |
| `--ambiguous-newest` | a hunk whose lines come from several commits goes to the newest of them (the behavior of git-absorb) instead of being skipped as `ambiguous` |
| `--root PATH`, `--repo NAME` | as for `prove` and `restack plan`: the repository is the one of the current directory unless `--repo` names another of the workspace |

There is no `--offline`: nothing leaves the machine. Git only; `jj` and GitButler checkouts are refused.

**The method** is that of git-absorb 0.9.0. The stack is the first-parent chain of HEAD that no remote-tracking branch
contains (and, without `--base`, no other local branch), cut at the first merge commit, the first commit whose author is not the
`user.name`/`user.email` of the Git configuration, or the limit. Staged changes only (the index against HEAD; the work tree is
never read) and only modified text files: added, deleted, renamed (a delete plus an add, rename detection is off), type-changed,
submodule, symbolic link, binary and mode-only changes are listed under `files_skipped`. Each hunk (a zero-context Myers diff
without the indent heuristic) is moved before the changes of the stack commits, newest first; the target is the newest commit
whose changes do not commute with it (`basis`: `overlaps-lines`, or `file-added` when that commit created the file). The removed
lines are blamed in HEAD (`origins`: evidence, not the decision). A hunk that cannot be placed is `skipped` with a `reason`:
`no-candidate` (commutes with every stack commit and its removed lines, if any, come from the stack; pure insertions land
here), `ambiguous` (the removed lines come from two or more commits; the target that would be chosen stays named),
`conflict` (the removed lines are not where the commuted hunk says in the target's blob), `outside-range` (the lines come from a commit beyond
the stack), `published` (from a commit a remote-tracking branch contains), `foreign` (from a commit by another author;
priority published, foreign, outside-range).

**Output** (`schema_version` 1; keys are added, never renamed). Keys in this order, all always present: `schema_version`,
`command` (`absorb`), `mode` (`plan`), `head`, `branch`, `index_digest` (SHA-256 of `git ls-files -s -z`), `base` (as given or
null), `max_stack`, `stack` (`sha`, `subject`, newest first), `stack_end` (`root`, `merge`, `foreign`, `limit`, `base`,
`published`, `branch`), `hunks` (`id` `h1`.., `path`, `old` and `new` `{start, count}` as in `@@ -a,b +c,d @@`, `assignment`
`absorbed`|`skipped`, `target` `{sha, subject}`|null, `basis`, `reason`, `origins` `[{sha, subject, lines}]`), `files_skipped`
(`path`, `reason`: `added`, `deleted`, `typechange`, `submodule`, `symlink`, `binary`, `mode-only`, `other`), `fixups` (`target`,
`subject` `fixup! <subject>` or `fixup! <full SHA>` when the subject is not unique in the stack, `hunks`), `rewrite` (`count`,
`commits`: the old SHAs of the target commits and all their descendants, oldest first; `ref`; `new_shas`: null, the successors
are unknown before the fold), `summary`, `effects` (`writes: []`, `index_refresh`, `network`, `hooks`, `prompts`: all empty or
false), `plan_sum`. `plan_sum` is the SHA-256 over the whole document (ref, index digest, hunks with assignment and reason,
fixups, rewrite), the options and the paths unmasked, as sorted-key compact JSON with ASCII escapes (the encoding of the spike,
`absorb::canonical_json`); a fold would recompute the plan and refuse on another checksum. The unmasked paths add nothing a
changed path does not already change in the document (the index digest covers them); they are in the sum as specified. Paths,
subjects and the branch name are masked in text and JSON: control, bidi and zero-width characters, line and paragraph
separators and every byte of an invalid UTF-8 sequence become `?`. The text lists the same facts (`absorb plan (read-only): head
.. on .. , stack n commit(s), ends at ..`, `h1 path -3,1 +3,2 -> sha7 subject (basis)` or `.. skipped: reason`, `file path
skipped: reason`, `fixup! subject <- h1, h3`, `rewrite: n commit(s) get new SHAs (oldest sha7); nothing was written`) and names no
command. The same state gives the same bytes (no time, no random, `LC_ALL=C` for blame).

**Exit codes and error codes.** 0: a plan was produced, also when every hunk is skipped. 1: a refusal with a typed code (on stderr
as `stw absorb: <code>: <message>`; with `--json` also the document `{"schema_version":1,"command":"absorb","error":{"code","message"}}`
on stdout). 2: usage error (unknown flag, `--plan-sum` without `--apply`, `--max-stack 0`, no repository). `--apply` without `--plan-sum` is exit 1 `plan-sum-required`.

| Code | Meaning |
| --- | --- |
| `nothing-staged` | the index equals HEAD (git-absorb exits 0 here) |
| `unmerged-index` | the index has unmerged paths |
| `not-owned` | HEAD itself is by another author |
| `published` | HEAD itself is contained in a remote-tracking branch |
| `no-commits`, `detached-head`, `no-identity`, `bad-base` | no commit yet; HEAD on no branch; `user.name` or `user.email` unset; `--base` is no commit or no ancestor of HEAD |
| `environment-override` | `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_COMMON_DIR` or `GIT_OBJECT_DIRECTORY` is set and not empty |
| `unsupported-backend`, `ambiguous-backend`, `invalid-metadata` | a `.jj` directory (in the working directory or above it up to the repository root) or GitButler metadata (`.git/gitbutler`, `refs/gitbutler/*`, `refs/heads/gitbutler/workspace`); both; a `.jj` that is no directory. Detected first; no fallback to Git writes |
| `partial-clone-unsupported` | the repository is a partial clone (`remote.*.promisor` or `extensions.partialclone`) and a read failed: objects are missing; nothing is fetched, ever |
| `blame-failed` | `git blame` of the removed lines failed (no git, too old, an error) or the path is not UTF-8 text: no plan without that evidence, never an empty `origins` |
| `git-failed` | any other unreadable object, ref or index |

**Git boundary and safety.** Everything is read through gix; the one `git` call is `git blame --porcelain --no-textconv
--ignore-revs-file= -L a,b <sha> -- <path>` (`Op::BlameLines`, `--literal-pathspecs`, `GIT_NO_LAZY_FETCH=1`, `GIT_OPTIONAL_LOCKS=0`,
no hook, no pager, floor git 2.46), once per hunk with removed lines. gix itself runs `git config -lz --show-origin ...` once to
find the system configuration of the installation, and the runner `git --version`; both read-only. A refusal at backend
detection runs no blame. External diff, textconv, hooks, the repository's diff settings and the work tree do not reach the plan
(tests). The repository tree and the index file are byte-equal before and after, also with a stale stat cache.

**Limits, named.** Only the first-parent chain; a hunk is checked against the stack commits' blobs, not replayed (the fold is
not built). The attributes `-diff`, `binary` and textconv are not consulted (a file git would show as "Binary files differ"
because of an attribute is planned as text; NUL in the first 8000 bytes still means binary, as in git). Commit messages are
read as stored (`i18n.commitEncoding` is not applied, no mailmap), the subject is the first paragraph on one line (`%s`). A
remote-tracking ref that is no commit is ignored (`git log --remotes` would fail). A repository with `skip-worktree`, sparse or
intent-to-add entries, linked work trees and bare repositories are not specially treated. A path that is not UTF-8 text has no
blame: a hunk that removes lines there is `blame-failed`; pure insertions there are planned. The set of commits a remote-tracking
ref contains is computed in full for every plan (cost grows with the history).

**Deviations**, one by one. From git-absorb 0.9.0 (inherited from the spike): (1) a hunk whose removed lines come from several
commits is skipped as `ambiguous`, `--ambiguous-newest` restores git-absorb (the worst silent error is half a hunk in one commit);
(2) a modified symlink is skipped, git-absorb treats it as text; (3) nothing staged is exit 1 `nothing-staged`, git-absorb exits 0;
(4) commits a remote-tracking branch contains are never targets (`published`), git-absorb does not read them; (5) `absorb.maxStack`
and `--no-limit` are not read; (6) per hunk reasons instead of one warning; (7) one fixup per target commit in the plan, git-absorb
one per hunk unless `oneFixupPerCommit`; (8) the diff is gix's Myers without indent heuristic, git-absorb uses libgit2: no
disagreement in 237 + 28 + 1 comparisons, unproven in general. From the Python spike: (9) reads run in process; `git` is only
`blame` (the spike ran plumbing: `rev-parse`, `log`, `diff-index`, `diff-tree`, `cat-file`, ...); (10) `--repo` is a workspace
repository name and `--root` is the workspace, as in the other stw commands (the spike took a path); (11) JSON is one compact line
(the spike printed `", "` and `": "` separators; the documents are equal after parsing, key order and `plan_sum` included);
(12) `--human` is the text; (13) the search for `.jj` starts in the current directory (the spike: the `--repo` path); (14) a branch
is excluded from the other local branches by its exact name (the spike passed it to `git log --exclude=` as a pattern; ref names
cannot hold glob characters, so no difference is observable); (15) `base` in the document is masked like the other text (the
checksum has it unmasked); (16) a path that is not UTF-8 text has no blame, see above (the spike handled it); (17) attributes
(`-diff`) are not consulted, see above (the spike let git decide); (18) the output of one `git blame` is bound by the runner (1 MiB per stream): a hunk that removes about 15,000 lines or more gets `blame-failed` (the spike has no bound and plans it); (19) a file whose only staged change is the mode and that is binary is `binary` here, the spike says `mode-only`; (20) a sparse index is not handled (see the limits): the spike did not handle it either, no deviation to the spike, named because the result is wrong, not refused.

**License.** The commutation test (`src/absorb_commute.rs`) and the stack and target rules (`src/absorb.rs`) are a translation of
git-absorb 0.9.0 (BSD 3-Clause, Copyright (c) 2018, Stephen Jung) through the accepted spike. Both files carry
`SPDX-License-Identifier: EUPL-1.2 AND BSD-3-Clause`; the license text is `LICENSE-git-absorb-BSD-3` in this directory and must stay
with every redistribution, in source and in binary form (documentation or other materials). The test files `tests/it/absorb_*.rs`
carry the same line where they take cases from the spike.

**Cost.** Worst case of the spike README (a synthetic history of 2450 commits, a 3000-line file whose lines 0 to 999 every commit
changed, a staged insertion at line 2000 with no candidate, `--base` the root: all 2449 commit diffs are read): the spike 10.0 to
10.4 s (one `git diff-tree` per commit); this port, debug build (the one `cargo test` runs), `--base <root>`: 6.24 to 6.30 s,
27 MB peak (3 runs, this host, aarch64); the plan document is equal to the spike's (checksum included). Release build (`opt-level 3`, `lto` switched off for the measurement, as the profile of `Cargo.toml` says thin): 0.49 to 0.62 s, 16 MB
(3 runs, same host and history; the git-absorb figure of the spike README is 0.64 to 0.73 s). A stack of 10 (the default) reads ten commit diffs. The work is the Myers diff of each commit's version of the file; the
set of commits of the remote-tracking branches is the other cost, in a repository of 1462 commits it does not show.

**Tests and counter-proofs.** `tests/it/absorb_*.rs` (115 tests: units, the 91 cases of the spike as CLI tests on throwaway
repositories, the read-only proofs), `tests/it/sha256.rs` (4), `tests/it/git_runner_blame.rs` (5); each test commit was red against the stub
before the implementation followed. The oracle `tests/it/absorb_oracle.rs` compares the plan with the output of the spike on 27
named scenarios and 60 pseudo-random repositories, in the default mode, with `--ambiguous-newest` and with `--max-stack 3`
(237 documents equal, key order and `plan_sum` included; skipped, with a message, when `python3` or the spike is missing);
a second comparison on 14 commits of a clone of this workspace (28 staged changes, the Rust plan against the spike): 28 equal.
git-absorb itself is not installed on this host; the comparison with it is the one in `spikes/stw/absorb/README.md` (162 of 163).
Mutations `AB1` to `AB99` in `tests/mutation/mutate.sh`: 99 of 99 killed at their named test (the first run left five alive: `AB34` was an equivalent mutant and is now the limit that hides the commit proving the limit was hit, `AB64` was equivalent because the git directory is the common directory in an ordinary repository and now removes both lookups, `AB70` and `AB71` named a test that does not reach the added side of a replacement, `AB80` named the oracle test, which cannot run in the mutation copy because the spike is not there, so the subject has its own test). The oracle test is therefore not part of any kill.

**UNKNOWN (not checked).** git older than 2.47.3 (2.46 is a claim from the release notes); macOS; the `extensions.partialclone`
key alone (no test; the promisor key is tested); a type change of a file inside a stack commit; the `conflict` reason from a real
history (it needs a hunk the commutation test places and the blob check refuses: not constructed, tested as a verdict
and as `lines_match` only; a mutation that always says consistent is not killed); an installed jj or GitButler (markers only);
linked work trees, bare repositories; a sparse index (observed: `files_skipped` then names directory entries as `added` and `deleted`, e.g. `b/`, and `index_digest` differs from the SHA-256 of `git ls-files -s -z`; a typed refusal `sparse-index-unsupported` is a behavior fix, not done, in the ledger); `GIT_DIR` and the other overrides in a caller's environment are
refused, not served; a very large diff is read in full by gix (the output of `blame` is bound, deviation 18); the comparison with
git-absorb versions other than 0.9.0.

**Observation.** After the watch port landed there are two SHA-256 implementations in the crate: `src/sha256.rs` (the checksum of this plan) and `watch::statedir::sha256`. They are not merged here (ledger).

## Stack preview: `stw restack plan` (M1/M2: port plus new)

`stw restack plan [BRANCH...] [--onto BASIS] [--old-parent REV] [--json] [--root PATH] [--repo NAME]` (read-only: no ref, no file,
no object on disk, no network, no `git` process; a test compares the whole directory including `.git` before and after). It says
before a rebase of a stack which level goes through cleanly, where a conflict arises (the files) and which level only inherits it.
Two parts:

* **Port** (single branch): the prediction of the hidden Go `restack plan --branch --old-parent --new-parent`, here
  `stw restack plan BRANCH --old-parent REV --onto BASIS`: the commits of `REV..BRANCH` are replayed in memory onto `BASIS` (the M1
  engine, `src/replay.rs`, gix `merge_trees`); predicted conflicts with the paths and kinds, commits that become empty, a merge or root commit refused,
  an old parent that is no ancestor refused, unrelated histories refused. Go cases: `go_*` in `tests/it/restack_plan.rs`.
* **New** (no Go counterpart; owner go, live acceptance open): the whole stack. `A B C` are local branches, bottom to top, each sits on
  the one before (`not-stacked` otherwise); level n is replayed onto the new tip of level n-1. **Without a branch the chain is derived
  from the local branches** (owner decision "explicit plus auto-detection"): it starts at the checked-out branch and goes down; the
  branches whose tip is already part of the base are no levels; below each branch the nearest local branch is the next level. Anything
  ambiguous is refused, never guessed, and the explicit spelling is suggested (`instead: stw restack plan b b2 --onto main`):
  two names at one commit, a fork (two candidates, none below the other), a detached `HEAD` (`detached-head`), the checked-out branch
  already in the base (`nothing-to-restack`), no common history with the base (`no-merge-base`). Branches above the starting branch
  are not in the plan; a note names them. The local default branch (`main`, from `origin/HEAD`) is never a level, also when it is ahead of `origin/main` (a note says so);
  checked out, it is refused (`default-branch`).

`--onto` is the new base (any revision); default: the default branch of the repository (`origin/HEAD`, else `origin/main`,
`origin/master`), else a usage error. The old base of the bottom level is `--old-parent` or the merge base of the bottom branch and the base;
the old base of the next level is the old tip of the one below.

Output: the first line is the verdict, `plan: clean` or `plan: conflicts at A (files x, y)` (also `plan: refused at B (...)`,
`plan: unknown at B (...)`, `plan: refused (code: ...)` for an input that stops before any level), then per level the branch, the old
and new base, the number of commits and the state:

| state | meaning |
| --- | --- |
| `clean` | every commit replays without conflict (commits that become empty are dropped, as `git rebase` does) |
| `conflict` | a commit conflicts; the files and kinds are listed; the replay stops there |
| `inherited-conflict` | a level below has a conflict; this level depends on how it is resolved and is not computed (a deviation from the spike, which went on with conflict-marker commits) |
| `already-upstream` | every commit is empty after the replay and its patch-id is in the base: `git rebase` drops them |
| `became-empty` | every commit is empty after the replay, the base has the change in another form |
| `refused` | a merge or root commit in the level |
| `unknown` | the result would be clean but the replay is not safe here (below), or a bound (500 commits) was reached |
| `not-computed` | above a level that is `refused` or `unknown` |

A refusal before any level (no levels computed) prints no level count.
Exit 0 for `plan: clean` (the states `clean`, `already-upstream`, `became-empty` only), 1 for conflicts, refused or unknown (the plan is
printed), 2 usage (also: no `--onto` and no default branch, a branch that does not exist). JSON (schema 2, `command: "restack-plan"`):
`read_only`, `plan` (the first line), `verdict` (`clean`, `conflicts`, `refused`, `unknown`), `source` (`explicit`, `auto-detected`),
`onto{rev,commit}`, `levels[]` (`branch`, `tip`, `old_base`, `new_base` or null, `commits`, `state`, `reasons[]`, `files[]`,
`conflicts[{commit,path,kind}]`, `dropped`, `steps[{commit,status,subject}]`), `summary{levels,clean,conflict,inherited_conflict,became_empty,already_upstream,refused,unknown,not_computed}`,
`refusal{code,message,suggestion}` or null and `notes[]`. Branch names, paths, subjects and reasons come from the repository and are
escaped (`escape_untrusted`) in text and JSON.

**Limits of the replay, and the difference to a real `git rebase`.** The plan is a prediction by an in-memory approximation (M1,
header of `src/replay.rs`), tested against `git rebase` as the oracle in disposable repositories (`tests/it/restack_plan.rs`: the plan says
clean exactly when `git rebase` succeeds, and a predicted conflict names the files git leaves unmerged). It is not a run of `git rebase`:
* Directory renames: gix has none; the engine reports a conflict where git renames a directory silently or conflicts (conservative:
  it may say conflict where git succeeds, never clean where git conflicts, in the cases tested).
* Rename detection and diff algorithm are pinned (`merge.renames=true`, `merge.renameLimit=7000`, myers). 7000 is the default of git for a merge (documented, and measured
  with `git merge-tree` on 1050 inexactly renamed files: git pairs them, a limit of 1000 does not); gix counts like git (sources times destinations
  above the limit squared skips the inexact pairing). A repository configuration with another `merge.renames`, `diff.renames`,
  `merge.renameLimit`, `diff.renameLimit` (any value but 7000) or `diff.algorithm` makes a level that would be clean `unknown`.
* Binary files: both sides changed is a conflict, as in git; no binary merge driver runs.
* Submodules: a level with a commit that changes a gitlink is `unknown` unless it conflicts.
* Merge drivers and attributes: no driver runs. A `merge` attribute, the built-in macro `binary` (it sets `-merge`; git stops with "Cannot merge binary files") or a macro definition (`[attr]name ...`, reported where it is defined, used or not) that sets either, in any `.gitattributes` of the new base, of a branch tip or added or changed by a commit of the
  replayed range, or in `info/attributes` of the common git directory (also seen from a linked worktree), makes a level that would be clean `unknown`. Attribute files of the user (`core.attributesFile`) and of the work tree
  that are not committed are not read.
* rerere is unknown to the plan: a conflict that `rerere` could resolve is still reported as a conflict.
* `git rebase` drops a commit whose patch-id is in the base (`--cherry-pick`); the engine drops a commit only when its merge leaves the
  base unchanged. A patch that the base made and later reverted comes through here and is skipped by git.
* Not looked at: a dirty work tree or a running operation (git refuses a rebase there), hooks, `rebase.*` and `pull.rebase` settings,
  line-ending conversion (`merge.renormalize`, `core.autocrlf`), sparse checkout, a branch checked out in another worktree.
The plan never says `clean` when one of the listed uncertainties applies and the result would be clean (`unknown` with the reason).

**Deviations from the Go `restack plan`** (the Go command is hidden until its conformance evidence exists), one by one: (1) visible,
with the form `BRANCH... --onto BASIS [--old-parent REV]`; branches are short names of local branches (Go: fully qualified `refs/heads/...`),
`--onto` is any revision (Go: 40-hex id or qualified ref), the old parent is optional (default: the merge base); (2) not ported: the
range pairing (needs `git range-diff`; this command runs no git), the cherry classification of the parent's commits, the own-diff
equality, the identities (`--identity`) and the outcome table (`evidence-clean`), the overlap risk, the affected refs, worktrees and
hooks, the lease-push and resync texts (stw prints no commands), `--expect-head`, `--remote`, the `PATH` argument (here `--root`/`--repo`), the
attribute and repository hazard refusals (replaced by `unknown`), the refusal of a dirty target, an operation in progress and a bare
repository, the jj and GitButler refusal (decision 8), the exit status 3 (incomplete) and the result byte bound; (3) the replay is the
gix engine, not `git merge-tree`: the kinds agree with git, the paths of `rename/rename`, `rename/delete`, `file/directory` and
`directory-rename` differ (header of `src/replay.rs`); (4) exit codes: 0 only for `plan: clean` (Go: 0 for every complete plan); (5) own output and JSON layout (Go: its own schema).

## Prepare a review handoff

Use `stw workspace commit-plan --source index|worktree PATH... --json`
to preview which version of each selected file enters the commit. Save the
JSON outside the worktree and apply it with
`stw workspace commit-apply PLAN --message MESSAGE --json`. Use
`--repo PATH` when running outside the worktree. Apply rechecks the branch,
index, and selected contents, keeps unrelated staged changes, and leaves
worktree bytes alone. It handles existing regular files. The worktree source
commits raw file bytes using the index mode, without applying Git's content
filters. The ref and index
are separate updates with guarded rollback on an index publication error.
An interruption between the updates can leave the new ref with the old
index. Inspect the branch and index before retrying after such an interruption.

Use `stw restack conflict-report --repo REPO --store DIR --onto REV BRANCH...`
to save a conflicting restack plan and its available blob versions. The
store must be outside the source repository. A second agent can run
`stw restack show-conflict --repo REPO --store DIR ID` to get the versioned
JSON, including the conflict and any blocked descendants. Exit 1 signals
that a pinned revision moved. The original versions remain available for
diagnosis.

## Checkout-free comparison

`stw inspect github OWNER/REPO PR` reads a published pull request from GitHub
without opening a local repository. `stw inspect git REPO BASE HEAD` resolves
local commits and compares their trees from the merge base. The local command
uses Git objects and needs neither GitHub credentials nor a checkout of either
revision. Both modes return one JSON document with `schema_version: 1`, the
requested revisions, resolved SHAs, comparison base, changed paths and both
sides of detected renames. They perform no writes.

Each changed path lists tracked ancestor `AGENTS.md` paths. The previous side
of a rename uses the comparison base. Test suggestions are inferred from
changed Rust test paths and tracked manifests. They include the evidence
paths and are suggestions to verify before running.

Exit 0 means the comparison was read completely. Exit 1 means the report is
incomplete, with available evidence and a reason in `incomplete`. A GitHub
file or tree limit, a changed PR during the read, or unreadable local context
can produce a partial result. Exit 2 is reserved for CLI usage errors.
