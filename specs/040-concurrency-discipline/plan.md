# Plan: concurrency discipline (Spec 040)

Test-first throughout (a failing test before behaviour; a characterisation test of the current output before any pool is replaced).
Each work package is one pull request for one middle-tier author, stacked linearly, ends with its tests green and `make check` clean,
and **deletes its lines from `scripts/go-statements.allow` in the same change** (the check demands it). Code pull requests get an
independent review of the exact head. Seams: new `internal/fanout`, `scripts/check-architecture.py`, `scripts/test_architecture.py`,
new `internal/architecture/gostmt_test.go`, new `scripts/go-statements.allow`. No build, release or push of the default branch is part of any WP.

Order: **WP-0**, then WP-1, WP-2, WP-3, WP-4 (independent of each other after WP-0, any order, but one pull request each), then WP-5,
then WP-6 (the Session primitive first; the exemption only if that fails, Decision 1).

| WP | Scope | Done when |
| --- | --- | --- |
| 0 fanout and rule | `internal/fanout` (`Do`, `Map`, `Both`, `Start`/`Session`) with synctest tests for bounded width, late cancellation returning nil, index order under reversed completion, cancellation (before, during, after), panic re-raise, leak; `internal/architecture/gostmt_test.go` (AST counter and `checkGoStatements`) run by `run_go_statement_check()` in `check-architecture.py`; `scripts/go-statements.allow` with the 14 current statements, each line `wp N`, plus the `done:` header; mutation tests (stray `go`, stale entry, `wp N` in `done`, second `exempt`, comment and raw-string `go`, `_test.go`, errgroup, WaitGroup) | `make check` passes with the allowlist at 14; every mutation target is killed; `fanout` imports only the standard library; coverage of `fanout` 100% of statements |
| 1 inspection | `inspectIndices` in `internal/inspection/repos.go` becomes `fanout.Do`; characterisation test of `inspectExplicitParallel` output over a fake `inspectFn` (rows, order, timeout mapping) first | allowlist line removed; output identical; `reposBounds` widths unchanged |
| 2 issues and pulls | both `pool` functions deleted; call sites (`issues/cycle.go:97`, `pulls/cycle.go:160, 213, 279`) use `fanout.Do` with the same widths; the existing `issues` synctest test passes untouched and an equivalent test is added for `pulls`; rate-limit stop behaviour characterised first | two allowlist lines removed; the peak in-flight tests (4, `Parallel`) pass; no change to result documents |
| 3 stwsync | both fan-outs in `status.go` become `fanout.Do`; the run-wide `slots` in `statusRun.g` stay and are THE bound on Git processes (widths of the repository and worktree `Do` only bound goroutines; nested `Do` must not raise the process count above `gitConcurrency`); no output change | two lines removed; the peak of concurrent Git calls over many repositories and worktrees stays at most `gitConcurrency`; the goroutine count is bounded; unchanged JSON for the existing fixtures |
| 4 main | `cmd/stw/main.go` drops the goroutine: `context.AfterFunc(ctx, stop)` (or `signal.NotifyContext` semantics as today) with a test that a signal cancels the context and releases the handler | one line removed; behaviour on SIGINT and SIGTERM unchanged (existing cli tests) |
| 5 process plumbing | `release/build.go` (two pumps, caller keeps its ticker and kill loop) and `workspace/native_stream.go` (stderr drain and ctx watcher as jobs, caller parses stdout) use `fanout.Start`; `tidy_v3_integration_patchid.go` (two readers, caller waits) uses `fanout.Both`; the strict stream tests (byte limits, blocked pipes, cancellation, reaping) pass untouched and run under `-race` locally once | four lines removed; no change to the sandbox launch policy or the byte caps; independent security-minded review of the exact head |
| 6 ref transaction (conditional) | `native_ref_transaction.go` session: either a `fanout.Start` (already in WP-0), or, only if it cannot express it, the single named exemption with a written justification and a test that fails on a second exemption (Decision 1) | allowlist reaches zero or exactly one named exemption |

## Test plan and mutation targets

| Area | Tests | Mutation targets |
| --- | --- | --- |
| `fanout.Do` | peak in-flight never above width for n=0,1,width,3*width; width<1 and >n; cancel before start runs nothing and returns `ctx.Err()`; cancel mid-run starts no new item; panic by index k re-raised after the others stopped; leak (the bubble ends only if all goroutines exit) | spawn n goroutines instead of width; drop `wg.Wait` before return; do not stop launching after cancel; swallow the panic; re-raise the highest index instead of the lowest; use `time.After` |
| `fanout.Start`, `Map`, `Both` | `Start`: `Close` waits for all jobs and is idempotent, `Completed` delivers every job once without blocking, a caller loop can kill while jobs run, cancellation reaches jobs; late cancellation after all items started returns nil; | skip `wg.Wait` in `Close`; block a job on an unread `Completed`; |
| `Map`, `Both` | output by input index with gates releasing in reverse; `Both` returns both errors and waits for the slower one even when the faster fails | append in completion order; return after the first error |
| check | the cases listed in the specification "Mutation proof", plus the real tree against the committed allowlist | count with a regex; skip `cmd/`; accept a stale entry; allow `internal/fanout` entries; count `_test.go` |
| each caller | characterisation first, then the migrated pool: identical document for a fixed fake edge, width honoured | width off by one; result slot shared between items |

Verification evidence (commands, SHAs, review) goes to `docs/progress.md` at handoff; nothing is marked verified without it. `-race` is
run once locally by the author of WP-0 and WP-5 and then by the nightly job (Spec 039), not by a reviewer.
