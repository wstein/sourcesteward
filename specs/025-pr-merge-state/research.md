# Research: Pull request merge-state explanation

Date of all measurements: 2026-10-16, 13:06 to 13:30 UTC, `gh` 2.102.0 (`gh api --method GET`, read-only), authenticated as `wstein` with scopes `repo`, `read:org`, `workflow`, `gist`, `project`; anonymous results used `curl` without a token. No token was printed. `wstein` is admin on `wstein/*` repositories and a read-only user (`permissions.pull` only) on `cli/cli`, `cli/go-gh` and `github/docs`, which gave both the admin and the "normal token" view. No GraphQL call and no mutating call was made. Facts below are **measured** unless marked **documented** (GitHub behavior stated in its documentation or known, not re-read or reproduced in this session) or **unverified**. Raw responses stayed in the author's scratch directory and are not part of the repository.

## Decisions and rated alternatives

### Command and contract shape

Scores 1 (poor) to 5 (good). Criteria: keeps `hosting.schema.json` unchanged (C), one discoverable place (D), reuses Spec 020 selection and ownership (R), room to grow without breaking consumers (G).

| Option | C | D | R | G | Verdict |
| --- | --- | --- | --- | --- | --- |
| A. New leaf `hosting merge-state NUMBER` with new schema `merge-state.schema.json` | 5 | 5 | 5 | 5 | **Chosen** |
| B. Flag on `hosting pr` (for example `--explain`) adding fields | 1 | 4 | 5 | 2 | Rejected: changes the existing JSON for one command and needs a new network read path under a flag |
| C. New `kind` value in the hosting schema | 1 | 4 | 5 | 2 | Rejected: the `kind` enum is closed; consumers dispatch on it; the requirement says the schema stays unchanged |
| D. Top-level `stw merge-state` | 5 | 2 | 2 | 4 | Rejected: duplicates `--repo/--host/--remote` selection outside the group that owns remote reads and its architecture allowance |
| E. Extend `hosting ci` | 2 | 3 | 5 | 1 | Rejected: CI is one of nine sources; the contract says CI output never claims merge eligibility |

### Evidence strategy

| Option | Verdict |
| --- | --- |
| REST composite of nine fixed reads (chosen) | Every read is a fixed `GET` within Spec 020 FR-006; every field has a measured shape below. Gap: REST has no review-thread resolution (measured), so conversation resolution is `unknown` with reason `rest-gap`. |
| GraphQL (`mergeStateStatus`, `reviewThreads.isResolved`, merge queue entry) | Richer, and the documented upgrade path for the REST gap. Rejected here: Spec 020 forbids GraphQL and Spec 022's transport allows only the three GET shapes. Adopting it needs an amendment of both contracts and its own specification. **Unverified** here (no GraphQL call was made). |
| `gh pr view/checks --json` | Starts a process, uses GraphQL, bypasses the bounded transport. Rejected. |
| Rely on `mergeable_state` alone | One field, but its cause is not named and it was stale or unexplained in measured data (M2). Rejected as sole evidence; echoed under `github_reported`. |
| Read only the pull request and report GitHub's state | Cannot explain the incident. Rejected. |

### Treating unreadable evidence

**Decision**: a permission-gated or ambiguous answer is `unknown` with a reason and never evidence of absence. **Rationale**: the measured non-admin answer for branch protection of a protected branch is `404 Not Found` (M6), and the repository merge-method fields are `null` (M10); reading either as "no rule" or "method allowed" would produce a wrong explanation. **Alternative rejected**: parse the `404` message text to tell "not protected" from "not permitted" (admin gets `Branch not protected`, a non-admin gets `Not Found`): message text is not a contract and the non-admin unprotected case was not measured.

### Never claiming mergeability

**Decision**: the result carries `verdict: "not-asserted"` as a constant; GitHub's `mergeable` and `mergeable_state` appear only under `github_reported`; a pull request with no waiting items says only that none was found in the evidence read. **Rationale**: measured pull request 112 reported `mergeable: true` and `mergeable_state: blocked` with every readable condition satisfied (M2); the state is computed lazily, can be `null`/`unknown` (M5) and cannot be treated as a prediction. **Alternative rejected**: a boolean "can merge": cannot be backed by read evidence (bypass actors, queue state, reviewer eligibility, thread resolution are unreadable).

### Transport requirements (neutral) and relation to Spec 022

Spec 022 is **implemented on `origin/main`** (`go.mod` pins `github.com/cli/go-gh/v2 v2.16.2`; `internal/hosting/transport.go` has `newGHWithTransport`, `guardedTransport`, `InspectWithTransport`; `specs/022-bounded-go-gh/spec.md` status "Implemented"; `git log origin/main` lists "build(deps): pin official go-gh v2.16.2"). The Spec 025 request described it as in flight; this record states what the code shows. Facts read from the code at `45a3c8b` (not run):

- `fixedEndpoint` is a closed regular expression allowing only `repos/O/N/pulls/NUM`, and `commits/SHA/check-runs|statuses?per_page=100&page=1..10`. Every Spec 025 source (`repos/O/N`, `branches/BASE`, `branches/BASE/protection`, `rules/branches/BASE`, `compare/…`, `pulls/NUM/reviews`) is outside it and needs an additive allowlist change.
- `ghAPI.get` returns a generic error for every non-2xx status and never exposes the status. The failure classes the spec needs (`forbidden`, `not-found`, …) are therefore not available today. The change is additive: a typed error carrying the class, with unchanged message text so the Spec 020/022 diagnostics stay identical.
- `guardedTransport` refuses any request whose `URL.RawPath` is non-empty. A percent-encoded `%2F` in a branch path sets `RawPath`, so encoding would have required relaxing a security guard. Literal slashes work (M15), so the spec validates the ref name and sends literal slashes.
- Bodies are capped at 1 MiB plus a sentinel byte; redirects and compressed responses are refused; the credential goes only to the selected API origin. All of this is reused unchanged.
- `readCI` decodes check runs without the app id (`decodeCheck`), and the published `Check` has no app field. Matching pinned required contexts needs `app.id`, which M2 and M8 show is present. The shared decoder must keep the published `Check` and `hosting.schema.json` unchanged and expose the app id to the new operation only.
- `inspect()` is one function that validates options, resolves remotes, selects the repository and creates the API client. The new operation needs the same prelude; extracting it as an internal function with unchanged behavior is a prerequisite refactor, guarded by the existing tests.

**Decision**: the spec states transport requirements (FR-013), not a mechanism, so they hold under the Spec 020 `gh api` transport or any successor, where the HTTP class is derivable except the header-based rate-limit cases (`gh api` does not print response headers unless asked; `HTTP NNN` text was seen on standard error for a `404`), so a subprocess transport may classify those as `other-http`: fixed GET, closed allowlist, bounds, failure classes, no redirects, selected-host credentials. They add to Spec 022's contract and never weaken it. **Ordering**: Spec 022 is a prerequisite and is already on `main`; the first work item here extends its transport additively before any operation uses the new shapes. **Rate limiting**: the class `rate-limited` (HTTP 429, 403 with `X-RateLimit-Remaining: 0`, or 403 with `Retry-After`) is **documented, not measured** (limit headers were seen, no limit was hit).

### Request budget

One explanation needs: pull request, repository, base branch, protection, compare, resample of pull request and of base branch (7 fixed), plus pages of rules, check runs, statuses and reviews (at most 10 each). Designed typical cost for a pull request like 112 (one page per collection): 11 requests (derived from the design, not measured; the authenticated rate limit was 5000 per hour). The bound is 50 requests and 30 seconds, below Spec 020's whole-operation deadline.

### Check-run matching

Required contexts match by exact name. Measured: classic protection gives `checks: [{context, app_id}]` with `app_id` 15368 (github-actions) for `wstein/bundledock` and `null` for `cli/cli`; rulesets give `required_status_checks: [{context, integration_id}]`. Check runs carry `app.id` (M12). The effect of `app_id: -1` and the exact treatment of `neutral`/`skipped` conclusions are **documented, unverified**: `neutral-or-skipped` is therefore `inferred`. Duplicate names occur (23 runs, 18 names for one `cli/cli` head; `filter=all` returned the same 23; pull request 112's head later had two `check` runs from the same app in different suites), so the newest record by `id` wins per app, and more than one competing record lowers confidence to `inferred`.

## Measurements

**M1. Pull request fields** (`GET repos/wstein/bundledock/pulls/112`, 13:06 UTC, admin and anonymous both `200`). 48 top-level keys including `state`, `merged`, `draft`, `locked`, `mergeable`, `mergeable_state`, `rebaseable`, `auto_merge`, `merge_commit_sha`, `requested_reviewers`, `head`, `base`. Values: `state` open, `draft` false, `mergeable` true, `mergeable_state` `blocked`, `auto_merge` null, head `f709f8b…` (same repository), base `f7fa77c…`.

**M2. The measured incident-adjacent pull request was not explained by the evidence the author read.** Pull request 112 (bot-authored, `github-actions[bot]`) at 13:06 UTC and again 20 seconds later: `mergeable_state` `blocked`, `updated_at` 12:17:11Z. Admin reads: protection `required_status_checks.strict` true with `contexts ["check"]`, `checks [{context: check, app_id: 15368}]`, `enforce_admins.enabled` true, `required_linear_history.enabled` true, `required_conversation_resolution.enabled` false, `required_signatures.enabled` false, `lock_branch.enabled` false, no `required_pull_request_reviews` key (no review requirement); repository `allow_rebase_merge` true, `allow_merge_commit` false, `allow_squash_merge` false, `allow_auto_merge` true, `allow_update_branch` true; `rules/branches/main` `[]`; `rulesets` `[]`; reviews `[]`; compare of branch tip `f7fa77c…` with head: `ahead_by` 1, `behind_by` 0; check runs at head: one run named `check`, `completed`, `success`, `app.id` 15368, completed 12:18:41Z; statuses `[]`; combined status endpoint: `state` `pending`, `total_count` 0. Workflow runs for that head: `CI` on `pull_request` with conclusion `action_required` and `CI` on `workflow_dispatch` with `success`. So every condition SourceSteward can read is satisfied yet GitHub reports `blocked`; the cause is **unverified**. Hypotheses, none tested: a stale cached state computed before the check finished (12:18:41Z) or a relation to the `action_required` run. Afterwards (independent re-measurement, not reproduced by the author): pull request 112 merged at 13:11:59Z; its timeline shows `auto_rebase_enabled` at 13:09:23, a second `check` run (same app 15368, another check suite) started 13:09:21 and completed 13:10:40, and `commits/SHA/check-suites` (read access) lists the `pull_request` suite behind the `action_required` run. The timing suggests the workflow approval mattered, but the cause is still unproven. A `check-suites` source (workflow approval pending) is a candidate extension and is listed as an open question, not a requirement. This is why `blocked-unexplained` exists with `unknown` confidence, and why `mergeable_state` is never promoted to a conclusion.

**M3. `auto_merge`** on merged pull requests 108–110 of the same repository: `{commit_message, commit_title, enabled_by{…user}, merge_method: "rebase"}`; `merge_method` is the field to use. It stays present after the merge. Merged pull requests report `mergeable` null and `mergeable_state` `unknown`; merged pull requests 100–107 likewise.

**M4. Draft pull requests never reported `mergeable_state` `draft`.** `cli/cli`: six drafts reported `blocked`; `wstein/sourcesteward` #44 draft reported `clean`; `wstein/sheetmill` #38 draft reported `unstable`. The `draft` field is the evidence.

**M5. Distribution and laziness.** 25 open `cli/cli` pull requests: 17 `blocked` (`mergeable` true), 5 `dirty` (`mergeable` false, `rebaseable` false), 3 `unknown` with `mergeable` null and empty `rebaseable`; the three stayed `unknown` on a re-read 5 seconds later. 11 open `github/docs` pull requests: 3 `blocked`, 8 `unknown`. The values `behind`, `has_hooks` and `draft` were never observed (documented to exist; **unverified** here).

**M6. Branch protection endpoint** (`GET repos/O/N/branches/BASE/protection`). Admin on a protected branch: `200` with `required_status_checks{strict, contexts, checks[{context, app_id}]}`, `required_signatures{enabled}`, `enforce_admins{enabled}`, `required_linear_history{enabled}`, `allow_force_pushes`, `allow_deletions`, `block_creations`, `required_conversation_resolution`, `lock_branch`, `allow_fork_syncing`; `required_pull_request_reviews` appears only when configured (absent for `wstein/bundledock`; its shape was **not observed**). Admin on an unprotected branch (`wstein/sourcesteward` `main`; a feature branch): `404` `Branch not protected`. Read-only user on branches that `branches/BASE` reports as protected (`cli/cli` `trunk` with classic protection; `cli/go-gh` and `github/docs`, ruleset-only): `404` `Not Found`. Anonymous: `401` `Requires authentication`. Read-only user on an unprotected branch: **not measured**. Conclusion: for a non-admin, a `404` is ambiguous.

**M7. Branch summary** (`GET repos/O/N/branches/BASE`, anonymous `200` on a public repository). Fields `name`, `commit.sha`, `protected`, `protection`, `protection_url`. `protection` is `{enabled, required_status_checks{enforcement_level, contexts[], checks[{context, app_id}]}}`: `wstein/bundledock` `enabled` true, `enforcement_level` `everyone`, contexts `[check]`; `cli/cli` `trunk` `enabled` true, `non_admins`, three contexts with `app_id` null; `cli/go-gh` and `github/docs` `protected` true, `enabled` false, `enforcement_level` `off`, no contexts (ruleset-only). The summary has no `strict`, `enforce_admins`, review or conversation field.

**M8. Rules for a branch** (`GET repos/O/N/rules/branches/BASE`). `200` for a read-only user and anonymously; `[]` when none apply (`wstein/bundledock`). Entries: `{type, ruleset_source_type, ruleset_source, ruleset_id, parameters}`. Types seen: `deletion`, `non_fast_forward`, `required_status_checks`, `pull_request`, `merge_queue`, `copilot_code_review`, `license_compliance_scanning`. `required_status_checks.parameters`: `strict_required_status_checks_policy`, `do_not_enforce_on_create`, `required_status_checks[{context, integration_id}]`. `pull_request.parameters`: `required_approving_review_count`, `dismiss_stale_reviews_on_push`, `require_code_owner_review`, `require_last_push_approval`, `required_review_thread_resolution`, `allowed_merge_methods`, others. `merge_queue.parameters`: `merge_method`, `grouping_strategy`, entry counts and timeouts. The list is paginated: `per_page=2` returned a `Link: …rel="next"` header and `per_page=1&page=2` returned a second record; there is no total count, so exhaustion is "a page shorter than the page size". The transport returns bodies only, so exhaustion must be inferred that way.

**M9. Rulesets list and detail** (`GET repos/cli/go-gh/rulesets`, `…/rulesets/ID`) answer `200` for a read-only user; the detail has `bypass_actors: null` and `current_user_can_bypass: "never"`. Bypass analysis is out of scope; the `rules/branches` endpoint already gives the rules that apply, and `evaluate`-mode rulesets are not asserted to appear there (**unverified**).

**M10. Repository merge settings** (`GET repos/O/N`). Admin (`wstein/bundledock`): `allow_merge_commit` false, `allow_squash_merge` false, `allow_rebase_merge` true, `allow_auto_merge` true, `permissions.admin` true. Read-only user (`cli/go-gh`): all four `allow_*` fields and `allow_update_branch` are `null`, `permissions` `{admin: false, maintain: false, pull: true, push: false, triage: false}`. A user with push but not admin access: **not measured**.

**M11. Compare** (`GET repos/O/N/compare/TIP...SHA?per_page=1&page=2`; page 1 was measured first and is rejected below). Fields `status`, `ahead_by`, `behind_by`, `total_commits`, `merge_base_commit`, `commits`, `files`. For `cli/cli` #14451 the branch tip `b758ce3…` against the head: `diverged`, `ahead_by` 11, `behind_by` 194; the pull request's own `base.sha` `2761962…` is stale (not the tip) and comparing with it gave `ahead` 7, `behind_by` 0. So `behind_by` must be computed against the branch tip, not `base.sha`. For `wstein/bundledock` #112: `ahead` 1, `behind` 0. On page 1, `per_page=1` limited `commits` to one but `files` still listed all 33 entries (177 KB in the independent re-measurement, 228 KB in the first), which can exceed 1 MiB for a large change. On `page=2` the `files` list is empty and `status`, `ahead_by` and `behind_by` are unchanged (15.7 KB for the same pull request, verified in both measurements), so the allowlisted shape uses `page=2`. An oversized response is still `too-large` and the item `unknown`. A fork pull request (`cli/cli` #14700, head repository a fork) compared by SHA in the base repository: `diverged`/ahead 1, behind 74.

**M12. Check runs** (`GET repos/O/N/commits/SHA/check-runs?per_page=100`): `total_count`, `check_runs[]` with `id`, `name`, `status`, `conclusion`, `head_sha`, `app.id`, `app.slug`, `started_at`, `completed_at`. See also the duplicate-name measurement above.

**M13. Commit statuses and combined status.** `commits/SHA/statuses` returns `[]` when there are none. The combined `commits/SHA/status` returned `state` `pending` with `total_count` 0 for a head with a successful required check run. That is GitHub's documented combined-status semantics (statuses only, latest per context), not a defect; it is not used because it carries no ids and ignores check runs. Raw `statuses` return the whole history (the independent re-measurement found `EasyCLA` both failure and success on one head and 135 records for 3 contexts on another), so the newest status per context wins.

**M14. Reviews and conversations.** `pulls/NUM/reviews` is an array with `user.login`, `state` (`COMMENTED` observed by the author; the independent review observed an `APPROVED` review followed by a `COMMENTED` review from the same reviewer on a pull request merged under a rule requiring one approval, so `COMMENTED` must not withdraw an approval; `CHANGES_REQUESTED` and `DISMISSED` are documented, **unverified**), `commit_id`, `author_association`. `pulls/NUM/comments` objects have no resolved or thread field (keys listed from a real response). GraphQL `reviewThreads` was not called: conversation resolution is `unknown` with reason `rest-gap`.

**M15. Base ref names with `/`.** `branches/publication-docs%2Fclaude%2F2.1.301-b1` and the literal `branches/publication-docs/claude/2.1.301-b1`, `rules/branches/…` both forms: `200`; `…/protection` both forms: `404 Branch not protected` (admin, unprotected). Literal slashes are sufficient.

**M16. Failure modes seen.** Nonexistent pull request: `404`. Anonymous protection: `401`. Rate-limit headers `X-Ratelimit-Limit: 5000`, `X-Ratelimit-Used`, `X-Ratelimit-Resource: core` are present on responses. Not exercised: `429`, a `403` rate-limit or abuse answer, a fine-grained token missing a permission (documented to answer `403` or `404` with a message such as `Resource not accessible by personal access token`), SAML-restricted organizations, `5xx`.

## Unverified and untestable here

- A repository where the caller has push but not admin access (are `allow_*` fields then readable? Documented as readable with push access).
- Admin view of `required_pull_request_reviews` (shape, `required_approving_review_count`), code owner and last-push rules, and a repository with required conversation resolution enabled.
- `mergeable_state` values `behind`, `has_hooks`, `draft` and their relation to strict rules.
- Whether `evaluate`-mode rulesets and bypass actors appear in `rules/branches`.
- Merge queue behavior for auto-merge and for the pull request fields.
- GitHub Enterprise Server and ghe.com hosts (endpoint availability, `rules/branches` on older Enterprise versions).
- GraphQL equivalents (not called).
- The cause of the `blocked` state of pull request 112 (M2).
- Behavior with `app_id: -1`, and GitHub's treatment of `neutral` and `skipped` conclusions for required checks.
- `429` and secondary rate limit handling, `5xx`, and responses above 1 MiB for the compare endpoint.
- Whether a read of a pull request triggers GitHub's server-side mergeability computation (documented behavior, not observed; the 5-second re-read of `cli/cli` pull requests did not change `unknown`).

## Git, jj and gh

`gh` 2.102.0 was used for measurements only; the product transport does not run `gh api` (Spec 022). Git 2.56.0 was present but no Git or jj repository behavior is measured by this research: the Tier 1 resolution is Spec 020's, and the new operation reuses it unchanged (read from `internal/hosting/operations.go`: `inspection.Remotes` and `inspection.Inspect` serve both backends; `RequireJJ` is set only by `gh jj`). Conformance for the reuse is a task (T-series in tasks.md), not a measured fact.
