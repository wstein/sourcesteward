# Specification: concurrency discipline

**Created**: 2026-10-22 | **Status**: proposed (owner decision 2026-10-22: write it; direction: a functional core with an imperative shell at the
edge, goroutines in one package, `go` statements forbidden elsewhere by the architecture check). Documentation only: no Go code, script
or allowlist is changed by this specification. It builds on the `internal/issues` pool and its `testing/synctest` test, and it follows
Spec 039 D10 (`-race` runs nightly and weekly, not on pull requests).

## Problem

`go` statements are spread over six packages, 14 in non-test code (measured 2026-10-22 on `origin/main` with
`git grep -nE '^\s*go (func|[a-zA-Z_.]+\()' -- '*.go' ':!*_test.go'`):

| File | `go` statements | Kind |
| --- | --- | --- |
| `cmd/stw/main.go` | 1 | signal watcher: `go func() { <-ctx.Done(); stop() }()` |
| `internal/inspection/repos.go` | 1 | bounded worker pool over an index channel (`inspectIndices`) |
| `internal/issues/cycle.go` | 1 | `pool(n, width, fn)`, semaphore and WaitGroup |
| `internal/pulls/cycle.go` | 1 | a second copy of the same `pool`, used three times |
| `internal/stwsync/status.go` | 2 | one goroutine per repository (limit only inside `s.repo` by a slot channel) and one per linked worktree (no limit) |
| `internal/release/build.go` | 2 | stdout and stderr copy goroutines of a child process |
| `internal/workspace/native_stream.go` | 2 | stderr drain and finish watcher of a streamed Git process |
| `internal/workspace/native_ref_transaction.go` | 3 | stderr drain, session wait and commit watcher of a `git update-ref --stdin` session |
| `internal/workspace/tidy_v3_integration_patchid.go` | 1 | two `patch-id` readers of a pipe |

`internal/statusremote` and `internal/hosting` start no goroutine themselves (they use `sync.Mutex` for shared counters, and
`statusremote` reaches the pools through `issues`, `pulls` and `stwsync`). Test files use `go` freely (21 files); they are out of this rule.

Consequences, each observed in the code above and not hypothetical:

1. **Three copies of one pool** (`inspection`, `issues`, `pulls`) with different edge behaviour: only `inspection` bounds by workers, the
   other two spawn n goroutines blocked on a semaphore; none stops launching when the context ends; none turns a panic in `fn` into
   anything but a process crash from an unrecoverable goroutine.
2. **Unbounded fan-out** in `stwsync` (a goroutine per linked worktree, each running `git status`).
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

Imports: the standard library only. Imported by the packages that need a pool. Exported surface (the whole of it; nothing else, nothing
for tests):

```go
// Do runs fn(ctx, 0..n-1) with at most width calls at the same time and returns when every started call has returned.
// Items that were not started because ctx ended are not run; Do then returns ctx.Err(). A panic in fn is recovered
// in its worker, the remaining items are cancelled, and the first panic (by index) is re-raised in the caller
// after all workers have stopped. width < 1 is 1; width > n is n.
func Do(ctx context.Context, n, width int, fn func(ctx context.Context, i int)) error

// Map is Do over a slice with the result of item i at index i: the order of the output is the order of the input,
// whatever the completion order.
func Map[In, Out any](ctx context.Context, in []In, width int, fn func(ctx context.Context, i int, v In) Out) ([]Out, error)

// Both runs a and b concurrently and returns both errors (a first); it is the join of two I/O pumps
// (stdout and stderr of a child process) and exists so process plumbing needs no `go` statement.
func Both(ctx context.Context, a, b func(ctx context.Context) error) (errA, errB error)
```

Rules the implementation must satisfy (each is a test, see the plan):

1. **Bounded.** At most `width` workers exist at any time, not n goroutines blocked on a semaphore. Workers pull the next index from a
   shared counter; there is no unbounded channel.
2. **Deterministic result order.** `Map` returns results by input index. `Do` callers write `out[i]` only; the contract forbids a shared
   append. Cancellation never reorders: skipped items keep the zero value, and the caller learns of it from `ctx.Err()`.
3. **No leak.** When `Do` returns no goroutine started by it is alive, on every path (normal, cancelled before start, cancelled during,
   panic). `fn` receives a context that is cancelled when the first panic is recorded, so a cooperative `fn` stops.
4. **No new deadline.** `fanout` adds no timeout of its own. Deadlines stay with the caller's context (`hosting` and `inspection`
   per-call bounds are unchanged).
5. **No wait on a dead context.** If `ctx` is already done, nothing is started and `Do` returns `ctx.Err()` without a goroutine.
6. **Panic is the caller's.** A recovered panic is re-raised unchanged (same value) in the calling goroutine so the existing top-level
   handling applies; it is never swallowed and never logged by `fanout`.

## The architecture rule

`scripts/check-architecture.py` gains `validate_go_statements(counts, allowlist)`, a pure function from `{file: number of go
statements}` and an allowlist `{file: allowed number}` to an error or nothing, and a runner that obtains `counts` from a small Go program
(`scripts/gostmt/main.go`, run by `go run`, built on `go/parser` and `*ast.GoStmt`, so comments, strings and raw strings cannot fool it
and an aliased or generated form cannot hide). Non-test files only (`_test.go` excluded), all packages of the module including
`cmd/`. The check fails when:

1. a file outside `internal/fanout/` has more go statements than its allowlist entry (a new file has entry 0: a stray `go` fails);
2. an allowlist entry is **stale**, that is larger than the count found (the list may only shrink, so a fixed violation must remove its
   entry in the same change; the check demands it);
3. the allowlist names a file under `internal/fanout/` or a file that no longer exists;
4. a non-test file outside `internal/fanout/` imports `golang.org/x/sync/errgroup`, or uses `sync.WaitGroup` (after the migration of
   its package; before that, the allowlist entry covers the file).

The allowlist is `scripts/go-statements.allow`, one `path count` pair per line, sorted, with a comment per line naming the Spec 040 work
package that removes it. Adding a line is a change to a reviewed file and needs the owner's decision stated in the pull request; the
plan starts it with the 14 statements of the table above and each work package deletes its lines.

### Mutation proof

`scripts/test_architecture.py` proves the check can fail (the same style as its existing import-boundary tests):

- a synthetic count with one `go` in a new file (`internal/foo/x.go`) makes `validate_go_statements` raise; the same count inside
  `internal/fanout/` passes;
- an allowlist entry larger than the count (stale) raises; removing a still-needed entry raises;
- an end-to-end case writes a temporary Go module with a stray `go func() {}()` in a non-test file and a clean sibling, runs
  `scripts/gostmt` over it, and asserts the offending path is reported and the clean one is not; the same `go` in a `_test.go` file is
  ignored; a `go` that appears only in a comment and in a raw string literal is ignored (kills a regex implementation);
- the real tree passes with the committed allowlist, and dropping one line from a copy of it fails.

## Testing with `testing/synctest`

`internal/issues/concurrency_test.go` is the model: `synctest.Test`, a gate channel to hold calls open, `synctest.Wait()` to reach a
quiescent point, assertions on the peak in-flight count, no sleep. Rules:

- `fanout` tests cover the six rules above: peak concurrency never exceeds width (a hold gate and a counter), results by index with
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
(the unbounded worktree fan-out gets a width); WP-4 `cmd/stw/main.go` (no goroutine needed: `context.AfterFunc`); WP-5 process plumbing
(`release/build.go`, `workspace` streams and patch-id); WP-6 the long-lived session in `native_ref_transaction.go`, which is
conditional (see Decisions).

Behaviour is preserved by the existing tests and by new characterisation tests written first in each package (TDD): the output for a
fixed fake edge must be byte identical before and after, including the order of rows and diagnostics.

## Cost and benefit (honest)

| | |
| --- | --- |
| Removed | two duplicate pools (about 30 lines each) and one hand-rolled worker loop; the only unbounded fan-out (`stwsync` worktrees); panic-crash behaviour of pool workers |
| Added | `internal/fanout` (about 120 lines with doc), one Go helper (about 50 lines), the allowlist, and a count check in `make check` that runs `go run` once (about 1 s after compilation) |
| Not gained | speed, new features, or a proof of freedom from data races: shared state behind `sync.Mutex` outside the pools is untouched, and `-race` stays the nightly authority |
| Risk | a behaviour change in ordering or error precedence when a pool is replaced; mitigated by characterisation tests first, one package per pull request, and unchanged widths |
| Real value | one place to review for goroutine leaks and panics, a rule that keeps the count at one package, and cheap deterministic tests (the `issues` bubble test pattern copied, not reinvented). It is hygiene, not a user-visible feature; the owner can stop after WP-3 and keep most of it |

The process-plumbing packages (WP-5, WP-6) are where cost exceeds benefit most: they sit in the sandboxed, security-reviewed Git launch
path (Specs 029 and 031) whose stream tests are strict about reaping and byte limits. They are scheduled last and each is optional.

## Decisions (foreman, 2026-10-22; open to owner override)

These were settled by the foreman after the first draft and stay open to an owner override.

1. **No blanket exemption.** The end state allows at most ONE named exemption, `workspace/native_ref_transaction.go`, and only if the
   conditional WP-6 shows that `fanout` (with a `Session` primitive) cannot express its long-lived session. The exemption is a line in
   `scripts/go-statements.allow` with a written justification, and a test in `scripts/test_architecture.py` fails when a second
   exemption (a second path with a nonzero count after WP-5) appears. No other file may stay on the list.
2. **Keep the import ban.** `sync.WaitGroup` and `golang.org/x/sync/errgroup` are forbidden outside `internal/fanout` (rule 4 of the check).
3. **Go AST helper.** `scripts/gostmt` (`go/parser`, `*ast.GoStmt`) invoked by `scripts/check-architecture.py`. A regex is rejected:
   comments, strings, raw strings and generated code fool it.

## Dependencies and sequence

None of Specs 037 to 039 blocks this one. WP-1 to WP-3 touch `inspection`, `issues`, `pulls` and `stwsync`, which Specs 037 and 038 also
change; WP-2 and WP-3 should therefore start after those packages' open work packages are merged, or rebase onto them (the pool is
five lines at the call sites; a conflict is mechanical).
