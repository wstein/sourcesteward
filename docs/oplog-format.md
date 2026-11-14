# Op log formats: `refs.log` and `rewrites.log` (spec for the Rust product)

Status: draft for the owner; readers, writers, backup refs, `oplog undo` and the hook install (`stw oplog hooks`, section 11) are built (package 1 to 4). Source: the accepted spikes `spikes/stw/oplog-reflog`, `spikes/stw/oplog-hooks`,
`spikes/stw/oplog-refhook` and the owner decisions of 2026-11-11/12. The spikes' `refs.log` and `rewrites.log` already have one
writer (shell hooks); this spec makes their format the contract that the Rust product reads and also writes.
Words: MUST, SHOULD as in RFC 2119. Open questions are at the end.

## 1. Location and files

All files live in `<git-common-dir>/stw/oplog/` (one log per repository, shared by all linked worktrees). Directory mode 0700, files 0600.

| File | Writer | Content |
|---|---|---|
| `refs.log` | `reference-transaction` hook (origin `hook`), stw itself (origin `stw`) | ref changes |
| `rewrites.log` | `post-rewrite` hook, stw itself | commit pairs of amend/rebase |
| `errors.log` (fallback `<common-dir>/stw-oplog-gaps`) | hooks, stw | gap lines, format in 2.1 |
| `events-refhook.log` (written by the refhook install), `events.log` (written by the post-rewrite install) | install/uninstall | `<UTC time>\tinstall|uninstall`; two files because a reader of one hook must not take the other's word `install` for its own (the spike's reason). The product keeps both names |
| `config` | owner | `remotes=on|off`, read up to 1 KB, never executed |

Two logs stay separate (owner decision). Rotation applies only to `refs.log` and `rewrites.log` (section 6); `errors.log`, `stw-oplog-gaps` and the events files are never rotated (small, written rarely) and keep growing until the owner deletes them.

## 2. Common rules

- UTF-8 (in fact ASCII), LF terminated, TAB separated, append only. One `write` per line on a file opened `O_APPEND`; the longest possible `refs.log` line is below 2 KB (computed here, not measured: refname at most 601 bytes, `old` and `new` as `ref:` + 601 bytes each, time 20, context 15, worktree 40, 5 TABs, LF = 1892 bytes; with 64-digit hashes 810; UNKNOWN whether git reports `old` as `ref:` at all, so 1892 is a bound, not an observed value). `PIPE_BUF` (4096 on Linux, 512 guaranteed by POSIX) is a limit for pipes, not for files: the guarantee relied on is that an `O_APPEND` write to a regular file on a local file system goes to the end in one piece; network file systems give no such guarantee (UNKNOWN, as in the spike).
- A line is never rewritten or deleted except by rotation (section 6).
- Writer rule (stw): before appending, if the file is non-empty and its last byte is not LF (a hook killed mid-write), write one LF first, in the same `write` call as the new line (`\n` + line), so the broken fragment ends and the new line starts clean. The shell hooks do not do this; the reader handles it (section 9).
- Lock wait (stw): the append of a line to `refs.log`, `errors.log` or `stw-oplog-gaps` takes an exclusive lock of the file for the tail check and the write (`File::try_lock`), and never waits for it for long: it retries with a short backoff (2 ms, doubling to 50 ms) for 300 ms in all. `rewrites.log` is only rotated by stw, not appended to (section 6). If the lock is still held after that, or the file system has no locks, stw writes the line without the lock; this is no failure (no `write-failed` gap, no notice), as with a held `rotate.lock` (section 6). The budget is per line: a command with n ref updates waits up to n x 300 ms for `refs.log`, plus 300 ms for each gap line it appends. The shell hooks write with `>>` and never take the lock. Worst case without the lock: a line that is in flight in another writer can look broken to the tail check, and one extra LF may go first, as after a hook killed mid-write. That LF can end up as an empty line in front of another writer's line (observed: 8 to 12 empty lines in 12000 with six writers on a FUSE mount). The reader skips empty lines (section 9); they count neither as lines nor as `bad`.
- Time: UTC ISO 8601 with `Z` (`2026-11-12T00:42:26Z`), second resolution, or the word `unknown` (hook without a working `date`).
- Hashes: 40 or 64 lowercase hex digits (SHA-1 or SHA-256). The zero id is `0` repeated to the hash length. Any other value in a hash field makes the line `bad`.
- Free text from git or users is never written, except encoded names (section 3.2).

### 2.1 `errors.log` and `stw-oplog-gaps`

One line per event, 6 TAB fields, as in the spike: `<time> gap <kind> failed=<n> bad=<n> <worktree-or-branch>`. Field 2 is the word `gap`; field 3 is the kind (fixed words only). The refhook writes the failing phase (`prepared`, `committed`) as kind; the post-rewrite hook writes its `kind` argument, `amend`, `rebase` or `other` (`stw-oplog-hook:49-50`); stw writes `rotated` (a rotation happened, `failed=0 bad=0`, last field the name of the rotated file, `refs.log` or `rewrites.log`; **deviation from the earlier text, which said `-`**, so that the reader can tell a second rotation of one file, see section 6, Q15), `rotate-failed` (`failed=1`, last field the file name or `-`), `write-failed` (a `refs.log` line of stw could not be written, `failed=1`, last field `-`, Q16) and `unreadable` (a log could not be read). `rotated` is information, not a capture gap: the gaps section counts every line with a kind other than `rotated` as `capture-gap` and shows rotations as a separate row `rotated`.

## 3. `refs.log`

### 3.1 Line

Exactly 6 TAB fields:

    <time> <old> <new> <refname> <context> <worktree>

| Field | Meaning |
|---|---|
| `time` | see section 2 |
| `old`, `new` | hash, or `ref:<encoded target>` for a symbolic ref. `old` all zeros = created or unknown (see 3.3). `new` all zeros = deleted |
| `refname` | `HEAD` or `refs/*`, percent-encoded (3.2). Per-worktree refs appear as plain `HEAD`, never `worktrees/<id>/HEAD` |
| `context` | see 3.3 |
| `worktree` | `main`, or the worktree id (the directory name under `<common-dir>/worktrees/`, without the prefix `worktrees/`) of the writing process, not necessarily of the ref. The id is `other` if it contains `/`; every byte outside `[A-Za-z0-9._-]` becomes `?`; cut to 40 characters (hook `stw-oplog-refhook:103`). `-` for markers. stw writes `main` or the same reduced id of the worktree it runs in |

### 3.2 Encoding of names

Every byte outside `[A-Za-z0-9._/+@:-]` becomes `%XX` (upper-case hex, byte-wise, so `%` is `%25`, `ä` is `%C3%A4`). A name longer than 200 bytes is cut at 200 bytes and ends in `%00` followed by 8 hex digits
(djb2 of the whole unencoded name; NUL cannot occur in a ref name, so the suffix is unambiguous). A cut may split a UTF-8 sequence; harmless because bytes are encoded one by one.
Readers decode for comparison with reflog names and MUST replace control characters on display. The Rust writer MUST produce the same output as the hook for the same name (djb2 over the bytes of the whole unencoded name: `h = 5381`, per byte `h = (h * 33 + byte) mod 2^32`, 8 lowercase hex digits). Fixed test vectors (computed with an independent implementation; the Rust test MUST contain them, and the shell hook must give the same):

| Unencoded name | Bytes | Encoded form in the log | djb2 |
|---|---|---|---|
| `refs/heads/main` | 15 | `refs/heads/main` | `0866f11d` |
| `refs/heads/ä` | 13 | `refs/heads/%C3%A4` | `aee7ecff` |
| `refs/tags/v1%` | 13 | `refs/tags/v1%25` | `9cff28ce` |
| `refs/heads/` + 210 × `x` | 221 | first 200 bytes + `%00ffb8bca8` | `ffb8bca8` |
| `refs/heads/` + 110 × `ä` | 231 | first 200 bytes (encoded byte by byte, ends inside a sequence) + `%00b252bbfa` | `b252bbfa` |
| empty string | 0 | not a ref name; the function returns | `00001505` |

Only names longer than 200 bytes carry the hash; the other rows fix the function.

### 3.3 Contexts

| `context` | Written by | Meaning |
|---|---|---|
| `ref-transaction` | hook | a ref change as git committed it |
| `pre-old` | hook (phase `prepared`) | marker: `old` = value the ref has now, `new` = zeros, worktree `-`. Not a ref change. The reader joins it to the next `ref-transaction` line of the same ref with `old` = zeros: delete `old -> 0`, or a move without expected old value `marker -> new`. A marker without a following line is an aborted transaction: ignored. A marker is consumed by exactly one following `ref-transaction` line of the same ref, whatever its `old` (the spike consumes it always; a following line with `old` not zeros keeps its own `old` and the marker is dropped). A second line with `old` = zeros has no marker (shows `old value unknown`) |
| `pre-same` | hook | marker: current value equals the new value (packing, no-op). The reader drops it together with the next `0 -> value` line of that ref |
| `stw` | stw | a ref update stw wrote itself (gix writes). Written so far: `stw sync` (create and prune of `refs/stw/recovery/sync/**`, the branch fast-forward and its put-back; for a checked-out branch the line is for the branch, not `HEAD`), `stw tidy apply` (branch delete) and `stw tidy restore` (branch create), `stw oplog undo` (counter ref update, backup ref create, prune of old backup refs, the branch update; section 8). `old` is the real old value, zeros for a create; `new` is zeros for a delete; never a marker. The line is written after the gix edit succeeded, independent of `core.logAllRefUpdates` (Q14). The command is not in the line; see Q2 |

Readers MUST ignore unknown contexts and lines with fewer than 5 fields (counted as `bad`, shown in the gaps section). A line with a good shape but unknown context is kept in JSON output (`context` verbatim, encoded) and skipped in the timeline.
A line with `old` and `new` both zeros is a delete whose old value git did not tell; without a marker it is shown as `old value unknown`. The second `0 0` line of an already deleted ref (git runs two transactions for a packed-only tag) is dropped by the reader (it follows a delete of the same ref with nothing created in between). `HEAD` lines that repeat a branch line of the same transaction are not written by the hook.

### 3.4 What is logged

Only `HEAD` and `refs/*`. `refs/remotes/**` only with `remotes=on` (default off; stw's own writes to `refs/remotes/**` follow the same switch). Not logged: pseudo refs (`ORIG_HEAD`, `AUTO_MERGE`, ...), `old = new`, the pruning of loose refs by `pack-refs`/`gc` (a delete whose old value is in `packed-refs`; the hook reads `packed-refs` only then, once, lazily). The packing itself is logged as `0 -> value` per ref after a `pre-same` marker, which the reader hides together with the line.
Per-worktree refs `refs/bisect/*`, `refs/worktree/*`, `refs/rewritten/*` get no `pre-old` marker (a delete shows `old value unknown`).

### 3.5 Hook phase rules (contract for the hook text)

The hook always exits 0 in every phase; `prepared` does at most one lookup per ref reported with old = 0 (loose ref, else `packed-refs`); `aborted` and unknown phases do nothing. A write error goes to `errors.log`, then `stw-oplog-gaps`, else nowhere (the transaction goes on).
The hook is a `sh` script embedded in the stw binary (`print`), header line 2 `# stw-oplog-refhook v1`; it does not call stw at run time.

## 4. `rewrites.log`

Exactly 6 TAB fields:

    <time> <amend|rebase|other> <old sha> <new sha> <extra> <branch|detached>

`extra` and `branch` are reduced: every byte outside `[A-Za-z0-9._/+@-]` becomes `?` (one `?` per byte, so a multi-byte character gives several; the original is not recoverable); `extra` may be empty, `branch` is `detached` if none. The kind written by stw itself is `other` (Q3 proposes a fixed word `stw`).
Readers ignore lines with fewer than 4 fields. **Unknown kinds are kept in the chain** (an edge is an edge; the kind is shown verbatim, escaped); only unknown `context` words of `refs.log` are skipped in the timeline (section 3.3). `(old, new)` is one edge; for duplicate pairs the first line wins. Self pairs are ignored. Several old commits with the same new commit = merge (fixup/squash); one old with several successors = split; cycles stop the chain. These semantics are those of `evolog` in the spike.

## 5. Version field

The line formats have no per-line version. Version lives in:

1. the hook header line 2 (`# stw-oplog-refhook v1`, `# stw-oplog-hook v1`), which is how install/uninstall recognise their own file;
2. the file name: a change of the field count or meaning of a field means new file names (`refs2.log`, `rewrites2.log`), never a silent change of the old files;
3. JSON output of the readers: `schema_version` (the product starts at 1 for the oplog JSON, independent of other stw schemas).

Adding a new `context` word or `rewrites.log` kind is not a format change (readers ignore unknown words). Old hooks keep writing the old files after a product upgrade. Reading two generations is not built for v1 (Q6).

## 6. Rotation and retention

- Only `refs.log` and `rewrites.log` rotate, by size: above 10 MiB stw renames the file to `<name>.1` (replacing an older `.1`) and the next append creates a new file; exactly one old generation. No silent deletion otherwise; no time-based expiry. `errors.log`, `stw-oplog-gaps` and the events files do not rotate (section 1).
- **Who rotates.** Only stw commands that write anyway (those that append their own `refs.log` or `rewrites.log` lines: sync, undo, tidy and similar) check for rotation, after their append. `timeline`, `evolog`, `undo-plan`, `status` and every other read-only command never write, also not a rotation (the read-only tests compare `.git` byte for byte). The hook scripts never rotate (cost and risk in `prepared`/`committed`).
- **Lock.** stw takes an exclusive OS file lock on the file `rotate.lock` (`std::fs::File::try_lock`, stable since Rust 1.89, toolchain 1.99.1; the file is created if absent and never deleted). The lock is released by the OS when the process ends, so there is no stale lock and no takeover. Sequence: `try_lock`; **check the size again under the lock** (another stw may have rotated already); rotate; unlock. **If the lock is held, do not rotate and do not wait** (`try_lock` returns at once): the command goes on and the next writer tries again. Rotation never makes a command block or fail; a failed rotation is a gap line `rotate-failed` and the command's result is unaffected. Network file systems: lock semantics are not guaranteed there (UNKNOWN); the worst case is two rotations in a row, which loses the older generation.
- A rotation is noted in `errors.log` as `<time> gap rotated failed=0 bad=0 <file>` with the name of the rotated file, `refs.log` or `rewrites.log` (section 2.1, Q15).
- A hook that opened the old file just before the rename keeps writing to `.1` for that one line (acceptable).
- Readers read `<name>.1` then `<name>` as one stream, so a rotation does not cut the timeline or the join of a `pre-old` marker at the end of `.1` with its line at the start of the new file. After a second rotation the oldest generation is gone; the gaps section then says `truncated` (for that file, from two `rotated` lines with that file name in `errors.log`) meaning "older lines were rotated away". The reflog spike uses the same word for a reflog that was expired or cut; the product keeps both rows with their own text.
- Backup refs (section 8) are separate: retention 10 runs.

## 7. Timeline model

The timeline merges reflog rows (gix reflog iterator, origin `reflog`) with `refs.log` rows (origin `hook` or `stw`; the sync journal refs `refs/stw/recovery/sync/**` are written by gix and get origin `stw` once stw logs its own writes, see section 8 item 7), sorted by time, then by file order. Row fields: `time`, `ref`, `old`, `new`, `origin`, `text`, `flag`.

- A ref change is `covered` when a log line exists for the same (ref, new value) within the hook window (after `install`, before `uninstall`); a reflog row without it inside the window is `uncovered`. Comparison by (ref, new value); the same value twice counts as covered once (known nit of the spike).
- Grouping is always labelled `suspected` (`"group_basis":"suspected"`): same text stem and time proximity. It is a display aid and never proof of one operation. Undo plans refer to one explicitly named ref change, never to a group.
- Gaps section always printed: `capture-gap`, `uncovered`, `pre-install`, `truncated` (older lines rotated away), `rotated` (a rotation happened), `still open` (clone, `-c core.hooksPath`, other tools), as in the spikes.
- JSON: `schema_version`, `entries[]`, `gaps{}`. Output goes through `escape_untrusted`; no log text is trusted.

## 8. Undo, compare-and-swap and backup namespace

Undo of a ref (one named change, not a full undo: index, worktree, other worktrees, pushes and running operations are not covered):

1. `undo-plan` writes nothing and prints all checks and, when they all pass, the `undo` command that applies it; `stw oplog undo REF --to OLD --expect NEW --yes` needs `--to` (a prefix of an old reflog value, as in the plan; no `--steps`), `--expect` (the full current value, lower case) and `--yes`. Without `--yes`, or with an abbreviated `--expect`, it is a usage error (exit 2) and nothing is written. A refused check or a changed ref is exit 1 with the plan's checks printed and nothing written. A ref that is no local branch is a usage error (the plan previews it).
2. Checks: the ref is `refs/heads/*` (other types: plan only); not symbolic; not checked out in any worktree (otherwise refuse and hint `--keep`, section 8a); old value is a commit and exists; the target is not the creation entry.
3. **Backup ref.** One safety copy per undo run on the current value, named `refs/stw/backup/<run>/<k>`. **Decided by the owner (2026-11-13, Q7, variant A):** the form is `refs/stw/backup/<run>/<k>` (`k` = 1, 2, ...; with one ref per run `refs/stw/backup/<run>/1`). Retention counts runs (10 runs, as for the sync journals), and a run can hold more than one ref. The earlier form `refs/stw/backup/<n>` is rejected: with one ref per number, retention would count refs instead of runs.
   - `<run>` is a positive integer that is **never reused**. A counter ref `refs/stw/backup-counter` points to a blob with the last used `<run>` as decimal text; a new run writes a new blob `<run> <pid>-<nanos>` (the number, a space, a nonce) and updates the counter with a CAS (`MustExistAndMatch`; `MustNotExist` for the first run). `<run>` is the larger of the counter and the highest `<run>` of an existing backup ref, plus one: a counter that was deleted or lowered by hand must not hand out a number that is in use or that the retention would remove at once. The nonce makes the blob differ for every writer: without it two first runs would write the identical blob `1`, the same OID, and `MustNotExist` would let the second pass (gix-ref `prepare.rs:126-134`). The lookup of item 4 is not used for the counter; the nonce is enough. A failed CAS means another run took the number: re-read the counter and try again with the new maximum + 1; give up after 5 tries with an error. Implementation: every failure of the counter update counts as a lost race (gix does not tell the cause apart), so a persistent I/O error also ends after 5 tries, with the last error in the message. The lookup of item 4 is not a lost race: it fails the command, as does any other error after the number was taken. A run number that was taken and then not used (the create-only check of item 4 fails, or a later step) is not given back; the counter only grows. The counter survives pruning, so that after pruning the newest runs a number is not reused (the maximum over existing refs alone would do that).
   - A name below `refs/stw/backup/` is a run only when it has the form `<run>/<k>` with `<run>` and `<k>` canonical positive decimal numbers (digits only, no sign, no leading zero, fits `u64`). `+1/1`, `01/1`, `0/1`, `1/01` and names of any other form are not runs: they are never counted and never pruned.
   - Retention: after creating a run, remove all runs except the 10 highest `<run>` values (all `k` of a run together) and except the run just created (the newest run of the caller always stays, also when other writers took higher numbers meanwhile). The counter ref is never pruned.
   - `refs/stw/recovery/sync/<run id>/<n>` (the sync journal, 10 runs) stays as it is and is not renamed by this spec. The spike's `refs/stw/recovery/oplog/` is not used by the product.
4. **Create-only check.** gix lets `PreviousValue::MustNotExist` pass when the ref already holds the same value (gix-ref `prepare.rs:126-134`), so the flag alone does not enforce create-only. Before creating a backup ref the code looks the ref up and fails if it exists with any value, as `create_recovery_ref` in `src/sync/journal.rs:493` does (`try_find_reference(...)` then error), and also passes `MustNotExist`, which stops a ref with another value that appears between the lookup and the edit. A ref that appears there with the same value is not caught (gix lets it pass); it holds the value the backup would hold.
5. **Reflog.** The reflog entries of the backup ref and of the undone branch must be written even when the repository has `core.logAllRefUpdates=false`. In gix 0.90 that setting makes the ref store run with `WriteReflog::Disable`, and `force_create_reflog: true` on the edit then has no effect. Mechanism: stw opens the ref store handle for these writes with `write_reflog = WriteReflog::Always` (a handle made for the call, not a change of the repository config) and asks for `RefLog::AndReference`; message `stw oplog undo`. **Required test:** a repository with `core.logAllRefUpdates=false` has the reflog entries for the backup ref and the branch after `undo`, with a mutation that removes the `Always` handle. If the mechanism does not work with gix 0.90 as written, this item is replaced by "no reflog guarantee with `logAllRefUpdates=false`; the `refs.log` line of item 7 is the record". **Result (package 3):** the mechanism works with gix 0.90 as written: the handle is `repo.refs.clone()` with `write_reflog = WriteReflog::Always`, applied with `transaction().prepare(..).commit(committer)`; the test `the_reflog_entries_are_written_with_log_all_ref_updates_false` has the entries for backup ref and branch, and mutation OU10 (the repository's own handle) fails it. The replacement text is therefore not needed. The counter ref gets no forced reflog (`force_create_reflog: false`; refs below `refs/stw` have none by default). Without a configured identity the reflog entries carry the in-memory fallback of `repo::open` (`stw <stw@localhost>`); nothing is refused.
6. **CAS update.** Update the ref with `old := --expect`, `new := --to` and `PreviousValue::MustExistAndMatch`, no-deref semantics (as `git update-ref --no-deref REF OLD CUR`); a change since the checks fails the update and leaves the backup.
7. stw appends its own `refs.log` lines (context `stw`) for the counter update (`refs/stw/backup-counter`), the backup creation, one line per pruned backup ref (`old` the value, `new` zeros) and the undo update, in this order; each line is written after its ref edit succeeded. A failed append is reported (gap `write-failed` and a notice on stderr, Q16) and does not undo the ref change.
   Order of the undo: plan checks, `--expect` check, counter, backup ref, prune, CAS update of the branch. The prune runs before the branch update (spec: "after creating a run"); a lost CAS leaves the backup and the prune as they are. Limit: a branch that is checked out between the checks and the update is not seen again (the CAS protects the value, not the checkout state).
8. The result prints backup ref and the way back as a command: `stw oplog undo REF --to <old value> --expect <new value> --yes`, which is an undo of the undo (the branch reflog has the entry the undo wrote). In the text output (the way back here, `Apply with:` of `undo-plan`) the ref name is written for pasting into a POSIX shell: unchanged when it holds only `[A-Za-z0-9._/+@:=%,-]`, else in single quotes with `'` written as `'\''`. The JSON fields `way_back` and `apply` keep the raw name (values, not shell text). `--json` adds `executed`, `previous`, `current`, `backup_ref`, `run`, `pruned`, `way_back`, `notices` to the common keys; `undo-plan --json` gains `apply` (null unless executable).

The same CAS rule applies to every stw ref write: expected old value always given.

## 8a. Undo of a checked-out branch: `--keep`

New behavior without a Go counterpart; the owner accepts it live. Owner decision 2026-11-11: a checked-out branch gets its own `--keep` path in which stw runs `git reset --keep <old value>` itself, only with a clean index and a clean work tree; never `--hard`.

`stw oplog undo-plan REF [--to OLD | --steps N] --keep` and `stw oplog undo REF --to OLD --expect NEW --keep --yes`. Without `--keep` nothing changes (a checked-out branch is refused, the text now says "add --keep ... or use `git reset --keep`"). Without `--yes` the `undo` is a usage error as before; the plan is `undo-plan --keep`, whose `Apply with:` line and JSON `apply` carry `--keep`.

Checks added by `--keep` (all in the shared plan function, so `undo-plan --keep` shows the same; each refusal writes nothing):

1. **Which worktree.** The worktree the command runs in: the repository the command selects (`--repo`, or the top level of the current directory; inside a linked worktree that worktree) and its `HEAD`. The branch must be that `HEAD`'s branch. Not checked out here (HEAD on another branch, or detached): refused ("not checked out in this worktree"). Checked out in any other worktree of the same repository (also when this one holds it too, which git forbids but `--ignore-other-worktrees` allows): refused ("another worktree"). The move of a branch that only another worktree holds is therefore done by running the command there.
2. **Clean.** No staged change, no unstaged change of a tracked file, no unmerged path. Counted with the status of `sync restore` and `sync --rebase` (gix; a submodule's own changes do not count, as `--ignore-submodules=all`). Untracked and ignored files do not count and stay. A status that cannot be read counts as not clean. (`git reset --keep` alone would keep some local changes; stw does not rely on that.) Also refused: an intent-to-add entry (`git add -N`, `reset --keep` would lose the marker) and an assume-unchanged or skip-worktree entry (a local edit there is invisible to the status check). Checked from the index flags.
3. **No operation** (`MERGE_HEAD`, rebase, cherry-pick, revert, bisect, sequencer) in any git dir: the existing plan check of section 8, item 2.
4. **No gitlink differs** between the current value and the target (a tree diff by path, no rename tracking: added, removed, changed or type-changed gitlink). `git reset --keep` does not touch submodule checkouts, so a submodule would stay on the wrong commit. Refused with the path.
   **In the way.** For every path the target ADDS (relative to the current tree), an entry in the work tree at that path, or a file where one of its directories is needed (unless the target deletes that file), is refused with the paths (first three). The work tree is clean, so what is there and not in the current tree is untracked or ignored, and `git reset --keep` overwrites such a file without a question (shown with an ignored file; untracked ones make git itself fail with an error). Reviewer finding 2026-11-14: this was data loss and is now refused in the plan, before the backup. A path that the target changes (tracked, clean) or removes is not affected. Untracked files elsewhere stay.
5. **No sparse checkout** (`core.sparseCheckout` true, read from the configuration): refused.
6. **Git.** The installed `git` must run and be 2.36 or newer (the minimum of the runner for `reset --keep`), and the object ids must be 40 digits (the runner's `Hex40`). Both are checked in the plan (`undo-plan --keep` starts `git --version` once, so it is no longer a pure gix read) and before the counter is touched, so a missing or old git leaves no backup ref (a refusal, exit 1). A repository whose configuration names a program (`filter.*`, `include`, `core.sshCommand`, ...) is stopped by the runner after the backup (see "After the backup").

Sequence: plan checks, `--expect` check, git checks (item 6), counter, backup ref, prune, then, instead of the CAS update of the ref: the ref is read again (it must still hold `--expect`) and `HEAD` must still be on the branch; then `git reset --keep --quiet <old> --` runs in the worktree through the runner (`src/git_runner.rs`, `Op::ResetKeep`: fixed argument vector, `-c core.hooksPath=/dev/null` and the other leading `-c` options of every runner call, scrubbed environment, no editor, no prompt, output and time limits). Then the `refs.log` line for the branch (origin `stw`, old and new value) is written, as for a plain undo. Index, work tree, `HEAD`'s branch move together; untracked files stay.

Differences from a plain undo, one by one:

- **Compare-and-swap.** Git's own ref update inside `reset` has no expected old value, so the guard is the re-read just before git starts. A change inside git's own run (a few milliseconds) is not caught; the plain path's CAS is exact. A change between the plan checks and the re-read is caught and leaves the backup.
- **Reflog.** The reflog entries of the branch and `HEAD` are written by git itself (message `reset: moving to <old>`), subject to `core.logAllRefUpdates` (with `false` git may write none). The `WriteReflog::Always` handle of section 8, item 5, applies to the backup ref only. The `refs.log` line is the record either way.
- **Hooks.** No hook of the repository runs (the runner's `core.hooksPath=/dev/null`). With the opt-in hooks installed, git's own ref update is therefore not logged by the hook; stw writes the line itself (the same rule as for every stw write, Q13).
- **After the backup.** A failure of git (an untracked file that the target would overwrite, a lock, the runner's refusal for a `filter.*` driver or another key that starts a program) is exit 1: "the branch was not changed (...). The backup refs/stw/backup/<run>/1 stays". Nothing is rolled back: the backup ref, the counter and the prune stay, as after a lost CAS (the run number is not given back). A refusal before the counter (checks above) leaves nothing. A git that was killed (signal), timed out (120 s) or hit the output limit may have written part of the work tree: the message says "may be changed in part; look at `git status` and `git reflog`" instead of "was not changed".
- **Output.** Text: `Result: done. The branch, the index and the work tree of this worktree were moved with git reset --keep ...`; the way back is the same command with `--keep` (an undo of the undo works on the same terms: clean tree, new reflog entry). JSON of `undo` and `undo-plan` gains `keep` (bool); `undo` gains `via` (`git` or null).
- **Exit codes** as for the plain undo: 0 done, 1 refused or failed, 2 usage.

Not covered (UNKNOWN or by design): a submodule's own work tree is not part of the check (changed contents of a submodule do not count as dirty, and `reset --keep` leaves them alone); a repository with `extensions.worktreeConfig` where `core.sparseCheckout` lives in another worktree's config; `git` older than 2.36 is refused rather than tried; SHA-256 repositories are refused for `--keep` (the runner has no 64-digit target); Windows and macOS were not run. The status error branch of the cleanliness check (unreadable status counts as not clean) has no test.

## 9. Parser behaviour (summary)

| Input | Behaviour |
|---|---|
| empty line (nothing between two LFs, or a lone LF) | skipped; not a line, not `bad`, no gap. A harmless leftover (an aborted append, an extra LF of the lock wait, section 2). A line of only white space is not empty: it has fewer fields than the minimum and is `bad` |
| fewer fields than the minimum (5 refs, 4 rewrites) | skipped, counted as `bad` |
| more than 6 fields | the first 6 are read, the rest ignored (forward compatibility), **unless** a field 7 or later is not a `key=value` extension, pattern `[a-z][a-z0-9_]*=[A-Za-z0-9._/+@:=%-]*`; then the line is `bad`. A time, a hash or a name in field 7 or later is therefore `bad`, not an extension |
| `old`/`new` not hash or `ref:`, bad time | counted as `bad`, skipped |
| unknown `context` (refs.log) | kept in JSON, skipped in the timeline |
| unknown kind (rewrites.log) | kept in the chain, shown verbatim (escaped) |
| missing file | empty log; the gaps section says "no install record" |
| file not regular (FIFO, device) | not read, gap `unreadable` |
| huge line (over 4 KB) | skipped as `bad`, stream continues |
| last byte of the file is not LF | a final fragment with 6 valid fields is read; a shorter fragment is `bad`. A stw writer that finds such a file writes an LF first (section 2) |
| log file or `stw/oplog` directory is a symlink | refused, gap `unreadable` (no following of links in the common dir, as the install rules of the spikes) |
| NUL byte | line skipped as `bad` |

`bad` lines are counted per file and shown in the gaps section; they are never deleted.

Parsing is by bytes, not lossy UTF-8 conversion; names are decoded from `%XX` only for comparison.

## 10. Open questions (with recommendation)

1. **Context word for stw's writes.** Recommend a single word `stw` in `refs.log`; the origin column of the timeline then reads `stw`. Alternative: one word per command (`stw-sync`, `stw-undo`): more detail, but the field becomes a vocabulary the hook readers must know. Recommend `stw` and put the command in the reflog message.
2. **Command name in the line.** The format has no field for "which stw command". Recommend: no 7th field (format stays frozen at 6); the backup ref and the reflog message carry it. Section 9 allows later extension fields, only as `key=value`.
3. **`rewrites.log` kind for stw.** Recommend a new fixed word `stw` (instead of `other`) so that stw's own rewrites are distinguishable; allowed because unknown kinds are accepted by readers.
4. **Rotation by hook or by stw only.** Recommend stw only (section 6): the hook stays at one `printf >>`. Price: a repository that never runs stw grows unbounded until the next stw write command (section 6); acceptable (10 MiB is about 80 000 lines).
5. **Rotation threshold.** 10 MiB per file and one generation as decided. Recommend keeping it config-free for the first version; `config` key `max_bytes` later if needed.
6. **Reading both file generations.** After a format change (new file names) the product reads old and new. Recommend: not implemented for v1 (no second format exists yet); keep the rule in this spec only.
7. **Backup name.** Decided by the owner (2026-11-13): `refs/stw/backup/<run>/<k>` (section 8 item 3); retention counts runs. `<run>` comes from the counter ref `refs/stw/backup-counter` and is never reused. The plain form `refs/stw/backup/<n>` is rejected (retention would count refs). Implemented so.
8. **Markers across rotation.** Recommend joining across files as in section 6; the alternative (markers dropped at the boundary) loses the old value of one delete per rotation.
9. **SHA-256 repositories.** The spec allows 64 digits; nothing was run beyond forged hook input. Recommend a Rust test with `--object-format=sha256`, marked as a deviation from the spike's proof.
10. **Timeline joining of `rewrites.log` and `refs.log`** ("which command"). The spike does not join them. Recommend not joining in v1; `evolog` and `timeline` stay separate commands.
11. **Reading `refs.log` of a repository where the hook was never installed** (stw's own lines only). Recommend: valid; the gaps section states "no hook install record" and marks every hook-covered kind as open.
12. **Reflog with `core.logAllRefUpdates=false`.** Recommend keeping the `WriteReflog::Always` handle (section 8 item 5) only if the required test passes with gix 0.90; otherwise drop the promise. The `refs.log` line is the record either way. **The test passes; the handle is kept.**
13. **Which gix updates stw logs (package 2).** Recommend all gix ref writes that exist: sync recovery ref create and prune, sync branch fast-forward and its put-back, tidy apply branch delete, tidy restore branch create. Not logged: ref writes through `git` processes (hooks see them when installed; `stw sync` runs its fetch with `core.hooksPath=/dev/null`, so remote-tracking updates stay unseen, `still-open` row) and the object writes. Later packages add undo and backup refs.
14. **`core.logAllRefUpdates=false`.** Recommend writing the `refs.log` line anyway: it is stw's own record and does not depend on reflogs (the gix reflog of the sync writes still follows its own rules). Implemented so.
15. **Truncated after two rotations (ledger N2).** The `rotated` gap line carries the file name in its last field (deviation from 2.1, named there); the reader says `truncated` for a file with two or more such lines. Lines with `-` (hand-made or from a version without the name) count for no file. `errors.log` is never rotated, so the count only grows; a repository that rotated twice long ago still says `truncated`, which stays true (the oldest lines are gone). Alternative: no name in the line and a count over both files (imprecise); not chosen.
16. **A write that fails.** Recommend best effort: the command's result is unchanged; stw writes a `write-failed` gap line (`errors.log`, else `stw-oplog-gaps`) and one notice on stderr (`stw: op log: the line for <ref> was not written: <reason>`), per update. Causes: `stw` or `stw/oplog` is a symbolic link, a log that is not a regular file, a full disk, no permission. The reader counts it as `capture-gap`. A held `rotate.lock` is no failure (no notice, no gap); neither is a lock of a log that stays held beyond the budget of section 2 (the line is written without the lock). Alternative: fail the command; rejected because the ref change has been made already and the spec 8 item 7 says a failed append does not undo it.
17. **Time and files.** Time is the system clock, UTC, second resolution (`unknown` is never written by stw). Rotation checks both `refs.log` and `rewrites.log` after every stw append to `refs.log`; no stw command writes `rewrites.log` yet, so its rotation is only reachable when a hook-written file is large. The threshold is the constant 10 MiB, no config key (Q5).
18. **`rust-version`.** `File::try_lock` is stable since Rust 1.89; `Cargo.toml` `rust-version` is raised from 1.88 to 1.89 (the toolchain is 1.99.1 already).

## 11. Hook install (`stw oplog hooks status | install | uninstall | print`)

New behavior without a Go counterpart (package 4); the owner accepts it live. It places the two hooks of the spikes in ONE repository.
The script texts are the spike texts byte for byte (`src/oplog/hooks/post-rewrite.sh`, `reference-transaction.sh`; header line 2
`# stw-oplog-hook v1` and `# stw-oplog-refhook v1`, section 5); a test compares them with `print` of the spikes. The formats of sections 3 and 4 are not changed.

Rules (owner decisions 2026-11-11/12, install rules of the spikes):

1. **Opt-in per repository.** Only `stw oplog hooks install --yes` writes a hook. `stw add`, `tidy`, `status` and every other command never install one (a hint is not part of this package).
   Without `--yes`, `install` and `uninstall` print the plan, write nothing and exit 1 (as the spikes; `oplog undo` without `--yes` is a usage error, exit 2: the plan is `undo-plan` there).
2. **`core.hooksPath` is read and never written.** Read from all sources the way git does (repository, worktree, global, system config, `GIT_CONFIG_COUNT/KEY/VALUE`, `GIT_CONFIG_PARAMETERS`; the last value wins; an empty value counts as set). When it is set, `install` refuses and writes nothing: it names the value, its origin and the place each hook would belong (`<value>/<name>`); the directory belongs to another tool and the owner decides. No config is written, ever.
3. **Existing hooks are respected.** A file (or link, or directory) named `post-rewrite` or `reference-transaction` that is not ours is never overwritten, chained or rebuilt: `install` refuses and prints a chain proposal as text (the hook script from `stw oplog hooks print NAME`, called from the existing hook; for `reference-transaction` with `|| :`, because the exit code of the phase `prepared` aborts the transaction). Ours means: a regular file whose line 2 is exactly the header (not a link, not `v10`, not CRLF).
4. **Both or nothing.** A refusal for one of the two hooks (or the repository-wide refusals of rules 2 and 5) refuses the install of both: the readers (`evolog`, `timeline`) then never see a half installation that looks complete. A hook that is already ours is left alone ("already installed"); a missing second one is added.
5. **Hooks directory.** `<git-common-dir>/hooks`, which all linked worktrees of a repository share (run from any worktree; the logs are the common ones). A submodule has its own git directory and hooks directory (`--repo NAME` selects it; `status` of the superproject lists the submodules as a hint). A hooks directory that is a symbolic link may be shared with other repositories: `install` and `uninstall` refuse. A missing hooks directory is created.
6. **Writing.** The script is written to a temporary file next to the target (mode 755) and put in place with a hard link, which fails when the target exists: a hook that appeared meanwhile wins and is never replaced. `<common>/stw/oplog` is created with mode 0700 (not through a symbolic link) and `install` appends `<UTC time>\tinstall` to `events.log` (post-rewrite) and `events-refhook.log` (reference-transaction), mode 0600, one append; `uninstall` appends `uninstall`. If the directory or the file cannot be written (a link, a file), the install stands, the output has a `note` line, and the timeline reports "no install record" (the hooks create the directory themselves on their first run).
7. **Uninstall** removes a file only if it is ours (rule 3) and the hooks directory is no link; foreign files are reported ("not ours") and left; the logs stay. A foreign file is no failure: there is nothing of ours to remove.
8. **No program runs.** `install`, `status`, `uninstall` and `print` start no git program (gix reads the configuration); the hooks themselves run `git rev-parse`, `date` and `awk` when git calls them.

Output: text as the spikes (`refused`, `plan`, `Result:`, `note`, `not executed: --yes is missing`), `--json` with `schema_version` 1, `command` (`oplog hooks install`, ...), `repo`, `hooks_path` (`value`, `origin` or null), `hooks[]` (`name`, `file`, `header`, `state` `absent|ours|foreign`, `executable`, `action` `install|already-installed|refused|remove|not-ours|none`), `executed`, `refused`, `notes`, `result`; `status` adds `hooks_dir`, `hooks_dir_is_symlink`, `worktrees`, `submodules[]`, `logs[]`. Exit 0: done, already installed, nothing to remove, status. Exit 1: refused, plan only, a write failed. Exit 2: usage.

Deviations from the spikes: (1) one command for both hooks (the spikes are two tools), all-or-nothing install (rule 4; the spikes decide per tool); (2) `uninstall` removes our file next to a foreign one and reports the other (the spikes refuse the whole run for the one tool); (3) the temporary file name has the process id and nanoseconds instead of `mktemp`; (4) `GIT_CONFIG_PARAMETERS` is parsed by stw (gix does not read it); the quoting forms `'k=v'` and `'k'='v'` are handled; (5) the chain proposal names `stw oplog hooks print NAME` instead of the spike tool; (6) the hook comments still say `Remove with: stw-oplog-hook uninstall --yes` (byte-identical to the spikes; a text change means a new header version, section 5).
UNKNOWN: macOS, a file system without hard links (the install then refuses with "cannot be linked"), `core.hooksPath` set only in a per-worktree config (`extensions.worktreeConfig`) of a worktree other than the one the command runs in, a repository whose ref backend gix does not read (reftable: `Ctx::repo` refuses), a bare repository (the workspace selection finds no root from inside it: `--root` is needed), root permissions (the unwritable-directory test is skipped as root).

Open questions for the owner (recommendation first):

19. **`core.hooksPath` set.** Recommend staying with refusal and the named place. Alternative: a later flag (`--into-hooks-path`) that writes into that directory after a plan; that directory is another tool's (husky, a shared template), and removing it later would touch their files.
20. **Existing hook.** Recommend refusal with the chain proposal. Alternative: write the chain into the existing hook after a backup; rejected for now (the existing hook is code of the owner; a rebuilt hook is the thing the decision says not to do).
21. **Hint after `stw add`, `tidy`, `status`.** A one-line hint ("op log hooks not installed: `stw oplog hooks status`") is not built; recommend adding it to `status` only, never an install.
22. **Updating an installed hook.** A newer script version needs `uninstall` and `install` (the header is the version); `install` leaves a hook with an older header alone ("ours"). Recommend a `--update` later when the first v2 exists.
23. **`--keep` without a CAS inside git.** Recommend accepting the re-read before git (section 8a). Alternative: no `git reset`, but a gix checkout of the new tree plus a CAS update of the ref; rejected for now (a second implementation of `reset --keep`'s merge rules, which the owner asked git to apply).
24. **Reflog text of `--keep`.** git writes `reset: moving to <old>`; the message `stw oplog undo` is not used there. Recommend leaving it (the backup ref and the `refs.log` line name the command). Alternative: `GIT_REFLOG_ACTION` in the runner's environment (a change of the shared runner).
25. **Failure after the backup.** A failed `reset --keep` leaves a backup ref (retention 10 runs) for an undo that did not happen. Recommend leaving it (as after a lost CAS); alternative: delete the backup of this run on failure (a second delete path next to the retention).
