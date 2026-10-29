# Pre-release readiness: stw v0.2.0

**A snapshot, not a gate.** State read with `gh` and `git` (read-only) on 2026-10-29 at `origin/main`
`ac2a7da6` (CI push run green, 2026-10-29T04:49Z; the previous snapshot was 2026-10-27 at `ab4c657c`). It supersedes nothing: the tracker is
[issue #263](https://github.com/wstein/sourcesteward/issues/263) and the owner decides what may stay open. Nothing
here authorizes a build, tag or publication; those are the owner's steps in the
[release protocol](releasing-v0.2.0.md). The owner decisions of 2026-10-27 are in the [protocol](releasing-v0.2.0.md#owner-decisions). Re-read the state before tagging; the table goes stale with every merge.
Scope (owner decision 2026-10-25, extended 2026-10-26): the complete tidy wizard with the janitor phase 1 of
Spec 044, `stw sync`, `stw watch`, the `stw tidy` hint of `stw status`. Not in v0.2.0: the Spec 029 gates and the
implementation of Spec 016 (v0.3.0). `whr` is released separately.

## 1. Open pull requests

| Item | State | Needed before the tag |
| --- | --- | --- |
| [#323](https://github.com/wstein/sourcesteward/pull/323) Spec 044 P4b, terminal-authorized janitor apply | Merged (head `2b9a44f8`); `origin/main` was `23ef6edf` when this row was updated (2026-10-29). The D1 to D4 findings of the T016 dogfood run are fixed in [#332](https://github.com/wstein/sourcesteward/pull/332) (head `ecbbe8e9`); the rerun evidence is in the [rerun section](../specs/044-tidy-janitor-wizard/evidence/T016-dogfood.md#rerun-after-the-d1-d4-fix-stw-332-head-ecbbe8e9fb717db16387ef6679cd26317527f5aa-2026-10-29). Author agent: Codex | Done (owner decision 2026-10-26, "Erst Janitor-Wizard Phase 1, dann v0.2.0"; the D12 receipt and D16 are part of phase 1) |
| [#330](https://github.com/wstein/sourcesteward/pull/330) the end of input counts as no at every terminal prompt (fixes #326) | Merged 2026-10-28 (head `6aeb4a76`) | Done |
| [#327](https://github.com/wstein/sourcesteward/pull/327), [#328](https://github.com/wstein/sourcesteward/pull/328) release protocol, notes draft, readiness checklist, owner decisions of 2026-10-27 | Merged 2026-10-27 | Done; the notes draft and the protocol still describe #323 as an open Draft and need the owner review in section 5 |
| [#331](https://github.com/wstein/sourcesteward/pull/331) T016 dogfood evidence, [#332](https://github.com/wstein/sourcesteward/pull/332) fix of D1 to D4, [#333](https://github.com/wstein/sourcesteward/pull/333) T016 rerun | Merged 2026-10-28 (#331, #332) and 2026-10-29 (#333) | Done |
| [#334](https://github.com/wstein/sourcesteward/pull/334) feat(tidy): annotate read-only registration prune risk (Draft, head `ec5f105e`; its body names pending review and incomplete verification; the `check` and `suite` checks read FAILURE at this snapshot) | Open Draft | Whether it is part of v0.2.0: not decided here, gate decision: pending owner |
| This documentation pull request | Draft (named blocker: independent review pending) | Reviewed and merged before the tag, so the protocol is in the tagged tree |

No other pull request is open (`gh pr list --state open`).

## 2. Open issues that touch the release

| Item | State | Needed before the tag |
| --- | --- | --- |
| #263 tracker | open; its checkboxes are stale (see section 5) | Updated by the final documentation pull request; closed by the owner after the release |
| #70 real tidy apply evidence | open; owner decision 2026-10-25: step 1 a full plan, save, apply, restore run in a throwaway clone workspace, step 2 exactly one owner-selected safe element on the live workspace with before and after evidence | **Required** (owner decision 2026-10-27): a real apply on one live element chosen by the owner (typed `/dev/tty` prompt) is a precondition of the tag, in addition to T016; step 1 (throwaway clone) per the 2026-10-25 decision |
| #326 separate `stw tidy apply` accepts `y` then end of input | **closed** 2026-10-28 by #330 | Done (owner decision 2026-10-27: required before v0.2.0) |
| #62, #69, #71 Spec 026 gates | open, re-scoped | Not required unless the owner switches to the self-hosted flow (tracker #263 text) |
| #269 other lock-taking writers run through `runSandboxed` rely on a 1 s `WaitDelay`; #218 later jj steps report a stopped read as the wrong cause; #216 the 1 s survival window in the build-timeout test can fail under load; #195 `stw tidy` shows the derived base and source differently from `workspace plan`; #108 three surviving mutants of `revalidateReviewedDecision`; #98 relax apply for repositories with submodules (later); #93 `stw hosting` bounded wait for CI runs; #74 PR inspection lifecycle evidence; #72 read-only hosting governance specification; #64 read-only Ready inspection | all ten open (`gh issue view`, 2026-10-29) | gate decision: pending owner (open decision 2 in the protocol) |
| #306, #311, #315 | open (`gh issue view`, 2026-10-29) | v0.3.0 (owner decision 2026-10-26): not release items |

Closed since the tracker was written: #217, #196, #241, #252, #253, #255, #95 (`gh issue view`) and, since the previous snapshot, #326.

## 3. Verification work

| Item | State | Needed |
| --- | --- | --- |
| T016 dogfood dry run of the janitor on a copy of the live workspace, no apply (`specs/044-tidy-janitor-wizard/tasks.md:43`) | run 2026-10-28 and rerun 2026-10-29 after the D1 to D4 fix in #332; evidence in [T016-dogfood.md](../specs/044-tidy-janitor-wizard/evidence/T016-dogfood.md) (merged in #331). D1 to D4 are fixed: the rerun offers 3 of 1,248 candidates and plans them without error; the 29 locked or missing worktree registrations are cleaned by the owner by hand (owner decision 2026-10-29); planning of a directory row, D6 and D7 were not exercised | Independent review of the exact head of this documentation; the task checkbox is ticked by the last pull request of the wave |
| Dry run of the release workflow (`workflow_dispatch`, a build) | last dry runs succeeded 2026-10-22 and 2026-10-23, before 800+ commits; none on current `main` (`gh run list --workflow release.yml`, newest 2026-10-23) | **Required** once before the tag (owner decision 2026-10-27, step A5 of the protocol); a build, needs the owner's approval |
| Native CI on the commit to tag | green on `ac2a7da6` (`gh run list`, workflow CI, push, 2026-10-29) | Green on the final C (step A3) |
| Smoke of the janitor in the shipped archive | the workflow smoke runs `version`, `--help`, `inspect`, `status` only; `tidy`, `sync` and `workspace` are covered by the `ci.yml` conformance lanes (`specs/034-bootstrap-release/spec.md`, smoke) | Nothing new; stated so nobody expects more |

## 4. Repository settings (owner, read-only reads of 2026-10-29)

| Setting | Read | Needed |
| --- | --- | --- |
| Variable `STW_RELEASE_DRAFT` | `true` (`gh variable list`) | keep `true` so the owner inspects a draft |
| Environment `release` | exists, no protection rules (`gh api environments`) | as designed (Spec 026) |
| `main` ruleset | active (`gh api rulesets`; the detail below is from the 2026-10-27 read); deletion, non-fast-forward, linear history, pull request and required checks `check` and `Git/jj conformance (current) / suite`; bypass actor repository role 5 (admin) in mode `pull_request` | nothing |
| Tag ruleset for `v*` | **absent**: the rulesets read of 2026-10-29 lists only `main` | Hard stop (`docs/releasing.md:54-57`): the owner creates it before tagging (owner-only, `v*`, no delete or move); no waiver (owner decision 2026-10-27) |
| Release immutability | `enabled: false` (`gh api immutable-releases`, 2026-10-29) | Turn on before the tag (owner decision 2026-10-27) |
| Merge methods | rebase only, delete branch on merge (`gh api repos`: rebase true, squash false, merge commit false, delete branch true) | nothing |

## 5. Documents, status and trackers

| Item | State | Needed |
| --- | --- | --- |
| Release notes | draft: [release-notes/v0.2.0.md](release-notes/v0.2.0.md); the v0.1.0 body read "First release" | Owner review; edit the known-limits paragraph to the state at the tag |
| Release protocol | merged in #327 and #328: [releasing-v0.2.0.md](releasing-v0.2.0.md) | Owner review; fill it while running; T029 asks for the filled copy under `specs/045-release-wizard/protocol/` |
| User documentation of the janitor | merged: #322, #325 (`docs/tidy.md`, `docs/commands.md`, roadmap pointers) | After #323: the apply text must match its behavior |
| `docs/feature-status.json` | 044 reads `planned`; 032, 034, 035, 041 read `implemented`; 026 reads `planned` | Update 044 in the final documentation pull request, only with evidence (T016) |
| Task ticks and `docs/progress.md` | Spec 044 tasks are all unticked (T016 included, `specs/044-tidy-janitor-wizard/tasks.md:43`); specs/041 T012b, T012b1 to T012b4 unticked although the prune merged (#287 to #298); Spec 011 has open boxes | Ticked by the last pull request of the wave (`AGENTS.md` rules), not by this one |
| Tracker #263 body | (read 2026-10-29) still lists T009, T016, T018, T014 of Spec 032, #217, #196, #241 as open although they are done or closed | Refreshed together with the final documentation pull request |
| `tidy-overview` schema | frozen and unpublished (`docs/json-contracts.md:9`); owner decision 2026-10-25: not published with v0.2.0 | Nothing |
| US-English sweep | deferred until after the release (instruction of 2026-10-27); new and changed text in this pull request is US English | After the tag |

## 6. At tag time (owner)

1. Everything above is closed or accepted by you; write the accepted exceptions into tracker #263.
2. Choose C and V and walk the [release protocol](releasing-v0.2.0.md); each build or tag in it needs your own approval.
3. After publication: gitlink update in Anchorage, then the toolbox bundle only after `whr` is finished.
