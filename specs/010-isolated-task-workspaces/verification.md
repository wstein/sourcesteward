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
unsupported sparse/promisor/worktree configuration refuse. Limits: 1,024 leaf entries,
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
