# Pre-release readiness: stw v0.2.0

**A snapshot, not a gate.** State read with `gh` and `git` (read-only) on 2026-10-27 at `origin/main`
`ab4c657c` (CI push run green, 2026-10-27T18:16Z). It supersedes nothing: the tracker is
[issue #263](https://github.com/wstein/sourcesteward/issues/263) and the owner decides what may stay open. Nothing
here authorizes a build, tag or publication; those are the owner's steps in the
[release protocol](releasing-v0.2.0.md). "Owner decision N" refers to the [table in the protocol](releasing-v0.2.0.md#owner-decisions-needed-before-the-tag). Re-read the state before tagging; the table goes stale with every merge.
Scope (owner decision 2026-10-25, extended 2026-10-26): the complete tidy wizard with the janitor phase 1 of
Spec 044, `stw sync`, `stw watch`, the `stw tidy` hint of `stw status`. Not in v0.2.0: the Spec 029 gates and the
implementation of Spec 016 (v0.3.0). `whr` is released separately.

## 1. Open pull requests

| Item | State | Needed before the tag |
| --- | --- | --- |
| [#323](https://github.com/wstein/sourcesteward/pull/323) Spec 044 P4b, terminal-authorized janitor apply | Draft, head `6bb53928`, merge state CLEAN, all checks pass; Draft blockers named in its body: the foreman's exact-head security review and ledger entry. Author agent: Codex | **Required**: merged (owner decision 2026-10-26, "Erst Janitor-Wizard Phase 1, dann v0.2.0"; the D12 receipt and D16 are part of phase 1) |
| This documentation pull request | Draft | Reviewed and merged before the tag, so the protocol is in the tagged tree |

No other pull request is open.

## 2. Open issues that touch the release

| Item | State | Needed before the tag |
| --- | --- | --- |
| #263 tracker | open; its checkboxes are stale (see section 5) | Updated by the final documentation pull request; closed by the owner after the release |
| #70 real tidy apply evidence | open; owner decision 2026-10-25: step 1 a full plan, save, apply, restore run in a throwaway clone workspace, step 2 exactly one owner-selected safe element on the live workspace with before and after evidence | Per that decision it gates the tag: step 1 and step 2 recorded, unless the owner drops them (owner decision 7). Step 2 needs the owner to approve the concrete element |
| #326 separate `stw tidy apply` accepts `y` then end of input | open, existing behavior, outside #323 | Owner decision 8: fix, or list as a known limit (the notes draft lists it) |
| #62, #69, #71 Spec 026 gates | open, re-scoped | Not required unless the owner switches to the self-hosted flow (tracker #263 text) |
| #269, #218, #216, #195, #108, #98, #93, #74, #72, #64 | open | Owner to confirm that none gates the tag; none is named in the scope above |
| #306, #311, #315 | open | v0.3.0 (owner decision 2026-10-26): not release items |

Closed since the tracker was written: #217, #196, #241, #252, #253, #255, #95 (`gh issue view`).

## 3. Verification work

| Item | State | Needed |
| --- | --- | --- |
| T016 dogfood dry run of the janitor on a copy of the live workspace, no apply (`specs/044-tidy-janitor-wizard/tasks.md:43`) | open; no evidence file exists (`specs/044-tidy-janitor-wizard/evidence` is absent) | **Required** (owner decision 2026-10-26): run it after #323 merges, record it in a pull request body with an independent review of the exact head |
| Dry run of the release workflow (`workflow_dispatch`, a build) | last dry runs succeeded 2026-10-22 and 2026-10-23, before 800+ commits; none on current `main` | Owner decision 3 (step A5 of the protocol); needs the owner's approval |
| Native CI on the commit to tag | green on `ab4c657c` | Green on the final C (step A3) |
| Smoke of the janitor in the shipped archive | the workflow smoke runs `version`, `--help`, `inspect`, `status` only; `tidy`, `sync` and `workspace` are covered by the `ci.yml` conformance lanes (`specs/034-bootstrap-release/spec.md`, smoke) | Nothing new; stated so nobody expects more |

## 4. Repository settings (owner, read-only reads of 2026-10-27)

| Setting | Read | Needed |
| --- | --- | --- |
| Variable `STW_RELEASE_DRAFT` | `true` | keep `true` so the owner inspects a draft |
| Environment `release` | exists, no protection rules | as designed (Spec 026) |
| `main` ruleset | active; deletion, non-fast-forward, linear history, pull request and required checks `check` and `Git/jj conformance (current) / suite`; bypass actor repository role 5 (admin) in mode `pull_request` | nothing |
| Tag ruleset for `v*` | **absent**: the rulesets read succeeded and lists only `main` | Hard stop (`docs/releasing.md:54-57`): create it, or an explicit written owner waiver (decision 1) |
| Release immutability | `enabled: false` | Owner decision 2 |
| Merge methods | rebase only, delete branch on merge | nothing |

## 5. Documents, status and trackers

| Item | State | Needed |
| --- | --- | --- |
| Release notes | draft: [release-notes/v0.2.0.md](release-notes/v0.2.0.md); the v0.1.0 body read "First release" | Owner review; edit the known-limits paragraph to the state at the tag |
| Release protocol | this pull request: [releasing-v0.2.0.md](releasing-v0.2.0.md) | Owner review; fill it while running; T029 asks for the filled copy under `specs/045-release-wizard/protocol/` |
| User documentation of the janitor | merged: #322, #325 (`docs/tidy.md`, `docs/commands.md`, roadmap pointers) | After #323: the apply text must match its behavior |
| `docs/feature-status.json` | 044 reads `planned`; 032, 034, 035, 041 read `implemented`; 026 reads `planned` | Update 044 in the final documentation pull request, only with evidence (T016) |
| Task ticks and `docs/progress.md` | Spec 044 tasks are all unticked; specs/041 T012b, T012b1 to T012b4 unticked although the prune merged (#287 to #298); Spec 011 has open boxes | Ticked by the last pull request of the wave (`AGENTS.md` rules), not by this one |
| Tracker #263 body | still lists T009, T016, T018, T014 of Spec 032, #217, #196, #241 as open although they are done or closed | Refreshed together with the final documentation pull request |
| `tidy-overview` schema | frozen and unpublished (`docs/json-contracts.md:9`); owner decision 2026-10-25: not published with v0.2.0 | Nothing |
| US-English sweep | deferred until after the release (instruction of 2026-10-27); new and changed text in this pull request is US English | After the tag |

## 6. At tag time (owner)

1. Everything above is closed or accepted by you; write the accepted exceptions into tracker #263.
2. Choose C and V and walk the [release protocol](releasing-v0.2.0.md); each build or tag in it needs your own approval.
3. After publication: gitlink update in Anchorage, then the toolbox bundle only after `whr` is finished.
