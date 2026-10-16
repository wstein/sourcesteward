# Tasks: Release hygiene for the first `stw` release

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/release.md](contracts/release.md), [contracts/readiness-record.md](contracts/readiness-record.md), [analysis.md](analysis.md).
**Tests**: Required by constitution principle IV. Write each behavior test and observe it fail before the implementation. Task checkboxes record execution, not design approval.
**Work items**: WI1 to WI6 in stacking order; each is one reviewable change that ends with `make check` green. Each task names the requirements (FR) and stories (US) it covers.
**Approvals**: No task builds a distributable artifact, tags, publishes or activates anything. Tests use fake runners. Tasks R1 to R6 at the end are the release itself, are not implementation work and each requires the explicit user authorization named in the [approval table](spec.md#approvals-normative).

## Phase 1: Analysis

- [ ] T001 Record independent cross-artifact analysis in specs/026-release-hygiene/analysis.md and resolve every critical or high finding before any code.

## WI1 — Stampable version and stamp test

- [ ] T002 [FR-002, FR-003, US2] Add the failing tests first: internal/cli/version_test.go with `TestVersionDefaultsToDev`, `TestVersionCommandUsesStamp` (set `Version`, run `Run`, compare text and JSON), and `TestReleaseStamp` that skips unless `STW_RELEASE_VERSION` is set and otherwise fails with `stamp-missing` (`dev`), `stamp-mismatch` or `version-invalid`; observe `TestReleaseStamp` fail with the variable set and no flag.
- [ ] T003 [FR-002, FR-014, US2] Add internal/cli/version.go (`var Version = "dev"`), change cmd/stw/main.go and cmd/gh-jj/main.go to pass `cli.Version`, remove `var version`; existing tests, `version.schema.json` and the unstamped output stay identical. Confirm with `go test -ldflags "-X github.com/wstein/sourcesteward/internal/cli.Version=v0.1.0" -run TestReleaseStamp ./internal/cli` under `STW_RELEASE_VERSION=v0.1.0` (pass) and with a misspelled variable (fail).
- [ ] T004 [FR-002, US2] Add a version-format test table (valid and invalid strings: `v0.1.0`, `v0.1.0-rc.1`, `0.1.0`, `v01.0.0`, `v0.1.0+x`, empty, whitespace, newline) shared by the Go test and the script (the pattern lives once in scripts/release-gates.json).

## WI2 — Release script: plan (dry run), gates and sentinels

- [ ] T005 [P] [FR-008, FR-017, US1] Add scripts/release-gates.json (gate ids, codes, waivable flag, `hidden_commands`) and failing scripts/test_release.py tests for loading it and for equality with the spec gate table (a test parses the table in spec.md).
- [ ] T006 [FR-004, FR-005, US1, US3] Add failing tests then scripts/release.py `plan` skeleton with a command-runner seam (argument vectors recorded, no process in tests), the constructed environment (polluted-environment fixture including `GOFLAGS=-trimpath`, `GOTOOLCHAIN=auto`, `GOENV`, `GOEXPERIMENT`), `toolchain-mismatch`, `go-env-unsafe`, `version-invalid`, `tree-dirty`, `commit-mismatch`, exit-status precedence 2, 3, 1, 0, human and JSON from one result, 200-line bound.
- [ ] T007 [P] [FR-008, SC-006] Add failing tests then the notices closure check (G07) using `go list -deps` output fixtures: complete, one module missing, `cmd/gh-jj` closure equal; offline, no network.
- [ ] T008 [P] [FR-008, SC-002] Add failing tests then the platform evidence reading from the record (`ci`, `local-recorded`, `manual`, `not-run`), `evidence-missing` dropping a platform, `--final` requiring G10 and G11, and `pending` rejection.
- [ ] T009 [FR-005, SC-002] Add an effect sentinel test: `plan` on a fixture repository with a recording runner writes no file outside a temporary Go cache directory, invokes no network program, and lists exactly the allowed argument vectors; add the statement that no distributable binary was linked.
- [ ] T010 [P] [FR-008, SC-003, US4] Add the scan test: no `.github/workflows` file with a `push`, `pull_request`, `schedule` or tag trigger contains `release.py build`, `gh release`, `git tag`, `git push`, `goreleaser`, `softprops/action-gh-release` or an upload of `dist/release`; no `make check` recipe invokes `release.py build`; `/dist/` stays ignored. Include a hostile fixture workflow that must be caught.
- [ ] T011 [FR-008, FR-014, US1] Add the Go test in internal/cli that reads `hidden_commands` from scripts/release-gates.json and asserts equality with the real root's `Hidden` commands (today `pins`) and absence of those names in root and per-command help; add the `plan` scan of docs/README/llms.txt/release notes (`hidden-command-advertised`) with a fixture that mentions `stw pins`.

## WI3 — Notices for the whole binary

- [ ] T012 [FR-008, SC-006] Add the Cobra (Apache-2.0) and pflag (BSD-3) license texts and notices to third_party/licenses and rows to third_party/README.md, generalize its title and scope text from "Hosting SDK" to the full `stw` closure, keep identical texts stored once, and make T007's check pass for the real repository. Read the licenses from the module cache; do not paraphrase.
- [ ] T013 [P] [FR-006] Add the archive content rule (binary, LICENSE, third_party/, README) to the script's tests with a fixture; no real archive.

## WI4 — Release script: build and verify (fake-runner tested)

- [ ] T014 [FR-004, FR-006, FR-011, US3, US4] Add failing tests then `build`: refuses without A1 matching commit, version and platforms (exit 3, no `go`), refuses an existing output directory or tag, constructs the recipe environment and `go build` argument vector exactly, runs the stamp test first, normalizes archive metadata (sorted, owner 0, `SOURCE_DATE_EPOCH`, modes, gzip mtime 0), writes `SHA256SUMS` in `sha256sum -c` format and `release-manifest.json`, removes partial output on failure. All with a fake runner; no real build.
- [ ] T015 [FR-005, FR-013, US5] Add failing tests then `verify`: tampered archive (`checksum-mismatch`), missing `SHA256SUMS` entry, unstamped binary fixture, build-information mismatch (`buildinfo-mismatch`) from `go version -m` fixture output, foreign-platform binary reported `execution: not-run` and `stamp_verified: buildinfo`, never executing a binary whose checksum failed, smoke S1 to S5 argument vectors and minimal environment.
- [ ] T016 [FR-012] Add `verify --rebuild` and the reproducibility comparison logic (binary hashes only; archive bytes not compared) with fake-runner tests for `match`, `mismatch`; requires A1.

## WI5 — Platform lanes (optional per platform; drops the platform if not done)

- [ ] T017 [FR-001, FR-015, US1] Extend scripts/backend-releases.json and scripts/install-backends.py for `linux-arm64` jj artifacts (official release names and SHA-256 values verified against the release assets) with failing tests in scripts/test_backends.py first. Network use is for authoring the hashes only.
- [ ] T018 [FR-001, FR-015] Add `ubuntu-24.04-arm` rows to the existing check and conformance jobs in .github/workflows/ci.yml (same steps, no release step) and a macOS arm64 conformance job only if `sandbox-exec` and Git source builds are shown to work; otherwise leave darwin arm64 as `local-recorded` plus `manual`. Pin actions by SHA like the others. Record outcomes; do not claim lanes that did not pass.

## WI6 — Documentation and templates

- [ ] T019 [P] [FR-007, FR-009, FR-011] Write docs/releasing.md (procedure, approvals A1 to A4, recipe, signing and notarization statement, evidence classes, no automation) and docs/releases/TEMPLATE-readiness.md and TEMPLATE-notes.md following contracts/readiness-record.md; link from docs/development.md, docs/README.md and AGENTS.md; keep README's "No release binaries have been published." until A3 completes; keep `python3 scripts/check-docs.py` passing.
- [ ] T020 [P] [FR-016] Add the convention list for smill to docs/releasing.md as a short section and state that adoption is a separate specification in wstein/sheetmill.
- [ ] T021 [P] [FR-001] Add `make release-plan` (runs `release.py plan` only); there is no `release` or `release-build` target.
- [ ] T022 [SC-007] Update docs/progress.md, docs/feature-status.json (026 stays `planned` until a release readiness record exists with all gates resolved) and docs/roadmap.md with the actual evidence; do not mark released.

## Release steps (not implementation; each needs the named approval)

- [ ] R1 [A1] On explicit authorization naming commit, version and platforms: run `go mod download` (network, visible) then `release.py build`; run the second independent build in another directory for G12.
- [ ] R2 Run `release.py verify`; run S1 to S5 on every platform; on the Mac mini follow the quickstart as `manual` evidence; complete the readiness record and notes.
- [ ] R3 [A2] On explicit authorization: create the tag.
- [ ] R4 [A3] On explicit authorization: publish files and notes.
- [ ] R5 After publication: repeat `verify` on the downloaded files (quickstart steps 4 to 6).
- [ ] R6 [A4] On explicit authorization per consumer: activate (Anchorage devcontainer, BundleDock lock); outside this repository.

## Dependencies

WI1 before WI4 (the recipe needs the variable). WI2 before WI4. WI3 before the first real `plan` can pass. WI5 is independent and may be skipped, which drops the platform. WI6 finishes last. R-steps follow WI1 to WI4 and WI6 and never happen automatically.
