# Contract: `scripts/release.py`

Release tooling, not a product command. It is documented here so that tests, the procedure and the record agree. `stw` itself gains no command, flag, schema or exit status.

## Commands

```text
release.py plan   --version VERSION [--record PATH] [--final] [--json]
release.py build  --version VERSION --record PATH --platforms LIST [--json]
release.py verify DIR [--version VERSION] [--rebuild] [--json]
```

| Command | Meaning | Build? |
| --- | --- | --- |
| `plan` | Dry run. Evaluates every gate it can evaluate without a distributable binary; reports others as `not-run` with the reason. | no |
| `build` | Runs the recipe for the listed platforms (subset of `darwin/arm64,linux/amd64,linux/arm64`) and writes `dist/release/<version>/`. | yes, needs A1 |
| `verify` | Checks an artifact directory: `SHA256SUMS`, manifest, build information, and smoke tests S1 to S5 for host-executable binaries. | no; `--rebuild` is a build and needs A1 |

`--final` makes `plan` also require G10 and G11 (complete record and notes). `--record` defaults to `docs/releases/<version>-readiness.md`. The script never prompts and never opens a pager. Standard output carries the result; diagnostics go to standard error.

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | All evaluated gates `pass` or `waived`; nothing required is `fail` |
| 1 | At least one gate `fail`, or a required gate `not-run` under `--final` or `build` |
| 2 | Usage error (unknown flag, invalid platform list, missing required argument); standard output stays empty |
| 3 | Refusal: `approval-missing` or `release-exists` |

Precedence: 2, then 3, then 1, then 0. A refusal runs no `go` command that links.

## Codes

All codes (`approval-missing`, `release-exists`, `go-env-unsafe`, `modules-unavailable`, `checksum-mismatch`, `buildinfo-mismatch`, `smoke-failed`, and the gate codes) use the kebab-case vocabulary. The gate codes are defined only in the spec's [gate table](../spec.md#release-gates-normative). The additional codes are:

| Code | Command | When | Status |
| --- | --- | --- | --- |
| `approval-missing` | `build`, `verify --rebuild` | no A1 entry matching commit, version and platforms | 3 |
| `release-exists` | `build` | output directory exists and is not empty, or the tag exists locally | 3 |
| `modules-unavailable` | `build`, `plan` | the offline module cache lacks a module | 1 |
| `checksum-mismatch` | `verify` | an archive or binary hash differs from `SHA256SUMS` or the manifest | 1 |
| `buildinfo-mismatch` | `verify` | build information disagrees with the manifest | 1 |
| `platform-unsupported` | all | a platform outside the three candidates | 2 |

## Recipe (`build`)

Environment is constructed, not inherited: `PATH`, `HOME`, `TMPDIR`, `GOMODCACHE`, `GOCACHE` from the caller; plus `GOENV=off` (the user's `go env -w` file is ignored), `GOTOOLCHAIN=local`, `GOFLAGS=`, `GOPROXY=off`, `GOWORK=off`, `CGO_ENABLED=0`, `GOOS`, `GOARCH`, `GOAMD64=v1`, `GOARM64=v8.0`. `GOEXPERIMENT`, `GODEBUG` and `GOFIPS140` are not passed. After construction the script reads `go env` in that environment; any pinned key that differs, or a non-empty `GOEXPERIMENT`, gives `go-env-unsafe` (gate G09, status 1) and no build runs.

Sequence: validate gates G01, G07, G09; run the stamp test with the exact linker flag; for each platform `go build -trimpath -buildvcs=false -mod=readonly -ldflags "-X github.com/wstein/sourcesteward/internal/cli.Version=<version>" -o <tmp>/stw ./cmd/stw`; write the archive with normalized metadata; write `SHA256SUMS` (archives, sorted by name, two spaces) and `release-manifest.json`. Partial output is removed if any step fails.

## Effects

| Command | Reads | Writes | Network | Processes |
| --- | --- | --- | --- | --- |
| `plan` | repository, record, module cache | Go build cache only | none | `git`, `go version/env/list/vet/mod verify/test` |
| `build` | same | `dist/release/<version>/` | none | the above plus `go build` and `tar`-free Python archiving |
| `verify` | `DIR` | none (`--rebuild`: a temporary directory and the build cache) | none | `go version -m`; host-executable `stw` runs S1 to S5 in a temporary directory with a minimal environment |

Downloaded artifacts are untrusted data: `verify` never executes a binary whose checksum failed.

## Human output

One line per gate (`G07 fail notices-incomplete  github.com/spf13/cobra v1.10.2 has no entry`), then per platform, then a final line `plan: fail (1 gate)`. In `plan`, the last lines state: `No distributable binary was linked; the effect of the linker flag and reproducibility are not verified by this dry run.`

## JSON

`--json` prints one `PlanResult` ([data-model.md](../data-model.md)). Human and JSON are rendered from the same result.

## Compatibility

Existing `stw` contracts and schemas are unchanged. `tool_schema` and `manifest_schema` start at 1 and are additive within a version.
