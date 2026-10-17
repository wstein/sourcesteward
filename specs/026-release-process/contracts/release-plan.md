# Contract: `stw release plan`

## Command

```text
stw release plan <commit|tag> [PATH] [--version V] [--for build|publish] [--online] [--repo OWNER/NAME] [--json]
```

`<commit>` is a full 40 or 64 hexadecimal id (abbreviations are a usage error: ambiguity is a safety problem here); `<tag>` is a name matching the project's scheme (`v0.1.0`, `toolbox/2026.10.15-b98.1`). Anything else exits 2. `PATH` defaults to the current directory. The command never prompts, opens an editor or pager, writes anything, or starts a project command. It reads the contract from the **evaluated commit's tree** (C for a commit, the record's commit for a tag), never from the working tree.

| Flag | Meaning |
| --- | --- |
| `--version V` | required with a commit (the version to be built); forbidden with a tag (derived from the tag) |
| `--for build\|publish` | default `build` for a commit and `publish` for a tag; `--for build` with a tag is the second input mode of D1 (build once on an existing tag); `--for publish` with a commit is a usage error |
| `--online` | evaluate the gates marked Net in the spec's [gate table](../spec.md#findings-and-gate-codes-normative) with bounded GitHub reads (hosting transport, read only); without it those gates are `not-run` and the verdict is `incomplete` |
| `--repo OWNER/NAME` | select the repository when the local remotes are ambiguous (hosting resolution rules of Spec 020) |
| `--json` | one result object |

## Gates evaluated

| Mode | Gates (spec gate table codes), in order |
| --- | --- |
| build, commit | `commit-not-on-main`, `config-invalid`, `version-invalid`, `version-file-mismatch`, `readiness-missing`, then Net: `version-not-monotonic`, `release-exists`, `ci-not-green` |
| build, tag (`--for build`) | the publish gates `tag-not-annotated`, `tag-version-mismatch`, `tag-not-on-main`, then the build gates above evaluated at the tag's commit, with `release-exists` meaning a release or draft (the tag is expected to exist) |
| publish, tag | `tag-not-annotated`, `tag-version-mismatch`, `tag-not-on-main`, `config-invalid`, `record-missing`, `record-invalid`, `record-not-canonical`, `build-commit-not-ancestor`, `diff-not-allowlisted`, then Net: `ci-not-green` (on the record's commit), `version-not-monotonic`, `already-published`, `draft-missing`; `artifact-*` and `attestation-*` are `not-applicable` (they need files: use `verify`) |

`commit-not-on-main` and `tag-not-on-main` use the local remote-tracking ref `refs/remotes/origin/main`; the output states that no fetch was made and the ref's recorded commit. A missing `refs/remotes/origin/main` or a shallow clone (where ancestry cannot be decided) makes the ancestry gates `not-run` with reason `main-ref-absent` or `shallow-clone`, never `pass`; the maintainer runs a visible `git fetch` first.

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | verdict `pass` |
| 1 | refusal, operational failure, cancellation, or verdict `incomplete` (any required gate `not-run`) |
| 2 | usage error |
| 3 | verdict `fail` (complete result, at least one gate `fail`) |

Precedence 2, 1, 3, 0.

## Backend capability

Per the spec's capability table: Git supported for commit and tag; jj supported for `plan <commit>` (ancestry and tree lookups through the existing read-only jj runner; unverified) and refused with `unsupported-backend` for `plan <tag>`; GitButler and ambiguous ownership refuse.

## Human output

```text
SourceSteward release plan: build 4e7f3b62… as v0.2.0 (git)
effects: read-only; no project command executed; nothing built; network: none (offline)
origin/main = 9a1c…, recorded locally, no fetch was made

gates
  pass     commit-not-on-main
  pass     config-invalid
  pass     version-invalid
  n/a      version-file-mismatch      version source is input
  not-run  version-not-monotonic      needs --online
  not-run  release-exists             needs --online
  not-run  ci-not-green               needs --online

would build (nothing executed)
  build cli (per-target): sh scripts/release-package.sh linux amd64 v0.2.0 dist/release/stw_0.2.0_linux_amd64.tar.gz  [x3 targets]
  assets: 3 files + SHA256SUMS + build-manifest.json
verdict: incomplete (3 gates not run)
```

Repository text (paths, names, tags, argv) has control characters escaped; the argv display is data, not an instruction.

## JSON

One `ReleasePlanResult` per invocation, validated by `schemas/v1/release-plan.schema.json`: `schema_version`, `command: "release-plan"`, `status` (`ok`, `refused`, `error`), `mode` (`build` or `publish`), `backend`, `subject` (`commit`, `tag` or `null`, `version`, `tag_commit` or `null`, `main_ref` with `commit` and `fetched: false`), `gates` (array of `{code, outcome, reason, detail}`; `outcome` is `pass`, `fail`, `not-run`, `not-applicable`), `verdict` (`pass`, `fail`, `incomplete`), `would_build` (array of builds with `id`, `scope`, `invocations` of `{target, argv}`; empty in publish mode), `would_publish` (the record's asset names, or empty), `network` (array of `{method: "GET", endpoint}` actually requested), `boundary` (`effects`, `limits`, `complete`), `error`. Example (a gate failure):

```json
{"schema_version":1,"command":"release-plan","status":"ok","mode":"publish","backend":"git",
 "subject":{"commit":null,"tag":"v0.2.0","version":"v0.2.0","tag_commit":"1111111111111111111111111111111111111111","main_ref":{"commit":"2222222222222222222222222222222222222222","fetched":false}},
 "gates":[{"code":"tag-not-annotated","outcome":"pass","reason":null,"detail":null},
          {"code":"diff-not-allowlisted","outcome":"fail","reason":"path-not-allowlisted","detail":"src/main.go (M)"},
          {"code":"ci-not-green","outcome":"not-run","reason":"needs-online","detail":null}],
 "verdict":"fail","would_build":[],"would_publish":["stw_0.2.0_linux_amd64.tar.gz"],"network":[],
 "boundary":{"effects":{"network":false,"writes":false,"project_commands":false},"limits":{"max_diff_entries":20000},"complete":true},"error":null}
```

## Effects

- Reads: ref and commit objects, trees and blobs of the evaluated commit (contract, record, version file, readiness paths, lock and tool-list paths are looked up, not read, except the version file and record), the raw tree diff between the record's commit and the tag commit, `refs/remotes/origin/main`, tag objects.
- Writes: none. Index, refs, HEAD, configuration unchanged; no lock file remains; for jj no working-copy snapshot and no operation-log entry.
- Network: none without `--online`; with it, only GETs through the hosting transport (workflow runs for the commit, releases and tags lists, at most 10 pages and 1,000 records).
- Processes: the fixed isolated Git invocations (`rev-parse`, `cat-file`, `merge-base --is-ancestor`, `diff-tree -r --raw -z --no-renames --no-ext-diff --no-textconv`, `ls-tree`, `for-each-ref`) with no hook, filter, textconv, fsmonitor, helper or alias; or the existing jj read-only runner. `gh auth token` only with `--online` (existing hosting behavior). No project command.

## Compatibility

New command and schema. `hosting`, `inspect`, `inventory`, `pins`, `repos`, `tidy`, `workspace` and `version` contracts are unchanged. Gate codes are append-only within `schema_version: 1`.
