# Implementation Plan: Multi-repository inventory

**Branch**: `codex/023-multi-repo-inventory` | **Date**: 2026-10-16 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw repos`, a read-only command that expands an explicit path list (and optionally each Git superproject's direct registered submodules) into at most 16 repository entries, runs the existing `Inventory` operation for each with bounded parallelism, validates that each result really describes the requested repository, derives a small summary from the embedded inventories, and returns one typed result rendered as text or versioned JSON. `internal/inspection/inventory*.go`, `schemas/v1/inventory.schema.json` and `stw inventory` do not change.

## Technical Context

**Language/Version**: Go 1.27.2; Git 2.46 or newer (existing lanes); native jj as for inventory.

**Primary Dependencies**: Cobra, the standard library and the existing JSON Schema test dependency. No new dependency.

**Storage**: None.

**Testing**: Go table tests with temporary Git repositories, an injectable inventory operation for hang, panic and oversize cases, schema fixtures, `scripts/smoke-repos.py` with a unit test, effect sentinels, existing architecture and docs checks.

**Target Platform**: linux amd64/arm64 and darwin arm64 supported; CI evidence is ubuntu-24.04 amd64 only, other results are local evidence.

**Project Type**: CLI with a shared operation layer.

**Performance Goals**: see the limits table in the spec (16 entries, 4 workers, 10 s per member, 60 s total, 8 MiB).

**Constraints**: no network, fetch, write, hook or helper; no recursion into nested submodules; no change to existing contracts.

## Constitution Check

Pre-research: PASS. Post-design: PASS.

- **I. Shared contract**: one `ReposResult` renders text and JSON; non-interactive; stdin never read; exit statuses 0, 1, 2 (spec table).
- **II. Evidence before actions**: read-only; facts are sampled and carry the inventory's non-transactional boundary; nothing authorizes a mutation.
- **III. Ownership and recovery**: no mutation. Git's own ownership check applies to every member (discovery without explicit `--git-dir`, measured). Foreign or unsupported members become entries, never a fallback.
- **IV. Specify and verify**: failing tests first; real temporary repositories (linked worktrees, nested, submodules, hostile); race and concurrency tests; effect sentinels per member; schema fixtures; conformance on both Git lanes and the jj lanes.
- **V. Tier 1 peers**: Git and jj members use their own adapters through the same inventory operation, selected by ownership. The capability differences (jj dirty state `unknown`; submodule discovery is Git-only) are stated in the result and docs. Ahead/behind is not provided for either backend, equally.
- **Scope**: network, hooks and writes are not used and disclosed. A new read-only command is within "workspace/worktree inspection".

## Project Structure

```text
internal/inspection/repos.go           # orchestration: expansion, workers, deadlines, assembly, exit-relevant status
internal/inspection/repos_result.go    # result types, codes, limits, ReposOptions validation, JSON budget
internal/inspection/repos_paths.go     # explicit path validation, duplicate and containment detection
internal/inspection/repos_discover.go  # minimal submodule discovery and FR-008 path/pointer checks
internal/inspection/repos_facts.go     # facts and summary derivation from inventories
internal/inspection/repos_*_test.go    # temporary-repository tests, fake inventory operation, sentinels
internal/cli/repos.go                  # command, flags, text rendering, exit mapping
internal/cli/root.go                   # register the command (one line)
schemas/v1/repos.schema.json           # new contract
internal/cli/testdata/repos-*.json     # schema-valid fixtures
internal/contracts/repos_test.go       # fixtures against the schema and embedded inventories against the inventory schema
scripts/smoke-repos.py, scripts/test_repos_smoke.py   # native conformance smoke and its unit test
docs/repos.md, docs/json-contracts.md, docs/README.md, docs/commands.md, docs/agent-guide.md   # guide and indexes
```

The Git adapter for discovery stays in `internal/inspection` and reuses `runGit`; `internal/cli` renders only and starts no process, so `scripts/check-architecture.py` needs no exception.

## Design Decisions

1. **New command and schema** (research Decision 1). The result `command` is `repos`, `schema_version` 1.
2. **Pipeline**: S1 validate options and paths (no process); S2 identity inspection (`Inspect`) of explicit entries, parallel; S3 deterministic validation of explicit entries (root equality, duplicates by native identity, containment); S4 discovery, only for explicit parents that passed S3 and are Git-owned, sequential and run from the identity's worktree root, then the process-free `.git` pointer check and identity inspection of discovered members (parallel) and their validation (expected path and Git directory, duplicates, containment); S5 inventory of surviving entries, parallel, with options chosen by backend (Git default scopes, jj `stored`), followed by the identity comparison of Decision 3; S6 assemble facts, summary and the size budget; S7 render. S1, S4 and S6 are deterministic functions of their inputs and are unit-testable without repositories through an injected `inspectFunc` and `inventoryFunc` (defaults `Inspect`, `Inventory`). Running `Inspect` first costs one extra identity inspection per entry and is what makes the root, duplicate and backend-scope decisions possible before any inventory runs, and keeps Git discovery from ever running in a jj-owned parent (principle V).
3. **Worker pool**: four goroutines per parallel stage read an index channel and write into a pre-sized slice, so output order is expansion order. Each call is wrapped in `recover`. One total-deadline context is derived once; `Inventory` applies its own 10 s on top. After the deadline unstarted entries get `inventory-not-started`; deadline-caused codes are mapped to `inventory-timeout` with a message naming the bound that fired (60-second total or 10-second per call). `Inventory` re-checks only against its own first inspection, so after S5 the inventory's identity is compared with the S2 identity (backend, Git `git_dir`/`common_dir`/`worktree`/HEAD, jj `workspace_root`/`repository_dir`/operation id); a difference (whenever the embedded identity has status `ok`, whatever the inventory status; error inventories without a usable identity are not compared) yields `changed-repository` and the inventory is discarded. Every identity, discovery and inventory call has its own 10 s deadline inside the 60 s total.
4. **Entry validation** uses identity fields (Git `worktree`, `common_dir`, `git_dir`; jj `workspace_root`, `repository_dir`) from S2, so a failing check means no inventory of another repository is ever run or embedded.
5. **Duplicate rule**: first in expansion order wins; later duplicates run no inventory. Aliased paths therefore cost one identity inspection only.
6. **Minimal discovery** (research Decision 8, FR-009): `ls-tree -z -l HEAD -- .gitmodules` (regular file and size check), `config --blob <oid from the ls-tree record> --null --list --no-includes`, then at most 64 `ls-files --stage -z -- ':(literal)<path>'` queries with an exact path and mode compare. Overlap with Spec 021's registry reader is recorded; once 021 is on main, `repos_discover.go` can call its reader (also gaining gitlinks without entries) behind the same `discover(superproject) []path` seam. A later reuse needs no contract change.
7. **Facts and summary** are pure functions of the embedded inventories (spec derivation table), computed before the size budget omits any inventory, so omitted entries keep facts and the fact-based summary counts do not depend on omission (`by_status` and `inventories_omitted` do reflect it). Submodule entries get `dirty: unknown` (research Decision 12).
8. **Output budget**: inventories are added in order while the encoded size (using the existing reflect-based JSON size estimate pattern of `inventoryJSONBudget`, adapted to the new types) stays within 8 MiB; the rest become `output-limit` entries (status `error`, facts kept). The result is re-checked once before rendering; if the check still fails every entry becomes `output-limit` and the overall status is `error`, rather than invalid output.
9. **Exit status** is mapped in `internal/cli/repos.go` with the existing `runtimeFailure` for status 1; no new exit type.
10. **Human output** is rendered from the result into one buffer, one block per entry with escaped paths (control characters escaped as inventory's `%q` does), at most 16 attention workspaces per entry listed from `workspaces` with a "+N more" line, then the summary and limitations.
11. **No hosting dependency**: nothing from `internal/hosting` or Spec 022.

## Risks

- Whole-run wall time is bounded at 60 s by the total deadline; per call bounds are 10 s, and the runner adds up to 1 s of process wait after a deadline. With 16 entries and slow members the total can fire and is reported per entry.
- jj concurrency and `repository_dir` equality are unverified until the jj lanes run (tasks WI4).
- The reused inventory is limited (a 64 KiB `check-attr` output cap that fails repositories of roughly 150 to 400 tracked files, 1,024 records, gitlink and bare repositories always `partial`, a submodule's worktree record points at its Git directory): realistic members often yield `partial` or `error`. Disclosed in the spec; shipping first and fixing inventory in a separate spec is an open question.
- jj `stored`-only scope is chosen from code reading and is unverified until the jj lanes run.
- The embedded-schema is loose by necessity (no cross-schema `$ref`); the contract test closes the gap.
- Discovery misses a gitlink without a `.gitmodules` entry until Spec 021's reader is reused; documented as a limitation.
- A hostile superproject can still make Git read a member's configuration during inventory (isolation neutralizes execution, not reading); the pointer check limits which Git directories are read.

## Open questions for the maintainer

1. Add an ahead/behind observation to single-repository inventory (compatible schema extension) in a separate spec, then surface it in `repos` facts. Recommendation: yes, separate spec after this ships.
2. `--strict` exit status for dirty or stale members. Recommendation: defer; wait for user demand.
3. Raise `max_entries` above 16. Recommendation: keep 16, raise only with measurements of 60 s total behavior.
4. Should a path-list file (`--paths-from`) exist? Recommendation: no, bounded argv is enough for 16.
5. Ship before the inventory limits are lifted (see risks)? Recommendation: yes; failures are per entry and honest; open a separate spec to fix inventory for submodule checkouts, gitlink-bearing superprojects and larger repositories.
6. Exit 0 for entries that are `partial` only because of qualified inventory observations (an opt-in flag)? Recommendation: defer until users ask; keep exit 1 consistent with `stw inventory`.
