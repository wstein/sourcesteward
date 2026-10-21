# Implementation Plan: Local restacking, first slice (`stw restack plan`)

**Branch**: `docs/015-restack-plan-first-slice` (specification); implementation branches follow the work packages | **Date**: 2026-10-21 | **Spec**: [spec.md](spec.md)

## Summary

Add the read-only `stw restack plan` for native Git: one operation in a new `internal/restack` package that resolves an explicit branch, old parent and new parent, replays the moved range with `merge-tree` plumbing in a private object quarantine, and returns one typed `restack-plan` result (text or JSON) with the evidence bundle of FR-013, the effects disclosure and the inert lease push and resync texts. It reuses the Spec 029/031 proof primitives (patch id, merge-noop gate, range-diff pairing) and the isolated native runner discipline. No apply, no jj, no push, no network, no verify commands in this slice.

## Technical Context

**Language/Version**: Go 1.27.2; existing supported minimum and current Git; jj and GitButler repositories are refused (`unsupported-backend`), the stated capability gap of D4.
**Dependencies**: Spec 010 contract vocabulary only; no Spec 012/014 code (D6).
**Testing**: real temporary Git repositories, test first; fixtures built with `git` plumbing under a throwaway identity passed by environment, never written to configuration. Mutation controls are listed in [tasks.md](tasks.md).
**Constraints**: offline; zero repository writes (sentinel over refs, config, index, worktree and object store); bounds in FR-018; cancellation honoured; no hooks, helpers, pager or prompt.

## Constitution and policy check

| Gate | Result |
| --- | --- |
| Git/jj Tier 1 peers | Git only with a stated capability gap and an explicit refusal for jj (D4); jj is Work package WP8 |
| Read-only first, inspectable plans | Yes (D5); apply is a later slice with its own plan and analysis |
| No undeclared executable/network | Fixed Git plumbing only; `internal/restack` joins `NETWORK_FREE_PACKAGES` in `scripts/check-architecture.py`; process starts only in reviewed files |
| Published contracts preserved | New schema file only; allowlist/copy tests extended; no existing byte changes |
| No Git configuration write | Identity from environment and flags only (D9, FR-015) |
| Push/force | Text only, `--force-with-lease=ref:sha` form, never run (D8) |

## Design

1. **Resolve** (`internal/restack/input.go`): validate syntax (FR-011), resolve through the isolated runner to commit ids, observe `old_head`, compute both merge bases, collect the moved range with `rev-list --topo-order --reverse`; refuse per FR-014 before any evidence work.
2. **Hazards** (`hazards.go`): reuse the Spec 029/031 repository, config, attribute and hook safeguards; report only names. Any hazard on a path the replay touches refuses (`repository-hazard`), because `merge-tree` may run merge drivers.
3. **Replay** (`replay.go`): for each moved commit, `merge-tree --write-tree --merge-base=<commit^> <tip> <commit>`; on a clean result `commit-tree` with the original author, the replay committer from the environment, in a quarantine object directory (`GIT_OBJECT_DIRECTORY` private, repository objects as alternates). The first conflict stops replay and is reported with its paths, kinds and allowlist class; later commits are `unpaired`. The quarantine is removed on every path and a sentinel test compares the object store.
4. **Evidence** (`evidence.go`): `git range-diff` and `git cherry` through the runner; tree equivalence of touched paths for `+` rows (reusing the patch-id and touched-path code of `internal/workspace/tidy_v3_integration_patchid.go` after extraction, WP2); own-diff patch-id equality; identity check; overlap indicator from `old_parent..new_parent` paths against moved paths.
5. **Effects and texts** (`effects.go`, `texts.go`): affected refs and worktrees from `for-each-ref --contains` and `worktree list --porcelain`; hook names by file presence only; lease and resync texts with strict ref-name validation and quoting.
6. **Result and CLI** (`result.go`, `internal/cli/restack.go`): one typed result, human rendering from it, `schemas/v1/restack-plan.schema.json`, exit mapping. The CLI imports only the reviewed `restack` operation (architecture check updated).

Reuse decision: extract the pure proof helpers of the tidy integration run into a neutral package or exported helpers with unchanged characterization tests first (refactor commit, no behavior change); do not make `restack` import tidy plan types.

## Work packages and effort

S about 0.5 author day, M about 1, L about 2 to 3 (each includes the repo's own tests, `make check` and one independent review; review rounds and CI waits are extra). Numbers are those of the 2026-10-21 estimate where valid.

| WP | Content | Effort | Depends on |
| --- | --- | --- | --- |
| WP0 | This design: plan, tasks, contracts, data model, independent design review, analysis (estimate 015-0) | L (done as draft in this PR; review rounds extra) | owner decisions D4-D13 |
| WP1 | Minimal explicit input and resolver, refusals, bounds (estimate 015-1 plus refusals of 015-3) | M | WP0 |
| WP2 | Evidence library: extract proof helpers, range-diff pairing, cherry, tree equivalence, own-diff equality, identity check (015-2) | M | WP1 |
| WP3 | Replay and conflict prediction in quarantine, effects, hazards, texts (015-3 remainder) | L | WP2 |
| WP4 | Result, schema, CLI, human output, architecture and docs checks | M | WP3 |
| WP5 | Conformance: minimum/current Git, effect sentinels, mutation controls, quickstart evidence (015-9, plan part) | M | WP4 |
| WP6 | Keep-both ledger classification from configuration (015-4); the plan slice reports `allowlisted` only | M | WP3 |
| WP7 | `restack apply` Git: recovery refs and journal, compare-and-swap ref update, revalidation, stop on conflict (015-5); own spec amendment and review | L | WP5; Spec 010 |
| WP8 | jj peer with change/commit identity mapping (015-6) | L | WP7 |
| WP9 | Push with lease: Spec 016 L2 amendment (015-7), outside Spec 015 | M | Spec 016 FR-006, Spec 024 |
| WP10 | Declared verify commands in a sandbox (015-8) | M | owner decision, configuration contract |

First slice (plan only, WP1 to WP5): about 5 to 6 author days with one editing author, sequential on `internal/restack` and the shared CLI root; WP6 may follow in parallel. Together with WP0 the estimate's slice 2 of 6 to 8 days stands. Git-only plan plus apply (WP1 to WP7): about 8 to 10 days; with jj (WP8) 14 to 16 days.

## Risks

| Risk | Handling |
| --- | --- |
| `merge-tree` runs a configured merge driver | hazard gate refuses before any call, as Spec 031 does |
| Quarantine objects leak into the repository | private object directory, alternates only for reads, sentinel test, removal on every path |
| Predicted tree differs from a real rebase (renames, whitespace, rerere) | stated in `limitations` (`prediction-is-not-apply`); apply (WP7) recomputes and revalidates |
| False confidence from `evidence-clean` | notice and `limitations` in every result; overlap indicator; verify commands are a later slice |
| Identity unknown in a sandbox | check is `unknown`, never `pass`; flag `--identity` |
| Shared files `cli/root.go`, schema allowlists with other authors | rebase before CLI wiring (WP4); keep each commit to one logical change |

## Open questions for review

1. Whether read-only `git config user.*` may feed the expected identity when no flag or environment value exists (currently: no).
2. Whether `--keep-both-config` belongs in WP1 or only in WP6.
3. Whether `range-diff` should also be available as post-check text after a by-hand rebase (printed in quickstart only).
