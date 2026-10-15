# WI2 Tasks: Authorized Local Reference Cleanup

## Setup and foundation
- [ ] T201 Finalize exact recovery/journal/interface and immutable-schema compatibility, run independent WI2 analyze in specs/011-workspace-reference-tidy/contracts/wi2.md and wi2-analysis.md.
- [ ] T202 Write failing prepared/fsync/recoveryrecord/collision/storefailure/interruption/cancel/outputbound tests in internal/workspace/tidy_apply_test.go and state_test.go.
- [ ] T203 Implement strict bounded durable tidy operation journal phases, recovery pointers and conservative retry validation in internal/workspace/tidy_apply.go and state.go.

## US2 — Apply only reviewed valid cleanup
Independent test: exact localtargets only deleted after durable recovery; stale/partial cases retain and report accurately.
- [ ] T204 [P] [US2] Write failing Git recoverypin/CAS/selectedbase/registration-hook/race/noGC/partial tests in internal/workspace/tidy_git_test.go.
- [ ] T205 [P] [US2] Write failing jj recoverybookmark/opstale/remote-counterpart/conflicted/workspaceused/noabandon/partial tests in internal/workspace/tidy_jj_test.go and internal/inspection/jj_tidy_test.go.
- [ ] T206 [P] [US2] Implement native private Git zero-OID recovery ref creation and old-tip CAS deletion with fixed arguments/nohooks/nonetwork in internal/workspace/tidy_git.go.
- [ ] T207 [P] [US2] Implement native protected jj recoverybookmark and exact localbookmark deletion with pinned/current operation sampling in internal/inspection/jj_tidy.go and internal/workspace/tidy_jj.go.
- [ ] T208 [US2] Implement ApplyTidy authorization/store-plan validation/per-action reobservation/recovery-before-delete and partial outcomes in internal/workspace/tidy_apply.go; never suppress unrelated changedstate.
- [ ] T209 [US2] Implement conservative verified-complete retry and manual recovery guidance, refusing uncertain prepared/incomplete replay in internal/workspace/tidy_apply.go and tidy_apply_test.go.

## US3 — Bounded contract
Independent test: apply errors/partial effects validate same published contract; native effects match action reports.
- [ ] T210 [P] [US3] Write failing apply flags/authorization/rendering/schema failure and real native fixtures in internal/cli/tidy_test.go and internal/contracts/tidy_test.go.
- [ ] T211 [US3] Expose apply parser and shared recovery/action rendering without weakening prior tidy schema in internal/cli/tidy.go and internal/contracts/tidy_test.go.
- [ ] T212 [P] [US3] Extend mandatory exact WI2 manifest/cancel/stale/persistence/remote/helper/effect cases in scripts/smoke-tidy.py, scripts/test_tidy_smoke.py and .github/workflows/ci.yml; missing cases fail.

## Review and handoff
- [ ] T213 [P] Document authorization/native recovery roots/retention/manual restoration limits in docs/tidy.md and docs/backends.md; preserve previous capability claims.
- [ ] T214 Obtain independent source review and final artifact analysis; run gofmt/make check/race/both native lanes and record actual revisions/commands in specs/011-workspace-reference-tidy/wi2-verification.md.
- [ ] T215 Update tasks/progress/status honestly, focused Conventional Commits and open/attach WI2 PR in specs/011-workspace-reference-tidy/wi2-tasks.md and docs/progress.md.

## Dependencies and parallel work
T201 sourcegate; T202→T203 foundation. Git T204→T206 and jj T205→T207 in separate worktrees;
T208→T209 common integration. CLI T210→T211 and conformance T212 prepare independently.
T213 docs parallel; all precede T214→T215. 15 tasks, US2 six and US3 three. No WI3 source here.
