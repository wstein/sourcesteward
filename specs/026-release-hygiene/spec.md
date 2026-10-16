# Feature Specification: Release hygiene for the first `stw` release

**Feature Branch**: `codex/026-release-hygiene`
**Created**: 2026-10-16
**Status**: Draft — first draft, independent analysis pending; implementation pending
**Input**: What must be true before a first `stw` release build, and how is a release produced and verified reproducibly and safely? This specification covers the stamped version, the build recipe, release platforms, evidence classes, release gates, the readiness record, the approval model and post-release verification. It does not build, tag, publish or activate anything.

## User Scenarios & Testing

### User Story 1 — Know whether a commit may be released (Priority: P1)

A maintainer or agent wants to know whether a given commit satisfies every precondition for a first release, without producing any distributable file.

**Why this priority**: The first release is irreversible in practice (consumers pin it). Preconditions must be checkable before anyone asks for a build.

**Independent Test**: Run the dry run (`scripts/release.py plan`) against fixture repositories that each violate exactly one gate of the [gate table](#release-gates-normative), and against one that violates none.

**Acceptance Scenarios**:

1. **Given** a clean checkout whose toolchain, modules, notices, docs and help satisfy the gates, **When** `release.py plan --version v0.1.0` runs, **Then** it reports every gate with its outcome, writes no file outside the Go build cache, starts no network access and exits 0.
2. **Given** a checkout violating one gate, **When** the dry run runs, **Then** it exits 1 and names that gate's `code` from the [gate table](#release-gates-normative); all other gates are still reported.
3. **Given** a platform without evidence of its required class, **When** the dry run runs with `--record`, **Then** it reports `evidence-missing` for that platform and the platform is not part of the plan.
4. **Given** the dry run, **When** it completes, **Then** its output states that it did not link a distributable binary, so it proves neither the effect of the linker flags nor reproducibility.

### User Story 2 — A release binary states its version, and an unstamped one cannot pass (Priority: P1)

A user or agent runs `stw version --json` on a downloaded binary and gets the release version. A release candidate built without the stamp is detected before publication.

**Independent Test**: `go test` under the release linker flag passes; the same test without the flag and with `STW_RELEASE_VERSION` set fails; the post-build verifier fails a binary whose `version` is `dev`.

**Acceptance Scenarios**:

1. **Given** a build stamped with `v0.1.0`, **When** `stw version --json` runs, **Then** it prints `{"schema_version":1,"name":"SourceSteward","version":"v0.1.0"}` and `stw version` prints `SourceSteward v0.1.0`.
2. **Given** an unstamped build, **When** `stw version` runs, **Then** it prints `SourceSteward dev` exactly as today.
3. **Given** `STW_RELEASE_VERSION=v0.1.0` and no linker stamp, **When** the stamp test runs, **Then** it fails with `stamp-missing`.
4. **Given** a linker flag naming a variable that does not exist, **When** the stamp test runs, **Then** it fails, because the linker ignores such a flag silently (measured, see research).
5. **Given** a version that does not match the [version format](#version-format-normative), **When** the plan runs, **Then** it fails with `version-invalid` before anything else.

### User Story 3 — Build recipe is pinned, offline and reproducible in principle (Priority: P1)

A maintainer who has been authorized to build runs one recipe that pins the toolchain, disables cgo, forbids dependency fetching and records everything needed to repeat the build.

**Independent Test**: Unit tests of `release.py build` with a fake command runner assert the exact environment, argument vectors, output paths and refusals, with no real build. The real double build is [Release gate G12](#release-gates-normative) and runs only after authorization.

**Acceptance Scenarios**:

1. **Given** a polluted environment (`GOFLAGS`, `GOTOOLCHAIN=auto`, `GOPROXY` set, `CGO_ENABLED=1`, `GOWORK`), **When** the recipe runs, **Then** it replaces the environment with the [recipe table](#build-recipe-normative) values and never inherits them.
2. **Given** no recorded approval A1 naming this commit, version and platforms, **When** `release.py build` runs, **Then** it exits 3 with `approval-missing` before running `go`.
3. **Given** an incomplete module cache, **When** the recipe runs, **Then** it fails with `modules-unavailable` and never fetches.
4. **Given** the build completed, **When** the manifest is read, **Then** it lists commit, version, Go version, flags, environment, and the SHA-256 of every binary and archive.

### User Story 4 — Nothing consequential happens without explicit approval (Priority: P1)

Build, tag, publication and consumer activation each need the user's explicit authorization. Pushes, merges, schedules, bots and local commits never grant it.

**Independent Test**: A scan test over `.github/`, `Makefile` and `scripts/` proves that no workflow trigger, schedule, Make target used by `make check` or hook produces release archives, creates tags or releases. Script tests prove `build` refuses without A1.

**Acceptance Scenarios**:

1. **Given** the repository, **When** the sentinel test runs, **Then** it fails if a workflow with a `push`, `pull_request`, `schedule` or tag trigger invokes `release.py build`, `gh release`, `git tag`, `git push --tags`, GoReleaser or an artifact upload of binaries.
2. **Given** an authorization for a build, **When** a later commit is made, **Then** the authorization does not cover the later commit.
3. **Given** a subagent or script, **When** it needs an approval, **Then** it stops and asks; no agent message counts as approval.

### User Story 5 — Verify a released binary on each platform (Priority: P2)

A maintainer checks checksums and runs smoke tests of the downloaded or built binary, on the Mac mini for darwin arm64.

**Independent Test**: `release.py verify DIR` against a fixture directory with good, tampered, unstamped and wrong-platform artifacts; the manual Mac mini procedure in the quickstart.

**Acceptance Scenarios**:

1. **Given** an artifact directory, **When** `verify` runs, **Then** it recomputes `SHA256SUMS`, compares manifest hashes, checks the build information of every binary and, for binaries executable on the host, runs the smoke tests of the [smoke table](#smoke-tests-normative).
2. **Given** a binary for another platform, **When** `verify` runs, **Then** it checks checksum and build information only and reports `execution: not-run` for it; such a platform stays unverified until the matching host (or the Mac mini for darwin arm64) records manual evidence.
3. **Given** a tampered archive, **When** `verify` runs, **Then** it fails with `checksum-mismatch`.

### User Story 6 — Share conventions with `smill` without coupling (Priority: P3)

The `smill` release in the separate repository `wstein/sheetmill` can follow the same conventions later.

**Independent Test**: The [convention table](#shared-conventions-with-smill-normative) is complete and contains no sheetmill implementation detail; a follow-up note exists.

**Acceptance Scenarios**:

1. **Given** the convention table, **When** a maintainer writes the sheetmill spec, **Then** every row states a choice that can be copied or consciously changed.

## Edge Cases

- A dirty tree, an untracked file in a tracked path, or a HEAD that differs from the approved commit: `tree-dirty` or `commit-mismatch`; the build never runs.
- The Go toolchain on the host differs from the `go` line of `go.mod` (`go.mod` has no `toolchain` line, measured): `toolchain-mismatch`. Automatic toolchain download is disabled (`GOTOOLCHAIN=local`).
- The module cache lacks a dependency: `modules-unavailable`. Populating the cache (`go mod download`) is a separate, visible network step before the build, never part of it.
- The tag already exists locally or the output directory is non-empty: `release-exists`; nothing is overwritten.
- `GOFLAGS` in the caller's environment (the author's shell sets `-trimpath`, measured): the recipe ignores caller `GO*` variables and sets its own.
- A darwin binary built on Linux cannot be executed there: it is `not-run` until run on darwin arm64.
- A hidden command is documented or advertised: `hidden-command-advertised`. Today the hidden command is `pins` (Spec 021, hidden in `internal/cli/pins.go` until its final work item); it stays hidden.
- The notices inventory lacks a module in the binary's dependency closure: `notices-incomplete` (today `github.com/spf13/cobra` and `github.com/spf13/pflag` are missing, measured).
- A build-information mismatch (`CGO_ENABLED`, `-trimpath`, `GOOS`, `GOARCH`, `vcs`, ldflags): `buildinfo-mismatch`.
- The same commit built twice yields different binary hashes: `reproducibility-mismatch`; archives are not compared byte for byte (see [FR-012](#functional-requirements)).
- The release is prepared from a jj-managed checkout: the recipe requires a Git checkout of the exact commit (`git rev-parse HEAD`); in a colocated checkout the Git view is used for reading only, and no write goes through Git.
- The darwin/arm64 binary is only ad-hoc code signed by the linker (unverified here) and not notarized; downloads through a browser may be quarantined by Gatekeeper. This is documented in the release notes, not hidden.

## Requirements

### Functional Requirements

- **FR-001**: Release platforms are exactly the [platform table](#release-platforms-normative) rows. A platform joins a release only when its required evidence exists; otherwise it is dropped from that release and the release notes say so. No other target is built.
- **FR-002**: The version is stamped through one linker variable, `github.com/wstein/sourcesteward/internal/cli.Version`, defaulting to `dev`. Both `cmd/stw` and `cmd/gh-jj` pass it to the command runner; the existing `version` parameter and `schemas/v1/version.schema.json` are unchanged. The stamp format is the [version format](#version-format-normative).
- **FR-003**: A Go test, skipped unless `STW_RELEASE_VERSION` is set, fails when `cli.Version` differs from that value or does not match the version format. The recipe runs it under the exact linker flag before building.
- **FR-004**: The build uses only the [recipe table](#build-recipe-normative): pinned toolchain equal to the `go` line of `go.mod`, `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, module mode `readonly`, `GOPROXY=off`, `GOWORK=off`, `GOENV=off`, an empty `GOFLAGS`, no stripping, and a clean Git checkout of the approved commit.
- **FR-005**: `scripts/release.py` provides `plan`, `build` and `verify` as in [contracts/release.md](contracts/release.md). `plan` is the dry run: it links no distributable binary, writes nothing but the Go build cache and uses no network. `build` and `verify --rebuild` are builds and require approval A1.
- **FR-006**: Output files are `stw_<version>_<os>_<arch>.tar.gz` archives, `SHA256SUMS` (the format `sha256sum -c` reads) and `release-manifest.json`, only under `dist/release/<version>/`. Archives contain the binary, `LICENSE`, `third_party/` and a short `README`. Archive metadata is normalized (sorted entries, zero owner, fixed modification time from the commit time, fixed modes). An SBOM is optional; if produced it is an additional file and never a gate.
- **FR-007**: Signing status is stated, not implied: `SHA256SUMS` is unsigned; binaries are not signed or notarized by SourceSteward; no attestation is produced in this feature. The release notes and `docs/releasing.md` say so, with the consequence (integrity against corruption, not authenticity against a compromised publication account).
- **FR-008**: Every release satisfies each row of the [gate table](#release-gates-normative). A gate's outcome is `pass`, `fail`, `not-run` or `waived` (waiver only where the table allows it, with the maintainer's recorded reason). A release with a `fail` or an unwaivable `not-run` gate is not ready.
- **FR-009**: A release readiness record per release at `docs/releases/<version>-readiness.md`, in the form of [contracts/readiness-record.md](contracts/readiness-record.md), lists exact versions, platforms, evidence class and outcome per gate, approvals, and what was not run. Release notes at `docs/releases/<version>.md` follow the same contract.
- **FR-010**: Evidence is classified by the [evidence table](#evidence-classes-normative). Local or manual evidence is never described as CI evidence.
- **FR-011**: Approvals follow the [approval table](#approvals-normative). `release.py build` refuses without an A1 entry in the readiness record naming commit, version and platforms; this stops accidents and is not an authorization mechanism. No workflow, schedule, push, merge, bot or local commit starts a build, a tag, a publication or an activation.
- **FR-012**: Reproducibility is claimed for the binaries only, and only for the recorded toolchain and platform set. The check compares binary SHA-256 values between two independent builds in different directories (gate G12). Archive bytes are not claimed identical because compressor output may differ between hosts (unverified).
- **FR-013**: Post-release verification follows the [smoke table](#smoke-tests-normative) on every released platform; darwin arm64 is verified on the Mac mini as manual evidence recorded in the readiness record.
- **FR-014**: Nothing in this feature adds, unhides or changes a user command, JSON schema or exit status of `stw`. `stw` has no `--version` flag (cobra is configured without one, `stw version` is the interface); this feature does not add one.
- **FR-015**: Capability statement (constitution V): the release platforms are supported only with passing Git and jj conformance lanes for that platform (Git 2.46.0 minimum and 2.56.0 current, jj 0.45.1 minimum and 0.46.0 current, as pinned in `scripts/backend-releases.json`). A failed or missing Git or jj lane blocks that platform equally. No backend equivalence is implied. jj on a platform requires its sandbox (bubblewrap on Linux, `sandbox-exec` on macOS).
- **FR-016**: Conventions in the [convention table](#shared-conventions-with-smill-normative) are stated for reuse. A follow-up specification in `wstein/sheetmill` is required to adopt them; this feature changes nothing there.
- **FR-017**: `release.py` output is bounded and non-interactive: no prompt, no pager; at most 200 report lines in human mode; `--json` renders the same result.

### Release platforms (normative)

| Platform | In first release | Required evidence (see [evidence classes](#evidence-classes-normative)) | Note |
| --- | --- | --- | --- |
| `linux/amd64` | yes | `ci` for G02 and G03 on the release commit | CI today: `ubuntu-24.04` amd64 only |
| `linux/arm64` | yes, when evidenced | `ci` or `local-recorded` for `go test`, smoke scripts and both Git/jj lanes | `scripts/backend-releases.json` has no `linux-arm64` jj artifact (measured); WI5 adds it or the platform is dropped from the first release |
| `darwin/arm64` | yes, when evidenced | `ci` or `local-recorded` for the same; `manual` smoke on the Mac mini | jj artifacts for `darwin-arm64` exist; macOS CI lane is optional follow-up |
| `darwin/amd64` | no | none | end of life; not built |
| `windows/*` | no | none | not supported by the product (no sandbox) |

### Version format (normative)

| Item | Rule |
| --- | --- |
| Pattern | `^v(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?$` |
| Tag | exactly the version string, e.g. `v0.1.0` |
| Stamp | exactly the version string including the `v`; no build metadata |
| Unstamped | `dev`; never allowed in a release (`stamp-missing`) |
| Text output | `SourceSteward <version>` |
| JSON output | `{"schema_version":1,"name":"SourceSteward","version":"<version>"}`; the schema keeps `minLength: 1` and is not changed |
| First version | `v0.1.0` (recommendation, see open questions) |

### Build recipe (normative)

| Setting | Value | Reason |
| --- | --- | --- |
| Toolchain | `go` equals the `go` line of `go.mod` (`go1.27.2`); `GOTOOLCHAIN=local` | no silent download, no drift |
| Cgo | `CGO_ENABLED=0` | `runtime/cgo` is in the closure with cgo on and absent with it off (measured); static, host-independent |
| Flags | `-trimpath -buildvcs=false` | no build path or VCS state in the binary |
| Module mode | `GOFLAGS=` empty, `-mod=readonly`, `GOPROXY=off`, `GOWORK=off`, `GOENV=off`; no `GO*` variable is inherited | no fetch; `go.mod`/`go.sum` unchanged; no per-user `go env -w` effect |
| Linker | `-ldflags "-X github.com/wstein/sourcesteward/internal/cli.Version=<version>"`; no `-s -w` | stamp only; debuggable |
| Microarchitecture | `GOAMD64=v1`, `GOARM64=v8.0` | the defaults, pinned explicitly |
| Source | clean Git checkout, `git rev-parse HEAD` equals the approved commit | traceability |
| Time | `SOURCE_DATE_EPOCH` = commit time, used for archive metadata only | deterministic archives |
| Output | `dist/release/<version>/` (git-ignored via `/dist/`) | no overwrite (`release-exists`) |
| Binary targets | `./cmd/stw` only; `cmd/gh-jj` and `cmd/stw-docs` are not released | scope |

### Evidence classes (normative)

| Class | Meaning | What is recorded |
| --- | --- | --- |
| `ci` | a hosted workflow run on the exact release commit | run URL, workflow, job, commit, conclusion |
| `local-recorded` | a command run by an authorized person or agent on a named host | command, output excerpt, tool versions, `uname -sm`, OS version, commit, date, outcome |
| `manual` | a human followed the quickstart steps on a named host | steps done, observations, host, date, who |
| `not-run` | not attempted | reason; listed in the notes |

### Release gates (normative)

| Gate | `code` on failure | Requirement | Waivable |
| --- | --- | --- | --- |
| G01 | `tree-dirty`, `commit-mismatch` | clean checkout; HEAD equals the commit named in A1 | no |
| G02 | `check-failed` | `make check` and `go test -race ./...` green (`ci` on linux/amd64) | no |
| G03 | `conformance-failed` | conformance `minimum` and `current` green (`ci`, linux/amd64) | no |
| G04 | `evidence-missing` | per extra platform in the platform table: tests, smokes and both lanes (`ci` or `local-recorded`) | drops the platform |
| G05 | `docs-inconsistent` | `python3 scripts/check-docs.py` passes; release notes mention only features whose `docs/feature-status.json` status is `implemented` or `verified`; no `planned` feature is described as available | no |
| G06 | `hidden-command-advertised` | no hidden command appears in help, the generated command reference, docs, `llms.txt`, README or release notes | no |
| G07 | `notices-incomplete` | every module in the dependency closure of `./cmd/stw` has retained license text in `third_party/` and the archive contains `LICENSE` and `third_party/` | no |
| G08 | `stamp-missing`, `stamp-mismatch` | stamp test passes pre-build; every binary reports the version (execution or build information) | no |
| G09 | `toolchain-mismatch`, `go-env-unsafe`, `modules-unverified`, `compile-failed`, `version-invalid` | `release.py plan` exits 0 | no |
| G10 | `release-notes-missing` | notes exist with the [contracted sections](contracts/readiness-record.md) | no |
| G11 | `record-incomplete` | readiness record has no `pending` entry and lists what was not run | no |
| G12 | `reproducibility-mismatch` | two independent builds give equal binary SHA-256 per released platform | yes, by recorded maintainer waiver; the notes then say reproducibility was not verified |
| G13 | `approval-missing` | A1 recorded before the build, A2 before the tag, A3 before publication | no |
| G14 | `smoke-failed` | the [smoke table](#smoke-tests-normative) passes on every released platform | no |

### Approvals (normative)

| Step | Name | Needs explicit user authorization naming | Never authorized by |
| --- | --- | --- | --- |
| A1 | Build (including any rebuild for G12) | commit SHA, version, platforms | push, merge, schedule, bot, local commit, this specification, a passing dry run |
| A2 | Tag | tag name and commit SHA | A1, a successful build |
| A3 | Publication (upload to a release, any public location) | tag, files, destination | A1, A2 |
| A4 | Consumer activation (Anchorage devcontainer, BundleDock catalog/lock, any pin) | consumer, version | A1 to A3 |

An authorization covers the named values only. A changed commit needs a new one. The approvals are recorded verbatim (date, quote, who) in the readiness record before the step.

### Smoke tests (normative)

| Id | Command | Pass condition |
| --- | --- | --- |
| S1 | `stw version` | prints `SourceSteward <version>` and exits 0 |
| S2 | `stw version --json` | one object equal to the version result with the stamped version |
| S3 | `stw --help` | exits 0; lists no hidden command |
| S4 | `stw inspect <temp repository> --json` | exits 0 in a fresh temporary Git repository; result is a valid inspect result |
| S5 | `stw inspect <temp jj repository> --json` | only where jj and its sandbox exist: valid result or a documented `jj-sandbox-unavailable` refusal; recorded either way |

### Shared conventions with `smill` (normative)

| Convention | Value |
| --- | --- |
| Version variable | `Version` in `internal/cli`, default `dev`, set with `-X <module>/internal/cli.Version=<version>` |
| Version format | the [version format](#version-format-normative), `v` included |
| Version output | `<name> <version>` text, with the tool's existing JSON contract unchanged |
| Platforms | linux amd64/arm64, darwin arm64 |
| Archive name | `<tool>_<version>_<os>_<arch>.tar.gz` |
| Checksums | `SHA256SUMS`, `sha256sum -c` format, unsigned unless stated |
| Build flags | `CGO_ENABLED=0 -trimpath -buildvcs=false`, local toolchain equal to `go.mod`, no fetch |
| Approval rule | build, tag, publication and consumer activation each need explicit authorization; automation never grants it |
| Gate vocabulary | the kebab-case `code` values of the gate table where they apply |

Follow-up (not part of this feature): a specification in `wstein/sheetmill`. Observation only: `smill` already has `cli.Version` and a stamp test that runs `go build`; this feature's stamp test uses `go test` to avoid producing a binary.

## Key Entities

- **Release plan**: result of the dry run, one entry per gate and platform.
- **Release manifest**: machine-readable record of one build (`release-manifest.json`).
- **Readiness record**: human-readable record of gates, evidence, approvals and omissions.
- **Evidence entry**: one row of evidence with a class.
- **Approval entry**: one recorded authorization.

## Success Criteria

- **SC-001**: With `STW_RELEASE_VERSION` set, the stamp test fails on an unstamped test binary and on a misspelled variable, and passes under the exact release linker flag.
- **SC-002**: The dry run reports every gate; fixtures violating one gate each yield that gate's code and exit 1; the clean fixture exits 0; an effect sentinel shows no file written outside the Go build cache and no network use.
- **SC-003**: No workflow or Make target reachable from `push`, `pull_request`, `schedule` or `make check` can build release archives, tag or publish (scan test).
- **SC-004**: `release.py build` with a fake runner refuses without A1 (exit 3), uses exactly the recipe environment and arguments, and refuses an existing output directory.
- **SC-005**: `release.py verify` detects a tampered archive, an unstamped binary, a wrong-platform build information and a missing `SHA256SUMS` entry.
- **SC-006**: The notices inventory covers the whole closure of `./cmd/stw` (today fails for Cobra and pflag; fixed by WI3).
- **SC-007**: A completed readiness record exists for the first release with every gate `pass`, `waived` or listed as `not-run`, and the Mac mini manual evidence for darwin/arm64 if that platform ships.
- **SC-008**: Reproducibility is recorded as `match`, `mismatch` or `waived` for each platform; it is never stated without that record.

## Scope and Dependencies

In scope: stamp, recipe, platforms, evidence, gates, record, approvals, dry run, verification, convention list.

Out of scope: running any real build, tag, publication or activation; signing, notarization, attestations (follow-ups); package managers; Windows; darwin/amd64; the `gh-jj` extension and Docker images; BundleDock and Anchorage integration beyond the A4 rule; automatic changelog generation; a `stw --version` flag; any change of `stw` commands or schemas; the sheetmill specification.

Dependencies: Spec 021 (the `pins` command stays hidden; a release must not wait for it), Spec 024 and 025 (not required, planned status excluded from notes by G05), the existing conformance lanes. Constitution Scope: release automation needs a later specification; this feature specifies a manual, approval-gated recipe and automates nothing consequential.
