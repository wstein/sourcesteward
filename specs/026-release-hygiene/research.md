# Research: Release hygiene for the first `stw` release

## Why this feature

`README.md` and `docs/quality.md` state that no binaries are released. The repository has `make build` (local, ignored `bin/`) and CI that builds `bin/stw` for smoke tests, but no stamped version, no recipe, no checksum convention, no notices for the whole binary, and no record of which platforms were evidenced. AGENTS.md of the umbrella requires explicit approval for any artifact build and for tagging, publication and consumer activation. This spec makes those preconditions checkable first.

## Measurements

Environment: Go 1.27.2 linux/arm64, Git 2.56.0, Python 3.13.5, run 2026-10-16 in a copy of the repository under the session scratchpad. No distributable artifact was produced. `go test` and `go vet` ran; no `go build` was run.

| Claim | Command or source | Result |
| --- | --- | --- |
| `go.mod` has a `go` line and no `toolchain` line | `head go.mod` | `go 1.27.2` only |
| Caller environment pollutes builds | `go env GOFLAGS GOTOOLCHAIN` | `-trimpath`, `local` in the author's shell |
| No cgo files in the repository | `go list -f '{{.CgoFiles}}' ./...` | none |
| `runtime/cgo` in the `stw` closure depends on `CGO_ENABLED` | `go list -deps ./cmd/stw \| grep runtime/cgo` | 1 hit with cgo on, 0 with `CGO_ENABLED=0` |
| Cross-platform type check | `GOOS/GOARCH go vet ./...` with `CGO_ENABLED=0` | passes for linux/amd64, linux/arm64, darwin/arm64 (also darwin/amd64, windows/amd64, not in scope) |
| Offline operation | `GOPROXY=off GOFLAGS=-mod=readonly go vet ./cmd/stw` | passes with the populated module cache |
| Module integrity | `go mod verify` | `all modules verified` |
| Dependency closure of the binary | `go list -deps -f '{{with .Module}}...' ./cmd/stw` | 16 third-party modules; `cmd/gh-jj` has the identical closure |
| Notices inventory | compare with `third_party/README.md` | `github.com/spf13/cobra v1.10.2` and `github.com/spf13/pflag v1.0.9` have no entry |
| Linker flag path | scratch test in package main: `go test -ldflags "-X main.version=v0.1.0"` | `version` stayed `dev` |
| | `-X github.com/wstein/sourcesteward/cmd/stw.version=v0.1.0` | `version` was `v0.1.0` |
| | `-X main.versoin=v0.1.0` (typo) | `version` stayed `dev`, no error |
| Where the variable lives | `cmd/stw/main.go`, `cmd/gh-jj/main.go` (`var version = "dev"`, package main); `internal/cli/root.go` defaults an empty parameter to `dev` | the assignment text said `internal/cli`; it is in `cmd/*` |
| `stw --version` | `internal/cli/root.go` (no cobra `Version` set), docs/commands.md | only `stw version` exists; flag not verified by running (no build) |
| Hidden commands | `rg Hidden internal cmd` | `pins` only (`internal/cli/pins.go`), not mentioned as `stw pins` in docs |
| Backend artifacts per platform | `scripts/backend-releases.json` | jj artifacts for `linux-amd64` and `darwin-arm64` only |
| CI platform | `.github/workflows/ci.yml` | `ubuntu-24.04` for `check`, `conformance` (`minimum`, `current`) and `pages` |
| Repository visibility | `gh repo view wstein/sourcesteward --json visibility` | `PUBLIC` |
| Build information shows build settings | `go version -m $(command -v gofmt)` (Go's own tool); reviewer: `go version -m` on a linked test binary | lists `-trimpath=true`, `CGO_ENABLED`, `GOARCH`, `GOOS`, `GOARM64`; under `-trimpath` there is no `-ldflags` entry (reviewer), without it the entry shows the flag text; `-buildvcs=false` leaves no `vcs` keys (reviewer) |
| Empty module cache with `GOPROXY=off` | reviewer | `go list`, `go vet`, `go mod verify` exit 1 ("module lookup disabled") and may create `GOMODCACHE/cache` |
| `git status` writes | reviewer, scratch repository | `git status --porcelain` rewrote `.git/index` after a timestamp change; `git --no-optional-locks status` did not |
| Python anchors | reviewer | `re.match(...$)` accepts `v0.1.0\n`; `re.fullmatch` does not |
| The `smill` convention | `/workspaces/Anchorage/sheetmill` `internal/cli/cli.go`, `docs/cli.md` | `Version = "dev"`, stamp `v0.1.0` with `v`, `-trimpath`, "distribution builds need separate approval", no tags |

## Not measured (unverified)

- Byte-identical binaries from two builds (needs a build; gate G12 after approval).
- That Go's linker adds an ad-hoc signature to darwin/arm64 binaries and how Gatekeeper treats a downloaded archive.
- That `-X <import path>.Version` behaves identically in `go build` as in `go test` (documented behavior; `smill` has a stamp test that builds). The reviewer measured it for `go test` on `internal/cli.Version`.
- Hosted `ubuntu-24.04-arm` and macOS arm64 runners for this repository, `sandbox-exec` and Git source builds on them.
- Archive byte stability across hosts.
- Whether a linked test binary counts as an "artifact build" for AGENTS.md; this spec treats `go test` as routine, with DD5's fallback.

## Decisions and alternatives

**Manual, scripted, approval-gated recipe (chosen).** Alternatives rejected: a tag-triggered release workflow (a push would start a build and publication, which AGENTS.md forbids as authorization); GoReleaser (new dependency and a larger surface than three platforms and one binary justify); `make release` (an easy-to-run target invites accidental builds).

**Python script (chosen).** All repository tooling is Python 3 standard library (`install-hugo.py`, `package-pages.py`), tests run under `make check` through `unittest`. A Go release tool would have to live outside `internal/cli`, which starts no processes.

**Variable in `internal/cli` (chosen).** Alternatives: keep `main.version` and use `-X main.version` (works for builds, silently ignored by tests, so no pre-build test with the exact flag); add an `init` check under a build tag `release` (changes the product binary).

**Version includes `v` (chosen).** Matches the tag and `smill`; the version schema only requires a non-empty string, so no schema change. Rejected: stamping without `v` (two spellings of one version); `git describe` derived versions (non-reproducible across checkouts).

**`-buildvcs=false` (chosen).** VCS stamping embeds a dirty flag and depends on the checkout; commit identity is carried by the tag, the manifest and the record. Rejected: `-buildvcs=true` (less reproducible across checkout kinds). Cost: the binary does not report its commit.

**No stripping (chosen).** Supportability over size; stripping does not change reproducibility.

**Unsigned, stated (chosen).** Signing and notarization need credentials and decisions about trust roots; `SHA256SUMS` guards against corruption only. Follow-ups: GitHub artifact attestations or signatures, Apple notarization.

**SBOM optional.** `syft` is available in the toolbox; the Go module list is in each binary's build information. A required SBOM would add a tool dependency to the gate.

**Platforms.** darwin/amd64 is end of life, Windows has no sandbox and is unsupported by the product. arm64 Linux needs jj artifacts that the installer lacks; the platform is dropped from the first release if WI5 does not complete.

**Evidence classes.** CI exists for linux/amd64 only. Requiring CI for all platforms would block the release on workflow work; `local-recorded` and `manual` evidence is acceptable but never labeled as CI (consistent with Spec 021).

**Waivable reproducibility.** Not verifying is acceptable if said so; claiming without checking is not.
