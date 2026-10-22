# Tasks: concurrency discipline (Spec 040)

**Input**: [specification](spec.md), [plan](plan.md). Tests are mandatory: write the failing test, watch it fail for the right reason,
then implement. A box is ticked only with the evidence in `docs/progress.md`. No task here builds, releases, tags or pushes the
default branch.

## WP-0 `internal/fanout` and the rule

- [ ] T001 Write failing synctest tests in `internal/fanout/fanout_test.go` for `Do`: width bound (gate and peak counter), n=0, width<1, width>n, ctx already done, cancel during, panic re-raise by lowest index, no leak.
- [ ] T002 Implement `Do` in `internal/fanout/fanout.go` (index counter, `width` workers, recovered panic, no timers) to pass T001.
- [ ] T003 Write failing tests and implement `Map` and `Both` (index order under reversed completion; both errors; waits for the slower pump).
- [ ] T004 Write failing mutation tests in `scripts/test_architecture.py` for `validate_go_statements` (stray `go`, stale entry, entry for a `fanout` or missing file, errgroup and WaitGroup import) and for `scripts/gostmt` (comment, raw string, `_test.go`, generated-looking file).
- [ ] T005 Implement `scripts/gostmt/main.go`, `validate_go_statements` and the runner in `scripts/check-architecture.py`, and create `scripts/go-statements.allow` with the 14 current statements (one commented line per file, naming the WP that removes it).
- [ ] T006 Run `make check`, `python3 -B scripts/test_architecture.py` and `scripts/check-docs.py`; record the numbers (allowlist total 14) and the killed mutations; update `docs/development.md` with the rule and the allowlist procedure.

## WP-1 `internal/inspection`

- [ ] T007 Characterisation test of `inspectExplicitParallel` and `inspectIndices` output (order, timeout mapping, bounds) with a fake `inspectFn`.
- [ ] T008 Replace the worker loop in `internal/inspection/repos.go` with `fanout.Do`; remove its allowlist line; tests identical.

## WP-2 `internal/issues` and `internal/pulls`

- [ ] T009 Characterise the rate-limit stop and the result order of both cycles; add the `pulls` synctest width test modelled on `internal/issues/concurrency_test.go`.
- [ ] T010 Delete both `pool` functions; call sites use `fanout.Do` with unchanged widths; remove two allowlist lines.

## WP-3 `internal/stwsync`

- [ ] T011 Failing bubble test: no more than `gitConcurrency` `git status` calls at once over many worktrees and repositories.
- [ ] T012 Replace both fan-outs with `fanout.Do`; state the worktree width in the PR as the one behaviour change; remove two allowlist lines.

## WP-4 `cmd/stw`

- [ ] T013 Test that cancelling by signal releases the handler; replace the goroutine with `context.AfterFunc`; remove one allowlist line.

## WP-5 process plumbing

- [ ] T014 Run the strict stream tests (`release` build, `workspace` stream and patch-id) green before the change; replace the copy and drain goroutines with `fanout.Both`; run them again plus `-race` once locally; remove four allowlist lines; exact-head independent review.

## WP-6 ref transaction (conditional on the owner)

- [ ] T015 Record the owner decision on question 1; either implement `fanout.Session` with tests or add the permanent named exemption with its reason; the allowlist is zero or contains that one line.

## Closing

- [ ] T016 Confirm `scripts/go-statements.allow` is empty (or holds only the recorded exemption); update `docs/progress.md`, `docs/development.md` and this list with the final evidence.
