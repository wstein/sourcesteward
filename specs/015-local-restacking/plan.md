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
| No Git configuration write | Expected identity from the `--identity` flag only; fixed constant replay identity; nothing written (D9, FR-015) |
| Push/force | Text only, `--force-with-lease=ref:sha` form, never run (D8) |

## Design

1. **Resolve** (`internal/restack/input.go`): validate syntax (FR-011), resolve through the isolated runner to commit ids, observe `old_head`, compute both merge bases, collect the moved range with `rev-list --topo-order --reverse`; refuse per FR-014 before any evidence work.
2. **Hazards** (`hazards.go`): reuse the Spec 029/031 repository, config, attribute and hook safeguards; report only names. Any hazard on a path the replay touches refuses (`repository-hazard`), because `merge-tree` may run merge drivers.
3. **Replay** (`replay.go`): for each moved commit, `merge-tree --write-tree --merge-base=<commit^> <tip> <commit>`; on a clean result `commit-tree` with the original author (`GIT_AUTHOR_NAME`, `GIT_AUTHOR_EMAIL` and `GIT_AUTHOR_DATE=@<unix> <zone>` of the moved commit, so `git range-diff` pairs it `=`) and a fixed constant committer given only through the environment (`GIT_COMMITTER_NAME=stw-restack-plan`, `GIT_COMMITTER_EMAIL=restack-plan@invalid`, `GIT_COMMITTER_DATE=1970-01-01T00:00:00Z`; process `HOME`, global and system configuration neutralized), so no repository-local or global `user.*` is ever read; the replay commits live only in the quarantine and are never pushed, so their committer identity is never evidence about the branch. The quarantine is a private object directory (`GIT_OBJECT_DIRECTORY` private, repository objects as alternates). The first conflict stops replay and is reported with its paths, kinds and allowlist class; later commits are `unpaired`. The quarantine is removed on every path and a sentinel test compares the object store.
4. **Evidence** (`evidence.go`): `git range-diff` and `git cherry` through the runner; tree equivalence of touched paths for `+` rows (reusing the patch-id and touched-path code of `internal/workspace/tidy_v3_integration_patchid.go` after extraction, WP2); own-diff patch-id equality; identity check; overlap indicator from `old_parent..new_parent` paths against moved paths.
5. **Effects and texts** (`effects.go`, `texts.go`): affected refs and worktrees from `for-each-ref --contains` and `worktree list --porcelain`; hook names by file presence only; lease and resync texts with strict ref-name validation and quoting.
6. **Result and CLI** (`result.go`, `internal/cli/restack.go`): one typed result, human rendering from it, `schemas/v1/restack-plan.schema.json`, exit mapping. The CLI imports only the reviewed `restack` operation (architecture check updated).

Implementation note (2026-10-21): the helpers were not extracted; `internal/restack` has its own small proof helpers behind its own reviewed runner and `internal/workspace` is unchanged (T004). Original reuse decision: extract the pure proof helpers of the tidy integration run into a neutral package or exported helpers with unchanged characterization tests first (refactor commit, no behavior change); do not make `restack` import tidy plan types.

## Work packages and effort

S about 0.5 author day, M about 1, L about 2 to 3 (each includes the repo's own tests, `make check` and one independent review; review rounds and CI waits are extra). Numbers are those of the 2026-10-21 estimate where valid.

| WP | Content | Effort | Depends on |
| --- | --- | --- | --- |
| WP0 | This design: plan, tasks, contracts, data model, independent design review, analysis (estimate 015-0) | L (done as draft in this PR; review rounds extra) | owner decisions D4-D13 |
| WP1 | Minimal explicit input and resolver, refusals, bounds (estimate 015-1 plus refusals of 015-3) | M | WP0 |
| WP2 | Evidence library: own proof helpers (no extraction from `internal/workspace`, T004 skipped), range-diff pairing, cherry, tree equivalence, own-diff equality, identity check (015-2) | M | WP1 |
| WP3 | Replay and conflict prediction in quarantine, effects, hazards, texts (015-3 remainder) | L | WP2 |
| WP4 | Result, schema, CLI, human output, architecture check | M | WP3 |
| WP5 | Conformance: minimum/current Git, effect sentinels, mutation controls, quickstart evidence (015-9, plan part) | M | WP4 |
| WP6 | Keep-both ledger classification from stw configuration (015-4), including the `--keep-both-config` flag and its tasks; the plan slice has no such flag and classifies nothing | M | WP3 |
| WP7 | `restack apply` Git: recovery refs and journal, compare-and-swap ref update, revalidation, stop on conflict (015-5); own spec amendment and review | L | WP5; Spec 010 |
| WP8 | jj peer with change/commit identity mapping (015-6) | L | WP7 |
| WP9 | Push with lease: Spec 016 L2 amendment (015-7), outside Spec 015 | M | Spec 016 FR-006, Spec 024 |
| WP10 | Declared verify commands in a sandbox (015-8) | M | owner decision, configuration contract |

First slice (plan only, WP1 to WP5, without the keep-both flag): about 4 to 5 author days with one editing author, sequential on `internal/restack` and the shared CLI root; WP6 may follow in parallel. Together with WP0 the estimate's slice 2 of 6 to 8 days stands. Git-only plan plus apply (WP1 to WP7): about 8 to 10 days; with jj (WP8) 14 to 16 days.

## Quarantine evidence (reviewer experiment, not a guarantee)

An independent reviewer measured with Git 2.56.0 in a scratch repository, global and system configuration set to `/dev/null`: with `GIT_OBJECT_DIRECTORY=<quarantine>` and `GIT_ALTERNATE_OBJECT_DIRECTORIES=<real objects>`, `merge-tree --write-tree` (clean and conflicting), `commit-tree`, `range-diff`, `cherry` and `patch-id` left the real `.git` byte-identical (sha256 listing and `find -newer`). A plain `git merge-tree --write-tree` without the quarantine writes objects into the real store (9 to 11 objects). Not verified on other Git versions or for pack-threshold triggered packing. It matches the runner environment of `internal/workspace/tidy_v3_integration_run.go` (`gc.auto=0`, `core.hooksPath=/dev/null`, `GIT_OPTIONAL_LOCKS=0`). The same experiment becomes a test on the CI Git versions (T012). It also showed that `commit-tree` reads a repository-local `user.*` when the environment has no identity, which is why the replay identity is fixed (FR-015).

## Known limits

- Semantic breaks after a clean replay are not detected; only the overlap indicator is reported.
- The prediction can differ from a real rebase (renames, rerere, whitespace options); apply recomputes it.
- Exotic traps (author-date edge cases, unusual attributes beyond the hazard gate, other Git versions than the CI lanes, pack-threshold packing in the quarantine) are not covered by fixtures; the plan then refuses or reports `unknown` where it can detect them, and otherwise the limit stands.
- Mutation controls are required for the safety-critical tasks only (refusals, identity, quarantine, texts, outcome table); other tasks rely on their tests.

## Risks

| Risk | Handling |
| --- | --- |
| `merge-tree` runs a configured merge driver | hazard gate refuses before any call, as Spec 031 does |
| Quarantine objects leak into the repository | private object directory, alternates only for reads, sentinel test, removal on every path |
| Predicted tree differs from a real rebase (renames, whitespace, rerere) | stated in `limitations` (`prediction-is-not-apply`); apply (WP7) recomputes and revalidates |
| False confidence from `evidence-clean` | notice and `limitations` in every result; overlap indicator; verify commands are a later slice |
| Identity unknown in a sandbox | check is `unknown`, never `pass`; `--identity` is required for a pass; the environment never counts |
| Shared files `cli/root.go`, schema allowlists with other authors | rebase before CLI wiring (WP4); keep each commit to one logical change |

## Open questions for review

1. (Resolved by the review) No `user.*` configuration feeds the expected identity; repository-local `user.*` is reported as a hazard.
2. (Resolved by the review) `--keep-both-config` moves to WP6; the first slice has no configuration input.
3. Whether `range-diff` should also be available as post-check text after a by-hand rebase (printed in quickstart only).
