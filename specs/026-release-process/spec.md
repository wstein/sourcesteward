# Feature Specification: Release process v2 (build once, promote after review)

**Feature Branch**: `codex/026-release-process-v2`
**Created**: 2026-10-17 (rewrite of the 2026-10-16 "release hygiene" draft, same number)
**Status**: Reviewed (READY after four independent rounds; the round 4 findings and the owner decisions of 2026-10-17 were applied by the author without a further review); constitution amendment pending; implementation pending (see analysis.md)
**Input**: A language-agnostic release process and the `stw release` commands that support it: a project contract (`.stw/release.toml`), a machine-written release record (`docs/releases/<version>.json`), a build-once-then-promote flow, and commands to check, plan, verify, record, dispatch and (in CI) build. Package-manager channels are out of scope.

> **Constitution notice.** Constitution 1.1.0 (Scope and Constraints) says "push/merge/release automation ... require later specifications" and "Future merges, releases and external mutations require authorization in their task context". This specification is that later specification for the commands and workflows it describes, but it does **not** amend the constitution. The proposed amendment text is a separate item for the maintainer: [constitution-amendment-proposal.md](constitution-amendment-proposal.md). Until the owner applies or rejects it, only the read-only work items WI1 to WI5 of [tasks.md](tasks.md) (WI5 adds disclosed read-only GitHub reads) may be implemented; `dispatch`, `build`, the workflows and the bootstrap release wait (WI6 to WI11).

## Decisions (binding, by Werner Stein, 2026-10-17; not re-litigated here)

| Id | Decision |
| --- | --- |
| D1 | **Build once, promote.** A release build runs once on a commit C of `main` (`workflow_dispatch` inputs `commit` and `version`; gates: C is an ancestor of `main`, CI is green on C, the version is greater than every existing tag and release of its track, no release exists), or, as a second input mode, once on an existing tag. The single build is never repeated per release; the old per-release double build is dropped. Reproducibility is checked by a separate job on `main`. The build's artifacts, `SHA256SUMS` and attestations are kept as a draft release. A follow-up commit D (docs and the release record with the build hashes) is merged (a rebase merge changes SHAs, so binding is by ancestry plus an allowlist diff between C and the tag commit, never by exact parent). The owner pushes an **annotated** tag on D. The tag-triggered workflow does **not** build: it verifies and publishes the draft. The allowlist is **deny by default**: anything not declared documentation or record counts as a build input. Builds must not depend on the tag (no VCS stamping, explicit version, `SOURCE_DATE_EPOCH`). |
| D2 | **Language agnostic.** A project contract, not a Go pipeline: the human-edited `.stw/release.toml` in a strict TOML profile with a versioned JSON Schema as the source of truth; the same model may be written as JSON by machines; YAML is not accepted. The machine-written record is canonical JSON at `docs/releases/<version>.json`. The project declares build argv, targets, outputs and manifest, version source and injection, reproducibility policy and the allowlist. No task runner is mandated. The reference adapter is "own script" (what `smill` and `bdock` have); GoReleaser, cargo-dist and similar tools are optional later adapters. |
| D3 | **Commands.** `stw release check`, `plan <commit\|tag>`, `verify`, `record`, and a disclosed-effect `dispatch`. `check`, `plan` and `verify` never execute project scripts. A build executes project commands only in CI on a commit that passed the gates; a local build is verification only and needs explicit authorization. Git and jj are Tier 1 peers. |
| D4 | **No standing approval.** Every tag and release needs the owner's explicit approval each time. The `release` environment has no required-reviewer rule (known limitation). Tags are never re-created; the next version is used. The immutable-release setting and the tag ruleset are the owner's repository settings (documented click path, not automation). Provenance is a GitHub artifact attestation; an SBOM is optional via `syft`. |
| D5 | **Out of scope:** package-manager channels (Homebrew tap, Scoop bucket, winget, deb/rpm, mise/aqua, registries) and pull-model channel workflows, planned as follow-up specs 028 and later (number 027 is inventory limits). The record schema carries what they need. Windows targets are a prerequisite for Scoop. |
| D6 | **Two artifact kinds from v1** so BundleDock bundles fit without a schema migration: `file` (name, URL template, SHA-256, size, os/arch) and `oci-image` (registry repository, index digest, per-platform manifest digests, resolved lock SHA-256 and tool list reference, license and source-offer reference for copyleft components). For `oci-image`, "promote" means adding a version tag to the **same** digest plus attestation, never a rebuild. The version scheme is declared per project (`semver`, `calver` or a pattern) and monotonicity is defined per scheme. Consumer activation (for example the Anchorage devcontainer pin) is a separate, separately authorized step outside the release. |
| D7 | **Bootstrap.** `stw` v0.1 is implemented right after this specification is READY and is then used for all releasable repositories. `stw` cannot release itself before it exists: its own first release uses the tag-workflow pattern already merged in `sheetmill` and `bundledock` ([contracts/bootstrap.md](contracts/bootstrap.md)); it self-hosts from the next release. |
| D8 | **Releasable set and waves.** Wave 1: SheetMill (`smill` CLI), SourceSteward (`stw` CLI, bootstrap via the old tag pattern), BundleDock CLI. Wave 2: the BundleDock toolbox bundle (`oci-image`) and WorkHarbor (polyglot: Go `whr` CLI, Elixir/Phoenix `mix release`, Python scripts; several build commands and several artifacts per project, possibly images). Wave 3: CrewBook (a skill pack: archive plus manifest of Agent Skills, released when a **readiness criterion** is met, not on a date, for example all skills pass validation, a versioned skill schema, a compatibility list of supported agents). The contract therefore supports several `[[build]]` entries and several `[[target]]` artifacts per project and an optional `readiness` gate. |
| D9 | **Multiple products and fetch builds** (maintainer input from the BundleDock third-party source mirror design). A repository may hold several **products**, each with its own contract file, tag grammar and monotonic version (for example `bdock/v1.2.3` and `<component>-v<version>` in one repository); versions are compared only within a product. A build may be a **fetch/assemble step** rather than a compilation (artifact = source bundle plus manifest): its network effect is declared in the contract and it is executed only in CI on a gated commit like any build; the declared command may be another tool (for example `bdock tools sources fetch`). Worked example 5 is the mirror repository `wstein/bundledock-third-party-sources`. |
| D10 | **Owner decisions of 2026-10-17** (decision log; no standing approval changes). (a) The constitution amendment is **not decided**: the owner reads the text first, it stays a proposal and WI6 to WI11 stay blocked. (b) `tag_commit` stays omitted from the record (`verify --json` reports it). (c) **Wave-2 precondition for the BundleDock toolbox bundle** (binding, solved before wave 2 starts): P1 a restartable counter through a `pattern` product with `monotonicity = "non-decreasing"` (counter groups outside `order`); P2 registry authentication is a **workflow-owned login step**, no secret or credential in the contract (the image-capable workflow variant may reference exactly one named secret, in that login step only); P3 moving aliases (`stable`, `latest`) are removed from the record and from `promote`: an alias is a separate, separately authorized step outside the release, like consumer activation. (d) The reproducibility workflow is `workflow_dispatch` only; a `schedule` only with explicit owner approval. (e) Retention is a draft release for the single build with workflow artifacts as fallback; a rehearsal in a **disposable repository** happens before WI10 and only with the owner's explicit approval. (f) Owner-only actor checks fail closed in organizations: accepted for now, recorded as a known limit. (g) `build` (modes `ci`, `promote`, `local-verify`) and `verify --published` are kept. |
| D11 | **Tag ruleset precondition.** A tag ruleset restricting creation (and update and deletion) of the release tags to the owner is a **required precondition before the first tag** of any product. It is a repository setting the owner applies; `stw` may only **report** it (an advisory in `plan --online`, [release-plan.md](contracts/release-plan.md)), never change it, and the report never changes a verdict. |

## User Scenarios & Testing

### User Story 1 - Lint the project contract (Priority: P1)

A maintainer or agent edits `.stw/release.toml` and wants to know whether it is valid, strictly profiled and unable to hide a build input in the allowlist.

**Why this priority**: every other command and the workflow trust the validated model; a contract nobody can lint rots.

**Independent Test**: run `stw release check` over a corpus of valid files and one invalid file per [check finding](#findings-and-gate-codes-normative); compare accepted files with Python `tomllib` in a script test.

**Acceptance Scenarios**:

1. **Given** a valid TOML file in the [strict profile](contracts/config.md#strict-toml-profile-normative), **When** `check` runs, **Then** it exits 0 and prints the normalized model (`--json`: a check result).
2. **Given** dotted keys, inline tables, split tables, a duplicate key, YAML, or both `release.toml` and `release.json`, **When** `check` runs, **Then** each is a distinct finding (`profile-violation` or `config-ambiguous`) and the exit status is 3.
3. **Given** an allowlist pattern that covers `.stw/`, `.github/`, a declared build input, the version file or a readiness file, **When** `check` runs, **Then** it fails with `allowlist-overlaps-input`.
4. **Given** any input, **When** `check` runs, **Then** nothing is written and no process runs other than the existing ownership detection (and the fixed `git cat-file` reads with `--commit`); no project command runs.

### User Story 2 - Know whether a commit may be built, or a tag published (Priority: P1)

A maintainer wants an explanation of every gate for a commit C (build) or for a tag (publish), and of what would be built, before asking anyone to dispatch or tag.

**Independent Test**: `stw release plan` against temporary repositories that each violate exactly one gate of the [gate table](#findings-and-gate-codes-normative) and against one that violates none, with an in-process fake for the GitHub reads.

**Acceptance Scenarios**:

1. **Given** a full commit id on `main` with a valid contract, **When** `plan` runs offline, **Then** local gates are evaluated, online gates are `not-run`, the verdict is `incomplete` and the exit status is 1; with `--online` the verdict is `pass` (0) or `fail` (3).
2. **Given** a tag, **When** `plan` runs, **Then** it evaluates the publish gates (annotation, ancestry, allowlist diff between the record's commit and the tag commit).
3. **Given** any invocation, **When** it completes, **Then** the output states that no project command was executed, that nothing was built, and which reads used the network.
4. **Given** a rebase-merged follow-up commit, **When** `plan <tag>` runs, **Then** the binding is by ancestry and allowlist diff, not by parent identity (measured, see [research](research.md#measurements)).
5. **Given** a contract with a `[readiness]` table, **When** `plan <commit>` runs, **Then** it checks only that each required evidence file exists in C's tree as a non-empty regular file (`readiness-missing` otherwise); it never executes anything and never judges file content.

### User Story 3 - Dispatch one build with disclosed effects (Priority: P1)

An authorized maintainer or agent starts the CI build of commit C for a version, after seeing an inspectable plan.

**Independent Test**: `dispatch` against an in-process GitHub fake: default output is a plan with `plan_id` and zero writes; `--apply` with a stale or wrong id refuses; a successful apply sends exactly one request.

**Acceptance Scenarios**:

1. **Given** no `--apply`, **When** `dispatch` runs, **Then** it evaluates the build gates online, prints the exact request (workflow, ref, inputs), the effects (network write that starts a CI build creating a draft release and attestations) and a `plan_id`, and sends nothing.
2. **Given** `--apply PLAN_ID` matching a freshly recomputed plan, **When** `dispatch` runs, **Then** it sends one workflow dispatch request and reports the Actions URL; a mismatch refuses with `plan-stale`.
3. **Given** a plan that failed a gate, **When** `--apply` is used, **Then** it refuses with `gate-failed`.
4. **Given** the output, **When** it is read, **Then** it says a plan is not authorization: the caller's own authorization for a build in the task context governs (umbrella AGENTS.md).

### User Story 4 - Write the release record deterministically (Priority: P1)

After the build, the maintainer downloads the draft's files and runs `record` to produce the canonical `docs/releases/<version>.json` for the follow-up commit D.

**Independent Test**: running `record` twice on the same inputs gives byte-identical output; tampering with any input changes the result or refuses; the output validates against the record schema and equals its own canonical re-encoding.

**Acceptance Scenarios**:

1. **Given** a directory with `SHA256SUMS`, `build-manifest.json` and the files, **When** `record --commit C --version V --from DIR` runs, **Then** it recomputes each hash and size, requires equality with `SHA256SUMS`, and prints the [canonical record](contracts/record.md) to standard output; `--write` creates exactly `docs/releases/<version>.json` and refuses to overwrite a different file.
2. **Given** an `oci-image` target, **When** `record` runs with its build manifest, **Then** the record carries the index digest, the per-platform manifest digests, the lock SHA-256 read from C's tree and the tool list and license references.
3. **Given** `--check`, **When** a committed record is not the canonical encoding of its content, **Then** it fails with `record-not-canonical`.

### User Story 5 - Verify before publishing and after downloading (Priority: P1)

The tag-triggered workflow (and any human) checks the annotated tag, the allowlist diff, CI, the attestation and the artifact hashes against the record, then the workflow publishes the draft without building.

**Independent Test**: `stw release verify` over fixtures: good, lightweight tag, tag not on `main`, disallowed file in the diff (code, workflow, config, mode change, symlink, gitlink), tampered artifact, missing artifact, extra artifact, wrong record version, non-monotonic version, existing release.

**Acceptance Scenarios**:

1. **Given** a tag whose commit descends from the record's commit with an allowlisted diff, an artifact directory equal to the record and (online) valid attestations, **When** `verify` runs, **Then** the verdict is `pass` and it exits 0.
2. **Given** a diff entry outside the allowlist, a type or mode change, a symlink, a gitlink or a rename into or out of an allowlisted path, **When** `verify` runs, **Then** it fails with `diff-not-allowlisted` naming the path (control characters escaped).
3. **Given** a jj-owned workspace, **When** `verify` runs, **Then** it refuses with `unsupported-backend`; the CI run (always a Git checkout) performs the authoritative verification.
4. **Given** the verification, **Then** no project command is executed and the publish step is performed by the workflow, never by `verify`.

### User Story 6 - Build once in CI, never per release (Priority: P1)

The CI build job, started only by `dispatch` (or the second input mode on an existing tag), builds the declared targets once and keeps the result as a draft release with attestations.

**Independent Test**: unit tests of `stw release build` with a fake command runner assert environment, argv, refusals and outputs; workflow rule tests with mutants assert the trigger, permission and job structure ([contracts/workflow.md](contracts/workflow.md)); no real build runs without authorization.

**Acceptance Scenarios**:

1. **Given** a commit that passed the build gates, **When** the build job runs, **Then** each `[[build]]` runs exactly once per [scope](contracts/config.md#build-and-target-tables) with the constructed environment of the [build table](#build-environment-normative), `SOURCE_DATE_EPOCH` from C's commit time and the explicit version.
2. **Given** `GITHUB_ACTIONS` unset and no `--mode local-verify`, **When** `build` runs, **Then** it refuses with `ci-only`.
3. **Given** an `oci-image` target, **When** the build ends, **Then** the image is pushed by digest to a staging reference and the version tag is added only by the tag-triggered promote step.
4. **Given** the tag-triggered workflow, **When** it runs, **Then** it contains no build step (rule test).

### User Story 7 - The same contract fits five kinds of project (Priority: P2)

A Go CLI, an OCI bundle with calendar versions, a polyglot project with several builds and artifacts, a skill pack and a multi-product source mirror with fetch builds declare their contracts and use the same commands ([worked examples](contracts/examples.md)).

**Independent Test**: the example contracts (six files) and their example records are fixtures; each passes `check`, each record validates against the record schema, and monotonicity is tested per scheme with the examples of the [version table](#version-schemes-normative).

**Acceptance Scenarios**:

1. **Given** the scheme `pattern` with `toolbox/2026.10.15-b98.1` and `toolbox/2026.10.10-b4.1`, **When** monotonicity is evaluated, **Then** the order is by the declared numeric groups, not by string.
2. **Given** a project without Go (the skill pack, the Elixir release), **When** `plan` runs, **Then** no Go tool is needed or mentioned.
3. **Given** a project with four `[[build]]` entries and five `[[target]]` artifacts, **When** `build` runs in CI, **Then** each build runs once and each artifact is recorded with its own build id.
4. **Given** a skill pack whose `[readiness]` evidence file is absent at C, **When** `plan` runs, **Then** it fails `readiness-missing`; the readiness criterion itself is evaluated by the project's own validation (not by `stw`).
5. **Given** a repository with two products (`.stw/release.zlib.toml`, `.stw/release.busybox.toml`), **When** `plan zlib-v1.3.2` runs, **Then** the product is selected by the tag grammar and only tags and releases matching that product's grammar enter the monotonic comparison; `plan <commit> --version 1.3.2` without `--product` refuses `product-required`.
6. **Given** a `fetch` build, **When** `plan` runs, **Then** the output lists the declared hosts and lock files as a disclosed network effect of the CI build (nothing is fetched by `plan`); `verify` treats the artifacts like any other and adds nothing for the fetch; `check` rejects a `fetch` build without hosts and, with reproducibility `required`, without lock files.
7. **Given** a declared command such as `bdock tools sources fetch`, **When** `check` runs, **Then** the tool is only an argv element; `stw` never resolves, runs or inspects it.

## Edge Cases

- **Existing-tag mode and publication.** In input mode 2 (build once on an existing tag) the tag exists before the build, so the tag-push workflow has already run and failed with `record-missing`. Publication therefore uses `release-publish.yml` started by `workflow_dispatch` with the tag and a **record commit** R on `main` that descends from the tag commit (C is the tag commit) and whose allowlisted diff from C holds the record: `verify <tag> --record-commit R`. The bootstrap release `v0.1.0` is published by its own tag-triggered workflow ([bootstrap](contracts/bootstrap.md)) and its record is added afterward; it is verified after publication with `verify --published` (below), not published by this path. The subject tag itself is excluded from the monotonic comparison.
- **Drafts and token visibility.** A read-only token does not see drafts (reported by the sibling design, unverified here), and `stw` cannot detect that. Therefore the draft part of `release-exists` is evaluated only when the caller passes `--with-drafts`, asserting that its token sees drafts (the workflow jobs with `contents: write`); without it that part is skipped and the reason `drafts-not-checked` is printed. The gate's outcome comes from the tag and release part (`pass` or `fail`) with the reason `drafts-not-checked` when the drafts were not checked; it is never `not-applicable` as a whole. The authoritative draft check is the `--with-drafts` run in the job that creates the draft. `draft-missing` always needs the draft: a token that does not see it fails the gate (detail: absent or not visible), which fails closed.
- **Record cannot contain its own commit.** The record names C (the built commit); the tag commit does not appear in it because the record lives in D (a commit cannot contain its own hash). Consumers resolve the tag through Git; `verify --json` reports `tag_commit`. A `tag_commit` field in the record is absent by construction (open question 2).
- **Rebase merge.** D and its parent change SHAs when rebase-merged. Binding is "C is an ancestor of the tag commit, the tag commit is an ancestor of `main`, and the net tree diff C..tag is allowlisted". Intermediate commits that change and revert build inputs are accepted because the tree equals C's outside the allowlist (measured: ancestry survives a rebase; the net diff lists only changed paths).
- **Main moved with code.** If other code merged between C and D, the net diff contains build inputs and `diff-not-allowlisted` fails; release the next version from a new C.
- **CI evidence needs a run on C itself.** With rebase merges, workflows run on the pushed tip only. C must be a commit CI ran on (normally the tip after a merge); otherwise `ci-not-green` with reason `ci-run-absent`. WorkHarbor's CI requires pull requests to target `main` (stated by the maintainer, **not yet read in that repository**); that requirement is compatible, because C is taken from `main`, but its release policy is an unverified dependency (see Scope and Dependencies).
- **Draft semantics.** Publishing a draft creates a missing tag at the target commit as a lightweight tag. The flow never publishes a draft in the web UI; the workflow publishes only after the owner's annotated tag exists, using `--verify-tag` (unverified behavior, see [research](research.md#unverified-github-behavior)).
- **Asset replaced in the draft.** Anyone with write access can alter draft assets. The tag-run compares hashes to the record reviewed in D and verifies attestations; a replaced asset fails `artifact-mismatch` or `attestation-invalid`.
- **Hostile repository text.** Config, record, paths, tags and release text are data; control characters are escaped on output; every field and file is bounded by the [limits table](#limits-normative).
- **Version file.** If the project declares its version in a file, the file at C must contain the dispatch version (`version-file-mismatch`); the file is a build input and is never allowlisted.
- **Unknown keys.** Config and record schemas reject unknown properties at `schema_version: 1` (unlike result schemas); additions need a new `schema_version`.
- **Windows targets.** Allowed in the model (`os: windows`, `.zip`); no Windows build or Scoop support is claimed (D5).
- **Annotated tag object text.** The tag message is data and is never executed or trusted for the version.
- **Readiness is evidence presence, not judgment.** `[readiness]` lists repository files (for example a validation report the project's own CI committed); `plan` checks that they exist at C, are regular and non-empty. A stale or false report passes; the owner's review of the follow-up PR is the real gate. The project's own CI produces the evidence.

## Requirements

### Functional Requirements

- **FR-001**: The commands are exactly those of the [command table](#commands-normative); the flags listed in each contract are normative and complete. `check`, `plan`, `verify` and `record` start no project command. They may start `git`, the jj read-only runner and `gh` only as listed in the effects of each contract; `internal/cli` starts no process and imports only reviewed operations (the architecture check is extended in WI1).
- **FR-002**: The project contract is `.stw/release.toml` (strict TOML profile) or `.stw/release.json`, validated against `schemas/v1/release-config.schema.json` (source of truth). Exactly one of the two may exist (`config-ambiguous`). YAML is not accepted. Profile, fields and limits are in [contracts/config.md](contracts/config.md).
- **FR-003**: The release record is canonical JSON at `docs/releases/<version>.json` validated against `schemas/v1/release-record.schema.json`, with the two artifact kinds `file` and `oci-image` ([contracts/record.md](contracts/record.md)). The record names the built commit C, never the tag commit.
- **FR-004**: Version schemes are `semver`, `calver` and `pattern` ([version table](#version-schemes-normative)); the scheme is declared per project and defines validity and monotonicity. Pre-release and build-metadata versions are not valid in v1. A tag is derived from a version by the scheme (`tag_prefix` + version, or `tag_template` for `pattern`), and the version recovered from a tag equals the original (round trip).
- **FR-005**: The build and publish gates are exactly the rows of the [gate table](#findings-and-gate-codes-normative). Outcomes are `pass`, `fail`, `not-run` or `not-applicable`; the verdict is `pass`, `fail` or `incomplete`. A `not-run` gate never counts as `pass`.
- **FR-006**: The diff allowlist is deny by default. A path is doc/record only if it matches a declared allowlist pattern and the entry is a regular file (mode 100644 or 100755, unchanged mode) with status A, M or D, with rename and copy detection disabled. Any other entry (type change, symlink, gitlink, mode change, path outside the patterns, more than 20,000 entries) fails `diff-not-allowlisted` or `release-limit`. Entries are streamed and parsed with a limit on entries and bytes (see the limits table). The check uses `git diff-tree -r --raw -z --no-renames --no-ext-diff --no-textconv` under the isolated Git prefix and reads no content, so no filter or textconv runs (measured).
- **FR-007**: Build independence from the tag: the declared build must not read VCS state or the tag. `check` enforces the declared contract only (the version arrives through placeholders, `SOURCE_DATE_EPOCH` is set from C's commit time, the environment is constructed). Whether a project command honors this is evidenced by the reproducibility job, not proven by `check`.
- **FR-008**: Build execution: `stw release build` runs project commands only when `GITHUB_ACTIONS=true` (modes `ci` and `promote`, and `ci --smoke`) or with explicit `--mode local-verify` (verification only, output outside the repository, no publication). Only `promote` sets a registry version tag, on the digest the verified record names; no mode publishes a release, pushes or creates a Git tag. See [contracts/build.md](contracts/build.md).
- **FR-009**: Retention is a draft GitHub release holding the files, `SHA256SUMS` and `build-manifest.json`, plus attestations; images are staged by digest. Publication is performed only by the tag-triggered workflow after `verify` passes ([contracts/workflow.md](contracts/workflow.md)). Fallback if draft behavior fails in rehearsal: workflow artifacts (research).
- **FR-010**: `dispatch` follows plan then apply with a stateless `plan_id`, revalidates the gates online at apply, sends exactly one request and discloses the effect ([contracts/dispatch.md](contracts/dispatch.md)).
- **FR-011**: Attestation is a GitHub artifact attestation made by the build job for every file artifact, `SHA256SUMS` and `build-manifest.json` (images by index digest). `verify --online` checks it with `gh attestation verify` pinned to the repository resolved through hosting (never taken from the record), the signer workflow `ci.release_workflow` read from the contract at C, the source ref `refs/heads/main` and `--deny-self-hosted-runners`. Repository, signer, tag, project, product, `url_template` and `tags` of the record are recomputed from the contract and hosting and must equal the record (`record-invalid`). The attested `build-manifest.json` binds the files to the built commit C ([research](research.md#attestation-binding-to-commit-c)).
- **FR-012**: No standing approval. `dispatch --apply`, `build --mode local-verify`, the owner's tag push and consumer activation each need explicit authorization in their task context. No push, merge, schedule, bot or commit authorizes them. The workflow permits dispatch and publish only for the repository owner (`github.triggering_actor` and `github.actor`); organization-owned repositories fail closed until a later specification adds an allow-list (open question 6).
- **FR-013**: Tags are never re-created: a version that exists as a tag or release (published or draft) is refused; the next version is used. The tag ruleset (restricting creation, update and deletion of release tags to the owner, **required** before the first release because the publish workflow runs the workflow file of the pushed tag commit) and the immutable-release setting are the owner's settings, documented in the [quickstart](quickstart.md#owner-settings-click-path), never automated.
- **FR-014**: Backend capability (constitution V) follows the [capability table](#backend-capability-normative): the backend is selected by ownership, a jj-managed workspace uses jj, ambiguous ownership refuses, unsupported capability refuses with `unsupported-backend` and never falls back to raw Git.
- **FR-015**: All commands are non-interactive, bounded by the [limits table](#limits-normative), print human output and (`--json`) one result derived from the same data with versioned schemas, and escape control characters in repository text.
- **FR-016**: Read-only claims are verified by effect sentinels (index, refs, HEAD, configuration, working-copy snapshot and operation log for jj, no new files, network sentinel, hook/filter/textconv sentinels). `record --write`, `dispatch --apply` and the `build` modes are the only effects and are tested as such.
- **FR-017**: A reproducibility check is a separate workflow (`workflow_dispatch` only) that builds `main` twice on independent runners and compares hashes; it never releases, tags or publishes. A `schedule` trigger needs the owner's explicit approval (umbrella AGENTS.md: schedules do not authorize builds) and is absent until given.
- **FR-018**: The `stw` workflows (bootstrap, release, reproducibility) follow the security posture of [contracts/workflow.md](contracts/workflow.md) (pinned actions, minimal permissions, no secrets, inputs only through `env:`), enforced by rule and mutation tests.
- **FR-019**: Reuse and drops from the earlier "release hygiene" draft are recorded in [research.md](research.md#reuse-and-drops-from-the-release-hygiene-draft); this specification replaces it under the same number 026.
- **FR-020**: A target joins a release only with its recorded evidence. CI conformance exists on linux/amd64 only today; arm64 and darwin results are local evidence. No false equivalence between Git and jj is implied.
- **FR-021**: A project may declare several `[[build]]` entries and several `[[target]]` artifacts; each target names the build that produces it. A build has scope `per-target` (runs once for every target that references it) or `once` (runs once and produces all targets that reference it). Targets may carry a `group` label and a `role` (`artifact`, `manifest`, `metadata`, `sbom`).
- **FR-022**: An optional `[readiness]` table lists repository files that must exist at C. `plan` evaluates it by tree lookup only (no execution, no content judgment, no network); it is gate `readiness-missing`. Readiness files are build inputs and cannot be allowlisted.
- **FR-023**: Worked examples for a Go CLI, an OCI bundle, a polyglot project, a skill pack and a two-product fetch mirror are fixtures validated against the schemas ([contracts/examples.md](contracts/examples.md)).
- **FR-024**: `stw` is released first by the bootstrap workflow and from the next release by the process of this specification ([contracts/bootstrap.md](contracts/bootstrap.md)); the bootstrap workflow is removed in the work item that switches `stw` to self-hosting.

- **FR-025**: A repository may declare several products ([contracts/config.md](contracts/config.md#products-and-files-normative)): one contract file per product, per-product tag grammar, monotonic comparison, record path and workflow input `product`; products' literal tag prefixes must not be equal or prefix-related (`product-tag-overlap`).
- **FR-026**: A build may declare `network = "fetch"` with its hosts and optional pinning `lock_files`; `plan` and the build manifest disclose it; `stw` does not enforce or sandbox network access (the repository's CI egress policy does); `lock_files` are build inputs.
- **FR-027**: Existing-tag publication (input mode 2) uses `verify <tag> --record-commit R` through a `workflow_dispatch` of the publish workflow. Verification of an already published release (downloaded assets; the bootstrap release `v0.1.0`) uses `verify <tag> --published [--record-commit R] --artifacts DIR`, where `already-published`, `draft-missing` and `version-not-monotonic` are `not-applicable` and, for a release not made by the build workflow, the signer file and source ref are given explicitly with `--signer-workflow-file` and `--source-ref` ([contracts/verify.md](contracts/verify.md)).
- **FR-028**: Wave-2 preconditions (D10) and the tag-ruleset precondition (D11) are checkable facts, not wishes: P1 is tested by the toolbox worked example (restart accepted, date regression refused); P2 by a workflow rule test (one secret reference, only in the login step of the image-capable variant, none in the contract); P3 by the record rule `tags == [version]` and a `promote` test that adds no other tag. `plan --online` reports the tag-ruleset state as an advisory (`present`, `absent`, `unknown`) and never changes it or the verdict.

### Commands (normative)

| Command | Purpose | Network | Writes | Starts |
| --- | --- | --- | --- | --- |
| `stw release check [PATH] [--product NAME] [--commit SHA]` | lint the contract(s) | none | none | the existing ownership detection; with `--commit` the fixed `git cat-file` reads |
| `stw release plan <commit\|tag> [--product NAME] [--version V] [--for build\|publish] [--record-commit R] [--online] [--with-drafts] [--repo OWNER/NAME]` | explain gates and what would be built or verified | only with `--online` (GETs) | none | `git` or the jj read-only runner; with `--online` the hosting reads |
| `stw release verify <tag> [--product NAME] [--record-commit R] [--published] [--signer-workflow-file PATH] [--source-ref REF] [--artifacts DIR] [--online] [--with-drafts] [--repo OWNER/NAME]` | publish gates: diff, record, artifacts, attestations | only with `--online` | none | as `plan`; with `--online` and artifacts `gh attestation verify` |
| `stw release record --commit C --version V --from DIR [--product NAME] [--repo OWNER/NAME] [--signer-workflow-file PATH] [--run-id N --run-attempt N] [--write \| --check FILE]` | write the canonical record | none | with `--write`: one new file | `git` (read C's tree) |
| `stw release dispatch (--commit C --version V \| --tag T) [--product NAME] [--repo OWNER/NAME] [--host HOST] [--apply PLAN_ID]` | start the CI build | GETs; one POST with `--apply` | remote: starts a workflow run | hosting transport |
| `stw release build --mode ci\|promote\|local-verify ...` (flags in [contracts/build.md](contracts/build.md)) | build the declared targets or fetch-assemble them (CI), smoke, promote an image tag, or rebuild to verify (local) | project-dependent; none by `stw` | the outputs directory | project commands |

All commands accept `--json` and an optional `PATH` (default: current directory). An empty `--product` value is treated as absent (so workflows may pass an optional input unconditionally). `--signer-workflow-file` and `--source-ref` are accepted only with `--published` (usage error otherwise) and `record` accepts `--signer-workflow-file`. Argument classification: an argument of exactly 40 or 64 lowercase hexadecimal characters is a commit; any other argument that is not empty, does not start with `-`, contains none of the characters Git forbids in ref names (`git check-ref-format --branch`-style rules: control characters, space, `~`, `^`, `:`, `?`, `*`, `[`, `\`, `..`, `@{`, a trailing `.lock` or `/`) and is at most 255 bytes is a tag name, looked up as `refs/tags/<arg>` and then matched against the product grammars (an absent tag is the refusal `tag-unresolved`, not a usage error); anything else is a usage error (exit 2, decided before any repository read).

### Findings and gate codes (normative)

All codes are kebab-case. "Net" means the gate needs the network (`--online` or CI). Gate outcomes are `pass`, `fail`, `not-run`, `not-applicable`.

| Code | Applies to | Net | Meaning |
| --- | --- | --- | --- |
| `commit-not-on-main` | build | no (local remote-tracking `main` of the hosting-resolved remote; staleness disclosed) | C is not an ancestor of `main` |
| `config-invalid` | build, publish | no | the contract at the evaluated commit fails `check` |
| `version-invalid` | build, publish | no | the version does not match the scheme |
| `version-not-monotonic` | build, publish | yes | not greater than every existing tag and release of the product (excluding the subject tag) |
| `release-exists` | build | yes | a tag (input mode 1), a release or a draft already exists for the version; the draft part is authoritative only with a token that sees drafts |
| `ci-not-green` | build, publish | yes | some declared workflow has no run with `head_sha` C, `event` `push`, `head_branch` `main` and conclusion `success` (reason `ci-run-absent`, `ci-run-failed` or `ci-run-pending`) |
| `version-file-mismatch` | build | no | the declared version file at C does not contain the version |
| `readiness-missing` | build | no | a `[readiness]` file is absent, not regular or empty at C |
| `tag-not-annotated` | publish | no | the tag is not an annotated tag object |
| `tag-version-mismatch` | publish | no | the tag does not equal the scheme's tag for the record's version |
| `tag-not-on-main` | publish | no | the tag commit is not an ancestor of `main` |
| `record-missing` | publish | no | no record at the tag commit |
| `record-invalid` | publish | no | the record fails its schema or semantic rules |
| `record-not-canonical` | publish, record | no | record bytes differ from the canonical encoding |
| `build-commit-not-ancestor` | publish | no | the record's commit is not an ancestor of the tag commit |
| `diff-not-allowlisted` | publish | no | a path between C and the tag commit is not an allowlisted regular-file change |
| `artifact-missing` | publish | no | a recorded artifact is absent from the provided directory |
| `artifact-unexpected` | publish | no | the directory holds a file the record does not list |
| `artifact-mismatch` | publish | no | size or SHA-256 differs from the record |
| `attestation-missing` | publish | yes | no attestation for a recorded subject |
| `attestation-invalid` | publish | yes | verification fails, another repository or workflow signed, or the attested manifest names another commit |
| `already-published` | publish | yes | a non-draft release exists for the tag (`not-applicable` under `--published`) |
| `draft-missing` | publish | yes | no draft release for the tag holds the recorded assets (also when the token cannot see drafts) |

A limit exceeded during `plan`, `verify`, `record` or `build` is never a gate outcome: it is the refusal `release-limit` (exit 1, `complete: false`, fail closed). In `check`, an oversized or over-count input is the finding `limit-exceeded`.

Check findings (exit 3 of `check`): `product-tag-overlap`, `fetch-unpinned`, `profile-violation`, `schema-violation`, `config-ambiguous`, `allowlist-overlaps-input`, `argv-not-array`, `placeholder-unknown`, `limit-exceeded`, `duplicate-id`, `unknown-reference`, `scheme-invalid`, `env-name-forbidden` (errors) and `unused-build` (info). Refusals and operational failures (exit 1; `product-required` and `product-unknown` are refusals of the commands that take `--product`, and `check` without `--product` lints every product instead): `release-limit`, `product-required`, `product-unknown`, `tag-unresolved`, `main-ref-stale`, `unsupported-backend`, `ambiguous-backend`, `bare-repository`, `not-repository`, `config-missing`, `commit-unresolved`, `remote-unresolved`, `online-unavailable`, `auth-unavailable`, `plan-stale`, `gate-failed`, `ci-only`, `checkout-mismatch`, `export-unsafe`, `output-exists`, `build-failed`, `promote-failed`, `canceled`; `unexpected-output` is an info note of `build`.

### Version schemes (normative)

| Scheme | Tag | Valid version | Order | Example |
| --- | --- | --- | --- | --- |
| `semver` | `tag_prefix` + version | `\Av(0\|[1-9][0-9]{0,8})\.(0\|[1-9][0-9]{0,8})\.(0\|[1-9][0-9]{0,8})\z` | numeric tuple (major, minor, patch) | `v0.1.0`; tag `bdock/v0.1.0` with prefix `bdock/` |
| `calver` | `tag_prefix` + version | `\A([0-9]{4})\.(0[1-9]\|1[0-2])\.(0[1-9]\|[12][0-9]\|3[01])(?:-b([1-9][0-9]{0,8})(?:\.([1-9][0-9]{0,8}))?)?\z` and a real calendar date (`2026.02.31` is invalid) | numeric tuple (year, month, day, build, patch); an absent group sorts below any present value | `2026.10.15-b98.1` |
| `pattern` | `tag_template` with `{version}` replaced | the `version` group of `tag_pattern`; 1 to 64 characters of `[A-Za-z0-9._-]`; groups named in `order` are `[0-9]{1,9}` | numeric tuple of the `order` groups (absent sorts below present); `monotonicity` `strict` (default) or `non-decreasing` | `zlib-v{version}` with `version` = `1.3.1`; `toolbox/{version}` with `order` of year, month, day and `non-decreasing` |

The **monotonicity set** of a product is every existing tag and release (published or draft) whose name fully matches the product's tag grammar (`tag_prefix` + the scheme's version regex, or `tag_pattern`). Names that do not match are ignored and counted. A new version must be strictly greater than every member of the set other than the subject tag itself; for a `pattern` product with `monotonicity = "non-decreasing"` it must instead have an `order` tuple greater than or equal to the maximum in the set while its full tag does not exist (`release-exists`), which permits restartable counters outside `order`. Because the set is defined by the full grammar, a tag with an optional group present or absent (`toolbox/2026.10.14` after `toolbox/2026.10.15-b98.1`) is compared in the same set and refused. A deleted tag without a release is invisible to the check, a limitation shared with the siblings. `{version}` is the version string; `{version_number}` is the version without a leading `v`. Leading zeros in `order` groups (`01` and `1`) give equal tuples for distinct tags; the second is refused because it is not strictly greater. A scheme whose counter may restart (BundleDock documents `2026.10.15-b1` after `2026.10.15-b98.1`) is declared as a `pattern` product with `monotonicity = "non-decreasing"` and the counter groups left out of `order` (precondition P1 of D10; the `calver` scheme stays strict).

### Placeholders (normative)

Placeholders are a closed set; any other brace expression, or a placeholder not allowed in the field, is `placeholder-unknown`. Values: `{version}` the version string; `{version_number}` without a leading `v`; `{commit}` and `{commit12}` the built commit (full, first 12 characters); `{source_date_epoch}` the commit time; `{os}`, `{arch}`, `{target}` the target's values; `{output}` the target's output path; `{out_dir}` the absolute path of `<root>/dist/release`; `{artifact}` the absolute path of the target's output file in the artifact directory; `{repository}` `owner/name` (url template) or the image repository (promote); `{tag}` the Git tag; `{name}` the asset file name; `{digest}` the image index digest.

| Field | Allowed placeholders |
| --- | --- |
| `build.argv`, `build.env_set` values | `{version}`, `{version_number}`, `{commit}`, `{commit12}`, `{source_date_epoch}`, `{out_dir}`; for a `per-target` build also `{os}`, `{arch}`, `{target}`, `{output}` |
| `target.output`, `target.result_file` | `{version}`, `{version_number}`, `{commit12}`, `{os}`, `{arch}`, `{target}` |
| `target.staging_tag` | `{commit}`, `{commit12}` (never `{version}`: a staging reference must not equal a release tag) |
| `target.smoke_argv` | `{version}`, `{version_number}`, `{commit}`, `{os}`, `{arch}`, `{target}`, `{artifact}`, `{out_dir}` |
| `promote.argv` | `{repository}`, `{digest}`, `{version}`, `{version_number}` |
| `outputs.url_template` | `{repository}`, `{tag}`, `{name}` |

### Build environment (normative)

| Item | Rule |
| --- | --- |
| Environment | cleared; then `env_inherit` names copied from the CI environment (default `PATH`, `HOME`, `TMPDIR`), then the build's `env_set` entries, then `SOURCE_DATE_EPOCH` (C's committer time), `TZ=UTC`, `LC_ALL=C`. Names matching `*TOKEN*`, `*SECRET*`, `*KEY*`, `*PASSWORD*`, `*AUTH*`, `*CREDENTIAL*`, `ACTIONS_*`, `GITHUB_*`, `SSH_AUTH_SOCK` are refused in `env_inherit` and `env_set` (`env-name-forbidden`). `stw` passes no secret; an inherited `HOME` can still expose credential files of the runner user, so a build runs on an ephemeral runner and the contract does not claim isolation |
| Version | injected only by the placeholders `{version}`, `{version_number}` in argv and `env_set` values; `stw` never reads it from Git for the build |
| Argument vector | `argv` array, no shell; placeholders per the [placeholder table](#placeholders-normative) |
| Working directory | the build's `working_dir` (default `.`), confined under the repository root, no `..`, no absolute path |
| Time | `timeout_seconds` per command (default 1800, maximum 7200) |
| Network | a build with `network = "fetch"` may contact its declared `network_hosts`; the declaration is disclosed by `plan`, the manifest and the dispatch plan, and is not enforced by `stw` |
| Output | each target's declared `output` (file kind) or the build's result file (oci-image kind); other new files are reported, not collected |
| Clean tree | HEAD equals C; tracked changes are checked before and after with `git diff-index --quiet HEAD`; `stw` never runs `git status` |

### Backend capability (normative)

| Command | Git | jj (standalone, colocated, secondary workspace) |
| --- | --- | --- |
| `check` | supported (file read; `--commit` reads the blob) | supported; working-tree file read directly; `--commit` through the existing jj read-only runner (unverified until WI1 conformance) |
| `plan <commit>` | supported (ancestry against `refs/remotes/<remote>/main`) | supported through jj's documented interface (ancestry as the revset `C & ::main@<remote>`, tree lookup, diff); unverified (jj is not installed where this specification was written) |
| `plan <tag>`, `verify` (including `--record-commit`, `--published`), `dispatch --tag` | supported | `unsupported-backend`: the annotation check needs the tag object, which jj is not known to expose through a documented interface (unverified); use a Git-owned checkout or the CI run |
| `record` | supported | supported (reads C's tree through jj; `--write` creates one file that jj snapshots on its next command, disclosed) |
| `dispatch` | supported (repository identity via hosting resolution) | supported (hosting resolution is native for both) |
| `build --mode ci` (including `--smoke`) | the CI checkout is Git | not applicable |
| `build --mode local-verify` | supported (tree-object export) | `unsupported-backend` |
| GitButler-managed | `unsupported-backend`, except `check` of the working-tree file | |
| Ambiguous ownership | `ambiguous-backend` | |

### Limits (normative)

| Limit | Value | Result when exceeded |
| --- | --- | --- |
| Config file | 64 KiB; 16 builds; 64 targets; 64 argv elements each at most 4,096 bytes; 64 `env_set` per build; 256 allowlist patterns each at most 256 bytes; 32 readiness files | `limit-exceeded` (check), `release-limit` elsewhere |
| Record file | 1 MiB; 64 assets; 32 platforms per image | `record-invalid` (over 1 MiB: `release-limit`) |
| `SHA256SUMS` | 64 KiB, 65 lines (64 assets plus the manifest) | `release-limit` |
| Raw tree diff C..tag | 20,000 entries and 4 MiB of raw output, read by a streaming parser (not the 64 KiB buffered Git runner) | `release-limit` (fail closed) |
| Artifact size hashed by `verify` and `record` | 2 GiB each, streamed | `release-limit` |
| Tags/releases listing | 10 pages, 1,000 records (the hosting caps) | `release-limit` |
| Other Git output | the existing inspection runner cap (64 KiB per stream); blobs read for the contract, record and version file are limited by their own rows above | `release-limit` |
| Project command output | 1 MiB retained per stream, the rest discarded with a note | noted, not a failure |
| Products per repository | 64 contract files | `limit-exceeded` (check), `release-limit` elsewhere |
| Deadlines | 30 s local commands; 120 s online `plan`/`verify` excluding artifact hashing; 60 s per `gh attestation verify` | `online-unavailable` or `canceled` |

### Exit statuses (normative)

| Status | Meaning |
| --- | --- |
| 0 | complete and clean: `check` valid; `plan`/`verify` verdict `pass`; `record` printed, written or `--check` equal; `dispatch` plan with verdict `pass` or request accepted; `build` built, smoked, promoted or (`local-verify`) hashes equal |
| 1 | refusal, operational failure, cancellation, or verdict `incomplete` (no gate failed, some required gate `not-run`) |
| 2 | usage error; standard output stays empty; decided before any repository read |
| 3 | `check` found findings; `plan`/`verify`/`dispatch`-plan verdict `fail` (at least one gate `fail`, even if other gates are `not-run`); `record` inputs disagree or `--check` differs (`artifact-mismatch`, `record-invalid`, `record-not-canonical`); `build --mode local-verify` found a hash difference |

Precedence: 2, then 1, then 3, then 0. Verdict rule (one definition): any `fail` gives `fail`; otherwise any required `not-run` gives `incomplete`; otherwise `pass`; `not-applicable` is ignored. `dispatch --apply` after a failed or changed plan refuses (`gate-failed`, `plan-stale`, exit 1).

## Key Entities

- **Release contract**: the validated `.stw/release.toml` model ([data-model.md](data-model.md)).
- **Release record**: canonical JSON per version, assets of kind `file` or `oci-image`.
- **Build manifest**: attested JSON written by the CI build next to the artifacts.
- **Plan and verification results**: gates, verdict and effects.
- **Dispatch plan**: the stateless plan and its `plan_id`.

## Success Criteria

- **SC-001**: `check` accepts the worked-example contracts and rejects one fixture per check finding; accepted TOML files parse identically under Python `tomllib`, and the profile violations that `tomllib` accepts (dotted keys, inline tables, split tables) are rejected (measured that `tomllib` accepts them).
- **SC-002**: One fixture per gate of the gate table yields exactly that code and exit 3; the clean fixture yields exit 0; offline `plan` yields `incomplete` and exit 1.
- **SC-003**: `record` is byte-deterministic and idempotent; `record --check` fails any non-canonical record.
- **SC-004**: `verify` detects each of: lightweight tag, tag not on `main`, code/workflow/config/mode/symlink/gitlink change in the diff, tampered, missing and extra artifact, wrong version, existing release, bad attestation.
- **SC-005**: Effect sentinels prove `check`, `plan`, `verify`, `record` (without `--write`) and `dispatch` (without `--apply`) change no ref, index, configuration, working-copy snapshot or operation log, run no hook, filter, textconv or project command, and touch no network without `--online`.
- **SC-006**: The tag-triggered workflow contains no build step and the release build workflow's only trigger is `workflow_dispatch` (rule and mutation tests).
- **SC-007**: Both artifact kinds validate against the record schema; the `oci-image` example carries every field the BundleDock consumers need (a statement about the fields, not that the toolbox bundle fits unchanged: see the wave-2 blockers in the open questions).
- **SC-008**: `stw`'s own contract validates (`stw release check` in `make check`).
- **SC-009**: The worked examples (contracts and records, including the two-product mirror) validate against the schemas in `internal/contracts` tests.

## Scope and Dependencies

In scope: contract (one or several products per repository, compile or fetch builds), record, schemas, six commands, the CI workflows for `stw` (bootstrap, release, reproducibility), documentation, the amendment proposal, the worked examples.

Out of scope (D5): package-manager channels and pull-model channel workflows (follow-up specs 028 and later; they depend on the record schema of this feature; Scoop additionally needs Windows targets), signing keys and notarization, changelog generation, GoReleaser and cargo-dist adapters (optional later; GoReleaser's publish-from-dist behavior and `GORELEASER_CURRENT_TAG` handling are **unverified**), consumer activation (separate and separately authorized), enforcing or sandboxing network access of fetch builds, executing readiness checks (the project's own CI produces the evidence).

Dependencies: Spec 022 (bounded `go-gh` transport; `dispatch` adds the first write there), Spec 025 (CI/PR merge-state reads inform the CI gate), Specs 020/021 (backend ownership and the Git isolation prefix). Constitution amendment (separate proposal). The sibling designs (`sheetmill` 019, `bundledock` `docs/design/bdock-release-workflow`) are the pattern for the bootstrap and the workflow security posture; migrating them to `stw release` is follow-up work in those repositories. **Unverified dependency:** WorkHarbor's own release policy has not been read; the maintainer states its CI requires pull requests to target `main`. Its wave-2 migration needs that policy read first. CrewBook's readiness criteria are the maintainer's examples, not read from its repository.

## Open questions

Closed by the owner on 2026-10-17 (D10, D11): constitution amendment (remains a proposal, undecided), `tag_commit` (omitted), toolbox-bundle blockers (wave-2 precondition), reproducibility schedule (dispatch-only), retention (draft with workflow-artifact fallback, rehearsal first), owner-only checks (accepted limit), `build` and `verify --published` (kept), tag ruleset (required precondition). Still open: (1) the constitution amendment wording and timing, which blocks WI6 to WI11; (2) WorkHarbor's release policy and CrewBook's readiness criteria, which have not been read; (3) whether GitHub's draft, attestation and publish behavior matches the unverified list, which only the rehearsal settles. See [analysis.md](analysis.md) for the decision log.
