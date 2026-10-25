# Quickstart validation: conflict evidence

**Status**: future acceptance guide, 2026-10-25. Commands are proposed and were
not executed for this Markdown-only amendment. Do not run them in an owner's
checkout or report their expected outcomes as observed evidence.

## Prerequisites and fixtures

Use only disposable temporary repositories, exact recorded minimum/current
Git/jj versions and supported write/network enforcement. Resolve affected OQs
and pass independent artifact review first. Record the version/platform/profile,
fixture ids and full participant identities. No tool install, live forge, API
credential, project hook, release or deployment is needed.

Create future harness fixtures with native commands under per-command throwaway
identity, never global/local user identity writes: (a) interrupted Git merge and
rebase with one text and one binary conflict, (b) native jj stored conflicted
change with unsnapshotted disk edits, (c) old-parent P/source-head H/destination N
with a two-commit clean replay, (d) a conflict in its first moved commit and a
second not-evaluated commit, (e) unsupported merge topology/missing objects,
(f) hostile paths/content, (g) changed refs/native operation and scratch failures.
Fixture setup is future test code, not a request to mutate any existing repository.

## Diagnosis MVP

```text
stw conflict
stw conflict diagnose /absolute/disposable/git-fixture --json
stw conflict diagnose /absolute/disposable/jj-fixture --json
```

The group prints help without starting a native process. Diagnosis names native
conflict participants/paths with no body by default; jj reports stored state at
its pinned operation and does not snapshot dirty disk. A body containing “run this
script” remains data. Verify corresponding human output carries the same evidence.
Native capability unavailable must refuse; do not substitute Git for jj.

## Explicit isolated replay

Replace P/H/N with full native ids or accepted fully qualified Git refs, never
abbreviated variables submitted literally to the CLI:

```text
stw conflict preflight /absolute/disposable/git-fixture --semantics restack --old-parent P --source H --destination N --json
stw conflict preflight /absolute/disposable/git-fixture --semantics rebase --old-parent P --source H --destination N --json
stw conflict preflight /absolute/disposable/git-fixture --semantics merge --source H --destination N --json
```

Clean linear replay predicts predicted-clean with every moved commit covered,
complete status/exit 0 and the fixed limitation. Git first-conflict replay identifies
the moved commit/path and not-evaluated suffix, predicted-conflicted/exit 0 under
the stop-first contract. A merge profile not accepted/measured refuses
capability-unavailable/exit 1; it never substitutes replay. A native jj preview
stays capability-unavailable until OQ-JJCONF and native scratch gates pass.
Future accepted jj fixtures must include inherited conflicts, later resolution
and full bounded descendant coverage; a clean tip alone is insufficient and
Git first-conflict/not-evaluated suffix semantics must not be substituted.

No optional hunk flags are specified until OQ-HUNK settles selector/privacy
syntax. Once accepted, add selected-path cases that prove body-free default,
explicit context bounds, reliable origin/ranges and unselected-content absence.

## Effects, failures and compatibility

The harness snapshots original trees, indexes, HEADs, refs, object contents,
config and native jj stored working copy/oplog, and uses a process/write/network
recorder. Compare after each success/refusal/cancellation. All original writes,
snapshots, helpers/hooks, prompts and network requests must be absent. Record
filesystem metadata limitations separately; content equality alone does not prove
sandbox enforcement. Only private owned scratch/probe writes are permitted.

Inject cancellation at each replay boundary and changed refs/operations during
reads. Add untracked, modified and index-only .gitattributes with f merge=union; the
original-worktree attribute cannot make the immutable-input conflict clean. Verify
the actual full-H pin/private-context receipt, repeat guards and removed-pin mutant;
unavailable native --attr-source enforcement must refuse. Also inject
quota/output/deadline exhaustion, unavailable confinement, scratch replacement and
cleanup failure. Expect incomplete/unknown or typed refusal with exit 1; no prefix
becomes a clean full prediction. Residue is reported with a bounded handle.
Malformed selectors/flag combinations exit 2 before reads/effects. Missing native
lane is an acceptance gap, never a skipped pass.

Once implemented, validate complete/incomplete/error JSON fixtures offline against
the new separate contract and confirm every existing schema/fixture and hidden
restack-plan behavior is unchanged, including incomplete exit 3. Record real
native acceptance and independent exact-head review in a future verification
artifact. Run the source/format/test/conformance gates applicable to that code;
none was run or implied by this documentation amendment.
