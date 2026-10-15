# WI2 Plan: Authorized Local Reference Cleanup

## Goal and dependency
US2/US3: apply the exact WI2-generated persisted local-reference plan after explicit authorization,
recording durable native recovery before every deletion. Depends on accepted WI1 API/schema.
No workspace retirement, remote intent changes, forge calls or history abandonment.

## Design
Reuse shared secure store, locks, TidyPlanRecord/Result and literal selected refs. WI2-generated plans
list explicit proposed native recovery/deletion effects; strict versioned effect-template purpose
validation rejects old WI1 observation-only plans and unknown templates even with authorization. Stateless WI1
plans cannot apply; stored immutable plan ID/state binding is mandatory. Reobserve relevant source,
base, refs, registrations, safety and jj operation before every action. Account only for known own
recovery/deletion effects, never broadly ignore changed refs/operation. Changed state refuses.
Prepared journal is fsynced before native recovery root, then root identity recorded durably before
target deletion. Root creation failure or persistence failure prevents deletion; already created
recovery roots remain disclosed. Git uses private recovery refs and old-OID CAS; jj uses protected
local recovery bookmarks and pinned/sampled operations, retaining any remote-counterpart target.
Native target CAS does not make fulloperation transactional. No forced rollback or blind retry.

## Constitution and analysis gate
Pre-design PASS subject to separate independent WI2 analyze and exact interface freeze.
Private recovery roots keep native history reachable; journal-only OIDs are insufficient.
Known completed/uncertain effects survive cancel/error/output bounds, with durable pointer.
Recovery is inspectable manual instructions within limits; no promised automatic restoration.

## Source and verification
Core tidy coordinator/state/journal; backend native recovery/delete helpers; CLI apply parser;
schema real fixtures; conformance tests. See [tasks](wi2-tasks.md) and [contract](contracts/wi2.md).
Test-first per task, independent source review/full checks/race and both pinned lanes before WI2 PR.
