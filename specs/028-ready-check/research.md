# Research: Ready evidence and deployment trust

**Date**: 2026-10-18. Read-only inspection; no infrastructure or settings changed.

## Existing source, measured

Inspected source `279865a525be13684e989e8c1d936c0cdff2269b`; dispatcher subsequently verified identical tree at main `9a704dbaf166ccc2bca57fce890b44cdc239374f`. Spec candidate is `4dbfd8e557148f315daf8c00c932d9f5a6b5aa46`.

- `internal/cli/hosting.go:26` registers only repo/pr/ci. `internal/hosting/operations.go:126` samples head CI and rechecks PR identity. `:236` already reads full statuses, but decoders at `:267`/`:285` discard App/creator metadata.
- `internal/hosting/transport.go:210` has the closed PR/check/status endpoint allowlist; `:256` enforces GET, HTTPS, selected origin, no redirect/path escape/compression and byte limits. Richer typed readers must extend this boundary explicitly.
- `internal/cli/hosting_transport_test.go:54` provides real native discovery with HTTP fixtures; operations tests cover moved heads, incomplete pages and malformed identities.
- `scripts/check-architecture.py:16` protects the CLI operation boundary; `:71` restricts Go network imports to hosting. A pure evaluator must gain equivalent effect/import checks.
- Spec 025 FR-006 deliberately forbids readiness assertions. Its required-set/unknown-source design is useful, but explanatory inference is not proof for this new normative contract.
- Installed CrewBook project-config review-note grammar is `CLEAR <full sha> role=<role> model=<model>`; the guidance explicitly calls it a self-report, not a signature. No grammar change is proposed. The current host exposes configured model metadata, not independently attested runtime identity.

## Live repository observations, limited scope

Read-only `gh api` on `wstein/sourcesteward` returned public visibility, owner type User, default branch main, rebase enabled, merge/squash disabled. Repository rulesets list was empty. Environment listing showed only github-pages. Classic branch-protection GET returned 404 "Branch not protected" using a token with reported admin permission. These observations do not prove enduring absence; the evaluator still requires branch-summary/effective-rule evidence and fresh reads.

No full App-installation inventory was collected. Existing GitHub Actions checks or its App ID do not establish that no other App exists. No alternative profile was installed or tested. A new App is a possible owner choice, not a demonstrated unavoidable prerequisite.

## Official sources and implications

1. [Commit statuses REST API](https://docs.github.com/en/rest/commits/statuses) documents creator metadata in full status records. Combined status examples can omit creator; use the full endpoint and require metadata rather than accepting its absence.
2. [Required-check rules](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets) permit selecting an expected GitHub App source. This binds App identity, not a particular Actions workflow. Inference: generic Actions App identity alone cannot distinguish trusted baseline logic from a same-name PR workflow.
3. [Required workflows](https://docs.github.com/en/enterprise-cloud%40latest/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets) are configured at organization/enterprise level. The measured User-owned target cannot assume that capability as a drop-in option; ownership/plan/source-binding acceptance is needed.
4. [Secure pull_request_target](https://docs.github.com/en/actions/reference/security/securely-using-pull_request_target) describes default-branch workflow execution and warns against untrusted PR execution with privileged tokens. Public-repository event-policy availability is changing; inspect actual rollout eligibility rather than relying on this date's default.
5. [Workflow events](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows) documents default-branch-only event cases and suppression of recursive Actions check events. A publisher must specify and test actual relevant status/comment/PR/base/activation triggers. A list of desired triggers is not proof they arrive.
6. [Required-check troubleshooting](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks) distinguishes head and test-merge checks, same-name check/status requirements and workflow-job event restrictions. External App checks have different event restrictions. V1 stays conservative about exact-head proof and does not claim all GitHub CI combinations are supported.
7. [Pull request REST API](https://docs.github.com/en/rest/pulls/pulls) exposes lazily computed mergeability. This design additionally requires explicit REST rebaseability under its rebase profile; missing/null is not retried into a passing value. No local merge simulation substitutes for hosting evidence.
8. [Environment protections](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments) bind access to deployment rules and describe secret access. Inference: an environment can help isolate credentials, but its name alone does not prove source trust. Actual event/ref restrictions and root-change controls need acceptance.
9. [CODEOWNERS](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners) and [protected branches](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches) offer human-mediated source review mechanisms. Their exact suitability and approval eligibility are not established here. The v1 REST evaluator does not pretend to verify code-owner eligibility.

10. [Environment variables REST API](https://docs.github.com/en/rest/actions/variables#get-an-environment-variable) requires Environments read for reads and Environments write for updates, and documents collaborator access. A named environment variable is not owner-only authorization proof. Channel write isolation is an unverified deployment acceptance requirement; absent that proof the proposed channel is unsupported.

These references are primary documentation, not hosted acceptance of this implementation. Repository settings, platform plans and event support must be resampled before deployment.

## Decisions and unresolved deployment acceptance

Prefer the existing bounded transport and pure Go evaluation; keep publisher writes outside the CLI. Full-PR reports and literal prefix rules avoid an unbounded policy language. Public reviewer labels and issuer declarations avoid unnecessary session/transcript disclosure. Exact byte hashes avoid ambiguous report mutation handling.

No material infrastructure question is needed to prepare source contracts. Before rollout, an independent reviewer evaluates the deployment matrix against current capabilities, including native/human-mediated/no-new-App alternatives and reuse of an existing App. Only then does the coordinator present a concrete owner choice with exact privileges, settings and acceptance gaps. Until that decision and its acceptance, deployment stays pending.
