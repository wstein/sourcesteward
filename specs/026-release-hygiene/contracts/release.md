# Contract: `scripts/release.py`

Release tooling, not a product command. It is documented here so that tests, the procedure and the record agree. `stw` itself gains no command, flag, schema or exit status.

## Commands

```text
release.py plan   --version VERSION [--commit SHA] [--record PATH] [--final] [--json]
release.py build  --version VERSION --commit SHA --record PATH --platforms LIST [--json]
release.py verify DIR --anchor SHA256 [--version VERSION] [--no-execute] [--rebuild --commit SHA --record PATH] [--json]
```

| Command | Meaning | Build? |
| --- | --- | --- |
| `plan` | Dry run on the code commit (`--commit`, default `HEAD`), evaluated from a `git archive` export. Gates are evaluated as in the spec's [gate evaluation table](../spec.md#gate-evaluation-by-command-normative). | no |
| `build` | Runs the recipe for the listed platforms (subset of `darwin/arm64,linux/amd64,linux/arm64`) from an export of the code commit and writes `dist/release/<version>/`. It prints the SHA-256 of `SHA256SUMS`, which the maintainer records as the trust anchor. | yes, needs A1 |
| `verify` | Checks an artifact directory: the anchor, `SHA256SUMS`, manifest, build information, and smoke tests S1 to S5 for host-executable binaries. `--no-execute` skips execution and is the only mode without a trusted anchor value. `--rebuild` is a build (G12) and needs A1. | no; `--rebuild` yes |

`--final` makes `plan` evaluate G10 to G14 from the record. `--record` defaults to `docs/releases/<version>-readiness.md` in the checkout; the record is data read from the checkout and need not be part of the code commit. The script never tags, pushes, publishes or activates, never prompts and never opens a pager. Standard output carries the result; diagnostics go to standard error.

## Exit status and codes

Exit statuses and every code are defined once in the spec's [code table](../spec.md#codes-normative). Summary: 0 no gate failed; 1 gate failure; 2 usage error (standard output empty, no JSON); 3 refusal. Precedence: 2, then 3, then 1, then 0. A refusal runs no `go` command that links.

## Recipe (`build`)

Environment is constructed, not inherited: `PATH` (resolved to absolute `go`, `git`, `python3`), `HOME` (a temporary directory), `TMPDIR`, `GOMODCACHE` (absolute, from the caller's `go env` before isolation), `GOCACHE` (fresh empty temporary directory); plus `GOENV=off`, `GOTOOLCHAIN=local`, `GOFLAGS=`, `GOPROXY=off`, `GOWORK=off`, `CGO_ENABLED=0`, `GOOS`, `GOARCH`, `GOAMD64=v1`, `GOARM64=v8.0`. `GOEXPERIMENT`, `GODEBUG` and `GOFIPS140` are not passed. After construction the script reads `go env` in that environment; any pinned key that differs, or a non-empty `GOEXPERIMENT`, gives `go-env-unsafe` (G09, status 1) and no build runs. Git runs as `git --no-optional-locks -c core.fsmonitor=false` with `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null`.

Sequence: resolve the code commit; export it with `git archive` into a fresh temporary directory; evaluate G01, G04 to G07, G09, G13 (A1) on the export; run the stamp test with the exact linker flag; for each platform `go build -trimpath -buildvcs=false -mod=readonly -ldflags "-X github.com/wstein/sourcesteward/internal/cli.Version=<version>" -o <tmp>/stw ./cmd/stw` in the export; write the archive with normalized metadata (sorted, owner 0, `SOURCE_DATE_EPOCH`, fixed modes, gzip mtime 0) from exact members only; write `SHA256SUMS` (archives, sorted by name, two spaces) and `release-manifest.json`. Partial output is removed if any step fails. `git status` is never run.

## Verification details

File names are checked against `stw_<version>_<os>_<arch>.tar.gz`. Extraction takes only the member `stw` (plus `LICENSE`, `README`, `third_party/` for content checks), as regular files, using the `tarfile` `data` filter and a 128 MiB size limit, into a new temporary directory. Smoke tests run with an allowlisted environment (`PATH`, `HOME` and `TMPDIR` in a temporary directory; no `GH_TOKEN`, `GITHUB_TOKEN`, `SSH_AUTH_SOCK` or other inherited variable). A binary whose checksum failed is never executed. Whether a binary is host-executable is decided from the manifest platform and the host (`uname`), never from the binary.

## Effects

| Command | Reads | Writes | Network | Processes |
| --- | --- | --- | --- | --- |
| `plan` | repository objects (via Git plumbing), record, module cache | temporary directories (export, stamp test binary), the Go build cache; a failing lookup may create `GOMODCACHE/cache` lock files (measured) | none | `git rev-parse/cat-file/archive/tag --list`; `go version/env/list/vet/mod verify/test` |
| `build` | same | `dist/release/<version>/`, temporary directories | none | the above plus `go build` |
| `verify` | `DIR` | a temporary directory (extraction) | none | `go version -m`; host-executable `stw` runs S1 to S5 |

The working tree and `.git` of the checkout are not modified; a real-run snapshot test asserts that for `plan` on a fixture repository.

## Human output

One line per gate (`G07 fail notices-incomplete  github.com/spf13/cobra v1.10.2 has no entry`), then per platform, then a final line `plan: fail (1 gate)`. In `plan`, the last lines state: `No distributable binary was linked; the effect of the linker flag and reproducibility are not verified by this dry run.`

## JSON

`--json` prints one `PlanResult` ([data-model.md](../data-model.md)). Human and JSON are rendered from the same result.

## Compatibility

Existing `stw` contracts and schemas are unchanged. `tool_schema` and `manifest_schema` start at 1 and are additive within a version.
