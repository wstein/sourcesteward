# Contract: `stw release build`

## Command

```text
stw release build --mode ci       --commit C --version V [--product NAME] --out DIR [--files-only] [--json]
stw release build --mode ci       --smoke TARGET --commit C --version V [--product NAME] --out DIR [--json]
stw release build --mode promote  --tag T [--product NAME] --record FILE [--json]
stw release build --mode local-verify --commit C --version V [--product NAME] --record FILE [--json]
```

`build` is the only command that executes project commands. It exists so the workflow runs one audited code path instead of shell glue. It is refused by default.

| Mode | When allowed | Effect |
| --- | --- | --- |
| `ci` | `GITHUB_ACTIONS=true`; the checkout was made with full history (the workflow uses `fetch-depth: 0`), HEAD equals C, tracked tree clean (`git diff-index --quiet HEAD`); the contract at C passes `check`; the offline build gates are re-evaluated and any failure refuses `gate-failed` (the online gates were evaluated by the preceding workflow step); `--out DIR` is new and empty | executes each `[[build]]` as the spec's [build table](../spec.md#build-environment-normative) says (compile builds and `fetch` builds alike), collects declared outputs into `DIR`, writes `SHA256SUMS` and `build-manifest.json` there. `--files-only` skips `oci-image` targets and the builds only they use (the reproducibility workflow must not push images) |
| `ci --smoke TARGET` | as `ci`, with `--out DIR` an existing directory holding the build outputs (read only) | runs the target's `smoke_argv` with the working directory at the root of the checked-out commit C, with `{artifact}` the absolute path of the target's output file in `DIR`; `stw` does not extract anything, the project's smoke script does; exit status of the command is the result |
| `promote` | `GITHUB_ACTIONS=true`, the workflow's verification step passed in the same run (the workflow orders the steps), `FILE` is the record read from the tag commit or the record commit | for each `oci-image` asset of the record executes `[promote].argv` with `{repository}`, `{digest}` and `{version}` taken from the record under the same environment rules; it adds the version tag to the same digest and never rebuilds, pushes a new image or changes another tag; if the tag already points to that digest it succeeds without change, and if it points to another digest it refuses (`promote-failed`) |
| `local-verify` | explicit user authorization in the caller's task context for a verification rebuild; Git-owned checkout | exports C's tree from tree objects into a fresh temporary directory (regular files, modes 0644/0755, `O_EXCL`, symlinks, gitlinks, `.git`, `..`, absolute, duplicate or case-colliding names and grafts refused with `export-unsafe`), builds there with the same environment, compares each artifact hash with `FILE` (a record or a build manifest) and prints the comparison; output stays in a temporary directory that is removed; nothing is published, pushed, tagged or registry-tagged |

Neither `ci` nor `local-verify` changes refs, the index or the working tree of the user's checkout (in `ci` mode the checkout is the CI's own). Any other mode, or `ci`/`promote` outside GitHub Actions, refuses with `ci-only`; `local-verify` without `--record` is a usage error. `local-verify` is **not** the release build and its output is never uploaded; it answers "does a second build of C give the recorded hashes" for the owner. A fetch build reaches its declared hosts; `local-verify` of a fetch build therefore needs the same network and the owner's authorization states that.

## Procedure (`ci`)

1. Validate version against the scheme; read the contract at C from the tree; compute `config_sha256`.
2. For each build in file order, for each invocation (per-target: each referencing target; once: one): construct the environment and argv by substitution, run with `timeout_seconds`, bounded output (1 MiB per stream retained, the rest discarded with a note), process group killed on timeout or cancellation. Exit status != 0 stops the build (`status: error`, code `build-failed`, the failing build id).
3. Verify each declared `output` exists as a regular file under `dist/release/`; read each `result_file` against its schema (`oci-image`); reject extra files only by reporting them (`unexpected-output` info).
4. Copy outputs into `DIR`, compute hashes and sizes, write `SHA256SUMS` (`sha256sum -c` format, sorted by name) and `build-manifest.json` (canonical form).
5. Smoke (`--smoke TARGET`) is a separate invocation on the target's native runner; the runner obtains the `stw` binary from the workflow's hand-off artifact ([workflow contract](workflow.md)).
6. The build manifest records, for every build, `network`, `network_hosts` and the SHA-256 of each `lock_files` entry read from C's tree, so a fetch is disclosed in the attested document.

## Exit status

0 built or promoted; 1 refusal or operational failure (`ci-only`, `build-failed`, `promote-failed`, `checkout-mismatch`, `gate-failed`, `output-exists`, `export-unsafe`, `canceled`, `release-limit`); 2 usage; 3 `local-verify` found a hash difference (`artifact-mismatch`). Precedence 2, 1, 3, 0.

## Backend capability

`ci`: the workflow's checkout is Git; `build` refuses a jj-owned or ambiguous tree. `local-verify`: Git only; jj and GitButler refuse with `unsupported-backend`.

## Effects

Executes project commands (disclosed, the purpose). A `fetch` build contacts its declared hosts; `stw` does not enforce or sandbox that. Writes: `DIR` (new, empty), the project's own build outputs, and the temporary export in `local-verify`. Network: whatever the project command does; `stw` itself makes none. Secrets: `stw` passes none and constructs the environment (an inherited `HOME` can still expose files of the runner user; the runner is ephemeral). Not done: tagging, pushing, publishing, registry tagging, promoting, attesting (the workflow's attest action does that).

## Compatibility

New command. It is the one command whose effects the constitution amendment proposal names.
