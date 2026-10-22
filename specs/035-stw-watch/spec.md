# Specification: `stw watch`, a read-only pull request watcher

**Created**: 2026-10-22 | **Status**: planned (spec-light: this page, [contracts/watch-events.md](contracts/watch-events.md) and
[plan.md](plan.md) are the whole design; no code yet). Draft for independent review.

`stw watch` observes the open pull requests of one or more GitHub repositories, compares each poll with the previous one and
prints **only the transitions** as JSON Lines events. It never writes to the forge. It replaces the foreman's ad-hoc `watch2.sh`
(a bash and `gh` script used in the Anchorage workspace) with a reusable command for any repository set, and it retires the
interim `scripts/ci-wait.sh` once parity is shown (WP-E). A consumer (an agent, a shell loop, `jq`, a Monitor tool) reads the
stream and decides what to do; `stw watch` itself decides nothing about merging or readiness.

## Decisions (owner, 2026-10-22)

1. **Read-only.** Reads PR and CI state only, through the Spec 022 bounded go-gh transport (the hosting layer behind Specs 025
   and 030). It never marks a PR Ready, reruns a check, merges, comments, labels or edits. Its only writes are its own state
   directory (mode 0700, atomic files, an append-only journal), like other `stw` state.
2. **Transitions only.** Output is the difference between two observations, never a table of current state.
3. **First run is a silent baseline.** `--baseline` forces one.
4. **Stack aware.** After `merged`, `restack-needed` events name the open PRs that must be restacked and the exact command.
5. **Liveness is part of the contract**: heartbeat file, `--status`, `watch-stalled`.

## Why restack-needed exists

The owner rebase-merges (see the workspace `AGENTS.md`, section "Pull requests and history"; Spec 015 explains the mechanics).
GitHub's rebase merge **rewrites every commit** of the parent PR: the new commits on the base branch have new SHAs. A child
branch stacked on the parent still carries the *old* parent commits, so after the parent merges the child is "ahead" by commits
that are already on the base under other identities, and a plain `git rebase main` or a GitHub update replays them and
conflicts or duplicates. The correct move is `git rebase --onto <new parent> <old parent> <child>`, naming both SHAs. The watcher
knows both at the moment of the merge (the old parent is the merged PR's last observed head SHA, the new parent comes from the
merged PR record), and it is the only component that sees the merge as it happens. Emitting the exact argv saves the
coordinator from reconstructing it after the evidence has moved. The watcher only reports the command; it never runs it.

## CLI synopsis

```
stw watch [--repo OWNER/NAME]... [--workspace [PATH]] [--config FILE]
          [--interval DUR] [--state-dir DIR] [--host HOST]
          [--once | --stream | --until-event KIND[,KIND]... [--timeout DUR]]
          [--baseline] [--human] [--max-idle DUR] [--pending-timeout DUR]
stw watch --status [--state-dir DIR] [--json]
```

- Modes (mutually exclusive; default `--stream`): `--once` polls every repository once, prints its events and exits;
  `--stream` polls until SIGINT or SIGTERM; `--until-event` is `--stream` that exits 0 right after the poll in which an event of
  a listed kind was printed (all events of that poll are printed first). `--until-event` takes `--timeout` (default `590s`,
  chosen to fit a 600 s tool limit; `0` is refused); at expiry it exits 4 with no output of its own except a `watch-stalled`
  or `watch-error` event already due.
- Repositories come from the union of, in this order: `--repo` flags; `--workspace` (root and every initialized submodule whose
  remote is a GitHub URL, found exactly as `stw status` finds the workspace, from `PATH` or the current directory; the remote is
  `origin`, else the only remote; a submodule without a GitHub remote is skipped and reported once as `repos-changed`
  detail); `[repos]` of the config file. Duplicates are removed, order kept. No source given: the config file `.stw/watch.toml`
  of the current workspace if it exists, else usage error (exit 2). `--host` defaults to `github.com`; GitHub Enterprise hosts
  follow Spec 022 host rules, one host per run.
- `--interval` default `60s`, minimum `10s`. A jitter of up to 5 s is added per cycle.
- `--human`: one line per event (format below) instead of JSON Lines. State and journal always hold JSON.
- `--baseline`: treat the poll as the first run (silent, state replaced) even when state exists; combined with `--once` it is
  "refresh the baseline and exit".
- `--max-idle DUR` (default off): in `--stream`, exit 0 after that long without any event.
- `--pending-timeout DUR` (default `45m`, `0` disables): see `checks-pending-timeout`.
- Output goes to stdout, one event per line, flushed per line. Diagnostics (never tokens) go to stderr only as `watch-error`
  style lines; stdout carries events only.

### Config file `.stw/watch.toml`

Strict profile like `.stw/release.toml` (unknown keys, wrong types and duplicate keys are usage errors; `schema_version = 1`
required). Minimal by design:

```toml
schema_version = 1
repos = ["wstein/crewbook", "wstein/sourcesteward"]
interval = "60s"
[ignore]
authors = ["dependabot[bot]"]      # PRs by these logins produce no events at all
labels  = ["no-watch"]             # PRs carrying one of these labels produce no events
[shape]
stack_checks = ["pr-target"]       # check names whose failure on a non-default base is the stack-merge-shape
```

Flags override the file. `[shape]` is optional; its default patterns are in the event table.

## State and journal layout

State directory: `--state-dir`, else `$XDG_STATE_HOME/stw/watch/<profile>` (fallback `~/.local/state`), where `<profile>` is the
first 12 hex digits of SHA-256 over host and the sorted repository list. Created `0700`; files `0600`. Nothing else is written.

| File | Content |
| --- | --- |
| `lock` | advisory `flock`; a second watcher on the same directory exits 3 and prints the holder's pid |
| `state.json` | `schema_version`, `seq` (last event sequence), per repository `list_etag`, `last_ok`, and per open PR the last accepted observation plus any debounce candidate and per-PR `etag`s; replaced atomically (write temp, fsync, rename) |
| `journal.jsonl` | every emitted event, one object per line, append-only, fsynced per poll; the dedupe source |
| `heartbeat` | one line `RFC3339-time pid mode`, rewritten atomically after every poll cycle that completed (also when some repositories failed, see liveness) |

Order inside one poll cycle: compute events from (state, observation); append them to the journal; print them; replace
`state.json`. A crash between the journal append and the state replace makes the next run compute the same events again; an event
whose `id` is already in the journal is neither journaled nor printed again. A crash between journal append and print can lose one
printed line (the journal has it; `stw watch --once --replay-journal` is not provided in v1, consumers read `journal.jsonl`).
A crash after print and before the state replace can print an event twice only if the journal append failed, which is an error exit.
Truncated trailing journal lines are ignored and healed with a newline on the next append. The journal is never rotated or deleted by
`stw watch`; its size is the owner's to manage (about 300 bytes per event).

## Observation, debounce and polling

Per repository and cycle (all GET, through the Spec 022 transport, 30 s per request, 1 MiB body cap, no redirects):

1. `pulls?state=open&per_page=100&page=N` (pages up to 10, so 1000 open PRs per repository; more is `watch-error` `list-truncated`
   and the repository keeps working on the first 1000). Gives number, title, user, labels, draft, head SHA and ref, base ref.
2. `pulls/N` for each open PR (mergeable, mergeable_state; `null` mergeable is "not computed" and keeps the previous value).
3. `commits/SHA/check-runs` and `commits/SHA/statuses` at each open PR head (existing allowlisted paths, bounded pagination).
4. For a PR that left the open list: `pulls/N` once (state, merged, merge commit, base, head).
5. `rate_limit` once per cycle (free).

Conditional requests: every list and detail response keeps its `ETag`; the next request sends `If-None-Match`; a 304 is accepted as
"unchanged" and costs no rate limit. The conditional primitive is new in the transport (WP-B) and is available only to the watch
reader; the existing one-shot operations are untouched.

Debounce: volatile fields (`checks` class, `mergeable`) become a transition only when two consecutive cycles observe the same
new value; a flap back cancels the candidate silently. Stable fields (`head`, `draft`, `base`, open or gone) transition at once.
`merged` and `closed` are immediate. This is what `watch2.sh` did and it removed most check-run noise.

Check class (per PR head, over check-runs and combined statuses): `red` if any check-run `conclusion` is `failure`, `cancelled`,
`timed_out`, `action_required` or `startup_failure`, or any status context state is `failure` or `error`; else `pending` if any
run is not `completed` or any context is `pending`; else `green` if at least one check exists; else `none`. Skipped and neutral
runs count as green. `none` never produces `checks-green`.

### Rate limit, errors, offline

- After each cycle the `rate_limit` core `remaining`, `limit`, `reset` are read. `remaining < 10%` of the limit doubles the
  interval for the next cycle (cap 10 min); `remaining < 50` or any 403/429 with rate-limit evidence sleeps until `reset` plus
  5 s and emits one `watch-error` `rate-limited` (re-armed after a successful cycle). Conditional 304s keep a 100-PR, 6-repository
  watch far below the limit.
- A failed request (network, 5xx, 401, 403 without rate-limit evidence, malformed or oversize body) fails only its repository for
  that cycle; the last accepted state of that repository is kept untouched. Backoff for that repository: `interval`, 2x, 4x, ...
  capped at `min(10 min, 10 x interval)`, reset by one success. `watch-error` is emitted on the first failure and again only when
  the error `code` changes or after 10 consecutive failures (`failures` counts them). Offline: the same path; no crash, no busy loop.
  A recovery after any emitted `watch-error` emits `watch-recovered`.
- `watch-stalled`: emitted once per repository when no successful poll happened for `max(5 x interval, 10 min)`; re-armed by
  success. (A single repository stalling does not stop the others.)
- Authentication: only the existing gh token route of Spec 022. A missing token is exit 1 before any request (`hosting-api-unavailable`).
  Tokens, headers and response bodies are never printed or stored; error text is the bounded sanitized text of Spec 022.

## Event kinds

All events carry the common fields of the contract. `details` per kind (exact keys, no others). `head` fields are full 40-hex SHAs.

| Kind | Emitted when | `details` |
| --- | --- | --- |
| `new-pr` | an open PR appears that was not in the previous accepted list (never during a baseline) | `title`, `author`, `draft`, `base`, `head_ref`, `head` |
| `head-changed` | head SHA of a still-open PR differs | `old`, `new` |
| `checks-green` | debounced class becomes `green` for the current head | `head`, `draft`, `base`, `total` (check count) |
| `checks-red` | debounced class becomes `red` for the current head | `head`, `failing` (sorted check names, at most 20), `failing_total`, `shape`: `stack-merge-shape`, `real` or `unknown`, `base`, `draft` |
| `checks-pending-timeout` | class stays `pending` on one head longer than `--pending-timeout`; once per head | `head`, `pending` (names, at most 20), `since` |
| `mergeable-changed` | debounced `mergeable` flips, or `mergeable_state` enters or leaves `behind` | `mergeable` (`true`/`false`), `state` (GitHub `mergeable_state`), `conflicting`, `behind` (booleans), `base` |
| `ready-state-changed` | `draft` flips | `from`, `to` (`draft`/`ready`) |
| `base-changed` | the PR's base ref changed (retarget, also GitHub's automatic retarget after a parent merge) | `old`, `new` |
| `merged` | a PR that left the open list is `merged` | `merge_commit`, `base`, `head` (the merged head SHA), `head_ref`, `merged_at`, `method_hint` (`rebase-or-squash` when `merge_commit` is not a merge commit with 2 parents, `merge` otherwise, `unknown` when unreadable) |
| `closed` | a PR that left the open list is closed unmerged | `head`, `base` |
| `restack-needed` | follows a `merged` event, one per affected open PR (rules below) | `base`, `reason` (`child-of-merged-branch` or `contains-merged-commits`), `old_parent`, `new_parent`, `new_parent_source`, `branch`, `argv` |
| `repos-changed` | the resolved repository set differs from the one in state (including a skipped submodule) | `added`, `removed`, `skipped` (with reason) |
| `watch-error` | see Rate limit, errors, offline | `code`, `message` (bounded, sanitized), `failures`, `retry_in_seconds`, `repo` (or null) |
| `watch-recovered` | first success after an emitted `watch-error` or `watch-stalled` | `after_failures`, `repo` |
| `watch-stalled` | see above | `since`, `threshold_seconds`, `repo` |

`review-state-changed` is **reserved**: not emitted in v1 (the REST reviews are per-PR and per-review, so a faithful "approved,
changes requested" needs a separate read budget and a decision about stale reviews; see Open questions). Consumers must ignore
unknown kinds.

`shape` classification (no log download; check-run `name`, `output.title` and the first 4 KiB of `output.summary` only): the
failing set is `stack-merge-shape` when the PR base is **not** the repository default branch and every failing check either is
listed in `[shape] stack_checks` or its text matches one of the defaults `Unexpected synthetic merge parents` or `PR must target
repository main`; `real` when every failing check is a plain `failure` conclusion and the first rule does not apply; `unknown`
for anything else (cancelled, timed out, action required, startup failure, status context errors, no readable text). Infrastructure
flakes therefore show as `unknown` or `real`; the consumer decides what to do. This is a heuristic and the field says so in the docs.

### restack-needed rules

On a `merged` event of PR M (base B, head ref H, merged head SHA S, merge commit C), in the same cycle and before the next poll:

- Candidate children: open PRs of the same repository whose `base` is H (**child-of-merged-branch**), or whose base is B and
  whose commit list (`pulls/N/commits`, at most 3 pages of 100) contains S (**contains-merged-commits**; this catches children
  that GitHub already retargeted to B after deleting H).
- `old_parent` is S: the last head SHA of M that the watcher observed (state), which is also the merged head the child was built on.
  If M's head moved between the last observation and the merge, `old_parent` is the merged head reported by `pulls/M`.
- `new_parent` is C (the merge commit of the PR as reported by GitHub; for a rebase merge it is the rewritten tip on B). It may be
  behind the branch tip if more merges followed; `new_parent_source` is `merge_commit_sha`, and the consumer may fetch and use
  `origin/B` after verifying. If C is unreadable the event is still emitted with `new_parent` null and `argv` null.
- `argv` is the exact argument vector, no shell:
  `["git","-c","rebase.updateRefs=false","-c","rerere.enabled=false","rebase","--onto",NEW,OLD,BRANCH]` where BRANCH is the child's
  head ref. The watcher only reports it. Forks: a child whose head repository differs from the base repository is reported
  with `branch` as `OWNER:REF` and `argv` null.
- A child with `old_parent` not found in its commit list is not reported. Order: ascending PR number. Ids include the merge commit,
  so a restart does not repeat them.

## Event ids, re-arming and idempotence

Every event has a stable `id`: `repo#pr:kind:key` where `key` is the head SHA for head-bound kinds (`checks-*`,
`mergeable-changed`, `checks-pending-timeout`), the new value for `ready-state-changed`, `base-changed` and `mergeable-changed`
(plus head), the merge commit for `merged` and `restack-needed` (plus child number), and a fixed word for the others (`closed`,
`new-pr` plus head). When a PR leaves a state (for example checks leave `green`), the ids of that state's events for that head
are forgotten (tombstone line in the journal) so the same state can be announced again later. Everything else is suppressed
when its id is in the journal, which makes restarts and replays idempotent. `seq` is a gapless counter across the state
directory and increases by one per emitted event. `time` is the observation time (UTC, `Z`).

## Liveness contract

- `heartbeat` is rewritten after every completed cycle (any repository may have failed); its content is the time of the last
  cycle and the pid.
- `stw watch --status [--state-dir DIR] [--json]` reads the directory only (no network, no lock): prints the state directory,
  the heartbeat age, `last_ok` per repository with age, the last `seq`, whether a watcher holds the lock (and its pid), and the
  open `watch-error` per repository. It is **fresh** when the heartbeat is younger than `max(3 x interval, 3 min)` (the
  interval recorded in state). Exit 0 fresh, 5 stale or missing heartbeat, 2 usage.
- A consumer that stops seeing events must call `--status` rather than assume quiet.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | `--once` finished (also with `watch-error` events: they are data); `--until-event` matched; `--stream` ended by SIGINT/SIGTERM or `--max-idle`; `--status` fresh |
| 1 | no repository could be read at all in `--once`, missing authentication, unwritable state directory, corrupt `state.json` (never silently reset: use `--baseline`) |
| 2 | usage or config error; nothing is read or written |
| 3 | another watcher holds the lock of this state directory |
| 4 | `--until-event` reached `--timeout` without a matching event |
| 5 | `--status`: heartbeat stale or missing |

## User stories and independent tests

Tests use an in-process fake forge (the Spec 022 HTTP injection seam, fixed responses per step) and a fake clock.

1. **US1 Baseline (P1).** First run prints nothing and writes state; a second `--once` after a fixture change prints exactly the
   changed events. `--baseline` after state exists prints nothing and replaces state.
2. **US2 Every event kind (P1).** One fixture sequence per kind of the table produces exactly that event with exactly those
   `details` keys, validated against the schema; a flap (red then green within one cycle pair) produces nothing (debounce).
3. **US3 Stacked merge (P1).** Parent PR rebase-merged (merge commit and SHAs differ from the head); a child on the parent head
   ref and a child already retargeted to main with the parent's commits yield two `restack-needed` events with `old_parent` = merged
   head, `new_parent` = merge commit and the exact argv; an unrelated PR yields none; an unreadable merge commit yields null.
4. **US4 Restart idempotence (P1).** Kill after journal append (before state replace), restart: no duplicate id in the journal; a
   cleared and re-entered state emits again (re-arm).
5. **US5 Rate limit and 304 (P1).** Second cycle with unchanged ETags sends `If-None-Match`, gets 304 and emits nothing and issues no
   detail requests beyond the conditional ones; `rate_limit` with `remaining` under 10% doubles the next interval; exhausted sleeps
   to `reset` and emits one `watch-error` `rate-limited`.
6. **US6 Offline (P2).** Connection errors back off 1x, 2x, 4x up to the cap, state untouched, one `watch-error`, then
   `watch-recovered`; no panic, exit 0 in `--once`, 1 when every repository failed in `--once`.
7. **US7 Discovery (P2).** `--repo` list; `--workspace` over a temporary root with two GitHub submodules, one non-GitHub (skipped,
   reported), one uninitialized (skipped); config file strictness (unknown key refused); ignore authors and labels; `repos-changed`
   after a config edit.
8. **US8 Modes and liveness (P1).** `--until-event checks-green,merged` prints the whole poll and exits 0; `--timeout` exits 4;
   `--status` exit 0 when fresh, 5 when the fake clock passed 3 intervals; second watcher exits 3; `--human` golden lines.
9. **US9 Scale (P2).** 120 open PRs in one repository (two pages) and 50 repositories complete one cycle; no cap below the
   documented 1000 PRs per repository; the journal and state stay valid JSON.
10. **US10 Read-only proof (P1).** The fake forge fails the test on any non-GET request; a source test greps that the watch
    package imports no write helper; no token appears in output, state or journal.

## Limits and safety

Own repositories, no hostile party is modeled (the same stance as Spec 022 for repository selection); still, bodies are capped by
the transport, text fields are bounded and escaped in `--human`, and JSON strings are valid UTF-8. There is no artificial cap on
PRs or repositories below the sanity bounds: 1000 open PRs per repository (10 pages), 256 repositories per run (as `stw status`),
20 names per `failing` or `pending` list, 4 KiB of check text read per run, 3 pages of commits per restack candidate. Output per
poll is bounded only by these (a baseline-free first-appearance of 1000 PRs would be 1000 `new-pr` lines, which is correct).
The 30 s operation context of Spec 022 applies **per request**, not to the whole watch; the finite-total-clock design of the
deferred Spec 030 `--watch` is a different, one-PR command and stays deferred.

## Out of scope (and why)

- **Auto-ready, rerun, merge, label, comment**: writes need their own spec with disclosed effects and an approval model. A possible
  later `stw pr ready --ledger FILE` could consume `checks-green` events and a ledger of reviewed SHAs; it is not designed here.
- **Webhooks or a long-lived server**: a poll with ETags is enough for a dozen repositories and needs no inbound network.
- **Chat or desktop notifications**: consumers read the JSONL stream.
- **A review ledger or review verdicts**: `watch` reports facts, never "CLEAR".
- **Log download and failure triage beyond `shape`**: needs workflow-run endpoints and log bytes; deferred.
- **jj or non-GitHub forges**: the hosting layer is GitHub only today.
- **Changing Spec 030**: `stw hosting pr --watch` stays deferred and unaffected.

## Open questions for the owner

1. Is `review-state-changed` wanted in v1 (extra per-PR reads) or stays reserved?
2. Is default state under `$XDG_STATE_HOME` right, or should `--workspace` mode keep it in `.stw/watch/` of the workspace?
3. `pulls/N` detail per open PR each cycle (304-cheap): acceptable, or poll detail only for non-draft PRs?
