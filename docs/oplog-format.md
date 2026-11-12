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
| `errors.log` (fallback `<common-dir>/stw-oplog-gaps`) | hooks | capture gaps |
| `events-refhook.log`, `events.log` | install/uninstall | `<UTC time>\tinstall|uninstall` |
| `config` | owner | `remotes=on|off`, read up to 1 KB, never executed |

Two logs stay separate (owner decision). Retention of `.log` files: section 6.

## 2. Common rules

- UTF-8 (in fact ASCII), LF terminated, TAB separated, append only. One `write` per line on a file opened `O_APPEND`; a line is far below `PIPE_BUF` (at most about 1.4 KB), so concurrent writers do not tear lines.
- A line is never rewritten or deleted except by rotation (section 6).
- Time: UTC ISO 8601 with `Z` (`2026-11-12T00:42:26Z`), second resolution, or the word `unknown` (hook without a working `date`).
- Hashes: 40 or 64 lowercase hex digits (SHA-1 or SHA-256). The zero id is `0` repeated to the hash length. Any other value in a hash field makes the line `bad`.
- Free text from git or users is never written, except encoded names (section 3.2).

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
| `worktree` | `main` or `worktrees/<id>` (id cut to 40 characters) of the writing process, not necessarily of the ref; `-` for markers |

### 3.2 Encoding of names

Every byte outside `[A-Za-z0-9._/+@:-]` becomes `%XX` (upper-case hex, byte-wise, so `%` is `%25`, `ä` is `%C3%A4`). A name longer than 200 bytes is cut at 200 bytes and ends in `%00` followed by 8 hex digits
(djb2 of the whole unencoded name; NUL cannot occur in a ref name, so the suffix is unambiguous). A cut may split a UTF-8 sequence; harmless because bytes are encoded one by one.
Readers decode for comparison with reflog names and MUST replace control characters on display. The Rust writer MUST produce the same output as the hook for the same name (djb2 over the bytes, 32-bit unsigned, 8 lowercase hex digits; the test corpus of the spike is the reference).

### 3.3 Contexts

| `context` | Written by | Meaning |
|---|---|---|
| `ref-transaction` | hook | a ref change as git committed it |
| `pre-old` | hook (phase `prepared`) | marker: `old` = value the ref has now, `new` = zeros, worktree `-`. Not a ref change. The reader joins it to the next `ref-transaction` line of the same ref with `old` = zeros: delete `old -> 0`, or a move without expected old value `marker -> new`. A marker without a following line is an aborted transaction: ignored |
| `pre-same` | hook | marker: current value equals the new value (packing, no-op). The reader drops it together with the next `0 -> value` line of that ref |
| `stw` | stw | a ref update stw wrote itself (gix writes: sync recovery refs, fast-forward, undo, backup refs). `old` is the real old value from `PreviousValue`, never a marker. The command is not in the line; see Q3 |

Readers MUST ignore unknown contexts and lines with fewer than 5 fields (counted as `bad`, shown in the gaps section). A line with a good shape but unknown context is kept in JSON output (`context` verbatim, encoded) and skipped in the timeline.
The second `0 0` delete line of an already deleted ref (git runs two transactions for a packed-only tag) is dropped by the reader. `HEAD` lines that repeat a branch line of the same transaction are not written by the hook.

### 3.4 What is logged

Only `HEAD` and `refs/*`. `refs/remotes/**` only with `remotes=on` (default off; stw's own writes to `refs/remotes/**` follow the same switch). Not logged: pseudo refs (`ORIG_HEAD`, `AUTO_MERGE`, ...), `old = new`, the pruning of loose refs by `pack-refs`/`gc`.
Per-worktree refs `refs/bisect/*`, `refs/worktree/*`, `refs/rewritten/*` get no `pre-old` marker (a delete shows `old value unknown`).

### 3.5 Hook phase rules (contract for the hook text)

The hook always exits 0 in every phase; `prepared` does at most one lookup per ref reported with old = 0 (loose ref, else `packed-refs`); `aborted` and unknown phases do nothing. A write error goes to `errors.log`, then `stw-oplog-gaps`, else nowhere (the transaction goes on).
The hook is a `sh` script embedded in the stw binary (`print`), header line 2 `# stw-oplog-refhook v1`; it does not call stw at run time.

## 4. `rewrites.log`

Exactly 6 TAB fields:

    <time> <amend|rebase|other> <old sha> <new sha> <extra> <branch|detached>

`extra` and `branch` are reduced to `[A-Za-z0-9._/+@-]` (other bytes become `?`); `extra` may be empty. The kind written by stw itself is `other` (Q3 proposes a fixed word `stw`).
Readers ignore lines with fewer than 4 fields and accept unknown kinds. `(old, new)` is one edge; for duplicate pairs the first line wins. Self pairs are ignored. Several old commits with the same new commit = merge (fixup/squash); one old with several successors = split; cycles stop the chain. These semantics are those of `evolog` in the spike.

## 5. Version field

The line formats have no per-line version. Version lives in:

1. the hook header line 2 (`# stw-oplog-refhook v1`, `# stw-oplog-hook v1`), which is how install/uninstall recognise their own file;
2. the file name: a change of the field count or meaning of a field means new file names (`refs2.log`, `rewrites2.log`), never a silent change of the old files;
3. JSON output of the readers: `schema_version` (the product starts at 1 for the oplog JSON, independent of other stw schemas).

Adding a new `context` word or `rewrites.log` kind is not a format change (readers ignore unknown words). Old hooks keep writing the old files after a product upgrade; the reader reads both generations if present (Q6).

## 6. Rotation and retention

- `refs.log` and `rewrites.log` rotate by size: when a file is larger than 10 MiB at the moment of a write by stw, stw renames it to `<name>.1` (replacing an older `.1`) and starts a new file; exactly one old generation. No silent deletion otherwise; no time-based expiry.
- The hook shell scripts do not rotate (cost and risk in `prepared`/`committed`). Rotation is done by the next stw command that writes or reads with write access (`stw oplog` commands), under a lock file `rotate.lock` (create-new, stale after 60 s). A rename is atomic; a hook that opened the old file for append keeps writing to `.1` for that one line (acceptable).
- A rotation is noted in `errors.log` as a gap line `<time> gap rotated failed=0 bad=0 -`.
- Readers read `<name>.1` then `<name>`, so a rotation does not cut the timeline; the oldest part is lost after the second rotation (stated in the gaps section as `truncated`).
- `pre-old`/`pre-same` markers across a rotation: a marker at the end of `.1` and its line at the start of the new file are still joined (the reader treats the two files as one stream).
- Backup refs `refs/stw/backup/<n>` (section 8) are separate: retention 10 runs.

## 7. Timeline model

The timeline merges reflog rows (gix reflog iterator, origin `reflog`) with `refs.log` rows (origin `hook` or `stw`), sorted by time, then by file order. Row fields: `time`, `ref`, `old`, `new`, `origin`, `text`, `flag`.

- A ref change is `covered` when a log line exists for the same (ref, new value) within the hook window (after `install`, before `uninstall`); a reflog row without it inside the window is `uncovered`. Comparison by (ref, new value); the same value twice counts as covered once (known nit of the spike).
- Grouping is always labelled `suspected` (`"group_basis":"suspected"`): same text stem and time proximity. It is a display aid and never proof of one operation. Undo plans refer to one explicitly named ref change, never to a group.
- Gaps section always printed: `capture-gap`, `uncovered`, `pre-install`, `truncated`, `still open` (clone, `-c core.hooksPath`, other tools), as in the spikes.
- JSON: `schema_version`, `entries[]`, `gaps{}`. Output goes through `escape_untrusted`; no log text is trusted.

## 8. Undo, compare-and-swap and backup namespace

Undo of a ref (one named change, not a full undo: index, worktree, other worktrees, pushes and running operations are not covered):

1. `undo-plan` writes nothing and prints all checks; `undo` needs `--to OLD`, `--expect NEW` (full current value) and `--yes`.
2. Checks: the ref is `refs/heads/*` (other types: plan only); not symbolic; not checked out in any worktree (otherwise refuse and hint `git reset --keep`; a `--keep` path is a later own package); old value is a commit and exists; the target is not the creation entry.
3. Backup: create `refs/stw/backup/<n>` on the current value, create-only (`PreviousValue::MustNotExist`, reflog forced). `<n>` is a monotonically increasing integer, one namespace for all stw safety copies (undo and slim M5); retention 10, the oldest are removed when an 11th is made. This replaces the spike's `refs/stw/recovery/oplog/` (no second namespace).
4. CAS: update the ref `old := --expect`, `new := --to` with `PreviousValue::MustExistAndMatch` and no-deref semantics (equivalent to `git update-ref --no-deref REF OLD CUR`); a change since the checks fails the update and leaves the backup. The reflog message is `stw oplog undo`, logged even with `core.logAllRefUpdates=false`.
5. stw appends its own `refs.log` lines (context `stw`) for the backup creation, the retention prune and the undo update, before reporting success. A failed append is reported, does not undo the ref change.
6. The result prints backup ref and the way back as a command.

The same CAS rule applies to every stw ref write: expected old value always given.

## 9. Parser behaviour (summary)

| Input | Behaviour |
|---|---|
| fewer fields than the minimum (5 refs, 4 rewrites) | skipped, counted as `bad` |
| more than 6 fields | the first 6 are read, rest ignored (forward compatibility) |
| `old`/`new` not hash or `ref:`, bad time | counted as `bad`, skipped |
| unknown `context` or kind | kept in JSON, skipped in timeline/chain |
| missing file | empty log; the gaps section says "no install record" |
| file not regular (FIFO, device) | not read, gap `unreadable` |
| huge line (over 4 KB) | skipped as `bad`, stream continues |
| trailing line without LF | read (a hook may be killed mid-write), marked `truncated` if shorter than 6 fields |
| symlink as log or directory | refused, gap `unreadable` |
| NUL byte | line skipped as `bad` |

Parsing is by bytes, not lossy UTF-8 conversion; names are decoded from `%XX` only for comparison.

## 10. Open questions (with recommendation)

1. **Context word for stw's writes.** Recommend a single word `stw` in `refs.log`; the origin column of the timeline then reads `stw`. Alternative: one word per command (`stw-sync`, `stw-undo`): more detail, but the field becomes a vocabulary the hook readers must know. Recommend `stw` and put the command in the reflog message.
2. **Command name in the line.** The format has no field for "which stw command". Recommend: no 7th field (format stays frozen at 6); the recovery/backup ref and the reflog message carry it.
3. **`rewrites.log` kind for stw.** Recommend a new fixed word `stw` (instead of `other`) so that stw's own rewrites are distinguishable; allowed because unknown kinds are accepted by readers.
4. **Rotation by hook or by stw only.** Recommend stw only (section 6): the hook stays at one `printf >>`. Price: a repository that never runs stw grows unbounded until the next `stw` command; acceptable (10 MiB is about 80 000 lines).
5. **Rotation threshold.** 10 MiB per file and one generation as decided. Recommend keeping it config-free for the first version; `config` key `max_bytes` later if needed.
6. **Reading both file generations.** After a format change (new file names) the product reads old and new. Recommend: not implemented for v1 (no second format exists yet); keep the rule in this spec only.
7. **Backup numbering.** `<n>` per repository, never reused (counter = max existing + 1, scanned from refs; after pruning the maximum survives because the newest is kept). Recommend this over timestamps (shorter, sortable, matches the sync journals' counter). Race between two stw processes is caught by `MustNotExist` (retry once with n+1).
8. **Markers across rotation.** Recommend joining across files as in section 6; the alternative (markers dropped at the boundary) loses the old value of one delete per rotation.
9. **SHA-256 repositories.** The spec allows 64 digits; nothing was run beyond forged hook input. Recommend a Rust test with `--object-format=sha256`, marked as a deviation from the spike's proof.
10. **Timeline joining of `rewrites.log` and `refs.log`** ("which command"). The spike does not join them. Recommend not joining in v1; `evolog` and `timeline` stay separate commands.
11. **Reading `refs.log` of a repository where the hook was never installed** (stw's own lines only). Recommend: valid; the gaps section states "no hook install record" and marks every hook-covered kind as open.
