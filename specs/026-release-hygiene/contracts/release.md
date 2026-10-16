# Contract: `scripts/release.py`

Internal release tooling, not a product command: it needs Python 3.12 or newer, prints human-readable text only (one line per gate or platform), has no JSON mode and no published schema. `stw` itself gains no command, flag, schema or exit status. The only machine-readable output is `release-manifest.json` ([data-model.md](../data-model.md)).

## Commands

```text
release.py plan   --version VERSION [--commit SHA] [--record PATH] [--notes PATH] [--final]
release.py build  --version VERSION --commit SHA --record PATH --platforms LIST [--out DIR]
release.py verify DIR [--anchor SHA256] [--version VERSION] [--no-execute]
```

| Command | Meaning | Build? |
| --- | --- | --- |
| `plan` | Dry run on the code commit, evaluated from an exact tree export. With a record, the code commit is the machine block's `code_commit` (a conflicting `--commit` is `commit-mismatch`); without a record `--commit` is required (`commit-required`). Gates follow the spec's [gate evaluation table](../spec.md#gate-evaluation-by-command-normative). | no |
| `build` | Refuses (exit 1) unless G02, G03 and G04 for every requested platform are recorded `pass` in the committed record, then runs the recipe for the listed platforms (subset of `darwin/arm64,linux/amd64,linux/arm64`) from an export of the code commit and writes `dist/release/<version>/` or `--out DIR` (under `dist/`). It prints the SHA-256 of `SHA256SUMS`, which the maintainer records as the trust anchor. A second build into another `--out` directory provides the G12 comparison. | yes, needs A1 |
| `verify` | Checks an artifact directory: the anchor, `SHA256SUMS`, manifest, build information, and smoke tests S1 to S5 for host-executable binaries. `--no-execute` skips execution and is the only mode without an anchor. | no |

`--final` is the only mode that reads the record in full, and it is deliberately small: it evaluates G01, G09, G11 and the A1 and A2 quote checks and requires `status: ready`; the reviewer checklist of the record contract covers the rest. It runs after A2 is recorded and before the tag is created, on branch `codex/release-<version>` (rooted at the code commit; every record commit lives there; merging it to `main` after the final record needs its own user authorization). In a colocated jj checkout `HEAD` may be detached; the check is that `refs/heads/codex/release-<version>` resolves to `HEAD`, and how the branch or bookmark is created is left to the maintainer, as for the tag. `--record` defaults to `docs/releases/<version>-readiness.md` and `--notes` to `docs/releases/<version>.md`, both read from the checkout. If the record does not exist, the gates that need it are `not-run` with reason `no record`. `check-docs.py` and the docs, README and `llms.txt` scans run on the export; the notes are scanned from the checkout. Under `--final` the record commit is `HEAD`: it must descend from the code commit (`git merge-base --is-ancestor`), `git diff-tree` between them must touch only `docs/releases/`, and the record and notes are read from `HEAD`'s tree; working-tree files that differ give `record-incomplete`. Further record commits (A3, A4 before their steps; post-release verification after publication) are allowed on the same branch; no script mode requires them. The `quote` fields of the machine block are canonical for every command. The script never tags, pushes, publishes or activates, never prompts and never opens a pager. Standard output carries the report; diagnostics go to standard error.

## Exit status and codes

Exit statuses and every code are defined once in the spec's [code table](../spec.md#codes-normative). Summary: 0 no gate failed; 1 gate failure; 2 usage error (standard output empty); 3 refusal. Precedence: 2, then 3, then 1, then 0. A refusal runs no `go` command that links.

## Record machine block

The record contains one fenced `json` block under the heading `Machine record`; `plan --final` parses only that block and ignores all prose. Its fields are defined in the [readiness record contract](readiness-record.md). The first fenced `json` block under that heading is read, at most 256 KiB, and unknown keys are ignored.

## Recipe (`build`)

Environment is constructed, not inherited: `PATH` (resolved to absolute `go`, `git`, `python3`), `HOME` (a temporary directory), `TMPDIR`, `GOMODCACHE` (absolute, from the caller's `go env` before isolation), `GOCACHE` (`build`: a fresh empty temporary directory; `plan`: the caller's absolute `GOCACHE`); plus `GOENV=off` (the user's `go env -w` file is ignored), `GOTOOLCHAIN=local`, `GOFLAGS=`, `GOPROXY=off`, `GOWORK=off`, `CGO_ENABLED=0`, `GOOS`, `GOARCH`, `GOAMD64=v1`, `GOARM64=v8.0`. `GOEXPERIMENT`, `GODEBUG` and `GOFIPS140` are not passed. `STW_RELEASE_VERSION` is set only in the environment of the stamp-test process. Every process started from the export (`go`, `python3 scripts/check-docs.py`) runs in this constructed environment. After construction the script reads `go env` in that environment; any pinned key that differs, or a non-empty `GOEXPERIMENT`, gives `go-env-unsafe` (G09, status 1) and no build runs. Git runs with `GIT_NO_REPLACE_OBJECTS=1`, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `--no-optional-locks` and `-c core.fsmonitor=false`, and never with `-c safe.directory`; a checkout Git refuses as foreign-owned and a shallow clone give `unsupported-checkout`.

Sequence: resolve the code commit; check A1 (G13) and `release-exists`, refusing with exit 3 before any `go` command; export the tree (`git ls-tree -r -z`, `git cat-file --batch`, regular files only, `O_EXCL`; refusals as in the spec's Source row) into a fresh temporary directory; evaluate G01 to G07 and G09 on the export (G02 and G03 from the record) and set G13 from A1; run the stamp test with the exact linker flag; for each platform `go build -trimpath -buildvcs=false -mod=readonly -ldflags "-X github.com/wstein/sourcesteward/internal/cli.Version=<version>" -o <tmp>/stw ./cmd/stw` in the export; write the archive with normalized metadata (sorted, owner 0, `SOURCE_DATE_EPOCH`, fixed modes, gzip mtime 0) from exact members only; write `SHA256SUMS` (the archives and `release-manifest.json`, sorted by name, two spaces) and `release-manifest.json`. Partial output is removed if any step fails. `git status` and `git archive` are never run.

## Verification details

File names are checked against `stw_<version>_<os>_<arch>.tar.gz`. Extraction accepts only the member `stw`, `LICENSE`, `README` and `third_party/` entries as regular files or directories, using the `tarfile` `data` filter and a 128 MiB size limit, into a new temporary directory. Smoke tests run with an allowlisted environment (`PATH`, `HOME` and `TMPDIR` in a temporary directory, `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`; no `GH_TOKEN`, `GITHUB_TOKEN`, `SSH_AUTH_SOCK` or other inherited variable). A binary whose checksum failed is never executed. Whether a binary is host-executable is decided from the manifest platform and the host (`uname`), never from the binary. The Mac mini flow does not run `release.py`; it uses the shell commands of the quickstart and is recorded by hand.

## Effects

| Command | Reads | Writes | Network | Processes |
| --- | --- | --- | --- | --- |
| `plan` | repository objects (Git plumbing), record, module cache | temporary directories (export, stamp-test binary), the Go build cache; a failing lookup may create `GOMODCACHE/cache` lock files (measured) | none | `git rev-parse/ls-tree/cat-file/merge-base/diff-tree/tag --list`; `python3 scripts/check-docs.py` in the export; `go version/env/list/vet/mod verify/test` |
| `build` | same | the output directory, temporary directories | none | the above plus `go build` |
| `verify` | `DIR` | a temporary directory (extraction) | none | `go version -m`; host-executable `stw` runs S1 to S5 |

The working tree and `.git` of the checkout are not modified; a real-run snapshot test asserts that for `plan` on a fixture repository.

## Human output

One line per gate (`G07 fail notices-incomplete  github.com/spf13/cobra v1.10.2 has no entry`), then per platform, then a final line `plan: fail (1 gate)`. In `plan`, the last lines state: `No distributable binary was linked; the effect of the linker flag and reproducibility are not verified by this dry run.`

## Compatibility

Existing `stw` contracts and schemas are unchanged. `manifest_schema` starts at 1 and is additive within a version.
