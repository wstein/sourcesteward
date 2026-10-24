# Progress and handoff

## Active work: Spec 039 tiered CI, WP-3 scheduled race tier (source candidate)

Branch `ci/039-wp3-race` adds `.github/workflows/race.yml` (nightly `-race` of all packages on the `current` lane, weekly
`minimum` lane; `schedule` and `workflow_dispatch` only, both jobs guarded to `refs/heads/main`), the `report` job with the only
`issues: write` and `scripts/race_report.py` (one open `race-failure` issue, at most 20 matched test names), tests and
`docs/development.md`. It stacks on WP-1 and adds the `race` input (boolean, default false) to `conformance.yml`, which only
`race.yml` sets. Cost is modelled, not measured: about 430 runner minutes a week (seven nightly runs and one weekly run of about
54 minutes each). Residual risk: a data race is found after the merge, up to a day later (a week for the `minimum` lane), only as a
`race-failure` issue; Ready no longer implies a race-clean head. Reviewed CLEAR; not yet merged or run on a schedule.

## Active work: Spec 029 tidy safety (source candidate, acceptance pending)

Explicit `tidy --contract 3` adds native Git reference proof, local tracking cleanup and
owned or explicitly completed legacy retirement. Default v1/v2 stays offline. Exact reviewed
dependencies, fresh-main reconciliations, safe disposable CLI conformance and remaining
gates are recorded in [integration evidence](../specs/029-tidy-dogfood-safety/verification-integration.md).
The feature remains `planned`: final integrated source review/checks, unavailable pinned
versions/platform lanes, separately approved candidate build/install and fresh real-repository
acceptance are distinct gates. No schema publication, activation or live cleanup is implied.

Tasks T001-T042 are ticked in [tasks](../specs/029-tidy-dogfood-safety/tasks.md) as implemented,
pending independent review; they close fully only after T046. Still open: T044 (pinned Git
2.46.0/2.56.0 and jj 0.45.1/0.46.0 on Linux and macOS lanes), T046 (independent review of the
exact integrated SHA, which differs from the reviewed fold tips), T047 (candidate build and
install, which needs separate owner approval) and T048/T049 (fresh real-repository acceptance).
Windows and Darwin remain unverified here.

## Active work: Spec 015 local restacking, first slice (source implemented, verification pending)

The read-only `stw restack plan` for native Git is implemented in source on `feat/015-restack-plan` (hidden command, unpublished
`schemas/v1/restack-plan.schema.json`): explicit branch, old parent and new parent, a plumbing replay in a private object
quarantine, `range-diff` pairing, `cherry` classification of the parent's commits, own-diff and identity evidence, predicted
conflicts, the overlap risk indicator, effects, and the inert lease push and resync texts, with a closed outcome table that never
reads as safe. See [the specification](../specs/015-local-restacking/spec.md), [tasks](../specs/015-local-restacking/tasks.md)
(20 of 24 done) and [verification](../specs/015-local-restacking/verification.md). Open:
the minimum/current Git lanes (T021), the independent review of the exact head (T023) and the reserved limitation (T024); T004 was not done by design. Status
stays `planned`; `restack apply`, jj and the push are later slices.

## Spec 041 stw tidy wizard (specification proposed, 2026-10-23)

[The specification](../specs/041-tidy-wizard/spec.md), its [plan](../specs/041-tidy-wizard/plan.md) and
[tasks](../specs/041-tidy-wizard/tasks.md) propose `stw tidy` as a workspace-wide wizard (overview of all repositories and owners, choice, stored plan set, `stw tidy apply <plan-id>`), derived defaults, the fix of the `changed-ownership` refusal after a branch switch (issue #174) and ownership as a view (a human sees everything; agents are narrowed). Documentation only: no code changed; open questions await the foreman. No build, release, tag or push of the default branch was run.

WP-1 (issue #174) is implemented in `internal/workspace` on a Draft pull request awaiting independent review: owned retirement now uses the identity part of `Verify` (`VerifyIdentity`) and a position rule (HEAD, attached or detached, must contain the recorded creation commit), and `TidyV3BranchLeg`/`ApplyTidyV3BranchLeg` add the dependent branch leg. Retirement now reuses the Spec 011 forge-free integration proofs (ancestor, patch-id, merge-noop, no-net-change, with `--integration-proof` and `--accept-whitespace-differences`; policy stored in the plan, plans stored without one keep the ancestry proof), so a rebase-merged head retires; this is an amendment of the Spec 041 text. The old `attached-current-head` plan-validation case was removed because attached heads are now valid. Evidence is in the pull request; T005 stays open until the independent review.

WP-2 (derivation) is implemented on a Draft pull request awaiting independent review: `internal/tidywizard/derive` derives workspace, member set, remote, base per repository (symbolic remote HEAD, jj trunk), caller label, protected worktrees and the known state directories, each as a `derived[]` entry (`name`, `value`, `source`, `overridden`). An ambiguous value is a typed result (`remote-ambiguous`, `base-ambiguous`, `base-missing` for an override naming a missing ref, `registration-record-missing` with its hint, `registration-record-ambiguous`) and is never guessed. State directories are checked (user-owned, real directory, mode exactly 0700, no symlink component, outside every repository), an unsafe store is skipped and reported, and creation records are looked up across all valid stores through the new read-only `workspace.TaskIDs` and `workspace.ReadTask`. `stw workspace plan|apply|handoff|retire-plan` now default `--state-dir` to `$STW_STATE_DIR` or `${XDG_STATE_HOME:-~/.local/state}/stw/workspace` (created 0700); an explicit `--state-dir` is unchanged, and two existing usage-test rows that relied on a required `--state-dir` were adjusted. Nothing is wired into `stw tidy`. Evidence: the tests in `internal/tidywizard/derive`, `internal/cli/workspace_statedir_test.go` and `internal/workspace/state_read_test.go`; `-race` run of the three packages before the first push. T006 to T008 ticked; T005 and the wizard wiring stay open.

WP-2b (owner scope) is implemented on a Draft pull request awaiting independent review: `internal/tidywizard/owner` derives the owner and scope of an item (task record, then path and branch convention such as `/tmp/whr*`, `codex/`, `claude/`, `claude-`, then a commit author hint that never makes an item `own`, then `unknown`; in agent mode only a task record makes an item `own`), detects the run mode from injected terminal facts and the environment (both streams terminals, no `STW_AGENT`, no `CLAUDECODE`/`CLAUDE_CODE_*`/`CODEX_*`/`AGY_*` variable present, even if empty, no `--mine`, which selects agent mode; `--by-owner` only narrows the view, so a human with it stays in human mode) and returns the mode evidence type (variable names only), filters the view and the plannable set (agent `--save` excludes `other` and `unknown`), writes `authorized_scopes` only on the human path, binds it into a content-addressed plan id, and reads the typed confirmation (confirmation word, the owner label or `unknown`, plus the first six plan-id characters) from `/dev/tty` only (a bare `y`, stdin and `--yes` are refused; no controlling terminal fails closed, also through `Confirm(DevTTY)` in a session without a terminal). Not delivered here and now explicit tasks: the `--as` absence test (T018a), the isatty source (T019a), recording the mode evidence in the plan, journal and `history` (T013b, T019b), `recently-active`/`no-work` (T010a) and the `scope-not-authorized` apply refusal (T013a). Nothing is wired into `stw tidy`. Evidence: the table tests in `internal/tidywizard/owner` (including a `Setsid` helper process for the missing terminal) and a `-race` run of `./internal/tidywizard/...`. T006a to T006d are ticked for the parts above only.

WP-3 (classification) is implemented on a Draft pull request stacked on the WP-2b one, awaiting independent review: `internal/tidywizard` classifies worktrees, branches, tracking refs and stale registrations as a pure function over a facts interface (`Source`), with verdict, reason, owner, scope and effects per candidate, sorted ids, an erroring or skipped repository reported without hiding the others, and the 4096-candidate and 512-byte bounds. Table fixtures cover each row of the behaviour table, `recently-active` and `no-work` (T010a); a stw lock with no record is `retain` `registration-record-missing` and only `lock-mismatch` is `decision` `locked-stw` (spec amended). Repositories run sequentially behind a one-line seam. Not done: T011a (adapter from the `internal/workspace` evidence, jj bookmarks, `internal/fanout`), so nothing is wired to `stw tidy` yet; T009a (view filtering and grouping) stays open.

## Spec 040 concurrency discipline (specification proposed, 2026-10-22)

[The specification](../specs/040-concurrency-discipline/spec.md), its [plan](../specs/040-concurrency-discipline/plan.md) and
[tasks](../specs/040-concurrency-discipline/tasks.md) propose one package, `internal/fanout`, as the only place with a `go` statement
(14 today in nine non-test files of seven packages), enforced by `scripts/check-architecture.py` with a shrinking allowlist and mutation tests, and tested
with `testing/synctest`. Documentation only: no code, script or allowlist is changed; the foreman decisions (at most one named exemption, the `sync.WaitGroup` and errgroup import ban, the Go AST check) are recorded in the
specification and open to owner override. No build, release, tag or push was run.

## Spec 035 stw watch (accepted, visible, 2026-10-22)

[The specification](../specs/035-stw-watch/spec.md), its [event contract](../specs/035-stw-watch/contracts/watch-events.md) and
[plan](../specs/035-stw-watch/plan.md) describe a read-only `stw watch` that prints pull request transitions (checks, merges,
`restack-needed` after a rebase merge) as JSON Lines, replacing the foreman's `watch2.sh`. Work packages A to E are implemented in
source and the command is listed in `stw --help`; `schemas/v1/watch-event.schema.json` is frozen and unpublished, and `stw watch` has a guide
([watch](watch.md)) and an end to end acceptance suite (US1 to US10, fake forge, no network). The owner accepted it on 2026-10-22 after a dogfood run on the
workspace (discovery of 7 repositories, silent baseline, `--status` fresh). The removal of `scripts/ci-wait.sh` is still open.

## Spec 036 stw pulls (implemented in source, review and acceptance pending, 2026-10-22)

[The specification](../specs/036-stw-pulls/spec.md), its [JSON contract](../specs/036-stw-pulls/contracts/pulls.md) and
[plan](../specs/036-stw-pulls/plan.md) describe a read-only, stateless `stw pulls` snapshot of all open pull requests of the workspace
repositories (bot PRs included, grouped by what needs action, with a `ready to merge` verdict) and an opt-in `--owner` mode that
discovers other repositories through one fixed `search/issues` read. Work packages 0 to C are implemented in source and the command is
listed in `stw --help`; `schemas/v1/pulls.schema.json` is frozen and unpublished, and `stw pulls` has a guide ([pulls](pulls.md)). The tests
use a fake forge only (no network, no build); independent review of the exact head and the owner's acceptance are pending.

## Spec 037 stw status online (work package E, command and output implemented in source, 2026-10-22)

`stw status` is online by default. `--offline` runs the unchanged Spec 033 code path (no discovery, no credential, no request; its text
and schema 1 JSON are pinned by goldens taken before the change), `--icons unicode|nerd|ascii` (accepted with `--offline`, where it only
changes the two repository markers) and `--timeout` (1s to 10m, a usage error with `--offline`) are new. `internal/statuscmd` holds the
terminal and icon decisions, the run (local status, workspace discovery for the origins, `statusremote.Collect` with `hosting.NewStatusReader`
as the default reader, `overview.Build`) and the schema 2 document builder; `internal/cli/status*.go` prints Needs action, Repositories
(the Spec 033 block plus a `remote:` line), Notices and the summary line, with one golden per icon set with and without colour (stripping the
colour leaves the same text). Exit 1 is an attempted request that failed, an unreadable repository or a failed run; a missing token, the
deadline and the items are exit 0. A test resolves the command of every item of all 25 categories against the cobra tree. Choices the spec
leaves open: the progress line on stderr counts requests (`reading remote: N requests`) because the number of repositories done is not
observable from `statusremote`; the Nerd Font code points are U+F057, U+F071, U+F058, U+F05A and U+F0AA (to verify in a Nerd Font); the
action section names `N` things of a grouped line with a noun per category; titles use the singular for a count of one. For work package F
(no schema file is touched here): `sources[]` entries are `{id, state, read, degraded, skipped}` for `local` and the six remote sources,
`degraded` counts failed reads and `state` is `ok`, `skipped`, `error` or `degraded`; `repos[].remote.sources.X` is `{state, error, data}`,
`data` null unless the state is `ok` and the contract's data table; `remote.state` is `ok`, `skipped` (with the reason of the first source),
`error` (every source failed) or `degraded`; `actions[].url` and the draft and run URLs are `https` or null; `options.icons` is always null;
the error code `hosting-api-unavailable` of a reader that could not be opened is an error code of every source; a local failure prints the
document with `error` set and empty lists, exit 1. The golden `internal/statuscmd/testdata/document.json` is a full valid document. Tests
use fakes only (no network, no credential, no build).

## Spec 037 stw status online (work package D, action model implemented in source, 2026-10-22)

[The specification](../specs/037-stw-status-overview/spec.md), its [JSON contract](../specs/037-stw-status-overview/contracts/status.md) and
[plan](../specs/037-stw-status-overview/plan.md) make `stw status` online and action-oriented. Work package D is the pure package
`internal/overview` (no I/O, no network, no clock): the 25 category rules, priorities, stable ids, deterministic ordering, human grouping with
the cap of 10 lines per priority, suggested commands built only from validated tokens with their `effect`, notices and summary counts. Its
tests are table-driven with one fixture per table row and never touch a network. Choices the spec leaves open: local findings use the
repository's local name in the id and every forge-derived finding (including `main-behind-origin`) its `OWNER/NAME`; tags sort by natural
version order (`v1.9.0` before `v1.10.0`) instead of a compiled release scheme; worktree items exist only when the status listed the
linked worktrees (`--worktrees`); a grouped line shows the command its items share or else the drill-down `stw pulls --repo OWNER/NAME`
(`gh release list -R OWNER/NAME` for drafts), and the omitted lines of a capped priority carry the read-only drill-down of the first omitted line's category and repository (or no command).
Text validators and the escape live in the standard-library-only `internal/untrusted` (moved from `watchcmd` and `cli`), so `internal/overview` is on the network-free list of the architecture check; its ids use the command validators (a component that fails them becomes `h-` and 12 hex digits of its SHA-256) and `stwsync.AttentionReasons()` is the single list of Spec 033 reasons that a test maps to rows.
The CLI, hosting, schema and the other work packages are not part of this package; the issue categories of Spec 038 are not either. For work package F (no schema file is touched here): the contract's `subject` object needs a key for the id part that is neither a number nor a tag (`Subject.Name`: fact kind, branch, path, source id), and `evidence` needs a key table per category: pull requests `group`, `reason`, `past_budget`; `release-draft` `drafts_visibility`, `assets`; `release-run-failed` `conclusion`, `released`; `main-red` `class`; `read-error` `source`, `code`; `repo-error` `code`; `local-changes` `staged`, `unstaged`; `local-operation` `operation` or `conflicts`; `local-behind` `ahead`, `behind`; `local-untracked`, `local-stash`, `branches-unmerged` one count each (`untracked`, `stash`, `not_integrated`); `gitlink-drift` and `gitlink-ahead` `relation`; the other categories have none. WP-A maps the verdict strings of `internal/pulls` to the group constants of this package; WP-B and WP-C choose the tag patterns and cap the unreleased tags at 5.

Work package A (pull requests, implemented in source) is `internal/statusremote/pulls.go`: it derives the repository of each workspace repository from its `origin` (a repository that was not read locally is skipped with `not-read-locally`, one without a usable origin with `not-github-origin`, one on another host with `other-host`, one whose path is not `OWNER/NAME` with `invalid-repo`; no request is made for a skipped repository) and runs one `pulls.Run` with the defaults of `stw pulls` over the reader it is given, so the verdict groups, the detail budget and the single bounded re-read of an unknown mergeability are the cycle's own. Each repository ends as `ok`, `error` with the cycle's code (a failed or rate-limit-skipped read; the rows that were read stay) or `skipped` with `deadline`, and the rows and group counts reach the action model unchanged (`PastBudget` only for rows cut by the budget). A test runs the real `stw pulls` command and this package on one fake forge and compares groups, counts and open numbers; no test needs a credential. `watchcmd.SplitRemote` exposes the host and path split that `ParseRemote` already used. The rows of an errored source still reach the action model (it turns the not-read rows into one `read-error` and no PR item), so work packages R and F must set the contract's `data` of the pulls source to null unless its state is `ok`. Not part of this package: the other sources, the orchestration with the overall deadline, the CLI and the schema.

Work package B (releases, tags, repository) is implemented in source: the typed reads `RepoInfo`, `ReleaseList` and `TagList` of the status reader in `internal/hosting` (`status_repo.go`, `status_releases.go`, `status_unreleased.go`), the draft visibility, the unreleased-tag rule over caller-given release tag patterns and `internal/statusremote/releases.go`, the per-repository source that returns the overview inputs and the states of the `repo`, `releases` and `tags` sources (see [hosting](hosting.md)). The default pattern `v*.*.*` is a semantic version with an optional `v` (`1.2.3` and `v1.2.3` are one version scheme; with both present they are listed by name). Only semantic-version tags are considered: calver and other product schemes of Spec 026 need their own later specification. The contract now says that `too-many-releases` also reports a tags listing past page 10, that `tags.latest` is a list of `{pattern, tag}`, that there is one entry per pattern and tag (`overview.ReleaseTag` carries `Pattern`, `Latest` and `Unreleased`; only `Unreleased` makes an item) and that the cap of 5 unreleased entries is filled in pattern order. `overview.Remote` carries the default branch, page and draft truncation of the `repo` and `releases` sources for the later output. Choices the spec leaves open: a source skipped by the overall deadline is `skipped` while a per-request timeout stays a `network` error, and a failed releases read leaves only the latest tag (nothing can be called unreleased without the releases). Tests use a fake forge and a fixture `gh` only; no build was made.

## Spec 037 stw status online (work package C, CI state implemented in source, 2026-10-22)

Work package C reads the CI state for the online `stw status`: `internal/statusremote` (`ReadCIMain`, `ReadReleaseProducts`, `ReadCIRelease`), the
typed `WorkflowRuns` read and decoders in `internal/hosting`, `stwsync.BehindOrigin` (local object lookup with `--no-lazy-fetch`; undecided on a
Git before 2.44) and `release.ReadWorktreeContracts` (the reader of `release check`); see [hosting](hosting.md). The default branch class reuses the
classifier of `stw watch`. `ReadCIRelease` adds the newest runs to the per pattern entries of the `tags` source (the latest tag of every pattern and at most 5 unreleased tags, N+5 reads, each with
the workflow of its product, `release.yml` by default, 404 skipped); an unusable contract is the error code `contract-invalid`. A red default
branch and a failed, cancelled or timed-out tag run reach the action model as red items (US5, tested through `overview.Build`). Tests use an
injected reader and temporary Git repositories only. `overview.Remote` carries neither the full list of runs (at most 20 per tag) nor the workflow
of each tag: WP-F and WP-R must take both from `CIRelease` (`CIReleaseTag.Runs`, `CIReleaseTag.Workflow`, `Product`), not from the overview. WP-R
passes `ReleaseProducts.Patterns()` to the releases and tags read and its tag entries to `ReadCIRelease`, and puts the result back by appending the
entries of `CIRelease.ReleaseTags()` to `Remote.Tags` (the overview merges them per tag); it never replaces `Remote.Tags`.

Work package R (orchestration, implemented in source) is `statusremote.Collect` (see [hosting](hosting.md#orchestration-of-the-sources-of-stw-status-spec-037-work-package-r)): it selects the GitHub repositories, opens the status client once (`Options.Open`; production passes `hosting.NewStatusReader`) and runs the sources under the overall deadline (`Options.Timeout`, default 90 s) with a gate in front of the client that stops every source at the first rate-limit evidence. `Result.Input(root, locals)` gives the `overview.Input` (per repository `Remote` with the pull requests, releases, drafts, tags, the default branch CI and the release runs appended to `Remote.Tags`, never replacing them, plus the deadline and the reset time of the rate limit); `RepoResult.Pulls` (open count and group counts) and `RepoResult.CIRelease` (runs and workflow of each release tag) carry what the overview does not, for WP-E and WP-F, and are nil unless their source ended `ok`, as are the `overview.Remote` fields of a source that did not. Choices the spec leaves open: all sources but the pull requests share one pool of four repositories, a worker reading one repository's sources in order (repo, releases, tags, `ci-main`, `ci-release`); `ci-main` and `ci-release` depend on the default branch and on the tags of the same repository, so when that source failed they are skipped as `not-attempted` (silent in the notices: the failure is reported once) and when it was skipped they carry the same skip (an unusable release contract is still `contract-invalid`); `offline` is a skip reason of every source (`overview.ReasonOffline`), a repository that cannot be read keeps its own skip reason also when offline or without a token; a client that cannot be opened for another reason than a missing token is the error `hosting-api-unavailable` (WP-F: add it to the error codes of the schema); the run needs no injected clock because it never sleeps: the tests run it in a `testing/synctest` bubble, where the fake clock proves that the 90 s deadline is the only time that passes and that a rate limit waits for nothing. The command, the output, `--timeout` and the schema are WP-E and WP-F. Tests use fake clients and temporary Git repositories only; no build was made.

Work package F (schema, contract tests and documentation, implemented in source) adds the unpublished [`schemas/v2/status.schema.json`](../schemas/v2/status.schema.json), byte-frozen in the freeze test and listed in `UNPUBLISHED_SCHEMAS` (the existing handling of `package-pages.py` already admits an unpublished file in the published `v2/` directory), the page [status](status.md) and the JSON contract entry. Decisions: `additionalProperties` is false everywhere, including the v1 repository members, which the schema repeats because schemas cannot reference each other; `subject` gains `name` (the id part that is neither a number nor a tag); every source of a repository is `{state, error, reason, data}`, where `reason` is the one addition to the contract so that a deadline, a missing workflow (`not-found`) and a source that was never attempted (`not-attempted`) can be told apart per source and not only when the whole repository was skipped; `data` is `null` unless the state is `ok`; `sources[]` has a fixed order (seven entries; Spec 038 adds `issues` and `viewer`, nine in all); `evidence` has fixed keys per category and priority and subject kind follow the category, checked by the schema and by a test that keeps it in step with `overview.Categories()`; the error code `hosting-api-unavailable` is in the enum. Tests validate the documents of every outcome the command can print and reject about sixty broken variants. The `viewer-unknown` skip reason, the `issues` and `viewer` sources and the issues line of Spec 038 follow when the issues work merges; they are an addition of optional keys and values to schema 2 and need no new schema version. After the full-range review: without a usable token the text is the `--offline` output plus the notices (no Needs action section, no remote lines, no second summary), `--timeout` is pinned by a test of the observable deadline, notices use the singular for a count of one, `CI` sources whose input source failed are `skipped` as `not-attempted` and the decisions are recorded in the [status contract](../specs/037-stw-status-overview/contracts/status.md#amendments-recorded-with-the-implementation). No build was made; nothing was run against a network.

Spec 038 landed before this command: the model has the issues source and the categories 26 to 28, and the schema 2 lists the three categories (priority, subject kind `issue`, evidence `reason`, `labels`, `assignees`, `author`). Work package G (the issues source in the run, the `issues:` line and the sources of the document) follows in the Spec 038 section below.

## Spec 038 stw issues (command, schema and action categories in source, 2026-10-22)

[The specification](../specs/038-stw-issues/spec.md), its [JSON contract](../specs/038-stw-issues/contracts/issues.md) and
[plan](../specs/038-stw-issues/plan.md) describe a read-only, stateless `stw issues` snapshot of the open issues of the workspace
repositories and the issue source of `stw status`. Work package 0 is implemented in source on `feat/038-issues-hosting`: in
`internal/hosting` the three fixed read shapes (open issue list, `user`, issue owner search), the guard mode `issues`, the issue list and
`user` in the `status` mode, the typed decoders and `NewIssuesReader` (see [hosting](hosting.md#guard-modes-of-the-read-only-transport)).
Work package B (owner search) is implemented in source on `feat/038-issues-owner`: `IssueSearchPage`, `DiscoverIssueOwner` and `DiscoverIssueOwners` discover the repository names of an owner through the one issue search shape with the decode, caps, `incomplete_results` precedence and separate search throttle of the pull request owner search, and the recorded fixtures are `issue_search_*.json`.

Work package A is implemented in source on `feat/038-issues-core`: the stateless cycle `internal/issues` (pages 1 to 10 by number, four repositories in parallel, rate-limit stop without sleeping, all-or-nothing repositories except `too-many-open-issues`, `issues-disabled` as a skip), the ordered priority rule table over all labels with ASCII case folding, the one `user` read per run and its outcomes, the local filters (`@me`, `@none`), the `--limit` cap and the counts. The tests use fake readers only (no network, no credentials, no build); the independent Opus review of the exact head is CLEAR (heads `410745e0` and, restacked onto `main`, `4b3f9d0b`; PR #160). Notes: repositories left unread by `--assignee @me` without a login are `skipped` with reason `viewer-unknown` and count as neither read nor failed (the contract and schema list it); an issue number repeated within one repository across pages is counted once, first occurrence wins; the cycle reuses `pulls.Dedupe`, which may become a shared helper later.

Work package C is implemented in source on `feat/038-issues-command`: `stw issues` (`internal/issuescmd`, wired in `internal/cli/issues.go`) with `--repo`, `--workspace [PATH]`, `--owner`, `--host`, `--label`, `--assignee` (`@me`, `@none`), `--author`, `--limit`, `--human` and `--json`; the unpublished, byte-frozen `schemas/v1/issues.schema.json` (listed in `UNPUBLISHED_SCHEMAS`, valid and broken fixtures; the viewer object gained a `reason` key and the repository reason enum `viewer-unknown`); and the guide [issues](issues.md). Output is one JSON document or the human table with a `run:` hint under red and yellow rows; exit 0 all read (a `no-identity` viewer skip and `issues-disabled` repositories included), 1 a repository, owner discovery or the viewer read failed or `--assignee @me` has no login, 2 usage. The end-to-end tests run the real guarded reader against an in-process forge.

Work package D, the part that does not need the status command, is implemented in `internal/overview`: the categories 26 to 28 (`issue-security` red, `issue-assigned` and `issue-bug` yellow; table, ids `issue-security:OWNER/NAME:N`, ordering, grouping with the `stw issues --repo OWNER/NAME` drill-down), the `issues` source whose failed read gives the `read-error` the hint `stw issues --repo OWNER/NAME` (a hint, not a command), the `Issue` rows (blue and bot issues are never items), `Input.Viewer` with the notices `no-identity` and `viewer-unknown`, and `issues-disabled` as a fact that neither raises a notice nor degrades the run. Work package G, the issues in `stw status`, is implemented in source on `feat/038-issues-in-status`, stacked on this work and on the status command (Spec 037): `statusremote.ReadIssues` runs the cycle of `stw issues` without filters (row cap 500) as a source of its own beside the pull requests, on a pool of 4 repositories, 15 s per request, inside the 90 s deadline and behind the shared rate-limit gate, with one `user` read per run (the `viewer`); `issues.RepoStatus.Counts` gives the counts per priority and repository; a read that fails while the context expires is the skip `deadline`, `issues-disabled` a skip that neither degrades nor raises a notice, more than ten pages a `truncated` datum and no failure. The document gains `remote.sources.issues` and the `issues` and `viewer` entries of `sources`, the schema 2 gained the three categories, `subject.kind` `issue`, the issue evidence and data with a new frozen digest, the notice of the viewer lists no repository (`[]`), and the text prints `issues: N open (R red, Y yellow)` after the `remote:` line. A test runs both commands over one forge and compares the counts and the items; `--offline` does not reach the new code and its goldens are unchanged. Decision (Q1): a skipped `viewer` degrades the run whatever the reason (`no-identity`, `no-token`, `deadline`), like a skipped source; `--offline` prints no document, and a run without a repository on GitHub skips the viewer as `not-github-origin` without a request. Independent review of the exact head and the owner's acceptance are pending.

Dogfood run against the real repositories (2026-10-24, `stw` built from `main` at `7d9a9a73`, read-only, `--workspace` of the umbrella with its seven GitHub repositories): `stw issues --json` read all seven repositories without error and listed 68 open issues (0 red, 3 yellow, 65 blue, 0 violet), the same per-repository counts as `gh issue list --state open`; the three yellow rows are the `bug`-labelled issues of crewbook, and the viewer was read (`ok`). `--assignee @me` returned none and `--assignee @none` all 68, as `gh` does; `--limit 10` on `--repo wstein/crewbook` kept 10 of its 27 rows and counted 17 as omitted; `--label bug` gave 3; a malformed `--repo` is exit 2, an unknown repository exit 1 with a `forbidden` entry, and a run outside a Git repository exit 2. `stw status` showed the same counts in its `issues:` lines and the three `bug` issues as one yellow item, with the sources `issues` and `viewer` `ok`. No difference from the contract was found. The run used about 100 requests of the core rate limit.

## Active work: Spec 032 stw sync (slice 1a source implemented, 2026-10-21)

[The specification](../specs/032-stw-sync/spec.md), [plan](../specs/032-stw-sync/plan.md) and [tasks](../specs/032-stw-sync/tasks.md) (4/20 done: T001 to T004) describe a manual one-step `stw sync [--plan]` and `stw sync restore` that replaces `git pull --rebase --prune` per repository (fast-forward only unless `--rebase`, dirty worktrees skipped unless `--autostash`, optional submodules). The independent design review is CLEAR at 3d8d1da (after NOT CLEAR at b0cc02d, F1-F8 resolved). Slice 1a (T002 to T004) is implemented in source on `feat/032-sync-plan`: `stw sync --plan|--dry-run [PATH] [--remote NAME] [--prune] [--json]` fetches once (remote-tracking refs only) and classifies every tracking branch (`internal/stwsync`), with the unpublished `schemas/v1/sync.schema.json`. Tests use temporary repositories with a bare remote and linked worktrees, including a before/after snapshot proving that a plan changes only remote-tracking refs. Not started: the real run, journal, prune reporting beyond the plan, `--rebase`, `--autostash`, `restore`, `--submodules`, acceptance evidence and a smoke script. No build, release artifact or real-repository run was made. Status stays `planned`.

## Active work: Spec 030 PR status (slice 1 source implemented, verification pending)

Slice 1 of [Spec 030](../specs/030-pr-status/spec.md) is implemented in source on `feat/030-pr-status-one-shot`: the schema 2 one-shot `stw hosting pr` / `gh jj pr` (`--json-version 2`, `--compact`, `--expect-head`; `--watch` is a usage error) and the head lookup `stw hosting prs SHA` / `gh jj prs` over the allowlisted `commits/SHA/pulls?per_page=30&page=1` GET. Source tests use HTTP fixtures only; no build, binary smoke, live API or keyring acceptance was run, and GitHub's association behaviour for the lookup is unverified. The feature stays `planned`. The watch, delivery arbiter and descendant verification remain a deferred slice with their NOT CLEAR history ([round 3](../specs/030-pr-status/round3-disposition.md)).

## Active work: Spec 027 inventory limits (implemented, verification pending)

The Git inventory reads large outputs incrementally, degrades to partial results with verbatim disclosures instead of `git-inventory-failed` for repositories of thousands of files or hundreds of tags, describes submodule and separate-Git-directory checkouts by their files and reports `tracked: true` for ordinary changes in gitlink-bearing superprojects. Schema v1 is byte-unchanged; the Spec 023 `stw repos` tests, goldens and texts that pinned the old limits were updated with it. Evidence and what was not run (hosted CI, binary acceptance, Git 2.46.0, jj) are in [verification](../specs/027-inventory-limits/verification.md); the feature stays `planned` until then.

## Active work: Spec 023 multi-repository inventory

`stw repos PATH... [--submodules] [--json]` inventories up to 16 Git/jj repositories and their direct Git submodules in one bounded read-only run ([PR #49](https://github.com/wstein/sourcesteward/pull/49), branch `codex/023-multi-repo-inventory`). Work items 1 to 5 are implemented in source; the feature stays `planned` in the status record until acceptance, and the Spec 027 inventory prerequisite lands before it ships. Two independent implementation reviews at bcb3324 were NOT CLEAR (1 critical, 6 high, 12 medium, 8 low); all findings were fixed test first and recorded in [analysis round 5](../specs/023-multi-repo-inventory/analysis.md#round-5-implementation-reviews-at-bcb3324), and the fixes have not been re-reviewed independently. Submodule confinement reuses the Spec 021 safety layer, so a submodule with alternates is refused. Evidence and what did not run (jj lanes, foreign-owner case, hosted CI, arm64/darwin as local evidence only) are in [verification](../specs/023-multi-repo-inventory/verification.md). Open maintainer decision: apply the usefulness assessment (fix single-repository inventory limits first, and/or cut `--submodules`) or ship as is; until then most real members end `partial` or `error` and the exit status is usually 1. No tag, release or publication is implied. Guide: [multi-repository inventory](repos.md).

## Active work: implemented command documentation refresh

README and Pages now expose all implemented command families and practical examples;
planned Specs 012–019 and GitButler integration remain separate. The public catalog and
Spec 007 US4 amendment distinguish local reads, stored plans, authorized local apply and
GitHub reads. Full local checks, generated-site/link/schema validation and responsive browser checks pass. [PR42](https://github.com/wstein/sourcesteward/pull/42) is open; hosted CI and merge/main publication remain pending. Evidence is tracked in
[Spec 007 verification](../specs/007-hugo-docs/verification.md).

## Earlier work: Spec 022 bounded go-gh transport

Spec 020 shared hosting inspection merged in [PR #40](https://github.com/wstein/sourcesteward/pull/40) at main ee86751. Spec 022 replaces only the API subprocess transport with pinned official go-gh/v2 HTTP middleware and owned bounded decoding. Native Git/jj operations, both CLI entry points and published schema/fixture bytes remain unchanged, except authentication refusal now reports `hosting-api-unavailable` with `boundary.network=false` (before any request). Bounded gh authentication remains required for explicit remote reads; local resolution stays offline.

Plan/tasks/independent analysis preceded implementation in separate transport and compatibility worktrees. Independent integrated source review of the migration found destination/lifecycle guards and concurrent host authentication sound, but the later independent review of b6edd4a was NOT CLEAR; the fixes (hermetic smoke, Content-Encoding, CLI tests, docs) are in follow-up [PR #43](https://github.com/wstein/sourcesteward/pull/43) (Refs #41), pending hosted CI and merge. See [post-merge review follow-up](../specs/022-bounded-go-gh/verification.md#post-merge-review-follow-up). [Spec 022 verification](../specs/022-bounded-go-gh/verification.md) tracks passing full local checks/race, both jj-version SDK fixtures, 28 binary cases and hosted CI. [PR 41](https://github.com/wstein/sourcesteward/pull/41) merged on 2026-10-16; all six final-head source/minimum/current checks passed. No live API/keyring acceptance, user-profile installation, release or deletion is implied.

## Earlier work: Spec 011 workspace and reference tidy

Spec 009 inventory and Spec 010 isolated workspace creation, handoff and retirement
preflight are merged. Spec 010 source and fixture follow-ups landed through PRs
#32–#35. Retirement preflight does not remove registrations, references or files.
The historical records below preserve their original handoff gates.

Delivery now proceeds in three separately reviewed work items:

1. Evidence-only tidy plans with explicit integration bases and retained reasons.
2. Authorized local Git branch/jj bookmark cleanup with durable recovery and stale checks.
3. Owned workspace retirement with separate registration and exact-path file effects.

Each follows plan → tasks → independent analysis → implement → review/test, with
native Git/jj conformance and a separate PR. WI1 implements evidence-only plans;
see [WI1 verification](../specs/011-workspace-reference-tidy/wi1-verification.md).
WI2 implements authorized local reference cleanup with [verification](../specs/011-workspace-reference-tidy/wi2-verification.md).
WI3 implements owned retirement; [verification](../specs/011-workspace-reference-tidy/wi3-verification.md) records local checks and [PR38 handoff](https://github.com/wstein/sourcesteward/pull/38). WI1 is merged as `81c5f07`; exact-head PR36
[CI 38809418076](https://github.com/wstein/sourcesteward/actions/runs/38809418076) passed source and both native Linux lanes. WI2 PR37 exact-head [CI 38812174447](https://github.com/wstein/sourcesteward/actions/runs/38812174447)
also passed source and both native lanes. WI3 source/security review and local full checks/race
plus 36 native CLI cases per lane pass; exact-head hosted acceptance remains required.

## Historical Spec 010 implementation handoff

Implemented native Git/jj task creation through stored plans and explicit authorization,
durable ownership/recovery journals, conservative retry, handoff and retirement preparation.
Plan → tasks → independent analysis preceded test-first implementation in separate core,
Git, jj, CLI/schema and conformance worktrees. No cleanup/deletion, transfer or agent launch.

Independent source review and final consistency analysis clear source `5e32515`. Full
`make check`, race tests and both pinned local native lanes pass: 10 inspection, six jj
inspection, 36 inventory and 45 workspace CLI cases per lane. See
[verification](../specs/010-isolated-task-workspaces/verification.md) for limits and exact evidence.
Scoped review and full checks also clear the post-CI unused-filter admission repair
`e5fbaf9`; active checkout filters and ambiguous sentinel-named drivers still refuse.
Spec 010 is implemented, not globally verified. PR handoff records hosted Linux minimum/current
CI at the final head; owner review/merge and live publication remain separate actions.
Next action: review [PR #32](https://github.com/wstein/sourcesteward/pull/32) and its exact-head checks.

Spec 009 design/implementation and Linux fixture repair were merged by the owner through
PRs #29/#30/#31; local main `e67ae90` passed hosted checks and automatic Pages publication.
The older Spec 009 snapshots below preserve their original pending gates and are historical.

## Historical Spec 009 implementation handoff

The implementation candidate adds bounded native Git/jj inventory and three-valued
queries. Plan/tasks/pre-implementation analysis preceded test-first implementation
in separate Git, jj, CLI/schema and conformance worktrees. M1–M4 organize 36 mandatory
native CLI cases; they are acceptance gates within this implementation work item.
See [verification](../specs/009-workspace-inventory/verification.md) for exact source,
review, local results and remaining hosted/owner gates. Later task/workspace mutations,
tidy, restacking, change editing and forge writes remain planned.

User merged specification PRs #12–#26. Primary main and active implementation worktrees
were updated onto origin/main 57497b1; completed historical worktrees also run current
main, with original branches/backups retained. New implementation PRs form a two-part
stack: analyzed design first, code/tests/evidence second. No owner merge, release or
live schema publication is performed by this handoff. Historical records follow.

## Previous milestone
008-jj-inspection implements native standalone, colocated and secondary workspace identity
with enforced read-only Linux/macOS sandboxing, isolated configuration and schema v2.
Published Git/version v1 contracts remain unchanged. Independently reviewed native source
949c082 passes both macOS jj versions and mandatory minimum/current Linux conformance at
beb3285 (Actions run38777527598), including capability/confinement/network-namespace proof.
The user merged policy PR #9 and implementation PR #10; main cf46592 has the same tree as
accepted beb3285. Final documentation analysis cleared95bb36d; a documentation-only follow-up
PR and CI handoff remain pending. See [Spec 008 evidence](../specs/008-jj-inspection/verification.md).

007-hugo-docs are merged to main through
[PR #7](https://github.com/wstein/sourcesteward/pull/7),
[PR #6](https://github.com/wstein/sourcesteward/pull/6) and
[PR #8](https://github.com/wstein/sourcesteward/pull/8). Their recorded acceptance covers
bounded ordinary-Git identity inspection, versioned JSON contracts and pinned Hugo guides/
command reference. This is merged-source status; deployment evidence remains in each feature's
verification record. Native jj inspection is implemented and merged through Spec 008;
its documentation follow-up handoff remains pending.

The Tier 1 policy amendment was merged by the user through PR #9: Git and jj receive equal support
commitments for explicitly supported capabilities. See [amendment evidence](tier1-policy.md).

004-site-themes is merged to main through [PR #5](https://github.com/wstein/sourcesteward/pull/5).
The README site link and system/light/dark Pages selector passed source, browser and
independent review checks. This records merged source; real OS dark preference/change
remains untested. See [theme evidence](../specs/004-site-themes/verification.md).

003-agent-hints is merged to main through [PR #4](https://github.com/wstein/sourcesteward/pull/4).
Root help links to task-specific agent guidance and llms.txt; version/JSON and error
contracts are preserved. Independent review cleared the implementation. See
[agent hints acceptance](agent-hints-verification.md).

002-wide-tie-brand is complete and published.
The wide tie asset set, navy/orange site, useful README badges and local make build target
pass source/browser checks and independent review. CI/Pages run 38751223604 passed;
live revision 63e5f9b matched the checked HTML/CSS, logo, favicons and social preview. See [branding evidence](../brand/verification.md).

001-project-foundation is complete. The public SourceSteward repository starts with an empty
commit and focused Conventional Commits for specifications, CLI, community, branding, site,
harness and CI. A durable local checkout is available. See [verification](verification.md).

## Decisions

- Repository/module: wstein/sourcesteward; product: SourceSteward; standalone short CLI: stw.
- Go 1.27.2 and Cobra v1.10.2; EUPL-1.2 original source/artwork; font licenses retained.
- Git and jj are Tier 1 peers with bounded native identity inspection implemented. Final
  Spec 008 implementation is merged; documentation follow-up is pending; GitButler and broader mutations require separate specifications.
- Wide navy tie, orange knot and white branch; local family fonts with preserved OFL notices.
- Static Pages with local assets and same-run exact-SHA checks; no CLI release automation.
- Weekly Dependabot, community standards, private security reporting and owner review.
- Spec Kit workflow and small repository-local harness; no agent scheduler or auto-merge.

## Measured state

make check, race tests and six real-process smoke cases passed. Independent source review
cleared fd4d652ce402621bf07b4d83ee5a5da7b70f4fd3 after two regression fixes. Browser inspection
passed at 1280×800 and 375×812 with no horizontal overflow. Initial Linux CI and Pages run
38743805161 succeeded; live revision and static bytes matched the checked source. The final
documentation-only evidence commit will go through the same CI/Pages gate.

## Historical next action — superseded
Open the documentation-only acceptance follow-up PR and confirm its exact-revision CI.
Policy PR #9 and implementation PR #10 were merged by the user; native source/effects and
final artifact analysis are independently cleared. No live schema publication or Pages
verification is claimed here. Broader status, mutation, recovery, repository migration and
GitButler integration require separate specifications.

## Historical LazyJJ-inspired roadmap proposal

The [roadmap](roadmap.md#proposed-workflow-features-inspired-by-lazyjj) now prioritizes
workspace/reference inventory, isolated task workspaces, checkpoints, stack inspection and
evidence-based tidy for native Git/jj. Restacking and forge coordination remain later
specifications. Ratings are planning recommendations; none of these capabilities is
implemented or acceptance-verified. Next feature action: specify a bounded read-only
workspace/reference inventory, then analyze its plan/tasks before implementation.

This documentation-only update was checked with `python3 scripts/check-docs.py` and
`git diff --check`; it does not establish runtime or independent-review evidence.

## Historical workflow specification batch

Specs 009–016 now capture all eight user-selected workflows with prioritized stories,
acceptance scenarios, requirements, measurable outcomes and author quality checklists.
The feature index marks each as planned; no implementation evidence is claimed.
Spec 010 owns the shared mutation contract; Spec 011 owns retirement execution, avoiding
a circular dependency for creation. Spec 009 is the active local Specify feature.
Next: review/plan Spec 009, generate tasks, then perform cross-artifact analysis before
implementation. No plans or tasks have been generated in this specification-only batch.

Specification-batch source checks: `make check` passed with the existing pinned Hugo
provided through `HUGO=/private/tmp/sourcesteward-hugo-docs/.tools/hugo` and cached Go
dependencies. The initial run stopped at the packaging test because this new worktree
had no `.tools/hugo`; rerunning with the existing executable passed all checks.
All eight drafts passed author structure/quality review. This is not independent
review, backend feature acceptance or cross-artifact spec/plan/task analysis.

### 009-workspace-inventory — Toolbelt use-case specification update

Workspace, State and Reference Inventory: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 012-stack-inspection — Toolbelt use-case specification update

Stack Inspection and Plan: reworked on 2026-10-17 for GitHub native stacked pull requests (read-only `stack show` and `stack plan`, opt-in pull request evidence, advisory plan; diffs withdrawn); full artifact set and author checks written.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Independent analysis and implementation remain pending.

### 013-checkpoint-resume — Toolbelt use-case specification update

Checkpoint, Resume and Selective Undo: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 014-conflict-diagnosis — Toolbelt use-case specification update

Conflict Diagnosis and Integration Preflight: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 017-work-transfer — Toolbelt use-case specification update

Selected Work Transfer: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 018-change-editing — Toolbelt use-case specification update

Amend, Fix Up and Split Changes: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### 019-upstream-sync — Toolbelt use-case specification update

Explicit Upstream Synchronization: draft scenarios, requirements and author quality checks updated.
Documentation links and whitespace checks passed before PR handoff. No runtime capability
or independent acceptance is claimed. Planning/tasks/analysis remain pending.

### Toolbelt specification batch verification

Integrated `make check` passed at `739abf5dced84f14987aa9d9b85fb16aa91fc269` with cached Go dependencies and the existing
pinned Hugo executable selected via `HUGO=/private/tmp/sourcesteward-hugo-docs/.tools/hugo`.
This covered Go tests/vet/module integrity, 32 Python tests, 12 theme cases, 112 authored
document checks, architecture boundaries and whitespace. All seven affected/new drafts
passed author structure, consecutive requirement numbering and checklist review; all
remain planned. This is source/documentation evidence, not new backend acceptance,
independent review or cross-artifact analysis.

Follow-up draft PRs #20–#26 target their predecessors, beginning on original draft PR #19.
Original PRs #12–#19 were preserved. Next bounded step remains planning Spec 009 and
generating tasks before cross-artifact analysis; no implementation was performed.

## Historical Spec 009 next action

Review the Spec 009 two-PR stack (analyzed design, then implementation and evidence)
and confirm hosted Linux minimum/current checks. Spec 009 is implemented with explicit
native qualifications; the earlier proposal/planning entries are historical and do
not override this action. Owner approval/merge and live schema publication are pending.
Do not start task workspace mutations or tidy from inventory evidence alone.

## Historical Spec 009 integration handoff

The owner merged Spec 009 design/implementation PRs #29/#30 into main `9c4f385`.
The conflict-only rebase preserved the full implementation tree. A Linux-only fixture
identity failure, also reproduced locally, is repaired in a focused test-only follow-up.
Independent review and full local checks pass; the next action is to review that repair
and confirm its exact hosted minimum/current checks. Earlier owner-merge-pending statements
are historical snapshots; no live schema publication or global verification is inferred.

## Spec 026 WI4 offline release source candidate

The isolated WI4 candidate implements streamed tag publication diffs, offline tag
planning and file verification, canonical record generation and the verify/record
CLI contracts. It preserves the Git-only tag boundary and uses native operation-pinned
jj committed reads for records; the no-follow writer has a reviewed Linux/Darwin
adapter design. See [WI4 evidence](../specs/026-release-process/verification-wi4.md)
for failing-first fixtures, native lane results, compiler-only Darwin checks and
remaining limits. New result schemas remain unpublished and Spec 026 remains planned.

Full source and race suites passed. Independent review of the exact final head,
after correcting the first review’s target binding, tag observation and isolated
regression coverage findings, remains pending. The image repository clarification
received independent assessment; it adds no network or publication effects.
No product build, Hugo packaging, release, publication, owner tag or consumer activation
was performed. Normal hosted workflows and later release operations retain their
separate owner authorization requirements.

## Spec 026 WI5 online release source candidate

The isolated WI5 source candidate adds bounded fixed release GETs, online gate
selection and fake-process attestation verification, with offline zero-request
sentinels. The [WI5 evidence](../specs/026-release-process/verification-wi5.md)
records executed source checks and the unverified real-gh/Darwin boundaries.
Independent source review and the no-write descriptor-input design assessment
remain pending; T026 acceptance and global Spec 026 completion are not claimed.
No actual credentials, external release operation or product/documentation build
was used. Later effects retain their separate owner authorization requirements.

## Spec 026 WI6 dispatch source candidate

The isolated WI6 candidate implements a stateless canonical plan identity,
one fixed POST transport and `release dispatch` plan/apply integration. It
recomputes native and online build gates, refuses stale ids and local/remote
main disagreement, and exposes attempted writes and HTTP status without remote
bodies. Named products, fetch disclosures and native Git/jj identities remain
explicit. The result schema is unpublished. [WI6 evidence](../specs/026-release-process/verification-wi6.md)
records failing-first fixtures, source guard mutants, native state sentinels and
remaining verification. T028/T029 have exact-source clearance; final T030 checks
and independent review remain pending. Spec 026 stays planned. No actual
workflow, build, publication, tag, release or activation was performed.

## Spec 026 WI7 build checkout view source candidate

`stw release build` now compares the tracked tree through a frozen private view of the
checkout metadata, captured before the first project invocation and revalidated after
the build, with a supervised native process group and a trusted nested-status route
entered from `cmd/stw`. This is source only and pending independent review: T031 and
T034 stay unchecked and Spec 026 stays planned. The
[verification note](../specs/026-release-process/verification-checkout-view.md) lists
the mutation evidence and what was not verified: the Darwin observer has only been
compiled and vetted, `internal/release` does not compile for Windows (also true before
this work), and no real checkout, build, workflow or release was run. The earlier
dead-code state of the view, which had no production caller, is closed by this wiring.

## Decision-file schema registration

The Pages source validator recognizes the exact candidate `v3/tidy-decisions.schema.json` as
unpublished, like the report schema; the unexpected or linked source check is unchanged.

## PR 104 unpublished report schema CI correction

The Pages source validator now recognizes the exact candidate
`v3/tidy-report.schema.json` as unpublished. The public schema allowlist is
unchanged. The failing-first regression reproduced the missing registration;
source tests cover exclusion of both v3 schemas, invalid JSON/UTF-8, linked
files/directories, unlisted sources and byte-identical published schema copies.
All 165 source Python tests passed, excluding the one test that invokes the
actual Hugo/Go documentation build. Documentation and architecture source
checks and whitespace checks passed. The theme tests could not run locally
because Node is unavailable. No documentation/product build was run; hosted
checks and independent exact-head review remain pending.

## Spec 031 restore and history source candidate

`stw tidy restore` plans, and with `--apply --authorize` carries out, the restore of branches that
a contract 3 local-reference apply deleted. `stw tidy restore --list` and `stw tidy history` list
the stored journals. The plan is read-only; apply creates only local branches by compare-and-swap
with `git update-ref --no-deref`, never overwrites or follows a ref, writes an intent record before
and an outcome record after each creation in the `tidy-v3-restores` namespace of the private
store, and keeps the recovery refs. Only the tip commit and its history of this clone come back;
reflog tips and worktree registrations do not. Journals now also record `branch.NAME.*` and the reflog
tip before a deletion (Spec 031 T045), the plan shows which keys can be reapplied and apply reapplies
`remote`, `merge`, `rebase` and `pushRemote` when the remote still exists; older journals restore as
before. The tests build their
repositories with a real tidy v3 apply (see [the evidence](../specs/031-tidy-integration-proofs/verification.md)).
The result schemas `schemas/v3/tidy-restore.schema.json` and `schemas/v3/tidy-history.schema.json`
are unpublished candidates registered in the Pages source validator. This is source only: the
acceptance on the dogfood data set (T052) follows the owner-authorized cleanup apply and is not
done, so Spec 031 stays planned. No product or documentation build, release, tag or `apply` on a
real repository was run for this work.

## Spec 039 WP-1 measurements and T1 source candidate

Source only: `ci.yml` no longer runs `-race` on pull requests or pushes, `scripts/ci_changes.py affected` and
`--files-from` select the affected packages (fail closed to `all`), and the `check` job tests only those
(none for a documentation-only change, which also skips the sandbox install). `scripts/ci-test-reads.json` declares the
packages whose tests read or build other packages' files, or read prose of the repository, without importing them (for example
`internal/release` reads `internal/cli` sources and `internal/releaseworkflow` reads `README.md`); prose that a test reads is not
docs-only and runs just its readers; root-level `.go` files select the tests that build `./cmd/stw`. A scan test keeps the file
complete and keeps build entries apart from file-read entries. Nothing was triggered on GitHub, built or released; the GitHub
figures are read only (`gh api`, `gh run list`, job logs). The local figures come from one 10-core arm64 VM with the real
sandbox on the tree of GitHub `main` 7c696cee (23 packages by `go list ./...`, 20 with tests), not from the CI runner, so they show
shares and ratios only. Every figure not listed here stays unmeasured.

**Plain versus race, per package** (`go test -json -count=1 ./...`, then with `-race`; seconds plain / race, packages of
5 s or more): workspace 698 / 946, watch 170 / 185, release 131 / 181, contracts 129 / 234, inspection 110 / 150,
stwsync 71 / 96, cli 27 / 42, releaseworkflow 22 / 29, acceptance 14 / 16, restack 10 / 12, watchcmd 7 / 8, hosting
5 / 20. Package sums 1394 s and 1928 s (1.38); wall 11 min 49 s and 16 min 4 s (1.36). An earlier run on the older
tree of the local clone gave 1675 s and 1848 s (1.10) and 13 min 46 s and 16 min 27 s (1.20), with other work on the
machine, so the local race overhead is 10 to 38 percent and not stable. The 80.7 runner minutes under `-race` in CI therefore
are mostly test and sandbox work; the saving of removing `-race` is smaller than that figure. The plain-versus-race ratio of
the `current` CI shards cannot be read from existing runs (no plain run exists); the first plain run after this change
measures it. The second race run is the local stand-in for the nightly all-package run (16 min 4 s wall); its CI cost stays
unmeasured.

**Sandbox install, decision gate D8** (job logs of 136 of the 408 successful `check` and shard jobs of the 60 runs, every
third job; times from log timestamps): the whole step takes median 105 s (8 to 222, p90 129). `apt-get update` takes median
5 s (3 to 82, p90 10), fetching and unpacking the packages (781 kB, index 12.5 MB) median 7 s (3 to 69). The rest is
`needrestart` after the install: in 126 of 136 steps it tries to restart `php8.3-fpm.service` and waits for the 90 s unit
timeout (median 92 s, p90 94 s). An apt cache or an install-once artifact could save at most the download part (about 7 s
median, up to 70 s on a slow mirror) and not the 90 s. Suppressing the restart (for example with `NEEDRESTART_MODE=a`) is a
WP-4 proposal only: its saving is unmeasured and needs an owner-approved workflow run. No sandbox install step was changed.

**Replay of `affected` over the 49 PR heads of the last 60 runs** (merge base from the commit's pull request, or the merge base
with `main` for those without one; graph from each head, with the declared reads): 5 `docs-only`, 8 `packages`, 36 `all`.
The `--files-from` classification equals the git classification for all 49. `all` comes from changed paths outside prose and
packages: non-prose spec files in 32 heads, `scripts/` in 26, `schemas/` in 3 and `.github/` in 2 (a head can have several);
this stays fail closed and is revisited after more data. 27 heads change prose that a test reads (README, agent guide or
declared spec prose); none of them was `docs-only`. Of the 8 `packages` heads 6 contain `internal/workspace` (through
`hosting` or `inspection`); the affected sets hold 12 to 17 of 19 to 23 packages, the other one 3 of 20. No head failed in CI
(19 success, 29 cancelled, 1 not finished), so the replay cannot show a missed package.

**T1 duration and target D3**: no CI figure. At local speed all packages take 11 min 49 s, of which `internal/workspace` takes
698 s; without it the longest package is `watch` at 170 s. With the CI floor of the specification (about 55 s of checks, 105 s
sandbox) a T1 without `internal/workspace` is about 5.5 min at local test speed, and a T1 with it is about 14 min. On the current tree
a change to `hosting`, `inspection` or `workspace` (3 of 23 packages) selects `internal/workspace`; any other package change
selects 1 to 15 packages. `check` leaves `internal/workspace` to the conformance shards when they run for the pull request
(`CONFORMANCE_TIER`, as before) and tests it plain otherwise, so the tests of every package that changed or depends on a change still run, except `internal/workspace` when the shards cover it;
the prose, cross-package and build dependencies are covered by the declared entries, and a dependency no test names and the scan
cannot see (a read through an unusual helper) is not covered, which T3 on `main` catches. The 5 minute goal of D3 is therefore
met only for pull requests whose shards run or whose change does not reach the workspace package. The owner and the
specification decide the rest with the first CI figures.
