# Tasks: Release process v2

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/](contracts/), [quickstart.md](quickstart.md), [analysis.md](analysis.md), [constitution-amendment-proposal.md](constitution-amendment-proposal.md).
**Tests**: Required by constitution principle IV. Write each behavior test, observe it fail, then implement. Task checkboxes record execution, not design approval. TDD and mutation expectations: every gate, every `check` finding, every parser rule and every workflow rule has a test that fails when that rule is removed or weakened (for gates and parser rules the review samples at least one single-edit mutant per rule; for workflows the rule/mutation suite of WI8 is the mutant set); hostile-input cases and effect sentinels are listed per work item.
**Work items**: WI1 to WI11, one reviewable change each, stacked in this order, each ending with `make check` green. Each task names the requirements (FR) and stories (US) it covers.
**Authorization boundary**: WI1 to WI5 are source-only (WI5 adds disclosed read-only GitHub reads). **WI6 to WI11 add effects (a remote write, project command execution, workflows, a release) and wait for the owner's decision on the constitution amendment proposal.** No task builds an artifact, runs a workflow, tags, publishes or activates anything; WI10 and WI11 each need the owner's explicit approval for any such step, named in the task.
**Cut order if scope must shrink** (least valuable first): `build --mode local-verify`, `plan --for build` on tags (second input mode), the `readiness` gate, `pattern` scheme (keep `semver`, `calver`), SBOM role, `release-repro.yml`.

## Phase 1: Analysis

- [ ] T001 Record independent cross-artifact analysis in specs/026-release-process/analysis.md, resolve every critical, high and medium finding in the artifacts, preserve review provenance.

## WI1 - Contract model, strict TOML profile and `stw release check`

- [ ] T002 [FR-002, FR-023, US1, US7] Add failing fixture tests, then add schemas/v1/release-config.schema.json (from contracts/config.md) and internal/contracts/release_config_test.go: the four worked examples valid, the seven negative mutants and one fixture per schema rule invalid, with the repository's test-only JSON Schema library; the schema file equals the schema block of contracts/config.md (a test extracts and compares).
- [ ] T003 [P] [FR-002, US1] Add failing tests, then implement the strict TOML profile parser in internal/release/contract.go: tables, arrays of tables, strings, integers, booleans, arrays of strings; reject dotted keys, inline tables, split tables, duplicate keys, floats, dates, multi-line strings, nested arrays, BOM, NUL, over 64 KiB; every rule has a test and a one-rule mutant. Add scripts/test_release_toml.py comparing accepted files with Python `tomllib` and asserting that the three constructs `tomllib` accepts are rejected; add a Go fuzz target (no panic, no accepted input outside the profile).
- [ ] T004 [P] [FR-002] Add failing tests, then implement the JSON loader (duplicate keys refused, same model) and the model types and semantic validation in internal/release/validate.go: closed placeholders per field, references, unique ids, `once`/`per-target` rules, `promote` iff `oci-image`, paths confined (`..`, absolute, `dist/release/` rule), forbidden environment names, limits.
- [ ] T005 [FR-006, US1] Add failing tests, then implement allowlist glob matching (`*`, `**`) and the overlap rules in internal/release/allowlist.go (`.stw/`, `.github/`, version file, readiness files, lock and tool-list files, working directories, existing repository paths named in argv); hostile names (newline, leading dash, case-only, non-UTF-8) table; fuzz the matcher.
- [ ] T006 [FR-001, FR-014, FR-015, US1] Add failing CLI tests, then add internal/cli/release.go (`stw release check [PATH] [--commit SHA] [--json]`), result types, schemas/v1/release-check.schema.json, human and JSON renderers from one result, one escaping function, exit statuses 0, 1, 2, 3 with precedence; the ownership order per the capability table (jj, GitButler, ambiguous, bare, not a repository).
- [ ] T007 [FR-001] Extend scripts/check-architecture.py (and its test): `internal/cli` may import `internal/release`; process starts in `internal/release` only in `vcs_*.go`, `attest.go`, `build.go`; no network outside `internal/hosting`. Failing test first.
- [ ] T008 [FR-016, SC-005] Add effect sentinels for `check`: index bytes and mtime, refs, HEAD, configuration, no new files, no process after detection (working-tree form), hook/filter/textconv sentinels, jj working-copy and operation-log sentinels on the jj lanes; record which cases the environment can exercise (jj is not installed in the authoring environment).

## WI2 - Version schemes, record schema and canonical encoding

- [ ] T009 [P] [FR-004, US7] Add failing tests, then implement `semver`, `calver` and `pattern` in internal/release/version.go: validity (whole-string, trailing newline refused), tags, tracks, numeric tuple comparison with 9-digit bound, absent-below-present; table-driven monotonicity cases from data-model.md including `v0.10.0 > v0.9.0` and `2026.10.15-b98.1`; `scheme-invalid` findings (pattern compile, missing group); mutants for each scheme rule.
- [ ] T010 [FR-003, FR-023, US4, SC-007] Add failing fixture tests, then add schemas/v1/release-record.schema.json, release-build-manifest.schema.json and release-oci-result.schema.json (from contracts/record.md) and internal/contracts/release_record_test.go: the four example records valid; mutants (empty `tags`, `index_digest` on a file asset, unknown key, short digest) invalid.
- [ ] T011 [FR-003] Add failing tests, then implement record, manifest and OCI result types and the canonical encoder in internal/release/record.go: sorted keys, two-space indent, arrays one element per line, trailing newline, integers only, escapes; round trip equals input bytes for the four example records; `record-not-canonical` for key-order, whitespace, escape and trailing-newline mutations.
- [ ] T012 [P] [FR-003] Semantic record validation (`record-invalid`): version against scheme, tag consistency, unique asset names, checksum coverage, placeholder closure, limits; one test per rule.

## WI3 - Offline `plan <commit>` for Git and jj

- [ ] T013 [FR-005, FR-014, US2] Add failing tests with temporary Git repositories, then implement internal/release/vcs_git.go reads (isolated prefix with `GIT_CONFIG_NOSYSTEM`, `GIT_CONFIG_GLOBAL=/dev/null`, replace objects off, capped output): resolve full ids, `merge-base --is-ancestor` against `refs/remotes/origin/main`, blob and tree lookups, shallow-clone and missing-ref `not-run` reasons.
- [ ] T014 [FR-005, US2] Add failing tests, then implement the build gates `commit-not-on-main`, `config-invalid`, `version-invalid`, `version-file-mismatch`, `readiness-missing` and the verdict logic (internal/release/gates.go, plan.go); one fixture repository per gate; Net gates `not-run` offline; `would_build` rendering without execution.
- [ ] T015 [FR-014, FR-016] jj reads in internal/release/vcs_jj.go through the existing read-only runner for `check --commit` and `plan <commit>`; conformance script scripts/smoke-release.py on the jj lanes with working-copy snapshot and operation-log sentinels; record verified versus unverified statements; refusal tests for `plan <tag>`.
- [ ] T016 [FR-015] `stw release plan` CLI, schemas/v1/release-plan.schema.json, human and JSON output, usage errors (abbreviated commit, missing `--version`, tag with `--version`), exit statuses; hostile text (tag names, paths) escaping tests.
- [ ] T017 [FR-016, SC-005] Effect sentinels for `plan`: no process other than the allowlisted Git/jj invocations, no network, nothing written, hook/filter/textconv/fsmonitor sentinels, index sentinel on a stat-dirty file.

## WI4 - `plan <tag>`, `verify` (offline file checks) and `record`

- [ ] T018 [FR-006, US2, US5] Add failing tests, then implement the net diff classification in internal/release/allowlist.go and vcs_git.go: `diff-tree -r --raw -z --no-renames --no-ext-diff --no-textconv` parsed with a bounded streaming parser; statuses, modes, symlink, gitlink, type change, forbidden directories, non-UTF-8 paths, 20,000-entry cap; the rebase-merge fixture of quickstart section 2 (ancestry, new SHAs); a filter/textconv sentinel proves no content is read.
- [ ] T019 [FR-005, US5] Publish gates offline: `tag-not-annotated`, `tag-version-mismatch`, `tag-not-on-main`, `record-missing`, `record-invalid`, `record-not-canonical`, `build-commit-not-ancestor`, `diff-not-allowlisted`, `config-invalid` at the tag commit; fixtures per gate (lightweight tag, tag on a side branch, missing and edited record, disallowed diff entries of every class).
- [ ] T020 [FR-005, US5] Artifact gates in internal/release/artifacts.go: bounded streaming hashing (2 GiB), regular files only (symlinks, subdirectories, devices refused), `artifact-missing`, `artifact-unexpected`, `artifact-mismatch`, `SHA256SUMS` parsing (64 KiB, 64 lines, `sha256sum -c` format incl. the binary marker, no path components); hostile directory fixtures.
- [ ] T021 [FR-001, US5] `stw release verify` CLI, schemas/v1/release-verify.schema.json, jj and GitButler refusals with the CI alternative message, output and exit statuses.
- [ ] T022 [FR-003, FR-016, US4] `stw release record` in internal/release/record.go and CLI: manifest and checksum agreement, lock and tool-list hashing from C's tree, byte-deterministic output (run twice, compare), `--write` with `O_EXCL`, identical-file no-op, `output-exists`, `--check`, jj snapshot disclosure, schemas/v1/release-record-result.schema.json; sentinels: only the one new file changes.
- [ ] T023 [SC-004] Verification matrix test: one case per SC-004 item through `verify` with a stub online layer; mutation sample on each gate.

## WI5 - Online reads and attestation verification (read-only network)

- [ ] T024 [FR-005, US2] Add failing tests with the in-process HTTP seam, then add internal/hosting/release.go reads: workflow runs by `head_sha` and file, releases (drafts when visible), tags, the `main` ref; bounds (10 pages, 1,000 records, 1 MiB), redirects and compression refused, credentials redacted, typed results; `drafts-invisible` and truncation handling.
- [ ] T025 [FR-005] Net gates in plan/verify: `ci-not-green` (reasons absent, failed, pending), `version-not-monotonic` (tags plus releases, track-aware, non-matching ignored and counted), `release-exists`, `already-published`, `draft-missing`; fail closed on any non-404 API error for existence checks; `--online`, `--repo` selection by hosting rules, `network` list in the result.
- [ ] T026 [FR-011, US5] internal/release/attest.go: bounded `gh attestation verify` with filtered environment, empty temporary directory, 60 s, 1 MiB; strict JSON parse; subject digest equality with the record; manifest commit equality; `--bundle` offline seam for tests; `attestation-missing`/`attestation-invalid` cases; record the process output shape as unverified until rehearsal.
- [ ] T027 [FR-016] Network sentinel: offline commands make no request; online commands make only the listed GETs; `gh auth token` only with `--online`.

## WI6 - `dispatch` (first remote write; blocked on the constitution decision)

- [ ] T028 [FR-010, US3] Add failing tests, then implement the dispatch plan and stateless `plan_id` (canonical JSON of repository, host, workflow, ref, inputs, resolved commit, remote main commit, contract hash, verdict) in internal/release/dispatch.go; mutation of any field changes the id.
- [ ] T029 [FR-010, US3] `postFixed` in internal/hosting: exactly the dispatch endpoint and method, 204 expected, no redirects, no retry, status-only errors; tests that other methods and paths fail closed, that a body error never leaks, and that exactly one request is sent.
- [ ] T030 [FR-010, FR-012, US3] CLI `dispatch` with `--commit/--version` and `--tag`, plan then `--apply PLAN_ID`, `plan-stale` (main moved, contract changed, gate changed between plan and apply), `gate-failed`, disclosed effects, schemas/v1/release-dispatch.schema.json; concurrency fixture: main advances between plan and apply.

## WI7 - `build` (CI execution; blocked on the constitution decision)

- [ ] T031 [FR-008, US6] Add failing tests with a fake command runner, then implement internal/release/build.go environment construction, placeholder substitution, working-directory confinement, timeouts with process-group kill, bounded output, `ci-only`, `checkout-mismatch`, `output-exists`; assert exact environment and argv per build scope (`per-target`, `once`).
- [ ] T032 [FR-008, FR-003] Output collection, `SHA256SUMS` and `build-manifest.json` (canonical), OCI result validation, `unexpected-output` notes, `--files-only`, `--smoke TARGET`; schemas/v1/release-build.schema.json.
- [ ] T033 [FR-008] `--mode promote`: record digest equality, `[promote].argv` substitution, `promote-failed`, refuses outside CI; `--mode local-verify`: tree-object export with `export-unsafe` fixtures (symlink, gitlink, `.git`, `..`, absolute, duplicate, case collision, grafts), hash comparison against a record; Git-only, jj refusal.
- [ ] T034 [FR-016, SC-005] Prove no test runs a real product build; scan test that `internal/release` command execution is reachable only from `build` and that `check`, `plan`, `verify`, `record`, `dispatch` never reach it (import/call graph test).

## WI8 - Workflows and their tests (blocked on the constitution decision)

- [ ] T035 [FR-018, SC-006] Add failing tests, then add internal/releaseworkflow (test-only) with the rule set of contracts/workflow.md and about 60 single-edit mutants plus a golden change detector; add the pinned test-only YAML dependency with justification in the commit.
- [ ] T036 [FR-009, FR-011, FR-012] Add .github/workflows/release-build.yml (gates, build, smoke, draft) to satisfy the rules; no run.
- [ ] T037 [FR-009, FR-012] Add .github/workflows/release-publish.yml (verify, promote, publish) with no build step (rule test), `environment: release`; no run.
- [ ] T038 [FR-017] Add .github/workflows/release-repro.yml (dispatch only, no schedule); rule test that no `schedule` trigger exists unless the owner approved it.
- [ ] T039 [FR-018] Test that no other workflow triggers on release tags or on `workflow_dispatch` of the release workflows, and that `ci.yml` has no release step.

## WI9 - Documentation, contracts publication and status

- [ ] T040 [P] Write docs/release.md (concepts, contract, record, commands, gates, what each proves and does not), docs/releasing.md (owner procedure, settings click path, rehearsal checklist, failure handling), update docs/commands.md, docs/backends.md (per-backend capability), docs/json-contracts.md, docs/agent-guide.md (exit status 3 meaning for the release commands), docs/README.md, llms.txt, README scope line; keep `python3 scripts/check-docs.py` passing.
- [ ] T041 [P] Extend scripts/package-pages.py `SCHEMA_PATHS` with the ten schemas; ensure the generated command reference includes `stw release` through cmd/stw-docs; do not run a site build without approval.
- [ ] T042 Update docs/feature-status.json (`026-release-process`), docs/progress.md and docs/roadmap.md only with real evidence; status stays `planned` until acceptance.

## WI10 - `stw` bootstrap release preparation (needs owner approval before any run or tag)

- [ ] T043 [FR-024, DD26] Add the missing Cobra and pflag notices to third_party/README.md (test: notices cover the module closure of ./cmd/stw); add scripts/release-package.sh and scripts/release-smoke.sh (own-script adapter), the stamp test skipped unless `STW_RELEASE_VERSION` is set, and .stw/release.toml (the go-cli example adapted); `make check` runs `stw release check` on it. Unit tests with a fake compiler; no real build.
- [ ] T044 [FR-024] Add .github/workflows/release-bootstrap.yml per contracts/bootstrap.md with the rule tests of WI8; no run.
- [ ] T045 [FR-024] Owner-approved dry run only: **requires explicit approval**; otherwise leave unchecked. Rehearse per quickstart section 6.

## WI11 - Acceptance evidence and the self-hosting switch

- [ ] T046 [SC-001 to SC-009] Run `make check`, `go test -race ./...`, `python3 scripts/check-architecture.py`, the smoke scripts and the release conformance lanes; record exact versions, platforms and outcomes in specs/026-release-process/verification.md, labelling arm64 and darwin results as local evidence and listing everything not run (jj lanes, GitHub behavior).
- [ ] T047 [FR-024] After the owner's `v0.1.0` release by the bootstrap workflow (separately authorized, not part of this task list's execution): add docs/releases/v0.1.0.json via `stw release record`, remove release-bootstrap.yml, document the first self-hosted release procedure; the first self-hosted release is the owner's separately authorized step.
- [ ] T048 Update docs/progress.md, docs/feature-status.json and the roadmap with the actual evidence; keep release, tag, publication, new CI platforms and consumer activation out of this feature.

## Dependencies

WI1 before everything. WI2 before WI3 (records and versions). WI3 before WI4. WI4 before WI5. WI6 needs WI5 (plan reads) and the amendment decision. WI7 needs WI2 (record/manifest) and the decision. WI8 needs WI6 and WI7. WI9 may start after WI1's contract text is stable and finishes after WI8. WI10 needs WI7, WI8 and the owner's approval; WI11 last. Tasks marked [P] within a work item touch different files and may run in parallel. Follow-up specs 028 and later (package channels) depend on the record schema of WI2.
