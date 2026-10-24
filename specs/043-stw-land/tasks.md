# Tasks: `stw land`

**Gate**: all runtime boxes stay unchecked until the design review of the exact head of this spec is CLEAR and the owner has settled
OQ1, OQ2 and OQ4 or accepted their defaults. Tests use temporary repositories with a bare remote and a fake `gh` transport (Spec 022
fixtures); never a real repository. Failing test first for each task. Each slice updates `docs/commands.md` and `docs/json-contracts.md`.

## Slice 0 - Design

- [ ] T001 Independent review of the exact committed head of this spec (strongest tier, executed checks).
- [ ] T002 Scratch-repository measurement of OQ4 (`delete_branch_on_merge` on) and of a ruleset-protected default branch; append to research.md.

## Slice 1 - `--plan` (read-only)

- [ ] T003 Failing tests for argument parsing and exit 2: missing `--pr`/`--head`/`--reviewed`, `--head` not 40 hex, `--yes`, `--json` with `--human`, bad `--wait`.
- [ ] T004 Failing tests then the precondition reader with fixtures for FR-010..FR-018 (one test per refusal reason: fork, merged, head-changed, head-not-reviewed, checks-unknown, draft, conflicting, base-not-default, not-fast-forward, object-missing, identity-mismatch, range-too-large, ancestry-unproven).
- [ ] T005 Failing tests then fast-forward by Git: `merge-base --is-ancestor` over a temporary repository with a bare remote; API says clean but Git says diverged refuses.
- [ ] T006 `--plan` output (human, `--json`) with exact argv, all failing preconditions listed, agent-mode labelling; schema `schemas/v1/land.schema.json` with valid and invalid fixtures; assert zero writes (no ref, no config, no process other than reads).

## Slice 2 - Mode and confirmation

- [ ] T007 Failing tests then agent-mode refusal of apply before any write or credentialed read (STW_AGENT, each marker, no TTY).
- [ ] T008 Typed `/dev/tty` confirmation (`land NUMBER SHA8`): wrong answer 3 times, no terminal, `--yes` ignored/usage error, stdin never read.

## Slice 3 - Apply

- [ ] T009 Failing tests then the landing push with `--force-with-lease=<ref>:<old-tip>`; a moved default branch gives `base-moved` and changes nothing; mutation probe that removes the lease value or uses `--force` fails a test.
- [ ] T010 Allowlist test of every git and gh argument vector (FR-033); no other write reachable.
- [ ] T011 Bounded wait for MERGED (fake clock), `landed-not-marked`, `merged-other-sha`.
- [ ] T012 Stack order: retarget children before deletion; deletion only with `--delete-branch`, only the PR's own head ref, lease on the head SHA; closed-child report; failure of one retarget blocks deletion.
- [ ] T013 Replay/idempotence: rerun after push-only, after retarget-only, after full success; no duplicate effect.
- [ ] T014 Ruleset refusal: remote rejection text reported verbatim, escaped, bounded to 2 KiB, nothing else changed.
- [ ] T015 Journal lines before and after each effect; local only; none committed or pushed.
- [ ] T016 Identity check: throwaway author or committer refused; owner identity read from global config, never written.

## Slice 4 - Docs and acceptance

- [ ] T017 `docs/commands.md`, `docs/json-contracts.md`, `docs/agent-guide.md` (agents run only `--plan`); smoke script on a local bare remote.
- [ ] T018 First dogfood: plan only, then one trivial docs pull request in a scratch repository, owner at the terminal; result recorded in research.md.
