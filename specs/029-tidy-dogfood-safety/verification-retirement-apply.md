# Retirement apply source evidence

This is the E source slice for T028–T031, based on cleared admission
`f696611d925ac6d320d66178c3bc739c9084b061`. It does not authorize or record
cleanup of user worktrees, public CLI exposure, installation or a build.
The exact final source head and independent review/check receipts belong to
its handoff; the development experiments below are not a frozen-head gate.

## Operation and effect boundaries

The planner keeps one offline ten-second or online thirty-second deadline and
one proof ledger across initial, repeated and final under-store-lock complete
observations. Apply keeps one sixty-second deadline and ledger. Recovery and
removal retain subordinate thirty-second contexts; every complete retirement
observation retains its subordinate ten-second context. None resets its parent.

| Apply observation | Boundary |
| --- | --- |
| 1 | Complete native/content/physical/current completion or original ownership preflight, before any provider session |
| 2 | Complete preflight under the private v3 store lock |
| 3 | Recovery adapter before the predeclared pin |
| 4 | Recovery adapter after pin, allowing only the exact recovery-ref delta |
| 5 | Coordinator after confirmed recovery |
| 6 | Coordinator before native removal |
| 7 | Removal adapter before constructing parent grants |
| 8 | Removal adapter after grants, before durable removal intent and native execution |

Legacy removal uses exactly `--no-replace-objects worktree remove -- DEST`.
It has no force, unlock or fallback path. Owned v3 uses the factored original
owned-only runner after re-proving the original completed creation records,
current detached native identity and exact `stw:<task>` lock at every full observation.
The original owned runner's fixed arguments, sandbox grants and lock semantics
are unchanged; H23 explicitly authorized its new private v3 source routing and
exclusive disposable fixture tests. Original records are never rewritten or
replaced by legacy completion declarations. Stable ownership uses the original
registration identity plus mandatory object-format equality; current head,
integration proof and recovery remain exact current native facts. Normal task
progress before planning can differ from the immutable creation snapshot. Head
changes during an observation or after planning refuse effects.

Recovery intent is durable before its native pin. Exact recovery is confirmed
before removal; registration and file outcomes are recorded separately although
native Git couples their execution. Branches remain present. Native failure,
cancellation, durability failure and lost completion responses retain recovery
or possible-recovery evidence and prevent automatic replay. Completed retries
are read-only and verify exact postconditions; a completed no-effect retained
plan instead freshly verifies the still-present target.

## Accounting and remaining race boundary

The normative channels and limits remain [streams.md](contracts/streams.md):
all-pass accounting fixes three/eight full observations, nine/twenty-four user
passes, at most three/eight raw-object processes, user 576/1536 MiB and total
three/eight times `(320MiB+320KiB)`. Raw buffers remain at most 64 KiB. Per user file
is 16 MiB and each user pass 64 MiB; metadata remains 64 KiB/file. Aggregate 1024 and
encoded 1 MiB limits continue to apply to complete records, including protection
facts. Regular sibling files are protection facts, not user/metadata payload.

`native_processes` reports the instrumented raw-object process channel;
`native_bytes` reports instrumented content-proof consumption. Neither measures
all native launches, layout/control reads, nor Git removal's internal I/O. Other
native outputs retain their 64 KiB bound; tree/filter streams retain the declared
4224-byte logical records, 4325376-byte outputs and 1024-leaf/path bounds. Layout
pointer/configuration files retain 64 KiB, loose-ref and sibling listings retain 1024-entry guards, and all remain
subordinate to the same
operation/observation deadlines. No new production instrumentation was added.

Source-audited per complete legacy observation: two source observers each run
their original before/after source safety proof; two target safety proofs remain.
Owned observations also retain both original `gitAdapter.Verify` calls, their
exact task-lock/backlink reads and source/target layout checks. Original private
creation/task/journal reads remain strictly bounded by the secure store. These
are separately bounded control channels, not extra manifest passes. Total
nested native-launch/control-read counts were not measured or claimed.

After native removal, absence, both parent containers, protected sibling facts,
complete source reference/registration state and literal recovery are verified.
This postcondition control observation is not another user-content or target
manifest pass. Physical checks and native no-force checks remain sampled:
external writers can race after the final proof; no transactionality, automatic
rollback or restoration of ignored/untracked data is promised.

## Executed development experiments

- First executable RED: missing native retirement coordination refused a real
  disposable linked target. GREEN then proved the exact no-force argv, one
  native removal, plan3/apply8 raw samples, separate completed outcomes,
  preserved branch/recovery and read-only completed retry.
- All three planning and eight apply boundaries reject late untracked actors.
  Eight-by-six apply cases reject same-size/restored-mtime writes, same-byte
  writes with restored mtime, chmod, hardlink, symlink and inode replacement.
- Lost native completion response, eight durable-write failure points and
  cancellation after recovery leave truthful incomplete records and no replay.
  Native tracked/untracked writes or a lock injected at invocation fail without
  a force retry, retaining actor files and recovery.
- Ordinary, separate-Git-directory and absorbed-submodule primary/linked source
  layouts execute actual removal while preserving unrelated primary/native
  metadata, a user document and ignored cache.
- Original owned 301117-byte content receives explicit Git v3 proof while old v2
  retains its 64 KiB refusal. Wrong original lock, missing lock, wrong owner,
  head changes during proof and dirty content refuse. Original creation records remain
  byte/identity-preserved through successful authorized owned fixture removal.
- Unchanged original v2 Git lifecycle/nested-parent/regular-sibling grant cases,
  required current jj owned lifecycle, and old v2 JSON/in-process CLI error
  contracts pass. Minimum-version native and native macOS runtime lanes remain
  unavailable; compiler-only checks do not substitute for them.

Six native in-place-commit fixtures executed actual retirement **then a fresh
reference plan and apply**, with 315/323/332/341/350/358 tracked files and an exact
301117-byte SVG each. Attached/detached targets alternate and retain native
`COMMIT_EDITMSG`. Every plan measured 3 observations/raw processes, 9 user passes
and 12 metadata passes; every apply measured 8/8, 24 and 32 respectively, with
maximum read buffer 65536. Plan deadlines remain 10 seconds offline / 30 online;
apply remains 60 seconds. Protected primary user-plan, ignored-cache and active
dirty/ignored sibling bytes stayed identical through both operations.

The integrated source uses the independently cleared T021/T023 provider/reference
primitives already present in base `f696611d925ac6d320d66178c3bc739c9084b061`.
It does not claim coverage of the separate US4 tracking successor. The first two
cases are offline; the final four bind original typed same-repository/head/merge
commit evidence for an intermediate PR base absent from the local branch set.
Each online case opens four fresh operation-scoped typed fixture sessions:
retirement plan/apply, then reference plan/apply. These are inert provider
fixtures, not live HTTP measurements. Native integration reachability is actual
Git state. Reference plans are newly collected after registration removal;
previous hashes or closed provider sessions are never reused as fresh evidence.

Raw log SHA-256:
`6deca905df603ddc125c40f39f0aaaf823e4a97bd5dce6978eab4986a0979c2e`.
The earlier retirement-only log is separately retained and is not used to claim
this whole sequence. The first integrated attempt omitted the explicit online
acknowledgment in four fixture apply options; those cases refused before removal.
Its retained SHA-256 is
`54862aa8e590b2d26ba8041b2831479d710fc32d1c0ccbb2e4a9465453b6ae9c`.
Only the fixture acknowledgment was corrected before the successful run.

| Files | Plan user / metadata / framing / stderr / total bytes | Apply user / metadata / framing / stderr / total bytes | Plan / apply seconds |
| --- | --- | --- | --- |
| 315 | 2782368 / 133482 / 47259 / 0 / 2963109 | 7419648 / 355952 / 126024 / 0 / 7901624 | 0.521 / 1.797 |
| 323 | 2784240 / 135822 / 48459 / 0 / 2968521 | 7424640 / 362192 / 129224 / 0 / 7916056 | 0.516 / 1.708 |
| 332 | 2786346 / 140226 / 49809 / 0 / 2976381 | 7430256 / 373936 / 132824 / 0 / 7937016 | 0.572 / 1.891 |
| 341 | 2788452 / 142938 / 51159 / 0 / 2982549 | 7435872 / 381168 / 136424 / 0 / 7953464 | 0.534 / 1.796 |
| 350 | 2790558 / 147354 / 52509 / 0 / 2990421 | 7441488 / 392944 / 140024 / 0 / 7974456 | 0.583 / 1.948 |
| 358 | 2792430 / 149682 / 53709 / 0 / 2995821 | 7446480 / 399152 / 143224 / 0 / 7988856 | 0.590 / 1.980 |

The required reviewed registration-parent/sibling model was imported exactly;
new facts participate in existing plan/private digests and aggregate guards.
Post-plan added siblings and changed full sibling identities refuse admission.
Apply compares required persisted facts at every full observation and final
absence proof; it has no apply-first or missing-field fallback. Original model
manifests and supplemental old/new blob provenance are retained in
[registration import](retirement-model-import-provenance.json) and
[owned progress import](owned-progress-import-provenance.json).

## Consolidated independent review correction

Exact development candidate `e9e8132949b85579e864f4bd365c0aef4494fb87`
passed normal tests (276.248 seconds), race tests (384.437 seconds) and source
checks. Independent executed review remained NOT CLEAR with two Medium findings;
those aggregate passes are not used as evidence that the findings were closed.
The same author reproduced both failures before changing production code.

The correction orders every blocking provider/native postcondition read before
final physical cwd, parent, absence and sibling checks. A destination sibling
that is exactly the same original canonical `registration_parent` fact receives
only that parent's permitted post-removal size/link-count/time changes; stable
path/device/inode/owner/type/mode/precision remain exact. Every unrelated sibling
and every pre-effect full fact remain unchanged and exact.

The independently cleared finite feasibility design also prevents deterministic
self-invalidation after recovery. Before an eligible executable plan is stored,
consider the fixed recovery reference and its potential reflog path, regardless
of reflog configuration. Both parents and both sibling lists supply the bounded
set of full directory facts, deduplicated only by exact canonical identity. If
an output lies below such a directory and its immediate next component is not
already a supported same-user directory, planning refuses before pinning,
journaling or emitting an executable plan. The helper creates no namespace and
normalizes no snapshot. It adds only finite layout-control directory reads under
the original deadline, bounded by two fixed outputs times the existing aggregate
fact bound; no extra content passes, raw processes or caps are introduced.

This is an overlap-specific refusal: a nonoverlapping ordinary first-use cold
reference/reflog namespace still executes native removal successfully. Warm
nested canonical roles complete; cold overlapping reference, sibling-reflog and
direct-parent-reflog cases refuse before effects. Provider and final native
callback parent-mode actors report partial, as do changed stable parent mode and
unrelated sibling file/directory facts. No current-head, original-owned runner,
force/unlock, task-lock, schema, old Git/jj/v2 or budget policy was changed.

The exact corrected source head, focused/full logs and independent closure belong
to the new handoff receipt; the earlier candidate's green suite is preserved
separately and is not reused as corrected-head coverage.
