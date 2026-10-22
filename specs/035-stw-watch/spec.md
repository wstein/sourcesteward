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
stack_patterns = ["PR must target repository main"]   # extra fixed strings, added to the two defaults (rule 2)
```

Flags override the file. `[shape]` is optional; the defaults are in the `shape` rules.

## State and journal layout

State directory: `--state-dir ABSOLUTE_PATH` if given, else `$XDG_STATE_HOME/stw/watch/<workspace-id>/` (fallback
`~/.local/state/stw/watch/<workspace-id>/`). It is **never** inside a repository, a worktree or a `.stw/` directory: `.stw/` is
committed project configuration (`release.toml`, `watch.toml`), and a journal there would be committed or dirty the tree; a
`--state-dir` inside a Git work tree is refused (exit 2). Created `0700` (an existing directory with wider mode is exit 1);
files `0600`. `<workspace-id>` is the first 16 lowercase hex digits of SHA-256 over the UTF-8 text
`stw-watch-v1\n` + host + `\n` + `workspace:` + the canonical (symlink-resolved, absolute) workspace root when `--workspace` is used,
else `repos:` + the sorted, de-duplicated, lowercased `OWNER/NAME` list joined by `\n`. A changed repository list therefore
selects a new directory (a new silent baseline) unless `--state-dir` pins it. The foreman's scratchpad use maps to an explicit
`--state-dir <scratchpad>/watch-state` (its `alive` file is the `heartbeat`, `emitted.jsonl` is `journal.jsonl`). Nothing else is written.

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
2. `pulls/N` detail (mergeable, mergeable_state; `null` mergeable is "not computed" and keeps the previous value) for **every**
   open PR, drafts included (a draft that turns conflicting after a merge is exactly the restack case), but **only when needed**:
   when the PR is new or its head SHA or base SHA (from the list) changed; for all open PRs of a repository in the cycle after
   a `merged` event of that repository (stacked children); and on a slow refresh every `--detail-every N` cycles (default 10), per
   PR staggered by number so the load is spread. Otherwise the previous `mergeable` is kept.
3. `commits/SHA/check-runs` and `commits/SHA/statuses` (existing allowlisted paths, bounded pagination) only for open PRs whose last
   accepted class is `pending` or `none`, or whose head just changed, and on the same slow refresh for `green`/`red` PRs. These are
   conditional reads (ETag per head and path).
4. For a PR that left the open list: `pulls/N` once (state, merged, merge commit, base, head).
5. `rate_limit` once per cycle (free).

GraphQL is not used: the Spec 022 allowlist (`fixedEndpoint`) admits only fixed REST GETs, and GraphQL is a POST; a read-only
GraphQL query would need an allowlist and policy change that this spec does not make. The conditional REST design is the contract.

Budget per poll (steady state, nothing changing): **one conditional list call per repository (a 304, no rate limit cost)**, plus
one conditional check read pair per PR still `pending`/`none` (304 when unchanged), plus one free `rate_limit` read per cycle.
Calls that cost rate limit are the 200 responses: a changed list, new heads, detail after head/base change or merge, the slow refresh.
Worst case for a repository with P open PRs is 1 + P detail + 2P check calls (a first baseline); a steady 100-PR, 6-repository
watch at 60 s is 6 conditional list calls plus the pending PRs' conditional check reads per minute.

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

`review-state-changed` is **reserved**: not emitted in v1 (owner decision). Review state is not a CI or PR-shape fact: it needs
its own read budget (REST reviews are per PR and per review, with stale-review rules) and, above all, the meaning of an
independent review ("CLEAR" for an exact head) lives in a review ledger, which is a separate concern with its own spec. A future
`review` source can plug into the event stream (same envelope, new kind) without changing the other kinds. Consumers must ignore
unknown kinds.

### `shape` classification (exact, testable)

No log download: the inputs are, per failing check-run, `name`, `conclusion`, `output.title` and the first 4 KiB of
`output.summary` and `output.text` (joined as the *text*), and, per failing status context, `context` and `description`. The
rules apply in this order to the whole failing set of one head; the first that matches decides:

1. **`real`** if **any** failing item has `conclusion` `failure` (or status `failure`) and its text contains a failing-test marker:
   `--- FAIL: `, a line starting with `FAIL` followed by whitespace, `panic:`, `Error:`, or `_test.go:` followed by digits. (`watch2.sh`
   ranks a real failure above a stack artefact in the same run, so does this rule: a real failure is never hidden by a
   stack-shaped failure next to it.)
2. **`stack-merge-shape`** only if **all** of the following hold: the PR's `base` is not the repository default branch (a stacked
   PR); **every** failing item has `conclusion` `failure`; **every** failing item's text contains one of the patterns `Unexpected
   synthetic merge parents` or `PR must target repository main` (extended by `[shape] stack_patterns`, fixed strings, matched
   case-sensitively); and rule 1 did not match. A check name alone never qualifies (the config does not list check names), and an
   empty or unreadable text never qualifies.
3. **`unknown`** otherwise. `unknown` is the default when in doubt: cancelled, timed out, action required, startup failure,
   status-context errors, `failure` with no readable text, infrastructure phrases (runner shutdown, no space left, HTTP 5xx), a
   mix of shape and non-shape failures. The watcher fails loud and never hides a possible real failure as a stack artefact.

This is at least as strict as `watch2.sh`, which classifies from job logs by the same two patterns but does not look at the PR
base and does not require every failing item to match; here both are required, and the log is replaced by the bounded check-run
text. A consequence is that a `stack-merge-shape` can come out as `unknown` where the log would have shown it (summary lacks the
text); that error direction is intended. The consumer may treat `stack-merge-shape` as expected on a stacked PR and everything
else as needing a look.

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

## Differences from `watch2.sh`

- **Dropped**: auto-ready and PR body editing (the Haiku brief did that; `stw watch` is read-only), the clear ledger
  (`clear-ledger.tsv`), the LLM relay that forwarded `EVT` lines, the Anchorage-specific repository list and `READY-CANDIDATE` /
  `READY-PR` labels (decisions belong to the consumer), `gh`-subprocess calls (`gh pr list`, `gh run view`, `gh api` job logs), the
  STOP file and `WATCH_*` environment variables (signals, flags and the config file replace them), the plain-text `EVT` format.
- **Kept**: poll-and-compare with a persistent state, append-only event log, id-based dedupe with re-arming when a state is left,
  debounce of checks and mergeability, 60 s interval with jitter, `--once`, `--stream`, `--until-event`, the `alive` heartbeat,
  the stack-merge-shape idea (stricter, see above), a `merged` event that tells the consumer to check restacks.
- **New**: JSON Lines with a frozen schema and gapless `seq`; `--human`; strict config and workspace discovery; ETag/304 and
  rate-limit awareness; backoff, `watch-error`, `watch-recovered`, `watch-stalled`; `--status` and exit codes; a lock against two
  watchers; `restack-needed` with the exact argv; `checks-pending-timeout`; `mergeable-changed` with `behind`; `base-changed`;
  `repos-changed`; no 60-PR limit (pages up to 1000); read-only guarantee tested; works for any repository set.

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

1. (Decided: `review-state-changed` stays reserved.)
2. Is a 10-cycle refresh default for `pulls/N` detail acceptable, or should it be configurable in `watch.toml` (it is a flag only)?
3. Should the reviewed-SHA ledger and `stw pr ready --ledger` get their own spec next?
