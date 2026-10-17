# Contract: bootstrap and self-hosting

`stw release` cannot release itself before it exists. The first release of `stw` (and, until each repository adopts `stw release`, the releases of the other repositories) therefore uses the **existing tag-workflow pattern** that is merged in `sheetmill` (spec 019, `release.yml`) and `bundledock` (`docs/design/bdock-release-workflow`, `release.yml`): the owner pushes an annotated tag; a workflow verifies it, builds, smoke-tests natively and publishes. This contract records what `stw` takes from that pattern and what differs. It is binding for decision D7.

## What the bootstrap release workflow is

`.github/workflows/release-bootstrap.yml` in `sourcesteward` (implemented in WI10, after the amendment decision): triggers `push` of annotated tags `v[0-9]+.[0-9]+.[0-9]+` and `workflow_dispatch` dry run (version label with a mandatory suffix, never publishes). Jobs `verify`, `build`, `smoke`, `attest`, `publish`, with the security posture and verification of the sibling workflows: owner-only actor checks, annotated tag, tag commit equals the run commit and is an ancestor of `main`, a successful `ci.yml` push run for the commit, no release for the tag (drafts included), version strictly above every existing tag and release, `make check` and `go mod verify`; build matrix `linux/amd64`, `linux/arm64`, `darwin/arm64` with `CGO_ENABLED=0`, `GOTOOLCHAIN=local`, `-trimpath`, `-buildvcs=false`, `-ldflags "-X main.version=<version>"`; native smoke on `ubuntu-24.04`, `ubuntu-24.04-arm`, `macos-15` (`stw version`, `stw version --json`, `stw --help`); publish with `gh release create --verify-tag`; `environment: release` unprotected; no standing approval.

Differences from the siblings, all deliberate:

| Item | Sibling pattern | `stw` bootstrap |
| --- | --- | --- |
| Reproducibility | each target built twice in the release run | built **once** in the release run (D1); reproducibility is the separate `release-repro.yml` |
| Build runner | shell | the build job builds the gate tool from the tag commit and runs `stw release build --mode ci`, which emits a schema-valid `SHA256SUMS` and `build-manifest.json`; this is the one deliberate use of the new code in its own bootstrap, and a defect would show in the manifest and the reproducibility workflow |
| Attestation | none | a separate `attest` job (the build job holds no `id-token`) attests the archives, `SHA256SUMS` and `build-manifest.json`, so `v0.1.0` is verifiable like later releases |
| Record | none | after publication a docs commit R adds `docs/releases/v0.1.0.json`, produced by `stw release record` from the downloaded assets and the manifest; the tag precedes the record, so `v0.1.0` is verified with `stw release verify v0.1.0 --record-commit R` (the input-mode-2 path), not by a tag-push workflow; the bootstrap workflow itself does not check the record |
| Contract | none | `.stw/release.toml` exists and is linted by `make check` from WI10 (task T043), so the contract is exercised before the first release |

The bootstrap workflow is a one-time, owner-run path: tagging and the run need the owner's explicit approval for `v0.1.0` (D4). The workflow itself needs the constitution amendment like any release automation.

## Self-hosting switch

From the release after `v0.1.0`: `stw` is released with `release-build.yml` and `release-publish.yml` ([workflow.md](workflow.md)), the first release made with its own commands. WI11 (T047) switches over: the dispatch rehearsal on a throwaway version label is not possible for the publish side, so the first self-hosted release is also the first real exercise of the draft and publish path (risk, below), and `release-bootstrap.yml` is deleted in the same change that adds the first self-hosted tag procedure to the documentation.

## Other repositories

| Wave | Repository | First release | Then |
| --- | --- | --- | --- |
| 1 | SheetMill (`smill`) | the merged tag workflow (no change) | adopts `stw release` by its own spec when `stw` v0.1 is released |
| 1 | SourceSteward (`stw`) | bootstrap (this contract) | self-hosted |
| 1 | BundleDock CLI (`bdock`) | the merged tag workflow | adopts `stw release`; prefix `bdock/` (`semver` with `tag_prefix`) |
| 2 | BundleDock toolbox bundle | adopts `stw release` directly (`oci-image`, `calver`, example 2) | one product file per bundle track (`toolbox/`, `opentofu/`, ...) next to the CLI product `bdock/`; the wave-2 blockers in open question 3 (counter restart, registry secret, moving aliases) must be resolved first |
| 2 | WorkHarbor (polyglot) | adopts directly (example 3) | its own release policy is **not yet read**; the maintainer states that its CI requires pull requests to target `main` |
| 3 | CrewBook (skill pack) | adopts directly (example 4), released when the readiness criterion is met | criteria are the maintainer's examples, not read from the repository |

Each adoption is a separate change in that repository under its own instructions; none is made by this specification.

## Risks

- The first self-hosted release exercises the draft/publish path with no earlier rehearsal of `publish`; the dispatch half can be rehearsed (a draft can be deleted by hand, nothing publishes). Mitigation: rehearse the dispatch workflow and `verify` against the draft first; if the unverified draft behaviors fail, fall back to workflow artifacts (research) before relying on the path.
- The bootstrap pattern builds per tag; a wrong tag cannot be repaired (never re-created); the next version is used.
