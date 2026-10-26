# Tasks: `stw land`

> **Owner-decision amendment — 2026-10-25:** all former OQ IDs are decided;
> [current profiles](../016-stacked-pr-coordination/contracts/owner-decisions.md) supersede earlier undecided recommendations.
> Human-only landing and measured-shape/retarget gates remain; trusted workspace policy is authority.
> This is a documentation target, not implementation or native acceptance.

**Reconciliation gate**: endpoint/ownership/constitution amendments in spec.md remain OPEN for separate owner decisions; the dated OQ answers do not approve them. No task may enable the affected effects before that review.

**Gate**: all runtime boxes stay unchecked until the design review of the exact head of this spec is CLEAR and the dated owner decision profile has been implemented. OQ2 and OQ4 are hard gates for apply (`land-unmeasured`), not defaults: T002 comes first and
its result must be in research.md before T009. Tests use temporary repositories with a bare remote and a fake `gh` transport (Spec 022
fixtures); never a real repository. Failing test first for each task. Each slice updates `docs/commands.md` and `docs/json-contracts.md`.

**Stack landing amendment (2026-10-26)**: slice 5 below adds `--stack`. It is gated by the measurements E1 to E3 (research.md), the independent
security review T023 **before any code** of it, and, like all of this feature, the first feature after v0.2.0. All boxes stay unchecked.

## Slice 0 - Design

- [ ] T001 Independent review of the exact committed head of this spec (strongest tier, executed checks).
- [ ] T002 **Owner-run** (ruleset settings are an owner operation): a scratch repository that is public or on a plan that serves rulesets (a private free repository answers 403 and cannot hold the shape), with a copy of the real sourcesteward ruleset and `delete_branch_on_merge` true. Measure the direct owner push and, for a stack, what happens to the child when the bottom branch is deleted after the merge: GitHub may auto-retarget the dependent pull request or close it; list both as possible and record whichever is measured (OQ2, OQ4). Append to research.md as measurements and add the shape to `internal/land/shapes.json`, the single allowlist. (Coverage: FR-031).
- [ ] T002a Failing tests then the `land-unmeasured` gate: apply refuses for an unrecorded shape, `--plan` works and lists it; the single allowlist `internal/land/shapes.json` keyed by normalised shape; an unreadable shape (403) never matches. (Coverage: FR-031).

## Slice 1 - `--plan` (read-only)

- [ ] T003 Failing tests for argument parsing and exit 2: missing `--pr`/`--head`/`--reviewed`, `--head` not 40 hex, `--yes`, `--json` with `--human`, bad `--wait`.
- [ ] T004 Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven). (Coverage: FR-010, FR-011, FR-012, FR-013, FR-014, FR-015, FR-018, FR-019).
- [ ] T005 Failing tests then fast-forward by Git: `merge-base --is-ancestor` over a temporary repository with a bare remote; API says clean but Git says diverged refuses. (Coverage: FR-016).
- [ ] T006 `--plan` output (human, `--json`) with exact argv, all failing preconditions listed, agent-mode labelling; schema `schemas/v1/land.schema.json` with valid and invalid fixtures; assert no writes beyond the disclosed private-namespace fetch (no user ref, no config, no process other than reads). (Coverage: FR-002, FR-050, FR-051, FR-052).

## Slice 2 - Mode and confirmation

- [ ] T007 Failing tests then agent-mode refusal of apply before any write or credentialed read (STW_AGENT, each marker, no TTY). (Coverage: FR-001, FR-004).
- [ ] T008 Typed `/dev/tty` confirmation (`land NUMBER SHA8`): wrong answer 3 times, no terminal, `--yes` ignored/usage error, stdin never read. (Coverage: FR-003).

## Slice 3 - Apply

- [ ] T009 Failing tests then the landing push as a plain `git push --porcelain <remote> <sha>:refs/heads/<default>`; a moved default branch (`fetch first`) gives `base-moved` and changes nothing; mutation probe that adds `--force*`, a lease or a `+` refspec fails a test. (Coverage: FR-020, FR-042).
- [ ] T010 Argument-vector allowlist test of every git and gh argument vector (FR-033); no other write reachable. (Coverage: FR-020, FR-021, FR-033).
- [ ] T011 Bounded wait for MERGED (fake clock under `testing/synctest`), `landed-not-marked`, `merged-other-sha`. (Coverage: FR-022, FR-023).
- [ ] T012a Failing tests then FR-034: after a retarget everything is re-read; `mergeable` null or pending checks refuse before the push; FR-031 order and its failure branch print `gh pr edit CHILD --base OLD_BASE` and run nothing. (Coverage: FR-014, FR-030, FR-031, FR-034).
- [ ] T012 Stack order: retarget children before deletion; deletion only with `--delete-branch`, only the PR's own head ref, lease on the head SHA; closed-child report; failure of one retarget blocks deletion. (Coverage: FR-015, FR-030, FR-032).
- [ ] T013 Replay/idempotence: rerun after push-only, after retarget-only, after full success; no duplicate effect. (Coverage: FR-023, FR-041, FR-044).
- [ ] T014 Ruleset refusal: remote rejection text reported verbatim, escaped, bounded to 2 KiB, nothing else changed. (Coverage: FR-040).
- [ ] T015 Journal lines before and after each effect; local only; none committed or pushed. (Coverage: FR-004, FR-043).
- [ ] T016 Identity check: expectation comes only from trusted workspace policy, global Git identity corroborates only; foreign local/worktree/global identity gives `identity-leak`, missing trusted source refuses; throwaway author or committer refused; nothing written. (Coverage: FR-017).
- [ ] T016a FR-012 fixtures: ruleset list, 404 protection, 403 ruleset, empty required list, default branch not the PR base. (Coverage: FR-012).
- [ ] T016b FR-016: merge-base exit 128 (old tip absent) is `object-missing`, never a fast-forward. (Coverage: FR-016).

## Slice 4 - Docs and acceptance

- [ ] T017 `docs/commands.md`, `docs/json-contracts.md`, `docs/agent-guide.md` (agents run only `--plan`); smoke script on a local bare remote. (Coverage: FR-050, FR-051, FR-052).
- [ ] T018 First dogfood: plan only, then one trivial docs pull request in a scratch repository, owner at the terminal; result recorded in research.md. (Coverage: FR-020, FR-031).

## Owner-decision profile tasks (all unchecked)

- [ ] T019 [FR-017, FR-043] Write red tests in internal/land/policy_store_test.go for trusted workspace owner source/global corroboration, missing-source refusal, typed .work land-action namespace/private-mode/ignore-untracked/quota/fsync admission, immutable linked intent/outcome old bytes unchanged, forged/missing chain refusal and reserved count/bytes before effects with missing completion unknown and unresolved no-expiry; no key/signing/helper fallback or change to human-only confirmation/shape gates; unreadable/unmet signature requirement or unsigned-vs-final signed SHA review mismatch refuses, 043 never signs. (Coverage: FR-017, FR-043).
- [ ] T020 [FR-017, FR-043] After T019 is red, integrate shared trusted-policy and metadata-store profile in internal/land/policy_store.go, preserving exact final-reviewed-SHA landing, human-only apply, existing conditional retarget/deletion and no signature creation. Use 015 final signed receipt if selected earlier; no local signing under landing authorization. (Coverage: FR-017, FR-043).

## Slice 5 - Stack landing `--stack` (all unchecked; order is binding)

- [ ] T021 **Spike-run, owner at the terminal** (a settings and push operation of the owner): measure E0 to E4 of research.md in `wstein/stw-release-spike` with a second stack, record the verbatim results in research.md and add the measured shape (`stack: true`, bypass mode) to `internal/land/shapes.json`. Nothing in T024 to T029 may enable apply before E1 to E3 are recorded. (Coverage: FR-067, FR-071, FR-073, FR-075).
- [ ] T022 Independent design review of the exact head of this amendment (strongest tier, executed checks) before tasks T024 and later start. (Coverage: FR-060 to FR-079).
- [ ] T023 **Security review before code** (strongest tier, separate from T022): threat-model the chain landing: one ref update of the owner's account with a bypass of mode `always` (an agent session holding the owner's token can push the default branch; mode guard is an accident guard, SI8), the typed word (`land stack TIP SHA8 COUNT`: replay across chains, a head changed between plan and typed word, TOCTOU between the last read and the push, the `--reviewed` list versus the actually pushed tip), review evidence that is information only (FR-062), untrusted PR titles and remote text in the table, the argument-vector allowlist, and the journal. The review result goes into the PR; findings change the spec before T024. (Coverage: FR-062, FR-067, FR-070, FR-072, FR-077).
- [ ] T024 Failing tests then argument parsing and exit 2 for `--stack`: a single number, more than 8, duplicate, `--reviewed` set not equal to the list, `--stack` with `--pr`/`--head`/`--yes`, `--head`-style SHA not 40 hex. (Coverage: FR-060, FR-003).
- [ ] T025 Failing tests then the chain precondition reader with fixtures, one test per reason: `head-changed`, `chain-not-linear` (Git decides: API clean, Git diverged), `base-not-in-chain`, `object-missing` (exit 128), `not-fast-forward` with `rebase needed`, `stack-side-child`, `checks-not-green` on the tip, `checks-pending` and a missing context on a non-tip head, `head-changed-no-checks` (events `head_ref_force_pushed` plus empty rollup, with the close-and-reopen text printed and not run), `native-stack-shape`, `tip-checks-pending`, `range-mismatch` (FR-081), `ruleset-forbids-push` (bypass `pull_request`, readable), `land-unmeasured` (unreadable 403 or unrecorded shape), Draft needing `--ack-draft`, fork, already merged; `review_evidence` found/none/unknown never refuses. (Coverage: FR-061 to FR-068, FR-081).
- [ ] T026 Failing tests then `--plan` for a chain: table, all failing preconditions, exact push argv, agent-mode labelling, `apply_allowed` false in agent mode, schema `chain` field with valid and invalid fixtures, no write beyond the private fetch. (Coverage: FR-002, FR-079, US6).
- [ ] T027 Failing tests then apply: agent-mode refusal before any write, typed word `land stack TIP SHA8 COUNT` from `/dev/tty` (wrong three times, no terminal, stdin never read), re-read of the whole chain before the push, one `git push --porcelain` of the tip only; mutation probes (`--force*`, a lease on the landing push, `+`, a second ref, a `gh pr edit`) fail the allowlist test (the delete guard `--force-with-lease=refs/heads/<ref>:<head>` of FR-075 is the one permitted lease, tested in T028); the typed word binds repo, default tip, tip, numbers and heads and apply re-reads them (FR-082); `base-moved` leaves everything unchanged. (Coverage: FR-070, FR-071, FR-072, FR-077, FR-080, FR-082).
- [ ] T028 Failing tests then the bounded wait per pull request (fake clock, `testing/synctest`): `landed`, `landed-not-marked`, `merged-other-sha`; replay after a push-only interruption (`already-landed`, no second push); `--delete-branch` only for each landed PR's own head ref after all are `MERGED`, one push per branch, `already-absent` when GitHub deleted it. No stw call closes, comments on or edits a pull request: a static test pins the allowlist, and the fallback close commands are printed only (FR-073, FR-076); the `--stack` refusal of a non-linear chain or moved default branch changes nothing (FR-078). (Coverage: FR-073, FR-074, FR-075, FR-076, FR-078).
- [ ] T029 Journal: one intent fact before the push and one outcome fact after it, one per branch delete; chain digest bound. (Coverage: FR-043, FR-077).
- [ ] T030 `docs/commands.md`, `docs/json-contracts.md` (when the schema is published), `docs/agent-guide.md` (agents run only `stw land --stack ... --plan`); smoke script on a local bare remote with a three-pull-request chain; relation to the single landing and to the one-PR commit series. (Coverage: FR-079, FR-083, US6, US7); the docs state the no-undo recovery of FR-083.
- [ ] T031 First dogfood after T021: plan only, then the real chain landing in the spike repository with the owner at the terminal; result recorded in research.md. (Coverage: FR-073).
