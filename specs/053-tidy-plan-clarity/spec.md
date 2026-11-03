# Specification: clearer `stw tidy plan` evidence

**Created**: 2026-11-03 | **Status**: implemented in source, independent review pending (spec-light: this page, contracts and tests).
Issues #386, #387, #388, #389. It **amends** [Spec 029](../029-tidy-dogfood-safety/spec.md) (registrations protect a branch) and
[Spec 011](../011-workspace-reference-tidy/spec.md) (the `workspace-used` rule); later sections of this page amend
[Spec 031](../031-tidy-integration-proofs/spec.md) and the base selection of `stw tidy plan`. Published v1/v2 schemas and bytes
are unchanged; `schemas/v3/tidy.schema.json` gains optional fields only.

## Context & Problem

On the owner's clone 182 of 286 branches were retained as `workspace-used`; only 19 of their worktree paths exist. The others
are registrations whose directory is gone (a devcontainer, finished agent sessions). Spec 029 let such a registration protect
its branch and commit, so a missing directory hid every branch it named.

## Section 1: a missing worktree directory protects nothing (#386)

- A registration counts as gone when Git reports it `prunable` and it is not locked (Git never reports a locked registration as
  prunable). Its directory is gone below a real parent; the existing qualification of such registrations is unchanged
  (`docs/tidy.md`, "stale registration"). A registration with an existing directory still gives `workspace-used`.
- A gone registration does not add its path to `workspace_paths` and does not protect its branch or commit. The branch goes
  through the other rules: symbolic reference, selected base, then the integration proofs.
- Contract 3 plans gain the optional, additive `stale_registrations` list (sorted by path; absent when empty, so a plan without a
  gone registration keeps its exact bytes). Each entry has `path`, `head_ref`, `head_oid`, `reason` (`workspace-missing`) and
  `next_command` (`git worktree prune`). The list is bound into the plan digest and a stored plan whose entry is not a gone
  registration of its native evidence is invalid.
- The default (contract 1/2) plan keeps its schema: each gone registration is named in `limitations` (`workspace-missing: ... run
  git worktree prune as a separate step`), at most 16 lines and a count of the rest.
- Planning deletes and prunes nothing. Apply deletes the branch as before; the gone registration stays and is then listed with an
  unborn head (all-zero object id) until the owner runs `git worktree prune`. Contract 3 accepts the all-zero head only for a stale registration.
- The default plan (contract 1/2) shows the `workspace-missing` limitation only without `--state-dir`; with `--state-dir` it still refuses with
  `unknown-workspace-path` while a gone path exists, since state-store disjointness is unproven.
- The wizard's classifier no longer treats a gone registration (`DirMissing` and `Prunable`) as attaching its branch.

## Section 2: integration proof by merged pull request head or patch-id (#387)

Contract 3 already proves integration by the head of a merged pull request (`--online`, wire word `github-merged-head`, [Spec 029
online contract](../029-tidy-dogfood-safety/contracts/online.md)) and by git's patch-id, the semantics of `git cherry` (`patch-id`,
[Spec 031](../031-tidy-integration-proofs/spec.md)). The wire words are published and stay; the issue's `merged-pr-head` is the
documented name of `github-merged-head`. This section adds only what was missing:

- `proof_policy.pr_proof` (optional, additive, in the plan digest) records whether the pull request proof could be used:
  `available` for an online plan, `unavailable-offline` for an offline plan, which keeps the forge-free proofs. A plan that predates
  the field has none; the value must agree with the plan's online selection. The text output prints `Pull request proof:`.
- Acceptance is pinned by tests: a branch whose tip equals the head of a merged pull request is `delete` with `github-merged-head`;
  the same name at another tip is `retain`; a partially applied branch is `retain` (Spec 031 `partial-match`, existing tests).
- No new hosting access: the reads are those of the guarded session; no write exists.

The default plan (`stw tidy plan --base`, contract 1/2) proves integration by native ancestry only and cannot gain proof words:
`schemas/v1` and `schemas/v2` are byte-frozen. Use `--contract 3` (optionally `--online`) for the other proofs.

## Section 3: `stw tidy plan --explain` (#388)

- `--explain` is an expert-layer flag (it selects the expert layer like `--base`). For every retained candidate it prints one sentence
  (`It stays because ...`) naming the missing proof, and one next command. The closed reason list is `ExplainedReasons`
  (`internal/workspace/tidy_explain.go`) and covers the Git reasons and the jj reasons (`not-integrated`, `remote-counterpart`,
  `recovery-reference`, `ambiguous-target`) and `reviewed-keep`. Tests check that every listed reason has its own text and command
  and that every reason literal in the planner code (Git and jj) is listed, so a new reason without an explanation fails. Another reason gets a generic text and command.
- Without `--explain` the output is unchanged: the default text only gets lines appended, and the contract 3 result has no
  `explanations` key.
- Default (contract 1/2) text: `Explain:`, `Why:`, `Next:` lines after the plan. `--explain --json` without `--contract 3` exits 2,
  because `schemas/v1` and `schemas/v2` are frozen. Contract 3 JSON: the optional, closed result field `explanations`
  (`name`, `reason`, `text`, `next_command`), outside the plan and its digest.
- The commands quote untrusted names and paths for a POSIX shell; they are suggestions that planning never runs. A command that
  starts `stw` is checked against the real flag parsing and validators (online proof needs `--remote`; `--report-dir` is created
  and passed as an absolute physical path, `"$(pwd -P)/tidy-review"`); the Git commands run against a real repository (the `gh` and `jj`
  commands are not run by the tests). jj plans get `jj -R PATH ...` commands, not `git -C` commands. The text output prints the `Next:`
  command as typed, not Go-quoted, so it can be copied; every non-printable character (control, bidirectional and zero-width
  format characters, U+2028/2029, invalid UTF-8 bytes) is escaped as `\x{HEX}`, so the line never reads differently from what it runs; a test parses the printed line with a shell and compares the arguments.

## Section 4: default base from origin/HEAD (#389)

- Without `--base`, the expert layer of `stw tidy plan` (default and `--contract 3`, retirement included) uses the target of the
  symbolic reference `refs/remotes/origin/HEAD`, resolved by `workspace.DefaultTidyBase`: two sandboxed native Git reads
  (`symbolic-ref --quiet`, `rev-parse --verify`) through the hardened runner, no network, no write. An explicit `--base` is
  never replaced and never triggers the read.
- The target must be below `refs/remotes/origin/` and name a commit. Otherwise the command exits 2 and the message names
  `git remote set-head origin -a` and `--base REF`. Outside a Git repository the old `--base is required` message remains.
- No other remote, `main`, `master` or `HEAD` is guessed. jj repositories (also colocated ones, decided by the ownership inspection) and the wizard layer (its own derivation) are unchanged: a jj repository without `--base` still gets `--base is required`.
- The plan output carries the used base as `Base selector`, so the choice is visible and bound into the plan.

## Limits

A gone registration of a directory on an unmounted volume looks like a deleted one to Git; lock the worktree (`git worktree lock`)
to keep protecting its branch. jj workspaces are unchanged.
