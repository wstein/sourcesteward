# Contract amendment: Git inventory limits and checkout description

This amends the normative contract of Spec 009 ([inventory.md](../../009-workspace-inventory/contracts/inventory.md)) for the Git adapter only. Everything not stated here, including flags, the result model, the scope semantics, the exit statuses and the jj adapter, is unchanged. The result stays inventory schema v1 (COMPAT-1).

## Invocation and exit status

`stw inventory [PATH] [--json]` with the flags of Spec 009. Exit status is unchanged: 0 for `ok` and for a known predicate (including a known `true` dirty/`false` clean predicate on a result whose only incompleteness is a listing truncation, DEG-1); 1 for `partial`, `error` and an `unknown` predicate; 2 for usage errors.

## Limits (normative)

Constants marked "schema" appear in `boundary.limits` and keep their value (schema v1 pins them with `const`).

| ID | Limit | Value | Meaning |
| --- | --- | --- | --- |
| LIM-1 | Total deadline (schema `deadline_seconds`) | 10 s | unchanged; exceeded gives `inventory-timeout` |
| LIM-2 | Buffered unit (schema `command_bytes`) | 65,536 bytes | the largest unit held at once: the whole output of a buffered command, or one record of a streamed command. It no longer caps how much a streamed command may print |
| LIM-3 | Scan allowance | 32 MiB | stdout plus stderr of one streamed invocation, consumed incrementally and never truncated silently |
| LIM-4 | Streamed record | 65,536 bytes | one delimited record; a longer record stops the command (DEG-5b treatment) |
| LIM-5 | Returned records (schema `records`) | 1,024 | workspaces + references + bases + files + nested targets and upstreams of references, as in Spec 009 |
| LIM-6 | Result bytes (schema `result_bytes`) | 1,048,576 | encoded result, as in Spec 009 |
| LIM-7 | Index read per workspace | 100,000 entries and 16 MiB of path bytes | per streamed `ls-files` read of one workspace |
| LIM-8 | Retained file candidates | 100,000 per run | file records kept in memory before the record share is applied |
| LIM-9 | Scanned references | 20,000 | references parsed and kept before the listing share is applied |
| LIM-10 | Content read per file | 65,536 bytes | unchanged (out of scope) |
| LIM-11 | Content read per workspace pass | 256 MiB | total bytes read for disk comparison in one pass of one workspace |

Process and time bounds that are not constants: every streamed command observes the caller's context and the total deadline; a command that prints nothing and blocks ends with the deadline.

## Git commands (normative allowlist)

The isolation prefix is the existing one, identical for buffered and streamed commands: `--no-replace-objects -c core.fsmonitor=false -c core.untrackedCache=false -c core.hooksPath=/dev/null`, followed by the neutralizing `-c filter.<name>.clean=` / `.process=` / `.required=false` overrides built from the repository configuration; environment `GIT_*` variables inherited from the caller are dropped and `GIT_OPTIONAL_LOCKS=0 GIT_TERMINAL_PROMPT=0 GIT_NO_LAZY_FETCH=1 GIT_PAGER=cat` are set; standard input is empty unless FEED-3. Discovery is by working directory (no explicit `--git-dir`), so Git's own ownership check applies. No Git subcommand is added; `check-attr` changes from path arguments to `--stdin`.

| ID | Command (after the prefix) | Mode | Delimiter | When bound exceeded |
| --- | --- | --- | --- | --- |
| FEED-1 | `for-each-ref --format=%(refname)%00%(objectname)%00%(upstream)%00%(symref)%00%(objecttype)%00%(*objectname)%00%(*objecttype)%00`, read twice (second read compared by digest) | stream | NUL then LF | DEG-6 |
| FEED-2 | `ls-files --cached -z` (attribute paths; only when raw comparison is requested and `core.autocrlf` is not set) | stream | NUL | DEG-5a, DEG-5b |
| FEED-3 | `check-attr --stdin -z filter text eol ident working-tree-encoding` with the FEED-2 paths on standard input | stream, stops at the first deciding value | NUL | DEG-5b |
| FEED-4 | `ls-files --stage -z`, one read per comparison pass | stream | NUL | DEG-5a, DEG-5b |
| FEED-5 | `diff --cached --name-status -z -M --no-ext-diff --no-textconv --ignore-submodules=none --submodule=short --`, one per pass | stream | NUL | DEG-1, DEG-7 |
| FEED-6 | `--literal-pathspecs ls-files --others --exclude-standard -z [--ignored] [-- <literal paths>]`, one per pass | stream, stops at LIM-8 | NUL | DEG-3, DEG-7 |
| FEED-7 | stored delta: `diff-tree --root --no-commit-id --name-status -r -z -M --no-ext-diff --no-textconv <oid>`, or `diff --name-status -z -M --no-ext-diff --no-textconv --ignore-submodules=none --submodule=short <parent> <oid> --` | stream | NUL | DEG-1, DEG-7 |
| BUF | every other command of the existing adapter: `config` reads, `worktree list --porcelain -z` (twice), `remote`, `rev-list --parents -n 1`, `merge-base --is-ancestor`, `rev-parse --verify`, `symbolic-ref`, `config --get`/`--get-all` | buffered at LIM-2 | n/a | DEG-11 |

## Degradation (normative)

`Q` below is the case of a `--query clean|dirty` request whose predicate value is known (not `unknown`) after the degradation.

| ID | Situation | Result |
| --- | --- | --- |
| DEG-1 | File records for the selected scopes exceed the file share (FS-1), or LIM-8 is exceeded | The first records in path order (then scope order) are returned, the rest omitted and counted. Plain inventory: `partial`, `boundary.complete=false`, TXT-1. Case Q: status unchanged, TXT-1 added to `boundary.limitations` only |
| DEG-3 | The untracked (or ignored) listing reaches LIM-8 | The command is stopped; `untracked` is `true` (a record was seen); the listing is treated as DEG-1 with "at least" |
| DEG-4 | Reference records exceed the reference share (FS-1) | References are returned in kind order local, remote, tag and name order within a kind, whole references while their cost fits; the rest omitted. Plain inventory: `partial`, TXT-2. Reference-predicate queries: status unchanged, TXT-2 only in `boundary.limitations`. `ref-exists`, `ancestor`, base candidates, upstream and remote resolution use the complete scanned list |
| DEG-5a | A workspace index exceeds LIM-7 | Disk comparison of that workspace is not performed: `tracked` is `unknown` with TXT-3 (also in its `limitations`); no `tracked`/`disk` file records; for the inspected workspace `partial`. Staged and untracked observations are independent and proceed |
| DEG-5b | FEED-2, FEED-3 or FEED-4 exceeds LIM-3 or LIM-4 | As DEG-5a with TXT-4 (scope `disk comparison`) |
| DEG-6 | References scanned exceed LIM-9, or FEED-1 exceeds LIM-3 or LIM-4 | `error`, code `inventory-limit`, message `reference enumeration exceeds its scan bound`; no partial listing (a reference listing cut at an unknown point could make a predicate lie) |
| DEG-7 | FEED-5, FEED-6 or FEED-7 exceeds LIM-3 or LIM-4 | The observation it feeds is not made: staged and stored comparisons add TXT-4 (scope `staged` or `stored`) and the result is `partial`; for untracked the workspace `untracked` is `unknown` with TXT-4 |
| DEG-8 | LIM-11 reached in a comparison pass | Remaining tracked paths of that pass are `unknown` with TXT-5 (the existing representation of an unobservable tracked path); the workspace `tracked` is `unknown`; `partial` for the inspected workspace |
| DEG-9 | The encoded result would exceed LIM-6 | File records are trimmed from the end, then references from the end of the DEG-4 order, until the result fits; TXT-6; `partial` (case Q: note only). If it cannot fit with no files and no tags, `error` `inventory-limit` as today |
| DEG-10 | Workspaces plus bases alone exceed LIM-5 | `error`, `inventory-limit`, unchanged |
| DEG-11 | A buffered command exceeds LIM-2 | unchanged: `error`, `git-inventory-failed` with that command's message |
| DEG-12 | Deadline or cancellation | unchanged: `inventory-timeout` or `canceled` |

### Record share (FS-1)

Let `W` and `Bs` be the workspace and base records, `B = LIM-5 - W - Bs` (an error under DEG-10 if negative), `F` the number of qualifying file records and `R` the summed cost of all scanned references, where one reference costs `1 + added_targets + removed_targets + upstreams`. If `F + R <= B`, nothing is omitted. Otherwise, if `F <= floor(B/2)` files are returned in full and references get `B - F`; else if `R <= B - floor(B/2)` references are returned in full and files get `B - R`; else files get `floor(B/2)` and references get `B - floor(B/2)`.

## Disclosure text (normative)

`{N}` is a decimal integer, or `at least {N}` when only a lower bound is known (DEG-3, LIM-8). These strings go in `boundary.limitations` and, where stated, in the workspace or file `limitations` or the observation `reason`. Tests assert them verbatim.

| ID | Text |
| --- | --- |
| TXT-1 | `file listing truncated: {N} file records omitted by the 1,024-record bound; omitted paths are not clean` |
| TXT-2 | `reference listing truncated: {N} references omitted by the 1,024-record bound (tags first); predicates use all references` |
| TXT-3 | `index exceeds 100,000 entries or 16 MiB of paths; disk comparison not performed` |
| TXT-4 | `native output exceeded the 32 MiB scan allowance; {scope} not observed` |
| TXT-5 | `content read budget of 256 MiB exhausted; remaining tracked disk content not compared` |
| TXT-6 | `listing trimmed to fit the 1 MiB result bound` |
| TXT-7 | `Git directory listed as workspace; checkout not identified from this worktree` |
| TXT-8 | `submodule disk state not observed; tracked is true from staged or ordinary-file evidence only` |
| TXT-9 | `submodule disk state excluded to avoid recursive helpers` (existing text, unchanged) |

## Gitlink rule (normative)

A workspace has gitlinks when its index contains a mode 160000 entry inside the selected path filter. Staged evidence is the `diff --cached` record of an ordinary path or of a gitlink (FEED-5 reads trees and the index; it never opens a submodule). Authoritative ordinary-file evidence is a disk difference of a non-gitlink path decided by the existing raw comparison when no transformation applies and no tracked path is unknown.

| ID | Gitlinks | Transformation or unknown tracked path | Positive evidence | `tracked` | Workspace limitations |
| --- | --- | --- | --- | --- | --- |
| GL-1 | no | no | yes | `true` | unchanged |
| GL-2 | no | no | no | `false` | unchanged |
| GL-3 | any | yes | any | `unknown` (existing reasons) | unchanged |
| GL-4 | yes | no | yes | `true` | TXT-8 |
| GL-5 | yes | no | no | `unknown`, reason TXT-9 | TXT-9; inspected workspace `partial` only if a gitlink is inside the filter |

Query rule: for `clean`/`dirty` with a gitlink-only incompleteness (GL-5 and no other incompleteness), a file record whose change is not `unknown` makes `dirty` `true` and `clean` `false`; otherwise the predicate is `unknown`.

## Checkout entry (normative)

| ID | Condition | Workspace record |
| --- | --- | --- |
| CK-1 | The `git worktree list` entry equals the common directory, the repository is not bare, the inspected path is not a linked worktree and the identity gives a worktree `W` different from the entry | `path` and `name` are `W`; the existing checks (directory exists, no foreign marker, `inspect(W)` with the same common directory, equal HEAD) apply to `W` |
| CK-2 | The entry equals the common directory, the repository is not bare, and CK-1 does not hold | registration `present`, `tracked`, `untracked` and `conflicts` `unknown` with TXT-7 as reason, TXT-7 in `limitations`, `partial` if the inspected path is that workspace; no process runs against the Git directory as a work tree |
| CK-3 | Any other entry, bare repositories included | unchanged |

## Human output

The text renderer is unchanged; the new disclosures are existing `Limitation:` lines. Exit status and error rendering are unchanged.

## Effects

Read-only as Spec 009. Streamed commands add: bounded memory (one record, 64 KiB, plus the retained candidates of LIM-8/LIM-9); no write, no index refresh, no lock; an early stop ends the process by cancelling its context (SIGKILL of a read-only Git process holding no lock) and waiting for it, so no process outlives the call. Standard input is used only by FEED-3 and carries paths already read from the index. No hook, filter, textconv, external diff, fsmonitor, alias, credential helper, prompt or network access is introduced; the sentinel tests of the verification plan prove it with the large fixtures.

## Compatibility notes (normative)

| ID | Change | Class | Consumer-visible effect |
| --- | --- | --- | --- |
| COMPAT-1 | `schemas/v1/inventory.schema.json` is byte-unchanged; no new property, enum value or diagnostic code; `boundary.limits` constants unchanged | none | Existing validators accept every new result |
| COMPAT-2 | Repositories that returned `error` (`git-inventory-failed`, `inventory-limit`) for size now return `ok` or `partial` | behavior, compatible | A consumer that read `error` as "repository too large" must read `status` and `boundary.complete` |
| COMPAT-3 | `workspaces[].path` and `name` of the main entry of a submodule or separate-Git-directory checkout is the checkout, not the Git directory (CK-1) | value correction within schema v1 | A consumer that matched this entry against `identity.repository.git_dir` or the common directory no longer matches; it now matches `identity.repository.worktree`. The old value was wrong (its `tracked: "true"` was false) |
| COMPAT-4 | `tracked` of a gitlink-bearing workspace can be `true` (GL-4) where it was always `unknown` | behavior, compatible | `unknown` consumers are unaffected; `true` is new evidence |
| COMPAT-5 | A truncated `files` or `references` listing under `partial` where the result was `error` | behavior, compatible | `boundary.limitations` carries TXT-1/TXT-2/TXT-6; consumers must not read a listing as complete when `boundary.complete` is false |
| COMPAT-6 | New texts in `limitations` and `reason` strings (TXT-1 to TXT-8) | additive text | Strings were never enumerated by the schema |
| COMPAT-7 | Documented meaning of `command_bytes` is the buffered unit (LIM-2), no longer "native command output capped at 64 KiB" | documentation | The number is unchanged |
| COMPAT-8 | `Inventory` and `InventoryOptions` Go signatures unchanged | none | `internal/workspace` handoff is unaffected; a larger embedded inventory still falls back to omission under its own bound |
| COMPAT-9 | jj adapter and its limits unchanged | none | jj members keep `jj-output-limit` behavior |

## Spec 023 consequences (recorded here, not applied)

Listed per artifact in [plan.md](../plan.md#spec-023-follow-up).

## Example (partial, truncated listing; validates against the unchanged schema)

See [data-model.md](../data-model.md#example); the example there is validated by the contract test added in WI6.
