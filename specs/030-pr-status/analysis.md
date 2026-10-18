# Cross-artifact analysis and frozen-gate brief

**Candidate status**: Complete artifact package for independent analysis; not self-reviewed CLEAR and not implementation-ready. This author owns these decisions and cannot provide their independent stamp.

## Coverage and counterexample audit

| Counterexample | Disposition in artifacts |
| --- | --- |
| Open PR has non-null raw merge SHA | Contract field table / open fixture: result_sha null, no integration inference |
| Rebase result differs from reviewed head | Separate identities and GitHub qualification; merged-rebase fixture; T004/T007 |
| Missing bool becomes false; missing nullable member becomes null | Presence-aware decoder contract and schema-required fields; T004/T005 |
| Closed-unmerged, null mergeability, denied/404 get mistaken for merged/closed | State/HTTP/exit table; only coherent merged bool/state/date accepted; T004/T008 |
| Fresh reviewed head silently attaches to a different result | Explicitly no review/attestation transfer; expected head compares only head; T007 |
| Changed observed head later returns to initial head | Stops at first changed sample; unobserved ABA explicitly undetectable; T007 |
| Base tip/draft changes are mistaken for head movement | Allowed latest-sample changes; base ref/repository retargeting stops; T007 |
| One request gets 30s on every poll / auth repeats | One work context/auth session and absolute deadline; T006/T008 |
| Terminal eighth sample rejected; ninth read attempted | Predicate-before-count stop; exact budget tests T008 |
| A wait clipped to deadline still permits a poll | No poll after clipped wait/W; global-clock tests T008 |
| Full output pipe or stderr blocks outside HTTP deadline | Explicit delivery A/B decision, measured capability gap and T010; remains blocker |
| Arbitrary writer timeout leaves live goroutine | Prohibited; effective sink deadline/cancellation preflight and joined monitor required |
| V1 is called strict without checking implementation | Corrected after inspecting extensible schema and explicit future-property test; v2 rationale revised |
| New decoder quietly breaks CI or legacy v1 validation | Separate enhanced path; characterization/frozen bytes and shared-caller ledger T004/T006/T009 |
| Last sample survives HTTP failure but appears fresh | Separate sampled_at/observed_at; error/incomplete and stop; denied-after-sample fixture |
| Hostile response supplies endpoint/credentials | Fixed GET only, inherited guard, redaction and bounded escaping tests T008/T009 |

FR/SC-to-task traceability is in [tasks.md](tasks.md); proposed file ownership and F/Ready integration dependencies are in [plan.md](plan.md). The schema encodes structure, required null members, lifecycle/state coherence, mode/stop/budget and success/error constraints. Equality of two arbitrary non-null SHA fields, expectation comparison and timestamps/counter provenance require result/operation tests; backend agreement and expressible nullness/count relationships are schema-enforced; JSON Schema alone is not acceptance proof. Round 1 exposed missing schema-expressible relationships; those are now encoded rather than deferred to Go tests.

## Initial candidate design verification (09274122; retained provenance)

- Temporary source-only output sink probe: `go test -count=1 -v /tmp/stw-spec030-output-*/sink_test.go`, exit 0. Results and limits in [research.md](research.md). No blocked-pipe runtime implementation was tested.
- Temporary source-only offline schema probe using the pinned jsonschema/v6 library, rejecting all external loads: `go test -count=1 -v /tmp/stw-spec030-schema-*/schema_test.go`, exit 0. Seven positive fixtures validated; 11 mutants rejected (wrong version, missing nullable key, null merged bool, open+merged, premerge result, invalid date, short SHA, ninth read, once/watch stop mismatch, incomplete success, error without diagnostic); future property accepted. Probe source is temporary design tooling, not shipping/runtime code.
- `python3 scripts/check-docs.py`: exit 0, final run 266 authored documents.
- `git diff --check`: exit 0, final freeze run.
- Full Go/race suite, CLI/site/artifact build, installation, live API/Enterprise/keyring, terminal/macOS delivery and native conformance were not run. They are not claimed by fixture validation. Runtime remains absent.

## Independent review request

Review the exact committed candidate against all five outcomes, current code evidence and shared consumers, schema/fixtures, state/exit/precedence tables, watch budgets and TDD tasks. Execute proportionate independent artifact checks; do not implement or delegate runtime work. Evaluate D1 compatibility and D2 bounds as engineering proposals within the five requested outcomes. For D3, assess proposed command-owned final-delivery C, cleanup-before-arming, original deadline/cancel arbitration and entry/platform gates against the A refusal/B weakened-delivery alternatives. Return findings with artifact locations; do not stamp runtime delivery acceptance from disposable models.

**Named blockers**: Round 1 independent review was NOT CLEAR; corrected exact revision requires the same independent strongest reviewer. D3 C still requires actual bounded cleanup, prepared-delivery/entry integration, terminal/platform and completion-race acceptance before runtime success claims. Runtime file handoff and reconciliation with F/Ready remain required. No build/release/push/issue-write authorization follows from design review. Dispatch owns the private issue write; a sanitized draft is in [issue-draft.md](issue-draft.md).

**Resume note**: retain this author/worktree for design corrections. Runtime may start only after independently CLEAR design, assigned D3 acceptance obligations and disjoint file assignment. If the main/shared interface moves, re-inspect and update the design before code.

## Round 1 corrections and round 2 gate

Independent review of `09274122cd12857b340135d46f2089cbb1c0bd12`: NOT CLEAR (3 Medium, 1 Low, plus the High implementation gate). Findings and original evidence hashes are retained in [research.md](research.md). No original stamp transfers to this revision.

- **M1**: Reproduced RED all seven schema contradictions plus two grammar counterexamples before edits. Added schema conditionals for stable success/expectation, raw/result nullness, expected-head presence, backend agreement, successful requests and null sample timestamps. Extended the class to request/sample bounds and sample-present expectation; preserved output-limit historical counters with cleared sample/identity fields. Arbitrary non-null SHA equality is still an operation invariant.
- **M2**: Standalone compile with denied external loader failed RED before edits; now the candidate embeds unchanged reused v1 definitions under local $defs and uses local references only. Deep-equality drift checks pass for all five reused definitions; standalone offline compile passes with no separately registered v1 document.
- **L1**: Both PR refs are nonempty; nullable head and non-null base repositories use the actual validRepo segment grammar/1–100 character limits. Extra tests cover base ref/repository, too-long segment and accepted null head repository.
- **M3**: Real issue #74 was created/read back by dispatch; both own focused commits receive `Refs: #74` when this correction is folded. Original commit/tree/message and review-file hashes are retained in private `/tmp/stw-spec030-round2/oid-manifest.json`; no ordinary backup branch is created.
- **D3 High gate**: Assessed proposed C with disposable models only; the contract now owns final process exit at cmd, after all operation cleanup and child waits. Both real executable entry points, actual finite cleanup, terminal/macOS and production races remain pending acceptance, not waived by the model. An expired-before-arming model deliberately demonstrates that finishing cleanup can exceed a shorter deadline; it does not establish bounded real cleanup.

Executed round 2 design checks: `/tmp/stw-spec030-round2/schema_test.go` first failed nine added negative cases plus standalone compilation, then passed all 21 original decisions, seven positive fixtures, standalone compile, five unchanged embedded definitions/local-reference scan and six extra grammar/expectation/output-limit decisions. `/tmp/stw-spec030-round2/final_guard_test.go` passed five decision boundaries, 100 simultaneous-ready callback cases and seven disposable process scenarios (blocked stdout/stderr/root-error stderr, cancellation, ordinary file, /dev/null, expired-before-arming cleanup ordering). No production file, provider request or CLI artifact was involved. Final docs/whitespace results are recorded in the handoff.

Review round 2 must inspect the complete changed package, original findings and D3 C's explicit limitations at the frozen exact head. No build, publication or runtime start is authorized by this correction.
