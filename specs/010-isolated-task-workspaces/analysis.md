# Spec 010 pre-implementation analysis

Independent read-only analysis cleared design `6d3e55e` before source implementation.
Required prerequisite scripts passed; no extension hooks are configured.

| Finding | Severity | Resolution |
| --- | --- | --- |
| H1 Store path replacement could evade final-file no-follow checks | High | Anchored os.Root access, directory identity validation and replacement/symlink tests in T003/T005/T021. |
| H2 Generic checkout-size wording exceeded native jj preflight guarantees | High | Git-only 1,024-entry/64 MiB preflight; jj deadline/output limits and partial disk effects explicit. |
| H3 Parallel adapter interface drift | High | Shared Snapshot/NativeWorkspace Observe/Create/Verify contract frozen before source. |
| M1 Adapter filenames differed between plan and tasks | Medium | Plan aligned with git.go/jj.go author ownership. |

Metrics: 11 functional requirements + four success criteria, 30 tasks, 15/15 covered
(100%), zero unmapped tasks, zero remaining critical/high findings or blocking ambiguity.
The full traceability table is in [tasks](tasks.md). No constitutional MUST conflict or
policy exception: Spec 010 provides the separately reviewed native mutation specification.
This record is written by the integrating author from independent analysis; the analyst
modified no artifacts. Clearance is design evidence, not runtime safety or acceptance.

Proceed with failing tests and implementation. Final exact-revision source review,
conformance, schema validation and handoff evidence remain required.

## Final implementation convergence

Independent read-only convergence confirmed shipping source
`5e32515e65ae4d8ffc41d416900a602d31fd488f`: 15/15 requirements, 30 tasks, zero missing
stories, zero critical/high consistency findings and no constitutional MUST conflict.
Source/JSON/native retention boundaries match the public contract. Final source review
separately cleared the same exact revision. Fault tests cover pre-action durability
refusal, post-action persistence failure, cancellation and conservative incomplete retry.
Encoded and aggregate result bounds apply even to escaped or combined stored evidence.

The illustrative private interface was synchronized with Backend/Reason/RecoveryLimits;
status, progress and acceptance evidence are updated at handoff. Hosted CI, owner merge
and live publication remain separate observations and cannot be inferred from analysis.

Scoped post-CI source review separately cleared `e5fbaf9`: bounded base-pinned attribute
evidence admits unused filter definitions while retaining helper/hook refusal. Reserved
sentinel-name ambiguity is regression-tested and refused; native/config evidence remains
bound to stale validation. Shared CLI, ownership, journal and recovery contracts did not change.

## Issue #62: measured baseline and design candidate

**State (2026-10-18): author design candidate; independent amendment analysis pending.**
The owner requires native Git worktrees and native jj bookmarks for v0.1.0 acceptance.
The preceding historical CLEAR results do not cover this amendment. No runtime source,
public schema or release capability is implemented by this specification commit.

### Problem and bounded decision

At source `279865a525be13684e989e8c1d936c0cdff2269b`,
`internal/workspace/git.go:gitSafety` refuses every `core.worktree` key. The same observation
is reused by reference tidy and owned retirement, so a normal submodule checkout and its
linked worktrees cannot use those operations despite supported native Git layouts.
Unused `filter.lfs` definitions are already admitted with bounded pinned-base attribute
proof; attributing this refusal to ordinary installed LFS definitions was incorrect.

The candidate [layout contract](contracts/workspace.md#git-checkout-layout-admission-issue-62)
replaces only that blanket key refusal with bounded origin, reciprocal-pointer and native
identity proof. It separates current, per-worktree, common and primary identities, fingerprints
layout/configuration evidence and keeps existing helper, ownership, sandbox, recovery and
post-action constraints. Spec 011 consumes the same proof; no universal adapter rewrite.
A missing proof remains refusal. Sparse/promisor, alternates, worktree-specific configuration,
gitlinks/nested-submodule activation and other unsupported cases are not admitted by this change.

Existing qualified native jj bookmark deletion already satisfies the specified operation:
exact local deletion after native recovery, with conflicted/base/recovery/workspace-used and
any remote-counterpart bookmark retained. Untracked remote references are counterparts too.
This is distinct from abandoning a jj change, forgetting a workspace or deleting remote refs;
none is newly authorized. The follow-up tests preserve these semantics.

### Executed source evidence

All following source tests ran in the clean assigned worktree at
`279865a525be13684e989e8c1d936c0cdff2269b` using disposable native fixtures, `umask 022`,
and shared `GOCACHE=/home/vscode/.cache/go-build`. Narrow host execution escalation was
needed because the outer tool sandbox could not mount `/proc`; the application sandbox was
still required and no unsandboxed product fallback was used. No artifact/CLI/documentation
build, real-user cleanup, push, tag, release or publication ran.

| Lane | Tool selection | Result |
| --- | --- | --- |
| Current installed | Existing `PATH`: Git `2.56.0`, jj `0.46.0-7d382314f9420b4b8d932245b49c99492c812b9f` | inspection 12.423s, workspace 7.089s; PASS, zero skips |
| Minimum jj | Prepend `/tmp/claude-1000/-workspaces-Anchorage/758a2430-cf93-4863-a486-ff5e320a7345/scratchpad/jjdl-0.45.1/x` to `PATH`; jj `0.45.1` | inspection 12.666s, workspace 6.044s; PASS, zero skips |

The mandatory flags were `STW_REQUIRE_JJ=1 STW_REQUIRE_NATIVE_SANDBOX=1`.
Command: `go test ./internal/inspection ./internal/workspace -count=1 -v -run '<selected exact test names>'`.
The current selection was exactly:

```text
TestGitInventoryWorktreesFilesAndEffects
TestGitWorkspaceLinkedSourceMetadataPreserved
TestGitWorkspaceNativeCreatePreservesSource
TestGitRetirementNativeCombinedRemovalAndRecovery
TestJJInventoryConformance
TestJJTidyCandidateSafety
TestJJTidyNativeReadOnly
TestJJTidyApplyNativeRecovery
TestJJWorkspaceNativeCreationPreservesSource
TestJJRetirementAdapterOwnedLifecycle
```

The minimum jj lane reran exactly the six `TestJJ` groups above. The tidy read-only/recovery
groups exercised both standalone and colocated repositories; candidate safety exercised
integrated, unintegrated, base, workspace commit/change, remote, Git-remote, conflict and
recovery cases. Creation/retirement groups prove only their fixture coverage, not every layout.
Captured logs were `/tmp/stw-native-design-current-tests.log` and
`/tmp/stw-native-design-min-tests.log`; these transient paths are not required external inputs.

A separate `go test ./internal/inspection -count=1 -v -run '<selected exact test names>'`
passed in 0.578s, with exactly these five existing layout groups and zero skips:

```text
TestGitInventorySubmoduleCheckoutIsDescribedByItsFiles
TestGitInventorySeparateGitDirCheckoutFromTheTopAndASubdirectory
TestGitInventoryLinkedWorktreeOfASubmoduleResolvesTheMainCheckout
TestGitInventorySeparateGitDirLinkedWorktreeQualifiesTheMainEntry
TestGitInventoryHostileCoreWorktreeIsNeverDescribed
```

This establishes existing qualified read-only layout support and existing native mutation
coverage. It does not establish the new layout admission, all platform/lane conformance,
full source checks, fresh binary acceptance or acceptance at the later amendment/source SHA.
Git minimum `2.46.0` was not run in this batch. The current jj measurement names the actual
installed commit-suffixed version, not a claim that the pinned stable binary was used.

### Disposable native layout reproduction

In `/tmp/stw-native-layout-design-2hyu0oby`, Git 2.56.0 initialized local fixture repositories,
added local `super/member` as a submodule and created a detached `linked` worktree. No network
or user repository mutation was involved. Native queries returned:

| Checkout | Native top-level | Native Git directory | Effective configuration |
| --- | --- | --- | --- |
| Primary | `<fixture>/super/member` | `<fixture>/super/.git/modules/member` | `core.worktree=../../../member` from `<fixture>/super/.git/modules/member/config` |
| Linked | `<fixture>/linked` | `<fixture>/super/.git/modules/member/worktrees/linked` | The same primary `core.worktree` value and origin |

Thus a legitimate shared value differs from the current linked root. Comparing those paths
for equality would preserve the defect. Both paths were passed only to read-only
`stw tidy plan PATH --base HEAD --json` using the pre-existing executable
`/tmp/claude-1000/-workspaces-Anchorage/758a2430-cf93-4863-a486-ff5e320a7345/scratchpad/stw-wi1`.
The session supplied its build source as `c34f69f9381314d759cf18537e2a56d537f75079`;
this batch did not build it or independently attest its provenance. Both calls exited 1 with
`git-unsafe-configuration`. Assigned-source inspection independently confirmed the same
blanket guard at `279865a`; old-binary observations are not fresh assigned-SHA CLI acceptance.

### Required independent analysis and handoff

[Spec 010 T031–T038](tasks.md#issue-62-native-git-layout-readiness) and
[Spec 011 T401–T404](../011-workspace-reference-tidy/tasks.md#issue-62-shared-layout-consumers)
cover FR-012 in both specifications and retain existing requirement/success gates. A fresh
independent reviewer must check the exact committed candidate across both specs/contracts,
including key origin/duplicate rules, linked versus primary proof, configuration hashing,
legacy-plan refusal, all cleanup consumers and narrow sandbox effects. This author does
not clear its own rule changes. Runtime implementation waits for that analysis.

The author was configured as `gpt-6-astra/high`; concrete runtime model identity was not
exposed. The assigned read-only research boundary and later spec-only author boundary were
procedural, not restricted tool capabilities. Spec026 and shared progress/feature-status files
were excluded from this author scope. Resume with independent amendment findings, then the
bounded failing-test implementation; new source/native evidence must name its own exact SHA.


Author validation of the specification candidate: `python3 -B scripts/check-docs.py` passed
(253 authored documents) and `git diff --check` passed. These are source-only documentation
and whitespace checks. No full source-suite rerun, Hugo/site build or new native acceptance
was performed for the prose-only candidate; the scoped baseline results above remain distinct.

## Issue #62 independently reviewed design prerequisite

The coordinator supplied executed independent design-only CLEAR from
`/root/dispatch/review_native_spec`, configured `gpt-6-astra/high` (runtime model identity
unexposed), first at `bfb7332cd8c141d0f88c84c3f3dee367f09b4189`, then at the exact
rebased design `4a861bba9e6661c6b5a37ee38704261a331bd713`. The author's old parent
`279865a525be13684e989e8c1d936c0cdff2269b` and new parent
`9a704dbaf166ccc2bca57fce890b44cdc239374f` both had tree
`edf9b48bfbf8cd3514e98b8402a7969b017dd788`; old/new design tree was
`eee9f5dbae5ab764f2469311e6ce0c5052a0a0df`, with `range-diff` equality `1 = 1` and
unchanged message/attribution. The same independent reviewer rechecked the exact new
SHA, tree/range/message proof, 253-document source check and whitespace. The earlier
15 current and six minimum-jj native groups were not rerun for that source-identical
rebase. This is prerequisite design evidence, not implementation review, a native Git
minimum run or artifact/build authorization.

The implementation author reproduced the old refusal with real native fixtures before
replacing it: ten admitted-key creation combinations failed in
`TestGitLayoutNativeAdmissionAndDetachedCreation`, and the config-replacement creation
case was blocked by that guard (`/tmp/stw-native-layout-red.log`). A separate failing
`TestGitLayoutVerifyRefusesReplacedConfiguration` proved that completed creation
verification previously ignored a same-byte common-config replacement
(`/tmp/stw-native-layout-verify-red.log`). Both defects are fixed at the shared workspace
safety boundary; native command arguments and sandbox grants are unchanged.

The runtime proof is now implemented and source-tested; [verification](verification.md#issue-62-source-implementation)
records the focused source commits, executed single-rule mutations, mandatory native jj
regressions, preserved effects and remaining exact-source review/fresh CLI acceptance gates.
The historical design-candidate wording above records the prerequisite state, not a claim
that the implementation has independently cleared its own safety changes.

### Issue #62 first implementation review and correction

Independent executed review of exact implementation/evidence head
`0075d75afaacbf773b5d4970a53978f0ba3121d5` by `/root/dispatch/review_native_layout`
(assigned strongest tier; runtime model identity unexposed) was **NOT CLEAR**, with one Medium finding and no other source findings. In
`git_layout.go`, `filepath.Dir(value)` cleaned the registration's backlink before the
literal traversal proof. Absolute and relative backlinks containing a symlink, missing
entry or regular file followed by `/../.git` could therefore be admitted and reach
successful native creation, contrary to T033/T034. The earlier source tests did not
establish this boundary; T031 design clearance remains separate and unchanged.

The author first reproduced all six hostile cases, including native creation and
source/index/config/ref/destination/external sentinel checks, in
`TestGitLayoutBacklinkLiteralTraversalRefused`. The separate positive matrix
`TestGitLayoutBacklinkLiteralDirectoryTraversalAdmitted` retains both absolute and relative
backlinks with real directory traversal and ordinary generated `..` components. The focused
suite failed before the correction, then passed after splitting the literal final `.git`
filename without cleaning and validating the complete parent traversal before comparison.
The old head is preserved locally; the correction is folded into the original shared-proof
commit. A rewritten head needs the same independent review again; no prior clearance is
carried onto it. See [verification](verification.md#issue-62-backlink-review-correction).
