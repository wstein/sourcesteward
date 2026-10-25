# Validation Guide: Stacked PR Coordination

**Future validation only**. No `stack publish` command is implemented by these
artifacts. Do not run proposed writes against a real user repository. Use
separately authorized disposable fixtures after owner decisions and implementation;
all scenarios below are test ideas, not results already measured.

## Prerequisites and fixture scope

Use a temporary native Git repository and bare remote, plus an in-process fake
forge with bounded 020/022 transport; no real credentials. Add native jj
minimum/current fixtures to verify observation and explicit publication refusal,
including colocated jj with unchanged operation log/working copy. The exact
version/platform capability table must be accepted before claiming conformance.
Trusted owner/review/check/scope evidence and the private store are test-owned,
not read from fixture repository instructions. No product or documentation build
is required to inspect the design.

Use one parent/child code dependency with disjoint file scopes and an unrelated
docs branch. Record full before-state refs/objects, trees/indexes, config, operation
logs, task state, remote refs and request/helper logs. Do not use destructive
commands in an owner's checkout. The [effect contract](contracts/forge-effects.md)
and [receipt matrix](contracts/receipts.md) define exact rules and bounds.

## 1. Offline/default-none MVP (US1)

Proposed lines for future fixture invocation:

```text
stw stack publish
stw stack publish plan FIXTURE --repo owner/project --observation OBSERVATION_FILE --human
```

Expect help to perform zero reads/effects and a plan with no selected actions.
Without proposed `--save`, rendering is unsaved (`saved: false`, null plan id),
with zero plan/store writes and no directory creation/repair. For a nonempty
selection, add proposed `--save --state-dir EXISTING_PRIVATE_DIR` to persist into
that explicitly selected validated directory; no default store is accepted.
`--save` without the directory is usage 2; empty selection never persists.
Apply an unsaved result or missing id and verify refusal, with no implicit save.
Supply current 012 nested `restack-advised` evidence and separately a published
035 `restack-needed` event: discriminate producer/kind/version and never use the
event's merge-commit destination as an unvalidated trunk/target. Supply 014
preflight without full-H attribute-source pin/private-context proof and reject
its profile evidence; a clean jj tip cannot bypass OQ-JJCONF. Supply 013 checkpoint
metadata and native jj samples without a CAS; independent safety-copy retention
and invoker working-copy exclusion never authorize publication.

Supply valid 012 relationship evidence, then vary PR head SHA, head/base repository,
ambiguous matches and stale local identities. Names alone never verify linkage.
Default offline consumes bounded evidence without auth/HTTP/fetch/cache. A remote
guard stays unknown/blocked. Verify no refs/files/index/objects/jj log changed
except an explicitly selected `--save` private plan write.

Online syntax remains an OQ-SURFACE proposal. 012's online inclusion and
`--pull-requests`/`--online` naming are open, not owner-approved. The accepted
`--after-fetch` hint is observation data and never fetches.

## 2. Selected own-topic lease push (US2)

After the fake native transport and policy capability are accepted, select one
`push-topic` action. Store its plan and apply only with its exact plan id and
`--authorize-remote ACTION_ID`; local `--authorize` cannot permit this write.
Verify one exact full new SHA/refspec and one full observed old SHA lease.
Inject a remote ref move after plan and before push: typed refusal, no forced
retry and no other ref touched. Try default/protected ref, tag, foreign task,
caller-widened identities and unknown permissions: all refuse.

Create the actual candidate before review. Exercise all rows of the receipt
matrix: docs-only checks, pure equal rebase, code requiring independent exact
actual-new-SHA CLEAR, middle tier for pure equal code rebase, unknown class as
code. Verify a predicted hidden-v1 SHA cannot count as actual review.

Include an own child whose moved range has owner identities but whose open
parent has a foreign committer. The full observed-remote-default..new-head gate
must catch it. Leaked local/global user identity also refuses with bounded origin
evidence and no config repair. Signed-source/signature-required uncertainty
blocks; signing-helper and secret sentinels remain unexecuted/unexposed.

## 3. PR writes, retarget and native capability (US2)

Until OQ-WRITES/OQ-NATIVE method/effect measurements are accepted, plan PR/native
actions as capability-unavailable and assert zero write requests. After acceptance,
assert only the fixed method/path/body union and explicitly selected own PR/body.
Standalone comments remain deferred and cannot execute in the initial capability.
Read head/base/state before/after a child retarget; head movement returns partial
head-changed. Invalidate old Ready evidence after base edits. Native rewrite
pending/unknown blocks competing push/rewrite. Native link reference has
`GH_REPO`, numeric bottom-to-top PRs and explicit trunk base, no `-R` or extension
execution. A three-open-layer stack blocks publication; unrelated docs target
default directly. Test native jj refusal without raw Git fallback.

## 4. Lost response, cancellation and retry (US3)

Crash at every intent/effect/outcome boundary. A successful write with lost
response is unknown until exact fresh state proves completion; retry then emits
already-completed without duplication. Unknown PR/comment/link completion never
causes a second write. Fail journal durability: no write begins. Cancel between
actions: unstarted suffix remains not-started. Fail permissions or hit request/
output/time budget: exit 1 with truthful completed/unknown outcomes, no next write.
Local restore cannot undo publication.

## 5. Ready and owner handoff (US4)

Supply exact-current-head independent CLEAR, all required CI green,
`rebaseable == true` and accepted PR scope. Individually select/authorize Ready.
Remove each gate or move the head/base: refusal. Old author review, 025
not-asserted, predicted-clean, absent required-check source and docs push exception
never pass. Out-of-scope future work may remain open; in-scope acceptance cannot.

Render both conditional 043 handoffs as inert data: readable-off land bottom →
retarget child → delete bottom only afterwards → land child; on/unreadable
retarget → re-read → owner land is unmeasured/043-gated, unreadable shapes fail
closed. Assert no land/merge/default push/deletion executes. Verify owner resync
data uses actual full old pushed/new SHAs; reset-hard is labelled destructive,
local-commit rebase uses exact old pushed SHA. Any agent-style configuration
overrides shown for the owner are explicitly a stricter optional variant.

## 6. Contract, hostile input and native acceptance (US5)

Run future shared operation/CLI fixtures for escaped controls/bidi/unusual names,
correct JSON escaping, bounded text/body, malformed data, unsupported ids,
response URLs, redirects, pagination and cancellation. Exhaust every documented
limit without complete-success output. Reverse independent read completion order:
results remain input-index ordered, operation budget unchanged, writes sequential.
Exercise Spec 042 D5 equal/mismatch/current-absent/no-recorded-session/human and
unreadable-record rows; Codex session stays absent/unmeasured, no public session id.
All preexisting JSON contracts remain byte-identical. Missing native/platform
execution remains a named acceptance gap, never a pass.
