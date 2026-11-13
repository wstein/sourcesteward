# Op log formats: `refs.log` and `rewrites.log` (spec for the Rust product)

Status: draft for the owner, nothing built. Source: the accepted spikes `spikes/stw/oplog-reflog`, `spikes/stw/oplog-hooks`,
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
| `stw` | stw | a ref update stw wrote itself (gix writes). Written so far: `stw sync` (create and prune of `refs/stw/recovery/sync/**`, the branch fast-forward and its put-back; for a checked-out branch the line is for the branch, not `HEAD`), `stw tidy apply` (branch delete) and `stw tidy restore` (branch create); later: undo, backup refs. `old` is the real old value, zeros for a create; `new` is zeros for a delete; never a marker. The line is written after the gix edit succeeded, independent of `core.logAllRefUpdates` (Q14). The command is not in the line; see Q2 |

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
- A rotation is noted in `errors.log` as `<time> gap rotated failed=0 bad=0 -` (section 2.1).
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

1. `undo-plan` writes nothing and prints all checks; `undo` needs `--to OLD`, `--expect NEW` (full current value) and `--yes`.
2. Checks: the ref is `refs/heads/*` (other types: plan only); not symbolic; not checked out in any worktree (otherwise refuse and hint `git reset --keep`; a `--keep` path is a later own package); old value is a commit and exists; the target is not the creation entry.
3. **Backup ref.** One safety copy per undo run on the current value, named `refs/stw/backup/<run>/<k>`. **Deviation from the owner decision:** the owner named the form `refs/stw/backup/<n>` with retention 10. Retention is meant per run (10 runs, as for the sync journals), and a run can hold more than one ref (`k` = 1, 2, ...); with one ref per number, retention would count refs instead of runs. For the common case of one ref per run the name is `refs/stw/backup/<run>/1`. The owner decides (Q7); the plain form is the alternative if every run holds exactly one ref.
   - `<run>` is a positive integer that is **never reused**. A counter ref `refs/stw/backup-counter` points to a blob with the last used `<run>` as decimal text; a new run writes a new blob `<run> <pid>-<nanos>` (the number, a space, a nonce) and updates the counter with a CAS (`MustExistAndMatch`, or `MustNotExist` plus the lookup of item 4 for the first run). The nonce makes the blob differ for every writer: without it two first runs would write the identical blob `1`, the same OID, and `MustNotExist` would let the second pass (gix-ref `prepare.rs:126-134`). A lost race means another run took the number (the CAS or the lookup failed): re-read the counter and try again with its number + 1; give up after a few tries with an error. The counter survives pruning, which is why "maximum existing + 1" is not used (after pruning the newest runs it would reuse a number).
   - Retention: after creating a run, remove all runs except the 10 highest `<run>` values (all `k` of a run together). The counter ref is never pruned.
   - `refs/stw/recovery/sync/<run id>/<n>` (the sync journal, 10 runs) stays as it is and is not renamed by this spec. The spike's `refs/stw/recovery/oplog/` is not used by the product.
4. **Create-only check.** gix lets `PreviousValue::MustNotExist` pass when the ref already holds the same value (gix-ref `prepare.rs:126-134`), so the flag alone does not enforce create-only. Before creating a backup ref the code looks the ref up and fails if it exists with any value, as `create_recovery_ref` in `src/sync/journal.rs:493` does (`try_find_reference(...)` then error), and also passes `MustNotExist`.
5. **Reflog.** The reflog entries of the backup ref and of the undone branch must be written even when the repository has `core.logAllRefUpdates=false`. In gix 0.90 that setting makes the ref store run with `WriteReflog::Disable`, and `force_create_reflog: true` on the edit then has no effect. Mechanism: stw opens the ref store handle for these writes with `write_reflog = WriteReflog::Always` (a handle made for the call, not a change of the repository config) and asks for `RefLog::AndReference`; message `stw oplog undo`. **Required test:** a repository with `core.logAllRefUpdates=false` has the reflog entries for the backup ref and the branch after `undo`, with a mutation that removes the `Always` handle. If the mechanism does not work with gix 0.90 as written, this item is replaced by "no reflog guarantee with `logAllRefUpdates=false`; the `refs.log` line of item 7 is the record". UNKNOWN until the test runs.
6. **CAS update.** Update the ref with `old := --expect`, `new := --to` and `PreviousValue::MustExistAndMatch`, no-deref semantics (as `git update-ref --no-deref REF OLD CUR`); a change since the checks fails the update and leaves the backup.
7. stw appends its own `refs.log` lines (context `stw`) for the backup creation, the counter update (`refs/stw/backup-counter`), the retention prune and the undo update. A failed append is reported and does not undo the ref change.
8. The result prints backup ref and the way back as a command.

The same CAS rule applies to every stw ref write: expected old value always given.

## 9. Parser behaviour (summary)

| Input | Behaviour |
|---|---|
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
7. **Backup name.** Owner form `refs/stw/backup/<n>` against this spec's `refs/stw/backup/<run>/<k>` (section 8 item 3). Recommend `<run>/<k>`: retention counts runs, as decided. `<run>` comes from the counter ref `refs/stw/backup-counter` and is never reused. Alternative: the plain form, valid only if each run makes exactly one ref; retention then counts refs.
8. **Markers across rotation.** Recommend joining across files as in section 6; the alternative (markers dropped at the boundary) loses the old value of one delete per rotation.
9. **SHA-256 repositories.** The spec allows 64 digits; nothing was run beyond forged hook input. Recommend a Rust test with `--object-format=sha256`, marked as a deviation from the spike's proof.
10. **Timeline joining of `rewrites.log` and `refs.log`** ("which command"). The spike does not join them. Recommend not joining in v1; `evolog` and `timeline` stay separate commands.
11. **Reading `refs.log` of a repository where the hook was never installed** (stw's own lines only). Recommend: valid; the gaps section states "no hook install record" and marks every hook-covered kind as open.
12. **Reflog with `core.logAllRefUpdates=false`.** Recommend keeping the `WriteReflog::Always` handle (section 8 item 5) only if the required test passes with gix 0.90; otherwise drop the promise. The `refs.log` line is the record either way.
13. **Which gix updates stw logs (package 2).** Recommend all gix ref writes that exist: sync recovery ref create and prune, sync branch fast-forward and its put-back, tidy apply branch delete, tidy restore branch create. Not logged: ref writes through `git` processes (hooks see them when installed; `stw sync` runs its fetch with `core.hooksPath=/dev/null`, so remote-tracking updates stay unseen, `still-open` row) and the object writes. Later packages add undo and backup refs.
14. **`core.logAllRefUpdates=false`.** Recommend writing the `refs.log` line anyway: it is stw's own record and does not depend on reflogs (the gix reflog of the sync writes still follows its own rules). Implemented so.
15. **Truncated after two rotations (ledger N2).** The `rotated` gap line carries the file name in its last field (deviation from 2.1, named there); the reader says `truncated` for a file with two or more such lines. Lines with `-` (hand-made or from a version without the name) count for no file. `errors.log` is never rotated, so the count only grows; a repository that rotated twice long ago still says `truncated`, which stays true (the oldest lines are gone). Alternative: no name in the line and a count over both files (imprecise); not chosen.
16. **A write that fails.** Recommend best effort: the command's result is unchanged; stw writes a `write-failed` gap line (`errors.log`, else `stw-oplog-gaps`) and one notice on stderr (`stw: op log: the line for <ref> was not written: <reason>`), per update. Causes: `stw` or `stw/oplog` is a symbolic link, a log that is not a regular file, a full disk, no permission. The reader counts it as `capture-gap`. A held `rotate.lock` is no failure (no notice, no gap). Alternative: fail the command; rejected because the ref change has been made already and the spec 8 item 7 says a failed append does not undo it.
17. **Time and files.** Time is the system clock, UTC, second resolution (`unknown` is never written by stw). Rotation checks both `refs.log` and `rewrites.log` after every stw append to `refs.log`; no stw command writes `rewrites.log` yet, so its rotation is only reachable when a hook-written file is large. The threshold is the constant 10 MiB, no config key (Q5).
18. **`rust-version`.** `File::try_lock` is stable since Rust 1.89; `Cargo.toml` `rust-version` is raised from 1.88 to 1.89 (the toolchain is 1.99.1 already).
