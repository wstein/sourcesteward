# Contract: `stw release record`

## Command

```text
stw release record --commit C --version V --from DIR [--repo OWNER/NAME] [--signer-workflow-file PATH] [--run-id N --run-attempt N] [PATH] [--product NAME] [--write | --check FILE] [--json]
```

Builds the canonical release record ([record.md](record.md)) from the downloaded build output. It never builds, contacts a network, or executes anything downloaded.

| Flag | Meaning |
| --- | --- |
| `--commit C` | the built commit (full id); its tree supplies the contract, the lock and tool-list hashes |
| `--version V` | validated against the contract's scheme |
| `--from DIR` | directory with `SHA256SUMS`, `build-manifest.json` and the file artifacts (downloaded draft assets); regular files only |
| `--repo`, `--run-id`, `--run-attempt` | default to the values in the manifest (parsed and hash-checked here; its attestation is checked by `verify --online`, not by `record`); if given they must equal it (`artifact-mismatch` otherwise) |
| `--product NAME` | the product (required when several exist; an empty value is treated as absent) |
| `--signer-workflow-file PATH` | explicit signer workflow file for a release not built by `ci.release_workflow` (the bootstrap release): the record's `signer_workflow` becomes `<repository>/PATH`; verify applies the same override |
| `--write` | create the product's record file (`docs/releases/<version>.json`, or `docs/releases/<product>/<version>.json`) in the working tree; refuse (`output-exists`) if a different file exists; an identical file is a no-op |
| `--check FILE` | exit 3 with `record-not-canonical` or `record-invalid` unless FILE is exactly what `record` would produce |
| none of the two | print the record to standard output |

## Procedure

Read the contract at C; parse `build-manifest.json` against its schema; recompute each file's SHA-256 and size from `DIR` and compare with `SHA256SUMS` and with the manifest (any difference is `artifact-mismatch`); require the manifest's `commit`, `version` and `config_sha256` (SHA-256 of the contract blob at C) to match; for `oci-image` targets take digests from the manifest's copy of the build result and hash `lock_file` and `tool_list_file` from C's tree (`null` when not declared); assemble, order and canonically encode with the hand-written, schema-ordered encoder (a generic JSON encoder is not used: its escaping and ordering differ from the canonical form). The repository, signer, `url_template`, `tag`, `project`, `product` and image `tags` are computed from the contract and the hash-checked manifest, never taken from flags other than `--repo`, which must equal the manifest's value. A `fetch` build's declaration and `lock_files` hashes come from the manifest and are not part of the record.

## Exit status

0 printed, written or checked equal; 1 refusal or operational failure (`output-exists`, `commit-unresolved`, `product-required`, `unsupported-backend`, `release-limit`, `canceled`); 2 usage; 3 `--check` found a difference or the inputs disagree (`artifact-mismatch`, `record-invalid`, `record-not-canonical`). Precedence 2, 1, 3, 0.

## Backend capability

Git and jj supported (reading C's tree; jj through its read-only runner, unverified). `--write` in a jj-managed workspace creates one untracked-to-jj file that the next jj command snapshots into the working-copy commit; the output says so. GitButler refuses `--write`. The command never stages, commits or tags.

## Output

Human: a header (commit, version, number of assets), the effects line, `wrote docs/releases/<version>.json` or `printed to standard output`. JSON: `ReleaseRecordResult` (`schemas/v1/release-record-result.schema.json`) with `status`, `record` (the record object or `null`), `path`, `written`, `canonical_sha256` (SHA-256 of the canonical bytes), `error`. The record itself, not the result, is what is written to disk.

## Effects

Reads: contract and blobs at C, files in DIR (streamed, at most 2 GiB each, 64 files). Writes: with `--write`, exactly one new file created with `O_EXCL` and mode 0644. Every path component under the repository root is opened without following symlinks (`openat` with `O_NOFOLLOW`); the directories `docs/releases/` and `docs/releases/<product>/` are created if absent, and a symlink or non-directory in the way refuses with `output-exists`; no staging, no commit; nothing else. Network: none. Processes: fixed isolated Git reads. Recovery: the written file is new, so nothing is overwritten; deleting it undoes the effect (constitution III).

## Compatibility

New command and schema.
