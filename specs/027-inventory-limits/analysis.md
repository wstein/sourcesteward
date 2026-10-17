# Pre-implementation analysis

## Round 0

Author draft, 2026-10-16, revision recorded by the commit that adds this file. Not independent. Measurements are in research.md (Git 2.56.0; two spot checks on Git 2.47.3; jj not available). Independent review rounds are recorded below as they complete.

## Round 1

Independent reviewer: read-only Opus review, revision a90c67f87487ca55de7a63165525836608828a02, 2026-10-16, Git 2.56.0 and 2.47.3. Verdict: NOT READY (1 critical, 6 high, 11 medium, 14 low). The reviewer's measurements are marked as theirs in research.md. The author re-verified finding 1 (the data model was truncated by a shell redirect bug in the first commit) and finding 18 (code reading of `resultRecordCount`).

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | Critical | data-model.md corrupt | Restored in full with invariants, assembly order and the example; example re-validated |
| 2 | High | Case Q vs the schema coupling of status and complete | QR-1: known-predicate query runs stay `ok`/`complete=true`, TXT string in limitations; Spec 009 sentence narrowed (COMPAT-12) |
| 3 | High | Reference truncation in queries; exit rule | QR-1 covers files, references and byte trimming; one exit rule in the contract |
| 4 | High | Gitlink superproject `ok` in a plain run | GL-4: plain run stays `partial`; only the predicate is exempt; example and quickstart changed |
| 5 | High | Predicates from the returned listing | QR-1 and data-model step 6: candidate count before truncation; test with zero files |
| 6 | High | FEED-5 overflow leaves `tracked` known | DEG-7: `tracked` unknown, clean/dirty unknown |
| 7 | High | Goal not met on real repositories (files over 64 KiB) | Per-file read raised to 16 MiB with chunked hashing (LIM-10, COMPAT-10, FR-010); SC-001 gains a real-shaped fixture |
| 8 | Medium | CK-2 contradicts US3-3 | CK-2 partial whenever it applies; US3-3 and T009 aligned |
| 9 | Medium | Query rule names only GL-5 | Rule covers GL-4 and GL-5 |
| 10 | Medium | LIM-8 "per run" | Per pass of the inspected workspace; other workspaces keep flags only |
| 11 | Medium | check-attr stdin deadlock | Concurrent writer required (FEED-3, data model); tests in T006 |
| 12 | Medium | DEG-5 leaves conflicts and signature undefined | Defined in DEG-5a/5b |
| 13 | Medium | `--path` retains outside entries | DEG-13 |
| 14 | Medium | LIM-7 unreachable | LIM-3 64 MiB, LIM-7 8 MiB of paths |
| 15 | Medium | Sparse checkout and `core.symlinks=false` | SP-1, SP-2, FR-015, COMPAT-11, FEED-4 uses `-t` (to be re-verified at implementation) |
| 16 | Medium | T004 stored expectations wrong | Corrected |
| 17 | Medium | SC-002 coverage gaps | DEG-10 to DEG-14, BUF, CLI exit and handoff and jj tests added to T006, T007, T013 |
| 18 | Medium | COMPAT-8 inaccurate | Corrected and a handoff test added |
| 19-32 | Low | DEG-2 missing, per-command deadline wording, TXT-4 reuse, invalid record code, memory bound, process claim, refs patterns, CK-2 rationale, stderr, DEG-3 ignored, worktree threshold, 023 8 MiB row, tag bytes, checklist | Applied (DEG-2 and DEG-14 added, TXT-10, patterns in FEED-1, CK via `inspect(entry)`, wording fixes, 023 row, checklist unchecked) |

## Round 2

Independent reviewer: read-only Opus review, revision f659417beb5c8a051febfface2f1f84ef4ae6e44, 2026-10-16, Git 2.56.0 and 2.47.3. Verdict: NOT READY (4 high, 8 medium, 8 low). Round 1 resolutions were confirmed except as listed (findings 5, 6, 7 below were partial). The reviewer's measurements are theirs; the author applied the resolutions and they are re-reviewed in round 3.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | `--separate-git-dir` falls into CK-2 (`inspect(entry)` fails there) | CK-1 takes `W` from `inspect(entry)` or from the non-linked inspected identity; research Decision 4, FR-006, T009 aligned |
| 2 | High | Query scope skip precedes CK; `clean` answers true with nothing observed | CK applied before the skip; clean/dirty `unknown` unless the inspected checkout entry was observed; T009 query cases |
| 3 | High | SP-1 gives a false clean for a present modified skip-worktree file | SP-1 exempts only entries absent on disk; present ones are compared; test added. Supersedes round 1 resolution 15 |
| 4 | High | SC-001 fixture cannot be `ok` (records per ref) | 200 branches and 200 tags |
| 5 | Medium | `-t` record format has four head fields, tag `M` | FEED-4 specifies H, S, M and `invalid-metadata`; T006 |
| 6 | Medium | LIM-11 per workspace times worktrees | Per run, counted in the first pass; second pass compares the same paths |
| 7 | Medium | Blob header length source | `st.Size()` plus read-length and after-stat check in LIM-10 |
| 8 | Medium | Reference-predicate early path unspecified | Data-model paragraph; T008 |
| 9 | Medium | DEG-7 predicates for FEED-6/7 | DEG-7 states the rule |
| 10 | Medium | Hostile `core.worktree` claim false | Corrected in contract and research; CK-2 never prints `W`; T009 reworded |
| 11 | Medium | Stale 16 MiB / tens of MiB, underived memory bound | Research and plan corrected; bound derived in the contract |
| 12 | Medium | Edge case says sparse is untouched | Replaced by SP-1 reference |
| 13-20 | Low | Checklist, COMPAT/TXT ranges, coverage rows, case-Q wording, `stopped` return and stdin adapter, two-records note, `{scope}` words, FR ordering | Applied |

## Round 3

Independent reviewer: read-only Opus review, revision 22519726d31f3ab88e164206d9fb1c166c00efb9, 2026-10-16, Git 2.56.0 and 2.47.3. Verdict: NOT READY (1 high, 5 medium, 4 low). All round 2 resolutions were confirmed except 1, 3 and 9 (partly). The resolutions below are re-reviewed in round 4.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | FR-015 and a data-model invariant still say skip-worktree entries are not compared (false clean for present modified ones) | FR-015 and the invariant say "absent on disk"; T006 asserts workspace `tracked` true |
| 2 | Medium | CK query rule over-applies to stored-only and bare queries | Limited to selected scopes that need disk or index; bare stored-only case in T009 |
| 3 | Medium | Failing candidate `W`: contract vs data model | One outcome: revert to CK-2; T009 case for a disappearing candidate |
| 4 | Medium | T009/US3-3 vantage point | US3-3 and T009 distinguish the main checkout (CK-1) from a linked worktree (CK-2) |
| 5 | Medium | DEG-7 FEED-5 sentence contradicts the general exception | FEED-5 follows the exception; T007 aligned |
| 6 | Medium | LIM-11 charged in list order | Inspected workspace first; T006 case |
| 7 | Low | H/S with non-zero stage | Any non-M tag with a non-zero stage is `invalid-metadata` |
| 8 | Low | Sparse fixture clears the bit | T006 and research name `update-index --skip-worktree` |
| 9 | Low | Stale status line, plan Decision 6, "never printed" | Corrected |
| 10 | Low | Scan-time record checks unnamed | Named in T007 |

## Round 4

Independent reviewer: read-only Opus review (general-purpose agent, commands allowed), revision 60690561ef589ad96322bfa0fbaea14db941885a, 2026-10-17, Git 2.56.0 and 2.47.3. Verdict: NOT READY (2 high, 2 medium, 4 low). All round 3 findings were confirmed resolved. The new findings concern the merged Spec 023 consumer; resolutions below are applied by the author and re-reviewed in round 5.

| # | Severity | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | High | 023 test changes scheduled after the work items that break them | Each 023 change moved into the WI that breaks it (WI2, WI3, WI5); WI7 keeps texts and docs |
| 2 | High | 023 table incomplete (repos.go note and limitation, repos_facts.go, goldens, repos_discover_test, smoke-repos.py, contracts/repos_test.go, contract lines 54/95, README, llms) | Table rebuilt with all locations and the removal of the dirty caveat |
| 3 | Medium | Spec/checklist/contract say 023 is not changed | FR-014, Scope, checklist and contract heading corrected |
| 4 | Medium | T004/US1-2 record count | Three records; US1-2 reworded |
| 5-8 | Low | FR-007 vs GL-3; output order; memory bound; stale headers | Applied (retained path bytes capped at 32 MiB per pass) |

## Round 5

Independent reviewer: read-only Opus review, revision 80fccd4e4d3d72a1d412ed3ee083bc5c0fd5b712, 2026-10-17. Verdict: READY (0 critical, 0 high, 4 medium, 4 low). Round 4 resolutions confirmed except the memory bound and the WI5/WI7 split (partly). Resolutions applied after the review: the repos_test.go change moved to WI7 with the 023 contract rewrite; the planned 023 "Always partial" narrowing replaced by the GL-4 statement; "Spec 023 changes" removed from the out-of-scope list; the 32 MiB retained-path cap made part of LIM-8, DEG-2, the bounds struct and T007; the memory worst case stated honestly; repos_facts_test.go, contract line 37 and docs/commands.md line 63 added to the table.
