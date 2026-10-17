# Contract: worked examples (fixtures)

Five worked examples (six contract files, because example 5 has two products) and their records are kept as fixtures in [`../examples/`](../examples/). They were validated against the schemas of [config.md](config.md) and [record.md](record.md) by a scratch program (Go, the repository's pinned `santhosh-tekuri/jsonschema/v6`, the TOML parsed by Python `tomllib` after a line check of the strict profile; ten mutated negative copies were rejected, see [research](../research.md#measurements)); the same validation becomes a test in `internal/contracts` (WI2, WI4). The records use fabricated digests; the names, repositories and versions are illustrative and say nothing about those projects' real release policy.

| # | Fixture | Shows | Scheme | Artifacts |
| --- | --- | --- | --- | --- |
| 1 | `go-cli.release.toml`, `go-cli.record.json` | own-script adapter, one `per-target` build, three file targets with native smoke runners | `semver` | 3 `file` |
| 2 | `oci-bundle.release.toml`, `oci-bundle.record.json` | one `once` build that pushes by digest and writes a result file, `[promote]`, lock, tool list and source-offer references | `calver` with `tag_prefix` | 1 `oci-image` |
| 3 | `polyglot.release.toml`, `polyglot.record.json` | four builds (Go per target, Elixir `mix release`, Python pack, image), five targets in three groups, version read from `web/mix.exs` | `semver`, `source = "file"` | 4 `file` + 1 `oci-image` |
| 4 | `skill-pack.release.toml`, `skill-pack.record.json` | one `once` build producing an archive and a manifest (role `manifest`), `[readiness]` evidence files | `semver` | 2 `file` |
| 5 | `mirror-zlib.release.toml`, `mirror-busybox.release.toml` (as `.stw/release.zlib.toml`, `.stw/release.busybox.toml`), `mirror-zlib.record.json`, `mirror-busybox.record.json` | **two products in one repository**, per-component tags `zlib-v1.3.1`, a **fetch build** (declared hosts, pinning lock file, the declared command is `bdock tools sources fetch`), source bundle plus manifest as file artifacts | `pattern` with `tag_template` | 2 `file` per product |

## How a project lists several builds and artifact groups

Repeat `[[build]]` and `[[target]]`. Each `[[target]]` names its build with `build = "<id>"`. `scope = "per-target"` runs the build once for every target that names it (example 1, the Go CLI in example 3); `scope = "once"` runs it once and every naming target's `output` or `result_file` must exist afterwards (the Elixir release, the Python pack, the image). `group` is only a label carried to the record, so a consumer can ask for "the `cli` group". Builds run in file order. No nesting is needed or allowed by the strict profile.

## Reading the examples

- Example 1 sets `STW_RELEASE_VERSION={version}` and `GOOS`/`GOARCH` through `env_set`; the project script (`scripts/release-package.sh`, not part of this specification) builds with `-trimpath`, `-buildvcs=false` and `-X main.version=...` and writes the archive. `stw` does not know Go. Reproducibility `policy = "required"` is a declaration checked by the reproducibility workflow.
- Example 2 never builds a version-tagged image: `staging_tag` is a commit-derived reference; the version tag is added by `[promote]` to the same digest after `verify`. The promote tool (`crane tag`) is an unverified example; any registry tool that tags a digest works if it fails when the digest is absent.
- Example 3: `source = "file"` makes `plan` require `web/mix.exs` at C to contain `version: "0.3.0"` for dispatch version `v0.3.0` (`file_form = "without-v"`). The `web-image` target has no lock or tool list (`null` in the record).
- Example 4: readiness files live outside the allowlist (`readiness/` and `schemas/`), because an allowlisted path could be added in the follow-up commit D and would then not be evidence about C. `plan` checks existence only. The project's own CI produces and commits `readiness/skills-validation.json` and `readiness/compatibility.json`; whether the criterion (all skills validate, versioned schema, compatibility list) holds is the project's validation, not `stw`'s.

## Example 5: the source mirror (multi-product, fetch builds)

Repository `wstein/bundledock-third-party-sources`. Each upstream component is a product with its own file `.stw/release.<component>.toml`, written by the mirror tooling as JSON in practice (the TOML form is shown for review). Tags are `<component>-v<version>` (`zlib-v1.3.1`, `busybox-v1.36.1`); each product's `tag_pattern` and `tag_template` form a literal prefix and suffix around the `version` group, so the product of a tag is found by its grammar and versions are compared only inside the product (`zlib-v1.3.2 > zlib-v1.3.1`; `busybox-v9.0` never competes with zlib). `product-tag-overlap` rejects, for example, a product `zlib` next to a product `zlib-v2` because `zlib-v` is a prefix of `zlib-v2-v`. Records live at `docs/releases/zlib/1.3.1.json` and `docs/releases/busybox/1.36.1.json`.

The build is not a compilation: `bdock tools sources fetch --component zlib --version 1.3.1 --out <dist/release>` assembles the source bundle and its manifest. The contract declares `network = "fetch"`, the hosts it may contact and `lock_files` that pin the inputs (required by `reproducibility.policy = "required"`; `check` reports `fetch-unpinned` otherwise). Treatment by the commands:

| Command | Treatment of the fetch build |
| --- | --- |
| `check` | validates host list and lock files; never runs or resolves `bdock` |
| `plan` | shows the build, its hosts and lock files as the disclosed network effect of the CI build ("this build will contact: zlib.net, github.com"); fetches nothing; gates are the ordinary ones, evaluated per product |
| `dispatch` | the plan lists the same disclosure and `product` in the request inputs; the effect statement names the fetch |
| `build --mode ci` | runs the declared command once in CI on the gated commit like any build; the manifest records `network`, `network_hosts` and the SHA-256 of each lock file |
| `verify`, `record` | treat the source bundle and manifest like any file artifact: hashes, sizes, attestation; they do not re-fetch and do not judge the fetch |
| `build --mode local-verify` | needs the same network; the owner's authorization names it |

The mirror tool is only an argv element: it must be installed by the workflow's setup steps (a pinned `bdock` release), which is the workflow-owned toolchain setup of the workflow contract.

## Record excerpt (example 1, canonical form)

```json
{
  "assets": [
    {
      "arch": "amd64",
      "build": "cli",
      "group": "cli",
      "kind": "file",
      "name": "stw_0.2.0_linux_amd64.tar.gz",
      "os": "linux",
      "role": "artifact",
      "sha256": "…64 hex…",
      "size": 1000000,
      "target": "linux-amd64",
      "url_template": "https://github.com/{repository}/releases/download/{tag}/{name}"
    }
  ],
  "attestation": {
    "predicate_type": "https://slsa.dev/provenance/v1",
    "provider": "github-artifact-attestation",
    "signer_workflow": "wstein/sourcesteward/.github/workflows/release-build.yml"
  }
}
```

(The full file, with three assets, `build`, `checksums`, `commit`, `kind`, `manifest`, `project`, `repository`, `schema_version`, `source_date_epoch`, `tag` and `version`, is `../examples/go-cli.record.json`.)
