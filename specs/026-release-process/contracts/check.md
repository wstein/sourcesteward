# Contract: `stw release check`

## Command

```text
stw release check [PATH] [--product NAME] [--commit SHA] [--json]
```

`PATH` defaults to the current directory (any directory inside the repository). Without `--commit` the command lints the contract file in the working tree; with `--commit` it reads the blob at `.stw/release.toml` or `.stw/release.json` in that commit's tree (for example C before a dispatch). It accepts no other flags, never prompts, opens an editor or pager, writes nothing and starts no project command. Without `--product` it lints every product file of the repository (see [config.md](config.md#products-and-files-normative)), reports `product-tag-overlap` across them and `config-ambiguous` for mixed layouts; with it, only that product.

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | the contract is valid |
| 1 | refusal or operational failure (`config-missing`, `unsupported-backend`, `ambiguous-backend`, `not-repository`, `bare-repository`, `commit-unresolved`, `product-required`, `product-unknown`, `canceled`) |
| 2 | usage error |
| 3 | one or more check findings |

Precedence 2, 1, 3, 0. All findings are reported (up to 100, then `limit-exceeded`), ordered by JSON pointer.

## Backend capability

Per the spec's [capability table](../spec.md#backend-capability-normative): the working-tree form needs only ownership detection (for the repository root); `--commit` uses `git cat-file` under the isolated prefix in Git and the existing jj read-only runner in jj; GitButler-managed repositories lint the working-tree file only; `--commit` refuses `unsupported-backend`.

## Human output

```text
SourceSteward release check: /path/to/repo (git)
effects: read-only, no network, no project command
contract: .stw/release.toml (schema 1, scheme semver, 3 targets, 1 build)
findings: 1 error
  error  /allowlist/paths/2  allowlist-overlaps-input: pattern "scripts/**" matches the build input "scripts/release-package.sh"
```

## JSON

One object per invocation: `schema_version`, `command: "release-check"`, `status` (`ok`, `findings`, `refused`, `error`), `backend`, `input_path`, `source` (`worktree` or `commit`, with `commit` when given), `contract` (the normalized model or `null`), `findings` (array of `{code, severity, pointer, message}`, severity `error` or `info`), `summary` (`errors`, `infos`), `boundary` (`effects` with `network`, `writes`, `processes` and `project_commands`, `limits`, `complete`), `error` (`{code, message}` or `null`). The result schema `schemas/v1/release-check.schema.json` has the same shape; unknown properties are additive extensions (unlike the contract schemas). Example refusal:

```json
{"schema_version":1,"command":"release-check","status":"refused","backend":"git","input_path":".","source":"worktree","contract":null,"findings":[],"summary":{"errors":0,"infos":0},"boundary":{"effects":{"network":false,"writes":false,"processes":"detection","project_commands":false},"limits":{"max_bytes":65536,"max_findings":100},"complete":false},"error":{"code":"config-missing","message":"no .stw/release.toml or .stw/release.json"}}
```

## Effects

- Reads: the contract file (never through a symlink; regular file only, at most 64 KiB), or one blob.
- Writes: none. Network: none. Processes: for the working-tree form only the existing ownership detection (which starts Git); `--commit` additionally starts the fixed `git rev-parse`/`cat-file` invocations or the jj runner. No project command, ever.
- Check findings: `product-tag-overlap`, `fetch-unpinned`, `profile-violation`, `schema-violation`, `config-ambiguous`, `allowlist-overlaps-input`, `argv-not-array`, `placeholder-unknown`, `limit-exceeded`, `duplicate-id`, `unknown-reference`, `scheme-invalid`, `env-name-forbidden` (errors) and `unused-build` (info).

## Compatibility

New command and schema. No existing contract changes.
