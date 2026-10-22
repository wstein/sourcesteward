# Tasks: concurrency discipline (Spec 040)

**Input**: [specification](spec.md), [plan](plan.md). Tests are mandatory: write the failing test, watch it fail for the right reason,
then implement. A box is ticked only with the evidence in `docs/progress.md`. No task here builds, releases, tags or pushes the
default branch.

## WP-0 `internal/fanout` and the rule

- [ ] T001 Write failing synctest tests in `internal/fanout/fanout_test.go` for `Do`: late cancellation returns nil, `n == 0` with a done context returns nil, `n > 0` with a done context starts nothing, width bound (gate and peak counter), n=0, width<1, width>n, ctx already done, cancel during, panic re-raise by lowest index, no leak.
- [ ] T002 Implement `Do` in `internal/fanout/fanout.go` (index counter, `width` workers, recovered panic, no timers) to pass T001.
- [ ] T003 Write failing tests and implement `Map`, `Both` and `Start`/`Session` (`Close` waits and is idempotent; `Completed` never blocks a job; a job panic is re-raised from `Close`, the other jobs finish, no leak) (index order under reversed completion; both errors; waits for the slower pump).
- [ ] T004 Write failing tests: the table of `checkGoStatements` rules 1 to 5 in `internal/architecture/gostmt_test.go`, and the end-to-end cases in `scripts/test_architecture.py` (temporary tree with a stray `go`, `STW_ARCH_ROOT`; comment, raw string and `_test.go` ignored; errgroup and WaitGroup import; malformed line, stale entry, `wp N` in `done:`, second `exempt`).
- [ ] T005 Implement `internal/architecture/gostmt_test.go` and `run_go_statement_check()` in `scripts/check-architecture.py`, and create `scripts/go-statements.allow` in the grammar of the specification (`done:` header and nine `wp N` lines for the 14 statements).
- [ ] T006 Run `make check`, `python3 -B scripts/test_architecture.py` and `scripts/check-docs.py`; record the numbers (allowlist total 14) and the killed mutations; update `docs/development.md` with the rule and the allowlist procedure.

## WP-1 `internal/inspection`

- [ ] T007 Characterisation test of `inspectExplicitParallel` and `inspectIndices` output (order, timeout mapping, bounds) with a fake `inspectFn`.
- [ ] T008 Replace the worker loop in `internal/inspection/repos.go` with `fanout.Do`; remove its allowlist line; tests identical.

## WP-2 `internal/issues` and `internal/pulls`

- [ ] T009 Characterise the rate-limit stop and the result order of both cycles; add the `pulls` synctest width test modelled on `internal/issues/concurrency_test.go`.
- [ ] T010 Delete both `pool` functions; call sites use `fanout.Do` with unchanged widths; remove two allowlist lines.

## WP-3 `internal/stwsync`

- [ ] T011 Tests in `internal/stwsync` with a fake Git env and a synctest bubble: (a) characterisation, passes before and after: the peak of concurrent Git calls over many repositories and worktrees is at most `gitConcurrency`; (b) fails today: 40 repositories, 16 Git calls held by a gate, the context is cancelled, the gate released: the number of Git calls the fake env sees afterwards is at most 16 (today the 24 queued repositories still run Git, because `statusRun.g` never checks the context); a skipped repository gets the row a cancelled Git call gives today. No `runtime.NumGoroutine`, no export.
- [ ] T012 Replace both fan-outs with `fanout.Do`; keep the run-wide slots as THE bound on Git processes; remove two allowlist lines and add 3 to `done:`.

## WP-4 `cmd/stw`

- [ ] T013 Test that cancelling by signal releases the handler; replace the goroutine with `context.AfterFunc`; remove one allowlist line.

## WP-5 process plumbing

- [ ] T014 Run the strict stream tests (`release` build, `workspace` stream and patch-id) green before the change; replace the copy, drain and watcher goroutines with `fanout.Start` (`build.go`, `native_stream.go`) and `fanout.Both` (patch-id); run them again plus `-race` once locally; remove four allowlist lines; exact-head independent review.

## WP-6 ref transaction (conditional)

- [ ] T015 Try `fanout.Start` for the session; only if it cannot express it, add the one named exemption for `native_ref_transaction.go` with a written justification and the `exempt` rule test (already in T004) proving a second exemption fails; the allowlist is zero or that one line.

## Closing

- [ ] T016 Confirm `scripts/go-statements.allow` is empty (or holds only the one named exemption with its justification); update `docs/progress.md`, `docs/development.md` and this list with the final evidence.
