# Watch scenarios (port corpus), format 1

Every `*.json` file in this directory except `index.json` is one scenario. They are generated from the Go tests of
`stw watch` (`legacy/sourcesteward`, `internal/watch`, `internal/watchcmd`, `internal/hosting/watch*`) by
`tests/oracle/watch_scenarios.sh`; the expectations are what the Go code produced for the inputs, never typed by hand.
The files are deterministic (two exports are byte-identical): object keys sorted, two-space indent, one trailing newline.
`tests/it/watch_scenarios.rs` checks the structure of every file and that this document names every field it checks; the
semantics are played by the ports of the core (W1, W3), the observer (W2) and the command (W4).

The scenarios are independent of the Go file formats (owner decision F3: the Rust port writes its own state directory
format). They give the inputs of an observer or of a command and the expected semantic outputs: kind, repository, PR,
details fields, order and step number of events, and a semantic view of the accepted state. Event ids, journal sequence
numbers and hashes are only additional information and are marked as format-dependent (see below).

## Common head

Every scenario starts with: `format_version` (1), `name` (equal to the file name without `.json`, unique), `kind`,
`description`, `source` and `format_dependent`.

- `source` is an object with `go_tests`: the Go tests (`file.go:TestName`, relative to `internal/watch/`, or
  `watchcmd/...`, `watchhost/...`, `hosting/...` for the other packages) the scenario was derived from.
- `format_dependent` names the JSON field names whose values are artifacts of the Go file formats (event `go`,
  `expected` of the id vectors, `line` of the event JSON, `go_message`, `skipped_reason`, `go_line`, `status_text`).
  A port may differ there; everything else is the contract of the scenario.

The kinds are `watch-cycles`, `vectors`, `observer-session`, `http-reader` and `run-loop`.

## watch-cycles

A repository set watched through a sequence of cycles of the core. The core takes one observation per repository per
cycle (this is the interface of the observer) and a restacker for merges.

Fields: `config`, `restacker`, `cycles`, `expected_events`, `expected_state_view`, `state_view_ignore`,
`expected_journal_ordinals`, `expect_unique_event_ids`, `status_probes`.

`config`: `repos` (polling order), `skipped` (objects with `path`, `reason`), `host`, `start_time` (the fake clock before the
first cycle), `interval_seconds`, `pending_timeout_seconds` (0 disables the timeout), `detail_every`, `ignore_authors`,
`ignore_labels`, `stack_patterns`, `mode` (heartbeat text).

`restacker` selects the children of a merged PR. `rule` is `by-merged-pr` with `candidates` (merged PR number as a string to a
list of objects `pr`, `reason`, `branch`, `argv`; the engine takes the order, the core sorts by PR number) or
`every-open-pr-above-merged` with `reason` and `argv` (every open PR with a higher number than the merged one). The
`argv` of a candidate is Go's command; the core puts it into `restack-needed` only when the new parent is known
(owner decision F1: kept as in Go, an exception to D-20261103-03).

`cycles` is the ordered list of inputs, one object per step: `step` (1-based, consecutive), `action` (`cycle`: run one poll
cycle; `open`: only start the watcher, which replays and re-prints), `time` (the fake clock of the step), `note`,
`config_change` (null or an object with `repos`, `skipped` or `baseline`: the repository set, the skipped list or the
forced baseline of the watcher from this step on), `restart_before` (the process is started afresh before the step: the
directory survives, memory does not; implied after a crash), `file_op` (empty, `delete-state` or `corrupt-state`: what
happens to the accepted-state file before the step), `crash` (null or an object `point`, `n`: the process dies after the
nth durable step `append` (event recorded), `print` (event printed) or `state` (accepted state saved) of this cycle) and
`observation`.

`observation` is an object from repository name to what the observer answers for it in this step: `error` (null or an object
`code`, `message`, `run_wide`, `retry_in_seconds`), `default_branch`, `prs` (the complete open list) and `gone`
(object from PR number to the answer for a PR that left the list; absent means unreadable). A PR has `number`, `title`,
`author`, `labels`, `draft`, `head`, `head_ref`, `base`, `fork`, `fork_known` (the fork flag is an answer only when true),
`detail` (null: not read in this cycle, else an object `mergeable` (true, false or null: not computed) and `state`) and `checks`
(null: not read, else `runs` with `name`, `status`, `conclusion`, `title`, `summary`, `text` and `statuses` with `context`,
`state`, `description`). A gone entry has `merged`, `merge_commit` (null: unreadable), `merged_at`, `base`, `head`, `head_ref` and
`method_hint`.

`expected_events` has one object per step: `step`, `result` (`ok`, `crashed` or `error`), `error_kind` (empty,
`corrupt-state` or `other`), `retry_in_seconds` (a run-wide pause the cycle asks for), `events` (the events recorded by this
step in order, including those recorded before a crash), `printed` (the ordinals of the events written to stdout during this
step, in order and with repetitions: a crash after the print makes an event appear twice), `hints`, `state_after` and
`go_message`.

An event has `ordinal` (1-based position in the logical event log, gapless), `step`, `time`, `kind` (one of the 15 kinds of schema
v1), `repo`, `pr` (null for run-level events), `details` (exactly the contract keys of the kind) and `go` (an object `id`, `seq`:
the Go event id and journal sequence number, format-dependent; a port may use other ids as long as they are unique and stable
per transition, which `expect_unique_event_ids` demands).

`hints` is an object from repository to what the core tells the observer about the accepted state, only for repositories polled
in this step: `cycle` (the cycle counter), `detail_every` and `known` (the open PRs: `number`, `head`, `class`,
`class_candidate`, `detail_due`).

`state_after` (null after a crash) and `expected_state_view` (after the last step, from the state saved on disk) are the semantic
view of the accepted state: `cycle`, `repos_epoch`, `interval_seconds`, `repos`, `skipped`, `run_health` and `repo` (object from
repository to `baselined`, `since`, `last_ok`, `default_branch`, `prs`, `closed`, `restack_due`, `restack_done`, `health`).
`prs` maps the PR number to: `number`, `title`, `author`, `labels`, `draft`, `head`, `head_ref`, `base`, `fork`, `generation`,
`epochs` (`head`, `checks`, `mergeable`, `ready`, `base`), `class`, `class_candidate`, `pending_since`, `pending_names`,
`timeout_fired`, `mergeable`, `mergeable_state`, `conflicting`, `behind`, `mergeable_candidate` (null or `mergeable`, `behind`, `state`),
`detail_due` and `gone`. `closed` lists closed PRs with `number`, `generation`, `epochs`; `restack_due` lists `merged_pr`,
`merge_commit`, `old_parent`, `base`, `head_ref`, `head_fork`; `restack_done` lists the (child, merge) pairs already reported:
`child`, `merged_pr`, `merge_commit`. `health` and `run_health` have `failures`, `episode`, `error_open`, `last_code`, `stalled`,
`stall_episode`, `next_try`. The cache of conditional requests is not part of the view.

`state_view_ignore` lists the view fields a comparison must skip in this scenario (`detail_due` after a crash: it is a read hint
and a crash may leave it one cycle longer).

`expected_journal_ordinals` is the list of all ordinals in the journal at the end (`expect_unique_event_ids`: true, every
transition has a distinct id).

`status_probes` (usually empty) are reads of the liveness report after the last step with the watcher stopped:
`offset_seconds` after the last heartbeat and `expected` with `fresh`, `heartbeat_age_seconds`, `interval_seconds`, `lock_held` and
`repos` (`repo`, `last_ok_age_seconds`, `failures`, `open_error`).

## vectors

A pure function with many cases: `function` (the name of the semantic function) and `cases`, each with `name`, `input` and
`expected`. The shapes of `input` and `expected` belong to the function and are described in the `description` of the file.
Functions: `classify_checks`, `backoff_seconds`, `status_freshness_seconds`, `event_id`, `id_counter`, `workspace_id`,
`default_state_dir`, `event_json`, `restack_candidates`, `merged_due_head_fork`, `decode_pull_commits`,
`restack_with_hosting_branch_check`, `human_line`, `human_line_properties`, `parse_config`, `parse_remote`, `split_remote`,
`valid_host`, `resolve_repositories`, `prepare_flags`, `decode_pull_detail`, `observe_default_branch`, `encode_branch_segment`,
`branch_segment_valid`, `assess_rate_limit`, `rate_limited_until`, `next_page_from_link`, `parse_retry_after`,
`parse_rate_headers`, `watch_read_endpoint_allowed`.

## observer-session

The observer over a fake forge: one `Observe` call per step against the answers of the forge. Fields: `config` (`max_open_prs`,
`ignore_authors`, `ignore_labels`, `clock`), `steps` and `expected`.

A step has `note`, `world`, `new_process` (the observer starts afresh; the conditional-request store the core keeps survives),
`hints` (as above), `after_cycle_first` (the previous cycle ended: the rate limit is read and may arm a hold) and
`context_canceled`. The `world` is what the forge answers: `list_items` (`number`, `title`, `user`, `labels`, `draft`,
`head_sha`, `head_ref`, `base_ref`), `list_error`, `pull_details` (PR number to the body of `pulls/N`), `pull_detail_etags`,
`branch_tips` (branch to commit), `check_runs` and `commit_statuses` (commit to body), `commits`, `pull_commits`, `rate_limit`
(`known`, `limit`, `remaining`, `reset_in_seconds`) and `call_errors` (call name such as `detail/6` to an error with `kind`,
`code`, `message`, `retry_after_seconds`).

`expected` has per step: `step`, `observation` (as above, or null), `error` (null or `code`, `run_wide`, `retry_in_seconds`,
`canceled`), `calls` (the reads in order: `call`, `conditional`: whether a validator was sent), `diagnostics` (lines that are not
events) and `slow_after_cycle` (null or whether the interval is to be doubled).

## http-reader

The HTTP reader against a fake forge that serves `routes` by request URI (path and query without the API prefix). Only GET
exists. Fields: `host`, `routes`, `calls` and `expected`.

A route has `uri`, `status` (0 means 200), `headers`, `body`, `body_repeat` (`text`, `count`: a body of that many repetitions,
for the 1 MiB limit), `declared_content_length`, `etag`, `not_modified_on_matching_validator` (answer 304 without a Link header
when the request carries exactly this validator) and `echo_authorization_header` (the body echoes the Authorization header).
A call has `op` (`pull_detail`, `pull_list_page`, `pull_commits`, `branch`, `commit`, `check_runs`, `statuses`, `rate_limit`,
`list_open_pulls`), `repo`, `number`, `page`, `per_page`, `name`, `sha`, `if_none_match`, `max_open_prs`, `prior_list_state` (`none`,
`previous`: the state of the last list result, `per_page_only`), `prior_per_page`, `new_reader_before` (a new reader: in-memory
state is gone), `reader_already_halved`, `set_routes` (routes added or replaced before the call) and `note`.

`expected` has per call: `call`, `error` (null or `code`, `status`, `new_name`, `retry_after_seconds`, `rate_limit`,
`message_leaks_secret_or_body`, `message_at_most_200_bytes`, `canceled`), `response` (`status`, `not_modified`, `etag`, `link`, `body`,
`rate_limit` with `known`, `limit`, `remaining`, `reset_unix`), `list` (`items`, `numbers`, `unchanged`, `per_page`, `pages` with
`etag`, `has_link`, `items`, and `first_item`), `rate_info` and `requests` (every request of the call: `uri`, `if_none_match`).

## run-loop

The command: Params and a sequence of invocations against the same state directory, with a fake clock, a fake sleep (the
sleeps are recorded, not waited), no jitter and a fake forge. Fields: `config`, `invocations` and `expected`.

`config`: `repos`, `host`, `interval_seconds`, `detail_every`, `pending_timeout_seconds`, `max_open_prs`, `jitter_seconds`,
`start_time`. An invocation has `note`, `action` (`run` or `status`), `mode` (`once`, `stream`, `until-event`), `until_kinds`,
`timeout_seconds`, `max_idle_seconds`, `human`, `baseline`, `advance_seconds` (clock advance before the invocation), `worlds`
(`after_sleeps` and `world` as above: the forge answers after that many sleeps), `cancel_after_sleeps` (the interrupt arrives
after that many sleeps), `state_dir_kind` (empty: the private directory of the scenario; `relative`, `inside-work-tree`,
`wide-mode`), `state_file` (empty or `corrupt-state`), `reader_cannot_open` (no token: the reader cannot be opened), `status_json`
and `status_now_offset_seconds`.

`expected` has per invocation: `invocation`, `exit_code`, `error_mentions` (which of a fixed set of phrases the error text names),
`stdout_events` (`kind`, `repo`, `pr`, `time`, `details`, `go`), `stdout_lines` (human mode), `sleeps_seconds`, `status_fresh` and
`status_text`.

## What is not exported

Tests of files, locks, signals, processes and the Go sources are not scenarios; they are listed in the README of the crate
and are written by hand in W1 and W4 (journal truncation and corruption, state directory modes, lock, heartbeat file,
signals, a second watcher, platform builds, import and verb source checks, token and `gh` handling, per-request
deadlines).
