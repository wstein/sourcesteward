# GitHub inspection and the gh jj extension

SourceSteward provides shared read-only hosting operations through `stw hosting`
and the optional `gh jj` entry point. `stw hosting` supports native Git and jj;
`gh jj` requires a jj-owned workspace, including colocated repositories. Both use
the same Go operations and [hosting JSON contract](../schemas/v1/hosting.schema.json).
Jujutsu is a supported runtime CLI dependency, not a linked Rust library.

```sh
stw hosting repo PATH --json
stw hosting pr 42 PATH --repo owner/project --json
stw hosting ci 42 PATH --repo owner/project --json
stw hosting pr 42 PATH --repo owner/project --json --json-version 2 --expect-head FULL_SHA
stw hosting pr 42 PATH --repo owner/project --compact
stw hosting prs FULL_SHA PATH --repo owner/project --json

gh jj repo JJ_PATH --json
gh jj pr 42 JJ_PATH --repo owner/project --json
gh jj ci 42 JJ_PATH --repo owner/project --json
```

Paths default to the current directory. `repo` reads local identity and active
native fetch/push remotes without gh or network. It reports a local association,
not confirmation that the hosted repository exists. Distinct candidates refuse
selection unless `--remote NAME` or `--repo OWNER/NAME` selects explicitly. Combined
selectors must agree. There is no assumption about origin, main or master.
Well-formed remotes for other hosts are ignored. jj's native interface lists
active remotes and omits empty sections and non-UTF-8 remote names; this is not a
raw configuration inventory. Unsupported visible records refuse native discovery.

The host defaults to github.com; GitHub Enterprise requires explicit `--host HOST`.
Remote text never selects an authentication host. Explicit `--repo` may target a
repository absent from local remotes after ownership/native discovery succeeds.
Fork head/base repositories remain separate identities. PR numbers are explicit;
there is no automatic branch/bookmark-to-PR inference in this feature.

PR/CI reads require GitHub CLI 2.102.0 or newer and the caller's existing
host-specific authentication. SourceSteward retrieves a token with bounded
`gh auth token --hostname HOST` execution outside the inspected repository, then
uses the pinned official go-gh/v2 HTTP client for fixed HTTPS GET requests. No editor, pager, git credential helper, fetch,
push, API mutation or CI rerun is requested. Authentication configuration linked
inside the inspected target is refused. Tokens and raw subprocess/API error bodies
are not included in diagnostics. Native jj retains enforced read-only sandboxing,
operation pinning and stored-unsnapshotted working-copy qualifications.

CI samples checks and commit statuses at the PR's exact head SHA, then rechecks
head/base identities. Missing permissions, malformed or incomplete pages, moved
heads and exhausted bounds produce incomplete error results. No aggregate passing
CI, required-check satisfaction, review approval or merge eligibility is inferred.
Evidence is sampled rather than transactional. API text reflects the SDK's JSON sanitization and is not a byte-for-byte raw HTTP
capture. Limits are 30 seconds total; 16 KiB for each authentication stdout/stderr
buffer; 1 MiB for each raw HTTP body, sanitized HTTP body, version-subprocess
buffer and final JSON result; 10 pages per collection; and 1000 combined CI records.
HTTP body limits include error responses, with at most one extra detection byte.
Redirects and compressed responses are refused. Automatic decompression, SDK
cache/logging, repository/API-host overrides and socket configuration are disabled.
GitHub uses api.github.com; ghe.com tenancy uses api.HOST; other Enterprise hosts
use HOST/api/v3. Caller-configured HTTP proxies remain trusted caller infrastructure.
Live Enterprise and real-keyring acceptance are separate from offline fixtures. Native inspection retains its tighter bounds.

All data commands offer human and `--json` output. Success exits 0, operational
failure exits 1 with a structured error observation, and usage errors exit 2.
Pre-dispatch cancellation may return only the process diagnostic, and writer/process failures may leave partial output. Consumers must check both exit status and JSON validity. Network-attempt and
completeness fields distinguish early local failures from remote observations.

## PR status (schema 2) and lookup by head SHA

`stw hosting pr` keeps its detailed human output and its v1 JSON byte for byte. Three
opt-ins select the one-shot status observation, which makes exactly one numbered PR GET:
`--json --json-version 2` ([hosting-pr.schema.json](../schemas/v2/hosting-pr.schema.json)),
`--compact` (one escaped line) and `--expect-head FULL_SHA`. They report `lifecycle`
(`open`, `closed-unmerged`, `merged`), `merged`, `merged_at`, `closed_at`, GitHub's raw
`merge_commit_sha` and `result_sha`. A test-merge SHA on an open PR is never a result;
the result is set only for a merged PR and is method-dependent, not proof that the head
or the base tip contains the work. Missing or contradictory evidence is an error, never
a guessed state. `--expect-head` takes only a full 40-character lowercase SHA and compares
the PR head, never `result_sha`; a mismatch exits 1 like any failure and is
distinguishable by `observation.stop` `expectation-mismatch` / diagnostic
`hosting-head-mismatch`, even for a merged PR. A match says nothing about review, CI or
ancestry. `--watch` is not available (usage error, exit 2): use `scripts/ci-wait.sh` for
long waits (or the planned `stw watch --until-event`, see [watch](watch.md)).

`stw hosting prs SHA` (and `gh jj prs SHA`) lists the PRs GitHub associates with a commit
through one `GET commits/SHA/pulls?per_page=30&page=1` (schema
[hosting-prs.schema.json](../schemas/v2/hosting-prs.schema.json)): zero, one or many, each
with number, state, draft, base, head, `merged`, merge commit and `head_match` (the PR's
current head equals the SHA). It never selects one PR, follows no further page (exactly 30
entries set `truncated` and the limitation `lookup-single-page`) and an oversize body is an
error with no partial list. It answers "which PRs are associated with this commit", not
"whose branch contains it"; GitHub's association rules were not verified against the live
API, which the first authorized live call must do.

Network policy: GET only, fixed allowlist, no redirects or compression. HTTP 401, 403
(including rate limits), 404, 422, 429 and any other non-2xx, and transport failures, stop
at once with exit 1 and a safe message: no retry, no `Retry-After` or backoff sleep, and
no token or API body in output. Rate-limit headers are not read; callers that poll must space
their calls.

## Guard modes of the read-only transport

Every request passes a guard that admits only fixed GET shapes, each produced by one generator and matched
exactly: no other parameter, order, value or escape case. The guard has five modes, one per kind of reader, and a
mode admits nothing outside its set. The one-shot shapes (pull request, check runs, statuses, releases, tags,
workflow runs, the tidy listings and the repository itself) are in every mode.

| Mode | Reader | Beyond the one-shot shapes |
| --- | --- | --- |
| `one-shot` | `stw hosting`, `gh jj` | nothing |
| `watch` | `stw watch` | the watch shapes (open pull request list, commits, branches, `rate_limit`) |
| `pulls` | `stw pulls` | the watch shapes and the pull request owner search (`is:pr`) |
| `status` | `stw status` online ([Spec 037](../specs/037-stw-status-overview/spec.md)) | the watch shapes (the open pull request list, commits, branches, `rate_limit`), the open issue list and `user` (Spec 038); no search. The default branch, release, tag, workflow run and repository reads it makes are one-shot shapes. The reader's per-request timeout is 15 s |
| `issues` | `stw issues` ([Spec 038](../specs/038-stw-issues/spec.md), see [issues](issues.md)) | the open issue list `repos/OWNER/NAME/issues?state=open&per_page=100&sort=created&direction=asc&page=N` (N 1 to 10), `user` (no query) and the issue owner search (`is:issue`); no watch shapes and no pull request search |

The two owner searches refuse each other: `pulls` admits only the `is:pr` search and `issues` only the `is:issue`
search. The issue owner search is read only through `IssueSearchPage` / `DiscoverIssueOwner(s)` and only to discover
repository names (at most 10 pages, 1000 results, `search-incomplete` and `search-too-many` as for pull requests); issue data is
never taken from it. A page of the issue list holds at most 100 entries and at most 1 MiB; entries with a `pull_request` key are
dropped, a 410 answer means the repository has issues disabled, and the `Link` header only signals that a further
page exists: its URL is never requested, the reader asks for the next page number itself. The `user` read answers who the token is,
once per run and without retry: a 403 or 404 without rate-limit evidence is the user-lookup skip (`no-identity`, exit 0), a 403 or
429 with it is `rate-limited`, anything else an error. The `status` mode admits the list and `user` but neither search, so the issue
source of `stw status` cannot discover repositories.

## Repository, release and tag reads of `stw status`

The online `stw status` ([Spec 037](../specs/037-stw-status-overview/spec.md)) reads `repos/OWNER/NAME`, `releases` and `tags` through typed decoders of the
status reader (`RepoInfo`, `ReleaseList`, `TagList`). They use request shapes that every guard mode already admits, so the guard gains no shape;
`ReleaseEntry` and the release verification are unchanged. Releases and tags are read from `per_page=100&page=1` on; the page of each request is the typed
number, and the `Link` `rel="next"` header only says whether another page exists (it must name the following page; its URL is never requested). A next page
after page 10 is the error `too-many-releases` (also for tags), a page over 1 MiB `page-too-large`, and a duplicate JSON key or a missing required field
`malformed-response`.

GitHub shows release drafts only to a token whose user has write access and does not say that it withheld any. `permissions` of the repository read give the
visibility: `push`, `maintain` or `admin` true is `write-access`, all three reported false `no-write-access`, anything less `unknown`; without
`write-access` no draft is reported, so "0 drafts" is never stated for a token that cannot see them. A fine-grained token with write access but without the
releases scope can still withhold drafts.

A tag is **unreleased** per release tag pattern (`v*.*.*`, a semantic version with an optional `v`, when the caller gives none) when its version is higher than that of the latest published release and no
release, draft or published, carries its name (without a published release only the highest tag counts). `*` matches any text and the text between the
pattern's literal head and tail must be a semantic version; other tags are ignored, prereleases order as in semantic versioning, and at most 5 unreleased tags are listed, pattern by pattern in the declared order. The latest tag of each pattern is reported as well, with one entry per pattern and tag.

## CI state for `stw status` (Spec 037, work package C)

The online `stw status` reads the CI state of each GitHub repository through the status reader (guard mode `status`, 15 s per
request, same bounds as `stw watch`). It adds no request shape. The default branch is read through `branches/DEFAULT` and the
existing check-run and commit-status reads, classified `none`, `pending`, `green` or `red` by the same classifier as `stw watch` and
`stw pulls`. A release run is read through the fixed Spec 026 shape
`actions/workflows/FILE/runs?head_sha=SHA&per_page=100&page=1`, page 1 only, by the typed `WorkflowRuns` read and the decoder
`DecodeWorkflowRuns`; the newest runs come first by creation time and at most 20 are kept per tag.

The reads live in `internal/statusremote`. `ci-release` does not choose tags: it takes the entries of the `tags` source (one per release
tag pattern and tag, the latest of the pattern or unreleased) and adds the newest runs to the latest entry of every pattern and to at most 5 unreleased ones, so that a repository
makes N+5 reads of page 1 for N patterns and no product hides a failed run. Each entry uses the `ci.release_workflow` (file name only) of the product that declares its
pattern, and `release.yml` for a pattern no contract declares; a 404 for the default `release.yml` of an undeclared pattern is not applicable (the entry is left out; the source is `skipped`, `not-found`, only when no entry has a workflow, and then neither degrades the run nor raises a notice), and a 404 for a declared workflow ends the source as `skipped`, reason `not-found`, with a notice; the runs already read are kept.
The contracts are read through the reader of `release check` (`release.ReadWorktreeContracts`: no symlink is followed, a file that
changes while it is opened is refused). A repository without `.stw` uses the defaults; a contract that exists but cannot be used ends the
source with the error code `contract-invalid` before any request, and the defaults are not a substitute. Only semver and pattern tag
grammars give a release tag pattern. A run of another commit than the tag's is ignored; the page of a run is kept only when it is an
https page of github.com under the repository, and an unknown status or conclusion (or a conclusion of a run that has not completed) is
information only. A failure, cancelled or timed-out newest run is a red item of the action model; a run still in progress is not.

`behind_origin` is decided in the local object store: `stwsync.BehindOrigin` looks the head of the default branch up and is true when the
commit is absent or is not an ancestor of the local branch tip. Every Git call passes `--no-lazy-fetch`, so a partial clone does not fetch
the commit on demand; a Git older than 2.44 refuses the option (exit 129) and the answer stays undecided, as it does without a clone or a
local branch of that name.

## Orchestration of the sources of `stw status` (Spec 037, work package R)

`statusremote.Collect` runs the remote sources of a workspace under one overall deadline (`Options.Timeout`, 90 s by default; `--timeout`
is wired by the command). The pull request source reads through the pulls cycle's own pool of four repositories; all other sources share one pool of four, a worker
reading one repository's `repo`, `releases`, `tags`, `ci-main` and `ci-release` in this order (`ci-main` needs the default branch and
`ci-release` the tags of the same repository; when that source failed they are skipped as `not-attempted`, which the notices do not repeat,
and when it was skipped they carry the same skip). All reads pass one gate in front of the client: no request is sent once the context is done, and after the first
rate-limit evidence of any source no request of any source is sent; the unread repositories end `error` with the code `rate-limited`,
and nothing sleeps, retries or waits for the reset.

What is attempted decides the outcome. `--offline`, a repository whose origin is not a GitHub repository and a run in which no
repository can be read make no request and look up no credential. A token is found while the client is opened, before any request: the
authentication error of the hosting layer is the skip `no-token`; any other failure to open the client (no `gh`, an old `gh`) is the error
`hosting-api-unavailable` (an attempt that failed), and only its code is reported, never the text of an error. A deadline that passes
cancels the requests in flight and skips the unfinished sources with the reason `deadline`, keeping what the finished ones read. The data of
a source is empty unless the source ended `ok`, so a failed pull request read leaves no rows to the action model, which then reports one
read error.

## Optional local extension installation

After reviewing/building this source:

```sh
make extension
gh extension install ./bin/gh-jj
gh jj --help
```

The directory and executable are both named gh-jj, as required by gh's local
extension interface. `stw` need not be separately installed for this entry point.
The repository is still named sourcesteward; `gh extension install
wstein/sourcesteward` is not the documented installation route. An existing
`gh jj` extension occupies the same command name; choose your preferred
implementation explicitly. SourceSteward never replaces an installed extension.
Source tests install only in disposable private configuration directories.

Release distribution, ARM64 release artifacts and automatic installation remain
separate work. Hosting mutations and stacked PR publication remain [Spec 016](../specs/016-stacked-pr-coordination/spec.md).

Authentication failure is detected in preflight, before any request, and reports `hosting-api-unavailable` with `boundary.network=false` (previously `hosting-pr-failed`, `network=true`). HTTP read failures use new messages.
