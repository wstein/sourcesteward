# Tasks: PR status

**Gate**: all runtime boxes remain unchecked. Complete artifacts do not imply independent review or owner disposition. Each behavior change starts with an observed failing test, then implementation and relevant checks. Commit focused Conventional Commits per work item; no runtime file ownership is granted by this list.

## WI0 — Design gate

- [x] T001 Inspect actual readers, shared callers, transport, schemas, cancellation and exit boundary; verify GitHub meanings using primary documentation. Correct the checkpoint's strict-v1 assumption.
- [x] T002 Specify lifecycle/field/exit tables, migration, compact/detail, expectation, bounded watch and delivery alternatives; create candidate schema and illustrative fixtures.
- [ ] T002a Complete the source-specific candidate blocking-path and bounded operation/teardown/error protocol in [watch-lifetime.md](watch-lifetime.md) and [auth-lifetime.md](auth-lifetime.md). Preserve the rejected direct-child counterexample, accepted30s comparison, full auth, cooperative child reaping and terminal nonexecution/direct-Wait/unconfirmed-orphan distinction. Finite Linux evidence is source-only; no runtime/platform acceptance follows. Round3 rejected the terminal-observation and native-producer closure; [the finite correction disposition](round3-disposition.md) records exact failures and safe hold. No round4 submission until a complete corrected mechanism exists.

- [ ] T003 Dispatch obtains strongest independent cross-artifact review of exact committed revision. Resolve findings; independently review changed rules. Evaluate proposed final-delivery C without weakening inclusive scope; no runtime before independent design CLEAR and named cleanup/entry/race/platform acceptance prerequisites.

## WI1 — Enhanced result and decoder (FR-001/003/009)

- [ ] T004 Add failing v2 schema/decoder tables for each state/field row: open with non-null test SHA; merged merge/squash/rebase with distinct head/result; nullable merged result; closed-unmerged with raw SHA; deleted/null head repo; null mergeable without retries; missing/null/wrong-type merged; missing nullable keys; invalid state/date/full SHA; incoherent dates/state/bool. Unknown fields remain tolerated. Duplicate existing v1/CI behavior guards and frozen-byte comparison pass before any extraction.
- [ ] T005 Implement enhanced typed result and decoder beside legacy types. Register the standalone proposed schema alone offline, with unchanged reused v1 definitions embedded locally and a deep-equality drift check; never load network resources. Add malformed-schema fixtures for required null members, lifecycle inconsistency, bad mode/stop, unexpected version and boundary overflow; Add failing schema counterexamples for every expressible cross-field relationship (including expectation/nullness, backend agreement, successful stability, request positivity, sample timestamp/nullness and output-limit exceptions). Keep pure operation tests for arbitrary non-null SHA equality and temporal counter provenance that JSON Schema cannot express.

## WI2 — Operation and bounded watcher (FR-004/005/007/010)

- [ ] T006 Characterize current resolution/auth/CI/v1 behavior green, trace every shared caller, then extract only the minimal prelude if needed. Add failing enhanced one-shot tests proving exactly one GET for open, merged and null mergeability; exactly one auth session. Native/selection/auth failure makes zero GETs; tokens/raw response errors never appear.
- [ ] T007 Add failing expectation and continuity tests: full expected SHA match/mismatch; compare head rather than result; initial mismatch even when merged; later head/ref/head-repo/base-ref/base-repo changes stop; base tip/draft/title changes allowed; observed A→B cannot recover; unobserved ABA explicitly outside the claim. Invalid new metadata never replaces the prior sample.
- [ ] T008 With injected monotonic clock/sleeper, add failing watcher tests for immediate first poll, exact 1/2/4 capped waits, eight open samples, terminal on eighth read, slow native/auth/read/decode consuming global time, short parent deadline, cancellation in each phase, no read starting at/after W, no wait truncation followed by a poll, request-limit vs timeout precedence and one final result. Implement after each failure. Redirect/denial/rate limit/malformed/oversize stops with no retry or Retry-After sleep; last valid sample remains marked incomplete.

## WI3 — CLI and delivery (FR-002/006/011)

- [ ] T009 Add failing tests for both entry points, invalid flags/combinations/SHA/NUMBER exit 2 before native/auth, v1 rejection of watch/expect, legacy JSON and CI byte behavior unchanged, enhanced JSON/detailed/compact from one result, success/error exit 0/1, compact single line and escaped hostile strings, final output one MiB including escape expansion/newline, writer short-write/error and root diagnostic suppression.
- [ ] T010 Implement only independently CLEAR D3 C with explicit runtime file handoff. Failing source tests first: both executable entry points prepare a resource-free delivery after every native/auth/HTTP close/child Wait/temporary cleanup; library watch without the host protocol refuses before effects; no os.Exit outside cmd ownership. Audit actual cleanup/cancellation bounds under original W/T/D; first reject direct-parent-kill orphaning, premature Wait, incomplete process observations and signals after reap, then demonstrate clean normal controls, two keyring fallback reaps, nonexecuting terminal error, residue diagnostics and supported Darwin observation. No automatic CommandContext kill may race the owned auth protocol. Include expired-before-arming. Final encoding/stdout/root stderr run under one final arbiter; blocked pipes on either stream, partial/error writes, cancel/complete both ready, at/past deadline, no duplicate exit, no post-disarm report and no candidate failure promotion are required cases. Exercise ordinary captured stdout, file, terminal and supported platforms; test model evidence is not production acceptance. Callback models stop/drain/join their monitor; real process exit has no pending external cleanup. No inherited descriptor flag/global environment mutation or detached writer goroutine. If finite cleanup, entry wiring, race or terminal/platform limits remain unresolved, record a named implementation blocker rather than a weaker deadline or a success claim.

## WI4 — Conformance, evidence and documentation (FR-008/012)

- [ ] T011 Add real temporary ordinary/linked Git and standalone/colocated/secondary jj fixtures with fake guarded HTTP; verify gh jj Git refusal, read-only native refs/files/working-copy/operation sentinels, both entry points and minimum/current supported versions. Test permission/config/root conditions through existing refusal seams rather than changing caller-owned configuration. Missing sandbox/tools are explicit skips, not conformance acceptance.
- [ ] T012 Update public hosting/JSON/agent docs, examples, capability limits, architecture if needed, progress/status and verification evidence only after source behavior exists; preserve planned status until acceptance. No generated reference or binary smoke without separately authorized build. Run source checks required by AGENTS/CONTRIBUTING on exact implementation SHA; independent runtime review before publication. Record all unrun platforms/live API/keyring/binary acceptance.

## Mutant and traceability plan

| Mutant / false implementation | Required killer | Coverage |
| --- | --- | --- |
| merged := raw SHA != null; result := head | open-test-SHA and merged-rebase fixtures | T004, FR-001, SC-001 |
| missing bool defaults false; nullable key silently omitted | missing/wrong-type table and schema mutants | T004/005, FR-003/009, SC-002 |
| null mergeable retries by default | one-shot exact request count | T006, FR-005, SC-001 |
| expected compared to result; terminal accepted before mismatch/change | merged mismatch and changed-head terminal tests | T007, FR-004/010, SC-001 |
| recreate timeout/auth/budget every poll; ninth request; poll after shortened wait | fake-clock/counter tests | T008, FR-005/007, SC-003 |
| retry HTTP denial; replace last valid sample on malformed body | no-extra-request/last-sample tests | T008, FR-009, SC-001 |
| timeout only HTTP; unbounded stderr; abandoned writer goroutine | blocked-pipe/root-boundary/monitor tests | T009/010, FR-006/011, SC-003 |
| hidden Git fallback or native write under jj | native ownership and state sentinels | T011, FR-008, SC-004 |
| advertise implemented before usability/review gate | T003/T010/T012 evidence review | FR-012, SC-005 |

No test must merely restate its implementation helper. Use CLI/operation outputs, recorded endpoints, real blocked pipes and native state as the observations. Illustrative JSON fixtures are design examples, not evidence that runtime behavior exists.
