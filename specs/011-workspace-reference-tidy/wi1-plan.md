# WI1 Plan: Evidence-only Tidy Plans

## Goal and scope
US1/US3: inspect local branches/bookmarks against explicit base, explain delete/retain decisions;
no native mutation. Stateless default; optional secure state persists immutable plan for later apply.
Existing Spec010 APIs/schemas unchanged. Shared context/gates: [plan](plan.md), [research](research.md),
[model](data-model.md), [CLI](contracts/tidy.md), [API](contracts/interface.md).

## Design and constitution gates
PASS subject to independent WI1 analyze. Native Git/jj ancestry only; protect selected base,
workspace-used targets including detached matching commits, symbolic/conflicted/remote-bound jj
bookmarks and unknown evidence. Missing explicit selectors refuse. Native jj operation-pinned
read-only sandbox has no snapshot; before/after full observation equality guards sampled freshness.
No remote/forge/squash inference. Optional state effect disclosed; no state means no store effects.
Mandatory limits and human/JSON errors share one model; absent capabilities fail closed.

## Implementation and review
Core owns TidyPlanRecord/Result/store extension; native authors own git/jj observation files;
CLI author owns parser/schema/publicdocs; conformance author owns smoke/CI. No mutable global
injection. API exact fields are frozen in contracts/interface.md. First write failing tests.
Plan tasks are [wi1-tasks](wi1-tasks.md). Independent analyze precedes source work; independent
source review, full checks/race and both pinned lanes precede WI1 PR. WI2/3 remain planned.

## Independent acceptance
Integrated vs unique local refs, selectedbase/workspace-used protection, goneupstream/emptyalone,
conflicted/remote-bound refs, ownership/version/sandbox refusal, bound/cancel/stale sampling,
stateless byte/mode-identical native state, persisted strict immutableplan and oldschema regressions.
