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
  unborn head (all-zero object id) until the owner runs `git worktree prune`.
- The wizard's classifier no longer treats a gone registration (`DirMissing` and `Prunable`) as attaching its branch.

## Limits

A gone registration of a directory on an unmounted volume looks like a deleted one to Git; lock the worktree (`git worktree lock`)
to keep protecting its branch. jj workspaces are unchanged.
