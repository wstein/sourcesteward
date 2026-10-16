# Bounded go-gh transport verification

## Baseline and analysis

Baseline main ee86751 passed `PATH=/private/tmp/stw-jj-research/0.46.0:/opt/homebrew/bin:$PATH GOCACHE=/private/tmp/stw-go-cache STW_REQUIRE_JJ=1 make check` on macOS with Git 2.54.0 and jj 0.46.0. This is baseline evidence only.

Independent pre-code analysis and resolved design findings are recorded in [analysis](analysis.md). Implementation, final review and hosted CI acceptance remain pending.

## Test-first implementation and independent review

Transport tests first failed for missing SDK constructor/client helpers. Shared CLI and contract tests first failed for missing InspectWithTransport/private CLI operation seam. The lifecycle regression first reproduced a response body not closing on a transport error. Each passed after its implementation/fix.

Focused hosting race tests, vet and module verification passed in the core worktree. Real native contract/schema/effect tests passed with Homebrew Git2.54.0 and both jj0.45.1/0.46.0: ordinary/linked Git and standalone/colocated/secondary jj, success and malformed remote evidence, 35 fixture HTTP requests. These are local macOS checks, not the pinned Linux Git2.46.0/2.56.0 CI lanes.

Independent sdk_review cleared integrated transport86fa008, CLIec1c034, native fixtures80b8b40 and binary fixture changesd7ff331. Review prompted concurrent-host authentication coverage and retention of x/sys, x/term and x/text PATENTS; both are resolved. No remaining actionable source findings. Reviewer inspected source, not live GitHub/keyring behavior.

## Acceptance boundaries

Published schemas and internal/cli/testdata JSON remain byte-for-byte unchanged against main ee86751. Native adapters, Options/Result and business apiReader operations remain unchanged except the HTTP fixture seam. Positive remote acceptance uses real native operations, the SDK and fake HTTP; actual process/private extension acceptance covers local behavior and authentication refusal (28 cases), without live network.

Full integrated make check, race suite, current binary smoke and hosted minimum/current Linux CI remain pending at this snapshot. No real-keyring/Enterprise/live GitHub compatibility claim, release, user-profile extension installation or deletion is made.
