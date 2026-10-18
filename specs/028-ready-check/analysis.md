# Design gates and evidence: Ready inspection

## Specification gate

Candidate `4dbfd8e557148f315daf8c00c932d9f5a6b5aa46`, base `9a704dbaf166ccc2bca57fce890b44cdc239374f`: coordinator reported independent `review_ready_spec` FINAL CLEAR, zero findings, covering 19 requirements, six success criteria and four stories. This is specification clearance only. The report's runtime model was not attested; no runtime-attestation claim is made.

Source verification on that unchanged candidate passed: Go tests and race suite (initially excluding two CLI fixture tests, then running exactly those two in normal/race mode; combined coverage complete), gofmt, vet, module verification, architecture checks, documentation checks (255 documents), git diff --check and 76 Python source tests. Exactly one actual Hugo packaging test was excluded by an asserted test-ID filter. Node was unavailable; theme tests, minimum-version lane and hosted CI were not run. Temporary CLI fixture compilation was explicitly classified by the coordinator as authorized routine test compiler verification; no distribution/image/release/site build was performed. Worktree remained clean.

## Plan gate

Plan/research/data-model/contracts are proposed. Input schemas and examples are unpublished structural fixtures, not feature acceptance. Author structural validation compiled all three input schemas offline with the existing pinned Go JSON Schema library, accepted their fictitious examples and rejected extra-field/missing-version negatives. Documentation checks passed (264 documents); whitespace checks passed. The first plan candidate was `fe53776ed89b74ba298b2552086060dcdef978b0`; full source/race checks passed, with Python packaging/Node/hosted limits unchanged. Original raw stdout was returned through tools rather than captured in files; the coordinator received an explicitly author-reported command/exit/chunk record, not a fabricated raw log. Independent plan review returned NOT CLEAR: three High findings, zero Medium/Low. Corrections below were submitted to the same reviewer on the rebased exact head; the subsequent accepted gate is recorded below.

Deployment alternatives need independent security assessment before any material owner choice. No App/secret/environment/settings profile is selected or activated. A context/App source pin is not proof of workflow identity; a new App is not presumed universally necessary. Remaining hosted proof obligations are explicit in the publisher contract.

## Plan review round 1 corrections

| Finding | Correction and counterexample |
| --- | --- |
| R1 High: observed invalidation could leave earlier successful check green | Publisher contract now mandates prior-success revocation, blocking in_progress/failure lifecycle, readback, and explicit enforcement-unknown on unconfirmed writes. Neutral/skipped are forbidden blocking results. |
| R2 High: per-PR serialization ignored commit-scoped shared check collisions | Shared repository/head/context/App key, complete bounded open-PR enumeration, singleton fail-closed predicate and before/after association resamples; observed collisions invalidate the shared check. |
| R3 High: partial activation comparison omitted lock.transition authority | Full closed lock bytes, including transition/null and every old/new/candidate field, authenticated at all samples; independently pinned channel cannot be redirected by candidate data. |

[Adversarial vectors](contracts/counterexamples.md) separate shape-valid transition tampering from authorization and give required publisher lifecycle outcomes. They are design regression inputs, not implemented behavior or hosted acceptance. The approved specification content is unchanged. The subsequent corrected-plan review closed all three findings; runtime remains blocked behind complete-artifact review.

## Full design and implementation gates

Independent cross-artifact analysis occurs only after approved plan and generated tasks. No implementation is authorized by the specification gate alone. Report actual exact-revision reviews, fixes, tests and acceptance gaps here; never convert a structural fixture or source check into deployment proof.

## Accepted corrected plan and rebased specification

The coordinator relayed the same independent `review_ready_spec` FINAL CLEAR on exact `88c9b8b2c8f5ecd6240c5065d0b9bd6de62632cc`, including rebased specification `23b22c0b5829a1fab07dea935ee61ce795d830cc` and corrected plan `1788539ab472d08967949372331c348b4c35a4ec`: zero open findings, including Low; all three prior High findings closed. This permits task generation only. No runtime-model attestation is claimed.

Owner-merged base changed from `9a704dbaf166ccc2bca57fce890b44cdc239374f` to `40b5befec12dac96e639ea58a6fbeda15eccd360`. The three owned commits were restacked without conflicts; all three range-diff entries were `=` and all Spec 028 content was byte-identical to corrected candidate `cd5d5e798544fd7c01ea832b090cb0be77069b30`. The old candidates remain preserved on local topic backup branches.

Fresh actual captured command logs at the rebased exact head show normal and race Go suites passing, gofmt clean, vet/module verification/architecture/docs (265 documents)/diff passing, and **79 Python source tests passing**, with exactly one actual Hugo packaging method asserted excluded before execution. The author's initial handback summary repeated the older count 76; 79 is the rebased run's actual logged count, not a rerun or altered historical result. Three schemas, five shape examples, six structural negatives, nine transition-only mutations plus null/omission/whitespace comparisons passed. These validate design structure and comparison expectations, not publisher implementation or hosted enforcement.

Actual captured logs, command exits and range-diff for exact checked revision `88c9b8b2c8f5ecd6240c5065d0b9bd6de62632cc` were retained locally and inspected by the independent reviewer; no permanent remote evidence archive is claimed. Node was unavailable; theme/minimum-version/hosted lanes and deployment acceptance remain unrun. No site/distribution/image build or installation occurred.

## Generated task candidate

[Tasks](tasks.md) contains 34 unchecked tasks with tests before behavior, all 19 functional requirements and six success criteria mapped, four independently testable stories, shared-file ownership and dependency order. US2 supplies the authenticated review that US1 consumes; inactive US3 publisher source is separate from its owner-gated deployment tasks; US4 establishes native effects and bounded refusal. The approved specification and normative contracts remain unchanged by task generation. The independent full-artifact gate is pending; no implementation, settings or credential resource is authorized by this record.

## Accepted full-artifact gate and bounded input increment

On 2026-10-18 the coordinator relayed independent `crewbook/review` full-artifact
FINAL CLEAR (configured strongest tier, `gpt-6-astra`/high) on exact design
`51a9015c5912d221e3d3e9fdafb2811c9572aff0`, covering the specification,
plan, research, data model, contracts, examples, tasks and traceability: zero
open findings, including Low. This completes the implementation design gate;
it grants no build, deployment, credential, settings or publication authority.
The configured review tier is not runtime attestation.

The first source increment owns T001–T004 only: this ledger, new hosting
compatibility tests and pure immutable lock/policy/report inputs. Shared
`internal/hosting/transport.go`, `scripts/check-architecture.py` and schema/site
inventories remain outside this author's ownership; the coordinator separately
owns their allocation. The three approved unpublished input schemas remain the
shape baseline. Existing hosting schema/fixture bytes and behavior are preserved.

| Requirement | Bounded evidence and remaining work |
| --- | --- |
| FR-005, FR-012 | Closed bounded input parsing; authentication, policy semantics and collection are later tasks. |
| FR-018 | Exact-byte published hosting compatibility assertions; Ready output/schema publication remains later work. |
| FR-019 | Failing parser tests precede implementation; independent source review and checks are recorded at handoff. |
| SC-001, SC-004, SC-006 | Parser/shape evidence only; evaluator/native/hosted acceptance and rollout remain incomplete. |

No T005+ evaluator, transport, CLI or publisher capability is implemented by this
increment. Fictitious examples establish shape only and grant no actual trust.

### T001–T004 source evidence

The bounded implementation adds standard-library strict token parsing and typed
immutable wrappers. `ParsePolicy`, `ParseLock` and `ParseReport` validate shape;
`Value` returns a fresh deep snapshot, `RawJSON` returns a defensive byte copy,
and `SHA256` hashes every original byte, including whitespace and lock transition.
These APIs prove no issuer authenticity, policy semantics, deployment authority
or runtime model identity. Cross-document and history predicates remain T009+.

TDD evidence: `go test ./internal/readiness -count=1` initially exited 1 for
missing parser APIs. With permissive typed scaffolding, the focused syntax/shape
suite exited 1 for accepted duplicate/escaped/nested keys, null roots, malformed
UTF-8/surrogates, depth, byte caps and closed-schema violations. Strict guards
then made it pass. Final independent-schema negative counts are policy 219,
lock 156 and report 178 (553 total), plus accepted enum, nullable, numeric,
string and maximum-array boundaries. These are regression fixtures, not an
independent source review. Depth 16 is accepted and 17 refused before nested
container allocation. JSON Schema string lengths count Unicode code points;
the command contract additionally caps each decoded string at 2,048 UTF-8 bytes.
Tests cover both bounds, valid surrogate pairs and exact document-size limits.

Checks on the unchanged production increment passed with Go 1.27.2 linux/arm64
and Python 3.13.5, under `umask 022`:

- `go test ./...` and `go test -race ./...` (exit 0); after final test-only boundary
  additions, `go test ./internal/readiness -count=1` and
  `go test -race ./internal/readiness -count=1` also passed (exit 0).
- `go vet ./...`, `go mod verify`, gofmt, `git diff --check`,
  `python3 -B scripts/check-docs.py` (266 documents) and
  `python3 -B scripts/check-architecture.py` passed (exit 0).
- 79 Python source tests passed (exit 0). The selector asserted exactly one
  exclusion, `test_docs.DocumentationTests.test_packaging_excludes_source_and_binds_revision`,
  before execution because it performs an actual Hugo build. Other schema/site
  tests used source or temporary HTML fixtures, not an actual site build.

`make check` was not invoked wholesale because its Python discovery includes
that unauthorized build and Node is unavailable. Its authorized source checks
were run individually; the Node theme tests remain unrun. Minimum-version and
hosted lanes, source independent review and all deployment acceptance remain
pending. Local captured RED/full Go/race/shape logs were retained for handoff;
no permanent remote evidence archive is claimed. No distribution, image, CLI
installation, actual Hugo site build, push or publication occurred.

### Independent source review correction: array-limit isolation

Independent `crewbook/review` returned NOT CLEAR on
`057a10b7d682c756cd1f9da0eac9856d363bf412`: one Medium, zero other findings,
including Low. The over-limit array fixtures repeated identical items, so
uniqueness rejection masked the array upper-bound guard. Removing only that
guard left the original parser suite green. This was a test-evidence gap;
no production parser defect was identified by that review.

The same author corrected tests only: each of the twelve approved array limits
now has distinct otherwise-valid max+1 items, an independent schema error tree
assertion requiring exactly one `maxItems` failure and no other leaf failure,
and an accepted exact-max control under both schema oracle and parser. Counts
remain policy 219, lock 156 and report 178 negatives (553 total); twelve existing
negatives now isolate cardinality instead of also violating uniqueness.

A temporary Go overlay removed only `|| int64(len(a)) > s.max` without editing
production files. `go test -overlay <temporary-overlay> ./internal/readiness
-count=1` exited 1 with exactly twelve `max-items` failures (eight policy, four
report). Without the overlay, focused normal/race parser and compatibility
checks passed. Production `parse.go`, `policy.go` and `report.go` are byte-identical
to the reviewed candidate, allowing its full Go/race evidence to remain applicable.
Documentation, architecture, formatting and whitespace checks passed again.
The previous candidate remains preserved locally; the corrected candidate
requires the same independent reviewer's exact-revision recheck. No source
CLEAR or deployment proof is claimed by this correction.
