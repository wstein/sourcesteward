# Specification: concurrency discipline

**Created**: 2026-10-22 | **Status**: proposed (owner decision 2026-10-22: write it; direction: a functional core with an imperative shell at the
edge, goroutines in one package, `go` statements forbidden elsewhere by the architecture check). Documentation only: no Go code, script
or allowlist is changed by this specification. It builds on the `internal/issues` pool and its `testing/synctest` test, and it follows
Spec 039 D10 (`-race` runs nightly and weekly, not on pull requests).

## Problem

`go` statements are spread over seven packages (`cmd/stw`, `inspection`, `issues`, `pulls`, `stwsync`, `release`, `workspace`), 14 in non-test code (measured 2026-10-22 on `origin/main` with
`git grep -nE '^\s*go (func|[a-zA-Z_.]+\()' -- '*.go' ':!*_test.go'`):

| File | `go` statements | Kind |
| --- | --- | --- |
| `cmd/stw/main.go` | 1 | signal watcher: `go func() { <-ctx.Done(); stop() }()` |
| `internal/inspection/repos.go` | 1 | bounded worker pool over an index channel (`runWorkerPool`, called by `inspectIndices`, about 25 lines) |
| `internal/issues/cycle.go` | 1 | `pool(n, width, fn)`, semaphore and WaitGroup (13 lines, `cycle.go:321-334`) |
| `internal/pulls/cycle.go` | 1 | a second copy of the same `pool` (13 lines, `cycle.go:302-314`), used three times |
| `internal/stwsync/status.go` | 2 | one goroutine per repository and one per linked worktree; the number of goroutines is unbounded, but every Git call goes through the run-wide `statusRun.g` slots (`gitConcurrency` = 16), so Git processes are already bounded |
| `internal/release/build.go` | 2 | stdout and stderr copy goroutines of a child process |
| `internal/workspace/native_stream.go` | 2 | stderr drain and finish watcher of a streamed Git process |
| `internal/workspace/native_ref_transaction.go` | 3 | stderr drain, session wait and commit watcher of a `git update-ref --stdin` session |
| `internal/workspace/tidy_v3_integration_patchid.go` | 1 | two `patch-id` readers of a pipe |

`internal/statusremote` and `internal/hosting` start no goroutine themselves (they use `sync.Mutex` for shared counters, and
`statusremote` reaches the pools through `issues`, `pulls` and `stwsync`). Test files use `go` freely (21 files); they are out of this rule.

Consequences, each observed in the code above and not hypothetical:

1. **Three copies of one pool** (`inspection`, `issues`, `pulls`) with different edge behaviour: only `inspection` bounds by workers, the
   other two spawn n goroutines blocked on a semaphore; none stops launching when the context ends; in `issues`, `pulls` and `stwsync` a
   panic in the worker function crashes the process from an unrecoverable goroutine (`inspection` recovers inside its callback).
2. **Unbounded goroutine count** in `stwsync` (a goroutine per repository and per linked worktree). This is small: the Git processes are
   bounded by the run-wide slots, so the cost is idle goroutines, not load. The slots remain **the** bound on Git processes after the
   migration (WP-3); `fanout.Do` widths there only bound goroutines, so a nested `Do` (worktrees inside repositories) cannot raise the
   number of Git processes above `gitConcurrency`.
3. **No rule keeps it from spreading.** Spec 039 moved `-race` off pull requests, so a data race introduced by a new goroutine is
   found the next night, not on the pull request. The cheapest defence is fewer places where one can be introduced.
4. **Concurrency and logic are interleaved.** The decisions (which repositories, which row, which verdict) sit in the same functions
   as the goroutines, so testing a decision needs the pool and testing the pool needs the decision.

## Goals

- **G1 Functional core, imperative shell.** Decisions are pure functions of values (rows in, rows out, no clock, no I/O, no goroutine).
  Effects (Git, HTTP, files, process) are done at the edge by a small number of reviewed entry points. Concurrency is an effect, so it
  lives at the edge too.
- **G2 One concurrency package.** `internal/fanout` is the only non-test code that contains a `go` statement.
- **G3 Enforced.** `scripts/check-architecture.py` (run by `make check`) fails on a `go` statement outside `internal/fanout`, except the
  shrinking allowlist of current violations, which reaches zero, or zero plus the one named exemption of Decision 1.
- **G4 Deterministic tests.** Concurrency is tested with `testing/synctest`, no `time.Sleep`, no wall-clock waits, and no export that
  exists only for tests.
- **G5 Simple enough for nightly race.** Because `-race` is not a pull request gate, the shape must be verifiable without it: results
  are written by index into a preallocated slice, never by completion order and never through shared maps.

## Non-goals

- Making anything faster. Widths stay what they are today (4 for issues, `c.o.Parallel` for pulls, `gitConcurrency` for status). The
  pools already run in parallel; this changes where the code lives and what is guaranteed, not the throughput.
- Removing `sync.Mutex`, `sync.Once` or channels from non-pool code (rate-limit counters in `hosting`, `watchcmd`, `issues`, `pulls`; the
  schema `Once`; the proof budget in `workspace`). They protect state, they do not start work. Only `sync.WaitGroup` and
  `golang.org/x/sync/errgroup` are added to the forbidden imports outside `internal/fanout`, because they exist only to join goroutines.
- Replacing the `context` deadline model or the hosting transport.
- Banning `time.Sleep` in the whole repository. The rule of G4 applies to the test files of every package that a work package has
  migrated, and is extended package by package.

## The package `internal/fanout`

Imports: the standard library only. Imported by the packages that need a pool. Exported surface (the whole of it: `Do`, `Map`, `Both`, `Start`, `Session`, `Completion`; nothing else, nothing
for tests):

```go
// Do runs fn(ctx, 0..n-1) with at most width calls at the same time and returns when every started call has returned.
// Items that were not started because ctx ended are not run; Do then returns ctx.Err(). If every item had started
// before ctx ended, Do returns nil: a late cancellation is not an error of this call. A panic in fn is recovered
// in its worker, the remaining items are cancelled, and the first panic (by index) is re-raised in the caller
// after all workers have stopped. width < 1 is 1; width > n is n.
func Do(ctx context.Context, n, width int, fn func(ctx context.Context, i int)) error

// Map is Do over a slice with the result of item i at index i: the order of the output is the order of the input,
// whatever the completion order.
func Map[In, Out any](ctx context.Context, in []In, width int, fn func(ctx context.Context, i int, v In) Out) ([]Out, error)

// Both runs a and b concurrently and returns both errors (a first): the join of two pumps when the caller has nothing
// to do meanwhile (the two patch-id readers).
func Both(ctx context.Context, a, b func(ctx context.Context) error) (errA, errB error)

// Start runs the jobs concurrently while the caller keeps its own goroutine (the child-process supervision loops of
// build.go and native_stream.go parse, tick or kill while pumps run). Jobs receive the session context, derived from ctx.
func Start(ctx context.Context, jobs ...func(ctx context.Context) error) *Session

// Completed delivers each finished job (index and error) on a channel buffered to len(jobs); it never blocks a job.
func (s *Session) Completed() <-chan Completion
// Cancel cancels the session context. Close cancels, waits for every job and returns the errors by job index; it is
// idempotent, and the caller must call it (defer) on every path, which is the no-leak guarantee. A panic in a job is recovered
// in its goroutine, cancels the session context and is re-raised (first by index, same value) from Close in the caller; the
// panicking job is still delivered once on Completed with a nil error.
func (s *Session) Cancel()
func (s *Session) Close() []error

type Completion struct { Index int; Err error }
```

Rules the implementation must satisfy for `Do` (each is a test, see the plan); `Session` must satisfy rule 3 (no leak after `Close`, also after a job panic), rule 4 and the panic behaviour documented on `Close` (rule 7 applied to `Close`):

1. **Bounded.** At most `width` workers exist at any time, not n goroutines blocked on a semaphore. Workers pull the next index from a
   shared counter; there is no unbounded channel.
2. **Deterministic result order.** `Map` returns results by input index. `Do` callers write `out[i]` only; the contract forbids a shared
   append. Cancellation never reorders: skipped items keep the zero value, and the caller learns of it from `ctx.Err()`.
3. **No leak.** When `Do` returns no goroutine started by it is alive, on every path (normal, cancelled before start, cancelled during,
   panic). `fn` receives a context that is cancelled when the first panic is recorded, so a cooperative `fn` stops.
4. **No new deadline.** `fanout` adds no timeout of its own. Deadlines stay with the caller's context (`hosting` and `inspection`
   per-call bounds are unchanged).
5. **Late cancellation is not an error.** `Do` returns `ctx.Err()` only when at least one item was skipped for it.
6. **No wait on a dead context.** If `n > 0` and `ctx` is already done, nothing is started and `Do` returns `ctx.Err()` without a goroutine. If `n == 0`, no item exists to skip, so `Do` returns nil even when `ctx` is done (rule 5 governs; one test row for each).
7. **Panic is the caller's.** A recovered panic is re-raised unchanged (same value) in the calling goroutine so the existing top-level
   handling applies; it is never swallowed and never logged by `fanout`.

## The architecture rule

`scripts/check-architecture.py` gains `run_go_statement_check()`, which follows the existing `run_release_ast_check` pattern: it runs a
Go test, `go test ./internal/architecture -run ^TestGoStatementsConfined -count=1`. The test file `internal/architecture/gostmt_test.go`
(a directory with only a test file, so no export exists for tests) walks the module with `go/parser` and counts `*ast.GoStmt` in
non-test files, so comments, strings, raw strings and generated or aliased forms cannot fool it, then applies a pure function
`checkGoStatements(counts, allow)` to the counts and `scripts/go-statements.allow`. The test reads its root from the environment
variable `STW_ARCH_ROOT` (default: the module root) so the Python tests can point it at a temporary tree. All packages including `cmd/`.

Allowlist grammar (`scripts/go-statements.allow`, exact; blank lines and `#` comment lines are ignored, everything else must match
one of these, or the test fails on the line):

```
done: <N> <N> ...                      exactly one header line, first; the work packages that have landed (may be empty: "done:")
<path> <count> wp <N>                  transitional: <count> go statements remain in <path>, removed by work package <N>
exempt <path> <count> <justification>  the one named exemption; justification is free text of at least 20 characters
```

Lines are sorted by path, paths are module-relative with `/`, `count` is a positive integer. The initial file is `done:` followed by the
14 statements of the table as nine `wp` lines (`cmd/stw/main.go 1 wp 4`, ..., `internal/workspace/native_ref_transaction.go 3 wp 6`).

The check fails when:

1. a file outside `internal/fanout/` has more go statements than its entry (a file without an entry has 0: a stray `go` fails);
2. an entry is **stale**, that is larger than the count found (the list may only shrink, so a fixed violation must remove or lower its
   entry in the same change; the check demands it);
3. an entry names a file under `internal/fanout/`, a file that no longer exists, or a path twice;
4. the grammar or the closed set is violated: a line that matches none of the three forms, no or two `done:` headers, a `wp N` line
   with N in the `done:` header (each work package adds its number to `done:` in the change that removes its lines, so a leftover line
   of a landed package fails), or an `exempt` line that is not for `internal/workspace/native_ref_transaction.go`, or a second
   `exempt` line (Decision 1);
5. a non-test file outside `internal/fanout/` imports `golang.org/x/sync/errgroup`, or uses `sync.WaitGroup` (a file with an entry is
   covered until its work package lands).

Adding or raising a line is a change to a reviewed file and needs the owner's decision stated in the pull request; the plan starts the
file with the 14 statements and each work package deletes its lines.

### Mutation proof

The pure function `checkGoStatements` is table-tested in `internal/architecture/gostmt_test.go` (one row per check rule above, each
killing a one-line mutation), and `scripts/test_architecture.py` proves the runner can fail (same style as its import-boundary tests):

- a count with one `go` in a new file (`internal/foo/x.go`) fails; the same count inside `internal/fanout/` passes;
- a stale entry fails; removing a still-needed entry fails; an entry in `internal/fanout/`, for a missing file or twice fails; a
  malformed line, a missing or doubled header, a `wp N` line with N in `done:`, two `exempt` lines and an `exempt` line for another path fail;
- end to end, the Python test writes a temporary tree (a stray `go func() {}()` in a non-test file next to a clean sibling), sets
  `STW_ARCH_ROOT`, runs the Go test and asserts the offending path is reported and the clean one is not; the same `go` in a `_test.go`
  file, in a comment and in a raw string literal is ignored (kills a regex implementation); an `errgroup` or `sync.WaitGroup` import fails;
- the real tree passes with the committed allowlist, and dropping one line from a copy of it fails.

## Testing with `testing/synctest`

`internal/issues/concurrency_test.go` is the model: `synctest.Test`, a gate channel to hold calls open, `synctest.Wait()` to reach a
quiescent point, assertions on the peak in-flight count, no sleep. Rules:

- `fanout` tests cover the seven rules above: peak concurrency never exceeds width (a hold gate and a counter), results by index with
  completion order reversed by the gates, cancellation before start / during / after, a panic in item k re-raised after the others
  stopped, and the leak rule (`synctest.Test` itself fails if a goroutine is still blocked when the bubble ends, which is the leak
  detector; no `runtime.NumGoroutine`).
- Callers keep their own tests but only assert **their** decision with a fake edge; pure decision functions are tested without a bubble.
- No test uses `time.Sleep` or a real timer for synchronisation in the packages a work package has migrated; deadlines use the fake
  clock inside the bubble.
- No export, build tag or variable exists only so that a test can reach it. A test that needs a seam uses an existing interface of the
  package or an internal test in the same package.
- `go test -race` is not required for these tests to be meaningful (rule 2 of G5), but they pass under it and the nightly run covers
  them.

## Migration

Work packages, in order (details and done-when in [plan.md](plan.md)): WP-0 `internal/fanout`, the check and the full allowlist;
WP-1 `inspection`; WP-2 `issues` and `pulls` (the two copies of `pool`, one WP so that both lose their copy together); WP-3 `stwsync`
(the run-wide Git slots stay the bound); WP-4 `cmd/stw/main.go` (no goroutine needed: `context.AfterFunc`); WP-5 process plumbing
(`release/build.go`, `workspace` streams and patch-id, on `Start` and `Both`); WP-6 the long-lived session in `native_ref_transaction.go`, which is
conditional (see Decisions).

Behaviour is preserved by the existing tests and by new characterisation tests written first in each package (TDD): the output for a
fixed fake edge must be byte identical before and after, including the order of rows and diagnostics.

## Cost and benefit (honest)

| | |
| --- | --- |
| Removed | two duplicate pools (13 lines each) and one hand-rolled worker loop (about 25 lines); the unbounded goroutine count of `stwsync` (Git processes were already bounded); the panic-crash behaviour of the `issues`, `pulls` and `stwsync` workers |
| Added | `internal/fanout` (about 150 lines with doc, `Session` included), one Go architecture test (about 80 lines), the allowlist, and a `go test` run in `make check` (about 1 s after compilation) |
| Not gained | speed, new features, or a proof of freedom from data races: shared state behind `sync.Mutex` outside the pools is untouched, and `-race` stays the nightly authority |
| Risk | a behaviour change in ordering or error precedence when a pool is replaced; mitigated by characterisation tests first, one package per pull request, and unchanged widths |
| Real value | one place to review for goroutine leaks and panics, a rule that keeps the count at one package, and cheap deterministic tests (the `issues` bubble test pattern copied, not reinvented). It is hygiene, not a user-visible feature; the owner can stop after WP-3 and keep most of it |

The process-plumbing packages (WP-5, WP-6) are where cost exceeds benefit most: they sit in the sandboxed, security-reviewed Git launch
path (Specs 029 and 031) whose stream tests are strict about reaping and byte limits. They are scheduled last and each is optional.

## Decisions (foreman, 2026-10-22; open to owner override)

These were settled by the foreman after the first draft and stay open to an owner override.

1. **No blanket exemption.** The end state allows at most ONE named exemption, `workspace/native_ref_transaction.go`, and only if the
   conditional WP-6 shows that `fanout` (`Session` is already in WP-0; if it still cannot express the transaction) cannot express its long-lived session. The exemption is an `exempt` line in
   `scripts/go-statements.allow` with a written justification; rule 4 of the Go test (`internal/architecture/gostmt_test.go`) fails on
   a second `exempt` line or one for another path. No other file may stay on the list: after the last work package every `wp` line is gone.
2. **Keep the import ban.** `sync.WaitGroup` and `golang.org/x/sync/errgroup` are forbidden outside `internal/fanout` (rule 5 of the check).
3. **Go AST check.** `go/parser` and `*ast.GoStmt`, run by `scripts/check-architecture.py` the way `run_release_ast_check` already runs
   a Go test (`internal/architecture`, no `scripts/gostmt` helper: the existing pattern is followed because nothing needs a separate
   binary; the environment root of the test serves the Python mutation tests). A regex is rejected: comments, strings, raw strings and
   generated code fool it.

## Dependencies and sequence

None of Specs 037 to 039 blocks this one. WP-1 to WP-3 touch `inspection`, `issues`, `pulls` and `stwsync`, which Specs 037 and 038 also
change; WP-2 and WP-3 should therefore start after those packages' open work packages are merged, or rebase onto them (the pool is
five lines at the call sites; a conflict is mechanical).
