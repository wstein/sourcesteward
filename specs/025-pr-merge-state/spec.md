# Feature Specification: Pull request merge-state explanation

**Feature Branch**: `codex/025-pr-merge-state`

**Created**: 2026-10-16

**Status**: Draft — specification written; independent analysis pending; implementation pending

**Input**: A read-only `stw hosting merge-state NUMBER` (and `gh jj merge-state NUMBER`) that lists what a GitHub pull request is waiting on, from explicit read-only evidence, each item with its evidence source and an explicit confidence, without ever claiming that the pull request is mergeable or will merge.

**Depends on**: [Spec 020](../020-gh-jj-inspection/spec.md) (hosting resolution and fixed REST reads) and [Spec 022](../022-bounded-go-gh/spec.md) (bounded go-gh transport, implemented on `origin/main`). See Scope and Dependencies.

## The incident this feature answers

A bot-authored pull request in `wstein/bundledock` had auto-merge enabled with the rebase method and still waited. The base branch required the status context `check`, required branches to be up to date (strict), required linear history, allowed only rebase merges and enforced those rules for administrators. Each pull request therefore had to wait for the previous one and then be updated. A maintainer assembled this from four separate API calls (pull request, check runs, branch protection, repository merge settings). Measured facts about those endpoints, including what a normal token sees, are in [research.md](research.md).

## User Scenarios & Testing

### User Story 1 — Explain a waiting pull request in one command (Priority: P1)

A maintainer or agent asks why pull request N has not merged and gets one list of the conditions it is waiting on.

**Why this priority**: This is the failure that motivated the feature; today the explanation needs four manual API calls and knowledge of which fields to combine.

**Independent Test**: With real temporary Git and jj repositories and an in-process fake HTTP transport serving fixed GitHub responses, reproduce the incident (strict, required `check`, linear history, rebase-only, enforce-admins, head behind base, `check` pending) and compare the item list.

**Acceptance Scenarios**:

1. **Given** the incident fixture with the required context `check` pending at the head and the head 3 commits behind the base under a strict rule, **When** `stw hosting merge-state N` runs, **Then** the items include `required-check` (`pending`, `waits`, `observed`) and `head-behind-base` (`waits`, `observed`, evidence `compare` and `branch-protection`), plus the information items `auto-merge` (method `rebase`), `strict-required-status-checks`, `linear-history-required`, `merge-methods` (rebase only) and `administrators-included`.
2. **Given** every item, **When** it is rendered, **Then** it carries its evidence sources and field names and one of `observed`, `inferred` or `unknown`; an `unknown` item carries a reason from the reason table.
3. **Given** auto-merge enabled with a method that the repository settings or a ruleset prove disallowed, **When** the command runs, **Then** `auto-merge-method-not-allowed` is listed with `observed` confidence and the proving source.
4. **Given** a head that is not behind the base, **When** the command runs, **Then** no `head-behind-base` item is produced.

### User Story 2 — Report limits of permission honestly (Priority: P1)

A caller with ordinary read access, not repository admin, runs the command. Branch protection details and repository merge settings are not readable for such a token.

**Independent Test**: Serve fixture responses that reproduce the measured non-admin behavior (protection `404 Not Found` on a protected branch, repository `allow_*` fields `null`, ruleset rules readable) and a fixture reproducing an anonymous `401`.

**Acceptance Scenarios**:

1. **Given** `GET …/protection` answers `404` while the branch summary says `protected: true` with classic protection enabled, **When** the command runs, **Then** it reports `classic-protection-unreadable` with `unknown` confidence and reason `not-found-ambiguous`; it never reports "no protection" and never invents a strict, review or enforce-admins value.
2. **Given** repository merge-method fields are `null`, **When** the command runs, **Then** `merge-methods` is `unknown` with reason `repository-setting-hidden`, and the ruleset `allowed_merge_methods`, when present, are still reported with their own source.
3. **Given** a `401` or `403` from an optional source, **When** the command runs, **Then** that source's outcome is `denied`, the result is still `complete`, and the items that needed it are `unknown` with reason `permission-denied`; the exit status is 0.
4. **Given** a `404` from the pull request read, **When** the command runs, **Then** the result is `error` with `merge-state-pr-failed`; a missing optional source is never promoted to a pull request failure.
5. **Given** the head is behind the base and the strict flag is unreadable, **When** GitHub reports `mergeable_state` `behind`, **Then** `head-behind-base` is `waits` with `inferred` confidence; with any other `mergeable_state` it is `may-wait` with `unknown` confidence and reason `permission-denied` or `not-found-ambiguous`.

### User Story 3 — Never claim the pull request is mergeable (Priority: P1)

A caller or agent must not take the explanation as permission to merge or as a prediction.

**Independent Test**: Run clean, blocked, unknown, closed, draft and conflicted fixtures and assert the fixed verdict and forbidden-wording tests on the human and JSON output.

**Acceptance Scenarios**:

1. **Given** a pull request whose evidence shows no waiting condition and GitHub reports `mergeable_state` `clean`, **When** the command runs, **Then** the result states only that no waiting condition was found in the evidence read, `verdict` is the constant `not-asserted`, and no output says or implies that the pull request can be merged or will merge.
2. **Given** GitHub reports `mergeable_state` `blocked` and no `observed` or `inferred` waiting item explains it (the measured state of `wstein/bundledock` pull request 112), **When** the command runs, **Then** `blocked-unexplained` is listed with `unknown` confidence and reason `cause-not-readable`.
3. **Given** GitHub's `mergeable` is `null` (not yet computed; measured on `cli/cli`), **When** the command runs, **Then** `mergeability-not-computed` is listed with `unknown` confidence; the command does not poll or retry.
4. **Given** a closed or merged pull request, **When** the command runs, **Then** only `pull-request-closed` is listed, the other sources are `skipped`, and the result is complete.
5. **Given** the GitHub-reported `mergeable` and `mergeable_state` values, **When** they are rendered, **Then** they appear only under the labelled `github_reported` object and the human line "GitHub reports …", never as SourceSteward's own conclusion.

### User Story 4 — Rely on one bounded contract on both Tier 1 backends (Priority: P1)

Humans and agents use the same result through `stw hosting` and `gh jj`, on Git and on jj.

**Independent Test**: Validate success, incomplete and error fixtures against the new schema; compare both entry points on the same jj fixture; run ordinary Git, linked Git worktree, colocated jj, standalone jj and secondary jj workspaces; confirm `hosting.schema.json` and its fixtures are byte-identical to the base.

**Acceptance Scenarios**:

1. **Given** a Git- or jj-owned workspace (including colocated and secondary), **When** `merge-state` runs, **Then** repository resolution and ownership follow Spec 020 unchanged and native identities are preserved; `gh jj merge-state` refuses Git ownership as the other `gh jj` commands do.
2. **Given** `--json`, **When** the command runs, **Then** one object valid against `schemas/v1/merge-state.schema.json` is printed and the human output is rendered from the same result.
3. **Given** the existing `hosting` schema and fixtures, **When** this feature is implemented, **Then** they are unchanged byte for byte and still validate.
4. **Given** exit conditions, **When** the command ends, **Then** the exit status follows the exit-status table.

### User Story 5 — See rulesets, reviews and rules the command cannot evaluate (Priority: P2)

**Independent Test**: Fixtures with rulesets (`pull_request`, `required_status_checks`, `merge_queue`, an unevaluated type), reviews and conversation-resolution requirements.

**Acceptance Scenarios**:

1. **Given** rules returned for the base branch, **When** the command runs, **Then** `rulesets-apply` lists them (id, source type, source, rule types) and required contexts, strict flag, required review count and allowed merge methods from rules are used as sources equal to classic protection.
2. **Given** a required review count N and fewer than N distinct latest `APPROVED` reviews, **When** the command runs, **Then** `review-required` is `waits` with `observed` confidence; with at least N it is `info` with `inferred` confidence, and the text says reviewer eligibility is not evaluated.
3. **Given** conversation resolution is required, **When** the command runs, **Then** `conversation-resolution-required` is listed with `unknown` confidence and reason `rest-gap`, because REST exposes no thread resolution state (measured).
4. **Given** a rule type that this command does not evaluate, **When** it applies to the base branch, **Then** `rule-not-evaluated` lists the type with `unknown` confidence.
5. **Given** a `merge_queue` rule, **When** the command runs, **Then** `merge-queue-required` is listed and queue position is stated as not read.

### User Story 6 — Run safely on hostile data, within bounds (Priority: P1)

**Independent Test**: Hostile pull request, context, ref, ruleset and review text; oversized, malformed and redirected responses; moved heads; exhausted pages; cancellation.

**Acceptance Scenarios**:

1. **Given** pull request titles, context names, ref names, ruleset names and logins containing control bytes, ANSI sequences, bidirectional controls or very long text, **When** rendered, **Then** they are data: bounded, escaped in human output, and never endpoints, commands or markup.
2. **Given** a base ref name outside the supported character set, **When** the command runs, **Then** every source that needs it is `skipped` with reason `ref-unsupported` and no request carries it.
3. **Given** the head, base ref, state or draft flag changes, or the base branch tip moves, between the first read and the resample, **When** the command ends, **Then** it is `incomplete` with `merge-state-changed`.
4. **Given** a malformed or oversized body, an exhausted page, record or request bound, or a transport failure on an optional source, **When** the command ends, **Then** the result is `incomplete` with a diagnostic, never a complete success with a guessed value.
5. **Given** cancellation or the 30-second deadline, **When** it occurs, **Then** the result is an `error` with `hosting-canceled`.

### Edge Cases

- Fork pull requests: the head SHA is compared in the base repository (measured to work for a fork pull request); the head repository may be deleted or inaccessible and is never required.
- Base branch names containing `/`; names with percent, `#`, non-ASCII or other characters outside the supported set (skipped).
- `mergeable_state` values other than those measured (`has_hooks`, `behind`, `draft` are documented but were not observed) are echoed as data and never drive a conclusion except where the tables say.
- Draft pull requests report `mergeable_state` `blocked` or `clean` in the measured data, never `draft`; the `draft` field is the evidence.
- Several check runs or statuses with the same name, from different apps, pinned and unpinned required contexts, a required context satisfied only by a commit status.
- Organization-level rulesets (`ruleset_source_type` `Organization`), rulesets with `evaluate` enforcement (not returned by the rules endpoint; unverified), bypass actors (not readable here).
- GitHub Enterprise Server or ghe.com hosts, where an endpoint may be missing: a `404` on an optional source is `not-found`/`unknown`, never an absence claim (not verified; see research).
- Required contexts or rules beyond the stated bounds; a repository with a merge queue.
- An empty PATH, a bare repository, no or ambiguous remotes: resolution refuses exactly as in Spec 020 before any network access.

## Requirements

### Functional Requirements

- **FR-001**: Provide `stw hosting merge-state NUMBER [PATH] [--repo OWNER/NAME] [--host HOST] [--remote NAME] [--json]` and the same leaf under `gh jj`, sharing one operation, parsing and rendering. `NUMBER` is an explicit positive integer; no heuristic mapping from branch to pull request. The command is read-only and non-interactive.
- **FR-002**: Reuse Spec 020 unchanged for ownership, native Git/jj identity, remote discovery, candidate selection, host selection (default `github.com`, enterprise only by explicit `--host`) and the `--repo`/`--remote` rules. Local resolution happens before any network access and is independent of authentication. `--repo` never overrides ownership.
- **FR-003 (Tier 1)**: Git and jj are supported peers for this capability through the shared hosting resolution. A jj-managed workspace, including colocated and secondary workspaces, uses jj; an unsupported, ambiguous or GitButler-managed workspace refuses with the existing hosting diagnostics. GitHub evidence is backend-independent, so the result differs between backends only in the preserved native identity. No backend capability is unsupported for this feature.
- **FR-004**: Read only the evidence sources of the **Sources table**, by fixed `GET` requests to validated paths on the selected API origin, through the hosting transport only. No GraphQL, no write method, no response-provided URL, no caller-provided endpoint, no generic forwarding. A source that does not apply (pull request not open, nothing to evaluate) is `skipped`, not requested.
- **FR-005**: Produce the item list of the **Items table** from those sources. Every item has `code`, `effect`, `confidence`, `evidence` (source and field names), a bounded `summary` and `detail`, and for `unknown` confidence a `reason` from the **Reasons table**. Confidence has the three values defined in the **Confidence definitions**; `unknown` is never converted into absence or into a negative claim.
- **FR-006**: Never claim that the pull request is mergeable or will merge. `verdict` is the constant `not-asserted`. Output never states or implies mergeability, readiness or a prediction. GitHub's own `mergeable` and `mergeable_state` are reported only inside `github_reported`, labelled as GitHub's report, and are not combined into a conclusion except in the rows of the Items table that name them. The result states a fixed notice that it describes evidence sampled at one time and authorizes nothing.
- **FR-007**: Classify every failed read into one transport-neutral **failure class** (`unauthorized`, `forbidden`, `not-found`, `rate-limited`, `other-http`, `too-large`, `malformed`, `transport`, `canceled`) and map it to a source outcome in the Sources table. A `404` or `403` from a permission-gated source is never evidence of absence. The pull request read failing is an `error`; an optional source being `denied` or `not-found` is a complete observation.
- **FR-008**: Validate the base ref name before it enters any path. The supported set is: at most 200 bytes, components separated by `/`, each component matching `[A-Za-z0-9._-]+`, no component starting with `.` or `-`, none ending in `.lock`, and no `..` anywhere. The path carries the name with literal `/` separators, never percent-encoding, so the transport's existing path guard needs no relaxation of its `RawPath` refusal. Other names make the dependent sources `skipped` with reason `ref-unsupported`.
- **FR-009**: Bound the operation: 30 seconds total, at most 50 requests (the fixed requests plus at most 10 pages of each paged collection), at most 1 MiB per response and for the final result, at most 1000 records per collection, at most 100 required contexts, at most 50 rule entries listed and at most 300 items. Exhausting a bound makes the result `incomplete` with `merge-state-limit`; it is never reported as a complete success.
- **FR-010**: Resample the pull request (state, draft flag, head and base SHAs and refs, auto-merge) and the base branch tip after all other reads; any change makes the result `incomplete` with `merge-state-changed`. The result is sampled evidence, not a transaction.
- **FR-011**: Provide understandable human output and a separate `schemas/v1/merge-state.schema.json` result with `schema_version: 1`, rendered from one result. The existing `hosting.schema.json`, its fixtures and all other published contracts do not change. The new schema is self-contained (no cross-file reference); its identity definitions are copies guarded by a drift test. Exit status follows the exit-status table.
- **FR-012**: Treat all GitHub and repository text (titles, refs, context names, ruleset names, logins, URLs) as untrusted data: bounded, valid UTF-8, escaped for terminal rendering, never used as an endpoint, command, markup or credential. Never echo tokens or raw response or error bodies.
- **FR-013 (transport neutrality)**: State requirements on the transport, not on a mechanism: fixed `GET` only; a closed allowlist of request shapes (Sources table) enforced before credentials are sent; bounded request and response sizes; deadline and cancellation; failure classes of FR-007 delivered to the operation without leaking bodies; no redirect following; authentication only for the selected host. These hold under the Spec 022 go-gh transport (implemented) and would hold under the Spec 020 `gh api` transport or a successor. The requirements add to, and never weaken, Spec 022's contract.
- **FR-014**: Disclose effects: network reads of the Sources table endpoints with the caller's selected-host credentials; no write to the repository, Git or jj state, remote, pull request, branch protection or settings; no jj snapshot or operation-log change; no hook, credential helper, editor or pager. Disclose that reading a pull request can cause GitHub to compute its mergeability server side; that is GitHub's cache, not a repository change.
- **FR-015**: Require failing behavior tests first, independent review, and conformance on the existing Git minimum and current lanes and the jj lanes with effect sentinels, in-process fake HTTP for success paths, schema fixtures offline, and no live network in acceptance. Only `ubuntu-24.04` amd64 is a CI lane; other platforms are local evidence.
- **FR-016**: Add no dependency. Reuse `internal/hosting` resolution and bounded response handling; the CLI renders and never starts a process or opens a network connection.

### Confidence definitions (normative)

| Confidence | Meaning |
| --- | --- |
| `observed` | Read directly from a successfully read, shape-valid response for this pull request at the sampled head and base, and the item states exactly what the response states (for example `draft: true`, a required context with no matching record in a complete collection). |
| `inferred` | Derived by SourceSteward from observed values through a rule written in the Items table, or taken from GitHub's own lazily computed summary (`mergeable_state`) where the underlying cause is not readable. |
| `unknown` | Not determinable from the evidence, with a `reason`. An `unknown` item says that the condition may matter, never that it does not. |

### Sources table (normative)

Paths are relative to the selected API origin; `O`, `N`, `NUM` are validated owner, name and number; `SHA` is a full lowercase 40-hex identity; `BASE` is the validated base ref with literal `/`. Each row is a fixed `GET`. Outcomes: `ok`, `denied` (class `unauthorized` or `forbidden`), `not-found`, `rate-limited`, `failed` (any other class), `skipped`.

| Source id | Request | Permission measured or documented | Fields used |
| --- | --- | --- | --- |
| `pull-request` | `repos/O/N/pulls/NUM` (read twice: first and resample) | read access; anonymous works for public repositories | `state`, `merged`, `draft`, `mergeable`, `mergeable_state`, `auto_merge.merge_method`, `head.sha`, `head.ref`, `head.repo.full_name`, `base.sha`, `base.ref`, `base.repo.full_name` |
| `repository` | `repos/O/N` | read access; `allow_squash_merge`, `allow_merge_commit`, `allow_rebase_merge`, `allow_auto_merge` are `null` without push access (measured for read-only) | the four `allow_*` fields, `permissions` |
| `base-branch` | `repos/O/N/branches/BASE` (read twice: first and resample) | read access | `commit.sha`, `protected`, `protection.enabled`, `protection.required_status_checks.{contexts,checks,enforcement_level}` |
| `branch-protection` | `repos/O/N/branches/BASE/protection` | admin; a non-admin gets `404 Not Found` on a protected branch (measured) and anonymous gets `401` | `required_status_checks.{strict,contexts,checks}`, `enforce_admins.enabled`, `required_linear_history.enabled`, `required_pull_request_reviews.required_approving_review_count`, `required_conversation_resolution.enabled`, `required_signatures.enabled`, `lock_branch.enabled` |
| `branch-rules` | `repos/O/N/rules/branches/BASE?per_page=100&page=P` (P up to 10) | read access | each rule's `type`, `ruleset_id`, `ruleset_source_type`, `ruleset_source`, `parameters` |
| `compare` | `repos/O/N/compare/TIP...SHA?per_page=1&page=1` where `TIP` is the `base-branch` tip SHA and `SHA` the pull request head SHA | read access | `status`, `ahead_by`, `behind_by` |
| `check-runs` | `repos/O/N/commits/SHA/check-runs?per_page=100&page=P` | read access | per run `id`, `name`, `status`, `conclusion`, `head_sha`, `app.id` |
| `commit-statuses` | `repos/O/N/commits/SHA/statuses?per_page=100&page=P` | read access | per status `id`, `context`, `state` |
| `reviews` | `repos/O/N/pulls/NUM/reviews?per_page=100&page=P` | read access | per review `id`, `user.login`, `state`, `commit_id` |

Dependencies between sources: `branch-protection`, `branch-rules`, `compare` and the others need the `pull-request` result and, where they use `BASE`, a supported ref; `compare` needs the `base-branch` tip; `check-runs` and `commit-statuses` are read only when the required-context set is non-empty or unknown; `reviews` only when a required review count is above zero or unknown. The combined status endpoint (`commits/SHA/status`) is not used: it reported `pending` with zero statuses for a head whose required check had succeeded (measured).

### Items table (normative)

`effect`: `waits` (the evidence says the pull request is held by this), `may-wait` (could be holding it; cannot tell), `info` (context that explains behavior). Items are listed in this order, then by `subject`. "Required-set" below means the union of required status contexts from `branch-protection`, the `base-branch` summary and `branch-rules`; it is `unknown` only when none of those sources was read.

| Code | Subject | Effect and confidence | Rule |
| --- | --- | --- | --- |
| `pull-request-closed` | — | `info`, `observed` | `state` is not `open`; `detail.merged` from `merged`. All other items are suppressed and the other sources `skipped`. |
| `draft` | — | `waits`, `observed` | `draft` is `true`. |
| `merge-conflict` | — | `waits`, `observed` | `mergeable` is `false` (GitHub's computed field; `mergeable_state` `dirty`). |
| `mergeability-not-computed` | — | `may-wait`, `unknown`, reason `not-computed` | `mergeable` is `null`. |
| `required-check` | context name | per `detail.state`, below | One item per required context. `detail.state`: `pending` (matching run not `completed`, or status `pending`): `waits`, `observed`; `failing` (conclusion `failure`, `cancelled`, `timed_out`, `action_required`, `stale`, `startup_failure`, or status `failure`/`error`): `waits`, `observed`; `missing` (no matching record in a complete collection): `waits`, `observed`; `successful` (conclusion `success` or status `success`): `info`, `observed`; `neutral-or-skipped`: `info`, `inferred` (GitHub's treatment of these conclusions is documented, not measured here); `unreadable` (collection not read): `may-wait`, `unknown`. With more than one matching record from different apps or sources the state is `inferred`, not `observed`. A context with a positive pinned app id matches only check runs of that `app.id`; a null or non-positive app id matches any. Matching is exact string equality. |
| `required-checks-unknown` | — | `may-wait`, `unknown` | No source that carries the required set was read. Reason from the failed sources. |
| `strict-required-status-checks` | — | `info`, `observed` | `strict` is `true` in `branch-protection` or `strict_required_status_checks_policy` is `true` in a `required_status_checks` rule. |
| `head-behind-base` | — | below | `compare.behind_by` > 0; `detail` has `behind_by`, `ahead_by` and `strict` (`true`, `false` or `null`). Strict `true` observed: `waits`, `observed`. Strict `false` observed: `info`, `observed`. Strict unreadable and `mergeable_state` is `behind`: `waits`, `inferred`. Strict unreadable otherwise: `may-wait`, `unknown`, reason `permission-denied` or `not-found-ambiguous`. No item when `behind_by` is 0. |
| `auto-merge` | — | `info`, `observed` | `auto_merge` is not null; `detail.method` is `merge`, `squash` or `rebase` (otherwise `unknown` value, not echoed). |
| `merge-methods` | — | `info`; `observed` or `unknown` | `detail.allowed`: the methods not excluded by readable sources (repository `allow_*`, rule `allowed_merge_methods`, linear history excluding `merge`), `detail.sources`. `unknown` with reason `repository-setting-hidden` when the repository fields are `null` and no rule gives a list. |
| `auto-merge-method-not-allowed` | method | `waits`, `observed` | The `auto-merge` method is excluded by a readable source; `evidence` names that source. Never produced from an unreadable source. |
| `linear-history-required` | — | `info`, `observed` | `required_linear_history.enabled` is `true` or a `required_linear_history` rule applies. |
| `administrators-included` | — | `info`, `observed` | `enforce_admins.enabled` read from `branch-protection`; `detail.enabled` states the value. Not produced when unreadable. |
| `classic-protection-unreadable` | — | `may-wait`, `unknown`, reason `not-found-ambiguous` or `permission-denied` | The `base-branch` summary says `protection.enabled` is `true` and `branch-protection` is not `ok`. `detail.unreadable` lists the fields that only that source carries: `strict`, `enforce_admins`, `required_reviews`, `conversation_resolution`, `linear_history`, `signatures`, `lock_branch`. |
| `review-required` | — | per rule | The required count R is read from `branch-protection` or a `pull_request` rule (largest R applies). `detail` has `required`, `approvals` (distinct reviewers whose latest review is `APPROVED`) and `changes_requested` (distinct reviewers whose latest review is `CHANGES_REQUESTED`). `approvals` < R: `waits`, `observed`. `approvals` ≥ R and `changes_requested` = 0: `info`, `inferred`. `changes_requested` > 0 with R > 0: `waits`, `inferred` (documented behavior, not measured). Reviewer eligibility, code owner, stale-approval and last-push rules are not evaluated and the text says so. |
| `conversation-resolution-required` | — | `may-wait`, `unknown`, reason `rest-gap` | `required_conversation_resolution.enabled` or a `pull_request` rule's `required_review_thread_resolution` is `true`. |
| `merge-queue-required` | — | `may-wait`, `observed` | A `merge_queue` rule applies; queue state is not read. |
| `rulesets-apply` | — | `info`, `observed` | The `branch-rules` source returned at least one rule; `detail.rulesets` lists each distinct `ruleset_id` with source type, source and rule types (at most 50). |
| `rule-not-evaluated` | rule type | `may-wait`, `unknown`, reason `not-evaluated-rule` | A rule type or protection field not in the evaluated list (`required_status_checks`, `pull_request`, `required_linear_history`, `merge_queue`) and not in the irrelevant list (`deletion`, `non_fast_forward`, `creation`) applies, for example `required_signatures`, `required_deployments`, `code_scanning`, `workflows`, `update`, `lock_branch`. |
| `blocked-unexplained` | — | `may-wait`, `unknown`, reason `cause-not-readable` | `mergeable_state` is `blocked` and no item with effect `waits` and confidence `observed` or `inferred` exists. |

### Reasons table (normative)

| Reason | Meaning |
| --- | --- |
| `permission-denied` | The source answered `unauthorized` or `forbidden`. |
| `not-found-ambiguous` | The source answered `not-found` but GitHub gives the same answer for "absent" and "not permitted", so absence is not concluded. |
| `repository-setting-hidden` | The repository read succeeded but the field is `null` for this caller. |
| `rest-gap` | The state is not exposed by the REST API. |
| `not-computed` | GitHub has not computed the value yet. |
| `cause-not-readable` | GitHub reports a blocking state that none of the readable evidence explains. |
| `not-evaluated-rule` | A rule applies that this command does not evaluate. |
| `ref-unsupported` | The base ref name is outside the supported character set. |
| `rate-limited` | The source answered `rate-limited`. |
| `source-failed` | The source failed with a class other than the above; the result is `incomplete`. |
| `skipped-not-open` | The pull request is not open. |
| `not-needed` | The source was not read because no earlier evidence required it (no required contexts, no required review count). |

### Result status, diagnostics and exit status (normative)

| `status` | Diagnostic codes | Meaning | Exit |
| --- | --- | --- | --- |
| `ok` | none | Every attempted source reached `ok`, `denied`, `not-found` or `skipped`; resample unchanged; no bound exhausted. `unknown` items do not change this. | 0 |
| `incomplete` | `merge-state-changed`, `merge-state-limit`, `merge-state-source-failed` | Partial items are retained; `boundary.complete` is `false`. | 1 |
| `error` | `hosting-invalid-options`, `hosting-native-failed`, `hosting-jj-required`, `hosting-invalid-remote`, `hosting-repository-selection`, `hosting-api-unavailable`, `merge-state-pr-failed`, `hosting-canceled`, `hosting-output-limit` | No usable pull request evidence; items empty; `boundary.complete` is `false`. | 1 |
| usage error | — | Invalid arguments; no JSON. | 2 |

`hosting-*` codes keep their Spec 020 and Spec 022 meanings. A `rate-limited` optional source makes its items `unknown` with reason `rate-limited` and the result `incomplete` with `merge-state-source-failed`.

## Success Criteria

- **SC-001**: The incident fixture yields the item list of User Story 1 scenario 1 on Git and on jj, in both entry points, with the stated confidences.
- **SC-002**: Every acceptance scenario and edge case has a deterministic test; skipped or missing execution never counts as acceptance.
- **SC-003**: Tests prove the output never states mergeability: `verdict` is `not-asserted` in every fixture, and a forbidden-wording test passes over human and JSON output of every fixture.
- **SC-004**: Permission-limited fixtures (measured shapes of research.md) yield `unknown` items with reasons and never a wrong explanation; no fixture produces an absence claim from a `404`, `403` or `null` setting.
- **SC-005**: Requests made in every fixture are exactly the Sources table shapes with method `GET`; zero other requests; the path guard refuses anything else.
- **SC-006**: Effect sentinels prove refs, files, jj stored working copy and operation log are unchanged, with no hook, helper, editor or pager.
- **SC-007**: All success, incomplete and error fixtures validate offline against the new schema; `hosting.schema.json` and its fixtures are byte-identical to the base; existing contract fixtures remain valid.
- **SC-008**: The existing Git minimum and current lanes and the jj lanes pass on `ubuntu-24.04` amd64 without counting an unavailable lane as a pass.

## Scope and Dependencies

This is a bounded read-only slice of the hosting capability. It uses Spec 020's resolution and Spec 022's bounded transport, which is implemented on `origin/main` (go-gh v2.16.2, `InspectWithTransport`). Ordering: Spec 022 first (done); this feature's first work item extends the transport additively (new allowlisted request shapes and failure classes) before any operation depends on them, as stated in [plan.md](plan.md). It does not change Spec 020 or 022 contracts; it supplies evidence for later work (Spec 016 coordination) and authorizes nothing.

Out of scope, each needing its own specification: updating a pull request branch, enabling or disabling auto-merge, merging, re-running checks, approving, resolving conversations (GraphQL only), reading merge queue state, evaluating code-owner or reviewer eligibility, bypass actor analysis, polling until mergeability is computed, listing pull requests, mapping branches to pull requests, GitHub Enterprise Server acceptance, and any prediction of merge order. Constitution 1.1.0 applies with no amendment: read-only, network reads disclosed, Git and jj peers.
