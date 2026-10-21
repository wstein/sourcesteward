# Spec 010 verification

## Design and baseline

Plan/tasks preceded source implementation. Independent analysis cleared `6d3e55e`;
[analysis](analysis.md) records 15/15 requirement coverage and resolved findings.
No extension hooks are configured; the specification quality checklist is complete.

Baseline `e67ae90` passed `make check` in the new implementation worktree with
GOCACHE=/private/tmp/gitsteward-go-cache, GOMODCACHE=/private/tmp/gitsteward-go-mod and
HUGO=/private/tmp/sourcesteward-hugo-docs/.tools/hugo. This included Go tests/vet/module
verification, 37 Python tests, 12 theme cases, 123 authored docs and architecture/whitespace.
This establishes baseline source health, not Spec 010 acceptance.

## Native research

Backend authors independently experimented in disposable fixtures before implementation.
Git worktree-add no-checkout still invokes reference-transaction hooks; full creation
therefore requires effective helper/hook admission rather than relying on no-checkout.
Native Git detached full checkout internally invokes destination-only reset; no source
reset, branch creation or hook suppression is authorized.

jj 0.45.1 and 0.46.0 two-phase experiments in disposable fixture
/private/tmp/stw-010-jj-two-phase-9wdkwfxo confirmed native add with snapshot suppression,
exact operation, explicit commit, git.colocate=false and isolated identity, followed by
workspace update-stale in the owned destination. Source disk/untracked bytes, source
working-copy metadata and colocated Git HEAD/index/config remained byte-identical.
Native update-stale refuses operation pinning; materialization is sampled before/after
and is not a transactional compare-and-swap. These are author research observations;
mandatory executable acceptance and independent source review remain separate gates.

## Final implementation acceptance

Shipping source `5e32515e65ae4d8ffc41d416900a602d31fd488f` passed independent source/safety
review with no blocking findings. Reviewer reran exact-head targeted bounded/store/state/
strict-record/malformed-journal/durability/cancellation/tree safety tests (0.618s) and
whitespace checks. Final independent convergence confirms 15/15 requirements across 30 tasks,
zero missing stories and zero critical/high consistency findings. See [analysis](analysis.md).

At that exact source, the integrating author ran:

- `make check` with mandatory current Git/jj and mutation sandbox settings: Go tests/vet/module
  verification, 42 Python tests, 12 theme tests, 132 authored docs, architecture and whitespace.
- `go test -race ./...` with mandatory current backends/sandbox: all packages passed.
- Native conformance author rebuilt `bin/stw` and ran all four smoke scripts in both pinned lanes:

| Suite | Minimum Git 2.46.0 / jj 0.45.1 | Current Git 2.56.0 / jj 0.46.0 |
| --- | ---: | ---: |
| Identity inspection | 10 | 10 |
| Native jj inspection | 6 | 6 |
| Inventory | 36 | 36 |
| Isolated workspaces | 45 | 45 |

Commands: `go build -o bin/stw ./cmd/stw`, then `python3 -u -B` with
`scripts/smoke-inspection.py`, `scripts/smoke-jj-inspection.py`, `scripts/smoke-inventory.py`
and `scripts/smoke-workspaces.py`. Each lane selected pinned executables through PATH and
set `STW_REQUIRE_JJ=1 STW_REQUIRE_NATIVE_SANDBOX=1`; enforced macOS sandboxing passed.
Existing optional invalid-UTF-8 cwd creation is unavailable on macOS and is explicitly
reported by the legacy inspection suite. No mandatory native case was skipped.

Tests independently preserve source disk/index/config/native working-copy identities, check
checkout content, helper sentinels, source/linked metadata and network denial. Recovery tests
cover failed pre-action persistence, post-action store failure, cancellation, incomplete retry
and foreign retention. Native CLI crash replay is explicitly a simulated incomplete journal;
it is not claimed as a real killed native process. Real native operations run only in disposable
repositories. Existing inspect/version/inventory schemas remain unchanged; real Git and
standalone/colocated jj lifecycle outputs, errors and corrupted stored plans validate offline.

## Reviewed fixes and capability limits

Review-driven tests/fixes cover encoded escaping and aggregate records, semantic malformed
state, duplicate/case-folded JSON keys, FIFO/hardlink/rename/lock replacement, precise effects,
exact initial finalization identity, jj name/manager preflight and Unicode checkout aliases.
Git checkout is detached and locked; helper/hook configuration, symlink/gitlink trees and
unsupported sparse/promisor/worktree configuration refuse (amended by the
[creation refusals amendment](contracts/workspace.md#creation-refusals-amendment): hooksPath,
`submodule.*` and gitlinks no longer refuse). Limits: 16,384 leaf entries (was 1,024),
16 MiB per blob, 64 MiB total stored bytes. Unicode normalization reuses already pinned
x/text v0.14.0 as an explicit dependency; standard library has no canonical normalization API.

jj writes only declared mutable stores/backing objects/refs-jj and owned destination; source
configuration and working-copy metadata stay read-only. Source metadata admits at most16 files,
64 KiB per sampled metadata/index/config file. Native add is operation-pinned; destination-only
update-stale cannot pin operations and uses sampled checks. Its 60-second limit is not a disk
quota. Linux network/write confinement does not claim executable admission against hostile
concurrent same-user configuration edits; helper refusal and sampled external/ABA limitations
are explicit. Missing sandbox capability refuses. No automatic adoption, rollback or deletion.

## PR and hosted gates

This evidence commit follows the reviewed/tested source with documentation/status updates only.
PR creation/attachment follows; the PR body records its final exact head and hosted Linux
source/minimum/current checks. Hosted acceptance is pending at record creation, not inferred
from local macOS evidence. Feature status remains implemented. No owner merge, release,
manual artifact publication or live schema deployment is performed by this handoff.

Implementation PR [#32](https://github.com/wstein/sourcesteward/pull/32) is open and attached
to the implementation task. All30tasks are complete; no owner merge was performed. The PR
body records final exact-head hosted checks as they finish, avoiding a documentation-only
source commit loop after successful CI.

## Hosted source-job prerequisite repair

Initial Linux source job [116650730308](https://github.com/wstein/sourcesteward/actions/runs/38801303196/job/116650730308)
failed with git-sandbox-unavailable: the source check now includes real native Git task
creation, but only conformance jobs provisioned bubblewrap/AppArmor. Regression `a16fec3`
first failed; repair `fc72b3c` provisions the same checked distro sandbox/profile and
preflight in the source job and requires native sandbox tests. Host protection values
are checked unchanged. No skips, unsandboxed fallback or production code changes.

Independent scoped review cleared the CI repair and reran all13backend/workflow tests.
All43Python tests pass with checked Hugo; documentation and whitespace checks pass.
Runtime source remains byte-identical to reviewed/tested5e32515. Final-head hosted Linux
checks are recorded in PR #32's body when complete; superseded runs preserve failure logs.

## Hosted unused-filter admission repair

Both pinned Linux conformance lanes passed at `ad49eb6`; the source job's sandbox
preflight passed but pristine distro-Git fixtures refused system-wide unused Git LFS
definitions. GitHub's image installs Git LFS; its
[package setup](https://github.com/git-lfs/git-lfs/blob/main/debian/postinst) installs
filters system-wide. No executable hook, active filter or host permission was bypassed.

Test-first source `e5fbaf94f1b5cd7fb9a4a72bea1d7ceb0b1178f7` admits unused definitions
only after complete bounded native attributes at the selected base. Active committed/
info/default-global filters and configured sentinel-named drivers refuse before action.
Helper sentinels remain unexecuted. Independent scoped review cleared that exact source
and reran four native filter cases (2.756s) with enforced sandbox; minimum/current
filter race tests passed. Full `make check` (43 Python, 12 theme, 132 docs and all Go
checks) and `go test -race ./...` pass at that source with mandatory current backends.
The 45-case native workspace suite passed again in both pinned local lanes at exact
source e5fbaf9 with enforced sandboxing and no mandatory skips. Final exact-head hosted
results are recorded in the PR body. Previous source/effects
evidence remains applicable to unchanged adapters and contracts.

## Merged acceptance and fixture quiescence follow-up

Owner merged implementation PR #32 and filter repair PR #33. Exact PR head `c3a6f97`
passed hosted source and both Linux conformance lanes in
[run38803498326](https://github.com/wstein/sourcesteward/actions/runs/38803498326);
both native logs confirm45workspace cases per lane. Merged main `e44c59f` has the
same full tree. Its [CIrun38803599327](https://github.com/wstein/sourcesteward/actions/runs/38803599327)
passed source, both native lanes and automatic Pages on attempt2. Initial source attempt
had one existing SHA256 no-write fixture failure; no assertion was weakened on rerun.
Live revision.txt reports e44c59f19106a685e79fd2f6b4f2e7e3d29f59ea; published workspace
schema bytes match source, SHA256 fb4d4623c7b3f329e596e7a3608116d10d3bb3c6801aaece6a073e154ccfd825.

A separate disposable reproduction found an empty fixture commit returning while
objects/maintenance.lock still existed; the file disappeared10mslater without any
inspection call. This proves a setup race, although the original hosted assertion did
not identify its changed path. Test-first fixture source `9d64c13` disables automatic
maintenance only for fixture write commands, retains full before/after bytes and
adds sorted relative changed-path diagnostics without file contents. Trace2 regression
failed before the fix, independently proving background maintenance was launched.

Independent scoped review cleared9d64c13 and reran the trace/shapes five times. Author
minimum/current Git trace/shapes30repetitions each pass; full inspection tests and vet
pass. Integrating author makecheck passes with mandatory current backends/sandbox,
43Python tests,12theme cases,132docs and all Go/module/architecture/whitespace checks.
This fixture-only follow-up changes no production operation, schema or native permission.
Its PR body records separate hosted acceptance; earlier merged feature checks stand.

## Issue #62 source implementation

The native layout implementation follows independently reviewed design
`4a861bba9e6661c6b5a37ee38704261a331bd713` ([design provenance](analysis.md#issue-62-independently-reviewed-design-prerequisite)).
`git_layout_test.go` and `git_layout_lifecycle_test.go` exercise canonical ordinary,
initialized submodule and separate-Git-directory primary/linked sources, generated relative
and explicit absolute mappings, detached creation, stored apply/handoff/retry, packed local
branch recovery/deletion and owned full retirement/retry. They check protected
source/primary/sibling bytes, index/configuration and exact declared reference changes.
Duplicate/external-origin settings, redirections, missing or mismatched reciprocal pointers,
malformed/oversized files and symlink/hardlink aliases refuse. Same-byte metadata replacement
stales evidence; config replacement after recovery retains both target and recovery root.
Helper tests preserve unused LFS definitions and dirty-source attribute behavior while
refusing committed/info/global active attributes, reserved driver settings and executable
hooks with absent process sentinels.

Source smoke manifests additionally require 24 workspace, 12 tidy-plan, 20 tidy-apply and
20 retirement layout cases across initialized-submodule/separate-Git-directory primary and
linked sources. Three source tests check all manifests, fail-closed prerequisites and
native fixture identities. These supplements preserve the original mandatory cases.
No new-layout CLI matrix acceptance, distribution/release/image/site build, local candidate
CLI installation, real-user cleanup, push, PR, tag, release or publication ran. The existing
`TestRealCLIOutputs` and `TestRealJJCLIOutputs` each compiled/executed a temporary `stw`
fixture during both full Go suites, within the authorized routine source-test/compiler
verification boundary. Test temporary directories were cleaned. Those generic schema tests
do not establish acceptance of the newly extended layout smoke matrix. Mandatory fresh
minimum/current Git CLI/schema matrix acceptance
and exact implementation review remain pending. Git 2.46.0 was unavailable; Node was absent
from PATH, so the theme test was not run. No tool was downloaded or installed.

The tested source was committed as `08398366936da8527218ec380ca1057adcf20c91`, after
focused proof (`5983db0`), lifecycle (`13984f3`) and smoke (`0839836`) commits. This evidence
and guide update changes no runtime/test/smoke source. The author was configured
`gpt-6.1-sol/high`; runtime model identity was unexposed. Narrow host execution escalation
was required because the outer sandbox could not mount `/proc`; the product's required
native filesystem/network sandbox remained enabled throughout.

| Executed source verification | Result |
| --- | --- |
| `STW_REQUIRE_JJ=1 STW_REQUIRE_NATIVE_SANDBOX=1 go test ./...` | PASS; full initial suite, including permitted temporary generic CLI fixtures, before final test-only additions |
| Same mandatory environment, `go test -race ./... -count=1 -v` | PASS; all packages; 14 new layout groups and six existing native jj groups passed with zero skips in those groups |
| `go test -race ./internal/workspace -count=1 -v -run '^TestGitLayoutOwnedRetirementRetainsUnsafeTargets$'` | PASS; final added fifteenth layout group, all four primary/linked external layouts; 3.595s |
| Existing minimum jj prepended to `PATH`, mandatory environment, six exact native groups below | PASS; inspection 19.278s, workspace 11.126s; zero skips |
| `go vet ./...`, `go mod verify`, gofmt check, `git diff --check` | PASS |
| Python source suite with exactly one packaging test excluded, exclusion count asserted | PASS; 79 tests; excluded only `test_docs.DocumentationTests.test_packaging_excludes_source_and_binds_revision` |
| `python3 -B scripts/check-docs.py`, `python3 -B scripts/check-architecture.py` | PASS; 253 authored documents, architecture boundary intact |
| `node --test scripts/test_theme.mjs` | NOT RUN; Node unavailable on PATH |

Installed current tools were Git `2.56.0` and jj
`0.46.0-7d382314f9420b4b8d932245b49c99492c812b9f`. Existing minimum jj was
`0.45.1-7c41cdeb16b6b321c64e789a966b6adf723816a5` at the same temporary pin used by the
reviewed design baseline. The minimum source command selected exactly
`TestJJInventoryConformance`, `TestJJTidyCandidateSafety`, `TestJJTidyNativeReadOnly`,
`TestJJTidyApplyNativeRecovery`, `TestJJWorkspaceNativeCreationPreservesSource` and
`TestJJRetirementAdapterOwnedLifecycle` across inspection/workspace packages. It preserves
standalone/colocated local bookmark deletion and native recovery while retaining remote
counterparts and native changes; no abandonment or remote deletion was added. The full
race suite's separate opt-in release debug helper `TestTOMLCorpusDump` skipped as designed;
that is not a skipped native acceptance case.

Three single-rule mutations were executed and restored: dropping direct config-origin
admission failed included/global fixtures; dropping backlink equality failed the wrong-backlink
fixture; dropping metadata inode evidence failed same-byte/same-mode config replacement.
The inode sample first exposed a test confounder (replacement also changed mode); the fixture
was corrected to change only inode, and the mutation then failed for the intended reason.
Transient logs use `/tmp/stw-native-layout-` prefixes and are not external test inputs.

Independent strongest-tier review of the exact final source/evidence SHA and the new CLI
layout/schema matrix on minimum/current Git remain pending. Git minimum 2.46.0, other
platforms, hosted CI, the single approval-required Hugo packaging test and Node theme test
are not claimed. No new layout smoke suite was run against a built CLI. These limits leave
Spec 010 T036–T038 / Spec 011 T404 open and do not establish overall v0.1.0 readiness.

## Issue #62 backlink review correction

The initial `0075d75afaacbf773b5d4970a53978f0ba3121d5` implementation was independently
**NOT CLEAR** for one Medium literal-backlink traversal finding. Its tests and source
results above remain historical evidence rather than clearance of the missed boundary.
[Analysis](analysis.md#issue-62-first-implementation-review-and-correction) records the
finding and test-first correction. The focused source matrix now refuses six absolute/relative
symlink, missing-entry and regular-file parent traversals and admits two valid real-directory
traversals. Each hostile case checks source/metadata/refs, an external sentinel and an empty
reserved destination before the next effect. The focused corrected test command
`go test ./internal/workspace -count=1 -run '^TestGitLayoutBacklinkLiteral'` passed in 0.495s.
No native argument, sandbox grant, configuration-origin rule or recovery effect was widened.

The correction and regression tests are folded into the owned shared-proof commit; the
initial head is preserved locally. Changed-source checks and same-reviewer exact-head
re-review must cover the rewritten candidate. Fresh CLI layout acceptance, Git 2.46.0,
Node, hosted CI, cross-platform checks and the approval-required Hugo packaging test remain
unrun gates. The expanded source layout suite contains 17 groups; this is a source-test
coverage count, not fresh CLI matrix acceptance.

Corrected source `bdf509a1e18e6d326e0e158d670c15c52ae2a904` was checked after folding
the correction. Old reviewed head `0075d75afaacbf773b5d4970a53978f0ba3121d5` is preserved
as local `codex/62-native-layout-review-round1`. `range-diff` retains equality for the
lifecycle and smoke commits; shared proof/tests and evidence changed. The final evidence
amendment changes no `internal/` or `scripts/` bytes from this checked corrected source.

| Corrected-source check | Measured result |
| --- | --- |
| Mandatory environment, `go test ./... -count=1 -v` | PASS; 17 new layout and six mandatory jj groups, zero skips in those groups; workspace 130.017s, inspection 142.097s |
| Mandatory environment, `go test -race ./... -count=1 -v` | PASS; same 17 layout and six jj groups, zero mandatory skips; workspace 138.943s, inspection 154.987s |
| Existing minimum jj pin, same exact six-group selection and mandatory environment | PASS; inspection 31.137s, workspace 16.025s; zero skips |
| `go vet ./...`, `go mod verify`, gofmt and whitespace | PASS |
| Python source suite, exactly the one Hugo packaging test excluded/count asserted | PASS; 79 tests |
| Source docs and architecture checks | PASS; 253 authored documents |

Both full corrected suites retained only the intentional opt-in `TestTOMLCorpusDump` skip.
Tool versions, allowed temporary generic CLI fixture compilation and every unrun gate remain
as recorded above. Same-reviewer review of the exact final amended head is pending; these
checks do not assert CLEAR or essential v0.1.0 acceptance.

## Creation refusals measurement

Evidence for the 16,384 `checkoutLeafLimit` of the
[creation refusals amendment](contracts/workspace.md#creation-refusals-amendment). Measured on
Linux arm64 (10 cores), Git 2.56.0, Go 1.27.2, with a throwaway test (not committed) that builds a
temporary repository with N tracked files in 200 directories (about 25-byte paths, 10-byte blobs) and times
`gitAdapter.Observe` (the plan read) and `gitAdapter.Create` (re-observation, sandboxed `git worktree add`
and verification, the apply) with the leaf limit raised for the probe. Two runs; the range is shown.

| Tracked files | Observe (plan) | Create (apply) |
| ---: | ---: | ---: |
| 1,001 | 27-113 ms | 75-105 ms |
| 4,001 | 35-36 ms | 142-192 ms |
| 16,001 | 73-85 ms | 365-447 ms |
| 32,001 | 120-125 ms | 733-811 ms |

Both are far below the ten-second read and sixty-second apply deadlines even at twice the cap, and
growth is linear, so 16,384 is bounded by a documented constant with about two orders of magnitude of
time headroom. Path length is the other cost driver: the unchanged 4,325,376-byte stream bound ends a
tree of long paths earlier than the leaf cap. SourceSteward itself has 1,062 leaf entries, the
Anchorage umbrella 34 files and three gitlinks, WorkHarbor 745 leaves with `core.hooksPath=.githooks`.
