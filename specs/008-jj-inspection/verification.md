# Verification Evidence: Native jj Inspection

## Baseline
Coordinator ran initial make check in the new worktree: Go/vet/module checks passed, but Hugo was absent at the worktree-local `.tools/hugo` path. This is an environment prerequisite gap, not a source failure.

Coordinator reran with `HUGO=/private/tmp/sourcesteward-hugo-docs/.tools/hugo` and existing shared Go caches: full make check passed, including19Python/12Node cases,80authored-document checks, Go/vet/module/architecture/whitespace checks.

## Design and Implementation
Tier 1 policy1.1.0 dependency integrated at a0bad365688ecb40497d6210871feb8e8d047e52. Specification/design/tasks are being prepared for independent analysis. No native jj implementation, conformance run, schema publication or live deployment is claimed by this baseline record.

## Implementation and Independent Review
Native source revision949c082 received independent source/effects clearance. The reviewer
ran14actual CLI effect cases across jj0.45.1/0.46.0 and verified unchanged target bytes and
modes. Native identity, snapshot/operation guards, configuration isolation, metadata bounds
and failure behavior are implemented. Review findings included FIFO/special-file blocking,
global metadata traversal budgets and output/cancellation/concurrency coverage; fixes and
regression tests are included in the cleared native revision.

Schema implementation used failing tests first: offline compilation, published-v1 hashes and
jj invariant tests initially failed for absent v2; publication initially rejected v2 as an
unknown directory. The unsupported-status negative fixture exposed a permissive status case,
fixed at3b53050. Native v2 success/error outputs validate for seven real CLI cases on each
jj version (14total); missing mandatory tools/sandbox do not count as conformance. Both v1
schema files remain byte-identical; three-file publication checks preserve exact source bytes.

## Coordinator macOS Verification
At integrated3b152f6, whose native bytes match independently cleared949c082:
- Minimum Git2.46.0/jj0.45.1 with STW_REQUIRE_JJ=1: full make check passed31Python/12Node
  cases,90authored-document checks, Go tests/vet/modules, architecture and whitespace.
- Current Git2.56.0/jj0.46.0: required `go test -count=1 -race ./...` passed CLI/contracts/
  inspection packages. Both runs used actual enforced macOS sandbox execution.
- Both backend versions passed required native/schema process fixtures; explicit missing-tool
  local skips in earlier optional runs were not counted as acceptance.

## Hosted Linux Acceptance
[Actions run38777280084](https://github.com/wstein/sourcesteward/actions/runs/38777280084)
passed at3b152f6: source check plus minimum job116568083169 and current job116568082856.
Both lanes installed exact pinned Git/jj versions, passed sandbox capability preflight,
mandatory real Go/schema/race tests and real Git/jj CLI zero-target-write checks. Initial
hosted sandbox capability failures were diagnosed and resolved through the pinned distro
AppArmor profile, without globally disabling host protections.

Latest integration beb3285 adds stronger Linux payload evidence: zero effective capabilities,
enforced unpriv_bwrap label and distinct network namespace. Its final CI and final public-doc
integration review remain pending; earlier passing source/native/schema inputs are unchanged.

## Acceptance Limits and Remaining Gates
Private temporary isolation/probe files outside the target are intentional disclosed setup
writes. Stored-unsnapshotted identity is not disk freshness, a transaction or full ABA detection.
Sandbox unavailable/denied and repair-requiring metadata fail safely; no unsandboxed fallback.
No CLI release, merge, live schema publication or Pages deployment is claimed.

T019/T020 final handoff remain open until integrated documentation/package checks,
post-implementation analysis, independent final review and latest-head CI are recorded.
