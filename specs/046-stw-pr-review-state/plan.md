# Plan: review state, owner queue and stack view (Spec 046)

Test-first throughout (a failing test before behavior), fake forge, no network. Three stacked pull requests, each ends with its tests green
and `make check` clean. Seams: a new pure package `internal/reviewstate` (grammar, state, hints; no I/O), `internal/hosting` (two shapes,
guard, decoders, readers), `internal/pulls` (review pass in the cycle, filters, summary), `internal/pullscmd` (flags, document, human),
`internal/statusremote` and `internal/statuscmd` (counts in the `pulls` source).

| PR | Base | Scope | Done when |
| --- | --- | --- | --- |
| 1 | `main` | this spec; `reviewstate` parser, state machine and hints; hosting shapes, guard rows, decoders; review pass in `pulls.Run`; `--no-review`, `--review`, `--hint`; JSON and human; status counts; schema members and digests; docs | every state, hint and rule has a test and a surviving-mutation check; `go test ./...` as in `docs/development.md` |
| 2 | PR 1 | `--mergeable` owner list: order, reason text, `then`, footer, document member | list tests incl. stack order and reason facts |
| 3 | PR 2 | `--stack` forest: tree build, rendering, footer, document member | tree tests incl. orphan base and fork exclusion |

## Mutation targets (a test must kill each)

| Rule | Mutation |
| --- | --- |
| grammar | accept upper-case hex; accept a missing `pr=`; accept a leading space; accept `patch-id` of 20 hex; ignore `\r` handling; accept `pr=` of another pull request |
| unparsed | drop the word `urteil`; match inside a word (`clearly`); treat any `review` word as unparsed |
| `clear` / `findings` | compare the head by prefix; take the first instead of the latest comment; ignore the committer date of H |
| `clear-carried` | skip the patch-id equality; skip `from` equality; allow a carry-over created before its CLEAR; chain of 9; carry a FINDINGS |
| `clear-stale` | state `none` for an older CLEAR; count a CLEAR for a newer head |
| `unparsed` | let an older unparsed comment override a decisive one; ignore a newer one |
| hints | `clear-not-ready` for a non-draft; `no-final-review` for `findings`; `base-not-default` with a case-insensitive match; `parent-merged-retarget` for an unmerged closed pull request or a different head repository; `ready-mergeable` without the base rule, with a Draft, with `clear-stale` |
| reads | read comments beyond page 6; skip the commit read; request `closed` without the fixed query; admit the shapes in the `issues` or `watch` guard |
| filters | OR across `--review` and `--hint`; filter before the reads; match a row with `review` null |
| status | counts differ from the `stw pulls` document of the same fixture |
