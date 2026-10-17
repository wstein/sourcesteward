# Contract: the release record, the build manifest and the OCI build result

Three machine-written JSON documents share the asset definitions below. All are canonical JSON (next section), validated by versioned schemas, and rejected when they carry unknown properties.

| Document | Path | Written by | Schema |
| --- | --- | --- | --- |
| Release record | `docs/releases/<version>.json` (single default product) or `docs/releases/<product>/<version>.json` (named product) in the follow-up commit D | `stw release record` | `schemas/v1/release-record.schema.json` |
| Build manifest | `build-manifest.json` next to the artifacts in the draft release; attested | `stw release build` (CI) | `schemas/v1/release-build-manifest.schema.json` |
| OCI build result | the target's `result_file`, written by the project's build | the project command | `schemas/v1/release-oci-result.schema.json` |

`<version>` is the version string with its leading `v` where the scheme has one (`v0.1.0`, `2026.10.15-b98.1`, `1.3.1` for a `pattern` product). The record names the **built commit C** and never the tag commit: the tag commit is D (the commit holding the record) and a commit cannot contain its own hash. Consumers read the tag commit with Git; `verify --json` reports it as `tag_commit`.

## Canonical form (normative)

UTF-8, no BOM; object keys sorted by byte order of their UTF-8 encoding at every level (the encoder is hand-written and schema-ordered; a generic JSON encoder is not used, and the canonical check re-encodes from the parsed, schema-ordered model so misordered arrays are detected); empty arrays are `[]` and empty objects `{}`; arrays in the order the schema or the producer defines (assets sorted by `target` then `name`; platforms sorted by `os`, `arch`, `variant`; tags sorted); two-space indentation; `\n` after every line including the last; a space after the colon; no trailing spaces; integers only (no floats, no exponents); strings use the minimal JSON escapes (`\"`, `\\`, `\n`, `\r`, `\t`, other controls as `\u00xx`; non-ASCII written literally, `<`, `>`, `&` not escaped); optional absent keys are omitted, never `null` unless the schema says so. `record --check` and every reader compare the file bytes to the canonical re-encoding of the parsed content (`record-not-canonical`). This is a project-specific canonical form, deliberately readable in a pull request diff; it is not RFC 8785 (JCS), which has no indentation (research DD6).

## Shared asset definitions

A **file asset**:

| Field | Type | Meaning |
| --- | --- | --- |
| `kind` | `"file"` | |
| `target` | id | the contract's target id |
| `role` | `artifact`, `manifest`, `metadata`, `sbom` | |
| `group` | id, optional | contract label |
| `build` | id | the contract's build id |
| `name` | string | file name (`[A-Za-z0-9._-]{1,128}`) |
| `url_template` | string | e.g. `https://github.com/{repository}/releases/download/{tag}/{name}` with `{repository}`, `{tag}`, `{name}` left in place; consumers substitute |
| `sha256` | 64 lowercase hex | |
| `size` | integer | bytes, at most 2 GiB |
| `os`, `arch` | strings | `any` allowed for platform-neutral files |

An **oci-image asset**:

| Field | Type | Meaning |
| --- | --- | --- |
| `kind` | `"oci-image"` | |
| `target`, `role`, `group`, `build` | as above | |
| `repository` | string | registry repository without tag, e.g. `ghcr.io/wstein/bundledock-toolbox` |
| `index_digest` | `sha256:<64 hex>` | digest of the image index; the attestation subject |
| `platforms` | array | each `{os, arch, variant (optional), manifest_digest}` |
| `tags` | array of strings | the tags the promote step adds to the **same** digest (the version); nothing else is tagged |
| `lock` | `{path, sha256}` or `null` | the resolved lock file read from C's tree |
| `tool_list` | `{path, sha256}` or `null` | tool list reference read from C's tree |
| `source_offer` | string or `null` | license and source-offer reference for copyleft components (path or URL) |

## Release record

| Field | Type | Meaning |
| --- | --- | --- |
| `schema_version` | 1 | |
| `kind` | `"release-record"` | |
| `project` | string | `project.name` |
| `product` | id, optional | `project.product`; present exactly for named products |
| `version` | string | validated against the scheme |
| `tag` | string | `tag_prefix` + version, or `tag_template` with `{version}` replaced |
| `commit` | 40 or 64 hex | the built commit C |
| `repository` | `owner/name` | where the release lives |
| `source_date_epoch` | integer or `null` | the value used for the build |
| `build` | `{workflow, run_id, run_attempt, stw_version}` | the CI run that built; informational, verified against the attestation |
| `assets` | array of file or oci-image assets | |
| `checksums` | `{name, sha256, size}` | the checksum file asset |
| `manifest` | `{name, sha256, size}` | the build manifest asset |
| `attestation` | `{provider, signer_workflow, predicate_type}` | `github-artifact-attestation`, `owner/name/.github/workflows/release-build.yml`, `https://slsa.dev/provenance/v1`; the subject digests are the assets' `sha256` (files) and `index_digest` (images) |

Semantic rules (`record-invalid`): version valid for the scheme; `tag` equal to the tag derived from the version; `commit` a full id; asset names unique; every file asset's `sha256` appears in the checksum file; image `tags` equal exactly `[version]`; `url_template` placeholders are closed (`{repository}`, `{tag}`, `{name}`). **Derived fields are recomputed, never trusted:** `repository` (hosting resolution), `attestation.signer_workflow` (`<repository>/<ci.release_workflow at C>`, or `<repository>/PATH` under the explicit `--signer-workflow-file PATH` of `record` and `verify`, used for the bootstrap release), `project`, `product`, `url_template` (`outputs.url_template` of the contract at C) must equal the recomputed values; the record is editable in the follow-up commit and the record path is allowlisted, so a forged repository, signer or URL must not verify.

## Build manifest

`schema_version`, `kind: "build-manifest"`, `project`, `product` (optional), `version`, `commit`, `source_date_epoch`, `config_sha256` (SHA-256 of the contract bytes at C), `stw_version`, `repository`, `run` (`workflow`, `run_id`, `run_attempt`), `builds` (per build `id`, `network`, `network_hosts` in contract order, `lock_files`), `assets` (same definitions). The attestation of this file is what binds the artifacts to C: the attestation certificate names the workflow identity, and the attested content names the commit ([research](../research.md#attestation-binding-to-commit-c)).

## Build result file for oci-image

`{"schema_version":1,"kind":"oci-build-result","index_digest":"sha256:...","platforms":[{"os":"linux","arch":"amd64","manifest_digest":"sha256:..."}]}`. The project's build writes it after pushing the image by digest to the staging reference. `stw` validates it, never contacts a registry, and copies it into the manifest. A registry that returns different digests than the file states is outside `stw`'s view; the attestation of the index digest and the promote step (which fails if the digest is absent) are the second and third lines of defense.

## Schemas

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://wstein.github.io/sourcesteward/schemas/v1/release-record.schema.json",
  "title": "SourceSteward release record (schema 1)",
  "type": "object",
  "additionalProperties": false,
  "required": ["schema_version", "kind", "project", "version", "tag", "commit", "repository", "source_date_epoch", "build", "assets", "checksums", "manifest", "attestation"],
  "properties": {
    "schema_version": {"const": 1},
    "kind": {"const": "release-record"},
    "project": {"type": "string", "pattern": "^[A-Za-z0-9._ -]{1,64}$"},
    "product": {"$ref": "#/$defs/id"},
    "version": {"$ref": "#/$defs/version"},
    "tag": {"type": "string", "pattern": "^[A-Za-z0-9._/-]{1,128}$"},
    "commit": {"$ref": "#/$defs/commit"},
    "repository": {"type": "string", "pattern": "^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$"},
    "source_date_epoch": {"type": ["integer", "null"], "minimum": 0},
    "build": {
      "type": "object", "additionalProperties": false, "required": ["workflow", "run_id", "run_attempt", "stw_version"],
      "properties": {
        "workflow": {"type": "string", "pattern": "^[A-Za-z0-9._][A-Za-z0-9._/-]{0,255}$"},
        "run_id": {"type": "integer", "minimum": 0},
        "run_attempt": {"type": "integer", "minimum": 1},
        "stw_version": {"type": "string", "pattern": "^[A-Za-z0-9._+-]{1,64}$"}
      }
    },
    "assets": {"type": "array", "minItems": 1, "maxItems": 64, "items": {"$ref": "#/$defs/asset"}},
    "checksums": {"$ref": "#/$defs/fileRef"},
    "manifest": {"$ref": "#/$defs/fileRef"},
    "attestation": {
      "type": "object", "additionalProperties": false, "required": ["provider", "signer_workflow", "predicate_type"],
      "properties": {
        "provider": {"const": "github-artifact-attestation"},
        "signer_workflow": {"type": "string", "pattern": "^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/[A-Za-z0-9._][A-Za-z0-9._/-]{0,255}$"},
        "predicate_type": {"const": "https://slsa.dev/provenance/v1"}
      }
    }
  },
  "$defs": {
    "version": {"type": "string", "pattern": "^[A-Za-z0-9._-]{1,64}$"},
    "commit": {"type": "string", "pattern": "^([0-9a-f]{40}|[0-9a-f]{64})$"},
    "id": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,31}$"},
    "sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
    "digest": {"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"},
    "fileRef": {
      "type": "object", "additionalProperties": false, "required": ["name", "sha256", "size"],
      "properties": {"name": {"$ref": "#/$defs/name"}, "sha256": {"$ref": "#/$defs/sha256"}, "size": {"type": "integer", "minimum": 0, "maximum": 2147483648}}
    },
    "name": {"type": "string", "pattern": "^[A-Za-z0-9._-]{1,128}$"},
    "pathSha": {
      "type": ["object", "null"], "additionalProperties": false, "required": ["path", "sha256"],
      "properties": {"path": {"type": "string", "pattern": "^[A-Za-z0-9._][A-Za-z0-9._/-]{0,255}$"}, "sha256": {"$ref": "#/$defs/sha256"}}
    },
    "asset": {
      "oneOf": [
        {
          "type": "object", "additionalProperties": false,
          "required": ["kind", "target", "role", "build", "name", "url_template", "sha256", "size", "os", "arch"],
          "properties": {
            "kind": {"const": "file"},
            "target": {"$ref": "#/$defs/id"},
            "role": {"enum": ["artifact", "manifest", "metadata", "sbom"]},
            "group": {"$ref": "#/$defs/id"},
            "build": {"$ref": "#/$defs/id"},
            "name": {"$ref": "#/$defs/name"},
            "url_template": {"type": "string", "maxLength": 512},
            "sha256": {"$ref": "#/$defs/sha256"},
            "size": {"type": "integer", "minimum": 0, "maximum": 2147483648},
            "os": {"type": "string", "pattern": "^[a-z0-9_]{1,16}$"},
            "arch": {"type": "string", "pattern": "^[a-z0-9_]{1,16}$"}
          }
        },
        {
          "type": "object", "additionalProperties": false,
          "required": ["kind", "target", "role", "build", "repository", "index_digest", "platforms", "tags", "lock", "tool_list", "source_offer"],
          "properties": {
            "kind": {"const": "oci-image"},
            "target": {"$ref": "#/$defs/id"},
            "role": {"enum": ["artifact", "manifest", "metadata", "sbom"]},
            "group": {"$ref": "#/$defs/id"},
            "build": {"$ref": "#/$defs/id"},
            "repository": {"type": "string", "pattern": "^[a-z0-9][a-z0-9._/-]{0,254}$"},
            "index_digest": {"$ref": "#/$defs/digest"},
            "platforms": {
              "type": "array", "minItems": 1, "maxItems": 32,
              "items": {
                "type": "object", "additionalProperties": false, "required": ["os", "arch", "manifest_digest"],
                "properties": {
                  "os": {"type": "string", "pattern": "^[a-z0-9_]{1,16}$"},
                  "arch": {"type": "string", "pattern": "^[a-z0-9_]{1,16}$"},
                  "variant": {"type": "string", "pattern": "^[a-z0-9_.]{1,16}$"},
                  "manifest_digest": {"$ref": "#/$defs/digest"}
                }
              }
            },
            "tags": {"type": "array", "minItems": 1, "maxItems": 8, "items": {"type": "string", "pattern": "^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$"}},
            "lock": {"$ref": "#/$defs/pathSha"},
            "tool_list": {"$ref": "#/$defs/pathSha"},
            "source_offer": {"type": ["string", "null"], "maxLength": 512}
          }
        }
      ]
    }
  }
}
```

The build manifest and the OCI result have their own schemas (two further JSON blocks below, extracted and tested like the first; the manifest reuses the asset definition of the record schema by reference).

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://wstein.github.io/sourcesteward/schemas/v1/release-build-manifest.schema.json",
  "title": "SourceSteward release build manifest (schema 1)",
  "type": "object",
  "additionalProperties": false,
  "required": ["schema_version", "kind", "project", "version", "commit", "source_date_epoch", "config_sha256", "stw_version", "repository", "run", "builds", "assets"],
  "properties": {
    "schema_version": {"const": 1},
    "kind": {"const": "build-manifest"},
    "project": {"type": "string", "pattern": "^[A-Za-z0-9._ -]{1,64}$"},
    "product": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,31}$"},
    "version": {"type": "string", "pattern": "^[A-Za-z0-9._-]{1,64}$"},
    "commit": {"type": "string", "pattern": "^([0-9a-f]{40}|[0-9a-f]{64})$"},
    "source_date_epoch": {"type": ["integer", "null"], "minimum": 0},
    "config_sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
    "stw_version": {"type": "string", "pattern": "^[A-Za-z0-9._+-]{1,64}$"},
    "repository": {"type": "string", "pattern": "^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$"},
    "run": {
      "type": "object", "additionalProperties": false, "required": ["workflow", "run_id", "run_attempt"],
      "properties": {
        "workflow": {"type": "string", "pattern": "^[A-Za-z0-9._][A-Za-z0-9._/-]{0,255}$"},
        "run_id": {"type": "integer", "minimum": 0},
        "run_attempt": {"type": "integer", "minimum": 1}
      }
    },
    "builds": {
      "type": "array", "maxItems": 16,
      "items": {
        "type": "object", "additionalProperties": false, "required": ["id", "network", "network_hosts", "lock_files"],
        "properties": {
          "id": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,31}$"},
          "network": {"enum": ["none", "fetch"]},
          "network_hosts": {"type": "array", "maxItems": 64, "items": {"type": "string", "pattern": "^[a-z0-9][a-z0-9.-]{0,252}$"}},
          "lock_files": {
            "type": "array", "maxItems": 16,
            "items": {
              "type": "object", "additionalProperties": false, "required": ["path", "sha256"],
              "properties": {"path": {"type": "string", "pattern": "^[A-Za-z0-9._][A-Za-z0-9._/-]{0,255}$"}, "sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"}}
            }
          }
        }
      }
    },
    "assets": {"type": "array", "minItems": 1, "maxItems": 64, "items": {"$ref": "https://wstein.github.io/sourcesteward/schemas/v1/release-record.schema.json#/$defs/asset"}}
  }
}
```

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://wstein.github.io/sourcesteward/schemas/v1/release-oci-result.schema.json",
  "title": "SourceSteward OCI build result (schema 1)",
  "type": "object",
  "additionalProperties": false,
  "required": ["schema_version", "kind", "index_digest", "platforms"],
  "properties": {
    "schema_version": {"const": 1},
    "kind": {"const": "oci-build-result"},
    "index_digest": {"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"},
    "platforms": {
      "type": "array", "minItems": 1, "maxItems": 32,
      "items": {
        "type": "object", "additionalProperties": false, "required": ["os", "arch", "manifest_digest"],
        "properties": {
          "os": {"type": "string", "pattern": "^[a-z0-9_]{1,16}$"},
          "arch": {"type": "string", "pattern": "^[a-z0-9_]{1,16}$"},
          "variant": {"type": "string", "pattern": "^[a-z0-9_.]{1,16}$"},
          "manifest_digest": {"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"}
        }
      }
    }
  }
}
```

## Consumers

Package-manager channels (follow-up specs) read: `version`, `tag`, per-file `name`, `url_template`, `sha256`, `size`, `os`, `arch`, the `attestation` block (verification recipe: `gh attestation verify <file> --repo <repository> --signer-workflow <signer_workflow>`), and for images `repository`, `index_digest`, `platforms[].manifest_digest`, `lock`, `tool_list`, `source_offer`. A Homebrew formula needs `url`, `sha256`, `version`; a Scoop manifest needs `url`, `hash` and `architecture` (Windows targets are a prerequisite, D5); mise/aqua checksums come from `sha256`. None of these channels is implemented here.

## Compatibility

New documents and schemas. `schema_version: 1` readers reject other versions. Additional asset kinds need `schema_version` 2.
