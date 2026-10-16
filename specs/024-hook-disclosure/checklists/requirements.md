# Specification Quality Checklist: Hook and bypass disclosure

**Purpose**: Author review of specification completeness before planning.
**Created**: 2026-10-16
**Feature**: [Specification](../spec.md)

## Content, completeness and readiness

- [x] Focused on user value: finding guards that are missing, disabled, unsafe or bypassed by the backend in use
- [x] All mandatory sections completed
- [x] No unresolved clarification markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable and describe observable outcomes
- [x] All acceptance scenarios and edge cases are defined
- [x] Scope is clearly bounded: read-only, `commit` and `push`, client-side hooks, no hook content
- [x] Dependencies and assumptions identified: existing ownership, Git runner and jj sandbox session; Spec 021 for the shared exit status

## Constitution alignment

- [x] I. One contract for humans and agents: text and JSON from one result, no prompts
- [x] II. Evidence only; every claim carries an evidence level; nothing authorizes an action
- [x] III. No mutation; hooks never skipped, installed or edited by the command
- [x] IV. Failing tests first; hostile, concurrent and limit cases; a conformance procedure that measures the jj claim
- [x] V. Tier 1: Git and jj supported with different, stated semantics; jj behavior marked unverified until measured; GitButler explicitly unsupported

## Notes

The author review is not independent analysis. Independent cross-artifact analysis is task T001 and precedes implementation. The jj hook behavior is documentation-level evidence only; no jj measurement exists yet.
