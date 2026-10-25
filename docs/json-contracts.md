# CLI JSON contracts

Use `stw version --json`, `stw inspect [PATH] --json` or `stw inventory [PATH] --json` for structured results.
`schema_version` identifies the contract independently of the program version.

| Command | Authoritative schema | Stable publication URL |
| --- | --- | --- |
| `stw tidy plan/apply --json` (owned retirement) | [tidy.schema.json](../schemas/v2/tidy.schema.json) | [Retirement schema 2](https://wstein.github.io/sourcesteward/schemas/v2/tidy.schema.json) |
| `stw tidy plan --json` and `stw tidy --json` (workspace overview, no expert flag) | [tidy-overview.schema.json](../schemas/v2/tidy-overview.schema.json) | Unpublished source schema, not on Pages |
| `stw tidy plan [PATH] --base REF --json` (reference plan) | [tidy.schema.json](../schemas/v1/tidy.schema.json) | [Tidy schema 1](https://wstein.github.io/sourcesteward/schemas/v1/tidy.schema.json) |
| `stw version --json` | [version.schema.json](../schemas/v1/version.schema.json) | [Version schema 1](https://wstein.github.io/sourcesteward/schemas/v1/version.schema.json) |
| `stw inspect --json` (Git and unselected/ambiguous ownership) | [inspect.schema.json](../schemas/v1/inspect.schema.json) | [Inspection schema 1](https://wstein.github.io/sourcesteward/schemas/v1/inspect.schema.json) |
| `stw inspect --json` (selected native jj) | [inspect.schema.json](../schemas/v2/inspect.schema.json) | [Native jj inspection schema 2](https://wstein.github.io/sourcesteward/schemas/v2/inspect.schema.json) |

These files use JSON Schema 2020-12. They contain only internal references, so downloaded
files validate offline with a compatible validator. Site packaging serves the checked-in
bytes. Publication URLs become available after the feature merges and exact-revision
Pages deployment succeeds; source checks alone do not prove live publication.

## Output and errors

Version success exits 0 and emits one JSON object plus a newline, with `schema_version`,
`name` and `version`. Version usage errors exit 2 and do not emit JSON.

Git inspection success exits 0; its `status` is `ok`, `backend` is `git`, `repository` contains
identity, and `diagnostics` is an empty array. Git and version output retain schema 1.
Recognized native jj ownership selects schema 2 for both success and operational failures;
callers must dispatch by `schema_version`, rather than guessing from field presence. Operational failures exit 1 and emit an
inspection result with `status` `error` or `unsupported`, a null `repository`, and a nonempty
`diagnostics` array. They also write a concise `Error:` diagnostic to stderr. Unsupported
managed backends retain detection evidence without pretending to be ordinary Git results.
`input_path` can be empty if early failure prevents an absolute, valid UTF-8 path.

Inspection usage errors exit 2 with no JSON. Writer failures can leave partial output;
callers must check both the exit status and JSON validity. No universal JSON envelope is
promised for every process failure. See the [inspection CLI contract](../specs/005-repository-inspection/contracts/inspect.md)
for exact outcomes and effect limits.

HEAD is attached, detached or unborn. Attached HEAD has both a full branch ref and an oid;
detached HEAD has null ref and an oid; unborn HEAD has a full branch ref and null oid.
Object IDs are full lowercase hex, 40 characters for SHA1 or 64 for SHA256. Bare repositories
have null worktree paths. Paths are opaque JSON strings, not URLs; preserve escaped control
characters and do not interpolate path or diagnostic strings into shell commands.

## Native jj identity (schema 2)

Selected native jj results always have `backend: "jj"` and `repository: null`. Successful
results additionally contain `jj_repository` with `workspace_root`, `metadata_dir`,
`repository_dir`, `workspace_name`, `change_id`, `commit_id`, `operation_id` and `observation`.
Paths are absolute and retain exact valid UTF-8; workspace names are opaque strings.

Change IDs are full 32-character reverse hex (`k`–`z`); commit IDs are full 40-character
lowercase hex; operation IDs are full 128-character lowercase hex. These are distinct
backend identities, not interchangeable Git branches or IDs.

The observation has `working_copy: "stored-unsnapshotted"` and a nonempty `limitation`
explaining freshness and concurrent-state limits. Inspection does not snapshot edited or
untracked disk content, update stale workspaces, merge operations or import/export Git
state. Native execution requires usable read-only sandbox enforcement; unavailable
enforcement returns `jj-sandbox-unavailable` without an unsandboxed retry. The inspected
repository is read-only; a private temporary isolation directory outside it may be used.

Operational failures omit `jj_repository` and have nonempty diagnostics; success has empty
diagnostics. Schema 2 allows additional object members and diagnostic codes but enforces
these known relationships. Ambiguous ownership and unsupported GitButler remain schema 1.
See the [native jj contract](../specs/008-jj-inspection/contracts/inspect.md) for effects,
version and process limits.

## Compatibility

Consumers must ignore additional object members at every nesting level and handle unknown
diagnostic codes. Existing required fields, types, discriminants and documented relationships
remain stable within schema 1. The schemas intentionally allow extra properties; they do not
reject compatible producer extensions. New status/backend/HEAD/object-format alternatives or
incompatible meaning/type changes require a new `schema_version` and a new `schemas/vN/` URL.
Published old versions remain available. Additive optional fields and new diagnostic codes
may extend an existing version. If exact schema bytes matter, pin a Git revision or checksum as well as
the versioned URL.

## Verification and dependencies

`go test ./internal/contracts -v` compiles schemas against the validator's embedded standard
metaschemas, prohibits external references/loading, rejects independently authored invalid
results and validates real CLI output using isolated temporary Git repositories. `make check`
and CI include these tests. Fixtures cover version, attached/detached/unborn HEAD, SHA1/SHA256,
bare and linked worktrees, managed workspace refusals and operational failures. Native jj
fixtures validate standalone, colocated and secondary workspaces, nested paths, unsnapshotted
content and failure results against schema 2. With `STW_REQUIRE_JJ=1`, unavailable jj fails
instead of skipping these fixtures; mandatory conformance CI uses this setting. Hardcoded
checksums also protect published v1 schema bytes from accidental changes.

The established [santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema)
validator is pinned at v6.0.3 in `go.mod` for tests only, with its dependencies pinned in
`go.mod`/`go.sum`. It supports the chosen dialect and embeds standard metaschemas. Go's
standard library has no JSON Schema engine; maintaining a custom engine would obscure the
contract under test. The runtime CLI does not import the validator; tests check its dependency
graph. Downloading pinned Go modules during initial setup is separate from offline schema
validation after dependencies are cached.

## Inventory schema v1

[Inventory schema](../schemas/v1/inventory.schema.json) has stable publication URL
[Inventory v1](https://wstein.github.io/sourcesteward/schemas/v1/inventory.schema.json).
Its `command` discriminator is `inventory`; do not confuse its independent schema v1
with inspect schema v1/v2. The envelope embeds the selected existing inspection identity
and typed workspace/reference/base/file observations, completion limits and an optional
true/false/unknown query. Known false is success (exit 0); required unknown evidence is
partial (exit 1), while operational failure is error (exit 1). Invalid combinations are
rejected by offline fixtures. See [inventory](inventory.md) and its
[normative contract](../specs/009-workspace-inventory/contracts/inventory.md).
Spec 027 keeps this schema byte-identical (the `boundary.limits` constants too) and changes Git
behavior within it: repositories that returned `error` for size now return `ok` or `partial` with
verbatim truncation notes in `boundary.limitations`; a submodule or separate-Git-directory
checkout is described by its checkout path (the old value was the Git directory); `tracked` of a
gitlink-bearing workspace can be `true`; `command_bytes` names the largest unit held at once.
Consumers must read `status` and `boundary.complete`, not infer "too large" from `error`. The
compatibility table is in the
[Spec 027 contract](../specs/027-inventory-limits/contracts/inventory-limits.md).
This source change stages the schema for Pages; it does not prove live deployment.

## Repos schema v1

[Repos schema](../schemas/v1/repos.schema.json) is independently versioned for `stw repos`; its
`command` discriminator is `repos`. One result holds an entry per explicit path and discovered
submodule, each with status, backend, an error object, discovery counts, derived facts and the
unchanged embedded inventory (also valid against the inventory schema), plus a summary, the
boundary (effects, limits, limitations) and diagnostics. Entry statuses are `ok`, `partial`,
`error` and `skipped`; the overall status follows the entries (exit 0 only when every entry is
`ok` or `skipped`, exit 2 for usage errors with no JSON). Inherited inventory limits apply to each
member and are listed in the contract. Offline tests validate real command output and the
fixtures. See [multi-repository inventory](repos.md) and its
[normative contract](../specs/023-multi-repo-inventory/contracts/repos.md).

## Workspace schema v1

[Workspace schema](../schemas/v1/workspace.schema.json) is independently versioned and staged
for publication at [Workspace v1](https://wstein.github.io/sourcesteward/schemas/v1/workspace.schema.json).
Its command discriminator is `workspace-plan`, `workspace-apply`, `workspace-handoff` or
`workspace-retire-plan`. Results contain typed nullable plan/task records, action outcomes,
diagnostics and limitations. Refusal/error output retains the same envelope; completed
actions survive partial failure. Git native commit identity remains distinct from jj
commit/change/operation identities. Embedded inventory uses its established independent contract.
Offline tests validate real operational output and independently invalid fixtures without
fetching external schema references. See [task workspaces](workspaces.md) for authorization,
effects, recovery and bounded execution. Publication evidence remains separate from source checks.

## Tidy results (independent schema 1)

`stw tidy plan --json` emits a tidy envelope with `command`, `status`, `backend`, nullable
`plan`, `actions`, `diagnostics` and `limitations`. The plan binds source, selected references,
base/native identities, digests, candidate decisions/proofs, declared effects and recovery limits.
Stateless plans have empty plan IDs; stored plans have generated immutable IDs. Candidates use
`branch` or `bookmark`, `delete` or `retain`, and `native-ancestry` or `none`; ambiguous retained
jj targets may have empty commit/change IDs only with `reason: "ambiguous-target"` and no proof.
Deletion candidates cannot contain workspace blockers or missing native identity/proof.

Complete observations, including all-retained plans, exit 0. Operational refusal/error/partial
results exit 1 and validate against the same schema. Usage/writer failures may not emit complete
JSON. `tidy-apply` now uses this same unchanged contract. Apply requires a stored plan and explicit
authorization; action details expose native recovery pins and result limitations identify the durable
journal location. Refused/partial outcomes must not be treated as completed deletion. Future incompatible retirement result shapes use a separate schema
version. Existing inspection, inventory, version and workspace schema bytes remain unchanged.

## Owned retirement results (schema 2)

Retirement selection emits `schema_version: 2`, `plan: null` and the separate `retirement` record.
Complete successful results include the record; early operational failures may omit it. The record
binds completed creation plan/task/journal ownership, native source and target identities, physical
path identities, stored/disk manifests, distinct registration/files selections and action blockers.
The envelope retains actions, diagnostics and limitations so partial effects and durable journal
locations remain visible. Authorization refusal for a recognized stored retirement plan also uses
schema 2; an unrecognized plan ID cannot establish its result kind and may use schema 1.

Publish/validate against the independent v2 URL; do not reinterpret a reference tidy v1 plan as
retirement authority. Both versions remain available and the v1 schema's exact bytes are checked by
a regression checksum. Additional object members remain compatible; new incompatible semantics or
field types require another version. See [tidy](tidy.md) for native coupled/separate effects.

## Pin audit (v1, planned)

`stw pins --json` (merged but hidden until the conformance work item of
[Spec 021](../specs/021-submodule-pin-audit/spec.md); feature status stays planned) emits one `PinsResult`
with `schema_version: 1` and `command: "pins"`, validated by
[pins.schema.json](../schemas/v1/pins.schema.json), which is present. Its `status` is `ok`, `incomplete`, `refused` or `error`; refusals and
operational failures keep the same envelope. Finding codes and refusal codes are fixed by the
specification and append-only within schema 1; it is an independent contract and existing schemas keep
their bytes. Exit status 3 (`--strict` with error findings in a complete result) is documented in
[pins](pins.md), which also holds the finding and refusal tables.

## Hosting observations (v1)

[hosting.schema.json](../schemas/v1/hosting.schema.json) is a separate contract shared by `stw hosting` and `gh jj`. It retains native identity, candidates, explicit hosting selection, PR head/base identities, individual CI checks/statuses, diagnostics, sample time and bounded completeness/network effects. Errors may occur before any remote read; missing evidence is never empty successful CI. Existing version/inspection/inventory/workspace/tidy schemas retain their bytes and meanings. See [hosting](hosting.md).

## PR status and lookup (v2)

[hosting-pr.schema.json](../schemas/v2/hosting-pr.schema.json) is the explicit opt-in `stw hosting pr --json --json-version 2` / `gh jj pr` contract and [hosting-prs.schema.json](../schemas/v2/hosting-prs.schema.json) the `hosting prs SHA --json` contract. Ordinary `--json` stays hosting schema 1 and its bytes. v2 has explicit nulls for merge/close dates, the raw GitHub merge SHA and the method-dependent `result_sha`; `observation` (`mode` always `once`, `stop`, expected head and expectation) and `boundary` (one request). Consumers ignore unknown properties, reject an unsupported `schema_version` and treat an unknown `observation.stop` as failure. The watch-only members of the design (`watch` mode, `timeout`, `changed`, `merged`/`closed-unmerged` stops) are reserved and rejected by this schema. See [hosting](hosting.md).

## Sync schema v1 (unpublished)

[Sync schema](../schemas/v1/sync.schema.json) describes `stw sync [--plan] --json` (Spec 032). It is checked in and
validated offline but, like the release source schemas, excluded from Pages packaging until the feature is
accepted, so its first shape changed in place before publication. One object with `schema_version` 1, `command`
`sync`, `mode` (`plan`, `sync` or `restore`), `run_id` (null for a plan and for a run that changed no branch, else the ID of the
journal and the recovery refs; always set for a restore), `options` (`rebase`, `prune`, `autostash`: the requested override or null when the Git
configuration decides; `merge` and `submodules`: true only for `--merge` and `--submodules`), `note` (set when the command ran inside a linked worktree, which it did not touch), `repos`, `summary` (`total`, `by_result`) and `error` (null, or `code` and `message` of a
refused run, with no repos). No key is omitted; absent values are null. A repository (`kind` `root` or `submodule`) has
`name` (`.` or the path relative to the workspace root, such as `app/lib`), `path`, `branch`, `remote`, `result`, `old_oid`, `new_oid`, `ahead`, `behind`,
`pruned` (remote-tracking refs the fetch removed), `reason` (set for every skip and failure) and `detail` (Git's own
message of a `conflict` or `autostash-conflict`) and `stash_oid` (the stash entry git kept after an autostash that could not be reapplied; null for every other result and when the entry cannot be identified without doubt). Results: plan `fast-forward`, `rebase`, `merge`; run `fast-forwarded`, `updated`, `unreachable` (a pinned submodule under `--submodules`),
`rebased`, `merged`, `conflict`, `autostash-conflict`, `failed`; both `up-to-date`, `ahead`, `diverged`, `skipped-dirty`,
`skipped-detached`, `skipped-no-upstream`, `skipped-busy`, `skipped-uninitialized`; restore `restored` (old and new OID: the tip before and after),
`already-restored`, `changed-since`, `journal-incomplete` (both with a reason) and `failed`. A restore lists one entry per journaled branch change. Exit 1 only for a refused run or a
`failed`, `conflict`, `autostash-conflict`, `unreachable`, `changed-since` or `journal-incomplete` repository; 2 for usage errors with no JSON. Tests validate the fixtures,
reject broken variants and check real command output from temporary repositories with bare remotes. See
[commands](commands.md#bring-the-workspace-and-its-submodules-up-to-date).

## Status schema v1 (unpublished)

[Status schema](../schemas/v1/status.schema.json) describes `stw status --offline --json` (Spec 033, byte-identical to what `stw status --json` printed before the online default). Like the sync schema it is
checked in and byte-frozen but excluded from Pages packaging until the feature is accepted. One object with
`schema_version` 1, `command` `status`, `options` (`fetch`, `worktrees`), `root`, `note`, `repos`, `summary` (`total`,
`attention`, `errors`) and `error` (null, or `code` and `message` of a refused or failed run, with no repositories). A
repository has `name`, `path`, `kind` (`root`, `submodule`), `state` (`ok`, `uninitialized`, `error`), `branch`,
`detached`, `head`, `upstream`, `upstream_gone`, `ahead`, `behind`, `changes`, `operation`, `stash`, `branches`,
`worktrees` (counts incl. `missing`, and `list` only with `--worktrees`), `gitlink` (null for the root), `fetch_error`, `attention` (boolean), `reasons` (why, empty unless attention) and
`error`. No key is omitted; absent values are null. Exit 0 also when repositories need attention, 1 for an unreadable
repository or a failed run, 2 for usage errors with no JSON. Tests validate the fixtures, reject broken variants and
check real command output from temporary repositories. The exact rules are in the
[spec](../specs/033-stw-status/spec.md).

## Status schema v2 (unpublished)

[Status schema 2](../schemas/v2/status.schema.json) describes `stw status --json`, the online default (Spec 037). Like the v1
schema it is checked in and byte-frozen but excluded from Pages packaging until the feature is accepted; the v1 schema and its
digest do not change. One object with `schema_version` 2, `command` `status`, `time` (UTC), `host`, `options` (`fetch`,
`worktrees`, `icons` always `null`), `root`, `note`, `actions`, `repos`, `sources`, `notices`, `summary` and `error`. No key is
omitted, absent values are null and `additionalProperties` is false everywhere; strings are at most 2048 bytes (notice messages
512).

- `actions`: every item, with `id` (`CATEGORY:REPO:SUBJECT`, stable), `priority`, `category` (the 28 of the
  [status page](status.md#needs-action)), `repo`, `path`, `title`, `url` (`https` or null), `subject` (`kind`, and the keys that
  apply of `number`, `tag`, `sha` and `name`, the id part that is neither a number nor a tag), `evidence` (small facts whose keys
  the schema fixes per category, or null), `command`, `effect` (`read` or `owner`, null exactly when `command` is) and `hint`.
- `repos`: all members of the v1 repository object plus `remote` with `repo` (`OWNER/NAME` or null), `state` (`ok`, `degraded`,
  `skipped`, `error`), `reason` (only when skipped) and `sources`, one `{state, error, reason, data}` for each of `repo`,
  `pulls`, `releases`, `tags`, `ci_main`, `ci_release` and `issues`. `state` is `ok`, `skipped` (with `reason`) or `error` (with a
  code); `data` is `null` unless the state is `ok`, and its keys are fixed per source. The `issues` data is `{open, red, yellow,
  blue, violet, pulls_dropped, truncated}` (`open` is the sum of the four counts, `truncated` is true for more than ten pages);
  its skip reasons add `issues-disabled`, which counts as read in `sources` and does not degrade `remote.state`.
- `sources`: exactly nine entries in the order `local`, `repo`, `pulls`, `releases`, `tags`, `ci-main`, `ci-release`, `issues`,
  `viewer`, each `{id, state, read, degraded, skipped}` with the counts of repositories per outcome and a `state` of `ok`,
  `skipped`, `error` or `degraded`. `viewer` is the one `user` read of the run, which belongs to no repository: `read` 1 when it
  answered, `skipped` 1 when it was skipped (the token class cannot read it, no request was made, or no repository is on GitHub:
  `not-github-origin`), `degraded` 1 when it failed, and no count at all when the run failed before any remote read. The category enum gains `issue-security` (red),
  `issue-assigned` and `issue-bug` (yellow), their subject kind is `issue` with a `number`, and their evidence is `{reason,
  labels, assignees, author}` with at most 20 labels and assignees. A notice about the run and not a repository (`no-identity`,
  `viewer-unknown`) has `repos` `[]`.
- `notices` (`code`, `source`, `repos`, `message`, one per missing source), `summary` (`repos`, `attention`, `errors`,
  `priorities`, `degraded`, `failed`, `notices`) and `error` (`status-` code and message of a failed run, with no repositories).

Exit 0 also with items, offline, without a token, after a deadline skip or a missing release workflow; 1 when an attempted
request failed, a repository cannot be read or the run fails; 2 for usage errors with no JSON. Tests validate the documents of
every outcome the command can print (healthy, no token, a reader that cannot be opened, failing requests, a rate limit, a
failed local read), reject about sixty broken variants and keep the schema in step with the action model.

## Watch event schema v1 (unpublished)

[Watch event schema](../schemas/v1/watch-event.schema.json) describes every line of `stw watch` stdout (without `--human`)
and of its `journal.jsonl` (Spec 035). It is checked in and byte-frozen but excluded from Pages packaging until the
feature is accepted. The envelope has `schema_version` 1, `seq`, `id`, `time`, `repo`, `pr`, `kind` (a closed enum of 15
kinds) and `details`, whose keys are fixed per kind; no key is omitted and absent values are null. A second top-level
definition, `#/$defs/status`, describes `stw watch --status --json`. Tests validate fixtures, reject broken variants and
validate every event the crash matrix and the end to end acceptance run generate. See [watch](watch.md).

## Pulls schema v1 (unpublished)

[Pulls schema](../schemas/v1/pulls.schema.json) describes the one document `stw pulls --json` prints (Spec 036). It is
checked in and byte-frozen but excluded from Pages packaging until the feature is accepted. Top-level keys are
`schema_version` 1, `command` `pulls`, `time`, `host`, `options`, `repos`, `groups` (`ready`, `checks`, `conflict`,
`draft`, `blocked`, each an array of rows, newest first), `summary` and `errors`; no key is omitted and absent values
are null. A row carries the verdict (`group`, `reason`, `ready`) and `detail`, which is false for a draft without detail
or a row past `--limit`; its `checks`, `mergeable`, `mergeable_state` and `rebaseable` are then null. `url`,
`created_at`, `age_days` and `head` are null only when the forge gave no usable value. An error always has both `repo` and `owner` keys, exactly one non-null. `--owner` alone reports scope `repos`. Tests validate the contract example, reject broken variants and validate the real output of
the command for every flag combination. See [pulls](pulls.md).

## Issues schema v1 (unpublished)

[Issues schema](../schemas/v1/issues.schema.json) describes the one document `stw issues --json` prints (Spec 038). It is checked in and
byte-frozen but excluded from Pages packaging until the feature is accepted. Top-level keys are `schema_version` 1, `command` `issues`,
`time`, `host`, `viewer`, `options`, `repos`, `issues` (`red`, `yellow`, `blue`, `violet`, each an array of rows sorted by repository and
number, after the filters and the `--limit` cap), `summary` and `errors`; no key is omitted and absent values are null. `viewer` is
`{state, login, reason, error}`: `ok` with a login, `skipped` with the reason `no-identity` (a 403 or 404 on `user` without rate-limit
evidence) or `not-github-origin` (no request was made), or `error` with a code. A repository is `ok`, `partial` (`too-many-open-issues`),
`error` or `skipped`; a skipped one has the reason `issues-disabled` (zero issues) or `viewer-unknown` (`--assignee @me` without a known
login: not read, `open` and `pulls_dropped` null). The group, `reason`, `command` and `effect` of a row agree: red rows are
`security-label`, yellow rows `assigned-to-you` or `bug-label`, both with `command` `gh issue view N -R OWNER/NAME` and `effect` `read`;
blue rows (`none`) and violet rows (`bot`) have neither. `summary.counts` covers everything read with the filters applied and the cap not.
An error has both `repo` and `owner` keys, at most one non-null; a run-level failure (`viewer-unknown`, a failed viewer read) has both
null. Tests validate the contract example, reject broken variants and validate the real output of the command. See [issues](issues.md).

## Release source schemas (unpublished)

Spec 026 remains planned/unreleased. The following checked-in schema v1 files
support current source implementations; they are deliberately excluded from
Pages packaging until T041. Their `$id` values do not prove live publication.
No release schema is promoted by this documentation change.

| Document/result | Source schema |
| --- | --- |
| Project contract | [release-config](../schemas/v1/release-config.schema.json) |
| Release record | [release-record](../schemas/v1/release-record.schema.json) |
| Build manifest | [release-build-manifest](../schemas/v1/release-build-manifest.schema.json) |
| OCI build result | [release-oci-result](../schemas/v1/release-oci-result.schema.json) |
| `release check --json` | [release-check](../schemas/v1/release-check.schema.json) |
| `release plan --json` | [release-plan](../schemas/v1/release-plan.schema.json) |
| `release verify --json` | [release-verify](../schemas/v1/release-verify.schema.json) |
| `release record --json` | [release-record-result](../schemas/v1/release-record-result.schema.json) |
| `release build --json` | [release-build](../schemas/v1/release-build.schema.json) |
| `release dispatch --json` | [release-dispatch](../schemas/v1/release-dispatch.schema.json) |

Result schemas admit additive members; the four contract/data formats reject
unknown properties. The manifest schema references the record schema; offline
validation must resolve that reference to the checked-in file, not fetch its
publication URL. The record object and the `record --json` result envelope are
different documents. Preserve the generated canonical record bytes.

Inspect `status`, `verdict`, gates and boundary qualifications as well as exit
status. Offline `plan`/`verify` cannot clear online gates; `--online` adds bounded
GitHub evidence, and verification additionally checks attestations. Exit 3 also
covers release findings, failed gates, record disagreements and local-verification hash mismatches. See
the [release guide](release.md) for the per-command matrix and remaining gates.
Dispatch source is implemented; its schema is also unpublished. Result-schema
`status` is separate from a gate `verdict` or from workflow completion:

| Results | Status interpretation |
| --- | --- |
| `check` | `ok` has no error findings; `findings` reports validation errors |
| `plan`, `verify`, `dispatch` | `ok` can still carry `fail` or `incomplete` verdicts; inspect gates and qualifications |
| `record` | `ok` produced/checked a record; `failed` reports input or comparison disagreement |
| All above | `refused`/`error` are not passing evidence; inspect the typed error and boundary |

For dispatch, `applied` reports acceptance of the one fixed POST, not completion
of its CI run. The required `request`, `plan_id`, `http_status`, `workflow_url`
and `approval` fields describe plan/apply effects; no-apply performs no POST.
`network` lists requests actually attempted, so an uncertain POST failure must
not be retried on the assumption that nothing started. The
[dispatch contract](../specs/026-release-process/contracts/dispatch.md) defines
its command-specific exit meanings and the separately required build approval.

## Tidy overview (wizard layer, unpublished schema)

`stw tidy plan --json` without a `PATH` and without an expert flag, and `stw tidy --json`, print one `tidy-overview` document
(`schemas/v2/tidy-overview.schema.json`, `schema_version` 1, `additionalProperties` false). It is frozen in the source tree but listed in
`UNPUBLISHED_SCHEMAS`, so no Pages URL exists yet and consumers must not rely on one. Required members: `command`, `workspace`,
`derived[]` (name, value, source, overridden), `repositories[]` (state `analysed`, `skipped` or `error`, with a reason), `candidates[]`
(verdict `safe`, `decision` or `retain`; owner, owner source, scope `own`, `other` or `unknown`, proof, effects, evidence), `summary`, `mode` (the run
mode evidence), `plan_id` (empty unless `--save` stored a plan set), `notices[]` and `truncated`. The document is bounded: 4096
candidates, 512 bytes of evidence each and 4 MiB; beyond that `truncated` is true and the exit code is 1 (`output-limit`). The
overview stores and changes nothing. The v1, v2 and v3 tidy schemas keep their meaning; see [tidy](tidy.md#the-tidy-wizard).

## Explicit tidy source contract 3

`stw tidy plan/apply --contract 3 --json` uses the independent source schema
`schemas/v3/tidy.schema.json`. It remains unpublished in the source-only inventory, as do the
candidate schemas `schemas/v3/tidy-report.schema.json` of the review report,
`schemas/v3/tidy-decisions.schema.json` of the reviewer decision file,
`schemas/v3/tidy-restore.schema.json` of the `tidy restore` plan and apply result and
`schemas/v3/tidy-history.schema.json` of `tidy history` and `tidy restore --list`; the
existing public v1/v2 schema URLs and bytes are unchanged. The v3 envelope identifies mode,
proof/provenance, exact effects, null executable plan on incompleteness, sampled freshness,
actual network attempts and cumulative measured budgets. Complete negative observations
exit 0, operational incomplete/refused/partial results exit 1 and malformed invocations exit 2.
A saved plan is immutable evidence, never permission. See [tidy](tidy.md) for exact selection
and online/local acknowledgments; old records are neither migrated nor reinterpreted.
