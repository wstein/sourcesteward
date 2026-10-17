# Research: release process v2

## Why a rewrite

The 2026-10-16 draft ("release hygiene", five analysis rounds) designed a Python `scripts/release.py`, a Go-specific recipe, a markdown readiness record with four approvals and a per-release double build. Since then the sibling repositories merged tag-driven workflows (`sheetmill` spec 019, `bundledock` `docs/design/bdock-release-workflow`) and the owner decided on build-once-then-promote, a language-agnostic contract, `stw release` commands and two artifact kinds (decisions D1 to D8). The draft's gates and recipe no longer describe that process, so this is a rewrite under the same number.

## Measurements

Environment: Linux arm64 (OrbStack), Git 2.56.0, Go 1.27.2, Python 3.13.5, `gh` 2.102.0; `jj` is **not installed**, so every jj statement is unverified. Scratch repositories were created under the session scratchpad. No product artifact was built and nothing on GitHub was read or written.

| Claim | Command or source | Result |
| --- | --- | --- |
| A rebase keeps ancestry of C but changes SHAs | scratch repo: commit C on `main`, branch `pr` with three commits, `main` advanced, `git rebase main`; `git merge-base --is-ancestor C D'` | exit 0; the PR commits have new ids |
| The net tree diff lists only changed paths and runs no filter/textconv | `.gitattributes` with `filter=evil diff=evil`, `filter.evil.clean` and `diff.evil.textconv` touching a sentinel file; `git diff-tree -r --raw -z --no-renames --no-ext-diff --no-textconv C D'` | six entries parsed; sentinel file absent |
| A mode change is visible as status `M` with differing modes | same diff | `:100644 100755 ... M docs/a.md` |
| A symlink change is visible by mode | same diff | `:120000 120000 ... M src/link` |
| Annotated and lightweight tags are distinguishable | `git cat-file -t refs/tags/v1` | `tag` for annotated, `commit` for lightweight |
| Python `tomllib` accepts dotted keys, inline tables and split tables | `tomllib.loads('a.b = 1')`, `'a = {b=1}'`, `'[x]\n[y]\n[x.z]'` | all parsed; so `tomllib` cannot enforce the strict profile |
| The two contract schemas and the example pairs are consistent | scratch Go program with the repository's pinned `santhosh-tekuri/jsonschema/v6` over the schema blocks extracted from `contracts/config.md` and `contracts/record.md`, plus a line check of the strict profile (contiguous `[[name]]` groups, no dotted keys, no inline tables) | six contracts (parsed with `tomllib`) and six records valid; ten mutated copies (file target with a repository, shell string for argv, `tag_pattern` under `semver`, unknown top-level key, `source = "file"` without file keys, empty `tags`, wrong asset keys, `fetch` without hosts, hosts without `fetch`, `pattern` without `tag_template`) invalid. The library requires RE2-compatible patterns (`\u0000` and repeat counts above 1000 are rejected), which the schemas now respect |
| Go `$` without the multi-line flag does not match before a trailing newline | Go program: `zlib-v1.3.1` plus newline against the mirror pattern | no match |
| `info/grafts` fakes ancestry even with replace objects disabled | independent reviewer, round 1 (reproduced by the reviewer, not by the author) | `merge-base --is-ancestor` true for an unrelated commit; hence the `grafts-present` reason |
| `git diff-tree` classifies mode changes (M with modes), file to symlink (`T`), gitlinks (mode 160000) and reports renames as D plus A | independent reviewer, round 1 | as stated |
| No embedded files in the repository | `grep -rn go:embed --include=*.go .` | none, so documentation paths cannot become compiled input |
| The JSON Schema library is test-only today | `grep -rln santhosh-tekuri --include=*.go .` | `internal/contracts` and a CLI test only |
| The hosting transport is GET only and `internal/cli` may import only inspection, workspace and hosting | `internal/hosting/transport.go`, `scripts/check-architecture.py` | a new `internal/release` package needs an architecture-check change (WI1); `dispatch` adds the first write to the transport |
| `gh release create` has `--draft`, `--target`, `--verify-tag`; `gh release edit` has `--draft` and `--verify-tag` | `gh release create --help`, `gh release edit --help` (2.102.0) | present |
| `gh attestation verify` supports `--repo`, `--signer-workflow`, `--format json`, `--bundle` (offline), `--predicate-type` | `gh attestation verify --help` | present; default predicate type is `https://slsa.dev/provenance/v1` |
| `gh workflow run` takes inputs with `-f key=value` and `--ref` | `gh workflow run --help` | present |
| `docs/feature-status.json` references `026-release-hygiene` | `grep -rn 026-release-hygiene .` | the entry is renamed to `026-release-process` with this change |

## Unverified GitHub behavior

None of these was exercised; each is a rehearsal item in the quickstart and a risk in the plan.

1. A draft release for a tag name that does not exist yet is created by `gh release create --draft --target <commit>`, is invisible to a read-only token, and appears in the authenticated releases list.
2. Assets can be uploaded to a draft and downloaded by asset id (`GET /releases/assets/{id}` with `Accept: application/octet-stream`); `releases/tags/{tag}` does not return a draft.
3. Publishing a draft (`PATCH /releases/{id}` with `draft=false`) uses an existing tag instead of creating a new one when the tag exists; when the tag does not exist GitHub creates a lightweight tag at the target. The flow therefore never publishes a draft before the owner's annotated tag exists.
4. With "immutable releases" enabled, assets are frozen at publication and the tag cannot move; drafts stay editable. The interplay with attestations of the release is not exercised.
5. A workflow run's `head_sha` for `workflow_dispatch` is the tip of the dispatched ref (`main`), not the input commit; hence attestation binding below.
6. `workflow_dispatch` through the REST API returns 204 without a run id.
7. `gh attestation verify` can verify a file produced by the attest action of the build workflow, with `--signer-workflow owner/repo/path`; its JSON output shape; behavior without network (`--bundle`).
8. `actions/attest`-style actions accept subject paths and, for images, a subject name and digest with `push-to-registry`; permissions `id-token: write` and `attestations: write`.
9. A workflow's `ubuntu-24.04-arm` and `macos-15` runners are available to this repository (the siblings use the same labels; not verified for `sourcesteward`).
10. The checks listing for a commit reached only by a rebase merge: CI has a run only for commits that were pushed; intermediate rebased commits have none.
11. GoReleaser publish-from-dist and `GORELEASER_CURRENT_TAG` behavior (optional later adapter).
12. `crane tag REPO@DIGEST VERSION` adds a tag to an existing digest and fails if absent (example promote tool).
13. `bdock tools sources fetch` exists with the flags used in example 5 (the mirror design is in flight in another repository; the example only shows the declaration).
14. A fetch build's network access is limited by the runner only; no egress control is claimed.
15. Whether `gh attestation verify --source-ref refs/heads/main` matches the ref of a `workflow_dispatch` run on `main` (flag exists per `--help`; semantics unverified).

## Attestation binding to commit C

A `workflow_dispatch` run executes the workflow file of `main`'s tip and its `github.sha` is that tip, which may be newer than C. A build-provenance attestation records the workflow identity and the tip, not C. The design therefore attests `build-manifest.json` as an ordinary subject; the manifest content (commit, version, config hash, asset digests) is produced by the same workflow and is protected by the attestation signature. `verify` checks that the attested manifest names C and the same digests as the record. The record, reviewed in D, is the second binding: a reviewer compares C to the commit they approved. The verifier pins the signer workflow (from the contract at C, never from the record), the repository (from hosting resolution) and `--source-ref refs/heads/main`, because a writer could otherwise dispatch a modified workflow from a branch and obtain valid attestations. Alternatives rejected: requiring C to be the tip of `main` at dispatch (breaks when other merges land first; kept as a fast path, not a rule), and a custom predicate type (adds a verifier dependency and an unverified action input).

## Decisions and rejected alternatives

| Id | Decision | Rejected | Why |
| --- | --- | --- | --- |
| DD1 | Draft release retains the single build | workflow artifacts (90-day maximum, zipped, expire); OCI-style staging for files; building again at tag | drafts are visible and long-lived; D1 asked for a draft. Workflow artifacts remain the documented fallback if rehearsal contradicts items 1 to 3 |
| DD2 | Bind by ancestry and a deny-by-default net diff | exact parent (rebase merge breaks it); tree equality (docs must differ); signed merge commits | measured above |
| DD3 | A small standard-library parser for the strict TOML profile, differential tests against `tomllib` | adding a TOML library (would still need a profile pass; new dependency); YAML; JSON only | the profile is tiny; `tomllib` accepts what the profile forbids, so a library alone would not enforce it; the dependency policy prefers the standard library |
| DD4 | Hand-written Go validators, schemas verified by corpus tests with the existing test-only JSON Schema library | linking the JSON Schema library into `stw` | keeps the shipped binary's closure unchanged; the schema stays the source of truth through the shared fixtures |
| DD5 | `stw release build` is a `stw` command executing project commands in CI only | shell glue in YAML; a Python script (the earlier draft) | one tested code path; `internal/cli` stays process-free (the process code lives in `internal/release`) |
| DD6 | Project canonical JSON (sorted keys, two-space indent) | RFC 8785 JCS (single line, unreviewable in a PR diff); unsorted output | reviewable and deterministic |
| DD7 | Monotonicity per scheme with numeric tuples | string comparison; Go `semver` library; allowing pre-releases | calver builds `-b98.1` and mixed schemes; no pre-releases in v1 |
| DD8 | Stateless `plan_id` for dispatch | stored plan files; interactive confirm | the constitution wants an inspectable plan and revalidation; there is no local state to protect |
| DD9 | `oci-image` promote runs the project's declared tag command | a registry client in `stw` | keeps `stw` out of registry protocols and credentials; verification rests on attestation of the index digest and a promote command that fails for an absent digest |
| DD10 | Readiness gate = file existence in C's tree | executing a readiness command; parsing evidence files | keeps `plan` read-only and language-agnostic; content judgment belongs to the project's CI and the reviewer of D |
| DD11 | Owner-only actor checks in the workflows | an allow-list from the contract | identical to the siblings, fails closed; organization support needs its own design |
| DD12 | Two workflows (build and publish) plus a repro workflow | one workflow with roles | the attestation signer is the build workflow; the publish workflow has no build step and different permissions, which is easier to test by rule |
| DD13 | Version in the release is `v`-prefixed for semver and stamped by the project through placeholders | stamping from Git (`git describe`) | tag independence (D1) |
| DD36 | `monotonicity = "non-decreasing"` for `pattern` products | making `calver` accept counter restarts; dropping monotonicity for bundles; a separate counter scheme | BundleDock documents a restarting `-bN` counter; the date must still never go back; keeping `strict` the default and `calver` strict preserves the siblings' rule |
| DD14 | No `tag_commit` field in the record | recording the tag commit | self-reference impossible; consumers resolve the tag; `verify --json` prints it |
| DD34 | One contract file per product; product discovered from the file name and the tag grammar | nesting products in one file (the strict profile has no nested tables); one global tag grammar | the profile forbids nesting; per-product files are machine-writable (JSON) for a mirror with many components |
| DD35 | `fetch` is a declared property of a build, not a build kind | a separate `kind = "fetch"` target type with its own runner | keeps one execution path; the effect is what matters and is disclosed |

## Owner decisions of 2026-10-17

Recorded in spec D10 and D11 and in the analysis decision log: amendment decided, applied by PR #58; `tag_commit` omitted; wave-2 preconditions P1 to P3 for the toolbox bundle; dispatch-only reproducibility; draft retention with workflow-artifact fallback and a disposable-repository rehearsal; owner-only actor limit accepted; `build` and `verify --published` kept; the tag ruleset as a required precondition that `stw` only reports. The `ghcr.io` variant needs no secret (`github.token`); a non-GitHub registry such as `quay.io` needs exactly one environment-scoped secret of the `release` environment, which widens the sibling "no secrets" rule in that one login step; the exposure of the credential store to project commands is stated in the workflow credential model.

## Reuse and drops from the release-hygiene draft

Reused, with the reason it still holds: the `v`-prefixed whole-string version format and the newline test; the construct-the-environment discipline (cleared environment, local toolchain, `-trimpath -buildvcs=false`, no GOFLAGS) as the content of the project's own script and of the build table; export from tree objects for `local-verify` (`git archive` applies filters and `export-ignore`, measured by an earlier reviewer); the isolated Git prefix (`--no-optional-locks`, `core.fsmonitor=false`, no user configuration, no replace objects); `<tool>_<version>_<os>_<arch>.tar.gz` names and a `sha256sum -c` checksum file; native smoke tests per target; the finding that CI evidence exists for linux/amd64 only and other platforms are local evidence; the open item that `third_party/README.md` lacks entries for `github.com/spf13/cobra` and `github.com/spf13/pflag` (still a prerequisite task for `stw`'s own first release, WI10); the macOS notarization note (documented, not solved).

Dropped: `scripts/release.py` (Go in `internal/release` instead); the gates G01 to G14, approvals A1 to A4 and the markdown readiness record (replaced by the gate table, D4 and the JSON record); the per-release double build and its `reproducibility-mismatch` gate (D1); the Mac mini manual evidence class as a release gate (native `macos-15` smoke replaces it; manual evidence stays optional); `verify --rebuild`; `release-notes-missing` and the reviewer checklist items (notes are generated by the workflow from the record); the `hidden-command-advertised` gate (a project smoke concern); the unsigned-only statement (attestation is added, signing and notarization stay out of scope); the `plan --final` flow (publish verification replaces it); the record-commit branch rule `codex/release-<version>` (the record is an ordinary PR commit D); decision 6's `codex/release-*` push exception (not needed: nothing is pushed by `stw`).

## Sibling designs: what is taken

From `bundledock` and `sheetmill`: owner-only actor checks (triggering and original actor), annotated tag fetched explicitly, ancestor-of-`main`, CI run query by workflow file and `head_sha`, release-exists with 404-only semantics and a draft-aware recheck in the write job, numeric monotonic check across tags and releases, pinned actions with exact `with:` keys, `persist-credentials: false`, literal gate lines, rule and mutation tests plus golden change detector, `--verify-tag`, the no-standing-approval statement, the tag ruleset click path. Differences: build once (not per tag), attestation, a draft retention and the verify-then-publish split, record-driven verification, language-agnostic contract.

## Risks

Unverified GitHub behavior (above); the owner-only rule blocks organization use; the gate tool is built from the repository being released; the allowlist is only as good as the project's declaration (a project can allowlist a path its build reads, for example an embedded file; `check` prevents only declared inputs and the first reproducibility run is the practical test); `once`/`per-target` builds on one runner cannot use native toolchains; first self-hosted release exercises publish without rehearsal.
