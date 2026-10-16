# Data model: Inventory limits and checkout description

The published result model does not change (inventory schema v1, COMPAT-1). This document defines the internal types and the assembly order that produce it. Values are those of [contracts/inventory-limits.md](contracts/inventory-limits.md); they are referenced by identifier (LIM, FEED, DEG, TXT, GL, CK, SP, QR, FS), not repeated.

## Internal types (package `internal/inspection`, unexported)

- `inventoryBounds`: LIM-3, LIM-4, LIM-7 (entries and path bytes), LIM-8, LIM-9, LIM-10 and LIM-11 as fields, set by `defaultInventoryBounds()`. A session holds one value; limit-row tests construct sessions with small values so they need no large repository (the scale tests of US1 use production values with real repositories).
- `gitStream`: `func(ctx, dir string, stdin io.Reader, delim string, args []string, each func(record []byte) error) (exit int, stopped bool, err error)`. `delim` is `"\x00"` or `"\x00\n"`. `record` is valid only during the call and never longer than LIM-4. Errors: `*streamLimitError{Kind: "scan"|"record"}`; `errStreamStop` returned by `each` ends the command early and surfaces as a nil error with `stopped=true`. Stdout and stderr both count toward LIM-3; the first 4,096 bytes of stderr are kept for messages and the rest discarded. When `stdin` is non-nil a goroutine copies it concurrently with the output read and ends on early stop or a closed pipe. `streamGit` builds the command exactly like `runGit` (same prefix, environment, `WaitDelay`), reads stdout through a pipe in 32 KiB chunks, ends the process by cancelling a derived context on overflow or stop (this kills the direct child; the commands used spawn no other process because filters and helpers are neutralized, and the tests assert none remain), drains the pipe and waits.
- Injected buffered runners (the existing `runner` type used by tests) are adapted into a `gitStream` that runs once and splits the output, so existing interposition tests keep working; the adapter cannot carry standard input, so tests of FEED-3 use the production stream with a recording wrapper around `streamGit`, not the buffered adapter. Production passes `streamGit`.
- `gitInventorySession` gains `stream gitStream` and `bounds inventoryBounds`.
- `inventoryCandidate`: a file record candidate (an `InventoryFile`) retained by the inspected workspace's passes up to LIM-8; `omitted` counts the rest and `lowerBound` is true when counting stopped early (DEG-2, DEG-3).
- `gitInventoryStatus` (existing) keeps only changed, conflicted, unknown and gitlink entries; clean entries are no longer retained. The staged merge (`diff --cached` results folded into index entries by path) is unchanged in result: a staged path with no retained entry gets a new entry `X.`, exactly what the former retained clean entry became.
- `indexOutcome`: `ok`, `tooLarge` (DEG-5a) or `aborted` (DEG-5b) for one index pass; the pass signature is a rolling SHA-256 over the index bytes and per-file observations, and for an aborted pass over the bytes read plus the abort marker (DEG-5a, DEG-5b), so two passes that abort identically still compare equal.
- `gitInventoryRefScan`: all parsed references (up to LIM-9) in `for-each-ref` order and the digest of the stream used by the recheck.

## Assembly order (Git adapter)

1. Filter configuration, worktree registrations (buffered), reference scan (FEED-1) and remote names; DEG-6 on overflow.
2. Per workspace entry: apply CK-1, CK-2 or CK-3, then the existing path, ownership and identity checks on the (possibly substituted) path.
3. Per checked workspace: attribute paths and check (FEED-2, FEED-3) only when raw comparison is requested; two index passes (FEED-4, SP-1, SP-2) with FEED-5 and FEED-6 inside each; per-pass state is the changed-entry list, the signature, the gitlink flag and path set, the `outsideFilter` flag (DEG-13) and the counters for LIM-7, LIM-8, LIM-10 and LIM-11. Only the inspected workspace retains candidates; other workspaces keep the flags needed for `tracked`, `untracked` and `conflicts`. Apply GL-1 to GL-5, DEG-5, DEG-7, DEG-8.
4. For the inspected workspace the changed-entry list becomes candidates (scope matching and filters as today), bounded by LIM-8.
5. Stored comparison (FEED-7) adds candidates under the same rules.
6. Bases and the `clean`/`dirty`/`ref-exists`/`ancestor` predicates are evaluated on the candidate count and the complete reference scan, before any truncation (QR-1).
7. Rechecks: registrations (buffered, string compare) and references (FEED-1, digest compare).
8. Truncation: FS-1 chooses file and reference counts; files are sorted by path then scope and cut; references are ordered local, remote, tag then name and cut at whole references; per-reference ancestry facts (`contains_revision`, `merged_into_base`, `local_only`) are computed only for returned references. DEG-9 fits the encoded size. `partial` and the TXT disclosures are added, subject to QR-1.
9. `checkInventoryLimits` remains the last guard; failure there is the existing `inventory-limit` error.

**Reference predicates** (`--query ref-exists|ancestor`) take the separate early path (`referencePredicate`): it uses the FEED-1 stream and LIM-9 (DEG-6), evaluates the predicate on the complete scan, returns references by FS-1 with `W = 0`, `Bs = 0` and no files, and applies QR-1 (truncation noted in `boundary.limitations`, status unchanged). It reads no workspace, so CK and GL do not apply.

**CK order**: step 2 runs for every entry before the query-scope skip of the existing loop; the skip then compares against the substituted path. A `clean`/`dirty` query is `unknown` (partial) unless the inspected worktree's entry was matched and observed.

## Invariants

- The result validates against the unchanged schema, including its coupling of `status: ok` to `boundary.complete: true` and of any other status to `false`; arrays are arrays, never null.
- `len(files) + len(references) + nested + len(workspaces) + len(bases) <= 1024` for every Git result.
- A truncation disclosure is present if and only if records were omitted. In a plain run it comes with `partial` and `complete=false`; in a query run with a known predicate it comes with `ok`/`true` (QR-1).
- A predicate value is never derived from a returned listing: `dirty`/`clean` use the candidate count, reference predicates the complete scan.
- `tracked: "true"` is produced only from a staged difference or an authoritative ordinary-file difference (a compared, non-skip-worktree, non-gitlink path, no transformation, no unknown tracked path), never from the absence of a file in the Git directory (CK).
- No streamed command runs in a directory other than the working directory of a validated workspace (or, for CK, the `inspect()` of the entry), and no caller path is passed as an argument beginning with `-`: FEED-3 uses standard input; FEED-6 keeps `--literal-pathspecs` and `--`.
- All repository text is data and appears only in existing string fields, JSON-encoded.

## Example

A plain run on a superproject with a modified ordinary file (GL-4) and a linked worktree whose gitlinks leave it unknown (GL-5); `partial` because submodule worktree state is unobserved. Validated against `schemas/v1/inventory.schema.json` on 2026-10-16 with the existing contract test helper (a scratch Go test, removed).

```json
{
  "schema_version": 1,
  "command": "inventory",
  "status": "partial",
  "backend": "git",
  "input_path": "/work/super",
  "identity": {
    "schema_version": 1,
    "status": "ok",
    "backend": "git",
    "input_path": "/work/super",
    "evidence": [],
    "repository": {
      "git_dir": "/work/super/.git",
      "common_dir": "/work/super/.git",
      "worktree": "/work/super",
      "bare": false,
      "linked_worktree": false,
      "object_format": "sha1",
      "head": {
        "state": "attached",
        "ref": "refs/heads/main",
        "oid": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      }
    },
    "diagnostics": []
  },
  "boundary": {
    "complete": false,
    "freshness": "sampled-nontransactional",
    "limitations": [
      "Sampled observations are not transactional; concurrent and ABA changes may be missed.",
      "submodule disk state excluded to avoid recursive helpers"
    ],
    "limits": {
      "deadline_seconds": 10,
      "command_bytes": 65536,
      "result_bytes": 1048576,
      "records": 1024
    }
  },
  "workspaces": [
    {
      "path": "/work/super",
      "name": "/work/super",
      "manager": "git",
      "registration": "present",
      "task_owner": "unknown",
      "commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "change_id": null,
      "operation_id": null,
      "tracked": {
        "value": "true",
        "reason": ""
      },
      "untracked": {
        "value": "false",
        "reason": ""
      },
      "conflicts": {
        "value": "false",
        "reason": ""
      },
      "limitations": [
        "submodule disk state not observed; tracked is true from staged or ordinary-file evidence only"
      ]
    },
    {
      "path": "/work/super-wt",
      "name": "/work/super-wt",
      "manager": "git",
      "registration": "present",
      "task_owner": "unknown",
      "commit_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "change_id": null,
      "operation_id": null,
      "tracked": {
        "value": "unknown",
        "reason": "submodule disk state excluded to avoid recursive helpers"
      },
      "untracked": {
        "value": "false",
        "reason": ""
      },
      "conflicts": {
        "value": "false",
        "reason": ""
      },
      "limitations": [
        "submodule disk state excluded to avoid recursive helpers"
      ]
    }
  ],
  "references": [],
  "bases": [],
  "files": [
    {
      "path": "README.md",
      "old_path": null,
      "scope": "disk",
      "change": "modified",
      "limitations": []
    },
    {
      "path": "README.md",
      "old_path": null,
      "scope": "tracked",
      "change": "modified",
      "limitations": []
    }
  ],
  "query": null,
  "diagnostics": []
}
```
