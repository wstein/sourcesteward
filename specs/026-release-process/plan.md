# Implementation Plan: Release process v2

**Branch**: `codex/026-release-process-v2` | **Date**: 2026-10-17 | **Spec**: [spec.md](spec.md)

## Summary

Add `stw release` (`check`, `plan`, `verify`, `record`, `dispatch`, `build`) and the CI workflows that implement build-once-then-promote for any language. A new package `internal/release` holds the contract parser and validators, version schemes, the gate engine, the allowlist diff, the canonical record encoder, the dispatch planner and the (CI-only) build runner; `internal/cli/release.go` only parses flags and renders results. Backends are reached through the existing ownership detection, Git isolation prefix and read-only jj runner; GitHub reads and the single dispatch write go through `internal/hosting`. Five contract-validated worked examples cover a Go CLI, an OCI bundle, a polyglot project, a skill pack and a two-product source mirror with fetch builds. A repository may hold several products (one contract file each, per-product tag grammar and monotonicity). `stw` is released first by a bootstrap tag workflow and is self-hosted from the next release.

## Technical Context

**Language/Version**: Go 1.27.2; Git 2.46 or newer (existing minimum and current lanes); `gh` 2.102.0 or newer for attestation verification and hosting (existing hosting requirement); jj as pinned in `scripts/backend-releases.json`.
**Primary Dependencies**: Cobra v1.10.2, the standard library, the existing `go-gh/v2`. No new runtime dependency for parsing or validation (strict TOML profile parser is hand-written, DD3; validators are hand-written, DD4). The scoped [record-writer design addendum](record-writer-design.md) incorporates the already pinned `golang.org/x/sys v0.47.0` only for descriptor-relative no-follow filesystem operations in the exact Linux/Darwin writer adapter, subject to its independent design review gate and closed import/call restrictions; no new module version or broad syscall permission is introduced. One **test-only** dependency, a YAML library, for the workflow rule tests (WI8), as the sibling repositories do; it is justified, pinned and not linked into `stw`.
**Storage**: None locally. `record --write` creates one file the caller commits.
**Testing**: Go table tests with real temporary Git repositories and jj workspaces; the contract and record corpus (valid, invalid and the five worked examples) validated by the existing test-only JSON Schema library and by the Go validators with identical verdicts; a script test comparing the TOML parser with Python `tomllib`; in-process GitHub fakes via the hosting transport seam; effect sentinels; workflow rule and mutation tests; Python conformance scripts in the existing lanes.
**Target Platform**: Code for linux amd64/arm64 and darwin arm64. CI evidence exists for `ubuntu-24.04` amd64 only; other results are local evidence.
**Project Type**: CLI plus CI workflows plus schemas.
**Performance Goals and limits**: the spec's [limits table](spec.md#limits-normative).
**Scale/Scope**: one repository, up to 16 builds, 64 targets, 64 assets, 20,000 diff entries.

## Constitution Check

Pre-research: needs the amendment (below; applied by PR #58 once merged). Post-design: PASS for WI1 to WI5 without the amendment; WI6 and later are blocked by it.

- **I. Shared contract**: every command prints human output and, with `--json`, one result from the same data; no prompt, editor or pager; versioned schemas; exit statuses in one table.
- **II. Evidence before actions**: `plan` and `verify` explain and prove; `dispatch` is plan then apply with a stateless digest, revalidated, stale plans refused; `record --write` refuses overwrite; a plan is not authorization (stated in output). Uncertain proof refuses (`not-run` is never `pass`).
- **III. Ownership and recovery**: no stash, reset, branch change or publication by `stw`; the only local mutation is one new file; remote effects (dispatch run, draft, tag promotion) are disclosed with their recovery limits (a dispatched run can be cancelled and a draft deleted by hand; a published release and a promoted tag cannot be undone by `stw`; tags are never re-created).
- **IV. Specify and verify**: this specification and its analysis precede code; every work item starts with failing tests; hostile fixtures (tree entries, tags, config text, record text, API text), concurrency (main moves between plan and apply), interruption and limits are specified; read-only claims have effect sentinels including jj working-copy and operation-log sentinels; conformance on the existing Git/jj lanes. **Gap**: jj is not installed where this was written, so every jj statement is unverified until WI1 conformance runs; the capability table says so.
- **V. Tier 1 peers**: capability per backend in the spec table; jj uses its native read-only runner; unsupported capability (`verify`/`plan <tag>` annotation check, `local-verify`) refuses with `unsupported-backend` and never falls back to raw Git; no equivalence claimed.
- **Scope and Constraints**: "push/merge/release automation ... require later specifications" and "Future ... releases and external mutations require authorization in their task context". This specification is the later specification, but automation of release steps (the CI build, draft, publish, promote) also changes what the constitution scope lists as roadmap. **The amendment was decided by the owner and is applied by pull request sourcesteward#58** ([constitution-amendment-proposal.md](constitution-amendment-proposal.md)); until that PR is merged, only the read-only work items (WI1 to WI5; WI5 adds disclosed read-only GitHub reads) may be implemented. No constitutional text is edited by this feature.
- **Umbrella AGENTS.md**: builds, tags, publication and activation need explicit authorization each time; schedules, bots, pushes and merges authorize nothing. The plan keeps `dispatch --apply`, `build --mode local-verify`, the tag push and consumer activation as separately authorized steps and ships no schedule.

## Project Structure

```text
internal/release/contract.go         # strict TOML profile parser, JSON loader, model types, limits
internal/release/product.go          # product discovery, selection by flag or tag grammar, overlap check
internal/release/validate.go         # semantic validation, placeholders, findings
internal/release/version.go          # semver, calver, pattern: validity, tags, monotonicity set, ordering
internal/release/allowlist.go        # glob matching, overlap rules, diff classification
internal/release/gates.go            # gate engine, verdict, offline/online evidence interfaces
internal/release/vcs_git.go          # isolated Git reads (refs, trees, blobs, tag objects); streaming diff-tree parser
internal/release/vcs_jj.go           # jj read-only runner reads (commit plan, record, check)
internal/release/record.go           # record, manifest, OCI result types, canonical encoder
internal/release/plan.go             # plan and verify orchestration, results
internal/release/artifacts.go        # bounded streaming hashing, directory checks
internal/release/attest.go           # bounded `gh attestation verify` process
internal/release/dispatch.go         # dispatch plan, plan_id, apply
internal/release/build.go            # CI build, smoke, local-verify export, promote; command runner
internal/release/*_test.go           # temporary repositories, fixtures, sentinels
internal/hosting/release.go          # read endpoints (runs, releases, tags, main ref, repository rulesets for the advisory) and the dispatch POST
internal/cli/release.go              # cobra commands, rendering, exit statuses
schemas/v1/release-*.schema.json     # ten schemas
internal/contracts/release_*_test.go # schema fixture tests and example validation
.stw/release.toml                    # stw's own contract
.github/workflows/release-bootstrap.yml, release-build.yml, release-publish.yml, release-repro.yml
internal/releaseworkflow/            # workflow rule and mutation tests (test-only)
docs/release.md, docs/releasing.md, docs/json-contracts.md, docs/commands.md, docs/backends.md
scripts/smoke-release.py, scripts/test_release_smoke.py
```

`scripts/check-architecture.py` is extended: `internal/cli` may import `internal/release`; `internal/release` may start processes only in `vcs_*.go`, `attest.go` and `build.go` (a new rule); `internal/hosting` remains the only package with network code.

## Design Decisions

Decisions DD1 to DD14, DD34 and DD35 are in [research.md](research.md#decisions-and-rejected-alternatives). Implementation decisions:

- **DD15 Contract read from the evaluated commit.** `plan` and `verify` read `.stw/release.*` from the commit's tree (blob via `cat-file`), never the working tree, so the answer is about the commit that would be built or was built. `check` reads the working tree by default.
- **DD16 Gate engine shape.** Gates are pure functions over an evidence snapshot gathered once (`Local` and `Online` evidence interfaces), so unit tests inject fixtures and in-process fakes. Each gate returns `Gate`; the verdict is derived in one place.
- **DD17 Allowlist classification is separate from matching.** Pattern matching (`*`, `**`) is pure and fuzzed; classification applies the regular-file/mode/status rules; both are table-tested with hostile names (newline, leading dash, non-UTF-8, case-only differences, `..`).
- **DD18 Git isolation prefix reused.** The existing `runGit` isolation (replace objects off, fsmonitor off, no optional locks, no prompts, capped output) is extended with `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null` for release reads, so the user's configuration cannot change a gate; a repository with `info/grafts` (which fakes ancestry even with replace objects disabled, measured) or a shallow clone makes the ancestry gates `not-run`; filter and textconv are never reached because only `--raw` tree diffs and `cat-file` blobs are used.
- **DD19 Online reads through hosting.** New read functions in `internal/hosting/release.go` reuse the transport, bounds and redaction (10 pages, 1,000 records, 1 MiB bodies, 30 s); results are typed, never raw JSON.
- **DD20 Dispatch write is a single fixed call.** The transport gains `postFixed(ctx, endpoint, body)` that accepts only the dispatch endpoint pattern and refuses redirects and compressed responses like the reads; a test proves any other method or path fails closed.
- **DD21 `gh attestation verify` as a bounded process.** No sigstore library is linked; the process runs with the hosting layer's filtered environment from an empty temporary directory, 60 s deadline, 1 MiB output cap, JSON parsed strictly; its exit status and output shape are part of the unverified list until rehearsal; `--bundle` offline verification is the test seam.
- **DD22 Build runner.** `build.go` constructs the environment, substitutes placeholders (closed set, no shell), runs with process-group kill on timeout or cancellation, and bounds output. A fake runner interface is the unit-test seam; the CI is the only place real project commands run, and no test runs one against a real product build without authorization.
- **DD23 One sentinel suite.** A shared test helper snapshots index bytes and mtime, refs, HEAD, configuration, reflogs, the working tree, the jj operation log and working-copy commit, then runs each read-only command and compares; hook, filter, textconv and network sentinels are planted in the fixture repositories.
- **DD24 Output escaping.** One function escapes control characters for every repository, tag, API and process text; used by human and JSON renderers alike.
- **DD25 Reproducibility workflow.** `release-repro.yml` runs `build --mode ci --files-only` twice on independent jobs with a dispatch-input version valid for the product and compares `SHA256SUMS`; offline build gates apply, release gates (monotonicity, existing releases) do not; it is dispatch-only; a `schedule` is a separate owner decision.
- **DD26 Notices and stamping for `stw`'s own release.** `third_party/README.md` gains the missing Cobra and pflag entries (WI10 prerequisite); the version is stamped with `-X main.version` (the existing variable) by `scripts/release-package.sh`; a Go test skipped unless `STW_RELEASE_VERSION` is set fails when the binary's reported version differs, run by the script before packaging.
- **DD28 Records are recomputed, not trusted.** `verify` recomputes repository (hosting), signer workflow (`ci.release_workflow` at C), project, product, `url_template` and image `tags` and requires equality with the record; attestation verification takes repository and signer from those values and pins `--source-ref refs/heads/main` and `--deny-self-hosted-runners` (the record path is allowlisted and editable in D).
- **DD29 Products.** Product discovery lives in `product.go`; every command takes the product from `--product` or from the tag grammar; the monotonicity set is the product's grammar, which removes the track notion; literal tag prefixes must not be prefix-related.
- **DD30 Fetch builds are ordinary builds with a declared effect.** `network` and `network_hosts` are disclosed by `plan`, `dispatch` and the attested manifest; `stw` neither enforces nor sandboxes network access; `lock_files` are hashed build inputs; `check` requires them when reproducibility is `required`.
- **DD31 Streaming diff.** The raw tree diff is read by a streaming parser with an entry and byte limit (20,000 entries, 4 MiB), modelled on `inventory_stream.go`, because the shared Git runner caps output at 64 KiB.
- **DD32 Verification and publication in one job.** The publish workflow verifies, promotes and publishes in a single job, re-lists the draft's asset ids and sizes just before publishing, and builds the verifier from the `main` tip (the tag commit is data); a tag ruleset restricting creation to the owner is a required setting.
- **DD33 Existing-tag publication.** `verify --record-commit R` plus a `workflow_dispatch` publish path serves input mode 2; the bootstrap release publishes itself and is checked afterward with `verify --published`.
- **DD36 Wave-2 preconditions and the ruleset advisory.** `pattern` products gain `monotonicity` (`strict` or `non-decreasing`, counters outside `order`); a `ghcr.io` image-capable variant references no secret (`github.token`) and a non-GitHub-registry variant references exactly one environment-scoped secret in the login step only, per the workflow credential model; the record forbids aliases (`tags == [version]`); `plan --online` reports the tag-ruleset state as an advisory that never changes the verdict (decisions D10 and D11).
- **DD27 Schedule of work.** WI1 to WI5 are source-only and ship under the existing authorization model; WI6 to WI11 add a remote write, project command execution, workflows and a release, and wait for the constitution decision; WI10 and WI11 need separate owner approvals for any rehearsal, tag or run.

## Risks

- Unverified GitHub behavior ([research](research.md#unverified-github-behavior)); mitigation: rehearsal checklist in the quickstart, fallback to workflow artifacts, and all draft-specific logic isolated in `draft`/`publish` steps.
- jj capability unverified; mitigation: conformance work in WI1 and WI3 before any jj claim becomes documentation.
- The hand-written TOML profile parser could drift from TOML; mitigation: differential test against `tomllib` on a generated corpus and a fuzz target.
- Hosting write path widens a read-only package; mitigation: DD20 and an architecture check.
- Allowlist mis-declaration by a project; mitigation: `check` overlap rules, plan output showing matched files, the reproducibility job.
- Scope growth: ten schemas and six commands; mitigation: work items stack with `make check` green at each and the cut order recorded in tasks (only non-decision items are cuttable without the owner).
- Wave-2 fit: the toolbox bundle's counter restart, registry credential and moving aliases are handled by decision D10 (P1 `non-decreasing` pattern product, P2 per-variant credential model, P3 no aliases in the record); adoption in the BundleDock repository still has to show all three, and the non-GitHub-registry variant keeps a documented credential exposure to project commands.
