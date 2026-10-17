# Contract: the release workflows of `stw` (and the pattern for other projects)

Three workflows implement the process for a repository whose contract declares them. Everything with judgment is in `stw release` (testable in Go); the YAML only orders steps, grants permissions and moves files. The workflows are implemented in WI8 (and the bootstrap workflow in WI10) and only after the constitution amendment decision (see [tasks.md](../tasks.md)). Nothing here is run by this specification.

| File | Triggers | Purpose |
| --- | --- | --- |
| `.github/workflows/release-build.yml` | `workflow_dispatch` only | gates, build once, smoke, attest, draft release |
| `.github/workflows/release-publish.yml` | `push` of tags matching the scheme's strict pattern (`v[0-9]+.[0-9]+.[0-9]+` for `stw`) | verify and publish the draft; promote images; **no build** |
| `.github/workflows/release-repro.yml` | `workflow_dispatch` only (a `schedule` needs the owner's explicit approval, FR-017) | build `main` twice on independent runners and compare hashes; no release |

`ci.yml` is unchanged except for the `make check` addition that runs `stw release check` over the repository's own contract. `release-build.yml` is the contract's `ci.release_workflow`: its identity is the attestation signer.

## release-build.yml

Inputs (strings, reaching shell only through `env:`): `mode` (`commit` or `tag`), `commit` (40 hex, mode `commit`), `tag` (mode `tag`), `version` (mode `commit`).

| Job | Permissions | Steps (in order) |
| --- | --- | --- |
| `gates` | `contents: read`, `actions: read` | require `github.ref == refs/heads/main`; require `github.triggering_actor` and `github.actor` equal `github.repository_owner`; checkout `main` tip (full history, `persist-credentials: false`); build the `stw` gate tool from that tip (a CI-local tool for this run, not a release artifact); resolve inputs strictly; `stw release plan ... --online --json` must exit 0 (verdict `pass`) for the commit (or `--for build` for the tag); outputs `commit`, `version`, `tag_name`, `smoke_matrix` (from `would_build` and the targets' `runner`) |
| `build` | `contents: read` (plus `packages: write` only in the image-capable variant) | checkout `commit` (the exact C; `persist-credentials: false`); the project's toolchain setup steps (pinned actions; workflow-owned, not in the contract); `stw release build --mode ci --commit C --version V --out $RUNNER_TEMP/out`; upload the out directory as one workflow artifact (retention 1 day) |
| `smoke` (matrix over `smoke_matrix`) | `contents: read` | download the artifact; `stw release build --mode ci --smoke TARGET` on the target's native runner |
| `draft` | `contents: write`, `id-token: write`, `attestations: write` | re-check owner; list all releases (drafts included) and fail if one exists for the tag name; in mode `commit` fail if the tag exists on the remote; download the artifact; attest every file in the out directory (the files, `SHA256SUMS`, `build-manifest.json`) and every image index digest with the attestation action pinned by commit SHA; `gh release create <tag name> --draft --target <C> --title ... --notes-file ...` with all files (mode `tag`: `--verify-tag`) |

Notes: the build job never has write permission or an attestation permission; the draft job runs no project command. If a project needs a native runner per build, a later schema revision adds `build.runner`; v1 builds on `ubuntu-24.04` and smoke-tests natively.

## release-publish.yml

| Job | Permissions | Steps |
| --- | --- | --- |
| `verify` | `contents: read`, `actions: read` | require owner as actor and triggering actor; checkout the tag commit (full history); fetch the tag object explicitly (forced refspec, `--no-tags`) and `main`; build `stw` from the checkout; locate the draft through the releases list (drafts are not returned by `releases/tags/{tag}`: unverified) and download its assets by asset id into an empty directory; `stw release verify <tag> --artifacts DIR --online` must exit 0 |
| `promote` (only if the contract has `oci-image` targets) | `contents: read`, `packages: write`, `id-token: write`, `attestations: write` | `stw release build --mode promote --tag T --record <record at the tag commit>`; attest the tagged image digest |
| `publish` | `contents: write` | `environment: release` (no required reviewer: known limitation); re-check owner; re-list releases and require exactly one draft for the tag and no published release; require the remote tag still peels to the verified tag commit; publish the draft (`PATCH /releases/{id}` with `draft=false`, using the existing tag) |

There is no build step, no `go build` of a product artifact, no `upload-artifact` of release files and no project build command in this file (rule test). The gate tool built from the checkout is not a distributed artifact.

## Security posture (rules enforced by tests)

Top-level `permissions: contents: read`; write permissions only in the jobs named above; no `id-token` or `attestations` outside `draft` and `promote`; no secrets (`GH_TOKEN` is `github.token`); every `uses:` pinned to a full commit SHA with a version comment and only the expected `with:` keys; `persist-credentials: false` on every checkout; `timeout-minutes` on every job; `concurrency` per workflow and ref with `cancel-in-progress: false`; no `continue-on-error`, no `if: false`, no `${{ }}` inside `run:` (inputs and contexts enter through `env:`), no `|| true`, `set +e` or `exit 0` in gate lines; `workflow_dispatch` is not reachable from `pull_request`; no other workflow starts on release tags except `ci.yml` (which has no release step). Tests: a test-only Go package `internal/releaseworkflow` parses the YAML (test-only dependency `go.yaml.in/yaml/v3`, as in the sibling repositories, not linked into `stw`), checks the rules with about 60 single-edit mutants, and pins a golden change detector. The test run builds no artifact.

## Known limitations

- The `release` environment has no required reviewer: an owner tag push that passes `verify` publishes at once. The controls are the owner-only actor checks, the annotated tag, ancestry, allowlist diff, hash and attestation verification, the refusal when a published release exists, and never re-creating a tag.
- Owner checks compare with `github.repository_owner`; in an organization they fail closed.
- Draft and publish semantics, the draft lookup and download by asset id, `PATCH draft=false` with an existing tag, and the attestation action's inputs are **unverified** until the first rehearsal (research).
- A tag deleted without a release is invisible to the monotonic check; only the tag ruleset prevents re-creation.
- The gate tool is built from the repository being released (bootstrap trust); a pinned, attested `stw` download is the later hardening for other projects.
- In a public repository any signed-in GitHub user can download workflow artifacts for their one-day retention; drafts are visible only with write access.
