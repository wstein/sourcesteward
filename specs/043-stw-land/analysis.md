# Current Owner-Decision Analysis: Spec 043

**Owner decisions: 2026-10-25. Author cross-artifact analysis only.** All former
OQ IDs are settled and indexed in [the decision index](../016-stacked-pr-coordination/research.md#cross-spec-decision-index).
The [concrete profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) distinguish workspace .work metadata from
external capture payloads, trusted policy, host-only optional local015 re-sign
and CLI exit-contract revision2. No new implementation/native/helper/forge test
or independent CLEAR is claimed. Historical implementation evidence stays at
its recorded revision; current hidden015code still returns3 until the new task.

## Current findings and gates

| ID | Disposition | Remaining evidence |
| --- | --- | --- |
| D1 | All39 owner directions are DECIDED, including --online-only/single-repo and default .work saved-plan metadata. | Exact current artifact review and source/native conformance; no invented native acceptance. |
| D2 | Metadata and external selected-byte/safety stores are distinct roles with explicit modes/race/layout/quota/export-risk guards. | Red-first shared store and role-separation tasks; no auto-ignore edits/expiry. |
| D3 | Optional actual re-sign belongs only to015 fixed trusted HOST helper/verifier; no stw keys, no helper means unavailable. | Measured profile/external-agent trust limits, bottom-up signed parents and final verified SHA before review;016 receipt validation only. |
| D4 | Target CLI exit-contract revision2 is0/1/2, no legacy3 selector; JSON schema 1 frozen independently. | Red dispatch/help/JSON cases and root.go/restack.go mapping task; CURRENT code 3 not misreported changed. |

The endpoint, ownership and constitution amendments in the spec reconciliation
section remain OPEN for separate owner decisions. Settling the eight indexed OQs
does not accept those amendments or enable human landing.

## Individual requirement coverage and literal future tests

Each current definition has one row, real task IDs and a literal future test.
Coverage is planned design mapping; unchecked tasks are not execution evidence.

| Requirement | Task IDs | Literal future test idea |
| --- | --- | --- |
| FR-001 | T007 | Fixture: Failing tests then agent-mode refusal of apply before any write or credentialed read (STW_AGENT, each marker, no TTY). |
| FR-002 | T006 | Fixture: `--plan` output (human, `--json`) with exact argv, all failing preconditions listed, agent-mode labelling; schema `schemas/v1/land.schema.json` with valid and invalid fixtures; assert no writes beyond the disclosed private-namespace fetch (no user ref, no config, no process other than reads). |
| FR-003 | T008 | Fixture: Typed `/dev/tty` confirmation (`land NUMBER SHA8`): wrong answer 3 times, no terminal, `--yes` ignored/usage error, stdin never read. |
| FR-004 | T007, T015 | Fixture: Failing tests then agent-mode refusal of apply before any write or credentialed read (STW_AGENT, each marker, no TTY). |
| FR-010 | T004 | Fixture: Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). |
| FR-011 | T004 | Fixture: Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). |
| FR-012 | T004, T016a | Fixture: Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). |
| FR-013 | T004 | Fixture: Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). |
| FR-014 | T004, T012a | Fixture: Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). |
| FR-015 | T004, T012 | Fixture: Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). |
| FR-016 | T005, T016b | Fixture: Failing tests then fast-forward by Git: `merge-base --is-ancestor` over a temporary repository with a bare remote; API says clean but Git says diverged refuses. |
| FR-017 | T016, T019, T020 | Fixture: Identity check: expectation comes only from trusted workspace policy, global Git identity corroborates only; foreign local/worktree/global identity gives `identity-leak`, missing trusted source refuses; throwaway author or committer refused; nothing written. |
| FR-018 | T004 | Fixture: Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). |
| FR-019 | T004 | Fixture: Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). |
| FR-020 | T009, T010, T018 | Fixture: Failing tests then the landing push as a plain `git push --porcelain <remote> <sha>:refs/heads/<default>`; a moved default branch (`fetch first`) gives `base-moved` and changes nothing; mutation probe that adds `--force*`, a lease or a `+` refspec fails a test. |
| FR-021 | T010 | Fixture: Argument-vector allowlist test of every git and gh argument vector (FR-033); no other write reachable. |
| FR-022 | T011 | Fixture: Bounded wait for MERGED (fake clock under `testing/synctest`), `landed-not-marked`, `merged-other-sha`. |
| FR-023 | T011, T013 | Fixture: Bounded wait for MERGED (fake clock under `testing/synctest`), `landed-not-marked`, `merged-other-sha`. |
| FR-030 | T012a, T012 | Fixture: Failing tests then FR-034: after a retarget everything is re-read; `mergeable` null or pending checks refuse before the push; FR-031 order and its failure branch print `gh pr edit CHILD --base OLD_BASE` and run nothing. |
| FR-031 | T002, T002a, T012a, T018 | Fixture: **Owner-run** (ruleset settings are an owner operation): a scratch repository that is public or on a plan that serves rulesets (a private free repository answers 403 and cannot hold the shape), with a copy of the real sourcesteward ruleset and `delete_branch_on_merge` true. Measure the direct owner push and, for a stack, what happens to the child when the bottom branch is deleted after the merge: GitHub may auto-retarget the dependent pull request or close it; list both as possible and record whichever is measured (OQ2, OQ4). Append to research.md as measurements and add the shape to `internal/land/shapes.json`, the single allowlist. |
| FR-034 | T012a | Fixture: Failing tests then FR-034: after a retarget everything is re-read; `mergeable` null or pending checks refuse before the push; FR-031 order and its failure branch print `gh pr edit CHILD --base OLD_BASE` and run nothing. |
| FR-032 | T012 | Fixture: Stack order: retarget children before deletion; deletion only with `--delete-branch`, only the PR's own head ref, lease on the head SHA; closed-child report; failure of one retarget blocks deletion. |
| FR-033 | T010 | Fixture: Argument-vector allowlist test of every git and gh argument vector (FR-033); no other write reachable. |
| FR-040 | T014 | Fixture: Ruleset refusal: remote rejection text reported verbatim, escaped, bounded to 2 KiB, nothing else changed. |
| FR-041 | T013 | Fixture: Replay/idempotence: rerun after push-only, after retarget-only, after full success; no duplicate effect. |
| FR-042 | T009 | Fixture: Failing tests then the landing push as a plain `git push --porcelain <remote> <sha>:refs/heads/<default>`; a moved default branch (`fetch first`) gives `base-moved` and changes nothing; mutation probe that adds `--force*`, a lease or a `+` refspec fails a test. |
| FR-043 | T015, T019, T020 | Create-only linked intent/outcome facts keep old bytes unchanged; forged/missing chains refuse, quota reserves bounded outcome before effects, and missing completion is unknown. Check ignored/untracked admission and no stw staging/commit/push; owner export remains residual risk. |
| FR-044 | T013 | Fixture: Replay/idempotence: rerun after push-only, after retarget-only, after full success; no duplicate effect. |
| FR-050 | T006, T017 | Fixture: `--plan` output (human, `--json`) with exact argv, all failing preconditions listed, agent-mode labelling; schema `schemas/v1/land.schema.json` with valid and invalid fixtures; assert no writes beyond the disclosed private-namespace fetch (no user ref, no config, no process other than reads). |
| FR-051 | T006, T017 | Fixture: `--plan` output (human, `--json`) with exact argv, all failing preconditions listed, agent-mode labelling; schema `schemas/v1/land.schema.json` with valid and invalid fixtures; assert no writes beyond the disclosed private-namespace fetch (no user ref, no config, no process other than reads). |
| FR-052 | T006, T017 | Fixture: `--plan` output (human, `--json`) with exact argv, all failing preconditions listed, agent-mode labelling; schema `schemas/v1/land.schema.json` with valid and invalid fixtures; assert no writes beyond the disclosed private-namespace fetch (no user ref, no config, no process other than reads). |

## Metrics and task state

31 individual FR/SC definitions, 24 task ids, 0 historical
checked tasks; 24 unchecked. Coverage 31/31 (100% design
mapping), zero requirement orphans. No implementation checkbox is changed by
owner answers or this analysis. All new exit/store/signing tasks are unchecked.
Task dependencies remain red test → implementation → native/effect conformance
→ exact-source independent review. Spec043 human-only landing remains separate.

## Compatibility and acceptance boundary

012 online means only --online; old alias/batch unavailable now. 013 payloads
and safety copies stay external; .work only holds private minimal metadata with
explicit export risk. 014 preflight never signs or mutates originals. 015 D7/D8
and unsigned D9 prediction stay settled; optional actual signing needs the host
profile and final signed-SHA review. 016 remote flags cannot invoke that local
helper. 043 identity authority is trusted workspace policy, global config only
corroborates, with existing human confirmation/shape/retarget/deletion gates.
No build, installation, helper run, schema-file edit or live write is claimed.
