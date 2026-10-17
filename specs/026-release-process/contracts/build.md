# Contract: `stw release build`

## Command

```text
stw release build --mode ci --commit C --version V --out DIR [--json]
stw release build --mode local-verify --commit C --version V --record FILE [--json]
stw release build --mode promote --tag T --record FILE [--json]
```

`build` is the only command that executes project commands. It exists so the workflow runs one audited code path instead of shell glue. It is refused by default.

| Mode | When allowed | Effect |
| --- | --- | --- |
| `ci` | `GITHUB_ACTIONS=true`, HEAD equals C, tracked tree clean (`git diff-index --quiet HEAD`), the contract at C passes `check`, the gates already passed in the same workflow run (the workflow orders the steps; `build` re-evaluates only the offline build gates and refuses `gate-failed`) | executes each `[[build]]` as the spec's [build table](../spec.md#build-environment-normative) says, collects declared outputs into the new empty directory `DIR`, writes `SHA256SUMS` and `build-manifest.json` there |
| `promote` | `GITHUB_ACTIONS=true`, the tag workflow's verification step passed in the same run (the workflow orders the steps), `FILE` is the record read from the tag commit | for each `oci-image` target executes `[promote].argv` with `{repository}`, `{digest}` (the record's index digest) and `{version}` under the same environment rules; it adds the version tag to the same digest and never rebuilds, pushes a new image or changes another tag; refuses (`gate-failed`) if the digest in the record differs from the one passed |
| `local-verify` | explicit user authorization in the caller's task context for a verification rebuild; Git-owned checkout | exports C's tree from tree objects into a fresh temporary directory (the export rules of the earlier draft: regular files, modes 0644/0755, `O_EXCL`, symlinks, gitlinks, `.git`, `..`, absolute, duplicate or case-colliding names and grafts refused with `export-unsafe`), builds there with the same environment, compares each artifact hash with `FILE` (a record or a build manifest) and prints the comparison; output stays in a temporary directory that is removed; nothing is published, pushed, tagged or registry-tagged |

Neither mode changes refs, the index or the working tree of the user's checkout (in `ci` mode the checkout is the CI's own). Any other mode, or `ci` or `promote` outside GitHub Actions, refuses with `ci-only`; `--files-only` (mode `ci`) skips `oci-image` targets and the builds only they use (the reproducibility workflow, which must not push images); `local-verify` without `--record` is a usage error. `local-verify` is **not** the release build and its output is never uploaded; it answers "does a second build of C give the recorded hashes" for the owner.

## Procedure (`ci`)

1. Validate version against the scheme; read the contract at C from the tree; compute `config_sha256`.
2. For each build in file order, for each invocation (per-target: each referencing target; once: one): construct the environment and argv by substitution, run with `timeout_seconds`, bounded output (1 MiB per stream retained, the rest discarded with a note), process group killed on timeout or cancellation. Exit status != 0 stops the build (`status: error`, code `build-failed`, the failing build id).
3. Verify each declared `output` exists as a regular file under `dist/release/`; read each `result_file` against its schema (`oci-image`); reject extra files only by reporting them (`unexpected-output` info).
4. Copy outputs into `DIR`, compute hashes and sizes, write `SHA256SUMS` (`sha256sum -c` format, sorted by name) and `build-manifest.json` (canonical form).
5. Run each target's `smoke_argv` (if any) in a fresh directory containing the extracted output; the workflow runs this step on the target's native runner, so `build` offers it as `--mode ci --smoke TARGET` over an existing `DIR` (executes the project's smoke command).

## Exit status

0 built or promoted; 1 refusal or operational failure (`ci-only`, `build-failed`, `promote-failed`, `checkout-mismatch`, `gate-failed`, `output-exists`, `export-unsafe`, `canceled`, `release-limit`); 2 usage; 3 `local-verify` found a hash difference (`artifact-mismatch`). Precedence 2, 1, 3, 0.

## Backend capability

`ci`: the workflow's checkout is Git; `build` refuses a jj-owned or ambiguous tree. `local-verify`: Git only; jj and GitButler refuse with `unsupported-backend`.

## Effects

Executes project commands (disclosed, the purpose). Writes: `DIR` (new, empty), the project's own build outputs, and the temporary export in `local-verify`. Network: whatever the project command does; `stw` itself makes none. Secrets: none are passed; the environment is constructed. Not done: tagging, pushing, publishing, registry tagging, promoting, attesting (the workflow's attest action does that).

## Compatibility

New command. It is the one command whose effects the constitution amendment proposal names.
