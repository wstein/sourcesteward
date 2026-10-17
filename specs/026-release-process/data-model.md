# Data model: release process

One normative table per concept lives in [spec.md](spec.md): gate and finding codes, version schemes, limits, exit statuses, build environment, backend capability. This document defines the entities and invariants those tables apply to and the schema files that carry them. Result schemas follow the existing convention (`schema_version: 1`, unknown properties are additive extensions); the **contract** schemas (`release-config`, `release-record`, `release-build-manifest`, `release-oci-result`) forbid unknown properties.

## Schemas

| File (`schemas/v1/`) | Describes | Source of truth | Published on Pages |
| --- | --- | --- | --- |
| `release-config.schema.json` | the project contract | [contracts/config.md](contracts/config.md) | yes |
| `release-record.schema.json` | `docs/releases/<version>.json` | [contracts/record.md](contracts/record.md) | yes |
| `release-build-manifest.schema.json` | `build-manifest.json` | record.md (same `$defs`) | yes |
| `release-oci-result.schema.json` | the `oci-image` build result file | record.md | yes |
| `release-check.schema.json` | `check --json` | [contracts/check.md](contracts/check.md) | yes |
| `release-plan.schema.json` | `plan --json` | [contracts/release-plan.md](contracts/release-plan.md) | yes |
| `release-verify.schema.json` | `verify --json` | [contracts/verify.md](contracts/verify.md) | yes |
| `release-record-result.schema.json` | `record --json` | [contracts/record-command.md](contracts/record-command.md) | yes |
| `release-dispatch.schema.json` | `dispatch --json` | [contracts/dispatch.md](contracts/dispatch.md) | yes |
| `release-build.schema.json` | `build --json` | [contracts/build.md](contracts/build.md) | yes |

Publishing them on Pages is the existing packaging list (`scripts/package-pages.py` `SCHEMA_PATHS`), extended in WI2 and WI4.

## Entities

### Product

One contract file per product (`.stw/release.toml` for the single default product, `.stw/release.<product>.toml` otherwise; at most 64). A product owns its tag grammar, version scheme, monotonicity set and record path. `ProductSet` is the list of validated contracts of a commit; selecting a product is by `--product`, or for a tag by the single product whose grammar fully matches the tag. Invariants: ids unique; literal tag prefixes pairwise not equal and not prefix-related (`product-tag-overlap`); a repository mixes either the default file or named files, never both.

### ReleaseContract

The validated model of `.stw/release.toml`/`.json`; the Go type mirrors the schema tables (`Project`, `Version`, `Build[]`, `Target[]`, `Outputs`, `Reproducibility`, `Allowlist`, `CI`, `Readiness?`, `Promote?`). Invariants (all `check` findings, see the spec's code table): ids unique within builds and within targets; every `target.build` exists; per kind required and forbidden keys (schema); placeholders closed and available per field; paths confined; `output`/`result_file` under `dist/release/`; `promote` present exactly when an `oci-image` target exists; allowlist patterns do not overlap declared inputs; at most the limits of the spec. Derived values: `config_sha256` (SHA-256 of the exact bytes read), `tag_for(version)` (prefix + version, or `tag_template` with `{version}` replaced) and `version_of(tag)` (inverse; the round trip is the identity by the shape rule for `tag_pattern`). A `Build` additionally carries `network` (`none` or `fetch`), `network_hosts` and `lock_files`; a build with `fetch` lists at least one host, and with reproducibility `required` at least one lock file.

### VersionScheme

`semver`, `calver` or `pattern` per the spec's version table. Operations: `Valid(version)`, `Tag(version)`, `ParseTag(tag) -> (version, tuple)`, `Compare(tupleA, tupleB)`. The monotonicity set of a product is the set of tags and releases that fully match its grammar; the subject tag is excluded. Tuples are fixed-length integer vectors with an absent component represented as -1. All numbers are at most 9 digits so comparison is exact in 32-bit arithmetic. Pattern compile, shape (literal prefix and suffix equal to the template's, one top-level `version` group) and group checks are `scheme-invalid` findings; `calver` also requires a real calendar date.

Monotonicity examples (tests): `v0.10.0 > v0.9.0`; `2026.10.15-b98.1 > 2026.10.15-b98 > 2026.10.15`; `toolbox/2026.10.14` after `toolbox/2026.10.15-b98.1` is refused (same set); `2026.02.31` is invalid; for the pattern product `zlib`: `zlib-v1.3.2 > zlib-v1.3.1 > zlib-v1.3`, and `busybox-v9.0.0` is outside the zlib set and only counted; `zlib-v1.3.1` followed by a newline does not match.

### Gate and verdict

`Gate {code, outcome, reason, detail}`. `outcome` in `pass | fail | not-run | not-applicable`; `reason` is a kebab-case token (`needs-online`, `main-ref-absent`, `shallow-clone`, `grafts-present`, `depends-on-<code>`, `ci-run-absent`, `ci-run-failed`, `ci-run-pending`, `drafts-not-checked`, `path-not-allowlisted`, `mode-change`, `type-change`, `symlink`, `gitlink`, `entries-exceeded`); `detail` is escaped, at most 200 bytes. Verdict rule (the spec's single definition): any `fail` gives `fail`; otherwise any required `not-run` gives `incomplete`; otherwise `pass`; `not-applicable` is ignored. `boundary.complete` is a different notion (false for refusals, errors and results cut by a bound) and does not change the verdict rule.

### Subject

`{product | null, commit | null, tag | null, version, tag_commit | null, record_commit | null, main_ref: {remote, commit | null, fetched: false}}`. `tag_commit` is the peeled commit of the tag. In build mode with a commit, `tag` is `null`.

### DiffEntry (internal, summarized in the result)

`{status A|M|D|other, old_mode, new_mode, path}` parsed from `diff-tree -z`; classification `allowed | not-allowlisted | mode-change | type-change | symlink | gitlink | forbidden-dir` (paths under `.stw/` or `.github/` are never allowed even if a pattern matches, which `check` also prevents). The result carries counts, up to 20 offending paths (escaped) and `truncated`.

### Record, Asset, BuildManifest, OciResult

As [contracts/record.md](contracts/record.md). Ordering: assets by `target` then `name`; platforms by `os`, `arch`, `variant`; tags sorted. The record gains `product` (optional) and the manifest gains `product` and `builds[{id, network, network_hosts, lock_files[{path, sha256}]}]`. The record and the manifest share the asset definitions; the record is derived from the manifest plus `SHA256SUMS`, the checksum and manifest file hashes, the attestation block, `tag` and `repository`. `config_sha256` is in the manifest only.

`kind: file` fields: `target, role, group?, build, name, url_template, sha256, size, os, arch`. `kind: oci-image` fields: `target, role, group?, build, repository, index_digest, platforms[{os,arch,variant?,manifest_digest}], tags[], lock{path,sha256}|null, tool_list{path,sha256}|null, source_offer|null`.

### Scheme declaration `pattern`

For per-component tags such as `zlib-v1.3.1` (worked example 5) or BundleDock's `opentofu/1.13.1-b1`:

```toml
[version]
scheme = "pattern"
source = "input"
tag_template = "opentofu/{version}"
tag_pattern = '^opentofu/(?P<version>(?P<major>[0-9]{1,9})\.(?P<minor>[0-9]{1,9})\.(?P<patch>[0-9]{1,9})-b(?P<build>[0-9]{1,9}))$'
order = ["major", "minor", "patch", "build"]
```

The product is the unit of comparison: its monotonicity set is every tag that fully matches this grammar. Each product has its own file (`.stw/release.opentofu.toml`).

### DispatchPlan

`{repository, host, workflow, ref: "main", inputs{mode, product, commit|tag, version}, resolved_commit, main_commit, config_sha256, verdict}`; `plan_id = sha256(canonical JSON of the plan)`. Stateless: never stored.

### CheckFinding

`{code, severity: error|info, pointer (JSON pointer or "line N" for profile violations), message}`; ordered by pointer then code; at most 100.

### BuildResult

`{status, mode, builds[{id, invocations[{target, exit, duration_seconds, stdout_truncated, stderr_truncated}]}], outputs[{name, size, sha256}], unexpected[], error}`. Output text is bounded and escaped.

## Completeness and escaping

Every string from repository, tag, release, API or project output is escaped for control characters (rendered as `\xNN`); non-UTF-8 paths are escaped per field with `path_encoding: "escaped"` and are never allowlist-matched (they fail `diff-not-allowlisted`). Arrays are never `null`. `complete: false` for refusals, errors, and results where a documented bound cut the evaluation.

## State transitions

None stored. The process state is carried by Git and GitHub: commit C on `main` -> dispatch -> draft release + attestations (+ staged images) -> follow-up commit D with the record -> annotated tag on D -> publish (and promote) -> published release. Each arrow is a separately authorized step; the data model keeps no local state.
