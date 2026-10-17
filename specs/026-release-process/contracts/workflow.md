# Contract: the release workflows of `stw` (and the pattern for other projects)

Three workflows implement the process for a repository whose contract declares them. Everything with judgment is in `stw release` (testable in Go); the YAML only orders steps, grants permissions and moves files. The workflows are implemented in WI8 (and the bootstrap workflow in WI10) and only after the constitution amendment decision (see [tasks.md](../tasks.md)). Nothing here is run by this specification.

| File | Triggers | Purpose |
| --- | --- | --- |
| `.github/workflows/release-build.yml` | `workflow_dispatch` only | gates, build once (compile or fetch), smoke, attest, draft release |
| `.github/workflows/release-publish.yml` | `push` of tags matching the product's strict tag pattern, and `workflow_dispatch` with `tag` and `record_commit` (input mode 2 only; the bootstrap release publishes itself and is checked with `verify --published`) | verify, promote images, publish the draft; **no build** |
| `.github/workflows/release-repro.yml` | `workflow_dispatch` only (a `schedule` needs the owner's explicit approval, FR-017) | build `main` twice on independent runners and compare hashes; no release |

`ci.yml` is unchanged except that `make check` also runs `stw release check` over the repository's own contract. `release-build.yml` is the contract's `ci.release_workflow`: its identity is the attestation signer. A repository with several products has one pair of workflows; the product is a dispatch input (`product`), and the tag-push trigger lists every product's tag pattern (generated from the contracts; the pattern list is a rule-tested constant).

## The `stw` binary in the workflows

The workflows need a `stw` binary on each job's runner. For `stw`'s own repository the `gates` job cross-builds the gate tool from the **main tip** (never from the commit being released, never a release artifact) for the platforms the smoke matrix needs and passes it to later jobs as a one-day workflow artifact. For other repositories the adopting workflow must obtain a pinned, checksum- and attestation-verified `stw` release instead (available after `v0.1.0`; the workflow template is a follow-up); this is explicit workflow-owned setup and the contract has no toolchain declaration in v1. Project toolchains (a compiler, Elixir, Python, a registry tool such as `bdock`) are likewise workflow-owned setup steps with pinned actions.

## release-build.yml

In the job rows below `[--product "$PRODUCT"]` means the flag is passed only when the `product` input is non-empty (an empty value is also treated as absent by the CLI).

Inputs (strings, reaching shell only through `env:`): `mode` (`commit` or `tag`), `product` (optional), `commit` (40 hex, mode `commit`), `tag` (mode `tag`), `version` (mode `commit`).

| Job | Permissions | Steps (in order) |
| --- | --- | --- |
| `gates` | `contents: read`, `actions: read` | require `github.ref == refs/heads/main`; require `github.triggering_actor` and `github.actor` equal `github.repository_owner`; checkout the `main` tip with `fetch-depth: 0` and `persist-credentials: false`; build the gate tool from that tip; resolve inputs strictly; `stw release plan ... [--product "$PRODUCT"] --online` must exit 0 (verdict `pass`); the draft part of `release-exists` is skipped here with the reason `drafts-not-checked` (no `--with-drafts`; the gate itself is `pass` or `fail` from tags and published releases) and is repeated authoritatively in `draft`; outputs `commit`, `version`, `product`, `tag_name`, `smoke_matrix` (targets with their `runner`) |
| `build` | `contents: read` (plus `packages: write` only in the image-capable variant, for the registry push) | checkout `commit` with `fetch-depth: 0` (the exact C; `persist-credentials: false`); download the gate tool; workflow-owned toolchain setup; in the image-capable variant a pinned registry-login step using `github.token` (the login writes under the runner's `HOME`, which the build inherits; the environment filter refuses token-named variables, so no token reaches the command except through that credential store); `stw release build --mode ci [--product "$PRODUCT"] --commit C --version V --out $RUNNER_TEMP/out` (compile and `fetch` builds alike); upload the out directory as one workflow artifact (retention 1 day). Holds no repository write, `id-token` or `attestations` permission |
| `smoke` (matrix over `smoke_matrix`) | `contents: read` | checkout C with `fetch-depth: 0` (the `--smoke` invocation re-evaluates the offline gates like `ci`); download the artifact and the gate tool for the runner's platform; `stw release build --mode ci --smoke TARGET [--product "$PRODUCT"] --commit C --version V --out DIR` on the target's native runner (the project's smoke script extracts what it needs) |
| `draft` | `contents: write`, `actions: read`, `id-token: write`, `attestations: write` | re-check owner; `stw release plan ... [--product "$PRODUCT"] --online --with-drafts` again with this write-capable token (it lists drafts and queries the CI runs, hence `actions: read`) and require verdict `pass`; in mode `commit` the tag must not exist on the remote; download the artifact; attest every file in the out directory (the files, `SHA256SUMS`, `build-manifest.json`) and every image index digest with the attestation action pinned by commit SHA (no project command runs in this job); `gh release create <tag name> --draft --target <C> --title ... --notes-file ...` with all files (mode `tag`: `--verify-tag`) |

If a project needs a native runner per build, a later schema revision adds `build.runner`; v1 builds on `ubuntu-24.04` and smoke-tests natively.

## release-publish.yml

Triggers: tag push of the strict pattern, or `workflow_dispatch` with inputs `tag` and `record_commit` (the record commit R of [verify.md](verify.md); owner-only; input mode 2). A step passes `[--product "$PRODUCT"]` only when the product is non-empty; an empty value is treated as absent by the CLI as well. A single job keeps verification and publication together so nothing can be replaced between them.

| Job | Permissions | Steps |
| --- | --- | --- |
| `verify-and-publish` | `contents: write`, `actions: read` (the CI-run query), `attestations: read` if the rehearsal shows `gh attestation verify` needs it (unverified); `packages: write`, `id-token: write`, `attestations: write` only in the image-capable variant | `environment: release` (no required reviewer: known limitation); require owner as actor and triggering actor; checkout the **tag commit** with full history, and **separately** the `main` tip (`persist-credentials: false`) from which the verifier `stw` is built (the tag commit is data, not code: its workflow file runs, so the tag ruleset below is mandatory); fetch the tag object explicitly (forced refspec, `--no-tags`); locate the draft through the authenticated releases list (drafts are not returned by `releases/tags/{tag}`: unverified) and record its asset ids, sizes and update times; download the assets by asset id into an empty directory; `stw release verify <tag> [[--product "$PRODUCT"]] [--record-commit R] --artifacts DIR --online --with-drafts` must exit 0; for `oci-image` assets `stw release build --mode promote --tag T [--product "$PRODUCT"] --record <record>`; in the image-capable variant a pinned registry-login step with `github.token` precedes it and attest the tagged digest; re-list the release assets and require the same ids, sizes and update times as at download (a replaced asset fails here; the residual window is seconds and, once published, an immutable release freezes the assets); require no published release and exactly one draft for the tag; require the remote tag still peels to the verified tag commit; publish with `gh release edit <tag> --draft=false --verify-tag` (or the REST `PATCH /releases/{id}` with `draft=false`, unverified) |

There is no build step, no `go build` of a product artifact, no `upload-artifact` of release files and no project build command in this file (rule test); the gate tool built from the `main` checkout is not a distributed artifact. If `promote` succeeded and `publish` then failed, the image carries its version tag without a published release: the run is repeated as the same owner (promote is idempotent for the same digest) or, if that is not possible, the version is abandoned and the next one released (tags are never re-created; a registry tag is not removed by `stw`).

## release-repro.yml

Inputs: `product` (optional; passed as `[--product "$PRODUCT"]` to every `stw release` step), `version` (a valid version for the product; for a project with `source = "file"` it must equal the file at the built commit). Two jobs on separate runner instances each check out the same `main` tip and run `stw release build --mode ci --files-only --commit <tip> --version V` (offline build gates are evaluated; release gates such as monotonicity and existing releases are not), then a third job compares the two `SHA256SUMS` files. Artifacts live one day; nothing is attested, drafted, tagged or published.

## Security posture (rules enforced by tests)

Top-level `permissions: contents: read`; write permissions only in the jobs named above; no `id-token` or `attestations` outside `draft` and `verify-and-publish`; no secrets (`GH_TOKEN` is `github.token`), except that the image-capable variants may reference exactly **one** named registry secret, only in the pinned registry-login step (decision D10 P2: the secret name is workflow-owned, the contract names no credential, and a rule test counts the references); every `uses:` pinned to a full commit SHA with a version comment and only the expected `with:` keys; `persist-credentials: false` on every checkout; `timeout-minutes` on every job; `concurrency` per workflow and ref with `cancel-in-progress: false`; no `continue-on-error`, no `if: false`, no `${{ }}` inside `run:` (inputs and contexts enter through `env:`), no `|| true`, `set +e` or `exit 0` in gate lines; `workflow_dispatch` is not reachable from `pull_request`; no other workflow starts on release tags except `ci.yml` (which has no release step). Tests: a test-only Go package `internal/releaseworkflow` parses the YAML (test-only dependency `go.yaml.in/yaml/v3`, as in the sibling repositories, not linked into `stw`), checks the rules with about 60 single-edit mutants, and pins a golden change detector. The test run builds no artifact.

## Required owner settings

A **tag ruleset restricting creation, update and deletion of the release tag patterns to the owner** (bypass list: the owner) must exist before the first release: the publish workflow runs the workflow file of the pushed tag commit, so only the ruleset keeps a non-owner from pushing a tag on a crafted commit whose workflow skips the checks. This is a repository setting (click path in the quickstart), not automation; the workflow cannot verify it.

## Multi-product consequences

The diff between C and the tag commit is repository-wide and `.stw/` is never allowlisted, so another product's contract or lock-file change merged between C and D blocks this release (`diff-not-allowlisted`); release products one at a time or re-run the build from a newer C. A new product requires, before its first tag, an edit of the publish workflow's tag triggers and of the tag ruleset (owner steps in the quickstart); until the ruleset covers the new prefix, a non-owner could create that product's tags.

## Known limitations

- The `release` environment has no required reviewer: an owner tag push that passes `verify` publishes at once. The controls are the owner-only actor checks, the annotated tag, ancestry, allowlist diff, hash and attestation verification, the refusal when a published release exists, and never re-creating a tag.
- Owner checks compare with `github.repository_owner`; in an organization they fail closed.
- Draft and publish semantics, the draft lookup and download by asset id, publishing a draft with an existing tag, and the attestation action's inputs are **unverified** until the first rehearsal (research).
- A tag deleted without a release is invisible to the monotonic check; only the tag ruleset prevents re-creation.
- Anyone with write access can edit draft assets between the build and the verification; verification against the reviewed record and the attestation detects it, and the final re-list narrows the window to seconds.
- In a public repository any signed-in GitHub user can download workflow artifacts for their one-day retention; drafts are visible only with write access.
- A fetch build contacts its declared hosts with the runner's network access; `stw` and the workflow do not restrict egress.
