# Implementation Plan: Inventory limits and checkout description

**Branch**: `codex/027-inventory-limits` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Make the Git inventory adapter scale with the repository instead of with the 64 KiB runner cap and the 1,024 returned-record limit, describe a checkout whose Git directory is elsewhere by its files, and report ordinary-file changes in a gitlink-bearing superproject. A small streaming Git runner with the shared isolation reads the commands whose output grows with the repository (FEED table); `check-attr` takes its paths on standard input; bounds that used to be errors become partial results with verbatim disclosures wherever omission cannot create a false claim. No schema, constant, flag or exit-status change.

## Technical Context

**Language/Version**: Go 1.27.2; Git 2.46 or newer (CI minimum and current lanes). Local lanes: Git 2.47.3 and 2.56.0 on linux/arm64.
**Primary Dependencies**: standard library, Cobra and the existing JSON Schema test dependency. No new dependency.
**Storage**: none.
**Testing**: Go tests with real temporary repositories (scale fixtures, submodules, `--separate-git-dir`, linked worktrees), hostile fixtures with effect sentinels, schema validation of real outputs, injected small bounds for limit rows, `scripts/test_inventory_smoke.py`, `go test -race -count=3 -shuffle=on`, a run under `PATH=/usr/bin:$PATH` (Git 2.47.3).
**Target Platform**: linux amd64/arm64 and darwin arm64 code paths (the streaming helper has no platform-specific code). CI evidence is ubuntu-24.04 amd64 only.
**Project Type**: CLI with a shared operation layer; the change is inside `internal/inspection`.
**Performance Goals**: 5,000 tracked files in well under the 10-second deadline (the measured cost of the proposed reads is tens of milliseconds, research Measurement 2); 50,000 files in about a second.
**Constraints**: no hook, filter, helper, network, prompt or write; no index refresh or lock; bounded memory (below about 100 MiB at the extremes of LIM-7 to LIM-9, derived in the contract); contracts unchanged.
**Scale/Scope**: single repository inventory, Git only.

## Constitution Check

Pre-research: PASS. Post-design: PASS.

- **I. Shared contract**: text and JSON still render one result; disclosures are existing `Limitation:` lines; exit statuses unchanged; non-interactive.
- **II. Evidence before actions**: nothing here authorizes an action; truncation and unknowns are disclosed, `clean` is never derived from an omission (invariants in the data model).
- **III. Ownership and recovery**: read-only; Git's own ownership check stays in force because discovery is by working directory (no explicit `--git-dir`, unlike Spec 021's runner); linked worktrees are all inspected as before.
- **IV. Specify and verify**: failing tests first; real Git fixtures at the reported scales; hostile inputs (planted filters, hooks, fsmonitor, textconv, external diff, hostile paths, oversized and unterminated records, blocked command); effect sentinels including no lock after an early stop; Git minimum and current lanes; CI remains the acceptance evidence, local arm64 and 2.47.3 runs are local evidence.
- **V. Tier 1 peers**: Git inventory changes; jj inventory is unchanged and the capability gap (jj commands still capped at 64 KiB) is stated in the contract and docs rather than claimed equivalent. jj ownership still uses jj; the Git adapter is not reachable for a jj-managed workspace.
- **Scope**: read-only inspection within implemented scope; no new network or hook effect; no amendment.

## Project Structure

```text
internal/inspection/inventory_stream.go        # new: streamGit, record splitter, limits, adapter from buffered runner
internal/inspection/inventory_stream_test.go   # new: real-git stream tests, stop/overflow/deadline, no process or lock left
internal/inspection/inventory_git.go           # changed: FEED-1..7, CK, GL, truncation, bounds
internal/inspection/inventory_git_scale_test.go    # new: scale fixtures, degradation rows with injected bounds
internal/inspection/inventory_git_checkout_test.go # new: CK and GL fixtures, hostile fixtures
internal/inspection/inventory_result.go        # changed: fair share and fit helpers (checkInventoryLimits stays)
internal/contracts/inventory_test.go           # extended: real-output schema validation, unchanged-schema guard
scripts/smoke-inventory.py, scripts/test_inventory_smoke.py  # extended (binary acceptance not run without build approval)
docs/inventory.md, docs/json-contracts.md, docs/agent-guide.md, docs/backends.md, specs/009-workspace-inventory/contracts/inventory.md (pointer)
docs/progress.md, docs/feature-status.json
```

## Design Decisions

1. **Streaming runner, not the pins runner** (research Decision 1). It lives next to the Git adapter, shares `gitEnvironment()`, the prefix and `WaitDelay` of `runGit`, adds a record callback, LIM-3/LIM-4 and cancellation. Injected buffered runners keep working through an adapter, so the existing interposition tests need no rewrite; production wires `streamGit`.
2. **`check-attr --stdin -z`** (FEED-3) removes argument-length and leading-dash concerns and lets the output be read incrementally. A writer goroutine feeds standard input concurrently with the output read, because `check-attr` answers each path before reading the next and a sequential write deadlocks (reviewer-verified). The decision rule is unchanged and early-exits. `core.autocrlf` early return unchanged.
3. **Do not retain clean index entries**: `workspaceStatus` keeps only changed, conflicted, unknown and gitlink entries; the signature becomes a rolling SHA-256. Memory is bounded by LIM-7 and LIM-8, not by the repository.
4. **Returned versus scanned records** (research Decision 2): schema constants untouched; the fair share (FS-1) is computed once at the end; per-reference ancestry facts and predicates are ordered so they cannot depend on a truncated list.
5. **Query run rule QR-1**: a truncated supporting listing does not change a known predicate's `status`, `complete` or exit (otherwise `--query dirty` on a dirty repository would exit 1), and predicates use the candidate count, not the listing.
6. **Checkout entry by identity** (CK, research Decision 4): `inspect()` of the entry, or the non-linked inspected identity when `inspect(entry)` fails (separate Git directory), gives the checkout, applied before the query-scope skip; no new repository-controlled path is read.
7. **Positive evidence for gitlinks** (GL, research Decision 5); no submodule is opened. A clean superproject stays `partial`; `stw pins` or a future spec supplies the negative proof.
8. **Per-file read raised to 16 MiB with chunked hashing (LIM-10) and a per-run budget (LIM-11)**, so real repositories with a few larger files give facts and huge ones degrade instead of timing out (round 1 finding 7: two tracked files over 64 KiB in `sourcesteward`, eight in `bundledock`).
9. **Early stop** by context cancellation with a no-lock, no-process assertion in tests.
10. **jj untouched**; shared helpers (`checkInventoryLimits`, `inventoryJSONBudget`) are reused unchanged, only Git calls the new fit helper.

## Risks

- A truncated listing is read as complete by a consumer: mitigated by `partial`, `boundary.complete=false` and verbatim TXT strings; QR-1 exceptions are narrow and tested.
- Substituting the checkout path changes a value consumers may have keyed on (COMPAT-3): documented; Spec 023's root-workspace rule matches the workspace whose path equals the identity's worktree (read from the 023 branch, spec US3-6), which CK-1 makes true for a submodule checkout, so the change helps it.
- The 10-second deadline remains for pathological repositories (cold cache, millions of files): the index limit and content budget degrade first, the deadline is the backstop and stays an `error`.
- `check-attr --stdin -z` on Git 2.46.0 is unverified (2.47.3 and 2.56.0 verified); the minimum lane in CI is the check.
- Reference scans are still O(refs) Git processes when `--base`, `--revision` or `--remote` are given; mitigated by computing facts after truncation, bounded by the deadline.
- Fair-share shape and the numbers of LIM-7/8/9/11 are design choices without a use-case measurement beyond research; flagged as open questions.

## Spec 023 follow-up

Spec 023 (`stw repos`) is merged (main f91c1e0) and consumes `Inventory` unchanged. The implementation of this spec includes the following 023 edits, in its own commits (WI7 in tasks.md), because the merged tests and documents pin the old limits:

| Artifact | Location | Change |
| --- | --- | --- |
| `internal/inspection/repos_integration_test.go` | lines 84-100 (fixtures r6Attr, r7Limit), 168-196 (assertions) | r6Attr (400 files, 160-byte paths) and r7Limit (more than 1,024 files) stop being `git-inventory-failed`: they become `ok` with facts (clean fixtures). Replace the "inherited limit" assertions with ok/facts assertions; keep r5Super (`inventory-partial`, text TXT-9) |
| `internal/inspection/repos_budget_test.go` | lines 34, 52 | Uses a synthetic `git-inventory-failed` entry for the output budget; stays valid (no change expected, re-run) |
| `internal/inspection/repos_facts_test.go`, `repos_real_test.go`, `repos_partial_test.go` | submodule and gitlink facts | Re-run; where a test pins `dirty: unknown` for a submodule checkout or for a gitlink superproject, update per CK-1/GL-4 |
| `specs/023-multi-repo-inventory/contracts/repos.md` | "Inherited limits" (lines 28-35, 41), example at lines 50, 392 | Replace the content-attributes and record-limit bullets with the new limits and degradation (reference contracts/inventory-limits.md); keep the jj bullet; "Always partial" narrows to a clean gitlink-bearing superproject and a bare repository; line 392 wording follows |
| `specs/023-multi-repo-inventory/spec.md` | US3-6 (line 53), the inherited-limits section (line 179), SC-001 (line 216), entry-status table row for `git-inventory-failed` (line 147) | A submodule entry's `dirty` can be derived from its root workspace record (CK-1); large repositories give facts; the error row keeps `git-inventory-failed` for other causes |
| `specs/023-multi-repo-inventory/research.md` | Decision 12 (lines 70-77) | Mark the Git-directory, 64 KiB and 1,024-record findings as fixed by Spec 027; keep the measurements as history |
| `specs/023-multi-repo-inventory/plan.md` | risk at line 76, open question 5 (line 88), facts derivation | Risk and question answered |
| `specs/023-multi-repo-inventory/data-model.md`, `tasks.md` (T011, T013) | facts derivation for `workspaces.dirty`; fixture expectations | Dirty derivation may use `tracked: true` of gitlink-bearing superprojects and submodule checkouts; T013 expectations for the 400-file and 1,030-file members change from error to ok |
| `specs/023-multi-repo-inventory/spec.md` and `data-model.md` | the 8 MiB embedding budget and `output-limit` | Inventories can be close to 1 MiB each (up to about 1,023 records), so `output-limit` omissions become more frequent; re-measure and state it |
| `specs/023-multi-repo-inventory/analysis.md`, `verification.md` | usefulness assessment; unverified list | Note the prerequisite is delivered |
| `docs/repos.md` | lines 32, 47-59 (inherited limits, Git directory note) | Update to the new behavior |
| `docs/agent-guide.md` | line 137 ("64 KiB attribute output, 1,024 records") | Update |
| `docs/commands.md`, `docs/backends.md` | repos paragraph; "64 KiB per-command output bound" | Update |
| `scripts/smoke-repos` or the repos smoke, if any pins the old errors | (grep at implementation time) | Update |

## Open questions (with recommendations)

1. Fair share when both sections overflow: half each, as specified. Recommendation: keep; revisit with consumer evidence.
2. Resolved by round 1: the per-file read is raised to 16 MiB in this spec (COMPAT-10) because the stated goal is not met on real repositories otherwise. Remaining question: is 16 MiB per file and 256 MiB per run the right ceiling? Recommendation: keep, measure cold-cache I/O in the verification.
3. Observe submodule worktree state to prove `false` for clean superprojects? Recommendation: do not in this prerequisite; use `stw pins` (Spec 021) or a later spec that reuses its confinement layer.
4. Make the references limit a partial listing even when the scan allowance is exceeded? Recommendation: no; a listing cut at an unknown point could make a predicate lie (DEG-6).
5. Should the jj adapter get the same treatment? Recommendation: yes, as a separate spec once jj can be measured in the authoring environment.
