# Specification Quality Checklist: Stacked PR Coordination

**Purpose**: Author assessment of the design, not implementation acceptance.
**Updated**: 2026-10-25 | **Feature**: [spec.md](../spec.md)

## Content quality

- [x] User value, scope and independently testable stories are explicit.
- [x] Mandatory specification sections, edge cases and assumptions are present.
- [x] Native/forge compatibility requirements are separated from plan architecture.
- [x] All new contracts are labelled UNPUBLISHED; current behavior is not overstated.

## Requirements and coverage

- [x] Each FR and SC is individually traceable to existing future task ids and a literal test idea in analysis.md.
- [x] All 38 implementation tasks are unchecked and ordered tests before implementation.
- [x] No unresolved choice is silently recorded as owner approval.
- [x] Offline/default-none, per-action remote authority, ownership, exact leases, actual identity/review/signing, Ready and retry limits are testable.
- [x] Shared flags and hidden 015 exit 3 compatibility are explicitly reconciled as proposals.
- [x] Native jj and native stack publication gaps remain capability-gated, without raw Git fallback or inferred endpoint effects.

## Gates still open

- [ ] OQ-SURFACE/POLICY/WRITES/NATIVE/SIGN/STORE/EXIT accepted for each affected capability.
- [ ] Independent review of the exact artifact/source head is CLEAR.
- [ ] Declared implementation/source/native minimum/current acceptance is executed and recorded.
- [ ] New structured contracts pass implemented conformance and any separate publication gate.

## Notes

Checked items are author document-completeness assessments. Unchecked gates block
affected implementation/acceptance; they do not erase accepted historical 015
source work or imply all future work must fit one PR. No runtime or live forge
behavior is verified by this checklist. Local observation authorizes no mutation;
local apply authorizes no remote effect. Spec 043 landing remains owner-only and
separately invoked, with its conditional order and unmeasured-shape gates intact.
