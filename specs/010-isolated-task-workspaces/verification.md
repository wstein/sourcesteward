# Spec 010 verification

## Design and baseline

Plan/tasks preceded source implementation. Independent analysis cleared `6d3e55e`;
[analysis](analysis.md) records 15/15 requirement coverage and resolved findings.
No extension hooks are configured; the specification quality checklist is complete.

Baseline `e67ae90` passed `make check` in the new implementation worktree with
GOCACHE=/private/tmp/gitsteward-go-cache, GOMODCACHE=/private/tmp/gitsteward-go-mod and
HUGO=/private/tmp/sourcesteward-hugo-docs/.tools/hugo. This included Go tests/vet/module
verification, 37 Python tests, 12 theme cases, 123 authored docs and architecture/whitespace.
This establishes baseline source health, not Spec 010 acceptance.

## Native research

Backend authors independently experimented in disposable fixtures before implementation.
Git worktree-add no-checkout still invokes reference-transaction hooks; full creation
therefore requires effective helper/hook admission rather than relying on no-checkout.
Native Git detached full checkout internally invokes destination-only reset; no source
reset, branch creation or hook suppression is authorized.

jj 0.45.1 and 0.46.0 two-phase experiments in disposable fixture
/private/tmp/stw-010-jj-two-phase-9wdkwfxo confirmed native add with snapshot suppression,
exact operation, explicit commit, git.colocate=false and isolated identity, followed by
workspace update-stale in the owned destination. Source disk/untracked bytes, source
working-copy metadata and colocated Git HEAD/index/config remained byte-identical.
Native update-stale refuses operation pinning; materialization is sampled before/after
and is not a transactional compare-and-swap. These are author research observations;
mandatory executable acceptance and independent source review remain separate gates.

## Current gates

Implementation, source review, schema validation, minimum/current native conformance and
PR handoff are in progress. No runtime acceptance, hosted CI, merge, release or live
publication is claimed by this initial record.
