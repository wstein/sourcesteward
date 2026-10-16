# Pre-implementation analysis

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
