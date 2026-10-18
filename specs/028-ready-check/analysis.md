# Design gates and evidence: Ready inspection

## Specification gate

Candidate `4dbfd8e557148f315daf8c00c932d9f5a6b5aa46`, base `9a704dbaf166ccc2bca57fce890b44cdc239374f`: coordinator reported independent `review_ready_spec` FINAL CLEAR, zero findings, covering 19 requirements, six success criteria and four stories. This is specification clearance only. The report's runtime model was not attested; no runtime-attestation claim is made.

Source verification on that unchanged candidate passed: Go tests and race suite (initially excluding two CLI fixture tests, then running exactly those two in normal/race mode; combined coverage complete), gofmt, vet, module verification, architecture checks, documentation checks (255 documents), git diff --check and 76 Python source tests. Exactly one actual Hugo packaging test was excluded by an asserted test-ID filter. Node was unavailable; theme tests, minimum-version lane and hosted CI were not run. Temporary CLI fixture compilation was explicitly classified by the coordinator as authorized routine test compiler verification; no distribution/image/release/site build was performed. Worktree remained clean.

## Plan gate

Plan/research/data-model/contracts are proposed. Input schemas and examples are unpublished structural fixtures, not feature acceptance. Author structural validation compiled all three input schemas offline with the existing pinned Go JSON Schema library, accepted their fictitious examples and rejected extra-field/missing-version negatives. Documentation checks passed (264 documents); whitespace checks passed. The first plan candidate was `fe53776ed89b74ba298b2552086060dcdef978b0`; full source/race checks passed, with Python packaging/Node/hosted limits unchanged. Original raw stdout was returned through tools rather than captured in files; the coordinator received an explicitly author-reported command/exit/chunk record, not a fabricated raw log. Independent plan review returned NOT CLEAR: three High findings, zero Medium/Low. Corrections below require the same reviewer on the new exact head. `tasks.md` must not be generated until the installed plan gate is accepted.

Deployment alternatives need independent security assessment before any material owner choice. No App/secret/environment/settings profile is selected or activated. A context/App source pin is not proof of workflow identity; a new App is not presumed universally necessary. Remaining hosted proof obligations are explicit in the publisher contract.

## Plan review round 1 corrections

| Finding | Correction and counterexample |
| --- | --- |
| R1 High: observed invalidation could leave earlier successful check green | Publisher contract now mandates prior-success revocation, blocking in_progress/failure lifecycle, readback, and explicit enforcement-unknown on unconfirmed writes. Neutral/skipped are forbidden blocking results. |
| R2 High: per-PR serialization ignored commit-scoped shared check collisions | Shared repository/head/context/App key, complete bounded open-PR enumeration, singleton fail-closed predicate and before/after association resamples; observed collisions invalidate the shared check. |
| R3 High: partial activation comparison omitted lock.transition authority | Full closed lock bytes, including transition/null and every old/new/candidate field, authenticated at all samples; independently pinned channel cannot be redirected by candidate data. |

[Adversarial vectors](contracts/counterexamples.md) separate shape-valid transition tampering from authorization and give required publisher lifecycle outcomes. They are design regression inputs, not implemented behavior or hosted acceptance. The approved specification is unchanged. Plan gate remains NOT CLEAR pending independent correction review; tasks and runtime remain blocked.

## Full design and implementation gates

Independent cross-artifact analysis occurs only after approved plan and generated tasks. No implementation is authorized by the specification gate alone. Report actual exact-revision reviews, fixes, tests and acceptance gaps here; never convert a structural fixture or source check into deployment proof.
