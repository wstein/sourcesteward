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
**Constraints**: no hook, filter, helper, network, prompt or write; no index refresh or lock; bounded memory (tens of MiB at the extremes of LIM-7 to LIM-9); contracts unchanged.
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
2. **`check-attr --stdin -z`** (FEED-3) removes argument-length and leading-dash concerns and lets the output be read incrementally. The decision rule is unchanged and early-exits. `core.autocrlf` early return unchanged.
3. **Do not retain clean index entries**: `workspaceStatus` keeps only changed, conflicted, unknown and gitlink entries; the signature becomes a rolling SHA-256. Memory is bounded by LIM-7 and LIM-8, not by the repository.
4. **Returned versus scanned records** (research Decision 2): schema constants untouched; the fair share (FS-1) is computed once at the end; per-reference ancestry facts and predicates are ordered so they cannot depend on a truncated list.
5. **Case Q and reference-predicate exception**: a truncated supporting listing does not change a known predicate's status or exit (otherwise `--query dirty` on a dirty repository would exit 1).
6. **Checkout entry by identity** (CK, research Decision 4); no new repository-controlled path is read.
7. **Positive evidence for gitlinks** (GL, research Decision 5); no submodule is opened. A clean superproject stays `partial`; `stw pins` or a future spec supplies the negative proof.
8. **Content read budget** (LIM-11) so that large-file repositories degrade instead of timing out.
9. **Early stop** by context cancellation with a no-lock, no-process assertion in tests.
10. **jj untouched**; shared helpers (`checkInventoryLimits`, `inventoryJSONBudget`) are reused unchanged, only Git calls the new fit helper.

## Risks

- A truncated listing is read as complete by a consumer: mitigated by `partial`, `boundary.complete=false` and verbatim TXT strings; case-Q exceptions are narrow and tested.
- Substituting the checkout path changes a value consumers may have keyed on (COMPAT-3): documented; Spec 023's root-workspace rule matches the workspace whose path equals the identity's worktree (read from the 023 branch, spec US3-6), which CK-1 makes true for a submodule checkout, so the change helps it.
- The 10-second deadline remains for pathological repositories (cold cache, millions of files): the index limit and content budget degrade first, the deadline is the backstop and stays an `error`.
- `check-attr --stdin -z` on Git 2.46.0 is unverified (2.47.3 and 2.56.0 verified); the minimum lane in CI is the check.
- Reference scans are still O(refs) Git processes when `--base`, `--revision` or `--remote` are given; mitigated by computing facts after truncation, bounded by the deadline.
- Fair-share shape and the numbers of LIM-7/8/9/11 are design choices without a use-case measurement beyond research; flagged as open questions.

## Spec 023 follow-up

Not changed by this spec (maintainer scope decision). When this lands, Spec 023 must be updated in these artifacts:

| Artifact | Section | Change |
| --- | --- | --- |
| `specs/023-multi-repo-inventory/contracts/repos.md` | "Inherited limits" (check-attr 64 KiB, record limit, always partial, jj) | Replace the first three bullets with the new limits (LIM table) and the degradation behavior; keep the jj bullet; the example's `inventory-partial: submodule disk state excluded...` line stays valid for a clean superproject |
| `specs/023-multi-repo-inventory/spec.md` | US3-6, SC-001, edge cases (over 1,024 files), FR-009 wording on the runner cap, the inherited-limits disclosure | Submodule entries can now derive `dirty` from the root workspace record (CK-1); large repositories give facts; the "over 1,024 files" fixture expectation changes from error to ok/partial |
| `specs/023-multi-repo-inventory/research.md` | Decision 12 | Mark the Git-directory finding as fixed by Spec 027; keep the measurement as history |
| `specs/023-multi-repo-inventory/data-model.md` and `plan.md` | facts derivation for `workspaces.dirty`, `dirty_unknown`; risks; open question 5 | Dirty derivation may use `tracked: true` of gitlink-bearing superprojects (GL-4) and of submodule checkouts; question 5 is answered |
| `specs/023-multi-repo-inventory/tasks.md` | T011, T013 fixtures | Expectations: a member with more than 1,024 files is no longer `error`; a submodule checkout has a root workspace record; superproject with a modified ordinary file is `tracked: true` |
| `specs/023-multi-repo-inventory/analysis.md` | Usefulness assessment | Note the prerequisite is delivered; the maintainer's other recommendations (cutting `--submodules`, alias for linked worktrees) are separate |
| `docs/repos.md`, `docs/agent-guide.md`, `docs/commands.md`, `docs/backends.md` (023 branch) | inherited-limit paragraphs ("64 KiB attribute output, 1,024 records", "gitlinks always partial", "a submodule's Git directory, not its checkout") | Update to the new behavior |
| `specs/023-multi-repo-inventory/quickstart.md` | expected output for `super` and `member1` | `super` is `partial` only because of the stale worktree and untouched gitlinks; `member1` has facts |
| 023 tests | embedded-inventory equality tests and fixtures with 1,030 files | Re-baseline; no schema change, so `repos` schema stays valid |

## Open questions (with recommendations)

1. Fair share when both sections overflow: half each, as specified. Recommendation: keep; revisit with consumer evidence.
2. Raise the 64 KiB per-file content read (files above it keep `tracked` unknown, measured on `big`)? Recommendation: separate follow-up spec; it changes documented behavior (docs/inventory.md) and needs a timing study.
3. Observe submodule worktree state to prove `false` for clean superprojects? Recommendation: do not in this prerequisite; use `stw pins` (Spec 021) or a later spec that reuses its confinement layer.
4. Make the references limit a partial listing even when the scan allowance is exceeded? Recommendation: no; a listing cut at an unknown point could make a predicate lie (DEG-6).
5. Should the jj adapter get the same treatment? Recommendation: yes, as a separate spec once jj can be measured in the authoring environment.
