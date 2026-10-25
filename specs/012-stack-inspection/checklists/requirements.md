# Specification Quality Checklist: Stack Inspection and Plan

**Purpose**: Author review of specification completeness before analysis. Historical checked items below record the 2026-10-17 author checklist, not implementation or current independent review.
**Created**: 2026-10-17 (rework of the 2026-10-15 checklist)
**Feature**: [Specification](../spec.md)

## Historical 2026-10-17 author checklist

- [x] Focused on user value: see drift and the exact repair without duplicating GitHub's native stacks
- [x] All mandatory sections completed
- [x] No unresolved clarification markers remained in that historical draft; owner recommendations were listed in plan.md
- [x] Requirements are testable and unambiguous; one normative table per concept (commands, capabilities, findings, linkage, remote sources, delegation, plan rules, argument rules, limits, statuses)
- [x] Success criteria are measurable and describe observable outcomes
- [x] All acceptance scenarios and edge cases are defined
- [x] Scope is clearly bounded: read-only, explicit base and tip, opt-in pull request evidence, advisory plan
- [x] Dependencies identified: Specs 008, 009, 020, 022 and the Spec 021 runner; scope split with 015, 016 and 025 stated
- [x] Withdrawn first-draft items have a disposition table
- [x] GitHub facts measured on 2026-10-17 and marked; everything else listed as unverified in research.md (U1 to U10)

## Constitution alignment

- [x] I. One contract for humans and agents: one result, text and JSON, no prompts
- [x] II. Evidence with confidence; the plan is advisory, names expected ids and authorizes nothing
- [x] III. No mutation; worktree ownership respected (`layer-checked-out-elsewhere`)
- [x] IV. Failing tests first; hostile, bounded, concurrent and permission-limited cases specified; effect sentinels
- [x] V. Tier 1: Git and jj through one operation; the one capability gap is stated and not filled by a different mechanism

## Notes

The author review is not independent analysis. Independent cross-artifact analysis is recorded in analysis.md. jj behavior is design only until task T002. No extension hooks are configured in this checkout.

## 2026-10-25 documentation refresh

- [x] All ten existing Markdown artifacts remain coherent; no executable JSON, code, generated docs or other feature file is part of this change.
- [x] Historical spike measurements, unverified items and old review provenance are preserved and clearly historical.
- [x] Current stack command/schema implementation is not claimed; all Spec 012 implementation task boxes remain unchecked.
- [x] Observation, optional local restack input and separate forge effects are distinct; Spec 015 D6/D7/D8/D9 remain settled.
- [x] `stack-too-deep` is inferred advice at complete candidate open_depth >2, excludes proven-landed layers and keeps incomplete/error open depth null, with independent hard cap 32 local layers; `restack-needed`, stale child base and closed-unmerged parent evidence have explicit rules and future tests.
- [x] Pure decision functions and Spec 040 bounded indexed fan-out are specified, with no background polling.
- [x] Every FR and SC is individually mapped to task ids and a literal test idea in analysis.md; this is design traceability, not executed acceptance.
- [x] OQ-NET, OQ-FLAGS and OQ-EXIT are labelled Needs owner decision, with recommended defaults and deterministic current contracts; no owner answer is invented.
- [x] Network opt-in was an open recommendation, not an owner-approved decision; shared online/output/timeout naming remains explicitly gated before implementation.
- [ ] Independent review is CLEAR for this refresh's exact final head and required publication checks are green (coordinator records separately).
- [ ] Implementation/native backend/schema conformance is executed and accepted (future source task, not this Markdown scope).

The read-only Spec Kit analysis report is recorded by the separately authorized docs workflow in analysis.md. No extension hooks are configured. Documentation checking and whitespace evidence are recorded there; no missing Go/build/make/native acceptance is counted as passing.
