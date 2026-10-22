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
          [--baseline] [--human] [--max-idle DUR] [--pending-timeout DUR] [--detail-every N]
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
- `--detail-every N` (default 10, minimum 1): slow refresh cycle of PR detail and settled checks; see Observation.
- Output goes to stdout, one event per line, flushed per line. Diagnostics (never tokens) go to stderr only as `watch-error`
  style lines; stdout carries events only.

### Config file `.stw/watch.toml`

Strict profile like `.stw/release.toml` (unknown keys, wrong types and duplicate keys are usage errors; `schema_version = 1`
required). Minimal by design:

```toml
schema_version = 1
repos = ["wstein/crewbook", "wstein/sourcesteward"]
interval = "60s"
detail_every = 10                  # cycles between refreshes of PR detail and checks
max_open_prs = 2000                # per repository; above it: watch-error too-many-open-prs
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
| `state.json` | `schema_version`, `printed_seq` (seq of the last event printed to stdout), per repository `last_ok`, the ETag of every list page and branch-tip read, `generation` and the per-PR epoch counters (see Event ids), the closed-unmerged PR generations (at most 5000, oldest dropped), and per open PR the last accepted observation plus any debounce candidate and per-PR `etag`s; **`seq` is not stored here**; replaced atomically (write temp, fsync, rename) |
| `journal.jsonl` | every emitted event and nothing else, one schema-valid object per line, append-only, fsynced before anything is printed; the source of `seq` and the delivery record |
| `heartbeat` | one line `RFC3339-time pid mode`, rewritten atomically after every poll cycle that completed (also when some repositories failed, see liveness) |

Order inside one poll cycle, exactly: (1) compute the events from (state, observation); (2) append each event line to
`journal.jsonl` and `fsync`; (3) print the events to stdout; (4) write `state.json` with the new observation, the counters
(epochs, generations, episodes, `repos_epoch`) and `applied_seq` = `printed_seq` = the last `seq` of the cycle.
`seq` is **derived from the journal tail**: the next `seq` is the `seq` of the last complete journal line plus one (1 for an empty
journal), so it is gapless and survives a lost or older `state.json`.

**Replay on start** (before the first poll): the watcher reads the journal lines with `seq` greater than `state.applied_seq` and
(a) **applies each event to the state** (the exact effect of every kind is the "Replay effects" table below): its counter is set to the epoch, generation or episode encoded in its `id` (never lowered),
and its observed effect is applied to the accepted observation (`new-pr` and `closed`/`merged` open or remove the PR and set the
generation; `head-changed` the new head; `checks-green`/`checks-red` the class; `mergeable-changed` the mergeability;
`ready-state-changed` the draft flag; `base-changed` the base; `repos-changed` the repository set and `repos_epoch`; `watch-*` the
failure or stall episode); then writes `state.json` with `applied_seq` advanced; and (b) prints the lines with `seq` greater than
`printed_seq` in order (at-least-once delivery). Because the journalled transition is already in the state, the first poll compares
against it: if the change flapped back, the reverse transition is a **new** event with the next epoch; if it still holds, nothing
repeats. Ids therefore stay unique and no event is lost or duplicated. Result: **exactly once in the journal, at least once on
stdout; consumers dedupe by `id`**. Crash windows: after (2) before (3): replay applies and prints the event once; after (3)
before (4): replay applies it, stdout has it twice, journal once; after (4): once and once. There is no tombstone or suppress file
(see Event ids).
Truncated trailing journal lines are ignored and healed with a newline on the next append. The journal is never rotated or deleted by
`stw watch`; its size is the owner's to manage (about 300 bytes per event).

## Observation, debounce and polling

Per repository and cycle (all GET, through the Spec 022 transport, 30 s per request, 1 MiB body cap, no redirects):

1. `pulls?state=open&per_page=25&page=N` (following pages are those of the `Link: rel="next"` header, never guessed). The bound is
   `max_open_prs` per repository, default **2000**, configurable in `watch.toml`: at 25 per page that is **80 pages**, at 12 per
   page **167 pages**; exceeding it is `watch-error` code `too-many-open-prs` for that repository (it keeps its last accepted
   state). Halving the page size **restarts the listing at page 1** with `per_page=12` (page numbers of different sizes are not
   mixed). On a `304` for a page the **stored Link header and the stored parsed page** (ETag, Link and the list items kept in
   `state.json`) are reused for that page, so a fully unchanged repository needs no response bodies. A `304` carries no `Link`, so when the last page answered `304` and holds exactly `per_page` items, the next page is also requested once (conditionally, with its stored ETag if any; an empty answer is stored too), because an older PR reopened behind a full last page would otherwise be missed; a `200` needs no probe because its `Link` is current, and no page beyond the allowlist bound (80 at 25, 167 at 12) is requested. The page
   size is 25 because a list item is large: a live read-only sample (`gh api 'repos/wstein/sourcesteward/pulls?state=open&per_page=5'`,
   2026-10-22, one open PR) returned **17,047 bytes for one PR**, and an earlier measurement of 20 PRs was 361,596 bytes (about 18 KB
   each); 25 PRs are about 450 KB, inside the 1 MiB body cap of Spec 022 FR-006, which this spec does **not** raise (100 per page
   would be about 1.8 MB and fail with a body-limit error from 58 PRs on). If a page still exceeds the cap the watcher retries that
   page once with `per_page=12` (and keeps 12 for that repository until restart: the hosting reader holds it in memory only and `state.json` must not persist it, so a restart tries 25 again); if that also exceeds it, the repository fails the
   cycle with `watch-error` code `page-too-large`. Each page has its own ETag. Gives number, title, user, labels, draft, head SHA and
   ref, base ref and base SHA.
2. `pulls/N` detail (mergeable, mergeable_state; `null` mergeable is "not computed" and keeps the previous value) for **every**
   open PR, drafts included (a draft that turns conflicting after a merge is exactly the restack case), but **only when needed**:
   when the PR is new, its head SHA changed, or the **tip of its base branch** changed (a conditional `branches/BASE` read, one per distinct base branch per repository per cycle; the `base.sha` of the list is **not** relied on: it could not be shown to track the moving tip, because the live repositories had only one open PR whose base was still the tip, so only the branch-tip read is a contract); for all open PRs of a repository in the cycle after
   a `merged` event of that repository (stacked children); and on a slow refresh every `--detail-every N` cycles (default 10), per
   PR staggered by number so the load is spread. Otherwise the previous `mergeable` is kept.
3. `commits/SHA/check-runs` and `commits/SHA/statuses` (existing allowlisted paths, bounded pagination) only for open PRs whose last
   accepted class is `pending` or `none`, or whose head just changed, and on the same slow refresh for `green`/`red` PRs. These are
   conditional reads (ETag per head and path).
4. For a PR that left the open list: `pulls/N` once (state, merged, merge commit, base, head).
5. `rate_limit` once per cycle (free).

GraphQL is not used: the Spec 022 allowlist (`fixedEndpoint`) admits only fixed REST GETs, and GraphQL is a POST; a read-only
GraphQL query would need an allowlist and policy change that this spec does not make. The conditional REST design is the contract.

Budget per poll (steady state, nothing changing): **ceil(open PRs / 25) conditional list calls per repository (all 304, free of
rate limit)**, one conditional `branches/BASE` call per distinct base branch (304), one conditional check read pair per PR still
`pending`/`none` (304 when unchanged) and one free `rate_limit` read per cycle. Calls that cost rate limit are the 200
responses: a changed page, a new head or base tip, detail after a head change or merge, the slow refresh. Worst case (first baseline)
for P open PRs: ceil(P/25) + P detail + 2P check calls. A steady 100-PR, 6-repository watch at 60 s is about 6 x 4 conditional list
calls per minute plus the pending PRs' conditional check reads; 304s do not count against the primary rate limit.

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
| `checks-pending-timeout` | class stays `pending` on one head longer than `--pending-timeout`; once per pending period (see Replay effects) | `head`, `pending` (names, at most 20), `since` |
| `mergeable-changed` | debounced `mergeable` flips, or `mergeable_state` enters or leaves `behind` | `mergeable` (`true`/`false`), `state` (GitHub `mergeable_state`), `conflicting`, `behind` (booleans), `base` |
| `ready-state-changed` | `draft` flips | `from`, `to` (`draft`/`ready`) |
| `base-changed` | the PR's base ref changed (retarget, also GitHub's automatic retarget after a parent merge) | `old`, `new` |
| `merged` | a PR that left the open list is `merged` | `merge_commit`, `base`, `head` (the merged head SHA), `head_ref`, `merged_at`, `method_hint` (`rebase-or-squash` when `merge_commit` is not a merge commit with 2 parents, `merge` otherwise, `unknown` when unreadable) |
| `closed` | a PR that left the open list is closed unmerged | `head`, `base` |
| `restack-needed` | follows a `merged` event, one per affected open PR (rules below) | `base`, `reason` (`child-of-merged-branch` or `contains-merged-commits`), `merged_pr`, `old_parent`, `new_parent`, `new_parent_source`, `branch`, `argv` |
| `repos-changed` | the resolved repository set differs from the one in state (including a skipped submodule); a repository that was **added** gets a **silent baseline** of its own (no `new-pr` for its existing PRs) | `added`, `removed`, `skipped` (with reason) |
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

`unknown` will be the common result for GitHub Actions failures, because check-run `output.summary` is often empty or generic; that is
intended and safe (fail loud). A later optional refinement may read check-run annotations (a further GET) to recognise more shapes; it is
a future work package, not v1.

This is at least as strict as `watch2.sh`, which classifies from job logs by the same two patterns but does not look at the PR
base and does not require every failing item to match; here both are required, and the log is replaced by the bounded check-run
text. A consequence is that a `stack-merge-shape` can come out as `unknown` where the log would have shown it (summary lacks the
text); that error direction is intended. The consumer may treat `stack-merge-shape` as expected on a stacked PR and everything
else as needing a look.

### restack-needed rules

On a `merged` event of PR M (base B, head ref H, merged head SHA S, merge commit C), in the same cycle and before the next poll:

- Two disjoint classes of candidate children (open PRs of the same repository):
  1. **`child-of-merged-branch`**: the PR's `base` ref name equals H. Detected by the base ref name **only**; no commit check
     (`pulls/N/commits` lists H...child and never contains S, so it cannot confirm this class).
  2. **`contains-merged-commits`**: the PR's `base` is B (not H) and its commit list (`pulls/N/commits`, at most 3 pages of 100,
     read only for open PRs on B in the merge cycle) contains S. This catches children that GitHub retargeted to B after the parent
     branch was deleted. The commit check applies to this class only.
- `old_parent` (defined once) is S: the head SHA of M as reported by `pulls/M` after the merge. If `pulls/M` cannot be read there
  is no `merged` event yet: the PR stays "gone, unresolved" in state and is retried every cycle (a `watch-error` follows the usual
  rules), so `restack-needed` never carries a guessed parent.
- `new_parent` is C (the merge commit of the PR as reported by GitHub; for a rebase merge it is the rewritten tip on B). It may be
  behind the branch tip if more merges followed; `new_parent_source` is `merge_commit_sha`, and the consumer may fetch and use
  `origin/B` after verifying. If C is unreadable the event is still emitted with `new_parent` null and `argv` null.
- `argv` is the exact argument vector, no shell:
  `["git","-c","rebase.updateRefs=false","-c","rerere.enabled=false","rebase","--onto",NEW,OLD,BRANCH]` where BRANCH is the child's
  head ref. The watcher only reports it. Forks: a child whose head repository differs from the base repository is reported
  with `branch` as `OWNER:REF` and `argv` null.
- A class-2 candidate whose commit list lacks `old_parent` is not reported. Order: ascending PR number. Ids include the merge commit,
  so a restart does not repeat them.

## Event ids, re-arming and idempotence

Every event has an `id` of the form `repo#pr:kind:key` (`-` for an absent repo or PR). Ids are deterministic functions of state, so
a replay after a crash recomputes the same id, and they are unique per transition because the state holds **counters** that
increment on every accepted transition. There are no tombstones and no suppress file: the journal holds events only (so every line
validates against the schema) and re-arming is done by the counters. Per-PR state: `epoch` per transition family (`head`, `checks`,
`mergeable`, `ready`, `base`), `generation` (incremented when a closed-unmerged PR is seen open again), per repository `episode`
per `watch-*` family, and the global `repos_epoch`.

| Kind | `key` |
| --- | --- |
| `new-pr` | `g<generation>` (a reopened PR is announced again as `new-pr` with the next generation) |
| `head-changed` | `<new head>:h<head epoch>` (the epoch increments on every accepted head change, so every force-push fires, also a return to an earlier SHA) |
| `checks-green`, `checks-red` | `<head>:c<checks epoch>`; the checks epoch increments on **every** accepted class change, also to `pending` or `none` (which emit nothing), so red, green, red announces each time |
| `checks-pending-timeout` | `<head>:c<checks epoch>:t` (once per pending period: a new period needs a class change, which bumps the epoch) |
| `mergeable-changed` | `<head>:m<mergeable epoch>` |
| `ready-state-changed` | `r<ready epoch>` |
| `base-changed` | `b<base epoch>` |
| `merged` | the merge commit SHA (`unknown` if unreadable) |
| `closed` | `g<generation>` (closed, reopened, closed again gives `g0`, then `g1`) |
| `restack-needed` | `<merged PR number>:<merge commit>` (the PR in the id is the child) |
| `repos-changed` | `p<repos_epoch>` (a counter in state, incremented per accepted change and replayed like the others; A, B, A, B all fire with distinct ids) |
| `watch-error` | `<code>:e<episode>:f<failures>`; an episode starts at the first failure after a success and ends at the next success; `failures` is the consecutive count at emission (1, then 10, 20, ...), so a new code, a new episode and every 10th failure fire and the same ongoing episode does not |
| `watch-recovered` | `e<episode>` (after a `watch-error` episode) or `s<stall episode>` (after a `watch-stalled`) |
| `watch-stalled` | `s<stall episode>` per repository |

`seq` is a gapless counter derived from the journal tail (see State and journal layout). `time` is the observation time (UTC, `Z`).

### Replay effects (audit of every kind)

**Invariant**: any state transition that a poll would detect again after a restart must either be covered by a replay effect that
makes the recomputed id different, or be impossible to re-detect (the replay already moved the accepted state past it). Level-based
triggers (a condition that stays true) therefore carry an explicit "fired" flag in state that the event and its replay both set;
edge-based triggers are covered by moving the accepted observation. A transition that emits no event (for example a class change to
`pending`) is never journalled; after a crash it is recomputed identically from the unchanged state, with the same counters.

| Kind | Id key (see above) | Fields the cycle advances | Replay effect from the journal line | Crash after append: first poll after restart |
| --- | --- | --- | --- | --- |
| `new-pr` | `g<generation>` | PR added to the accepted open set with its list data; `generation` | add the PR from `details` (head, base, draft, title, author), set `generation`, checks class `none`; mark the PR **due for a detail read** | PR known: nothing |
| `head-changed` | `<new>:h<head epoch>` | `head`, `head epoch`; checks class reset to unobserved, debounce candidates cleared | head = `new`, head epoch from the id, checks reset; mark the PR due for a detail read | head equal: nothing; another force-push: new event, next epoch |
| `checks-green` / `checks-red` | `<head>:c<checks epoch>` | accepted class, checks epoch, candidate cleared | class = green/red for `head`, epoch from the id | same class: nothing; flapped back: new event, next epoch |
| `checks-pending-timeout` (level-based) | `<head>:c<checks epoch>:t` | `pending_since`, flag `timeout_fired` for that head and epoch | class = pending, `pending_since` = `since`, `timeout_fired` = true, epoch from the id | flag set: nothing; class changed: bumps the epoch, so a later pending period fires again with a new id |
| `mergeable-changed` | `<head>:m<mergeable epoch>` | accepted `mergeable`, `state`, `conflicting`, `behind`, epoch | those values from `details`, epoch from the id | same value: nothing; flapped back: new event, next epoch |
| `ready-state-changed` | `r<ready epoch>` | accepted `draft`, epoch | draft = (`to` is `draft`), epoch from the id | nothing; flipped back: next epoch |
| `base-changed` | `b<base epoch>` | accepted `base`, epoch | base = `new`, epoch from the id; mark the PR due for a detail read | nothing; changed again: next epoch |
| `merged` | `<merge commit>` | PR removed from the open set and marked resolved; `restack_due` entry for the repository | PR removed and resolved; `restack_due` (merged PR, merge commit, old parent) added; every other open PR of the repository marked due for a detail read (as a live cycle does after a merge) | PR resolved: no second `merged`; pending restack work is finished (next row) |
| `restack-needed` (level-based on the open children) | `<merged PR>:<merge commit>` (child in the id) | `restack_due` cleared at the end of the same cycle | the line's (child, merge commit) is recorded as done; `restack_due` stays until the next successful poll | the first poll recomputes the candidates of every `restack_due` merge (including the class-2 `pulls/N/commits` reads, which happen in that same cycle) and emits only the (child, merge commit) pairs not yet journalled, then clears `restack_due`; a child already rebased is no longer a candidate |
| `closed` | `g<generation>` | PR removed, `closed_prs[n]` = generation | PR removed, generation recorded | not in the open list and resolved: nothing; reopened: `new-pr` with the next generation |
| `repos-changed` | `p<repos_epoch>` | repository set, `repos_epoch`; baseline mark for added repositories | set and `repos_epoch` from the id; added repositories marked "needs silent baseline" | set equal: nothing; added repositories baseline silently |
| `watch-error` (level-based) | `<code>:e<episode>:f<failures>` | per-repository open episode, last emitted code, last emitted failure count | episode, code and count from the id and `details` | same code, count not at a multiple of 10: nothing; new code or the 10th: new key |
| `watch-recovered` (level-based) | `e<episode>` / `s<stall episode>` | episode closed, failures 0, stall flag cleared | episode closed (flags cleared) | no open episode: nothing |
| `watch-stalled` (level-based) | `s<stall episode>` | per-repository `stalled` flag, stall episode | `stalled` = true, episode from the id | flag set: nothing until a success clears it (then `watch-recovered`) |

Plan acceptance test (WP-A, generated from this table): for **each** row, a table-driven test builds the pre-state, runs the cycle
that emits the kind, kills after the journal append (and separately after the print), restarts against an observation that (a) still
shows the condition and (b) shows it flapped back, and asserts that the journal holds **exactly one line** for that id, that every id in
the journal is unique, and that (b) adds exactly one new event with the next epoch. A new event kind is not accepted without a row here. "Due for a detail read" is state that makes the first poll after a restart fetch `pulls/N` for that PR exactly as the live cycle would have, so `mergeable` is never stale for up to `detail_every` cycles because of a replay.

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
   ref and a child already retargeted to main with the parent's commits yield two `restack-needed` events (`child-of-merged-branch`
   detected by base ref name alone with the commits endpoint never read for it, and `contains-merged-commits` detected by the commit
   list) with `old_parent` = merged head from `pulls/M` (with `pulls/M` failing there is no `merged` and no `restack-needed` until it is readable),
   `new_parent` = merge commit and the exact argv; an unrelated PR yields none; an unreadable merge commit yields null.
4. **US4 Restart idempotence (P1).** Three kill tests with a fake clock: kill after the journal append (restart prints the event
   once, journal has it once), kill after the print (stdout has it twice, journal once, same `id`), kill after the state write
   (once and once); kill after append, restart with a snapshot in which the change flapped back, then a real transition: ids are unique, no event is lost, the flap-back appears as a new event with the next epoch; `seq` stays gapless and is taken from the journal tail even when `state.json` is deleted; every journal line
   validates against the schema; head-changed fires on each force-push, checks red-green-red fires three times, closed-reopened-closed
   fires two `closed` with distinct ids, a second failure episode fires `watch-error` again.
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
   documented 2000 PRs per repository (`too-many-open-prs` above it); the journal and state stay valid JSON.
10. **US10 Read-only proof (P1).** The fake forge fails the test on any non-GET request; a source test greps that the watch
    package imports no write helper; no token appears in output, state or journal.

## Limits and safety

Own repositories, no hostile party is modeled (the same stance as Spec 022 for repository selection); still, bodies are capped by
the transport, text fields are bounded and escaped in `--human`, and JSON strings are valid UTF-8. There is no artificial cap on
PRs or repositories below the sanity bounds: 2000 open PRs per repository (80 pages at 25 per page; `max_open_prs`), 256 repositories per run (as `stw status`),
20 names per `failing` or `pending` list, 4 KiB of check text read per run, 3 pages of commits per restack candidate. Output per
poll is bounded only by these (a baseline-free first-appearance of 2000 PRs would be 2000 `new-pr` lines, which is correct).
The 30 s operation context of Spec 022 applies **per request**, not to the whole watch; the finite-total-clock design of the
deferred Spec 030 `--watch` is a different, one-PR command and stays deferred.

## Amendments to Spec 022 (explicit)

`stw watch` needs these, and only the watch reader may use them; the one-shot operations and their fixtures stay unchanged:

- **FR-005 (allowlist)**, new fixed GET endpoints: `pulls?state=open&per_page=25&page=1..80` and `per_page=12&page=1..167`, `pulls/N/commits?per_page=100&page=1..3`,
  `branches/BASE` (BASE encoded as below), `commits/SHA` (to read the merge commit and decide `method_hint`), `rate_limit`;
  existing `pulls/N`, `commits/SHA/check-runs|statuses` stay. Conditional `If-None-Match` and reading the `ETag` and `Link`
  response headers are added for the watch reader. Redirects are still refused.
- **Branch-name path segments (FR-005 guard)**: `branches/BASE` needs a percent-encoded segment (`feat%2Fx`), and `guardedTransport`
  refuses any `RawPath`. The hosting layer encodes BASE with **one tested function** (`url.PathEscape` on the whole branch segment,
  after validating BASE against the git branch-name rules: no control or NUL, no `..`, no leading `-`, no `@{`, not ending in `/` or
  `.lock`, at most 255 bytes); the guard accepts a `RawPath` **only** for a segment produced by that function, re-validated by
  decoding exactly once and re-encoding to the same text (so `%2E`, `%00`, `%252F` and any double encoding are refused). Tests:
  `feat/x`, `release/v1.2`, a non-ASCII branch (UTF-8 percent-encoded), and the attack strings `..%2F`, `%252F`, `%2e%2e`, `%00`.
  A literal `%` is refused in a branch name (it would be double encoding), so a PR whose base branch contains `%` cannot be watched:
  the `branches/BASE` read fails with `malformed-response` and a diagnostic naming the invalid branch name. The guard checks the
  escaped wire form of the request path (`EscapedPath`), not the decoded `Path`. GitHub answers `404` for a missing branch and for an
  inaccessible repository alike; the hosting layer maps every `404` to code `forbidden` with HTTP status `404` kept in the error, so
  the caller can tell a deleted base branch (`branches/BASE`) from other `forbidden` reads.
- **FR-007 (deadline)**: the 30 second context is **per request** for the watch reader (the process is long-lived); the whole-watch
  clock of the deferred Spec 030 `--watch` is not used here.
- **Renamed repositories**: GitHub answers 301 and the transport refuses redirects; the watcher reports `watch-error` code
  `repo-moved` (with the new full name from the `Location` header in `message` if present) and **never follows it**; the owner
  updates the repository list, which starts a silent baseline for the new name.

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
  `repos-changed`; no 60-PR limit (2000 open PRs per repository by default); read-only guarantee tested; works for any repository set.

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
