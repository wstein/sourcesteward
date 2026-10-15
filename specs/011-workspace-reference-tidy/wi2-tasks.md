# WI2 Tasks: Authorized Local Reference Cleanup

## Setup and foundation
- [x] T201 Finalize exact recovery/journal/interface and immutable-schema compatibility, run independent WI2 analyze in specs/011-workspace-reference-tidy/contracts/wi2.md and wi2-analysis.md.
- [x] T202 Write failing old-WI1-observation-plan rejection/new-proposed-effects template validation, prepared/predeclared-root/fsync/recoveryrecord/collision/storefailure/interruption/cancel/possible-pin-effects/outputbound tests in internal/workspace/tidy_apply_test.go and state_test.go.
- [x] T203 Implement strict bounded durable tidy operation journal phases, recovery pointers and conservative retry validation in internal/workspace/tidy_apply.go and state.go.

## US2 — Apply only reviewed valid cleanup
Independent test: exact localtargets only deleted after durable recovery; stale/partial cases retain and report accurately.
- [x] T204 [P] [US2] Write failing Git recoverypin/CAS/loose-packed-refs/unrelated-sibling-preservation/selectedbase/registration-hook/race/noGC/partial tests in internal/workspace/tidy_git_apply_test.go.
- [x] T205 [P] [US2] Write failing jj private-execution-context/no-init-or-registration/sourceGitHEAD-index-WC-preservation/copy-race-and-symlink/recoverybookmark/exact-operation-parent-and-delta/concurrentheads/opstale/remote-counterpart/conflicted/workspaceused/noabandon/partial tests in internal/workspace/tidy_jj_test.go and internal/inspection/jj_tidy_apply_test.go.
- [x] T206 [P] [US2] Implement native private Git zero-OID recovery ref creation and old-tip CAS deletion with exact loose/packed-refs metadata grants, unrelated sibling protections and fixed arguments/nohooks/nonetwork in internal/workspace/tidy_git.go.
- [x] T207 [P] [US2] Implement bounded known-file nofollow private jj execution context with same-repo pointer/no.git/noinit/no source writes, native protected jj recoverybookmark and exact localbookmark deletion with pinned/current operation sampling, sole expected operation parent and only predeclared bookmark deltas in internal/inspection/jj_tidy.go and internal/workspace/tidy_jj.go.
- [x] T208 [US2] Implement ApplyTidy authorization/strict versioned proposed-effects purpose templates rejecting old WI1 read-only plans/store-plan validation/per-action reobservation/predeclared-root preparedjournal/confirmed-root fsync-before-delete and partial possible-root outcomes in internal/workspace/tidy_apply.go; never suppress unrelated changedstate.
- [x] T209 [US2] Implement conservative verified-complete retry and manual recovery guidance, refusing uncertain prepared/incomplete replay in internal/workspace/tidy_apply.go and tidy_apply_test.go.

## US3 — Bounded contract
Independent test: apply errors/partial effects validate same published contract; native effects match action reports.
- [x] T210 [P] [US3] Write failing apply flags/authorization/rendering/schema failure and real native fixtures in internal/cli/tidy_test.go and internal/contracts/tidy_test.go.
- [x] T211 [US3] Expose apply parser and shared recovery/action rendering without weakening prior tidy schema in internal/cli/tidy.go and internal/contracts/tidy_test.go.
- [x] T212 [P] [US3] Extend mandatory exact WI2 manifest/cancel/stale/persistence/remote/helper/effect cases in scripts/smoke-tidy-apply.py, scripts/test_tidy_apply_smoke.py and .github/workflows/ci.yml; missing cases fail.

## Review and handoff
- [x] T213 [P] Document authorization/native recovery roots/retention/manual restoration limits in docs/tidy.md and docs/backends.md; preserve previous capability claims.
- [x] T214 Obtain independent source review and final artifact analysis; run gofmt/make check/race/both native lanes and record actual revisions/commands in specs/011-workspace-reference-tidy/wi2-verification.md.
- [ ] T215 Update tasks/progress/status honestly, focused Conventional Commits and open/attach WI2 PR in specs/011-workspace-reference-tidy/wi2-tasks.md and docs/progress.md.

## Dependencies and parallel work
T201 sourcegate; T202→T203 foundation. Git T204→T206 and jj T205→T207 in separate worktrees;
T208→T209 common integration. CLI T210→T211 and conformance T212 prepare independently.
T213 docs parallel; all precede T214→T215. 15 tasks, US2 six and US3 three. No WI3 source here.
