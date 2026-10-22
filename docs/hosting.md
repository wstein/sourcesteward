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
