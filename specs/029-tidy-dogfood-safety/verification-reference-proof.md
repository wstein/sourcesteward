# Unit D reference-proof helper handoff

This source slice covers T020–T021 only. Reference apply/recovery T022–T023,
tracking effects, retirement, public CLI exposure and feature acceptance remain
pending. No build, installation, real cleanup, fetch, publication or push ran.
The independent exact-head helper gate has not yet run; E must not consume this
candidate before that gate.

## Dependency provenance

Fresh local and GitHub main agreed at
`c93ac0705f7ab2d770e050e02ef5c5cb19ae27d9`, tree
`7a5db84b66c6865d2018de2ff5803021fdab74d2`, immediately before import.
The [manifest](dependency-imports.json) binds every imported source SHA/tree,
source/destination path and old/new blob. Exact imports were committed as:

- Shared reviewed contract: `04fb822ceca0ae507ae61e63bfd2d36dea027411`.
- A native primitives: `0cf5b3d152cd94495fe6e493c7b63f07d5a0bd81`.
- B model/store: `044813996b0a9296b24d6441bf0205386928b2a2`.
- C hosting readers: `238ec5eede1f8119d9eb20119cfe9be7b1782a10`.

The original reviewed shared documents remain intact except B's independently
reviewed commit-proof data-model update. Distinct [native](verification-native.md)
and [hosting](verification-hosting.md) receipts preserve the original ledgers.
No dependency branch ancestry, foreign commit rewrite or merge was inferred.
A native/retirement collector files and C transport remain frozen dependencies.

## Shared helper candidate

`tidy_v3_proof.go` owns `proveTidyV3Commit` and
`revalidateTidyV3Commit`: complete native target/base identities, actual original
workspace paths, optional complete typed same-repository merged-head evidence,
and a native ancestry callback. There is no local reference identity in commit
proof. Native proof retains its own meaning; provider proof requires the exact
head and reported integration-result ancestry into the immutable selected base.
Original PR base ref/OID are bound facts and need not equal the selected base.

The narrow `tidyV3ProofSession` permits repository identity and complete selected
PR resampling only. Stored-proof revalidation compares the complete tuple and
rechecks native result ancestry. Typed diagnostics retain provider drift,
unsupported identity, budget failure and unavailable evidence distinctly while
preserving cancellation. Provider/native input errors cannot authorize a prefix.

The same file supplies complete typed native reference/registration observation
using A's unchanged safety/stream collectors, and pre-authentication remote
qualification binding exact configuration bytes/origins. `tidy_v3_refs.go` adds
local-reference planning only. Registered commit/name use, selected bases,
symbolic refs and their targets retain before provider discovery. Stateless
plans have no stored identity; persisted plans bind complete private native
facts and undergo final provider/native/root resampling under the store lock.

## Test-first evidence and current blocker

Meaningful failing behavior preceded implementation of:

- Nine native rewritten heads, seven with deleted intermediate original bases.
- Stateless planning with selected-base and symbolic-reference protection.
- Cancellation occurring after native ancestry returned success.
- Changed native result ancestry and complete stored provider tuple resampling.
- Conflicting PR hints refusing before authentication.
- A late actor symlink replacing a registered root before plan persistence.
- Explicit unsupported-backend and changed-provider refusal statuses.

The focused `go test ./internal/workspace -run '^TestTidyV3' -count=1`
passed in 2.886 seconds before adding the known dependency regression below.
Fixtures isolate all inherited Git controls and HOME/XDG/gh configuration before
the first Git setup command, and explicitly guard missing native Git.
Native/provider fixtures cover nine qualifying heads through the coordinator,
private immutable plan readback, complete negatives versus incomplete no-plan,
missing/nonreachable objects, amended/patch-only heads, wrong repositories,
all registered workspace use, offline/no-auth, unsafe configuration and late
base/reference/configuration/root changes. Source refs remain unchanged.

`TestTidyV3FailedHTTPBoundPreservesActualDetectionCounter` failed against
original imported B at helper candidate
`a464bbf249c42f13ef3a7ac4408c206984365dd3`. C reports 128 requests and 33,554,433 consumed bytes on its
32 MiB limit plus one detection byte; D retains the actual counters and exposes
`hosting-evidence-limit`, status error, null plan and incomplete observation.
The original B result bound replaced that with a generic partial fallback and
zero counters. B's same owner corrected that result-only representation and
obtained independent exact-source clearance at
`a8512764088f50920dbb333a71e0576ad3717d83`. D imported only its five reviewed
correction paths at `bc74bd78a6e0e8c38b5390e69a15787b35558b74`; the
[supplemental manifest](dependency-corrections.json) preserves each old/new blob
and source revision alongside the unchanged original manifest. Forty-seven
original import blobs remain unchanged, with five explicitly declared
corrections. D runtime helper code was not changed by this correction.

The full owned focused command `go test ./internal/workspace -run
'^TestTidyV3' -count=1` now passes all cases, including the integrated counter
regression (4.136 seconds). Required aggregate checks and exact independent
helper review remain pending. No complete source clearance, freeze for
consumers or implementation acceptance is claimed.

## Remaining verification

Run one coordinated full normal/race source window after the coordinator
releases the current unrelated aggregate window. Run formatting, vet, module, docs, architecture,
Python source checks and whitespace validation. Exclude exactly the actual Hugo
packaging test absent separate build authority; Node is unavailable and will not
be installed. Minimum Git 2.46.0/jj 0.45.1 and macOS remain unexecuted acceptance
lanes. Current available tools are Go 1.27.2 linux/arm64, Git 2.56.0,
jj 0.46.0-7d382314f9420b4b8d932245b49c99492c812b9f and Python 3.13.5.
