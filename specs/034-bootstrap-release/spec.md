# Specification: bootstrap release of `stw` (v0.1.0)

**Created**: 2026-10-22 | **Status**: implemented in source, never run (spec-light: workflow, scripts and offline tests first;
this page is the plan and the contract). Nothing here tags, builds, publishes or changes a repository setting.

`stw` has no release, no tag and no release workflow. Spec 026 (D7, D8, [bootstrap contract](../026-release-process/contracts/bootstrap.md))
says its **first** release uses the simple tag-workflow pattern already merged in `sheetmill` (spec 019) and `chartscribe`
(`specs/002-release`, which just released v0.1.0), not the future `stw release` machinery. This page adopts that pattern.
The Spec 026 machinery (`stw release build`, the manifest, the record commit, `release-build.yml`/`release-publish.yml`) is not
implemented here and, when it lands, **supersedes this workflow**: the bootstrap workflow is then deleted in the work item that
switches `stw` to self-hosting (Spec 026 WI11, FR-024).

## Authority

An owner-pushed **annotated** tag `vMAJOR.MINOR.PATCH` on a commit of `main` is the publication approval for that tag's release.
`workflow_dispatch` is a dry run: its version label needs a suffix (default `v0.0.0-dryrun`), it never publishes, and it needs the
same owner approval as any standalone build. Agents never tag, run the workflow or publish.

## Workflow `.github/workflows/release.yml`

Triggers are exactly the tag push and `workflow_dispatch`; top-level permissions are `contents: read`; one concurrency group per
ref, never cancelled. Jobs:

1. **verify** (read-only): the version has the strict grammar; the run was started and originally pushed by the repository owner;
   the tag is annotated, names the run's commit and that commit is an ancestor of `main`; **`ci.yml` has a successful push run for the
   commit** (it runs the `check` job and both Git/jj conformance lanes through `conformance.yml`; `ci-full.yml` is a label event on pull
   requests and `conformance.yml` is only called, so `ci.yml` is the one gate); no release (draft or not) exists for the tag;
   `go mod verify`, `go vet ./...`, `scripts/check-architecture.py`, `scripts/check-docs.py` and the offline workflow tests of
   `internal/releaseworkflow`. The full test suite is not repeated: the green `ci.yml` run is its evidence. Required status checks
   on `main` (an owner setting) make that run cover every merged commit.
2. **build**: per target, with `CGO_ENABLED=0`, `GOTOOLCHAIN=local`, `-mod=readonly`, `-trimpath`, `-buildvcs=false` and the Go
   version of `go.mod`: `stw` is built **twice with separate empty build caches and compared byte for byte**, then packaged
   reproducibly (`SOURCE_DATE_EPOCH` is the commit time). The version is stamped with
   `-ldflags "-X main.version=<tag>"`, verified against the real `main.version` of `cmd/stw` by a test; an unstamped build keeps
   the default `dev`. `stw version` prints `SourceSteward <tag>`.
3. **smoke**: the shipped archive runs natively on a runner of its own platform: checksum, exact members, `stw version`
   (text and `--json`), `--help`, then on a temporary Git repository without network `stw inspect` and `stw status`, which
   must leave every reference and the working tree unchanged. Commands that run Git inside the native sandbox (`tidy plan`, `sync`,
   `workspace`) are **not** in the smoke: on Linux they need bubblewrap, which the smoke job does not install (no package is installed
   in this workflow) and whose availability on the runners is unverified; the macOS smoke does not use `sandbox-exec`. The `ci.yml`
   conformance lanes cover them. A test runs the smoke with failing `bwrap`/`sandbox-exec` shims.
4. **publish** (the only job with `contents: write`, `attestations: write` and `id-token: write`; `environment: release`):
   re-checks the owner, the absence of any release (drafts included) and that the tag still names the verified commit; writes and
   checks `SHA256SUMS`; attests build provenance for the archives and `SHA256SUMS`; `gh release create --verify-tag`. The repository
   variable `STW_RELEASE_DRAFT=true` makes the release a draft for inspection before the owner publishes it.

Every action is pinned to a full commit SHA (the pins of the chartscribe workflow), checkouts use `persist-credentials: false`,
no job installs a package or fetches a tool, expressions never appear inside `run:` scripts, and no step may swallow a failure.
`internal/releaseworkflow` pins all of this as text rules with mutation probes (a dropped `persist-credentials`, an expression in
`run:`, `|| true`, an action pinned to a branch or tag, an unlisted action, an `apt-get`, a fetched tool, a missing `set -euo pipefail`,
other triggers, an archive count that differs from the matrix). It is part of `go test ./...`, so it runs in `ci.yml`; it is offline.

## Platform matrix

Decided from facts: `GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go build ./cmd/stw` with Go 1.27.2 (2026-10-22), and the documented
behaviour in [backends](../../docs/backends.md): native jj needs Linux bubblewrap or macOS `sandbox-exec`; Git and jj are the
Tier 1 backends and run as native child processes. The matrix is the set that compiles **and** is meaningful; the owner may widen it later.

| Target | Compiles | Shipped | Reason |
| --- | --- | --- | --- |
| linux/amd64 | yes | yes, native smoke | Tier 1 backends; CI runs here (check and conformance) |
| linux/arm64 | yes | yes, native smoke (`ubuntu-24.04-arm`) | same code path; native runner available |
| darwin/arm64 | yes | yes, native smoke (`macos-15`) | `sandbox-exec` path exists in `internal/workspace/native_sandbox.go` |
| darwin/amd64 | yes | yes, native smoke (`macos-15-intel`) | same |
| linux/riscv64, linux/ppc64le, linux/s390x, linux/loong64 | yes | no | no CI conformance and no native runner; `qemu-user` cannot run the native `git`/`jj`/`bwrap` children `stw` starts, so an emulated smoke would prove only `version` and `--help` (reasoning, not measured). Candidates when the owner wants them. |
| linux/arm (armv6, armv7) | **no** | no | `internal/release/build_checkout_view_linux.go` assumes 64-bit `Ctim` fields |
| windows/amd64, windows/arm64 | **no** | no | `internal/release` uses `Setpgid` and `syscall.Kill`; there is also no Windows sandbox or Windows conformance evidence |
| freebsd/, openbsd/, netbsd/ (amd64, arm64), illumos/amd64 | **no** | no | `internal/release` lacks the BSD/illumos checkout-view stamp functions; no sandbox backend |

`gh-jj` (`cmd/gh-jj`) is an optional extension and is not shipped.

## Archives and verification

`stw_<version>_<os>_<arch>.tar.gz`, one top directory with `stw`, `LICENSE`, `README.md`, `llms.txt`, `agent-guide.md` (from
`docs/agent-guide.md`) and `third_party/` (a README table and the license text of every module linked into the shipped binaries, Cobra
(Apache-2.0) and pflag (BSD-3) included; a test compares the table with `go list -deps` for linux and darwin), sorted, owner 0, normalised modes and times, plus a
`.sha256` sidecar at build time. The release carries 4 archives and `SHA256SUMS`. The binaries are not signed or notarized;
the attestation (`gh attestation verify ARCHIVE --repo wstein/sourcesteward`) and `SHA256SUMS` are the evidence. Release notes are generated from the
Conventional Commit subjects since the previous release tag (`.github/scripts/release-notes.sh`).

## Owner actions before the first tag (this change sets none)

- Environment **`release`** (Settings, Environments), no required reviewers by design (Spec 026).
- Repository variable **`STW_RELEASE_DRAFT`** (`true` for a draft first release; unset or `false` publishes directly).
- A **tag ruleset** for `v*` restricting creation, update and deletion to the owner (Spec 026 D11; see [releasing](../../docs/releasing.md)),
  and release immutability if wanted.
- Required status checks on `main` so every merged commit has the `ci.yml` run the gate reads.
- The dry run (`workflow_dispatch`) and the tag are builds and publication: each needs the owner's explicit approval.
- After the release: the BundleDock catalog entry (separate later change) and the record commit of Spec 026.
