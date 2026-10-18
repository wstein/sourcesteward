# Spec 026 WI5 source candidate

Scope: T024–T027 and the T052 read-only tag-ruleset advisory. This candidate
adds typed bounded release GETs, online gate decisions, and fixed attestation
verification. It adds no dispatch, build, publication or repository mutation.
Spec 026 remains planned. Independent exact-head review and the descriptor-input
[design assessment](attestation-input-design.md) are pending; T026 acceptance is
not claimed.

## Failing-first evidence

- Hosting acceptance tests initially failed because `readRelease`, `releaseTags`
  and the closed endpoint predicate were absent. After implementation, typed
  workflow/file/head-SHA, draft/assets, tags and main-ref fixtures passed.
- Online gate tests initially failed because `onlineGates` was absent. The
  implemented gates cover product grammar/monotonicity, draft visibility,
  already-published/draft coverage, push/main CI and published-audit exemptions.
- Attestation tests initially failed because the fixed verifier and strict JSON
  digest parser were absent. Fake-gh tests now exercise fixed signer/repository/
  source-ref flags, inherited no-follow descriptor bytes, empty external cwd,
  filtered environment, one-MiB output bounds, cancellation, strict JSON and
  digest mismatch. Concurrent content mutation and renamed-path/symlink cases
  fail closed. No content snapshot is written.
- The ruleset fixture initially failed because its typed reader was absent.
  Ruleset permission/shape failures remain advisory-only `unknown`; no setting
  is changed and no advisory changes a gate outcome.

## Source checks

Full Go tests passed, including native temporary-repository cases. Focused
hosting/release/CLI suites passed after additional qualification and descriptor
checks. Go vet, module verification, architecture, documentation and whitespace
checks passed. The Python suite passed 81 of 82 tests after asserting that the
only excluded test was the actual Hugo packaging build
`test_docs.DocumentationTests.test_packaging_excludes_source_and_binds_revision`.
Node is absent, so the theme suite was not run. `make check` was not represented
as complete because its Hugo build needs separate authorization and Node is
unavailable. No dependency was installed.

Final exact-candidate raw fixture/source/race and Darwin compiler evidence is
reported in the coordinator handoff, rather than treating this prose as runtime
proof. These checks are source/test compilation, not a product artifact build.

## Limits

All GitHub HTTP, token and attestation interactions used in-process fixtures or
fake executables. No real gh authentication, attestation or external request ran.
The gh JSON output shape, real gh `/dev/fd/3` seek/hash compatibility, TUF/cache
behavior and actual rulesets permissions/shape remain UNVERIFIED until an
explicitly authorized rehearsal. Darwin has compiler-only evidence; no native
Darwin process acceptance is claimed. Unsupported descriptor/shape behavior has
no path fallback and fails closed. See the design proposal for point-in-time
sampling limits. No owner tag, release, publication, workflow run, image
activation, distribution build or documentation build was performed.

## First independent review corrections

The initial `fcb3c49` review returned one High and two Medium findings. Focused
regressions reproduced all three before correction: late attestation launch
received caller-relative/nested paths instead of the native root; a pinned
`/dev/null` descriptor was accepted as an empty file; and cancellation at the
final ruleset advisory or after an online reader could be discarded.

The corrected candidate passes the native root to every later verification
launch, independently requires an existing canonical absolute process boundary,
checks the pinned descriptor's regular-file mode before reading, and propagates
cancellation/deadline failures while leaving ordinary advisory permission errors
non-authoritative. Five isolated guard-removal mutants were killed by behavioral
assertions, covering both cancellation boundaries, pinned regular-file mode,
native-root propagation and absolute-root preparation. No mutant failed merely
because it did not compile. These fixes await the same independent reviewer's
assessment of the folded exact candidate.

The draft-ordering and existing-tag CI binding fixes are folded into their owning
online-gates commit; foreign parent `f459e5b` is preserved. Repository-required full
normal and full race suites, rather than only affected-package race checks, are
run once on the final folded candidate and recorded in the coordinator handoff.
