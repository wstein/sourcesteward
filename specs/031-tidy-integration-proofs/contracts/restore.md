# Restore and history contract

Subordinate to the [specification](../spec.md) FR-042..FR-047 and to Spec 029's [tidy v3 contract](../../029-tidy-dogfood-safety/contracts/tidy-v3.md) (selection, journal phases, recovery namespace, result statuses).

## Commands

```text
stw tidy restore [PATH] --contract 3 --plan ID --state-dir ABS [--action ID ...] [--json]
stw tidy restore [PATH] --contract 3 --plan ID --state-dir ABS [--action ID ...] [--as NEW_NAME] --apply --authorize [--json]
stw tidy restore [PATH] --contract 3 --list --state-dir ABS [--limit N] [--json]
stw tidy history [PATH] --contract 3 --state-dir ABS [--limit N] [--json]
```

`--contract` accepts exactly 3; omission is a usage error. Usage errors exit 2 before any native or store work and are only argument errors: duplicate `--action`, `--as` without exactly one `--action`, `--as` not a valid `refs/heads/` name, `--authorize` without `--apply`, `--apply` without `--authorize`, `--list` with `--plan`, a malformed plan ID or `--limit`. An unknown plan ID or an action not in the journal needs a store read and is a refusal (`plan-not-found`, `action-not-found`; exit 1). Result statuses and exit codes follow tidy v3 (`ok|refused|partial|error`; 0 for `ok`, 2 usage, 1 otherwise); command names are `tidy-restore` and `tidy-history`. Without `--action`, all entries of the plan are in scope. There is no stored restore plan or digest: `--apply` computes the effects at apply time from the journal and the clone, re-evaluates each entry immediately before its CAS and never accepts replacement OIDs or refs. `--authorize` covers exactly those effects. `--state-dir` follows the tidy v3 rules: canonical path, no aliases, private, outside every protected workspace.

Status: plan `ok` when the journal was read (even with no `restorable` entry, the counts say so); plan `refused` for `unsupported-mode`, `journal-unreadable`, `plan-not-found` or `action-not-found`. Apply `ok` when every in-scope entry is `created`, `created-recovered` or `already-present`; `partial` when some entries were created and others ended otherwise; `refused` when nothing was created and at least one in-scope entry was not `already-present`; `error` on an unexpected failure.

## Inputs and checks

1. Load the V3Journal of `ID` from the private state store (bounded like the Spec 029 journal reads). An unknown plan refuses (`plan-not-found`), a malformed journal refuses (`journal-unreadable`) and a journal whose mode is not `local-refs` refuses (`unsupported-mode`: `tracking-refs` and retirement journals), all with nothing written.
2. For each in-scope entry with a confirmed recovery phase (`recovery-confirmed` or later), read `refs/stw/recovery/v3/<plan>/<hash>` by exact name (typed argv, `--`, planning environment of FR-019), require it to be a direct ref (`git symbolic-ref -q` fails) whose target equals the journal OID, and the object to be a readable commit (`git cat-file -e OID^{commit}`). Reasons: `recovery-ref-absent`, `recovery-ref-retargeted`, `recovery-ref-symbolic`, `object-unreadable` (all state `recovery-missing`).
3. Validate the ref name (`git check-ref-format` semantics, prefix `refs/heads/`, at most 4,096 bytes, no control characters) as data; a journal name that fails is that entry `journal-incomplete` with reason `invalid-ref-name`, never an option or path.
4. Read the current value of the target ref without dereferencing: absent, equal OID, other OID, or symbolic (`name-taken` with reason `symbolic-reference`).

## Entry states

| State | Condition | Apply effect |
| --- | --- | --- |
| `restorable` | recovery ref target equals journal OID, commit readable, target ref absent | create ref by CAS |
| `already-present` | target ref exists at the journal OID | none |
| `name-taken` | target ref exists at another OID or is symbolic | none; use `--as` for one action |
| `recovery-missing` | recovery ref absent, symbolic, target differs from the journal OID, or commit unreadable (pruned, re-clone), with the matching reason | none; no reflog guessing |
| `journal-incomplete` | entry has no confirmed recovery, the phase is unknown, or the ref name is invalid | none; never replayed |

States are evaluated in the order `journal-incomplete`, `recovery-missing`, `already-present`, `name-taken`, `restorable`.

## Plan result (JSON, candidate, unpublished)

```json
{"schema_version": 1, "command": "tidy-restore", "status": "ok", "plan_id": "...", "mode": "plan",
 "entries": [{"action_id": "...", "ref": "refs/heads/topic", "oid": "<full OID>",
   "recovery_ref": "refs/stw/recovery/v3/<plan>/<hash>", "state": "restorable", "reason": null,
   "class": "patch-id", "flags": [], "decision": null}],
 "counts": {"restorable": 1, "already-present": 0, "name-taken": 0, "recovery-missing": 0, "journal-incomplete": 0},
 "limits": ["tip-and-history-only", "no-config", "no-reflog", "no-worktrees"]}
```

`decision` is the journal's `{reviewer, kind, decision, reason}` or null; text from the journal is escaped data. Deterministic order (journal order). Apply adds per entry `outcome` and a `restore_journal` record reference and sets `status` `partial` when some entries were created and others refused for `name-taken` or `recovery-missing`; refusing states never turn into an effect.

## Apply effect

Per `restorable` entry, in journal order: re-read the recovery ref and the target ref, then `git update-ref --no-deref -m "stw restore PLAN ACTION" -- REF OID 0000000000000000000000000000000000000000` (zero OID of the repository's hash width) after the write-ahead `intent` record is durable. A lost race gives `name-taken`, a symbolic target `symbolic-reference`; any other failure gives `error` and stops the run (earlier effects reported, no retry with force). Then append the `outcome` record. Reflog and argv: the new branch receives one reflog entry with the message `stw restore PLAN ACTION`, so plan and action IDs (non-secret, at most 128 bytes) appear in argv and in the reflog. The recovery refs, other refs, worktrees, remotes, configuration, index and working files are not touched. Ref creation runs hooks only as configured for the repository; if a `reference-transaction` hook would run, apply refuses naming the class, as Spec 031 FR-022 does for deletion. Exactly the named local effect requires `--authorize`; no other authority is implied.

## State and journal

The `restore` journal kind lives in its own namespace of the private state store (`tidy-v3-restores`, next to `tidy-v3-plans` and `tidy-v3-operations`), under the tidy v3 secure-store primitives; the V3Journal is only read. One record is a write-ahead `intent` (plan ID, action ID, ref, OID, who, when) and, after the CAS, an `outcome` (`created`, `created-recovered`, `name-taken`, `symbolic-reference`, `recovery-missing`, `error`). Records are appended, never rewritten. Entries ending as `already-present`, `name-taken` (at re-evaluation), `recovery-missing` or `journal-incomplete` before the effect write no record. Crash semantics: a crash after the intent and before or after the ref creation leaves an intent without outcome; the rerun re-evaluates from the clone: ref at the journal OID appends `created-recovered`, ref absent retries, another value is `name-taken`. A crash cannot cause an overwrite, only a repeated safe attempt. Known limit: a ref that another actor created at the same OID after the crash is indistinguishable and is reported `created-recovered`; this is harmless (same OID, no data changed). If the store write of the intent fails, no ref is touched.

## History result

`entries[]` of [HistoryEntry](../data-model.md#historyentry), newest first by journal time, at most 256 entries (`--limit N`, ceiling 1,024). The store enumeration scans at most 4,096 journal records for `total`; beyond that the result carries the truncation marker `{unit, shown, total}` with `total` as a lower bound flagged `scan_truncated`. Per journal the reader checks recovery refs by exact name and counts `restorable` with the same bounded reads as the plan (one ref read per entry, at most 1,024 entries per journal, the Spec 029 observation limit) and one batched `git cat-file --batch-check` per history call for all object checks, at most 65,536 input lines and 4 MiB of output; the whole history call has a 30-second budget and each journal read is bounded by the Spec 029 stream limits. Exceeding a bound or the budget marks the affected journals `journal_status: incomplete` and adds a truncation marker, never a silent omission. `journal_status` is `complete`, `incomplete` or `unreadable` (listed with its ID, never skipped silently). Journals of other modes show the mode and `restorable` 0.

## Hostile data

Branch names, decision reasons, reviewer names and plan IDs from the journal are data: escaped and bounded on output, validated before use, never argv options (`--` separates every ref and OID argument), never endpoints or paths. A recovery ref name is derived from plan and hash and compared with the journal; it is never taken from journal text.

## Tests (mutation targets)

Mutants that must each fail a named test (T048): M8 drops the CAS (plain `update-ref` without the zero old value), M9 drops the OID cross-check between journal and recovery ref, M10 allows an overwrite of an existing ref.
