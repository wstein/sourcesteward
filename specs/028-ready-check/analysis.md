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

## T005–T006 pure result and reduction increment

The bounded source increment `6a54918569b58ac573f15df68b5d7fa55fc71150` starts
from reviewed input increment `75b897cf8b4f5c9923b5c92feed5bb88ce4696a9` and
implements only result construction, component reduction and purity guards.
The accepted full-artifact design remains
`51a9015c5912d221e3d3e9fdafb2811c9572aff0`. No T009/T011 review authentication,
CI/constraint/deployment predicate, hosting collector, CLI, published schema or
publisher is implemented by this scaffold. Caller-supplied passing component
projections are test inputs, not authenticated review evidence or installed-gate
acceptance. T005–T006 still require independent exact-source review and the
coordinated full source/race check before final handoff.

`Evaluate(policy, sample)` returns an immutable `Result` and an error. Invalid
operation timestamps or native JSON snapshots return an error and no result
bytes; the evaluator never invents an observation time. Valid operational-error
samples retain a contract result with `decision=error`, incomplete boundary and
incomplete review. Other reductions preserve known failures alongside unknown
components; incomplete passing components cannot pass. Only an undeployed
policy permits deployment `not-required`. Reasons follow the documented family
and code order, with repeated subjects sorted by UTF-8 bytes and source ties
ordered deterministically. Raw GitHub booleans remain nullable display metadata
here; future explicit predicates own their meaning.

Native identity uses an operation-supplied defensive `json.RawMessage` snapshot
of the existing `inspection.Result` representation. Git/jj fixture comparisons
preserve native fields, including jj change/commit/operation identities and
observation limits. Strict JSON/object/backend validation proves no native
resolution or source authenticity. Typed native adaptation belongs to the
future hosting operation boundary: importing `internal/inspection` here would
also import its external-state capabilities. Backend and identity can be null
only for early operational failure. `Value` and `RawJSON` return fresh copies;
mutation tests cover supplied and returned nested slices, pointers and bytes.
Runtime attestation is always `unavailable`, independent of configured metadata.

Purity uses a closed reviewed value-library import allowlist, across all
readiness source files and subpackages, plus Go syntax-tree checks restricting
`fmt` to `Errorf`/`Sprintf`, refusing built-in output and bodyless functions.
The guard refuses clock, filesystem, process, environment, network, unsafe,
cgo, foreign source, linkname and hidden project/third-party adapter imports.
Tests may use native fixtures and filesystem helpers. Standard-library internal
dependencies alone are not effects: `encoding/json` and the existing parser's
value-formatting APIs transitively import packages with broader capabilities.
The guard therefore checks direct usable imports/APIs instead of rejecting that
entire standard-library closure. No callbacks or interface-valued operation
fields enter evaluation.

TDD evidence is retained in local `/tmp/stw-ready-evaluator-*.log` receipts:

- Missing result APIs first failed compilation. A permissive typed scaffold
  then failed behavior assertions for unknown/incomplete/failed/error reduction,
  installed deployment `not-required`, missing UTC timestamps and reason order.
- Purity fixtures initially accepted forbidden imports; alias/dot/blank and
  hidden-subpackage variants now refuse. Separate RED fixtures exposed foreign
  source suffix gaps and built-in `print`/`println`, then passed after guards.
- Further RED fixtures exposed error-boundary completeness, invalid UTF-8 raw
  merge state and valid explicit UTC `+00:00` handling. The final implementation
  passes those controls, including `Z`, leap-day and fractional timestamps,
  while rejecting unknown `-00:00` and non-UTC offsets.
- `go test ./internal/readiness -count=1`, 12 Python architecture tests,
  `python3 -B scripts/check-architecture.py`,
  `python3 -B scripts/check-docs.py` and `git diff --check` passed (exit 0).

Full normal/race checks are queued with the coordinator, not claimed complete.
The required aggregate `make check` contains an actual Hugo packaging test
requiring separate owner build approval; that exact method will be asserted
excluded while other source tests run. Node is unavailable in this environment;
theme tests are consequently unavailable. No artifact/site/image build,
installation, hosted acceptance, publication, settings change or API write ran.

### T005–T006 independent review corrections

Independent strongest-tier review of exact
`08ff6a50ffb2d5c4cbf3cdb76c0d48bf72361537` returned NOT CLEAR: two Medium
findings, no Low. Current evaluator entry inputs were concrete, but the guard
accepted hypothetical public callbacks and `io.Reader`/`io.Writer`, custom
JSON marshaler and Stringer inputs. The review executed an accepted custom
marshaler that wrote a temporary file. The guard also accepted parenthesized
`(println)(...)`, which compiled and produced output. These were guard defects;
the review did not identify an implemented evaluator semantic failure.

Regression tests first failed for all thirteen public capability examples and
parenthesized output. The corrected guard type-checks each complete readiness
package and recursively inspects exported functions, variables, value types and
method sets, including aliases, private nested fields, promoted methods,
recursive containers and generic exposures. Public values must be concrete and
capability-free; returned callable/interface capabilities refuse too. Built-in
`error` remains permitted only as an outbound diagnostic result. The existing
concrete `json.RawMessage` snapshot is retained by exact type identity, including
its standard-library alias representation, without admitting arbitrary
marshaler implementations. Internal parser interfaces and pure concrete
JSON/formatting calls remain supported. Call classification now unwraps nested
parentheses before refusing either built-in output function.

Both focused regression groups pass. Separate temporary Go overlays disabling
public-API checking or parenthesis unwrapping each compiled and failed the
corresponding rejection assertions (exit 1); no product source was mutated.
Receipts are `/tmp/stw-ready-evaluator-review-fixes-red.log`,
`/tmp/stw-ready-evaluator-review-fixes-green.log` and
`/tmp/stw-evaluator-guard-mutants-eydxhcq6/`. Corrected targeted readiness,
Python architecture, and complete architecture checks passed (exit 0).
The candidate requires the same independent reviewer's exact-revision recheck.
Full normal/race checks remain queued behind the coordinator's active gate;
no aggregate suite, build, installation or hosted operation ran in this fix.

The same reviewer rechecked exact
`7cb54e621183c1a0451e408fa0423daaa028d7a2`: parenthesized output was closed,
but the capability finding remained Medium. An `error` return could hide a
private concrete implementation with exported `Invoke(func())`; an external
type assertion recovered that method and executed a file-writing callback.
Allowing the interface solely because it was an outbound error was insufficient.

Eleven otherwise-valid error-mediated rejection fixtures first failed for
value/pointer methods, error/result aliases, callback fields, nested recursive
containers and readers, generic instances/methods, promoted methods and
anonymous error values. The guard now audits every package-local concrete error
implementation and its pointer method set, including private named types and
anonymous/instantiated expression types, using the same recursive value and
method checks before permitting the diagnostic interface result. This bounded
audit requires no factory body-flow inference. Pure standard-library error
outputs and concrete local string-only value/pointer errors remain accepted.
No production evaluator or input-parser behavior changed.

The eleven rejections and four accepted controls pass. A temporary overlay
disabled only the concrete-error audit: it compiled, then failed the intended
rejection tests (exit 1). Receipts are
`/tmp/stw-ready-evaluator-error-capabilities-{red,green}.log` and
`/tmp/stw-evaluator-error-mutant-7yjxbgs3/result.log`. Corrected readiness and
complete architecture checks passed (exit 0); exact-source review and the
coordinated aggregate verification remain pending. No aggregate/build/install
or hosted operation ran in this correction.
