# Tasks: Stack inspection and plan (v2)

**Input**: `spec.md`, `plan.md`, `research.md`, `data-model.md`, `contracts/stack.md`, `contracts/later-effects.md`, `analysis.md`.
**Tests**: Required by constitution principle IV. Write each behavior test and observe it fail before the implementation; characterization and byte-equality guards (T007 and the unchanged-bytes assertions of T003) cannot fail first by construction and are committed green before the code they guard. Task checkboxes record execution, not design approval.
**Status (2026-10-25)**: all tasks below remain unexecuted for Spec 012. Historical M1–M16 measurements and old review rounds are provenance, not checked implementation tasks. This refresh edits Markdown only and runs no implementation acceptance.

**Work items**: The existing six implementation work items WI1 to WI6 are retained to preserve reviewed design sequencing. Their real code dependencies determine order, not an automatic PR stack; each future source change ends with its authorized source checks green. Future publication follows the repository history policy; 012’s candidate open-depth recommendation is observation rather than enforcement of that publication policy. No work item exposes the command on one backend only: the `stack` command is registered in WI4, after both adapters. Each task names the requirements (FR) and stories (US) it covers; success criteria (SC) are named where a task is their evidence. A source-only run never counts as acceptance for a lane it did not execute.
**Execution boundaries**: Future implementation follows the repository contribution, ownership and authorization rules. No artifact, image, release or site build, or live GitHub write, is authorized by a task-list entry alone. T023 needs the owner to create an open native stack if none exists.

## Phase 1: Analysis and measurement

- [ ] T001 Record independent cross-artifact analysis in `specs/012-stack-inspection/analysis.md`, resolve every critical, high and medium finding in the artifacts before any code, and preserve review provenance. Resolve OQ-NET/OQ-FLAGS before implementing the affected online or shared-flag surface; an author recommendation is not an owner answer. Coverage: SC-007.
- [ ] T002 Measure on the pinned jj versions 0.45.1 and 0.46.0 in scratch repositories (ordinary, colocated, secondary workspace) and append the commands, outputs and versions to `research.md`: bookmark and range reads on one pinned operation with `--ignore-working-copy`; the template keywords for conflict, immutability and divergence; tracked remote bookmark state; `jj rebase -s C -o N` moving descendants and the working-copy commit; the facts the round 2 reviewer measured (M13: `-o`/`-d`, quoted literals, keywords, no operation added by `--ignore-working-copy` reads, empty commit for an already applied layer) are re-run by the task's own scripts on both versions and recorded. Any change to the Capability table or the jj plan rule is made in `spec.md` before WI3 starts. Without this task, WI3 does not start. Coverage: FR-003, FR-004, FR-010, U1.

## WI1 — Result, schema and pure derivation (no I/O)

- [ ] T003 Add failing schema fixture tests then add `schemas/v1/stack.schema.json` and `internal/contracts/stack_test.go` with fixtures `internal/cli/testdata/stack-*.json`: ok `show` and `plan` (the contract example), an applied layer, a blocked plan, jj with the capability gap, incomplete (`stack-limit`, `stack-changed`, `stack-remote-failed`), error (`stack-ambiguous-topology`, `stack-ref-invalid`) and remote fixtures. Per-code `detail` validation, `reason` non-null iff `unknown`, observe `layer` null iff note `run-again`, `plan` null when mode is `show` or status is `error` and non-null otherwise, `argv` null iff `display` null; a drift guard that the copied identity definitions equal the hosting schema's; a byte comparison that every existing schema and `internal/cli/testdata/*.json` fixture is unchanged. The byte-equality guard is committed green. Coverage: FR-013, SC-005.
- [ ] T004 [US1] Add failing table tests then implement `deriveFindings` in `internal/inspection/stack_findings.go` and the result types in `stack_result.go`: one named test row per Findings table row and per confidence branch (including `parent-merged-child-stale` kinds `rebase`, `merge`, `rewritten` with and without native stack, `head-sha-differs` relations, `base-mismatch` with a merged and an open parent, `stack-link-missing` versus `stack-split` precedence, `pull-request-missing` with an incomplete candidate list giving `unknown`), the Linkage rule (`verified`, `name-matched`, ambiguity, full page of five), and a test that iterates every table code and fails when a code lacks a row. No finding is derived from a `denied`, `not-found`, `failed` or `skipped` source except as `unknown`. Coverage: FR-006, FR-008, US1, US2, US3.
- [ ] T005 Add failing tests then implement the invariants of the data model: `action` never `unknown`, `unknown` iff a reason, summary equals its template with escaped parameters, deterministic ordering, layers partition the range, status and diagnostic mapping (including precedence when several apply: `error` over `incomplete`, `stack-canceled` over all), bounds (400 findings, 8 refs per layer, 20 fork-point refs, 64 steps, 1 MiB result) yielding `stack-limit` and never a silent truncation. Coverage: FR-006, FR-011, FR-013. Paths: `internal/inspection/stack_result_test.go` and `internal/inspection/stack_result.go`.
- [ ] T006 [US2] Add failing tests then implement `buildPlan` and the Argument rules in `stack_plan.go`: Git rebase chain whose argv begins exactly `["git", "-c", "rebase.updateRefs=false", "-c", "rerere.enabled=false", "rebase", "--onto"]`, with both overrides fixed despite repository config, bottom up with sampled old tips as full object names (SHA-1 and SHA-256 lengths), the new parent as trunk ref then previous primary branch; the single jj step with revset-literal names; `fetch`/`observe` only for missing fetchable objects, `--after-fetch` giving `object-unreachable`, the evidence-chosen notes (`take-upstream`, `native-rewrite-pending`, `upstream-ahead`, `diverged-undecided`) and `stale-base`; a jj remote-tracking base written `"name"@"remote"` (and `"main@origin"` quoted as one literal refused); `head-sha-differs` ignored for merged and closed pull requests; the dropped-set rules per kind and the `merged-layer-not-bottom` block; group precedence when rows co-occur (`stack-behind-base` with a native `parent-merged-child-stale`); the offline and failed-remote `native_checked: false` limitation; landed only when `merge_commit_sha` is a local ancestor of `--base` (stale base for rebase, squash and merge kinds); fetch-first only for missing objects and the terminating evidence-chosen notes for behind/diverged upstreams including after the maintainer's own rebase (a diverged upstream, including a collaborator's commit or an 'Update branch' merge commit, always gives `diverged-undecided` with the two counts and no remedy; no `push-needed` note exists), per-layer observe steps ordered bottom first, upstream `gone` and dropped layers' upstreams ignored, the jj `jj-landed-layer-undetectable` limitation and the backend-independent `landed-undetectable` observe (Git and jj; native stack with the merged member's `head.sha` in the range; a merged member whose head is not in the range changes nothing; a plain chain case that stays unguarded and prints the moved-base step), `after_fetch` echoed; `link` never beside `rebase`; the blocked layer of `merged-layer-not-bottom`; the `link` step with `GH_REPO` and explicit `--base` equal to validated `base.trunk_ref` (including non-default trunk `release`); known remote selector `origin/release` passes only trunk `release`, unknown trunk blocks at layer 1 with `trunk-ref-unknown`, unsafe trunk with `unsafe-name`, and no missing trunk falls back to GitHub’s default; `expected` records `numbers`/`trunk_ref`; every `blocked` reason; unsafe names (leading `-`; jj names ending in `-` quoted as revset literals, names with `+` unsafe, quote, space, control byte, bidirectional control, 201 bytes, `..`, `.lock`) giving `blocked` or an unprintable step and never a partially quoted display; `expected` ids present on every `rebase` step; the plan never contains a step kind or effect outside the table. Coverage: FR-010, FR-014, SC-006, US2.

## WI2 — Git adapter (local, read-only)

- [ ] T007 Add characterization tests then add the thin wrapper around the Spec 021 isolated runner for the process allowlist of `plan.md`: isolation prefix present on every call, explicit `--git-dir`/`--work-tree` only after the ownership validation (per-worktree config, `core.worktree` and `extensions.worktreeConfig` cases of Spec 021 analysis), stdin empty, per-call timeout, no call outside the allowlist (a test fails on any other first argument), the Spec 021 tests unchanged and green. Coverage: FR-015, FR-016.
- [ ] T008 [US1] Add failing tests then implement ref resolution and the range read in `stack_git.go`: base and tip as branches, remote-tracking base, tags and ambiguous names refused (`stack-ref-invalid`, `stack-ref-ambiguous`), range linear and connected, merge commit (`stack-ambiguous-topology`), root commit (`stack-no-common-base`), tip contained in the base (`stack-landed`), detached and unborn HEAD, bare repository, 500 and 501 commits (`stack-limit`), a range whose parent output nears the runner scan allowance. Coverage: FR-002, FR-004, US1.
- [ ] T009 [US1] Add failing tests then implement layers, `layer-ref-multiple`, `fork-point` (including a sibling branch and remote-tracking refs not counted), `stack-behind-base` with the capped count, upstream relation (`head-differs-from-upstream`: same, ahead, behind, diverged, upstream object absent) and bounded commit subjects with hostile text. Coverage: FR-004, FR-006, US1.
- [ ] T010 [US2] Add failing tests then implement patch equivalence through `cherry` and fixtures for the three merge shapes of the spike built in temporary repositories (merge commit: no finding; rebase merge: `layer-applied-in-base`, rebase chain of the contract; squash: a one-commit layer is patch-equivalent and a multi-commit layer is `pending`); partially applied layer blocks the plan; repeated old-tip recording across three layers; the plan commands, run by the test itself on a copy of the fixture, produce the expected history (the test executes the printed argv in a scratch clone, never through `stw`). In that disposable copy enable `rebase.updateRefs=true` and `rerere.enabled=true`, retain an unrelated side ref into the moved history and preseed rerere state; assert the fixed overrides remain in the display/argv, only the selected branch changes and rr-cache is unchanged. These are future scratch tests, not current execution or a change to 015 D7. Coverage: FR-006, FR-010, US2, SC-002.
- [ ] T011 [US2] Add failing tests then implement `layer-checked-out-elsewhere` from `worktree list --porcelain -z` for ordinary and linked worktrees, a layer checked out in the inspected worktree itself (not blocked), prunable and locked worktrees, and paths with newlines. Coverage: FR-004, FR-010, US2.
- [ ] T012 [US1] Run the Git fixtures of T008 to T011 on the minimum (2.46.0) and current (2.56.0) lanes, adjust only with evidence, and record versions and outcomes in `research.md`; an unavailable lane is recorded as not run. Coverage: FR-016, SC-007, U2.
- [ ] T013 [US4] Add failing sentinel tests: before/after equality of refs, reflogs, index, working-tree files, packed-refs, loose and packed object counts (a `merge-tree` call would fail this test), and the absence of `.git/gh-stack`, with no hook, filter, textconv, external diff, fsmonitor or helper executed (planted markers) and no network access (an environment with no route is not assumed; the process allowlist test plus a recorder of started programs is the evidence). Coverage: FR-005, FR-015, SC-004. Paths: `internal/inspection/stack_git_test.go`.
- [ ] T014 [US4] Add failing hostile tests: ref names and subjects with control bytes, ANSI, bidirectional controls and 200-byte-plus text, planted `.gitattributes` `diff`/`filter`/`merge` drivers and `diff.external`, binary and very large patches under `cherry`, replace refs and `info/grafts`, a repository owned by another user or with world-writable Git directory (existing ownership refusals), hooks directories. Prove nothing executes and every refusal happens before reads. Coverage: FR-014, FR-015, U9, US4. Paths: `internal/inspection/stack_git_test.go`.
- [ ] T015 [US4] Add failing tests then implement limits, the resample (ref moved between samples, branch deleted, tip advanced, base advanced) giving `stack-changed`, per-process and total deadlines, and cancellation giving `stack-canceled`; no Git process remains running after any stop. Coverage: FR-011, FR-012, US4. Paths: `internal/inspection/stack_git_test.go` and `internal/inspection/stack_git.go`.

## WI3 — jj adapter (local, read-only); starts after T002

- [ ] T016 [US1] Add failing tests on real jj repositories (standalone, colocated, secondary workspace) then implement `stack_jj.go`: pinned-operation session, bookmark resolution, range, layers, tracked remote bookmark upstream relation, `stack-behind-base`, `stack-landed`, `fork-point`, ambiguity cases (conflicted bookmark, divergent change, merge commit) refusing with `stack-ambiguous-topology`; jj ownership including colocation never reaches the Git adapter. Coverage: FR-002, FR-003, FR-004, US1.
- [ ] T017 [US2] Add failing tests then implement `layer-conflicted`, `layer-immutable`, the once-per-stack `capability-unavailable` finding, `landed: unknown`, and the one-step jj plan from the T002 results (the test executes the printed argv in a scratch copy and checks the resulting stack; it also checks the already applied layer case recorded in T002, the remote-deletion case of scenario 8b including the empty commit after a rebase-merge, and both multi-commit squash outcomes (different files: both emptied and abandoned by `--skip-emptied`; overlapping lines: first commit conflicted and `--skip-emptied` without effect), and the `jj-landed-layer-undetectable` limitation on every jj result). Coverage: FR-003, FR-006, FR-010, US2.
- [ ] T018 [US4] Add failing jj sentinel tests: the stored working copy, the operation log (no new operation), bookmarks, and files unchanged; `--ignore-working-copy` on every call (no `jj op diff` or other unlisted read); no hook, filter or helper; `jj-sandbox-unavailable` and `jj-timeout` mapping unchanged from Spec 008. Coverage: FR-005, FR-015, SC-004. Paths: `internal/inspection/stack_jj_test.go`.
- [ ] T019 [US4] Add failing jj hostile, limit, resample (operation advanced between samples) and cancellation tests mirroring T014 and T015, including bookmark names that fail the Argument rules and Git-ref-incompatible bookmark names. Coverage: FR-011, FR-012, FR-014, US4. Paths: `internal/inspection/stack_jj_test.go`.

## WI4 — Command layer and local conformance

- [ ] T020 [US4] Add failing CLI tests then implement `stack show|plan` in `internal/cli/stack.go`: required flags and flag combinations (`--after-fetch` with `show` is exit 2) (`--repo` without `--pull-requests` is exit 2 with no JSON), human output grouped as in the contract with escaping and the wording rule (every finding line is its template; no sentence says the stack is correct), JSON from the same result, exit statuses 0, 1 and 2, no `gh jj` leaf, and the injection seam. Assert the current draft rejects `--online`, `--offline`, `--human`, `--timeout` and `--state-dir` with usage exit 2, until OQ-NET/OQ-FLAGS explicitly select a changed surface; then update the Commands table and flag tests together before implementation. Existing commands, flags and help are unchanged (golden comparison). Coverage: FR-001, FR-013, FR-014, SC-005, US4.
- [ ] T021 [US4] Add failing native conformance tests in `internal/contracts` using real temporary repositories: ordinary and linked Git worktrees, standalone, colocated and secondary jj; ambiguous backend, GitButler and bare repository refuse before any read; the same stack gives the same layers and findings on Git and jj apart from the stated gap; sentinels of T013 and T018 around the CLI; minimum and current lanes; the existing schemas unchanged. Coverage: FR-003, FR-016, SC-001, SC-002, SC-004, US1, US4.
- [ ] T022 [US4] Add `scripts/smoke-stack.py` (local cases on temporary repositories through the built binary: help, usage errors, a three-layer stack on Git and, where jj is installed, jj; refusals) with a unit test file for the observer, and wire it into the existing smoke call only; add no CI platform. Coverage: FR-016.

## WI5 — Opt-in remote evidence (depends on Specs 020 and 022; rebases over Spec 025 WI1)

- [ ] T023 [US3] Measure with read-only calls and record in `research.md` before any decoder is written: the `head=O:REF` filter and refs containing `/`; whether filtered list items carry `stack`; an open native stack (`open: true`, member base refs while open, positions after a middle merge); `stacks` pagination and `per_page`; `merge_commit_sha` for a multi-commit rebase merge and a squash merge; a fork pull request; the documented stack size limit. The owner creates any open stack or pull request needed; no write is made by this task. Coverage: FR-007, FR-008, U3, U4, U5, U6, U8, U10.
- [ ] T024 [US3] Add failing tests then extend the closed allowlist in `internal/hosting/transport.go` by exactly the two shapes of the Remote sources table; refusal tests for everything the guard can tell apart (non-GET, extra or reordered query, `per_page` other than 5, `state` other than `all`, a second `stacks` path segment, `RawPath`, userinfo, fragment, redirect, other host) before credentials are attached; update `specs/022-bounded-go-gh/contracts/transport.md` and the Spec 020 endpoint table in the same change. If Spec 025 WI1 has landed, reuse its typed failure classes; otherwise map every non-success status to `failed`. Coverage: FR-007, SC-004.
- [ ] T025 [US3] Add failing tests then implement `ExplainStack` and `ExplainStackWithTransport` in `internal/hosting/stack_remote.go`: local phase first, hosting resolution before any request, no request without `--pull-requests`, remote failure keeping the local result (`incomplete`), request order and budget (2 x 20 + 8 within 50) with a counting wrapper that rechecks the allowlist, resample of the pull request reads, cancellation giving `stack-canceled`. Coverage: FR-005, FR-007, FR-012, US3.
- [ ] T026 [US3] Add failing tests then implement the decoders for `pull-requests-by-head` and `native-stack`: malformed-shape table (missing and mistyped fields, `stack` absent versus `null` versus malformed, non-hex SHAs, invalid UTF-8, duplicate numbers, `head.repo` null, `merged_at` null with `state: closed`), a full page of five, oversized bodies, `stacks/K` members beyond 32. Coverage: FR-007, FR-011, US3.
- [ ] T027 [US3] Add failing tests then implement the Linkage rule and the remote findings against fixtures replaying M3 to M5 (closed three-member stack, the `stack` object, a plain chain without the key), including `stack-link-missing`, `stack-split`, `stack-member-not-local`, `stack-order-differs`, `native-stack-managed`, `parent-merged-child-stale` for each `parent_merge` and for native and plain chains, `parent-merge-not-local`, and the assertion that no fixture is `verified` without a head commit match, and a summary assertion preserving M3’s head-deletion-enabled-only qualification rather than claiming the behavior was measured with that setting off. Coverage: FR-008, FR-009, US3, SC-003.
- [ ] T028 [US3] Add failing permission-matrix tests: each source `denied`, `not-found`, `rate-limited`, `failed` and oversized; unauthenticated; repository selection failure; the Enterprise-style `404` on `stacks/K` (modelled, unverified); assert local layers and findings are retained, remote findings are `unknown` with the reason, the status is `incomplete`, and no fixture yields an absence claim. Coverage: FR-006, FR-007, SC-003, US3. Paths: `internal/hosting/stack_remote_test.go`.
- [ ] T029 [US2] Add failing scenario tests that run the three merge shapes of the spike end to end with fake HTTP and real repositories on Git and jj, including the acceptance scenarios 6 to 10 of User Story 2 (multi-commit squash, stale base for each merge kind, fetch-first versus reconcile and termination, merged-layer-not-bottom, offline and failed-remote `native_checked`, link versus rebase), the native-stack rebase merge (all objects local: a single `observe` `take-upstream` or `native-rewrite-pending`, never `rebase`; with a missing object: `fetch` and `observe`, then `object-unreachable` under `--after-fetch`) using an open-pull-request head fixture, plus a merged pull request whose deleted-branch head (the spike's `6e019b0`) is never required and the plain-chain rebase merge (the `rebase` chain; a `link` step only in a plan without `rebase`, so a later run after the rewrite and push), plus scenario 8b on Git (`fetch --prune` with a `gone` upstream on an applied layer) and the group 5 guard on Git and jj: a native stack whose merged member's `head.sha` is in the range and `stack-behind-base` the only trigger gives `landed-undetectable` (not the group 4 note, since the merged member is not a local layer; Git case: squash- and rebase-merge with the local `s1` deleted after `fetch --prune`, where a rebase-merge or one-commit squash may instead block as `layer-partially-applied`), plus the limitation on every jj result; one observe per affected layer when several merged heads land in different layers (a fixture-only case that needs fake HTTP); a merged member whose head is outside the range changes nothing; a plain-chain case that stays unguarded and prints the moved-base step with the disclosure (jj: the limitation and the empty or conflicted outcomes; Git with the local `s1` deleted and `--pull-requests`: the printed `git -c rebase.updateRefs=false -c rerere.enabled=false rebase --onto` drops the commits already upstream or halts on an add/add conflict, asserted by executing the argv in a scratch copy), a collaborator's commit and an 'Update branch' merge commit on a layer upstream (`diverged-undecided` with counts), and a jj tracked bookmark moved locally and rewritten by the remote (refusal). Coverage: FR-009, FR-010, SC-002, US2, US3.
- [ ] T030 [US4] Add failing hostile remote tests: titles, refs and logins with control bytes, ANSI and bidirectional controls and long text; a head repository URL pretending to be an endpoint; response bombs, compressed bodies and redirects; a credential-bearing error body; every recorded request is an allowlisted `GET`; no body or token reaches a diagnostic. Coverage: FR-014, SC-004, SC-006, US4. Paths: `internal/hosting/stack_remote_test.go`.
- [ ] T031 [US3] Add remote conformance: fake HTTP with real Git (ordinary, linked worktree) and jj (standalone, colocated, secondary) repositories, request recorder, sentinels around the run, and the existing schemas and hosting fixtures byte-identical. Coverage: FR-016, SC-001, SC-004. Paths: `internal/contracts/stack_test.go`.

## WI6 — Documentation and acceptance evidence

- [ ] T032 [US5] Add `docs/stack.md` (commands, findings, confidence, native-stack handling, plan, what the output does not prove, the later-effects catalog link) and update `docs/json-contracts.md`, `docs/agent-guide.md`, `docs/commands.md` (generated reference through `cmd/stw-docs` and the existing documentation tests), `ARCHITECTURE.md`, `docs/README.md`, `docs/hosting.md` (allowlist), `llms.txt` and `docs/progress.md`; `python3 scripts/check-docs.py` passes. Do not run a site build without approval. Coverage: FR-009, FR-015.
- [ ] T033 Run `make check`, `go test -count=1 -race ./...`, `go mod verify`, `python3 scripts/check-architecture.py`, `git diff --check` and the smoke script on the existing lanes; record exact versions, platforms and outcomes in `specs/012-stack-inspection/verification.md`, labelling arm64 and darwin results as local evidence and listing everything not run, including every unverified item of `research.md` that stayed unverified. Coverage: FR-016, FR-017, SC-007.
- [ ] T034 Update `docs/feature-status.json` (`planned` to `implemented`, evidence link) with the actual evidence; leave release, tag, publication, live-network acceptance and every item of `contracts/later-effects.md` out of this feature. Coverage: FR-016, SC-007.

## Toolbelt refresh acceptance tasks (proposed, not executed)

- [ ] T035 [US1] In `internal/inspection/stack_findings_test.go` add failing fixtures for complete all-pending candidate open depths 2, 3 and 32, total local depth 3 with one proven-landed parent, unknown jj/offline landing, closed-unmerged parents, non-prefix landing, incomplete/error evidence and 33 local layers; then implement FR-018 in `internal/inspection/stack_findings.go`. Assert `depth == len(layers)`, candidate `open_depth` excludes only proven-landed layers with stale-base precedence, and null open_depth for incomplete/error. Notice is inferred only for complete candidate open_depth >2, with detail `{depth, open_depth, recommended_max: 2}`; depth 3/open_depth 2 has no notice, no fixture treats candidate bounds as verified open-PR counts, and 33 gives independent stack-limit. Retain all bounded local layers with unchanged status/exit/plan blocking caused by advice alone. Extend future contract fixtures in `internal/contracts/stack_test.go` for both result fields and qualified notice shape/summary. Coverage: FR-018, SC-008.
- [ ] T036 [US5] In `internal/cli/stack_test.go` and `specs/012-stack-inspection/contracts/later-effects.md`, assert no-subcommand help starts no observation/prompt; show/plan never invoke 013/014/015/016/043, run a printed argv, write a store or perform owner resync. Verify the data-model field-to-flag mapping: selected short Git branch becomes a validated `refs/heads/...`; old parent/new first base/child tip map to `--old-parent`/`--new-parent`/`--expect-head`; later null destinations require the preceding actual apply outcome and a fresh plan. Reject missing predecessor receipts, jj/64-hex input to Git SHA-1-only v1 and identity derived from any 012/config/environment field. Assert explicit caller-only `--identity` and the diagnostic reconciliation table’s actual per-command exits: 012 stack-limit 1 versus 015 bound-exceeded 3, invalid/missing/not-commit and ambiguous errors 1, usage syntax 2 with no result JSON. Boundaries remain sampled ids, not an apply plan id, receipt or lease. Keep actual-head independent review before any separate 016 lease push in the handoff documentation; inherited observation/local apply never supplies remote authorization. Coverage: FR-001, FR-009, FR-010, FR-019, SC-004.
- [ ] T037 [US4] BLOCKED until OQ-FANOUT approves a concrete reachable caller entry point and its contract; do not create unused production batch code or silently choose an internal-only API. After that approval, in `internal/inspection/stack_test.go`, `internal/hosting/stack_remote_test.go` and `internal/hosting/stack_batch_test.go`, add failing caller-orchestration tests over an explicit mixed Git/jj repository list, including reverse completion, a per-entry ownership refusal, distinct bases/tips, no inherited online/auth selection, independent resamples, cancellation before start/during reads and shared request/output/deadline exhaustion. Implement `internal/hosting/stack_batch.go` only at the caller edge with Spec 040 `internal/fanout`, 1..16 selected entries and at most four workers (reuse `specs/023-multi-repo-inventory/contracts/repos.md` bounds), preallocated input-index outcomes and a single shared 30-second/50-GET/1-MiB allowance rather than multiplied worker allowances. Preserve each started entry’s native status, identities, findings and sample time; canceled/not-started or output-limited entries remain explicit incomplete caller outcomes and never silently disappear or become success. Assert caller aggregate exit 1 for any incomplete/refused/error/canceled/not-started/output-limited member, exit 0 only for all-complete observations (never readiness), and usage exit 2 for invalid selection before workers start. Preserve existing single-PATH behavior, per-repository caps and published schemas. If OQ-FANOUT selects a new CLI, amend the Commands table/contract and entry tests before production orchestration; otherwise assert no new flag or CLI surface. Do not add unselected discovery/aggregate JSON, implicit network, ad hoc `go`, polling or shared-map append. Keep `stack_findings.go` and `stack_plan.go` pure; run the repository architecture rule in the later authorized source change. Coverage: FR-017, FR-020, SC-006.
- [ ] T038 [US2] In `internal/inspection/stack_findings_test.go` and `internal/hosting/stack_remote_test.go`, add failing `restack-advised` cases: offline Git rebase-merge with child indices pointing to `Layer.fork_commit_id`/tip and sampled base (not ids invented inside the finding), plan `expected` for the first destination and later null destinations awaiting actual predecessor apply, and `native_status: unchecked`; confirmed plain chain; confirmed native stack suppressed; parent merge object absent or outside base suppressed; closed-unmerged parent never landed. A child based on a merged/closed parent reports `base-mismatch` without retarget. Test merge-commit omission, multi-commit squash and conflict-resolution patch-id mismatch as qualified uncertainty, never cleanup/identity/review/lease proof. Add distinct namespace fixtures for the existing 035 top-level restack-needed watch event versus 012’s nested restack-advised finding: event new_parent is merge_commit_sha C, while a later selected base B may differ; neither signal requires the other, schemas/payloads never coerce, event argv never executes and native boundaries/destination are revalidated for a new effect plan. Implement only the Findings table rule and preserve six-group precedence; extend `internal/contracts/stack_test.go` for the new notice's model. Coverage: FR-006, FR-008, FR-010, FR-019, SC-002, SC-003.

## Deferred (not scheduled; each needs its own specification amendment, analysis and authorization)

L1 `stack link`, L2 `stack push`, L3 `stack sync`, L4 `stack unstack`, L5 `stack merge` and L6 separately planned local restack are defined in [contracts/later-effects.md](contracts/later-effects.md) with owners (Specs 015 and 016) and preconditions. No task of WI1 to WI6 or T035–T038 implements, stubs or flags an effect. Spec 015 D7 forbids executing the printed porcelain rebase argv; separate plans use the same exact replay boundary through native plumbing/CAS.

## Dependencies and parallel work

T001 gates all code; OQ-FANOUT approval of a concrete reachable caller specifically gates production T037 orchestration, and no unused batch helper is added while it is unresolved. T035 and T038 extend the WI1 pure derivation before adapters; T036 extends WI4/WI6 effect-handoff checks and T037 extends WI2/WI3/WI5 edge checks.  T002 gates WI3 only. WI1 first (pure, no I/O). WI2 and WI3 are independent after WI1 and may be authored in parallel worktrees; both finish before WI4. WI5 needs WI4 and starts with T023 (measurement, owner action). T024 touches `transport.go` and must rebase over Spec 025 WI1 and any Spec 022 follow-up. WI6 last. A task whose failing test cannot be observed to fail is not complete.

## Coverage map

| Requirement, criterion or story | Tasks |
| --- | --- |
| FR-001 | T020, T036 |
| FR-002 | T008, T016 |
| FR-003 | T002, T016, T017, T021 |
| FR-004 | T002, T008, T009, T011, T016 |
| FR-005 | T013, T018, T025 |
| FR-006 | T004, T005, T009, T010, T017, T028, T038 |
| FR-007 | T023, T024, T025, T026, T028 |
| FR-008 | T004, T023, T027, T038 |
| FR-009 | T027, T029, T032, T036 |
| FR-010 | T002, T006, T010, T011, T017, T029, T036, T038 |
| FR-011 | T005, T015, T019, T026 |
| FR-012 | T015, T019, T025 |
| FR-013 | T003, T005, T020 |
| FR-014 | T006, T014, T019, T020, T030 |
| FR-015 | T007, T013, T014, T018, T032 |
| FR-016 | T007, T012, T021, T022, T031, T033, T034 |
| FR-017 | T033, T037 |
| FR-018 | T035 |
| FR-019 | T036, T038 |
| FR-020 | T037 |
| SC-001 | T021, T031 |
| SC-002 | T010, T021, T029, T038 |
| SC-003 | T027, T028, T038 |
| SC-004 | T013, T018, T021, T024, T030, T031, T036 |
| SC-005 | T003, T020 |
| SC-006 | T006, T030, T037 |
| SC-007 | T001, T012, T033, T034 |
| SC-008 | T035 |
| US1 | T004, T008, T009, T012, T016, T021, T035 |
| US2 | T004, T006, T010, T011, T017, T029, T038 |
| US3 | T004, T023, T024, T025, T026, T027, T028, T029, T031 |
| US4 | T013, T014, T015, T018, T019, T020, T021, T022, T030, T037 |
| US5 | T032, T036 |

## FR-to-scenario map (SC-007 traceability)

Scenarios are those of [spec.md](spec.md); `USn.k` is acceptance scenario k of User Story n, `8a` the evidence-based reconcile scenario of User Story 2.

| FR | Acceptance scenarios |
| --- | --- |
| FR-001 | US1.1, US4.1 |
| FR-002 | US1.1, US1.2, US2.2 |
| FR-003 | US1.5, US2.2, US4.4 |
| FR-004 | US1.1, US1.2, US1.3, US1.4, US2.8a |
| FR-005 | US4.4 |
| FR-006 | US1.3, US2.1, US2.3, US2.8a, US2.8b, US3.1 to US3.6 |
| FR-007 | US3.1 to US3.6 |
| FR-008 | US3.5 |
| FR-009 | US2.8, US2.8a, US3.1, US3.4, US5.1 |
| FR-010 | US2.1 to US2.11 (including 8, 8a and 8b), US5.2 |
| FR-011 | US4.3 |
| FR-012 | US4.3 |
| FR-013 | US4.1 |
| FR-014 | US4.2 |
| FR-015 | US4.4, US5.1 |
| FR-016 | US1.5, US4.4 (conformance lanes) |
| FR-017 | verified by T033/T037 (dependency and architecture checks) |
| FR-018 | US1.6, T035 |
| FR-019 | US2.12, US5.1, T036, T038 |
| FR-020 | US4.3, US4.4, T037 |

## Reverse task coverage

These are the same edges as the forward table, derived from each task’s explicit Coverage plus US label; shared foundation/evidence tasks have no story label. A task may cover several stories, so story coverage overlaps rather than changing the 38-task count.

| Task | FR/SC coverage | Story coverage |
| --- | --- | --- |
| T001 | SC-007 | Shared foundation/evidence |
| T002 | FR-003, FR-004, FR-010 | Shared foundation/evidence |
| T003 | FR-013, SC-005 | Shared foundation/evidence |
| T004 | FR-006, FR-008 | US1, US2, US3 |
| T005 | FR-006, FR-011, FR-013 | Shared foundation/evidence |
| T006 | FR-010, FR-014, SC-006 | US2 |
| T007 | FR-015, FR-016 | Shared foundation/evidence |
| T008 | FR-002, FR-004 | US1 |
| T009 | FR-004, FR-006 | US1 |
| T010 | FR-006, FR-010, SC-002 | US2 |
| T011 | FR-004, FR-010 | US2 |
| T012 | FR-016, SC-007 | US1 |
| T013 | FR-005, FR-015, SC-004 | US4 |
| T014 | FR-014, FR-015 | US4 |
| T015 | FR-011, FR-012 | US4 |
| T016 | FR-002, FR-003, FR-004 | US1 |
| T017 | FR-003, FR-006, FR-010 | US2 |
| T018 | FR-005, FR-015, SC-004 | US4 |
| T019 | FR-011, FR-012, FR-014 | US4 |
| T020 | FR-001, FR-013, FR-014, SC-005 | US4 |
| T021 | FR-003, FR-016, SC-001, SC-002, SC-004 | US1, US4 |
| T022 | FR-016 | US4 |
| T023 | FR-007, FR-008 | US3 |
| T024 | FR-007, SC-004 | US3 |
| T025 | FR-005, FR-007, FR-012 | US3 |
| T026 | FR-007, FR-011 | US3 |
| T027 | FR-008, FR-009, SC-003 | US3 |
| T028 | FR-006, FR-007, SC-003 | US3 |
| T029 | FR-009, FR-010, SC-002 | US2, US3 |
| T030 | FR-014, SC-004, SC-006 | US4 |
| T031 | FR-016, SC-001, SC-004 | US3 |
| T032 | FR-009, FR-015 | US5 |
| T033 | FR-016, FR-017, SC-007 | Shared foundation/evidence |
| T034 | FR-016, SC-007 | Shared foundation/evidence |
| T035 | FR-018, SC-008 | US1 |
| T036 | FR-001, FR-009, FR-010, FR-019, SC-004 | US5 |
| T037 | FR-017, FR-020, SC-006 | US4 |
| T038 | FR-006, FR-008, FR-010, FR-019, SC-002, SC-003 | US2 |

## Story acceptance and parallel opportunities

US1 independently proves local topology, native ownership and advisory depth (T008/T009/T016/T035). US2 independently proves qualified rewritten-parent findings and exact inert replay boundaries (T006/T010/T017/T029/T038). US3 independently proves bounded opt-in PR/native-stack mapping with fake HTTP (T025–T028/T031). US4 independently proves matching human/JSON results, effects, cancellation and deterministic edge ordering (T013–T022/T030/T037). US5 independently proves that catalogued effects are separate and never invoked (T032/T036). The smallest source MVP is the pure model plus both native local adapters and the command layer; an unavailable required backend is not a pass.

WI2 and WI3 can be implemented independently after WI1 and T002's jj gate, in separate owned files/workspaces; remote decoder tests can be prepared after T023 without changing local adapters. Pure test cases within T035/T038 are independent in design but share a test file and must have one editor. No parallel opportunity implies a stacked documentation PR or authority to run another toolbelt operation.
