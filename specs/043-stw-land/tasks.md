# Tasks: `stw land`

**Gate**: all runtime boxes stay unchecked until the design review of the exact head of this spec is CLEAR and the owner has settled
OQ1 and the Reconciliation items. OQ2 and OQ4 are hard gates for apply (`land-unmeasured`), not defaults: T002 comes first and
its result must be in research.md before T009. Tests use temporary repositories with a bare remote and a fake `gh` transport (Spec 022
fixtures); never a real repository. Failing test first for each task. Each slice updates `docs/commands.md` and `docs/json-contracts.md`.

## Slice 0 - Design

- [ ] T001 Independent review of the exact committed head of this spec (strongest tier, executed checks).
- [ ] T002 Scratch repository with a copy of the real sourcesteward ruleset and `delete_branch_on_merge` true: measure the direct owner push and the FR-031 pre-retarget order; append to research.md as measurements (OQ2, OQ4).
- [ ] T002a Failing tests then the `land-unmeasured` gate: apply refuses for an unrecorded shape, `--plan` works and lists it; allowlist keyed by normalised shape.

## Slice 1 - `--plan` (read-only)

- [ ] T003 Failing tests for argument parsing and exit 2: missing `--pr`/`--head`/`--reviewed`, `--head` not 40 hex, `--yes`, `--json` with `--human`, bad `--wait`.
- [ ] T004 Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven).
- [ ] T005 Failing tests then fast-forward by Git: `merge-base --is-ancestor` over a temporary repository with a bare remote; API says clean but Git says diverged refuses.
- [ ] T006 `--plan` output (human, `--json`) with exact argv, all failing preconditions listed, agent-mode labelling; schema `schemas/v1/land.schema.json` with valid and invalid fixtures; assert no writes beyond the disclosed private-namespace fetch (no user ref, no config, no process other than reads).

## Slice 2 - Mode and confirmation

- [ ] T007 Failing tests then agent-mode refusal of apply before any write or credentialed read (STW_AGENT, each marker, no TTY).
- [ ] T008 Typed `/dev/tty` confirmation (`land NUMBER SHA8`): wrong answer 3 times, no terminal, `--yes` ignored/usage error, stdin never read.

## Slice 3 - Apply

- [ ] T009 Failing tests then the landing push as a plain `git push --porcelain <remote> <sha>:refs/heads/<default>`; a moved default branch (`fetch first`) gives `base-moved` and changes nothing; mutation probe that adds `--force*`, a lease or a `+` refspec fails a test.
- [ ] T010 Allowlist test of every git and gh argument vector (FR-033); no other write reachable.
- [ ] T011 Bounded wait for MERGED (fake clock under `testing/synctest`), `landed-not-marked`, `merged-other-sha`.
- [ ] T012a Failing tests then FR-034: after a retarget everything is re-read; `mergeable` null or pending checks refuse before the push; FR-031 order and its failure branch print `gh pr edit CHILD --base OLD_BASE` and run nothing.
- [ ] T012 Stack order: retarget children before deletion; deletion only with `--delete-branch`, only the PR's own head ref, lease on the head SHA; closed-child report; failure of one retarget blocks deletion.
- [ ] T013 Replay/idempotence: rerun after push-only, after retarget-only, after full success; no duplicate effect.
- [ ] T014 Ruleset refusal: remote rejection text reported verbatim, escaped, bounded to 2 KiB, nothing else changed.
- [ ] T015 Journal lines before and after each effect; local only; none committed or pushed.
- [ ] T016 Identity check: expectation is `git config --global` only; a local or worktree `user.*` gives `identity-leak`; throwaway author or committer refused; nothing written.
- [ ] T016a FR-012 fixtures: ruleset list, 404 protection, 403 ruleset, empty required list, default branch not the PR base.
- [ ] T016b FR-016: merge-base exit 128 (old tip absent) is `object-missing`, never a fast-forward.

## Slice 4 - Docs and acceptance

- [ ] T017 `docs/commands.md`, `docs/json-contracts.md`, `docs/agent-guide.md` (agents run only `--plan`); smoke script on a local bare remote.
- [ ] T018 First dogfood: plan only, then one trivial docs pull request in a scratch repository, owner at the terminal; result recorded in research.md.
