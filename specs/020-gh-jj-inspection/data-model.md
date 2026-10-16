# Data model: hosting observation v1

All types live in `internal/hosting` except the reused native `inspection.Result` and diagnostic type. JSON names use snake_case. Arrays are non-null even when empty; optional object values are null. This is sampled evidence, not a transaction or mutation plan.

## Options

`Options{Kind string, Number int, Repo string, Host string, Remote string, RequireJJ bool}`. Kind is `repo`, `pr` or `ci`. Number is a positive integer for PR/CI and absent/zero for repo. Repo is validated `OWNER/NAME`, Host is an explicitly selected valid DNS hostname (default github.com), Remote is an optional exact configured name; Repo and Remote cannot contradict selection. RequireJJ is set by extension policy, not repository text.

`ValidateOptions(Options) error` performs usage validation before effects; `Inspect(context.Context, string, Options) Result` owns observation and effects.

## Result

- `schema_version`: integer 1.
- `kind`: repo/pr/ci; `status`: ok/error; `backend`: selected native backend or explicit unknown/ambiguous/unsupported ownership evidence.
- `identity`: nullable native `inspection.Result`, preserving schema/version/backend-specific identities without flattening change/commit/operation IDs.
- `repository`: nullable Repository; `candidates`: Candidate array.
- `pull_request`: nullable PullRequest; `checks`: Check array; `statuses`: CommitStatus array.
- `diagnostics`: reused diagnostic array; successful results empty, errors nonempty.
- `boundary`: completeness, network effect, enforced limits and limitations.
- `observed_at`: RFC3339 UTC observation time.

Error results may retain valid sampled observations, but boundary.complete is false. Complete repo success has a selected repository without live existence verification. PR success has a matching PR; CI success additionally has completed checks/status collections and stable sampled PR head.

## Repository and Candidate

Repository: string `host`, `owner`, `name`, `selection`. Selection records explicit repository, explicit remote or unique-candidate discovery. It is not a default-branch claim or live API verification.

Candidate: string `remote`, `host`, `owner`, `name`; provenance remains visible when duplicate repository identities deduplicate selection. Never expose embedded credentials or accept URLs as endpoints.

## PullRequest

Integer `number`; string `title`, `url`, `state`; bool `draft`; string `head_sha`, `base_sha`, `head_ref`, `base_ref`, `head_repository`, `base_repository`. Full SHA identity is validated; local jj change IDs are never confused with SHA IDs. A deleted/inaccessible required head repository must be explicit refusal rather than a fabricated identity. URL is untrusted display data and never followed as API routing.

## Check and CommitStatus

Check: positive int64 `id`; strings `name`, `status`, `head_sha`, `url`; nullable string `conclusion`. Pending checks legitimately have null conclusion. SHA must match exact PR head. Unknown API status/conclusion strings remain opaque evidence with no aggregate passing inference.

CommitStatus: positive int64 `id`; string `context`, `state`, `description`, `url`. Status values are observation data, not required-check policy. Different IDs for repeated context updates remain distinguishable.

## Boundary

Bool `complete`, `network`; integer `max_pages`=10, `max_records`=1000, `max_bytes`=1048576, `timeout_seconds`=30; string array `limitations`. Network means PR/CI remote reads were requested/attempted, not that all succeeded. Native local identity limitations remain nested and human-visible. No persistent state transition or application authorization exists in this feature.

## Native remote observation boundary

`inspection.Remotes(ctx, identity Result) RemoteResult` returns `Identity Result`, `Status string`, `Remotes []Remote` and `Diagnostics []Diagnostic`. `Remote{Name string, FetchURLs []string, PushURLs []string}` remains internal observation data, never raw output with credentials. Git uses bounded isolated built-ins; jj uses native sandboxed remote list with strict conservative parsing because its supported interface is textual. Ambiguous/unsupported representations refuse. Before/after identity and remote resampling qualify sampled consistency; no transaction is claimed. Head/base hosting repository strings are `owner/name` under the selected host.
