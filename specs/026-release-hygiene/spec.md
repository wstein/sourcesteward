# Feature Specification: Release hygiene for the first `stw` release

**Feature Branch**: `codex/026-release-hygiene`
**Created**: 2026-10-16
**Status**: Draft — revised after independent analysis round 1; implementation pending
**Input**: What must be true before a first `stw` release build, and how is a release produced and verified reproducibly and safely? This specification covers the stamped version, the build recipe, release platforms, evidence classes, release gates, the readiness record, the approval model and post-release verification. It does not build, tag, publish or activate anything.

## User Scenarios & Testing

### User Story 1 — Know whether a commit may be released (Priority: P1)

A maintainer or agent wants to know whether a given commit satisfies every precondition for a first release, without producing any distributable file.

**Why this priority**: The first release is irreversible in practice (consumers pin it). Preconditions must be checkable before anyone asks for a build.

**Independent Test**: Run the dry run (`scripts/release.py plan`) against fixture repositories that each violate exactly one gate of the [gate table](#release-gates-normative), and against one that violates none.

**Acceptance Scenarios**:

1. **Given** a commit whose toolchain, modules, notices, docs and help satisfy the gates that `plan` can evaluate, **When** `release.py plan --version v0.1.0` runs, **Then** it reports every gate (gates that need a build, approval or post-release evidence as `not-run` per the [gate evaluation table](#gate-evaluation-by-command-normative)), writes only the [listed effects](contracts/release.md#effects), uses no network and exits 0.
2. **Given** a commit violating one gate, **When** the dry run runs, **Then** it exits 1 and names that gate's code from the [code table](#codes-normative); all other gates are still reported.
3. **Given** a platform without evidence of its required class, **When** the dry run runs with `--record`, **Then** it reports `evidence-missing` for that platform, the platform is not part of the plan and `build --platforms` naming it is refused with exit 1.
4. **Given** the dry run, **When** it completes, **Then** its output states that it did not link a distributable binary, so it proves neither the effect of the linker flags nor reproducibility.

### User Story 2 — A release binary states its version, and an unstamped one cannot pass (Priority: P1)

A user or agent runs `stw version --json` on a downloaded binary and gets the release version. A release candidate built without the stamp is detected before publication.

**Independent Test**: `go test` under the release linker flag passes; the same test without the flag and with `STW_RELEASE_VERSION` set fails; the post-build verifier fails a binary whose executed `version --json` is not the release version.

**Acceptance Scenarios**:

1. **Given** a build stamped with `v0.1.0`, **When** `stw version --json` runs, **Then** it prints `{"schema_version":1,"name":"SourceSteward","version":"v0.1.0"}` and `stw version` prints `SourceSteward v0.1.0`.
2. **Given** an unstamped build, **When** `stw version` runs, **Then** it prints `SourceSteward dev` exactly as today.
3. **Given** `STW_RELEASE_VERSION=v0.1.0` and no linker stamp, **When** the stamp test runs, **Then** it fails with `stamp-missing`.
4. **Given** a linker flag naming a variable that does not exist, **When** the stamp test runs, **Then** it fails, because the linker ignores such a flag silently (measured, see research).
5. **Given** a version that does not match the [version format](#version-format-normative), **When** `plan` or `build` runs, **Then** it exits 2 with `version-invalid` on standard error and empty standard output before any command runs.

### User Story 3 — Build recipe is pinned, offline and reproducible in principle (Priority: P1)

A maintainer who has been authorized to build runs one recipe that pins the toolchain, disables cgo, forbids dependency fetching and records everything needed to repeat the build.

**Independent Test**: Unit tests of `release.py build` with a fake command runner assert the exact environment, argument vectors, output paths and refusals, with no real build. The real double build is [gate G12](#release-gates-normative) and runs only after authorization.

**Acceptance Scenarios**:

1. **Given** a polluted environment (`GOFLAGS`, `GOTOOLCHAIN=auto`, `GOPROXY` set, `CGO_ENABLED=1`, `GOWORK`), **When** the recipe runs, **Then** it replaces the environment with the [recipe table](#build-recipe-normative) values and never inherits them.
2. **Given** no recorded approval A1 whose quote names this commit, version and platforms, **When** `release.py build` runs, **Then** it exits 3 with `approval-missing` before running `go`.
3. **Given** an incomplete module cache (fixture), **When** `plan` or the recipe runs, **Then** it fails with `modules-unavailable` and never fetches.
4. **Given** the build completed, **When** the manifest is read, **Then** it lists commit, version, Go version, flags, environment, and the SHA-256 of every binary and archive.

### User Story 4 — Nothing consequential happens without explicit approval (Priority: P1)

Build, tag, publication and consumer activation each need the user's explicit authorization. Pushes, merges, schedules, bots and local commits never grant it.

**Independent Test**: A scan test over `.github/`, `Makefile` and `scripts/` proves that no workflow trigger, schedule, Make target used by `make check` or hook produces release archives, creates tags or releases. Script tests prove `build` refuses without A1.

**Acceptance Scenarios**:

1. **Given** the repository, **When** the sentinel test runs, **Then** it fails if any file under `.github/workflows`, `.githooks`, `Makefile` or `scripts/` (other than `release.py` itself and its tests) that a `push`, `pull_request`, `schedule`, tag trigger, hook or `make check` can reach invokes `release.py build`, `gh release`, `git tag`, `git push`, GoReleaser, or an upload of `dist/release`.
2. **Given** an authorization for a build, **When** a later commit is made, **Then** the authorization does not cover the later commit.
3. **Given** a subagent or script, **When** it needs an approval, **Then** it stops and asks; no agent message counts as approval.

### User Story 5 — Verify a released binary on each platform (Priority: P2)

A maintainer checks checksums and runs smoke tests of the downloaded or built binary, on the Mac mini for darwin arm64.

**Independent Test**: `release.py verify DIR` against a fixture directory with good, tampered, unstamped and wrong-platform artifacts; the manual Mac mini procedure in the quickstart.

**Acceptance Scenarios**:

1. **Given** an artifact directory and a trust anchor (the SHA-256 of `SHA256SUMS` recorded in the readiness record or printed by the authorized build), **When** `verify` runs, **Then** it checks the anchor, recomputes `SHA256SUMS`, compares manifest hashes, checks the build information of every binary and, for binaries executable on the host, extracts the exact expected member and runs the smoke tests of the [smoke table](#smoke-tests-normative). Without the anchor it refuses to execute anything (`trust-anchor-missing`) and offers only `--no-execute` checks.
2. **Given** a binary for another platform, **When** `verify` runs, **Then** it checks checksum and build information (`GOOS`, `GOARCH`, `CGO_ENABLED`, `-trimpath`) only and reports `execution: not-run`; its stamp stays unverified until the matching host (the Mac mini for darwin arm64) runs it and records manual evidence, which gate G14 requires before publication.
3. **Given** a tampered archive, **When** `verify` runs, **Then** it fails with `checksum-mismatch`.

### User Story 6 — Share conventions with `smill` without coupling (Priority: P3)

The `smill` release in the separate repository `wstein/sheetmill` can follow the same conventions later.

**Independent Test**: The [convention table](#shared-conventions-with-smill-normative) is complete and contains no sheetmill implementation detail; a follow-up note exists.

**Acceptance Scenarios**:

1. **Given** the convention table, **When** a maintainer writes the sheetmill spec, **Then** every row states a choice that can be copied or consciously changed.

## Edge Cases

- The record and notes cannot live in the commit they describe (a commit cannot contain its own hash). The build therefore uses a **code commit** named in A1 and exported from its tree objects into a fresh directory; uncommitted or ignored files in the checkout never reach the build. The readiness record and notes are committed afterwards in a **record commit** (documentation only) and are read from the checkout. The tag (A2) points at the code commit. CI evidence for G02 and G03 refers to the code commit; a later documentation-only record commit has its own, unrelated CI runs.
- The Go toolchain on the host differs from the `go` line of `go.mod` (`go.mod` has no `toolchain` line, measured): `toolchain-mismatch`. Automatic toolchain download is disabled (`GOTOOLCHAIN=local`).
- The module cache lacks a dependency: `modules-unavailable`. Populating the cache (`go mod download`) is a separate, visible network step before the build, never part of it.
- The tag already exists locally or the output directory is non-empty: `release-exists`; nothing is overwritten.
- `GOFLAGS` in the caller's environment (the author's shell sets `-trimpath`, measured): the recipe ignores caller `GO*` variables and sets its own.
- A darwin binary built on Linux cannot be executed there: it is `not-run` until run on darwin arm64.
- A hidden command is documented or advertised: `hidden-command-advertised`. Today the hidden command is `pins` (Spec 021, hidden in `internal/cli/pins.go` until its final work item); it stays hidden.
- The notices inventory lacks a module in the binary's dependency closure: `notices-incomplete` (today `github.com/spf13/cobra` and `github.com/spf13/pflag` are missing, measured).
- A build-information mismatch (`CGO_ENABLED`, `-trimpath`, `GOOS`, `GOARCH`, no `vcs` keys): `buildinfo-mismatch`. Under `-trimpath` the build information contains no `-ldflags` entry (measured by the reviewer), so the stamp is verified only by execution.
- The same commit built twice yields different binary hashes: `reproducibility-mismatch`; archives are not compared byte for byte (see [FR-012](#functional-requirements)).
- The release is prepared in a jj-managed checkout: in a colocated checkout the script reads commits through Git plumbing only (`rev-parse`, `ls-tree`, `cat-file`, `merge-base`, `diff-tree`, `tag --list`), never `git status`, never a write. A jj workspace without a Git directory is refused with `unsupported-checkout` (exit 3). The script never creates or pushes a tag; the maintainer creates the tag after A2 with the method that matches the checkout's owner (Git in a Git checkout, a Git-visible tag in a colocated one), and this specification does not choose it for a jj-owned checkout.
- The darwin/arm64 binary is only ad-hoc code signed by the linker (unverified here) and not notarized; downloads through a browser may be quarantined by Gatekeeper. This is documented in the release notes, not hidden.
- `STW_RELEASE_VERSION` set in a developer shell makes the stamp test run and fail in `make check`; `docs/releasing.md` says to unset it, and only the release recipe sets it.
- Text from a record (quotes, reasons) is data: control characters are removed before printing and each field is truncated at 2,000 bytes with a trailing `…`.
- A published schema of a hidden command (`schemas/v1/pins.schema.json`) is already on Pages (`scripts/package-pages.py`); this is accepted and out of scope. `stw help pins` prints the hidden command's help on direct request; hidden means not listed.

## Requirements

### Functional Requirements

- **FR-001**: Release platforms are exactly the [platform table](#release-platforms-normative) rows. A platform joins a release only when its required evidence exists; otherwise it is dropped from that release and the release notes say so. No other target is built.
- **FR-002**: The version is stamped through one linker variable, `github.com/wstein/sourcesteward/internal/cli.Version`, defaulting to `dev`. Both `cmd/stw` and `cmd/gh-jj` pass it to the command runner; the existing `version` parameter and `schemas/v1/version.schema.json` are unchanged. The stamp format is the [version format](#version-format-normative).
- **FR-003**: A Go test, skipped unless `STW_RELEASE_VERSION` is set, fails when `cli.Version` differs from that value or does not match the version format. The recipe runs it under the exact linker flag before building.
- **FR-004**: The build uses only the [recipe table](#build-recipe-normative): pinned toolchain equal to the `go` line of `go.mod`, `CGO_ENABLED=0`, `-trimpath`, `-buildvcs=false`, module mode `readonly`, `GOPROXY=off`, `GOWORK=off`, `GOENV=off`, an empty `GOFLAGS`, no stripping, a fresh empty `GOCACHE`, and an export of the code commit named in A1 built from tree objects (`git ls-tree` and `git cat-file`, never `git archive`, which applies repository-local filters and attributes, measured by the reviewer).
- **FR-005**: `scripts/release.py` provides `plan`, `build` and `verify` as in [contracts/release.md](contracts/release.md). `plan` is the dry run: it links no distributable binary, uses no network and has only the effects listed in the contract (Go build cache, temporary directories, and the stamp test's temporary test binary). `build` and `verify --rebuild` are builds and require approval A1. The script never tags, pushes, publishes or activates.
- **FR-006**: Output files are `stw_<version>_<os>_<arch>.tar.gz` archives, `SHA256SUMS` (the format `sha256sum -c` reads) and `release-manifest.json`, only under `dist/release/<version>/`. Archives contain the binary, `LICENSE`, `third_party/` and a short `README`. Archive metadata is normalized (sorted entries, zero owner, fixed modification time from the commit time, fixed modes). An SBOM is optional; if produced it is an additional file and never a gate.
- **FR-007**: Signing status is stated, not implied: `SHA256SUMS` is unsigned; binaries are not signed or notarized by SourceSteward; no attestation is produced in this feature. The release notes and `docs/releasing.md` say so, with the consequence (integrity against corruption, not authenticity against a compromised publication account).
- **FR-008**: Every release satisfies each row of the [gate table](#release-gates-normative). A gate's outcome is `pass`, `fail`, `not-run` or `waived` (waiver only where the table allows it, with the user's verbatim authorization recorded like an approval). Which command evaluates which gate is fixed by the [gate evaluation table](#gate-evaluation-by-command-normative). A release with a `fail` or a `not-run` gate in the final record is not ready.
- **FR-009**: A release readiness record per release at `docs/releases/<version>-readiness.md`, in the form of [contracts/readiness-record.md](contracts/readiness-record.md), lists exact versions, platforms, evidence class and outcome per gate, approvals, and what was not run. Release notes at `docs/releases/<version>.md` follow the same contract.
- **FR-010**: Evidence is classified by the [evidence table](#evidence-classes-normative). Local or manual evidence is never described as CI evidence.
- **FR-011**: Approvals follow the [approval table](#approvals-normative). `release.py build` refuses without an A1 entry in the readiness record whose verbatim quote contains the version, every requested platform and at least 12 hexadecimal characters of the code commit; this stops accidents and is not an authorization mechanism, because whoever writes the record can write the entry. The reviewer of the record compares it with the user's actual message. No workflow, schedule, push, merge, bot or local commit starts a build, a tag, a publication or an activation.
- **FR-012**: Reproducibility is claimed for the binaries only, for the recorded toolchain, platform set and hosts. The check compares binary SHA-256 values between two builds, each from its own `git archive` export in a different directory with its own empty `GOCACHE` (gate G12); whether both ran on one host or two is recorded and the claim is limited to that. Archive bytes are normalized but not claimed identical across hosts (compressor output, unverified).
- **FR-013**: Verification follows the [smoke table](#smoke-tests-normative) on every released platform, before publication on the built files and after publication on the downloaded files; darwin arm64 is verified on the Mac mini as manual evidence recorded in the readiness record. Checks that rely on files from the same directory (`SHA256SUMS`, manifest) are anchored by a hash recorded outside it (the record), and nothing is executed without the anchor. Archive extraction accepts only the exact member names, regular files, with a size limit of 128 MiB.
- **FR-014**: Nothing in this feature adds, unhides or changes a user command, JSON schema or exit status of `stw`. `stw` has no `--version` flag (cobra is configured without one, `stw version` is the interface); this feature does not add one.
- **FR-015**: Capability statement (constitution V): the release platforms are supported only with passing Git and jj conformance lanes for that platform (Git 2.46.0 minimum and 2.56.0 current, jj 0.45.1 minimum and 0.46.0 current, as pinned in `scripts/backend-releases.json`). A failed or missing Git or jj lane blocks that platform equally. No backend equivalence is implied. jj on a platform requires its sandbox (bubblewrap on Linux, `sandbox-exec` on macOS).
- **FR-016**: Conventions in the [convention table](#shared-conventions-with-smill-normative) are stated for reuse. A follow-up specification in `wstein/sheetmill` is required to adopt them; this feature changes nothing there.
- **FR-017**: `release.py` output is bounded and non-interactive: no prompt, no pager; at most 200 report lines in human mode; `--json` renders the same result.

### Release platforms (normative)

| Platform | In first release | Required evidence (see [evidence classes](#evidence-classes-normative)) | Note |
| --- | --- | --- | --- |
| `linux/amd64` | yes | `ci` for G02 and G03 on the code commit | CI today: `ubuntu-24.04` amd64 only |
| `linux/arm64` | yes, when evidenced | `ci` or `local-recorded` for `go test`, smoke scripts and both Git/jj lanes | `scripts/backend-releases.json` has no `linux-arm64` jj artifact (measured); WI5 adds it or the platform is dropped from the first release |
| `darwin/arm64` | yes, when evidenced | `ci` or `local-recorded` for the same; `manual` smoke on the Mac mini | jj artifacts for `darwin-arm64` exist; macOS CI lane is optional follow-up |
| `darwin/amd64` | no | none | end of life; not built |
| `windows/*` | no | none | not supported by the product (no sandbox) |

### Version format (normative)

| Item | Rule |
| --- | --- |
| Pattern | `v(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)\.(0\|[1-9][0-9]*)(-rc\.[1-9][0-9]*)?`, matched against the whole string (Go `MatchString` with `\A...\z`, Python `re.fullmatch`; a trailing newline does not match, measured) |
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
| Cgo | `CGO_ENABLED=0` | `runtime/cgo` is in the closure with cgo on and absent with it off (measured); no C toolchain dependency (darwin binaries still link the system library) |
| Flags | `-trimpath -buildvcs=false` | no build path or VCS state in the binary |
| Module mode | `GOFLAGS=` empty, `-mod=readonly`, `GOPROXY=off`, `GOWORK=off`, `GOENV=off`; no `GO*` variable is inherited | no fetch; `go.mod`/`go.sum` unchanged; no per-user `go env -w` effect |
| Linker | `-ldflags "-X github.com/wstein/sourcesteward/internal/cli.Version=<version>"`; no `-s -w` | stamp only; debuggable |
| Microarchitecture | `GOAMD64=v1`, `GOARM64=v8.0` | the defaults, pinned explicitly |
| Source | exact tree of the code commit written to a fresh directory from `git ls-tree -r -z` and `git cat-file --batch`: regular files only, modes 0644/0755, symlinks and gitlinks refused (`export-unsafe`) | traceability; ignored files, filters, `export-ignore` and `export-subst` cannot change the source |
| Caches | `GOMODCACHE` resolved from the caller's `go env` and passed as an absolute path (never inferred from `HOME`); `GOCACHE` a fresh empty temporary directory for `build` | offline modules; independent builds |
| Git | `git --no-optional-locks -c core.fsmonitor=false` with `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, absolute path | no user configuration or hook program runs |
| Time | `SOURCE_DATE_EPOCH` = commit time, used for archive metadata only | normalized archives |
| Output | `dist/release/<version>/` (git-ignored via `/dist/`) | no overwrite (`release-exists`) |
| Binary targets | `./cmd/stw` only; `cmd/gh-jj` and `cmd/stw-docs` are not released | scope |

### Evidence classes (normative)

| Class | Meaning | What is recorded |
| --- | --- | --- |
| `ci` | a hosted workflow run on the exact code commit | run URL, workflow, job, commit, conclusion |
| `local-recorded` | a command run by an authorized person or agent on a named host | command, output excerpt, tool versions, `uname -sm`, OS version, commit, date, outcome |
| `manual` | a human followed the quickstart steps on a named host | steps done, observations, host, date, who |
| `not-run` | not attempted | reason; listed in the notes |

### Release gates (normative)

Gates apply to the **code commit**. Codes are defined once in the [code table](#codes-normative).

| Gate | Requirement | Waivable |
| --- | --- | --- |
| G01 | the code commit resolves and exports safely; whenever a record is read, `--commit` (default: the record header's code commit, not `HEAD`) equals the header commit, the A1 commit and the commit of every `ci` evidence entry | no |
| G02 | `make check` and `go test -race ./...` green on the code commit (`ci`, linux/amd64); the script checks the record's assertion (run URL, conclusion, commit), a reviewer cross-checks it with `gh run view` outside the script | no |
| G03 | conformance `minimum` and `current` green on the code commit (`ci`, linux/amd64); record-asserted and cross-checked like G02 | no |
| G04 | per extra platform: `go test`, the smoke scripts (hosting smokes excluded on arm64) and the `minimum` and `current` Git and jj lanes (`ci` or `local-recorded`). A platform without it is dropped; G04 then `pass`es with the dropped platforms listed, and the notes (G10) must name each dropped platform | no |
| G05 | `python3 scripts/check-docs.py` passes on the export; the release notes' feature list (ids; notes read from the checkout) contains only features whose `docs/feature-status.json` status is `implemented` or `verified` | no |
| G06 | no hidden command appears in root help, per-command listings, the generated command reference, docs, `llms.txt`, README (all scanned in the export) or the release notes (read from the checkout); list in `scripts/release-gates.json` | no |
| G07 | every module in the union of the dependency closures of `./cmd/stw` over the shipped targets, and the Go standard library and runtime, has retained license text in `third_party/`; archives contain `LICENSE` and `third_party/` | no |
| G08 | stamp test passes pre-build under the exact flag; `verify` build information of every binary matches the manifest | no |
| G09 | toolchain, environment, module and compile checks of the dry run pass | no |
| G10 | release notes exist with the [contracted sections](contracts/readiness-record.md) | no |
| G11 | the readiness record has no `pending` entry and lists what was not run; the record commit descends from the code commit and its diff touches only `docs/releases/` | no |
| G12 | two independent builds give equal binary SHA-256 per released platform | yes, by a recorded user authorization; the notes then say reproducibility was not verified |
| G13 | A1 recorded before the build, A2 before the tag and its push, A3 before publication | no |
| G14 | the anchor and checksums match (the manifest is listed in `SHA256SUMS`) and the [smoke table](#smoke-tests-normative) passes by execution on every released platform; the executed `version --json` verifies the stamp | no |

### Gate evaluation by command (normative)

`E` evaluated; `-` not evaluated, reported `not-run` with a reason. Without a record, G02, G03, G04 and G13 are `not-run` with the reason `no record` in `plan`; under `--final` that is `record-incomplete`. A `not-run` gate never makes `plan` exit 1, but `plan --final` and the final record require every gate `pass` or `waived`.

| Gate | `plan` | `plan --final` | `build` (before building) | `verify` |
| --- | --- | --- | --- | --- |
| G01 | E | E | E | `--rebuild` only |
| G02, G03 | E (record) | E | E | - |
| G04 | E (record) | E | E, per requested platform | - |
| G05, G06, G07 | E | E | E | - |
| G08 | stamp test only | E | stamp test | E (build information) |
| G09 | E | E | E | `--rebuild` only |
| G10, G11 | - | E | - | - |
| G12 | - | E | - | `--rebuild` only |
| G13 | A1 if present | E | A1 (first, before any `go` command) | `--rebuild`: A1 |
| G14 | - | E | - | E |

### Codes (normative)

All codes are kebab-case. "Exit" is the process status; `1` is a gate failure reported in the result, `2` a usage error with empty standard output and no JSON, `3` a refusal reported in the result with `status: refused`.

| Code | Gate or origin | Exit |
| --- | --- | --- |
| `version-invalid` | argument | 2 |
| `platform-unsupported` | argument | 2 |
| `commit-unresolved` | G01 | 1 |
| `commit-mismatch` | G01 (code commit differs from A1) | 1 |
| `unsupported-checkout` | G01 (jj workspace without a Git view) | 3 |
| `check-failed` | G02 | 1 |
| `conformance-failed` | G03 | 1 |
| `evidence-missing` | G04 | 1 for `build --platforms`; for `plan` a dropped platform |
| `docs-inconsistent` | G05 | 1 |
| `hidden-command-advertised` | G06 | 1 |
| `notices-incomplete` | G07 | 1 |
| `stamp-missing`, `stamp-mismatch` | G08 (stamp test) and G14 (executed `version --json`) | 1 |
| `buildinfo-mismatch` | G08 (`verify`) | 1 |
| `export-unsafe` | G01 (symlink, gitlink or non-regular entry in the tree) | 1 |
| `toolchain-mismatch`, `go-env-unsafe`, `modules-unverified` (`go mod verify` fails), `modules-unavailable` (module missing in the offline cache), `compile-failed` | G09 | 1 |
| `release-notes-missing` | G10 | 1 |
| `record-incomplete` | G11, and any gate needing a record under `--final` | 1 |
| `reproducibility-mismatch` | G12 | 1 |
| `approval-missing` | G13: gate failure under `plan --final`; refusal in `build` and `verify --rebuild` | 1 / 3 |
| `release-exists` | `build` (output directory non-empty or tag exists locally) | 3 |
| `trust-anchor-missing` | G14 (`verify` without anchor and without `--no-execute`) | 3 |
| `checksum-mismatch`, `smoke-failed` | G14 | 1 |

### Approvals (normative)

| Step | Name | Needs explicit user authorization naming | Never authorized by |
| --- | --- | --- | --- |
| A1 | Build (including any rebuild for G12) | code commit, version, platforms | push, merge, schedule, bot, local commit, this specification, a passing dry run |
| A2 | Tag: creation and push of the named tag | tag name and commit | A1, a successful build |
| A3 | Publication: upload to a release or any public location; the release is created with `gh release create --verify-tag` so that it never creates a missing tag | tag, files, destination | A1, A2 |
| A4 | Consumer activation (Anchorage devcontainer, BundleDock catalog/lock, any pin) | consumer, version | A1 to A3 |

An authorization covers the named values only; a changed commit needs a new one. Approvals and waivers are recorded verbatim (date, quote, who relayed it) in the readiness record before the step. `ci.yml` runs on every push including tag pushes; it contains no release step, which the sentinel test enforces.

### Smoke tests (normative)

| Id | Command | Pass condition |
| --- | --- | --- |
| S1 | `stw version` | prints `SourceSteward <version>` and exits 0 |
| S2 | `stw version --json` | one object equal to the version result with the stamped version |
| S3 | `stw --help` | exits 0; lists no hidden command |
| S4 | `stw inspect <temp repository> --json` | exits 0 in a fresh temporary Git repository; the output is one JSON object with `schema_version`, `backend` equal to `git` and a 40-hex HEAD (checked with the Python standard library, no schema validator) |
| S5 | `stw inspect <temp jj repository> --json` | only where jj and its sandbox exist: valid result or a documented `jj-sandbox-unavailable` refusal; recorded either way |

### Shared conventions with `smill` (normative)

| Convention | Value |
| --- | --- |
| Version variable | `Version` in `internal/cli`, default `dev`, set with `-X <module>/internal/cli.Version=<version>` |
| Version format | the [version format](#version-format-normative), `v` included |
| Version output | `<product or tool name> <version>` text (`SourceSteward v0.1.0`, `smill v0.1.0`), with each tool's existing JSON contract unchanged |
| Platforms | linux amd64/arm64, darwin arm64 |
| Archive name | `<tool>_<version>_<os>_<arch>.tar.gz` |
| Checksums | `SHA256SUMS`, `sha256sum -c` format, unsigned unless stated |
| Build flags | `CGO_ENABLED=0 -trimpath -buildvcs=false`, local toolchain equal to `go.mod`, no fetch |
| Approval rule | build, tag, publication and consumer activation each need explicit authorization; automation never grants it |
| Gate vocabulary | the kebab-case code values of the code table where they apply |

Follow-up (not part of this feature): a specification in `wstein/sheetmill`. Observation only: `smill` already has `cli.Version` and a stamp test that runs `go build`; this feature's stamp test uses `go test` to avoid producing a binary.

## Key Entities

- **Release plan**: result of the dry run, one entry per gate and platform.
- **Release manifest**: machine-readable record of one build (`release-manifest.json`).
- **Readiness record**: human-readable record of gates, evidence, approvals and omissions.
- **Evidence entry**: one row of evidence with a class.
- **Approval entry**: one recorded authorization.

## Success Criteria

- **SC-001**: With `STW_RELEASE_VERSION` set, the stamp test fails on an unstamped test binary and on a misspelled variable, and passes under the exact release linker flag.
- **SC-002**: The dry run reports every gate; one fixture per gate G01 to G14 that `plan` can evaluate yields that gate's code and exit 1 and the clean fixture exits 0; a real-run snapshot test on a fixture module in a temporary repository shows the working tree and `.git` unchanged and only the listed effects.
- **SC-003**: No workflow or Make target reachable from `push`, `pull_request`, `schedule` or `make check` can build release archives, tag or publish (scan test).
- **SC-004**: `release.py build` with a fake runner refuses without A1 (exit 3), uses exactly the recipe environment and arguments, and refuses an existing output directory.
- **SC-005**: `release.py verify` detects a tampered archive, an unstamped binary, a wrong-platform build information and a missing `SHA256SUMS` entry.
- **SC-006**: The notices inventory covers the whole closure of `./cmd/stw` (today fails for Cobra and pflag; fixed by WI3).
- **SC-007**: A completed readiness record exists for the first release with every gate `pass` or `waived`, checks that were not run listed, and the Mac mini manual evidence for darwin/arm64 if that platform ships.
- **SC-008**: Reproducibility is recorded as `match`, `mismatch` or `waived` for each platform; it is never stated without that record.

## Scope and Dependencies

In scope: stamp, recipe, platforms, evidence, gates, record, approvals, dry run, verification, convention list.

Out of scope: running any real build, tag, publication or activation; signing, notarization, attestations (follow-ups); package managers; Windows; darwin/amd64; the `gh-jj` extension and Docker images; BundleDock and Anchorage integration beyond the A4 rule; automatic changelog generation; a `stw --version` flag; any change of `stw` commands or schemas; the sheetmill specification.

Dependencies: Spec 021 (the `pins` command stays hidden; a release must not wait for it), Spec 024 and 025 (not required, planned status excluded from notes by G05), the existing conformance lanes. Constitution Scope: release automation needs a later specification; this feature is that specification for a manual, approval-gated recipe. The script automates the build steps only after A1 and never tags, pushes, publishes or activates.
