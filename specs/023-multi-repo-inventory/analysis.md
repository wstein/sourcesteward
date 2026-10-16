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
