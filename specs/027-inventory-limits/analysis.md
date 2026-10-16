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
