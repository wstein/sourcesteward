# Watching pull requests with `stw watch`

`stw watch` ([Spec 035](../specs/035-stw-watch/spec.md)) polls the open pull requests of one or more GitHub repositories,
compares each poll with the previous one and prints **only the transitions**, one JSON object per line. It decides
nothing: a consumer (an agent, a shell loop, `jq`, a Monitor tool) reads the stream. The command is listed in
`stw --help`; the owner accepted it on 2026-10-22 after a dogfood run. The event schema is
[`watch-event.schema.json`](../schemas/v1/watch-event.schema.json) (unpublished, byte-frozen, see
[JSON contracts](json-contracts.md#watch-event-schema-v1-unpublished)).

## Use

```sh
stw watch --repo wstein/sourcesteward --repo wstein/crewbook          # stream until SIGINT or SIGTERM
stw watch --workspace                                                 # the workspace root and every initialized GitHub submodule
stw watch --repo wstein/sourcesteward --once                          # one poll, print, exit
stw watch --repo wstein/sourcesteward --until-event checks-green,merged --timeout 590s
stw watch --repo wstein/sourcesteward --once --human                  # one readable line per event
stw watch --status --state-dir /abs/dir [--json]                      # liveness, no network
```

The first run on a state directory is a **silent baseline**: it prints nothing and writes state. Later runs print what
changed. `--baseline` repeats the baseline on an existing state. Repositories come from `--repo`, then `--workspace`
(found as `stw status` finds the workspace), then the `repos` of `--config`; without any of them `.stw/watch.toml` of the
current workspace is used when it exists.

### Modes and exit codes

| Mode | Behaviour |
| --- | --- |
| `--once` | poll every repository once |
| `--stream` (default) | poll until SIGINT or SIGTERM (a second signal ends the process at once); `--max-idle DUR` ends it after a quiet period |
| `--until-event KIND[,KIND]...` | like `--stream`, exits 0 right after the poll that printed a listed kind; `--timeout` (default 590s) |
| `--status` | reads the state directory only (no network, no lock) |

| Exit | Meaning |
| --- | --- |
| 0 | done (`--once` also with `watch-error` events: they are data), `--until-event` matched, stream ended, `--status` fresh |
| 1 | no repository readable in `--once`, missing authentication (before any request), unwritable or corrupt state |
| 2 | usage or configuration error; nothing is read or written |
| 3 | another watcher holds the lock of this state directory |
| 4 | `--until-event` timed out |
| 5 | `--status`: heartbeat stale or missing |

A consumer that stops seeing events must call `--status` instead of assuming quiet: the heartbeat is fresh when younger
than `max(3 x interval, 3 min)`.

### State directory

`$XDG_STATE_HOME/stw/watch/<workspace-id>/` (mode 0700, files 0600), never inside a repository; `--state-dir ABSOLUTE`
pins it (inside a Git work tree it is refused). It holds `lock`, `state.json`, `journal.jsonl` (every event, fsynced
before it is printed; `seq` is derived from its tail) and `heartbeat`. A changed repository list selects a new directory
(a new baseline) unless `--state-dir` pins it. A deleted `state.json` is rebuilt silently from the journal.

### Configuration

`.stw/watch.toml` (strict: unknown keys are exit 2; `schema_version = 1`):

```toml
schema_version = 1
repos = ["wstein/crewbook", "wstein/sourcesteward"]
interval = "60s"            # minimum 10s, a jitter of up to 5s is added
detail_every = 10
max_open_prs = 2000         # 1..2000; above it: watch-error too-many-open-prs
[ignore]
authors = ["dependabot[bot]"]
labels = ["no-watch"]
[shape]
stack_patterns = ["PR must target repository main"]
```

Flags override the file.

## Events and deduplication

Every line has `schema_version`, `seq` (gapless per state directory), `id`, `time`, `repo`, `pr`, `kind` and `details`;
no key is omitted. Kinds: `new-pr`, `head-changed`, `checks-green`, `checks-red`, `checks-pending-timeout`,
`mergeable-changed`, `ready-state-changed`, `base-changed`, `merged`, `closed`, `restack-needed`, `repos-changed`,
`watch-error`, `watch-recovered`, `watch-stalled`. Checks and mergeability become events only when two consecutive
cycles agree (a flap prints nothing).

Delivery is at least once: after a crash the journal line is printed again with the **same `id`**. A consumer keeps the
set of ids it has seen and ignores a repeated one; `seq` orders the journal. An `id` is `repo#pr:kind:key`, where the key
carries an epoch counter, so a legitimately repeated transition (a second force-push, a second failure) has a new id.

### `restack-needed`

After a `merged` event the watcher names every open PR that must be restacked, for the owner's rebase merges (GitHub
rewrites the parent commits): `child-of-merged-branch` (the PR's base is the merged head branch) and
`contains-merged-commits` (an open PR on the merged base whose commit list holds the old head). The details give
`old_parent` (the merged head), `new_parent` (the merge commit), `branch` and the exact `argv`:

```sh
git -c rebase.updateRefs=false -c rerere.enabled=false rebase --onto NEW_PARENT OLD_PARENT BRANCH
```

`new_parent` and `argv` are `null` when the merge commit is unreadable; for a child whose head lives in a fork only `argv`
is `null` (`new_parent` stays the merge commit and `branch` is the head ref). The watcher only
reports the command; it never runs it. With `--human` the SHAs show seven characters.

## Safety and limits

- **Read-only.** Only fixed REST `GET` requests of the Spec 022 hosting transport: no mark-ready, rerun, merge, comment,
  label or edit. Its only writes are its own state directory. The acceptance tests fail on any non-GET request and the
  sources are checked for write helpers. The token comes from the `gh` route of the hosting commands and is never
  printed or stored.
- **Rate limits.** Lists, branch tips, details and checks are conditional (`ETag`, `304` costs nothing). Below 10 percent
  of the limit the interval doubles; below 50 remaining (or a 403/429 with rate-limit evidence) the whole run waits until
  the reset plus 5 s and prints one `watch-error` `rate-limited`.
- **Errors.** A failing repository keeps its last state, backs off (1x, 2x, 4x, capped), prints one `watch-error` and a
  `watch-recovered` after the next success; `watch-stalled` follows a long silence.
- **Bounds.** 2000 open PRs per repository, 256 repositories per run, 20 names per list, three pages of commits per
  restack candidate.
- **Platforms.** Linux, macOS and other Unix systems (the state lock is `flock`); Windows and other non-Unix platforms are
  unsupported: the command exits 2 with "not supported on this platform".

## Relation to the scripts

`stw watch` replaces the foreman's `watch2.sh` (the parity list is in the spec section "Differences from `watch2.sh`":
auto-ready, the clear ledger and the relay are dropped on purpose) and is meant to retire the interim
`scripts/ci-wait.sh`. Removing the script is a separate owner decision after the feature is accepted.
