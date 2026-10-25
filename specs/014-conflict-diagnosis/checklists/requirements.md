# Specification quality checklist: Conflict diagnosis and integration preflight

**Date**: 2026-10-25 | **Feature**: [spec.md](../spec.md)
**Purpose**: author document-quality review, not independent review or acceptance.

## Content and bounded scope

- [x] User stories distinguish existing diagnosis, resolver handoff, agent contract and isolated prediction.
- [x] Requirements describe observable behavior; backend names/identity and isolation effects are necessary compatibility constraints.
- [x] No original tree/index/ref/object/config or jj working-copy/oplog writes are authorized.
- [x] Spec 015 D7 plumbing/quarantine and D8 remote separation remain settled; no porcelain replay is proposed.
- [x] Native Git/jj capabilities and measurement gaps are explicit; no fallback or false parity.
- [x] Default-none, offline operation, untrusted data and secret-minimal bounded output are explicit.
- [x] Every FR and SC has an individual task and literal test idea in [analysis.md](../analysis.md).
- [x] New JSON exists only as UNPUBLISHED Markdown proposals; existing schema files stay frozen.
- [x] All future implementation tasks are unchecked and red-test-first behavior pairs are explicit.
- [x] Edge cases include topology, first-conflict suffix, concurrency, hostile paths, hunk privacy and scratch residue.

## Outstanding gates

- [ ] Owner choices for affected OQ-SEM/TOPO/HUNK/SCRATCH/JJCONF/EXIT capabilities are recorded as decisions rather than recommendations.
- [ ] Independent complete-artifact review is CLEAR at the exact current head.
- [ ] Future implementation and minimum/current native conformance/effect evidence exist for enabled capabilities.

## Qualification

Artifact completeness and traceability are assessed by this author only. Open
choices remain visible in [spec](../spec.md#needs-owner-decision); no arbitrary
answer is invented to tick a checklist. Hunk/native merge/native jj preview
capabilities stay disabled until their gates pass. No product code/tests/builds,
full make check, schemas or progress/commands changes are part of this amendment.
Installed Spec Kit guidance is applied to the existing directory with no-persist
feature selection; no extension hooks are configured. Independent review and
publication stay separate from document-author checks.
