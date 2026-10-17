# Contract: `stw release verify`

## Command

```text
stw release verify <tag> [PATH] [--artifacts DIR] [--online] [--repo OWNER/NAME] [--json]
```

`<tag>` is a tag name matching the project's scheme. `verify` evaluates every publish gate of the spec's [gate table](../spec.md#findings-and-gate-codes-normative) including the artifact and attestation gates: it is [`plan <tag>`](release-plan.md) plus the files. It never publishes, tags, builds or executes a project command; publication is the workflow's step and happens only after `verify` exits 0 in the same run.

| Flag | Meaning |
| --- | --- |
| `--artifacts DIR` | a directory holding the release assets (downloaded draft assets or a downloaded release). Required for `artifact-*` gates; without it they are `not-run` (verdict `incomplete`) |
| `--online` | evaluate the Net gates (CI on the record's commit, monotonicity, releases and drafts) and run the attestation checks |

## Procedure (normative order; every step is a gate, later steps still run after a failure so all failures are reported, except that steps that depend on a missing record or tag are `not-run` with the reason `depends-on-<code>`)

1. Resolve the tag: `git cat-file -t refs/tags/<tag>` must be `tag` (`tag-not-annotated`); the tag name must equal the scheme's tag for the record's version (`tag-version-mismatch`); `tag^{commit}` is the tag commit; it must be an ancestor of `refs/remotes/origin/main` (`tag-not-on-main`).
2. Read `docs/releases/<version>.json` from the **tag commit's tree** (`record-missing`); validate it against the record schema and semantic rules (`record-invalid`); compare its bytes with the canonical re-encoding (`record-not-canonical`).
3. Read the contract from the tag commit's tree and `check` it (`config-invalid`); verify that the contract at the tag commit equals the contract at the record's commit byte for byte through the diff in step 4 (a change is `diff-not-allowlisted`).
4. The record's `commit` C must exist and be an ancestor of the tag commit (`build-commit-not-ancestor`); then `git diff-tree -r --raw -z --no-renames --no-ext-diff --no-textconv C <tag commit>` under the isolated prefix is parsed; every entry must have status `A`, `M` or `D`, both modes `100644` or `100755` (or `000000` on the missing side) with unchanged mode on `M`, a path matching an allowlist pattern or exactly the record path, and a path outside `.stw/` and `.github/`; otherwise `diff-not-allowlisted` names the first 20 offending paths (escaped). More than 20,000 entries is `release-limit`.
5. Net: CI on C: every workflow of `ci.workflows` has a run for `head_sha == C` with conclusion `success` (`ci-not-green`); the version is greater than every other tag and release of its track (`version-not-monotonic`); no published release exists for the tag (`already-published`); a draft release for the tag exists whose assets include every recorded file (`draft-missing`).
6. Files (`--artifacts`): the directory holds exactly the files named by `assets` (kind `file`), `checksums` and `manifest` (`artifact-missing`, `artifact-unexpected`; subdirectories and non-regular files are `artifact-unexpected`); each file's size and SHA-256 equal the record (`artifact-mismatch`); the `SHA256SUMS` file lists exactly the file assets and the manifest, with the recorded hashes. Files are hashed streaming, at most 2 GiB each.
7. Net attestations: for each file asset, the checksum file and the build manifest, and for each image index digest, `gh attestation verify` is run as a bounded process with `--repo <repository> --signer-workflow <signer_workflow> --format json`, outside the repository (working directory a fresh empty temporary directory); the result must verify (`attestation-invalid` / `attestation-missing`); the verified subject digest must equal the record's. The build manifest, once its attestation verifies, must name `commit == C`, `version` and the same asset digests (`attestation-invalid`).

## Exit status

As [`plan`](release-plan.md#exit-status): 0 pass, 1 refusal/incomplete, 2 usage, 3 fail.

## Backend capability

Git supported. jj (any workspace form), GitButler and ambiguous ownership: refused (`unsupported-backend`, `ambiguous-backend`); the message names the authoritative alternative, the CI run, and says that `plan <commit>` works in jj. No fallback to raw Git inside a jj-managed workspace.

## Human and JSON output

Human output lists the steps above with outcome and escaped detail, then `verdict:`. JSON is a `ReleaseVerifyResult` (`schemas/v1/release-verify.schema.json`): the `ReleasePlanResult` shape with `command: "release-verify"`, plus `artifacts` (array of `{name, size, sha256, outcome}`), `attestations` (array of `{subject, outcome, signer_workflow, run_url}`), `tag_commit`, and `diff` (`entries`, `offending` up to 20 paths, `truncated`).

## Effects

- Reads as `plan`, plus the files in `--artifacts` (regular files only, never through a symlink, no content execution) and `.git` objects. Writes: none. Network: only with `--online` (hosting GETs; `gh attestation verify`, which itself calls GitHub's attestation API and its TUF trust root; unverified offline behavior).
- Processes: as `plan`, plus `gh attestation verify` (environment filtered as in the hosting layer, `GH_PROMPT_DISABLED=1`, no pager) with 60 s and 1 MiB output bounds.
- Never executes: a project command, a downloaded file, a hook, a filter.

## Compatibility

New command and schema.
