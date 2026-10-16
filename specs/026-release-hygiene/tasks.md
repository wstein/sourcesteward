# Tasks: Release hygiene for the first `stw` release

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/release.md](contracts/release.md), [contracts/readiness-record.md](contracts/readiness-record.md), [analysis.md](analysis.md).
**Tests**: Required by constitution principle IV. Write each behavior test and observe it fail before the implementation. Task checkboxes record execution, not design approval.
**Work items**: WI1 to WI6 in stacking order; each is one reviewable change that ends with `make check` green. Each task names the requirements (FR) and stories (US) it covers.
**Approvals**: No task builds a distributable artifact, tags, publishes or activates anything. Tests use fake runners. Tasks R1 to R6 at the end are the release itself, are not implementation work and each requires the explicit user authorization named in the [approval table](spec.md#approvals-normative).

## Phase 1: Analysis

- [ ] T001 Record independent cross-artifact analysis in specs/026-release-hygiene/analysis.md and resolve every critical or high finding before any code.

## WI1 — Stampable version and stamp test

- [ ] T002 [FR-002, FR-003, US2] Add the failing tests first: internal/cli/version_test.go with `TestVersionDefaultsToDev`, `TestVersionCommandUsesStamp` (set `Version`, run `Run`, compare text and JSON; wiring in cmd/stw is covered by executed S1 and S2), a misspelled-variable case that is a Go test running `go test -ldflags` on a copy only through the recipe's pre-build check (T014), and `TestReleaseStamp` that skips unless `STW_RELEASE_VERSION` is set and otherwise fails with `stamp-missing` (`dev`) or `stamp-mismatch` (including a value that does not match the version format); observe `TestReleaseStamp` fail with the variable set and no flag.
- [ ] T003 [FR-002, FR-014, US2] Add internal/cli/version.go (`var Version = "dev"`), change cmd/stw/main.go and cmd/gh-jj/main.go to pass `cli.Version`, remove `var version`; existing tests, `version.schema.json` and the unstamped output stay identical. Confirm with `go test -ldflags "-X github.com/wstein/sourcesteward/internal/cli.Version=v0.1.0" -run TestReleaseStamp ./internal/cli` under `STW_RELEASE_VERSION=v0.1.0` (pass) and with a misspelled variable (fail).
- [ ] T004 [FR-002, US2] Add a version-format test table (valid and invalid strings: `v0.1.0`, `v0.1.0-rc.1`, `0.1.0`, `v01.0.0`, `v0.1.0+x`, empty, whitespace, `v0.1.0\n`) run by the Go test (`MatchString` with `\A...\z`) and by the script (`re.fullmatch`); the pattern lives once in scripts/release-gates.json.

## WI2 — Release script: plan (dry run), gates and sentinels

- [ ] T005 [P] [FR-008, FR-017, US1] Add scripts/release-gates.json (gate ids, codes, waivable flag, `hidden_commands`) and failing scripts/test_release.py tests for loading it and for equality with the spec gate table (a test parses the table in spec.md).
- [ ] T006 [FR-004, FR-005, US1, US3] Add failing tests then scripts/release.py `plan` skeleton with a command-runner seam (argument vectors recorded), the constructed environment (polluted fixture including `GOFLAGS=-trimpath`, `GOTOOLCHAIN=auto`, `GOENV`, `GOEXPERIMENT`, a caller `GOPATH` differing from `HOME`), `toolchain-mismatch`, `go-env-unsafe`, `modules-unavailable`, `version-invalid` and `platform-unsupported` (exit 2, empty stdout), exit-status precedence 2, 3, 1, 0, human and JSON from one result, 200-line bound. Export from tree objects, never `git archive`: fixtures with `info/attributes` `export-ignore` and `export-subst`, a local clean/smudge filter that must not run, a symlink and a gitlink in the tree (`export-unsafe`). Real temporary-repository tests (not fakes) for G01: untracked file, ignored `.go` file (absent from the export), detached HEAD, unknown commit (`commit-unresolved`), colocated jj repository (read through Git plumbing, skipped without jj) and a jj workspace without Git (`unsupported-checkout`).
- [ ] T007 [P] [FR-008, SC-006] Add failing tests then the notices closure check (G07) over the union of the closures of the shipped targets plus the Go standard library row, using `go list -deps` output fixtures: complete, one module missing, a module present on one target only; offline, no network.
- [ ] T008 [P] [FR-008, FR-009, SC-002] Add failing tests then the record reader: evidence classes per platform, `evidence-missing` dropping a platform and refusing `build --platforms`, A1 quote check (version, every platform, 12 hex of the commit), G01 commit agreement between `--commit`, record header, A1 and every `ci` entry (`commit-mismatch`, one test each; a later commit needs a new A1), missing record (`not-run`, `no record`; `record-incomplete` under `--final`), G04 dropped-platform outcome (`pass` with list; notes must name it), G11 record-commit ancestry and `docs/releases/`-only diff, notes read from the checkout and docs scans from the export, hosts recorded for G12, waiver needs a quote, control-character removal and 2,000-byte truncation, `pending` rejection, `--final` evaluation of G10 to G14, and the gate evaluation table of the spec (one test per row).
- [ ] T009 [FR-005, SC-002] Add the real-run snapshot test: `plan` on a fixture module in a temporary Git repository with a real `go` (vet and test only) leaves the working tree and `.git` byte-identical (index included) and writes only the contract's effect list; add the recording-runner test that no command outside the allowed vectors runs and that `git status` is never used; the output states that no distributable binary was linked.
- [ ] T010 [P] [FR-008, SC-003, US4] Add the scan test: no file under `.github/workflows`, `.githooks`, `Makefile` or `scripts/` (except release.py and its tests) reachable from a `push`, `pull_request`, `schedule`, tag trigger, hook or `make check` contains `release.py build`, `gh release`, `git tag`, `git push`, `goreleaser`, `softprops/action-gh-release` or an upload of `dist/release`; `/dist/` stays ignored. Include hostile fixture workflows that must be caught.
- [ ] T011 [FR-008, FR-014, US1] Add the Go test in internal/cli that reads `hidden_commands` from scripts/release-gates.json and asserts equality with the real root's `Hidden` commands (today `pins`) and absence of those names from root help and command listings; add the `plan` scan of docs, README, llms.txt, the generated command reference and release notes (`hidden-command-advertised`) with a fixture that mentions `stw pins`.

- [ ] T011a [FR-008, SC-002] Add failing tests then the G05 check (feature ids in the notes compared with docs/feature-status.json; `planned` id rejected, unknown id rejected) and one fixture per gate G01 to G14 that `plan` can evaluate, asserting its exact code from the spec code table and exit status.

## WI3 — Notices for the whole binary

- [ ] T012 [FR-008, SC-006] Add the Cobra (Apache-2.0) and pflag (BSD-3) license texts and notices to third_party/licenses and rows to third_party/README.md, add a Go standard library and runtime row (BSD-3-Go.txt exists), generalize the title and scope text from "Hosting SDK" to the full `stw` closure, keep identical texts stored once, and make T007's check pass for the real repository. Read the licenses from the module cache; do not paraphrase.
- [ ] T013 [P] [FR-006] Add the archive content rule (binary, LICENSE, third_party/, README) to the script's tests with a fixture; no real archive.

## WI4 — Release script: build and verify (fake-runner tested)

- [ ] T014 [FR-004, FR-006, FR-011, US3, US4] Add failing tests then `build`: refuses without A1 (exit 3, no `go`), refuses an existing output directory or tag, re-evaluates G01, G04 to G07, G09 on the export, constructs the recipe environment and `go build` argument vector exactly (fresh `GOCACHE`, absolute `GOMODCACHE`), runs the stamp test first, normalizes archive metadata from exact members, writes `SHA256SUMS` in `sha256sum -c` format and `release-manifest.json`, prints the trust anchor, removes partial output on failure. Fake runner; no real build.
- [ ] T015 [FR-005, FR-013, US5] Add failing tests then `verify`: tampered manifest (listed in `SHA256SUMS`), executed-stamp failures (`stamp-missing`, `stamp-mismatch` under G14), `--rebuild` rows (A1 refusal before `go`, `.rebuild` directory, `release-exists`), refusal without the anchor (`trust-anchor-missing`, `--no-execute` allowed), tampered archive (`checksum-mismatch`), missing `SHA256SUMS` entry, build-information mismatch from `go version -m` fixture output, foreign-platform binary `execution: not-run` and `stamp_verified: none`, hostile archives (path traversal, symlink, hardlink, device, oversize, extra members, bad file name) refused, a failed-checksum binary never executed, smoke argument vectors S1 to S5 and the allowlisted environment without `GH_TOKEN`, `GITHUB_TOKEN`, `SSH_AUTH_SOCK`.
- [ ] T016 [FR-012] Add `verify --rebuild` and the reproducibility comparison logic (binary hashes only; archive bytes not compared) with fake-runner tests for `match`, `mismatch`; requires A1.

## WI5 — Platform lanes (optional per platform; drops the platform if not done)

- [ ] T017 [FR-001, FR-015, US1] Extend scripts/backend-releases.json and scripts/install-backends.py for `linux-arm64` jj artifacts (official release names and SHA-256 values verified against the release assets) with failing tests in scripts/test_backends.py first. Network use is for authoring the hashes only.
- [ ] T018 [FR-001, FR-015] Add a separate `conformance-arm64` job with a `lane: [minimum, current]` matrix on `ubuntu-24.04-arm` to .github/workflows/ci.yml with the sandbox, backend-install, `go test -count=1 -race ./...` and non-hosting smoke steps only (no Pages upload; no Hugo or gh installation, because scripts/hugo-release.json and install-hosting-gh.py have no linux-arm64 pins; hosting smokes are recorded `not-run` on arm64), and a macOS arm64 job only if `sandbox-exec` and Git source builds are shown to work; otherwise darwin arm64 stays `local-recorded` plus `manual`. Pin actions by SHA. Do not claim lanes that did not pass.

## WI6 — Documentation and templates

- [ ] T019 [P] [FR-007, FR-009, FR-011] Write docs/releasing.md (procedure, approvals A1 to A4, recipe, signing and notarization statement, evidence classes, no automation) and docs/releases/TEMPLATE-readiness.md and TEMPLATE-notes.md following contracts/readiness-record.md; link from docs/development.md, docs/README.md and AGENTS.md; keep README's "No release binaries have been published." until A3 completes; keep `python3 scripts/check-docs.py` passing.
- [ ] T020 [P] [FR-016] Add the convention list for smill to docs/releasing.md as a short section and state that adoption is a separate specification in wstein/sheetmill.
- [ ] T021 [P] [FR-005] Add `make release-plan` (runs `release.py plan` only); there is no `release` or `release-build` target.
- [ ] T022 [SC-007] Update docs/progress.md, docs/feature-status.json and docs/roadmap.md with the actual evidence; 026 becomes `implemented` when the tooling and docs are merged and `verified` only when a first release readiness record has every gate `pass` or `waived`; never mark a release done before A3.

## Release steps (not implementation; each needs the named approval)

- [ ] R1 [A1] On explicit authorization naming the code commit, version and platforms: run `go mod download` (network, visible) then `release.py build`; run the second independent build in another directory for G12.
- [ ] R2 Run `release.py verify`; run S1 to S5 on every platform; on the Mac mini follow the quickstart as `manual` evidence; commit the readiness record and notes as the record commit (documentation only).
- [ ] R3 [A2] On explicit authorization: create the tag at the code commit and push it (A2 covers both; the script never does either).
- [ ] R4 [A3] On explicit authorization: publish files and notes with `gh release create --verify-tag`.
- [ ] R5 After publication: repeat `verify` on the downloaded files (quickstart steps 4 to 6).
- [ ] R6 [A4] On explicit authorization per consumer: activate (Anchorage devcontainer, BundleDock lock); outside this repository.

## Dependencies

WI1 before WI4 (the recipe needs the variable). WI2 before WI4. WI3 before the first real `plan` can pass. WI5 is independent and may be skipped, which drops the platform. WI6 finishes last. R-steps follow WI1 to WI4 and WI6 and never happen automatically.
