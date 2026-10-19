# Release commands: current source and planned process

Spec 026 remains **planned and unreleased**. Source implementations exist for
`check`, `plan`, `verify`, `record`, `build` and `dispatch`; the `release` group
is hidden from root help while the feature is incomplete. Online gate reads,
pinned attestation verification and explicit workflow dispatch are implemented
in source. Their live GitHub/workflow acceptance remains pending. Registration
does not establish feature acceptance, a completed release workflow or authorization to build.
See [feature status](feature-status.json) and the
[work items](../specs/026-release-process/tasks.md). WI9 documentation can start
after the contract is stable but finishes only after WI8 workflows.

## Current command and acceptance matrix

Use an already available binary matching the source revision. Hidden commands
can be inspected explicitly with `stw release --help` and subcommand `--help`;
older binaries can reject them. Do not infer availability from the design alone.
All current release commands offer human output and `--json`.

| Registered command | Current source behavior | Boundary still pending |
| --- | --- | --- |
| `stw release check [PATH] [--commit C]` | Validates working-tree contracts, or committed contracts at full C; fixed ownership/native reads, no network, writes or project commands | Contract validity is not proof of script behavior or release acceptance |
| `stw release plan C --version V [PATH]` | Reads C's contract, checks local build gates and displays argv/effects as data; `--online` adds bounded authenticated GitHub GET reads | Required remote gates remain `not-run` offline; live end-to-end acceptance remains pending |
| `stw release plan TAG [PATH]` | Checks annotation, ancestry, record and allowlist-diff gates; `--online` adds remote gates; `--for build` selects existing-tag build mode | Git-owned checkout required; artifacts and attestations require `verify` |
| `stw release verify TAG [PATH] --artifacts DIR` | Adds exact file-set, size, checksum and canonical-record checks; `--online` adds remote gates and bounded `gh attestation verify` processes | Git-owned checkout required; source conformance is separate from real release acceptance |
| `stw release record --commit C --version V --from DIR [PATH]` | Generates canonical JSON from existing outputs; prints by default; `--check FILE` compares; `--write` creates the record and missing parent directories without replacing a different existing file | Does not verify attestations, stage, commit, build or contact a network |
| `stw release build --mode MODE ...` | Explicit project-command boundary for `ci`, `promote` and `local-verify`; see below | Runtime preconditions are not owner approval; workflow integration and end-to-end acceptance remain pending |
| `stw release dispatch (--commit C --version V \| --tag T) [PATH]` | Recomputes native/online build gates and displays a request/`plan_id`; only `--apply PLAN_ID` attempts one fixed POST | Build approval is separate; an accepted request is not CI completion, a tag or publication; target workflows still need WI8 |

C is a full lowercase 40- or 64-hex commit object ID, not an abbreviation.
`--product NAME` selects a product when several contracts exist; a tag selects
the product by its grammar. `plan` and `verify` use committed contracts, not
working-tree edits. `--record-commit R` supports a record added after an existing
tag; see the [command contracts](../specs/026-release-process/contracts/release-plan.md).

Offline plans and verification normally exit 1 with verdict `incomplete` when
local checks pass but required online gates are `not-run`. Any failed gate makes
the verdict `fail` instead. `not-applicable` does not block. Read status, verdict,
gate outcomes and boundary qualifications together; neither exit 1 nor a passing
local gate clears publication. Gate names identify violations being tested, so
`pass` for `config-invalid` means that violation was not found.

| Exit | Meaning for release commands |
| --- | --- |
| 0 | Successful operation or passing complete verdict |
| 1 | Refusal, operational failure, cancellation or incomplete verdict |
| 2 | Usage error |
| 3 | `check` findings; failed `plan`/`verify`/dispatch-plan verdict; `record` disagreement/noncanonical comparison; local verification hash mismatch |

The [normative tables](../specs/026-release-process/spec.md#findings-and-gate-codes-normative)
define the individual gates, bounds and status precedence. Local ancestry reads
the hosting-selected remote's recorded `main` and never fetches; missing refs,
shallow history and grafts prevent passing ancestry evidence. Allowlist counts
show tracked-leaf pattern reach, including zero matches, not publication eligibility.

## Online reads and dispatch effects

`plan --online` reads qualified CI push runs on C, product tags/releases and the
ruleset advisory through the bounded hosting transport. `--with-drafts` is the
caller's assertion that its token sees drafts; otherwise the existence gate may
pass for tags/published releases while reporting `drafts-not-checked`. It is not
proof of draft absence. `verify --online` also runs bounded attestation verification
outside the repository, pins repository/workflow/source-ref and checks the
attested manifest's C and digests. Those processes may read GitHub attestations
and trust roots and use private temporary setup. No project command executes.

Dispatch performs online reads even without `--apply`. Its request targets the
contract's workflow on `main`; its stateless ID binds destination, inputs,
resolved C, remote main, contract hash and verdict. A stale local main ref refuses
`main-ref-stale`. Apply recomputes fresh evidence: changed identity refuses
`plan-stale`, and failed gates refuse rather than dispatch. A matching passing
apply attempts one POST without retry. The result's `applied`, `http_status` and
workflow URL describe that request, not a successful build or an inferred run ID.
A timeout or uncertain transport result needs direct Actions inspection before
considering another request. The intended draft/build effects depend on the
reviewed target workflow; WI8 integration and rehearsal are not implied by this
command. See the [dispatch contract](../specs/026-release-process/contracts/dispatch.md).

## Contract, outputs and evidence

The project contract is `.stw/release.toml` or its equivalent JSON model; YAML
is unsupported. The [strict TOML profile and schema](../specs/026-release-process/contracts/config.md)
reject unknown fields, dotted keys, inline/split tables and duplicate keys.
Named products use `.stw/release.<product>.toml` or `.json`, with separate tag
grammars, version ordering and record paths. Do not mix default and named layouts.

The project declares argv, environment, inputs, targets, outputs, version injection,
CI gates, reproducibility policy and a documentation allowlist. Several builds and
file/image targets can coexist. Tools and language toolchains are workflow setup;
`stw` does not mandate a task runner. A declared `fetch` build discloses hosts and
lock inputs but does not enforce egress. Readiness files must be non-empty regular
files in C; their content and the project's criterion are not judged by `stw`.
The [worked examples](../specs/026-release-process/contracts/examples.md) are
illustrative fixtures, not release evidence or adopted policies for those projects.

`build-manifest.json` records C, version, contract hash, run, asset digests and
declared fetch/lock evidence. `SHA256SUMS` covers file assets and the manifest.
`record` hashes those existing files into canonical JSON at
`docs/releases/<version>.json`, or `docs/releases/<product>/<version>.json`.
The record names built commit C, not its containing commit. Image records carry
index/platform digests and declared lock, tool-list and source-offer references.
See the [record format](../specs/026-release-process/contracts/record.md).

| Evidence | Establishes | Does not establish |
| --- | --- | --- |
| Valid contract | Declared profile, schema and input/allowlist rules | Safe scripts, installed toolchains or reproducibility |
| Offline plan/verify | The local gates actually evaluated and, for verify, supplied file agreement | Live CI state, trusted attestations or permission to publish |
| Canonical record | Agreement among downloaded files, manifest and checksums | Trusted provenance; `record` does not verify attestations |
| Online verification | Recorded bytes and manifest checked against the expected repository, workflow and commit when all gates pass | Source correctness, independent reproducibility, owner approval or unmeasured workflow behavior |

## Build modes and backend limits

Build modes execute project commands. `ci` and `promote` refuse outside GitHub
Actions; `ci` requires the exact gated C, tracked-clean full-history checkout and
contract checks. It collects outputs, checksums and the manifest. `--smoke TARGET`
runs that target's declared smoke argv against existing outputs. `promote` runs
the declared image-tag command for the recorded digest/version and never rebuilds.
Project commands may write outputs or use networks; the contract is not a sandbox.
No build mode creates or pushes Git tags or publishes a GitHub release.

The tracked-tree comparison of `ci` (and `ci --smoke`) runs through a frozen private
view of the checkout metadata: captured before the first project invocation
(`.git` anchor, git directories, index and configuration, nested gitlink checkouts
included), compared with C through a supervised native Git process, and revalidated
after the build. A change of that metadata or of the native configuration during the
build refuses `checkout-mismatch`; the stat data of the verified index copy is cleared and
Git re-hashes every tracked file, so a forged mtime or size is not trusted. Entries flagged assume-unchanged or skip-worktree, and the
worktree content of gitlink checkouts, are bound but not re-read. A checkout with a sparse index is refused (cone-mode sparse checkouts with a full index are accepted). This is source-implemented and pending independent
review. The Darwin observer has only been compiled, not run; `internal/release` does
not compile for Windows, as before. `promote` does not compare the checkout and does
not use the view. See the
[verification note](../specs/026-release-process/verification-checkout-view.md).

`local-verify` requires separate explicit build approval and a Git-owned checkout.
It exports C to a temporary tree, builds and compares file hashes, then removes
its outputs. Any OCI target in the trusted selected contract, including a mixed
product, refuses `unsupported-target` before export or execution. `--files-only`
does not enable local image verification. In CI files-only mode, a shared `once`
build producing both file and image targets refuses before any project invocation;
separate file/image builds are required. Normal mixed CI builds remain valid.
Neither file comparison claims image reproducibility.

Git and jj use native ownership and never silently substitute raw Git in jj.
Native jj `check --commit` and commit planning admit the measured regular-file
subset; declared input resolution reaching a committed symlink refuses
`unsupported-backend`, exit 1, with incomplete evidence. Unrelated symlinks remain
admitted for inventory/counting. Tag planning and verification require Git-owned
checkouts. `record` has native Git/jj source readers; `--write` discloses that the
next jj command snapshots the new file. Build modes require Git. Consult
[backend limits](backends.md#release-source-capabilities) and the separate
conformance evidence rather than assuming equivalent acceptance across backends.

## Planned build-once release flow

The complete flow builds C once in authorized CI, smoke-tests and attests it,
then retains a draft. Reviewed commit D adds only allowlisted documentation and
the record; C must be an ancestor of D. The owner pushes an annotated tag on D
with explicit approval for both the tag and triggered publication. The publish
workflow verifies the retained artifacts and attestations before publishing,
without rebuilding the product. The repository-wide diff denies undeclared
changes, so intervening build-input changes require a new candidate/build approval.

Images gain only the version tag at the same digest. Moving aliases, package
channels and consumer pin activation are separate steps. Online reads/dispatch
have source implementations; WI8 workflows, schema publication, bootstrap and final
acceptance remain pending. This source guide completes none of those later gates.
The [owner preparation guide](releasing.md) links their contracts and explains what must be established before execution.
