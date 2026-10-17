# Contract: `stw release verify`

## Command

```text
stw release verify <tag> [PATH] [--product NAME] [--record-commit R] [--published] [--signer-workflow-file PATH] [--source-ref REF] [--artifacts DIR] [--online] [--with-drafts] [--repo OWNER/NAME] [--json]
```

`<tag>` is a tag name (resolved as `refs/tags/<arg>`). `verify` evaluates every publish gate of the spec's [gate table](../spec.md#findings-and-gate-codes-normative) including the artifact and attestation gates: it is [`plan <tag>`](release-plan.md) plus the files. It never publishes, tags, builds or executes a project command; publication is the workflow's step and happens only after `verify` exits 0 in the same run.

| Flag | Meaning |
| --- | --- |
| `--record-commit R` | read the record from commit R instead of the tag commit (input mode 2 and the bootstrap release); R must descend from the tag commit and be an ancestor of `main`; C is the tag commit |
| `--published` | verify an already published release (assets downloaded into `--artifacts`, for example the bootstrap release `v0.1.0` or an audit after the fact): `already-published`, `draft-missing` and `version-not-monotonic` are `not-applicable`; every other gate runs |
| `--signer-workflow-file PATH`, `--source-ref REF` | each only with `--published` (a usage error otherwise) and each independently optional. `--signer-workflow-file` replaces the default signer workflow file (`ci.release_workflow` at C) both in the signer recomputation of step 2 (the record's `signer_workflow` must equal `<repository>/PATH`) and in the pin of step 7; `--source-ref` replaces the default attestation source ref `refs/heads/main` in step 7. Both are explicit and never read from the record; the bootstrap release uses `.github/workflows/release-bootstrap.yml` and `refs/tags/v0.1.0` |
| `--with-drafts` | the caller asserts that its token sees draft releases; `verify` has no `release-exists` gate, so the flag only makes the missing-draft detail of `draft-missing` read "absent" instead of "absent or not visible" |
| `--artifacts DIR` | a directory holding the release assets (downloaded draft assets or a downloaded release). Required for `artifact-*` gates; without it they are `not-run` (verdict `incomplete`) |
| `--online` | evaluate the Net gates (CI on the record's commit, monotonicity, releases and drafts) and run the attestation checks |

## Procedure (normative order; every step is a gate, later steps still run after a failure so all failures are reported, except that steps that depend on a missing record or tag are `not-run` with the reason `depends-on-<code>`)

1. Resolve the tag: `git cat-file -t refs/tags/<tag>` must be `tag` (`tag-not-annotated`); the product is selected by the tag grammar among the contract files at the tag commit; the tag name must equal the scheme's tag for the record's version (`tag-version-mismatch`); `tag^{commit}` is the tag commit; it must be an ancestor of the remote-tracking `main` of the hosting-resolved remote (`tag-not-on-main`; ancestry gates are `not-run` for shallow clones, a missing ref or `info/grafts`).
2. Read the product's record from the **tag commit's tree** (or from R with `--record-commit`) (`record-missing`); validate it against the record schema and semantic rules (`record-invalid`), including that `repository` (hosting-resolved), `signer_workflow` (`<repository>/<ci.release_workflow of the contract at C>`, or `<repository>/PATH` under `--signer-workflow-file`), `project`, `product`, `tag`, `url_template` (`outputs.url_template`, default included) and `tags` (exactly `[version]` for images) equal the values recomputed, never trusted from the record; compare its bytes with the canonical re-encoding (`record-not-canonical`).
3. Read the contract from the tag commit's tree and `check` it (`config-invalid`); a contract at the tag commit (or R) that differs from the one at C is detected by the diff in step 4 (`.stw/` is never allowlisted, so it is `diff-not-allowlisted`).
4. The record's `commit` C must exist and be an ancestor of the tag commit (with `--record-commit`: equal to the tag commit, with R a descendant) (`build-commit-not-ancestor`); then `git diff-tree -r --raw -z --no-renames --no-ext-diff --no-textconv C <tag commit or R>` under the isolated prefix (streamed, 20,000 entries, 4 MiB) is parsed; every entry must have status `A`, `M` or `D`, both modes `100644` or `100755` (or `000000` on the missing side) with unchanged mode on `M`, a path matching an allowlist pattern or exactly the record path, and a path outside `.stw/` and `.github/`; otherwise `diff-not-allowlisted` names the first 20 offending paths (escaped). More than 20,000 entries or 4 MiB is the refusal `release-limit`.
5. Net: CI on C: each workflow of `ci.workflows` has a run with `head_sha == C`, `event == push`, `head_branch == main` and conclusion `success` (`ci-not-green`; the event and branch filter follows the sibling design, because `ci.yml` also runs on pull requests and other branches); the version is greater than every other tag and release of the product (`version-not-monotonic`, the subject tag excluded; `not-applicable` under `--published`, which may audit any older release); no published release exists for the tag (`already-published`); a draft release for the tag exists whose assets include every recorded file (`draft-missing`; a token that does not see drafts fails this gate, detail "absent or not visible", which fails closed); with `--published` neither `already-published` nor `draft-missing` applies.
6. Files (`--artifacts`): the directory holds exactly the files named by `assets` (kind `file`), `checksums` and `manifest` (`artifact-missing`, `artifact-unexpected`; subdirectories and non-regular files are `artifact-unexpected`); each file's size and SHA-256 equal the record (`artifact-mismatch`); the `SHA256SUMS` file lists exactly the file assets and the manifest, with the recorded hashes. Files are hashed streaming, at most 2 GiB each.
7. Net attestations: for each file asset, the checksum file and the build manifest, and for each image index digest, `gh attestation verify` is run as a bounded process with `--repo <hosting-resolved repository> --signer-workflow <repository>/<ci.release_workflow at C, or PATH under --signer-workflow-file> --source-ref <refs/heads/main, or REF under --source-ref> --deny-self-hosted-runners --format json`, outside the repository (working directory a fresh empty temporary directory); the result must verify (`attestation-invalid` / `attestation-missing`); the verified subject digest must equal the record's. The build manifest, once its attestation verifies, must name `commit == C`, `version` and the same asset digests (`attestation-invalid`).

## Exit status

As [`plan`](release-plan.md#exit-status): 0 pass, 1 refusal/incomplete, 2 usage, 3 fail (the spec's exit-status table and verdict rule).

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
