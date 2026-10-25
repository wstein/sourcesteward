# Specification quality checklist: local restacking

**Created**: 2026-10-15; **amended**: 2026-10-25.
**Feature**: [spec.md](../spec.md). Author document review, not implementation or
independent acceptance. Historical first-slice implementation/evidence remains
separate from proposed local apply/recovery/jj work.

## Author completeness review

- [x] Existing hidden v1 source, UNPUBLISHED frozen schema, actual exit3 and T024 limitation gap are stated accurately.
- [x] D4–D9/D12/D13 and original dates/historical review evidence are preserved.
- [x] T001 historical CLEAR is distinguished from this amendment's unreviewed head; T004/T021/T023/T024 remain unchecked.
- [x] User outcomes and acceptance scenarios cover explicit local scope, stale state, native effects, partial outcomes and contract failure.
- [x] Exact two-dot `--onto` replay semantics preserve D7 plumbing/quarantine/CAS, no porcelain rebase or implicit descendants.
- [x] Actual new SHA/equality/author AND committer evidence precedes a class-dependent policy receipt; 016 preserves docs-only pre-push exemption, code exact-new-SHA review and equal-pure-rebase middle-tier allowance; Ready always needs independent exact-head CLEAR plus green checks.
- [x] Owned unoccupied-ref first apply is an explicit recommendation pending OQ-6, with original worktree/index/config unchanged.
- [x] Proposed typed recovery seam reuses Spec 032; no existing shared API, jj support or frozen sync-record compatibility is fabricated.
- [x] Native jj operation/bookmark/change/commit identities and capability gaps are explicit; no Git fallback.
- [x] D8 remote push/retarget belongs to separately authorized 016; owner resync/reset remains inert destructive-marked data and 043 landing separate.
- [x] All 34 FRs and 8 SCs have an individual task plus literal test/fixture row in [analysis](../analysis.md).
- [x] Research, plan, data model, contracts, quickstart, tasks and verification use the same proposed/implemented boundary.
- [x] New JSON examples exist only in Markdown and are clearly UNPUBLISHED fragments, not production schemas.
- [x] Every new implementation unit has dependencies, bounded scope and red-first security/effects/failure fixture ideas.

## Gates still open

- [ ] Owner dispositions for affected OQ-1–OQ-6; OQ-7 wording clarification remains explicit.
- [ ] Independent complete-artifact review of this exact documentation head.
- [ ] First-slice T021 minimum/current Git and T023 exact implementation-head acceptance; T024 actual limitation emission.
- [ ] Implementation, failing/passing fixture evidence and enabled native Git/jj conformance for proposed apply/recovery.
- [ ] Accepted future machine protocol/version/migration, fixture validation and publication.

The specification intentionally contains settled replay/ownership compatibility
constraints and unresolved owner choices; no blanket “no implementation details”
or “all ambiguities resolved” box is asserted. Documentation checks can validate
links/whitespace and coverage bookkeeping only; absent native or independent
review evidence never counts as passed acceptance.
