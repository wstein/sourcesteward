# Contract amendment: Git inventory limits and checkout description

This amends the normative contract of Spec 009 ([inventory.md](../../009-workspace-inventory/contracts/inventory.md)) for the Git adapter only. Everything not stated here, including flags, the result model, the scope semantics, the exit statuses and the jj adapter, is unchanged. The result stays inventory schema v1 (COMPAT-1).

## Invocation and exit status

`stw inventory [PATH] [--json]` with the flags of Spec 009. Exit status (one rule, unchanged from the CLI): 0 if and only if `status` is `ok` and the query, if any, is not `unknown`; 1 otherwise; 2 for usage errors. Consequently a known predicate exits 0 only when `status` is `ok`, which is why the supporting-listing rule (QR-1) exists.

### Query run rule (QR-1, normative)

A *plain run* has no `--query`. A *query run* has `--query` and, after all degradations, a known predicate value (`true` or `false`). In a query run the `files`, `references` and `workspaces[].limitations` are supporting evidence, not requested observations: a truncation of a supporting listing (DEG-1, DEG-2, DEG-4, DEG-9) leaves `status` and `boundary.complete` unchanged (`ok` and `true`, as the schema couples them) and adds its TXT string to `boundary.limitations`; the predicate is complete, the listing is not claimed complete and the TXT string is the signal. This narrows Spec 009's sentence "never silently truncate and claim complete": the truncation is explicit, in `limitations`, but does not alter `complete` when the predicate is known. A predicate that is `unknown` after degradation makes the run `partial` as before. Predicates `clean` and `dirty` are computed from the candidate count before any truncation (data model, step 6), never from the returned listing, so trimming to zero files cannot turn `dirty` into `false`.

## Limits (normative)

Constants marked "schema" appear in `boundary.limits` and keep their value (schema v1 pins them with `const`).

| ID | Limit | Value | Meaning |
| --- | --- | --- | --- |
| LIM-1 | Total deadline (schema `deadline_seconds`) | 10 s | unchanged; exceeded gives `inventory-timeout` |
| LIM-2 | Buffered unit (schema `command_bytes`) | 65,536 bytes | the largest unit held at once: the whole output of a buffered command, or one record of a streamed command. It no longer caps how much a streamed command may print |
| LIM-3 | Scan allowance | 64 MiB | stdout plus stderr of one streamed invocation, consumed incrementally and never truncated silently |
| LIM-4 | Streamed record | 65,536 bytes | one delimited record; a longer record stops the command (as a scan overrun, with TXT-10) |
| LIM-5 | Returned records (schema `records`) | 1,024 | workspaces + references + bases + files + nested targets and upstreams of references, as in Spec 009 |
| LIM-6 | Result bytes (schema `result_bytes`) | 1,048,576 | encoded result, as in Spec 009 |
| LIM-7 | Index read per workspace | 100,000 entries and 8 MiB of path bytes | per streamed `ls-files` read of one workspace; 8 MiB of paths makes the `check-attr` output (about five path copies plus 109 bytes per path) fit LIM-3 |
| LIM-8 | Retained file candidates | 20,000 records and 8 MiB of path bytes per comparison pass of the inspected workspace | applied identically in both passes; other workspaces keep only the flags needed for their own `tracked`, `untracked` and `conflicts`, never records |
| LIM-9 | Scanned references | 20,000 | references under `refs/heads`, `refs/remotes` and `refs/tags` (the only namespaces the adapter keeps; FEED-1 names them as patterns so other namespaces such as `refs/pull` are not scanned) parsed and kept before the listing share is applied |
| LIM-10 | Content read per file | 16 MiB | raised from 64 KiB (COMPAT-10); content is hashed in chunks and never held whole; the blob header length is `st.Size()` from `Lstat`, the bytes read must equal it and the size, modification time, inode change time (where the platform offers it) and file identity of the opened file and of the path after reading must equal those before, otherwise the existing `tracked content changed during inventory` failure (`changed-repository`); a larger file is `unknown`. A rewrite that preserves size and modification time is seen through the change time unless it falls into the same tick of the filesystem clock; a rewrite within one tick that restores the modification time cannot be detected (sampling, as for the rest of the result) |
| LIM-11 | Content read budget | 256 MiB per run | total bytes read for disk comparison, counted in the first comparison pass of every workspace, the inspected workspace first and the others in `worktree list` order; the second pass of a workspace compares exactly the paths the first pass compared (so the signatures stay comparable) and is not counted again, hence at most twice the budget is read. A workspace reached after exhaustion has its content comparison skipped (DEG-8) |

Time: there is no per-command deadline; every streamed command observes the caller's context and the total deadline (LIM-1), so a command that prints nothing and blocks ends with the deadline (`inventory-timeout`). Memory: retained data is bounded by LIM-7, LIM-8 and LIM-9 plus one 64 KiB record (candidates of the first pass of the inspected workspace, scanned references, the index changes; the second pass keeps only its signature and counters; streams are never retained). Measured peak resident set of the process (linux/arm64, Git 2.56.0): 31 MiB for 100,000 untracked files, 53 MiB for 100,000 modified tracked files, 92 MiB for the same with `--scope stored --scope disk`; the stated bound is below 128 MiB for realistic reference names. Reference-name bytes are bounded only by LIM-3 and LIM-4, so hostile reference names can exceed it; the deadline bounds that case.

## Git commands (normative allowlist)

The isolation prefix is the existing one, identical for buffered and streamed commands: `--no-replace-objects -c core.fsmonitor=false -c core.untrackedCache=false -c core.hooksPath=/dev/null`, followed by the neutralizing `-c filter.<name>.clean=` / `.process=` / `.required=false` overrides built from the repository configuration; environment `GIT_*` variables inherited from the caller are dropped and `GIT_OPTIONAL_LOCKS=0 GIT_TERMINAL_PROMPT=0 GIT_NO_LAZY_FETCH=1 GIT_PAGER=cat` are set; standard input is empty unless FEED-3. Discovery is by working directory (no explicit `--git-dir`), so Git's own ownership check applies. No Git subcommand is added; `check-attr` changes from path arguments to `--stdin`.

| ID | Command (after the prefix) | Mode | Delimiter | When bound exceeded |
| --- | --- | --- | --- | --- |
| FEED-1 | `for-each-ref --format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(objecttype)%00%(*objectname)%00%(*objecttype)%00 refs/heads refs/remotes refs/tags`, read twice (second read compared by digest) | stream | NUL then LF | DEG-6 |
| FEED-2 | `ls-files --cached -z` (attribute paths; only when raw comparison is requested and `core.autocrlf` is not set) | stream | NUL | DEG-5a, DEG-5b |
| FEED-3 | `check-attr --stdin -z filter text eol ident working-tree-encoding` with the FEED-2 paths on standard input, written by a goroutine concurrently with the output read (a writer that is not drained deadlocks: `check-attr` answers each path before reading the next; verified) and stopped on early stop or a closed pipe | stream, stops at the first deciding value | NUL | DEG-5b |
| FEED-4 | `ls-files --stage -t -z`, one read per comparison pass; each record is `TAG mode oid stage<TAB>path` (four head fields, verified on both Git versions): `H` cached, `S` skip-worktree (SP-1), `M` unmerged (only with stage 1 to 3); `M` with stage 0, and any non-`M` tag with a non-zero stage, or any other tag, is `invalid-metadata` (DEG-14) | stream | NUL | DEG-5a, DEG-5b |
| FEED-5 | `diff --cached --name-status -z -M --no-ext-diff --no-textconv --ignore-submodules=none --submodule=short --`, one per pass | stream | NUL | DEG-1, DEG-2, DEG-7 |
| FEED-6 | `--literal-pathspecs ls-files --others --exclude-standard -z [--ignored] [-- <literal paths>]`, one per pass | stream, stops at LIM-8 | NUL | DEG-3, DEG-7 |
| FEED-7 | stored delta: `diff-tree --root --no-commit-id --name-status -r -z -M --no-ext-diff --no-textconv <oid>`, or `diff --name-status -z -M --no-ext-diff --no-textconv --ignore-submodules=none --submodule=short <parent> <oid> --` | stream | NUL | DEG-1, DEG-2, DEG-7 |
| (all) | A streamed record that is not valid UTF-8 or a stream that ends inside a record | | | `error`, code `invalid-metadata`, the messages the buffered parsers give today (DEG-14) |
| BUF | every other command of the existing adapter: `config` reads, `worktree list --porcelain -z` (twice), `remote`, `rev-list --parents -n 1`, `merge-base --is-ancestor`, `rev-parse --verify`, `symbolic-ref`, `config --get`/`--get-all` | buffered at LIM-2 | n/a | DEG-11 |

## Degradation (normative)

Terms: *plain run* and *query run* are defined by QR-1. Wherever a row says "partial" it means `status` `partial` and `boundary.complete=false` for a plain run (and for a query run whose predicate is `unknown`); a query run with a known predicate keeps `ok`/`true` and only adds the TXT string (QR-1).

| ID | Situation | Result |
| --- | --- | --- |
| DEG-1 | File records for the selected scopes exceed the file share (FS-1) | The first records in path order (then scope order) are returned, the rest omitted and counted: partial, TXT-1 |
| DEG-2 | LIM-8 (count or path bytes) is exceeded in a pass | Further candidates are counted, not kept; the listing is DEG-1 with "at least" in TXT-1. Both passes apply the same rule so the pass signatures stay comparable |
| DEG-3 | The untracked listing (not the ignored listing) reaches LIM-8 | The command is stopped; `untracked` is `true` (a record was seen); DEG-2 applies to the listing. An ignored listing reaching LIM-8 is stopped and treated as DEG-2 without any `untracked` statement |
| DEG-4 | Reference records exceed the reference share (FS-1) | References are returned in kind order local, remote, tag and name order within a kind, whole references while their cost fits; the rest omitted: partial, TXT-2. `ref-exists`, `ancestor`, base candidates, upstream and remote resolution use the complete scanned list |
| DEG-5a | A workspace index exceeds LIM-7 | Disk comparison of that workspace is not performed: `tracked` is `unknown` with TXT-3 (also in its `limitations`), `conflicts` is `unknown` with TXT-3, no `tracked`/`disk` file records, partial for the inspected workspace. The pass signature is the digest of the bytes read up to the abort plus the marker `aborted:index-limit`; both passes abort at the same entry. Staged and untracked observations are independent and proceed |
| DEG-5b | FEED-2, FEED-3 or FEED-4 exceeds LIM-3 or LIM-4 | As DEG-5a with TXT-4 (scope `disk comparison`), or TXT-10 for a record overrun, and the marker `aborted:scan` |
| DEG-6 | References scanned exceed LIM-9, or FEED-1 exceeds LIM-3 or LIM-4 | `error`, code `inventory-limit`, message `reference enumeration exceeds its scan bound`; no partial listing (a reference listing cut at an unknown point could make a predicate lie) |
| DEG-7 | FEED-5, FEED-6 or FEED-7 exceeds LIM-3 or LIM-4 | The observation it feeds is not made, and a `clean`/`dirty` query whose selected scope it feeds is `unknown` unless a candidate whose change is not `unknown` exists. FEED-5 (staged): workspace `tracked` is `unknown` with TXT-4 (scope `staged`), partial; `clean`/`dirty` follow the general exception above (a non-`unknown` candidate still gives a known answer, otherwise `unknown`). FEED-7 (stored): TXT-4 (scope `stored`), partial. FEED-6 (untracked): `untracked` is `unknown` with TXT-4 |
| DEG-8 | LIM-11 reached, or a file exceeds LIM-10 | Remaining tracked paths of that workspace pass (or that file) are `unknown` with TXT-5 (TXT-11 for LIM-10) in the existing representation of an unobservable tracked path; the workspace `tracked` is `unknown`; partial for the inspected workspace |
| DEG-9 | The encoded result would exceed LIM-6 | File records are trimmed from the end, then tag references from the end (local and remote references are never trimmed), until the result fits: partial, TXT-6; the records dropped here are added to the TXT-1 and TXT-2 counts. If it cannot fit with no files and no tags, `error` `inventory-limit` as today |
| DEG-10 | Workspaces plus bases alone exceed LIM-5 | `error`, `inventory-limit`, unchanged |
| DEG-11 | A buffered command exceeds LIM-2 | unchanged: `error`, `git-inventory-failed` with that command's message. Note `worktree list` is buffered, so about 400 linked worktrees (not 1,024) already end here; documented, not changed |
| DEG-12 | Deadline or cancellation | unchanged: `inventory-timeout` or `canceled` |
| DEG-13 | `--path` filters | Index entries outside the filters are not retained as records: they set one per-pass flag `outsideFilter`, which gives the existing `unknown` reason "disk path outside requested literal selection" once per workspace, not per entry |
| DEG-14 | A streamed record (including a `check-attr` attribute value) is not valid UTF-8, a configured `filter.<name>.clean|process|required` key is not valid UTF-8 (the filter keys are matched on bytes, not by a locale-dependent pattern), or the stream ends inside a record | `error` `invalid-metadata` with the message the corresponding buffered parser gives today |

### Record share (FS-1)

Files and references are counted after the predicates and facts that need the full lists have been computed (data model, steps 6 and 8). Let `W` and `Bs` be the workspace and base records, `B = LIM-5 - W - Bs` (an error under DEG-10 if negative), `F` the number of qualifying file records and `R` the summed cost of all scanned references, where one reference costs `1 + added_targets + removed_targets + upstreams`. If `F + R <= B`, nothing is omitted. Otherwise, if `F <= floor(B/2)` files are returned in full and references get `B - F`; else if `R <= B - floor(B/2)` references are returned in full and files get `B - R`; else files get `floor(B/2)` and references get `B - floor(B/2)`.

## Disclosure text (normative)

`{N}` is a decimal integer, or `at least {N}` when only a lower bound is known (DEG-3, LIM-8). These strings go in `boundary.limitations` and, where stated, in the workspace or file `limitations` or the observation `reason`. Tests assert them verbatim.

| ID | Text |
| --- | --- |
| TXT-1 | `file listing truncated: {N} file records omitted by the 1,024-record bound; omitted paths are not clean` |
| TXT-2 | `reference listing truncated: {N} references omitted by the 1,024-record bound (tags first); predicates use all references` |
| TXT-3 | `index exceeds 100,000 entries or 8 MiB of paths; disk comparison not performed` |
| TXT-4 | `native output exceeded the 64 MiB scan allowance; {scope} not observed` (`{scope}` is one of `disk comparison`, `staged`, `untracked`, `ignored`, `stored`) |
| TXT-5 | `content read budget of 256 MiB exhausted; remaining tracked disk content not compared` |
| TXT-6 | `listing trimmed to fit the 1 MiB result bound` |
| TXT-7 | `Git directory listed as workspace; checkout not identified from this worktree` |
| TXT-8 | `submodule disk state not observed; tracked is true from staged or ordinary-file evidence only` |
| TXT-9 | `submodule disk state excluded to avoid recursive helpers` (existing text, unchanged) |
| TXT-10 | `native output record exceeded 64 KiB; {scope} not observed` |
| TXT-11 | `tracked disk content exceeds 16 MiB per-file observation limit` (replaces the 64 KiB text) |
| TXT-12 | `skip-worktree entries not compared` (workspace `limitations`, SP-1) |
| TXT-13 | `core.symlinks is false; symlink entries not compared` (SP-2) |

## Gitlink rule (normative)

A workspace has gitlinks when its index contains a mode 160000 entry inside the selected path filter. Staged evidence is the `diff --cached` record of an ordinary path or of a gitlink (FEED-5 reads trees and the index; it never opens a submodule). Authoritative ordinary-file evidence is a disk difference of a non-gitlink path decided by the existing raw comparison when no transformation applies and no tracked path is unknown.

| ID | Gitlinks | Transformation or unknown tracked path | Positive evidence | `tracked` | Workspace limitations |
| --- | --- | --- | --- | --- | --- |
| GL-1 | no | no | yes | `true` | unchanged |
| GL-2 | no | no | no | `false` | unchanged |
| GL-3 | any | yes | any | `unknown` (existing reasons) | unchanged |
| GL-4 | yes | no | yes | `true` | TXT-8; the inspected workspace is still `partial` in a plain run with TXT-9 in `boundary.limitations`, because submodule worktree state is unobserved and absent from `files` |
| GL-5 | yes | no | no | `unknown`, reason TXT-9 | TXT-9; inspected workspace `partial` only if a gitlink is inside the filter |

Query rule (GL-4 and GL-5): for `clean`/`dirty` with a gitlink-only incompleteness, a candidate whose change is not `unknown` makes `dirty` `true` and `clean` `false` and the run is a query run with a known predicate (QR-1: `ok`, exit 0); otherwise the predicate is `unknown`.

### Sparse and symlink entries (normative)

| ID | Condition | Rule |
| --- | --- | --- |
| SP-1 | FEED-4 tag `S` (skip-worktree) | If the path is absent on disk (`Lstat` not found) the entry is not compared and produces no record (Git treats it as clean; the current code reports such entries as deleted, measured on a sparse checkout) and TXT-12 is added to the workspace `limitations`. If the path is present it is compared like any other entry (a modified skip-worktree file is a real edit that removing the worktree would lose; measured by the reviewer) |
| SP-2 | `core.symlinks=false` and a mode 120000 entry | The entry is not compared (it is a regular file on disk); `tracked` is `unknown` with TXT-13 |

## Checkout entry (normative)

The adapter inspects the entry path with the existing `inspect()`. Measured on Git 2.56.0 and 2.47.3 (reviewers): from a submodule's Git directory it reports the checkout as `repository.worktree`; for a linked worktree of a submodule it reports the main checkout; for a `--separate-git-dir` repository `rev-parse --show-toplevel` **fails** from the Git directory (no `core.worktree`), so the checkout is known only from the inspected identity; a `core.worktree` pointing at another repository or at a missing path is **returned** by `rev-parse --show-toplevel` from the Git directory, so the candidate `W` must pass the existing checks below before it is used.

CK is applied while the workspace list is built, **before** the query-scope skip of entries other than the inspected worktree; a `clean`/`dirty` query whose selected scopes need disk or index observation is `unknown` (partial) unless the entry of the inspected worktree was matched and observed (stored-only queries, including on bare repositories, are unaffected; today every submodule or separate-Git-directory checkout is skipped by that rule and a query returns `true` with nothing observed).

| ID | Condition | Workspace record |
| --- | --- | --- |
| CK-1 | The `git worktree list` entry equals the common directory and the repository is not bare, and a candidate `W` different from the entry exists: `W` is `repository.worktree` of `inspect(entry)` when that is `ok`, Git, non-linked, with the same common directory, or otherwise the inspected identity's `repository.worktree` when the inspected identity is non-linked with the same common directory | `path` and `name` are `W` if `W` passes the existing checks (directory exists, no foreign marker, `inspect(W)` with the same common directory, equal HEAD); otherwise CK-2 |
| CK-2 | The entry equals the common directory, the repository is not bare, and CK-1 does not hold or its checks fail (no candidate, a hostile or vanished `core.worktree`) | registration `present`, `path` and `name` stay the Git directory, `tracked`, `untracked` and `conflicts` `unknown` with TXT-7 as reason, TXT-7 in `limitations`; `partial` in a plain run whenever CK-2 applies to any entry; `W` is not printed by the workspace record or read beyond the existing checks (the inspected identity may itself contain a hostile worktree path, which is data in `identity`); no `ls-files`, `diff` or content read is made against `W` or the Git directory for it |
| CK-3 | Any other entry, bare repositories included | unchanged |

## Human output

The text renderer is unchanged; the new disclosures are existing `Limitation:` lines. Exit status and error rendering are unchanged.

## Effects

Read-only as Spec 009. Streamed commands add: bounded memory (one record, 64 KiB, plus the retained candidates of LIM-8/LIM-9); no write, no index refresh, no lock; an early stop ends the process by cancelling its context (SIGKILL of the direct child, a read-only Git process holding no lock; verified for the five streamed read commands, no lock and an unchanged index) and waiting for it; the commands spawn no other process because filters and helpers are neutralized, and the tests assert that none of the test's own descendants remain. Standard input is used only by FEED-3 and carries paths already read from the index. No hook, filter, textconv, external diff, fsmonitor, alias, credential helper, prompt or network access is introduced; the sentinel tests of the verification plan prove it with the large fixtures.

## Compatibility notes (normative)

| ID | Change | Class | Consumer-visible effect |
| --- | --- | --- | --- |
| COMPAT-1 | `schemas/v1/inventory.schema.json` is byte-unchanged; no new property, enum value or diagnostic code; `boundary.limits` constants unchanged | none | Existing validators accept every new result |
| COMPAT-2 | Repositories that returned `error` (`git-inventory-failed`, `inventory-limit`) for size now return `ok` or `partial` | behavior, compatible | A consumer that read `error` as "repository too large" must read `status` and `boundary.complete` |
| COMPAT-3 | `workspaces[].path` and `name` of the main entry of a submodule or separate-Git-directory checkout is the checkout, not the Git directory (CK-1) | value correction within schema v1 | A consumer that matched this entry against `identity.repository.git_dir` or the common directory no longer matches; it now matches `identity.repository.worktree`. The old value was wrong (its `tracked: "true"` was false) |
| COMPAT-4 | `tracked` of a gitlink-bearing workspace can be `true` (GL-4) where it was always `unknown` | behavior, compatible | `unknown` consumers are unaffected; `true` is new evidence |
| COMPAT-5 | A truncated `files` or `references` listing under `partial` where the result was `error` | behavior, compatible | `boundary.limitations` carries TXT-1/TXT-2/TXT-6; consumers must not read a listing as complete when `boundary.complete` is false |
| COMPAT-6 | New texts in `limitations` and `reason` strings (TXT-1 to TXT-13) | additive text | Strings were never enumerated by the schema |
| COMPAT-7 | Documented meaning of `command_bytes` is the buffered unit (LIM-2), no longer "native command output capped at 64 KiB" | documentation | The number is unchanged |
| COMPAT-8 | `Inventory` and `InventoryOptions` Go signatures unchanged; but `internal/workspace` counts the embedded inventory's records against its own 1,024 `observationLimit` (`resultRecordCount`) | behavior | FS-1 can now fill an inventory to about 1,023 records, so a handoff that embeds such an inventory takes its existing fallback (`Inventory omitted because the result bound was exceeded`, partial `result-limit`); the handoff code is unchanged and a test pins the fallback |
| COMPAT-9 | jj adapter and its limits unchanged | none | jj members keep `jj-output-limit` behavior |
| COMPAT-10 | Per-file content read raised from 64 KiB to 16 MiB (LIM-10), bounded by LIM-11; the reason text for a larger file changes (TXT-11) | behavior, compatible | Files between 64 KiB and 16 MiB now give known `tracked` values instead of `unknown`; docs/inventory.md's "capped at 64 KiB per file" sentence is amended |
| COMPAT-11 | Skip-worktree entries are no longer reported as deleted (SP-1); entries under `core.symlinks=false` are `unknown` (SP-2) | value correction | Sparse checkouts stop reporting hundreds of false `deleted` records |
| COMPAT-12 | QR-1: a query run with a known predicate stays `ok`/`complete=true` when a supporting listing is truncated | behavior, narrowing a Spec 009 sentence | The truncation is in `boundary.limitations` only |
| COMPAT-13 | A query run on a submodule or separate-Git-directory checkout (CK-1) | value correction | Such a run used to be skipped for the workspace (the entry was the Git directory): it listed no files, reported registration `unknown` and `tracked`/`untracked`/`conflicts` `unknown`, and `--query clean` answered `true` and `--query dirty` `false` with nothing observed. It now observes the checkout: files are listed, registration is `present`, the observations are known and the predicates reflect the files |
| COMPAT-14 | CK-2 entry (checkout not identified): registration `present`, manager `git`, observations `unknown` with TXT-7, `partial` | value change | Previously the entry took the registration and manager of a failed inspection of the Git directory |
| COMPAT-15 | A linked worktree of a bare repository: the bare registration entry is reported as `present` with the limitation `bare repository has no disk checkout`; the run is `ok` | behavior, error to ok | `main` ended with `changed-repository` (the bare entry's HEAD was compared with the empty registration HEAD) |
| COMPAT-16 | `boundary.limitations` order: new notes (TXT-1, TXT-2, TXT-6, TXT-9 for soft reasons) are appended after the base-candidate note | order | Consumers must not depend on the position of a limitation string |

## Spec 023 consequences (applied by the implementation, WI2, WI3, WI7)

Listed per artifact in [plan.md](../plan.md#spec-023-follow-up).

## Example (partial, truncated listing; validates against the unchanged schema)

See [data-model.md](../data-model.md#example); the example there (a plain run, `partial`) is validated by the contract test of T013 (WI5).
