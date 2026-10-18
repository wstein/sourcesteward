# Fresh-main v3 integration work in progress

F uses isolated branch `codex/tidy-v3-cli-integration`, initially based on freshly
observed GitHub API and fetched main `2e9b9fc99888b605bcce08afa6ef98846359e06a`.
The [integration ledger](integration-ledger.json) binds every old baseline,
reviewed aggregate and fresh-main blob before import and records the integrated
blob for each path. Sixty-five paths were imported only after proving fresh main
still had the original baseline blob; five reviewed release files were already
identical on main. Five shared files received additive reconciliations instead
of old whole-file replacement. Fresh typed authentication failures, dedicated
workflow-dispatch transport and all current release schema inventories remain.

Original dependency imports and supplemental correction manifests are preserved
byte-for-byte, including the original 52-entry ownership graph. D aggregate
`e9ee81bc653c9e7cc2ea61f4e6d0ed049d2053a2` supplies frozen reference/tracking,
primary binding and the original C hosting source. E admission
`f696611d925ac6d320d66178c3bc739c9084b061` is stable provenance; pending E apply
source is not imported or wired. Legacy-specific flags stay hidden and the
workspace entry point does not execute retirement yet. This is an intermediate
source candidate, not feature acceptance, source CLEAR or activation authority.

The first F test failed on fresh main: `go test ./internal/cli -run
'^TestTidyV3UnsupportedBackendStructured$' -count=1` reported CLI code 2,
`unknown flag: --contract`, and no JSON. The explicit v3 path now returns its
versioned unsupported-backend refusal with code 1 and no executable plan.
Further test-first failures reproduced unrelated OID hints reporting code 1
instead of syntax code 2, canceled apply reaching store admission before caller
cancellation, and five explicitly empty scalar selectors reaching operations.
Each failure was fixed and its targeted test passed afterward.

Executed intermediate checks (working-tree evidence, not a frozen source gate):

- All hosting package tests passed, 3.567 seconds, including current typed
  authentication and dispatch regressions with the imported tidy readers.
- All CLI package tests passed, 8.206 seconds, before the later narrow tests;
  the final targeted `TestTidy` run passed after empty-selector fixes.
- All contracts package tests passed, 62.091 seconds, before the new F native
  contract test was added. That new test passed independently in 1.734 seconds.
- The real v3 CLI contract flow uses 695 tracked files and base metadata beyond
  the old 64 KiB limit, one selected branch, dirty primary document and ignored
  cache, unrelated-OID syntax refusal before HTTP, explicit online/local apply
  acknowledgment, exact native recovery/deletion, unchanged completed retry,
  moved-target stale refusal and byte-preserved primary user files.
- Public entry-point apply/recovery/retry and canceled-before-store tests passed
  in 1.166 seconds. Original operation deadline covers dispatch/bootstrap;
  coordinator subcontexts cannot reset it.
- Thirteen architecture unit tests, eight source-only Hugo tests, the unpublished
  schema-copy test and `git diff --check` passed. The Hugo tests operate on
  synthetic HTML/source inventories; no Hugo process or site build ran.

This Linux arm64 session has Git 2.56.0, jj 0.46.0 and Go 1.27.2. Other pinned
versions/platforms have not been executed here. Full frozen normal/race gates,
remaining source gates, final E retirement integration, realistic combined
retirement-then-ref acceptance and independent exact-head review remain pending.
The actual Hugo packaging/build test requires separate authorization and will be
excluded by exact test identity if still unauthorized; Node availability will
be reported separately. No build/install, live HTTP, real cleanup, push, hosted
CI, tag, release, publication, settings or submodule gitlink change ran.


## Early exact-source review corrections

Independent early F review of `1b592323a212bf9889d0aad98de83c3d78542dbe`
was NOT CLEAR: one High fixture-environment escape, one Medium repeated-singleton
syntax violation and one Low missing-host-Git classification. Review evidence is
`/tmp/stw-review-f-cli-evidence/review.json`; this was not final Spec 029 acceptance.

Failing-first corrections are retained under
`/tmp/stw-f-review-fixes-ff607zjf/red-receipt.json` and `green-receipt.json`.
The fixture regression uses only disposable external sentinel repositories and
controlled hook scripts. It first reproduced changed sentinel refs/config/index
and hook execution. Every new F fixture Git invocation, including standalone
show-ref, now uses the same canonical executable and an explicit isolated
HOME/config/template/noninteractive environment rather than inherited Git controls.
The child test reruns the actual 695-file flow with injected repository/index and
config/hooks/template controls and verifies unchanged external bytes and hook markers.
The fixture now measures that its complete metadata really exceeds 64 KiB.

The parser counts all singleton occurrences through their existing typed flag
values and rejects every repeated scalar/bool singleton only on explicit v3.
Spaced/equal and repeated-identical inputs are covered; repeatable refs/PR hints/
protections and default v1/v2 last-value semantics remain covered. Missing Git is
an explicit optional-local skip, not acceptance, while the existing mandatory
native-sandbox/jj conformance controls force failure. Product admission is unchanged.
Focused correction checks passed: CLI 0.015 seconds; real flow/sentinel/availability
contracts 4.791 seconds. Same-reviewer exact-source re-review and final E port/full
normal/race gates remain pending.


## Reviewed admission and registration-model preparation

The prior F candidate `34a1430bb1b256d070da0a217df870097cd290a4` received
CONTENT-CONDITIONAL CLEAR with zero open findings from the same independent
reviewer (`/tmp/stw-review-f-cli-r2/review.json`). It remains an immutable
historical source-content checkpoint; later integration does not inherit that
clearance. No additional checkpoint branch was created.

The finite [registration model layer](registration-model-import.json) imports
all eight exact source blobs from SOURCE CLEAR
`fb4ddf177b52275d3f01162eb39b58bee48abd5a`. Required registration parent and
sibling facts reach model/schema/store fixtures and the evidence-only collector
constructor. Its source-provenance SHA-256 and each source/old/new blob are retained.
The [admission dependency layer](admission-dependency-import.json) supplies only
15 already-reviewed admission/content/contract dependencies from SOURCE CLEAR
`f696611d925ac6d320d66178c3bc739c9084b061`, using the exact fb4ddf1 collector
transport. Seven primary bindings already present from D were verified identical.
The earlier regular-sibling and `.git`-pointer accounting model prerequisites
are explicit; no stale whole-tree or local-only apply overwrite occurred.

Original import/correction manifests and the original 75-row import history stay
intact, with new layers naming their later overrides. Fresh-main release SDK,
authentication, dispatch transport and unpublished schema inventories, all prior
F guard fixes and D tracking apply behavior remain present. Admission/content
and the constructor collect evidence only. Pending E removal/coordinator/runner
code is absent and retirement CLI flags remain hidden/unwired.

Intermediate focused checks passed on the imported working tree: model/schema
workspace 2.761 seconds/contracts 0.082 seconds; admission/content/model checks
workspace 3.482 seconds/contracts 0.076 seconds. Native fixture checks were launched
with inherited Git/jj/Rust controls removed; these are targeted source checks,
not final full-suite, native-platform acceptance or new independent clearance.
