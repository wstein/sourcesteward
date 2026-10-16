# Pre-implementation analysis

## Usefulness assessment

Written after round 4 (independent reviewer on revision 1bdcb49, real `stw` built in a scratch directory, Git 2.56.0; jj not run). The maintainer decides; no scope change has been applied.

**Walk-through of the motivating case.** Fixture: an umbrella superproject with two initialized submodules and a linked worktree, a repository with a dirty file and a dirty linked worktree, a repository with a stale linked worktree. Five places are dirty (both submodules, the umbrella's linked worktree, the plain repository and its linked worktree). `stw repos umbrella dirtyrepo stalerepo --submodules` would report: umbrella `partial` ("submodule disk state excluded to avoid recursive helpers", `dirty` unknown, the linked worktree's edit invisible); `dirtyrepo` `ok`, `dirty: true` (its dirty linked worktree appears in no fact); `stalerepo` `partial` (stale 1); both submodules `ok` with `dirty` unknown (the inventory's only workspace record is the submodule's Git directory and already says `tracked: "true"` for a clean submodule, an existing defect of `stw inventory` on submodule checkouts); overall `partial`, exit 1. The summary reads ok 3, partial 2, dirty true 1, false 1, unknown 3, stale 1: it finds 1 of the 5 dirty places. Linked worktrees listed explicitly become `duplicate-repository` errors. At Anchorage scale (inferred, not run) any member with roughly 150 to 400 tracked files fails with `git-inventory-failed` because the one `check-attr` call exceeds the runner's 64 KiB output cap, so most real members would be `error` with no facts, and the exit status would always be 1.

**What one run adds today:** a single envelope in which a failed member cannot be lost, duplicate and containment detection, automatic listing of submodule paths, and stale/foreign totals (already inside each inventory). It does not answer "what is dirty" or "what is ahead/behind" for this case.

**Recommendation: do a small prerequisite first, and reduce scope.** (1) Prerequisite, a follow-up to Spec 009, in priority order: (a) bound or batch the `check-attr` read so repositories of a few hundred files are not `error` (without this most real members have no facts); (b) for a submodule checkout, describe the checkout rather than its Git directory (fixes today's false `tracked: "true"`); (c) for a gitlink-bearing superproject report `tracked: true` when ordinary files changed and keep `unknown` only where `false` cannot be proven. (2) Reduce 023: the `--submodules` half (US3, FR-006, FR-008 discovery, FR-009, WI3, the `skipped` status, the `discovery` fields, `path-unsafe`) adds a hostile-path surface and a third of the spec and currently yields `dirty` unknown for every submodule; Spec 021's `stw pins` already answers the submodule half better (per-submodule dirty state from index metadata, checkout ahead/behind versus the pin, publication, uninitialized submodules, linked-worktree superprojects) but not the superproject's own state, standalone repositories, worktree registrations or jj. Combined use would be `stw repos <roots>` plus `stw pins <umbrella>`. (3) Cheapest change inside 023 without touching inventory: derive `workspaces.dirty` and `workspaces.dirty_unknown` over every non-missing workspace record (excluding submodule entries), list dirty workspaces among the attention workspaces, treat a root `files` record as positive dirty evidence when `tracked` is unknown, and optionally treat an explicitly listed linked worktree as an alias of the kept entry instead of an error. Shipping as is would be honest but would mostly return `partial`, `unknown` and `error` on a realistic workspace.

## Round 0

Author draft, 2026-10-16. Not independent. Independent review rounds are recorded below as they complete.

## Round 1

Independent reviewer: read-only Opus review, revision c19e73f64da908b34b4e6743a36ba3ecc37b0ec3, 2026-10-16, with scratch repositories on Git 2.56.0 (linux/arm64) and a `stw` binary built by the reviewer in a scratch directory; jj not available. Verdict: NOT READY (1 critical, 7 high, 11 medium, 10 low). The author re-verified finding 1 (`git worktree list` inside a submodule prints its Git directory, `core.worktree` set) and the code reading behind findings 2 and 3; the other measurements are the reviewer's and are marked so in research Decision 12.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Critical | Submodule inventory describes the Git directory; root-workspace rule never matches, loosening it reports clean submodules dirty | Facts: no root match gives `dirty` unknown with reason; US3-6; entry `notes`; research Decision 12; T011, T013 fixtures; follow-up inventory fix is a separate spec (plan open question 5) |
| 2 | High | jj members can never be `ok` with default scopes | FR-002: jj uses `stored` scope, chosen by backend after identity inspection; contract capability table; unverified without jj (research Decision 12) |
| 3 | High | Superprojects with gitlinks and bare repositories always `partial`; contract example impossible | Inherited-limits section; exit table wording; example regenerated and both embedded inventories validated against the real schema |
| 4 | High | Inventory limits make realistic members `error` | Inherited-limits disclosure; SC-001 and T013 fixture above 1,024 files; open question 5 |
| 5 | High | `partial` entry has no code | New `inventory-partial` |
| 6 | High | Overall `error` rule contradicted itself | One rule in FR-012 and the data model (no entry with a usable inventory); T003 tests |
| 7 | High | Discovery needed an unspecified identity call; Git discovery before ownership known | Pipeline S1-S7 (plan DD2): `Inspect` first, discovery gated on Git ownership; contract Effects list all processes |
| 8 | High | Foreign rule always 0 for Git | Foreign = manager differs and registration not `missing`; Git fixture in T011 |
| 9 | Medium | Timeouts surface under other codes | Timeout mapping and T010 |
| 10 | Medium | Summary depended on output budget | Sum over entries with non-null `facts` |
| 11 | Medium | Facts and embedding rules inconsistent | One table of `inventory`/`facts` per status; data-model invariants |
| 12 | Medium | Invalid paths handled two ways | Structural violations are usage errors; missing or non-directory paths are `invalid-path`; non-UTF-8 discovered paths `path-unsafe` with `path_encoding` |
| 13 | Medium | Explicit/discovered overlap handled two ways | Not added, counted in `discovery.deduplicated`, not an error |
| 14 | Medium | Per-path gitlink check inexact and unbounded | Exact path and mode compare, overflow means `ignored`, 64 candidate cap |
| 15 | Medium | Discovered-path encoding, last value, duplicates | FR-008, FR-009 |
| 16 | Medium | Two quickstart cases wrong | Quickstart corrected |
| 17 | Medium | Research claimed symlink blob exits 128 | Corrected; crafted-blob fixture mandatory (T007) |
| 18 | Medium | Missing tests | T005, T007, T010, T011, T013, T016 extended |
| 19 | Medium | Error precedence undefined | Precedence list with `notes` |
| 20-29 | Low | `gitmodules_bytes` via `ls-tree -l`; `by_backend.other`; nearest `contained_in`, component prefixes; root compared with identity and TOCTOU disclosed; 60 s message override; attention workspace defined and text bounded; task tags; foreign-owner method; duplicate rationale; HEAD vs index disclosed | Applied in spec, contract, data model and tasks |

## Round 2

Independent reviewer: read-only Opus review, revision c75bc7269ad5563e94d3a9d8923261915c68f06b, 2026-10-16, Git 2.56.0, with a `stw` binary built by the reviewer in a scratch directory; jj not available. Verdict: NOT READY (2 high, 8 medium, 11 low). All round-1 resolutions were confirmed applied; the findings below are new or refine them.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | No table row for an entry whose inventory ran and returned `error`; `inventory-limit` had two meanings | Spec Entry status now has an inventory/facts table per case (error inventory embedded, facts null); omission by the 8 MiB bound is the new code `output-limit`; `inventories_omitted` counts it; T012 |
| 2 | High | `Inventory` re-checks only against its own first inspection, so a swap after the identity checks was undetected | Plan DD3 and FR-008: after every inventory its identity is compared with the earlier identity; mismatch is `changed-repository`, inventory discarded; T010 real-repository test |
| 3 | Medium | Discovery ran before root and duplicate checks; subdirectory-relative `ls-tree` | FR-006 and plan DD2: discovery only for validated Git parents, run from the identity's worktree root; T007 |
| 4 | Medium | Deadline inside `Inspect` surfaces as `canceled` | Timeout mapping extended to any deadline when the caller is not canceled; T010 |
| 5 | Medium | No per-call deadline for identity and discovery; research Decision 3 contradicted the spec | Per-call 10 s deadlines; Decision 3 and risks recomputed; the 60 s total may fire and is reported per entry |
| 6 | Medium | Equality with `stw inventory` ignored jj `stored` scope | US1-1 and SC-001 wording |
| 7 | Medium | jj 64 KiB per-command limit missing from inherited limits | Added, marked unverified |
| 8 | Medium | Example inventory had `untracked` true without a `files` record | Example regenerated and both embedded inventories validated again with the contract helper |
| 9 | Medium | `inventory-partial` message picked an unrelated limitation | First limitation after the sampling statement; all limitations to `notes` |
| 10 | Medium | No jj-lane tests for jj-only edge cases | T016 extended |
| 11-21 | Low | Edge-case wording; `.git` entry kinds; lexical failures as `ignored` (not entries, so `path_encoding` removed); blob id passed to `config --blob`; value-less keys; provisional pointer rule; `discovery_not_applicable` counts explicit entries; facts-only fallback replaced by all-`output-limit`; per-entry line bound 24; `inventory-not-started` marked new; `notes` format; status line; leading-dash path needs `--` | Applied in spec, data model, contract, plan and tasks |

The author did not accept the reviewer's low-severity suggestion that the 8 MiB estimate always fits (16 inventories of up to 1 MiB each exceed it); the budget omission is kept.

## Round 3

Independent reviewer: read-only Opus review, revision 49efc031b68f4625181a02614ab7d18fca958259, 2026-10-16, Git 2.56.0; jj not available. Verdict: NOT READY (1 high, 4 medium, 6 low). All round-2 resolutions were confirmed except as listed. The guide's limit of three rounds is reached; the resolutions below were applied by the author and have not been re-reviewed.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | Post-inventory identity comparison is invalid for `error` inventories (no repository identity); would relabel inner `Inspect` timeouts as `changed-repository` | FR-008 and plan DD3: compare only `ok` and `partial` inventories; a mismatch nulls inventory and facts; T010 adds the inner-timeout case. **Superseded by round 4 finding M1**: error inventories usually do keep an identity, so the comparison applies whenever the embedded identity status is `ok` |
| 2 | Medium | data-model still said `inventory-limit` for omission | `output-limit` |
| 3 | Medium | plan DD6, research Decision 8 and contract Effects still used `HEAD:.gitmodules` | Blob id from `ls-tree` everywhere (research keeps the measured form with a note) |
| 4 | Medium | Omission of an `error` inventory undefined | Entry status table: keep inherited code, add `output-limit` to `notes`, facts null, counted in `inventories_omitted` |
| 5 | Medium | SC-001 lacked the jj scope qualification | Added |
| 6-11 | Low | Duplicate phrase and explicit-only wording; bare repository gate; T016 tags; jj operation-id comparison marked unverified; timeout grammar and message alignment; `notes` format of `inventory-partial` and of ignored paths | Applied |

## Round 4

Independent reviewer: read-only Opus review, revision 1bdcb49f178aea7c9eee9489b8943949944b8bfc, 2026-10-16, with a `stw` binary built by the reviewer in a scratch directory, Git 2.56.0; jj not available. Part A (verification and consistency) verdict: NOT READY (1 high, 3 medium, 7 low); the round-3 resolutions were confirmed applied. Part B is the usefulness assessment at the top of this file. The author applied the Part A resolutions below; they have not been re-reviewed.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | The inherited-limits disclosure named the wrong cause: the Git inventory fails on a 64 KiB `check-attr` output cap, at roughly 150 to 400 tracked files, with no attributes involved (measured: 400 short-path files ok, 250 files with 60-byte paths error, a 442-file tree error) | Spec inherited-limits section, research Decision 12, plan risks, T013 fixture corrected; the round-1 attribution to attribute content is marked wrong |
| M1 | Medium | "Error inventories carry no repository identity" is false: `git-inventory-failed` embeds a valid identity | FR-008, plan DD3: compare whenever the embedded identity status is `ok`; identity-less errors are skipped |
| M2 | Medium | The omission paragraph split the inventory/facts table | Paragraph moved below the table |
| M3 | Medium | `changed-repository` had two behaviours (inventory's own versus raised by `repos`) | Separate table row for the `repos`-side comparison (both null) |
| 5 | Low | Data-model omission invariant lacked the error-inventory exception; T010 missing FR-008; DD7 summary wording; `inventory-partial` message could be the informational base-candidate limitation; contract Effects missing "validated"; `discovery_not_applicable` definition; quickstart entry path | Applied |

## Round 5: implementation reviews at bcb3324

Two independent read-only Opus reviews of the implementation of WI1 to WI4 (revision bcb3324, plus a7866e1 which made the fake call counters atomic): a security and discovery review (reproducers under a scratch `rv`) and a correctness and test review (41 mutants: 23 killed, 18 survived; reproducers `rv023`, `out1..4.json`, `mut.py`, `mut2.py`). Git 2.56.0 on linux/arm64; jj was not available. Both verdicts: NOT CLEAR (1 critical, 6 high, 12 medium, 8 low). The fixes below were made by a third agent, test first (each new test was run against the old code and failed there, or is recorded as a regression guard that passed), and have not been re-reviewed by an independent reviewer.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| C1 | Critical | Real output never validated against `repos.schema.json`: `diagnostics` null, empty `backend` on invalid-path, skipped and path-unsafe entries, hand-written fixtures differing from real output | Result and entries are constructed with `diagnostics: []` and backend `unknown`; the real command output (healthy, invalid, duplicate, submodule states, subdirectory) and the usage-error result are validated against the repos and inventory schemas; fixtures regenerated to the real shapes and a test compares their entries with real output |
| C2 | Critical | `[submodule]` without subsection: `k[10:9]` panic, no JSON, exit 2 | Keys are split with the Spec 021 `splitSubmoduleKey` (non-empty name required); discovery runs per parent under `recover` mapped to `member-failed` (a recovered-panic test injects a panicking runner) |
| H1 | High | Path through a symlinked parent or cwd was `not-repository-root` | Explicit paths are resolved with `EvalSymlinks(Abs(EvalSymlinks(p)))`; both paths reported; tests for a symlinked parent and a symlinked cwd; the test suite now uses symlink-free temporary directories and passes with a symlinked `TMPDIR` |
| H2 | High | Submodules of a linked-worktree or gitfile superproject always `path-unsafe`; FR-008 identity check missing | The modules directory comes from `rev-parse --git-path modules` (Spec 021 `newPinsSuperLayout`); the Git directory Git reports for the submodule must equal the validated one; real linked-worktree and gitfile fixtures |
| H3 | High | Discovered submodules bypassed bounds (no per-call deadline, no recovery, sequential) | Parents are discovered concurrently on the pool; discovered entries are inspected through the same pooled closure as explicit ones (per-call deadline, recovery, timeout mapping); each discovery command runs under its own 10 s deadline; tests with a recorded deadline, a panic, a hanging `ls-tree` through a PATH shim, and a FIFO as `.git` and as `HEAD` |
| H4 | High | `inventory-partial` message always the sampling sentence | The sampling statement (limitation 0) is skipped for message and notes; notes are capped at 16 plus a summary note, and every message is cut at 512 bytes on a rune boundary |
| H5 | High | A failing `ls-tree`/`ls-files` reported discovery `ok` | Only a proven unborn HEAD, a bare repository or empty output is `ok`; anything else is `discovery-failed` (or a mapped timeout); test with an unreadable root tree |
| H6 | High | T007, T008, T010 ticked without their mandated tests | The real-Git tests were written (mktree symlink blob, linked worktree, gitfile, symlinked path and component, `.git` symlink and FIFO, process-free proof, 16-entry and 64-candidate caps, sort order, unborn, bare, many-file directory, value-less key, non-UTF-8 path, swapped real repository, `extensions.submodulePathConfig` on Git 2.56 with a skip before any fixture exists); see tasks.md |
| M1 | Medium | Discovered entries not deduplicated by identity | Explicit and discovered entries share one identity index; a submodule that is the same repository as an earlier entry is `duplicate-repository` |
| M2 | Medium | Symlinked submodule path counted `deduplicated` | Confinement (no symlink) is decided by the safety layer; a discovered path is never resolved; test with a symlink to another listed repository |
| M3 | Medium | One non-UTF-8 resolved path nulled every inventory | Such a path is `invalid-path` in S1; repos-composed strings are cleaned; the budget separates encoding from size errors; the output-limit note is added only where an inventory was removed |
| M4 | Medium | Pointer and Git-directory confinement weaker than Spec 021 | Reuse of `pinsSafety.resolve`, `checkPointer`, `pinsReadSmall`, `recheck` and `pinsRunner` through `discoverParent` (an adapter of about 60 lines). Consequence, accepted and documented in the spec, contract and docs: alternates and commondir are refused outright, so a submodule that borrows objects through alternates is `path-unsafe` and not inventoried by `repos --submodules`; an uninitialized submodule directory writable by others is `path-unsafe`, not `skipped` |
| M5 | Medium | Integration test compared status and backend only | Marshalled inventories compared with `Inventory`; the submodule entry, parent, notes and facts asserted; inherited causes pinned by code and message |
| M6 | Medium | Pointer-outside-modules test passed for the wrong reason | Replaced by real fixtures whose only defect is the pointer (outside, the modules directory itself, another module, oversized, garbage) |
| M7 | Medium | Inherited check-attr and record limits disclosed only in spec.md | Stated in the contract (section "Inherited limits"), docs/repos.md and the agent guide; a contract test requires the statements; integration test asserts the messages |
| M8 | Medium | FR-009 said a staged removal is still discovered | FR-009 corrected: it is ignored and counted |
| M9 | Medium | Bare repository ran discovery processes | Bare and unborn parents return `ok`/0 without a process (counted with an `env` hook) |
| M10 | Medium | US3-6 caveat only in `facts.dirty.reason` | Appended to the notes of every submodule entry with facts |
| M11 | Medium | Output-limit note format, displaced causes, unbounded notes, unreachable final re-check | `output-limit: <message>` note; the displaced cause goes to the notes through `ReposResolveCauses`; partial notes capped; tests for causes, for a non-inventory overflow and for an omission whose own growth overflows the bound |
| M12 | Medium | Weak bound, escaping and timeout tests | Peak concurrency exactly 4 for 8 entries (a gate holds the first four callers); normative limits asserted on the boundary object; text tests for control characters, newline, U+2028, invalid UTF-8, 30 attention workspaces, the 24-line and 512-byte bounds and exact `canceled` |
| L1 | Low | Foreign counting differed between text and facts | One exported classifier `ReposWorkspaceKind` |
| L2 | Low | `escapeReposLine` could split a rune or an escape; ignored-path notes inconsistent | Cut after escaping between whole characters; every ignored path is `discovery-ignored: <escaped path>` |
| L3 | Low | Caller deadline reported as the 60-second bound | Any caller context error is `canceled` with its own message |
| L4 | Low | Leading-dash existing path refused | Check dropped: paths are data |
| L5 | Low | Regular file at a registered path was `skipped`; `.git` Lstat errors fell through | Safety layer: a non-directory is `path-unsafe`, only ENOENT is uninitialized |
| L6 | Low | Contract example and fixtures differed from the code; spec status | Example, fixtures and texts aligned; a gitlink-bearing superproject is `partial` in the skipped fixture |
| L7 | Low | Unused duplicate functions | Deleted |
| L8 | Low | Omission discarded the earlier error | Moved to the notes (see M11) |

Mutants: the reviewers' surviving mutants (M04, M05, M11 to M17, M21, M27, M32b, M33b, M35, M36b, M37, M39, M40, M41) were re-run against the fixed code with their patterns mapped onto it; the results and 30 new mutants of the fixes are in verification.md. A mutant that no test can distinguish is recorded there as equivalent, not hidden.

**Deviations from the spec text** (all in the spec, contract or docs now): discovery commands run through `pinsRunner` and use `--literal-pathspecs` instead of the `:(literal)` pathspec prefix; a retained-output overflow of one `ls-files` query counts as `ignored` as before, now by the runner's 1 MiB retained budget with a record filter; two `.gitmodules` names for one path use the lowest name (as the pin audit does) and a mismatching Git directory is refused; a discovery step that times out marks the parent `partial` with `inventory-timeout` and the inventory still runs; the per-call and total deadlines are package variables so tests can shorten them; the discovery seam is a runner factory (`reposNewRunner`) instead of a `runner` function.

**Open maintainer decision (not decided here):** the usefulness assessment above recommended a prerequisite inventory fix (bound or batch the `check-attr` read, describe a submodule checkout instead of its Git directory, report tracked state for gitlink-bearing superprojects) and cutting `--submodules`, which now carries the reused Spec 021 safety layer. Neither was applied. The reviews of this round did not change the assessment: the implementation is correct against the spec, but a realistic workspace still mostly yields `partial` or `error` entries and exit status 1 because of the inherited limits.
