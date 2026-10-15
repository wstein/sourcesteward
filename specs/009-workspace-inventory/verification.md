# Spec 009 verification

## Candidate source and independent review

Native implementation source `1639e30bb63aada4b88a056b5f7763d3e9fdf9a0` is independently
source/safety reviewed with no remaining actionable findings. The reviewer independently
reran actual CLI reproductions and `go test ./internal/inspection -run Inventory -count=1`
(16.549s), plus whitespace checks. Shared-model/CLI/schema review separately cleared
`b24ac16`, including a 100,000-character escaped PATH error reduced from 1,200,792 bytes
to a bounded 760-byte structured error. Original inspect/version schemas are unchanged.

Pre-implementation analysis covered all 15 FRs and four SCs against 31 tasks. See
[analysis](analysis.md). Tests failed before their corresponding behavior was implemented;
focused Conventional Commits retain setup, regressions, implementations and fixes.

## Local native/source evidence

At source `b4e608293e42784e1607f04491c7dea81c18d9ab`:

- Full `scripts/smoke-inventory.py` passed 36/36 real CLI cases in both pinned local lanes:
  Git 2.46.0 + jj 0.45.1, and Git 2.56.0 + jj 0.46.0. Required macOS sandbox enforcement
  passed; every observation preserved fixture bytes/modes including indexes/op stores.
- M1=8, M2=4, M3=8, M4=16; full runs enforce the complete stable case-ID manifest.
- `make check` with mandatory jj passed: Go tests/vet/module verification, 37 Python tests,
  12 theme tests, 122 authored document checks, architecture and whitespace.
- `go test -race ./...` with mandatory current jj passed for all packages.

The final three-line broader-untracked summary qualification at `1639e30` passed its
focused regression suite (2.903s) and final independent review. This distinction preserves
exact-revision evidence; earlier broad checks are not claimed at a later revision.

Commands selected pinned native binaries through PATH, the existing pinned Hugo through
`HUGO=/private/tmp/sourcesteward-hugo-docs/.tools/hugo`, cached Go dependencies through
`GOCACHE=/private/tmp/gitsteward-go-cache GOMODCACHE=/private/tmp/gitsteward-go-mod`, and
`STW_REQUIRE_JJ=1` for mandatory real jj cases. Native fixture operations ran only in
private temporary repositories; outer execution permission did not remove required sandboxing.

## Reviewed safety fixes

- Queries do not become false/failed from unrelated missing workspaces or file scopes.
- Ignored traversal is opt-in; literal selected paths avoid unrelated FIFO/symlink reads.
- No Git worktree content hashing invokes filters; confined Go raw reads are bounded.
- Staged and stored gitlink deltas are retained; they cannot disappear as false clean.
- Unsafe/unreadable/oversized disk observations are unknown, never clean.
- Native conflicted jj bookmark target commit/change IDs and null normal targets survive.
- Cancellation/error paths and encoding/record limits apply to every result envelope.
- Filtered workspace-wide summaries remain unknown when broader state was not observed.

## Remaining gates

Populated real-output schema fixtures at `544dd08` passed in both pinned local native lanes,
including committed Git refs/linked worktrees, native jj bookmark targets and the bounded
oversized-input error envelope. Hosted Linux minimum/current conformance and PR handoff
remain pending at this record's creation. No live schema/Pages deployment, release, merge or mutation
capability is claimed. The implementation status is `implemented`, not globally verified.
