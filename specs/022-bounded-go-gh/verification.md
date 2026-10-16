# Bounded go-gh transport verification

## Baseline and analysis

Baseline main ee86751 passed `PATH=/private/tmp/stw-jj-research/0.46.0:/opt/homebrew/bin:$PATH GOCACHE=/private/tmp/stw-go-cache STW_REQUIRE_JJ=1 make check` on macOS with Git 2.56.0 and jj 0.46.0. This is baseline evidence only.

Independent pre-code analysis and resolved design findings are recorded in [analysis](analysis.md). This paragraph records the baseline snapshot; implementation/review and subsequent acceptance are recorded below.

## Test-first implementation and independent review

Transport tests first failed for missing SDK constructor/client helpers. Shared CLI and contract tests first failed for missing InspectWithTransport/private CLI operation seam. The lifecycle regression first reproduced a response body not closing on a transport error. Each passed after its implementation/fix.

Focused hosting race tests, vet and module verification passed in the core worktree. Real native contract/schema/effect tests passed with Homebrew Git 2.56.0 and both jj 0.45.1/0.46.0: ordinary/linked Git and standalone/colocated/secondary jj, success and malformed remote evidence, 35 fixture HTTP requests. These are local macOS checks, not the pinned Linux Git 2.46.0/2.56.0 CI lanes.

Independent sdk_review cleared integrated transport 86fa008, CLI ec1c034, native fixtures 80b8b40 and binary fixture changes d7ff331. Review prompted concurrent-host authentication coverage and retention of x/sys, x/term and x/text PATENTS; both are resolved. No remaining actionable source findings. Reviewer inspected source, not live GitHub/keyring behavior.

## Acceptance boundaries

Published schemas and internal/cli/testdata JSON remain byte-for-byte unchanged against main ee86751. Native adapters, Options/Result and business apiReader operations remain unchanged except the HTTP fixture seam. Positive remote acceptance uses real native operations, the SDK and fake HTTP; actual process/private extension acceptance covers local behavior and hermetic authentication refusal (28 cases), without live network.

Current binary smoke passed all 28 cases using the final core and integrated sources. Full integrated make check/race pass; hosted minimum/current Linux CI remains a separate pending handoff gate. No real-keyring/Enterprise/live GitHub compatibility claim, release, user-profile extension installation or deletion is made.

## Final local acceptance and PR handoff

At integrated source 296298c (subsequent changes are evidence-only), the following passed on macOS with Homebrew Git 2.56.0, gh 2.102.0 and pinned jj 0.46.0:

- `PATH=/private/tmp/stw-jj-research/0.46.0:/opt/homebrew/bin:$PATH GOMODCACHE=/private/tmp/stw-sdk-mod-cache GOCACHE=/private/tmp/stw-go-cache STW_REQUIRE_JJ=1 make check`: all Go tests/vet/module integrity, 57 Python tests, 12 theme cases, docs 175, architecture and whitespace. An earlier overlapping run loaded the obsolete pre-migration gh-api contract fixture and failed; the final integrated run includes its SDK replacement and passes.
- Same environment, `go test -count=1 -race ./...`: all packages pass.
- `make build extension` and `python3 -B scripts/smoke-hosting.py`: 28 real native binary/private-extension cases pass; byte/mode-identical state and direct cases audited against a fake gh, dispatched cases hermetic (unresolvable `*.invalid` host, no credential, `hosting-api-unavailable`, `boundary.network` false). Local test executables stay ignored.
- Same Go/cache/native environment with jj 0.45.1: `go test -count=1 -race ./internal/cli ./internal/contracts ./internal/hosting -run 'TestHostingSDKEntryPoints|TestRealHostingPRAndCIContracts|TestRealJJHostingContracts|TestSDK|TestAuthentication|TestConcurrentHosts'` passes. Minimum-jj final binary smoke also passed in the compatibility worktree.
- `git diff --exit-code ee86751 -- schemas internal/cli/testdata internal/inspection` passes: published schemas, JSON fixtures and native adapters are byte-identical.

Final independent consistency analysis clears source 296298c; low stale sentence/task-path corrections are included in this handoff. [PR 41](https://github.com/wstein/sourcesteward/pull/41) is separate from merged PR40. Hosted exact-head CI, including pinned Linux Git 2.46.0/jj 0.45.1 and Git 2.56.0/jj 0.46.0 conformance, remains pending at source handoff. This local evidence does not substitute for those CI lanes. No owner merge or release was performed.

## Notice-only follow-up

All six source/minimum/current checks on b6edd4a passed in [PR CI 38888310316](https://github.com/wstein/sourcesteward/actions/runs/38888310316) and push CI 38888303686; Pages correctly skipped for the topic branch. CI reported no findings, but independent team review of b6edd4a was NOT CLEAR (three reviewers): one high (the smoke used the keyring credential and live network), several medium (result-code change, evidence Git version, commit hygiene) and several low findings. All were fixed forward in later commits or decided by the owner (H62:a, H63:a).

Repeated dependency notices were consolidated: 18 files/401 lines become 7 files/189 lines. Each MIT copyright is retained with one whitespace-equivalent permission/disclaimer text; Go BSD and patent copies were byte-identical and now shared once. Distinct safeexec, timezone and YAML license/NOTICE contents remain byte-identical. The version-to-license inventory remains explicit. No production code, safety test, schema, module pin or workflow changed.

The follow-up passed attribution comparison against every original notice, `make check` with the same current-jj/native environment as above, documentation checks and `git diff --check`. Hosted CI for the new notice-only head is checked at PR handoff; the earlier green source evidence is unchanged.

## Merged status (2026-10-16)

PR 41 is merged into main (this documentation refresh starts at dfd6035). All six
final-head checks for 0825640 passed in PR CI 38891155861 and push CI 38891151895.
Earlier pending CI/merge statements above are historical handoff snapshots.

## Post-merge review follow-up

Independent review of b6edd4a was not clear; the fixes land in a follow-up PR (Refs #41), not in merged PR 41. The binary smoke is now hermetic: dispatched cases use an unresolvable host and assert `hosting-api-unavailable` with `boundary.network` false. The independent reviewer ran it in a scratch copy with jj 0.46.0 and all 28 cases passed. jj 0.45.1 and the Linux lanes remain for CI to prove.
