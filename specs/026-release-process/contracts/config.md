# Contract: the project release contract (`.stw/release.toml`)

The contract is a human-edited file in a **strict TOML profile**, or the same model written as JSON by a machine (`.stw/release.json`). YAML is not accepted. Exactly one of the two files may exist (`config-ambiguous`). The versioned JSON Schema below is the source of truth for the model; the TOML profile is a restriction of TOML to what maps one-to-one onto it. `stw release check` is the linter.
## Products and files (normative)

A repository has one or more **products**; each product has its own contract file, its own tag grammar and its own monotonic version, and versions are compared only within a product. A repository with one product uses `.stw/release.toml` (no `project.product`). A repository with several products uses one file per product, `.stw/release.<product>.toml` (or `.json`), where `<product>` is an id (`^[a-z0-9][a-z0-9-]{0,31}$`) that must equal `project.product` inside the file. Mixing `release.toml` with named files, or two files for one product, is `config-ambiguous`. At most 64 product files are read (`limit-exceeded`). Machines (for example a mirror tool that adds one product per third-party component) write the JSON form. Commands take `--product NAME`; it is required when more than one product exists (`product-required`) and an unknown name is `product-unknown`; a tag argument selects its product by the tag grammar. Without `--product`, `check` lints every product file and also reports `product-tag-overlap` when two products' literal tag prefixes are equal or one is a prefix of the other. Records are `docs/releases/<version>.json` for the single default product and `docs/releases/<product>/<version>.json` for named products.

## Strict TOML profile (normative)

| Rule | Allowed | Finding when violated |
| --- | --- | --- |
| Tables | only top-level tables `[name]` and top-level arrays of tables `[[name]]`; nothing nested | `profile-violation` |
| Dotted keys (`a.b = 1`, `[a.b]`) | never | `profile-violation` |
| Inline tables (`{ k = v }`) | never | `profile-violation` |
| Split tables (the same `[name]` twice) and interleaved arrays (`[[t]]`, then another table, then `[[t]]` again) | never; a `[name]` appears once; the `[[name]]` entries of one name are contiguous (measured: `tomllib` accepts the interleaved form) | `profile-violation` |
| Duplicate key in one table | never | `profile-violation` |
| Values | basic strings `"..."` with the TOML escapes, literal strings `'...'`, integers (decimal, no sign except `-`, no `_`), booleans, arrays of strings (may span lines, trailing comma allowed) | `profile-violation` |
| Not allowed | floats, dates and times, multi-line strings, arrays of non-strings, arrays of arrays, mixed arrays, `+inf`/`nan`, hexadecimal/octal/binary integers | `profile-violation` |
| Comments, blank lines | allowed | none |
| Encoding | UTF-8 without BOM, LF or CRLF line ends, no NUL, at most 64 KiB | `profile-violation`, `limit-exceeded` |
| Keys | bare keys `[A-Za-z0-9_-]+` only | `profile-violation` |
| Unknown table, unknown key | never (the schema forbids additional properties) | `schema-violation` |

The reference TOML parser for tests is Python `tomllib`: every accepted file must parse there to the same data (measured: `tomllib` also accepts dotted keys, inline tables and split tables, so it cannot be the profile checker; see [research](../research.md#measurements)). The linter is a small standard-library parser for exactly this profile (decision DD3 in the plan). A JSON file is parsed with the standard `encoding/json` with duplicate keys refused and the same schema applied.

## Model

Top-level keys: `schema_version` (integer 1), tables `project`, `version`, `outputs`, `reproducibility`, `allowlist`, `ci`, `readiness` (optional), `promote` (required when an `oci-image` target exists), and arrays of tables `build` and `target`.

| Table | Key | Type | Meaning |
| --- | --- | --- | --- |
| `project` | `name` | string | display name, 1 to 64 characters `[A-Za-z0-9._ -]` |
| | `product` | id, optional | required in `.stw/release.<product>.*` files and equal to `<product>`; absent in `.stw/release.toml` |
| `version` | `scheme` | `semver`, `calver`, `pattern` | see the spec's version table |
| | `tag_prefix` | string, optional | for `semver`/`calver`: characters `[a-z0-9._/-]`, empty or ending with `/` |
| | `tag_template` | string | for `pattern` only: the tag as a template with exactly one `{version}` (for example `zlib-v{version}`) |
| | `tag_pattern` | string | for `pattern` only: RE2 of the form `^<literal prefix>(?P<version>...)<literal suffix>$` with exactly one top-level `version` group; the literal prefix and suffix (characters `[A-Za-z0-9._/-]`) must equal the template's, so version to tag to version is the identity |
| | `order` | array of strings | for `pattern` only: named numeric groups, at most 8 |
| | `monotonicity` | `strict` or `non-decreasing`, default `strict` | for `pattern` only: `strict` requires the new version's `order` tuple to be greater than every existing one; `non-decreasing` requires it to be greater than or equal to the maximum existing tuple and the full tag not to exist yet, so groups that are **not** listed in `order` (a build counter) may restart within the same ordered tuple (for example `2026.10.15-b1` after `2026.10.15-b98.1` with `order` of year, month and day; the order within an equal tuple is undefined and consumers must not infer recency from the counter) |
| | `source` | `input` or `file` | where the version is declared (`input` = the dispatch input / tag) |
| | `file` | path | for `source = "file"`: repository file that must contain the version at C |
| | `file_pattern` | string | for `source = "file"`: RE2 multi-line pattern with exactly one capture group holding the version (compared as `{version}` or `{version_number}` per `file_form`) |
| | `file_form` | `with-v` or `without-v` | for `source = "file"`: how the file spells the version |
| `build` (array) | `id` | id | unique, `^[a-z0-9][a-z0-9-]{0,31}$` |
| | `scope` | `per-target` or `once` | runs once per referencing target, or once for all |
| | `argv` | array of strings | the command; no shell; placeholders per the spec's build table |
| | `env_inherit` | array of names, optional | default `PATH`, `HOME`, `TMPDIR` |
| | `env_set` | array of `NAME=value` strings, optional | values may contain placeholders |
| | `working_dir` | path, optional | confined under the root; default `.` |
| | `timeout_seconds` | integer, optional | 1 to 7200, default 1800 |
| | `network` | `none` or `fetch`, default `none` | `fetch` declares that the command fetches inputs over the network (for example a source mirror assembling upstream sources); a declaration and a disclosure, not an enforcement |
| | `network_hosts` | array of host names, required iff `network = "fetch"` | the hosts the command may contact, at most 64 |
| | `lock_files` | array of paths, optional | repository files that pin the fetched inputs; build inputs (never allowlisted), hashed into the build manifest |
| `target` (array) | `id` | id | unique |
| | `kind` | `file` or `oci-image` | artifact kind |
| | `role` | `artifact`, `manifest`, `metadata`, `sbom` | default `artifact` |
| | `group` | id, optional | label that groups artifacts in the record (for example `cli`, `web`) |
| | `build` | id | the build that produces it; must exist |
| | `os`, `arch` | strings `[a-z0-9_]+` (or `any`) | `file` kind; `oci-image` uses `platforms` |
| | `output` | path template | `file` kind: repository-relative path written by the build; placeholders allowed |
| | `runner` | string, optional | opaque CI runner label for the native smoke job |
| | `smoke_argv` | array of strings, optional | command run in CI after the build at the root of the checkout of C, with `{artifact}` the target's output file; `stw` extracts nothing |
| | `repository` | string | `oci-image`: registry repository without tag |
| | `platforms` | array of `os/arch[/variant]` strings | `oci-image` |
| | `staging_tag` | string template | `oci-image`: reference the build pushes (no version tag) |
| | `result_file` | path | `oci-image`: JSON the build writes (schema in [record.md](record.md#build-result-file-for-oci-image)) |
| | `lock_file`, `tool_list_file` | path, optional | `oci-image`: repository files hashed at C into the record |
| | `source_offer` | string, optional | `oci-image`: path or URL reference to the license and source offer for copyleft components |
| `outputs` | `checksums` | string | name of the checksum file, default `SHA256SUMS` (format `sha256sum -c` reads) |
| | `url_template` | string | default `https://github.com/{repository}/releases/download/{tag}/{name}`; placeholders per the spec's placeholder table (`{repository}`, `{tag}`, `{name}`) |
| `reproducibility` | `policy` | `required`, `best-effort`, `none` | declared claim; checked by the reproducibility job, not by `check` |
| | `source_date_epoch` | `commit-time` or `none` | |
| `allowlist` | `paths` | array of glob strings | documentation/record paths, deny by default |
| `ci` | `workflows` | array of workflow file names | all must have a successful run for C |
| | `release_workflow` | path | the workflow that builds and attests; its identity is verified |
| `readiness` | `files` | array of paths | evidence files that must exist at C |
| `promote` | `argv` | array of strings | `oci-image` only; placeholders per the spec's placeholder table; run by the publish workflow |
| | `timeout_seconds` | integer, optional | |

Rules beyond the schema (findings): every `target.build` names an existing build (`unknown-reference`); ids are unique (`duplicate-id`); argv elements contain only known placeholders (`placeholder-unknown`); `argv` is a non-empty array (`argv-not-array` also for a string where an array is required, because a shell string is never accepted); a `per-target` build referenced by no target and any build referenced by none are warnings reported as info `unused-build` (not an error); the product's record path (`docs/releases/<version>.json`, or `docs/releases/<product>/<version>.json` for a named product) is always allowlisted implicitly and is the only record location; allowlist patterns are globs where `*` matches within a path segment, `**` matches whole segments, no other metacharacter, and a pattern may not match (a) anything under `.stw/` or `.github/`, (b) the version file, (c) any readiness file, any build `lock_files` entry, any `lock_file`, `tool_list_file`, any path equal to a `working_dir` prefix or a build `argv` element that is a repository path that exists in the tree, (d) `go.mod`, `Cargo.toml`-style manifests cannot be detected generically and are the project's responsibility (stated limit). `allowlist-overlaps-input` is evaluated on the declared inputs only; the tree is the second line of defense: at `plan` time the matching patterns are shown with the number of tracked files each matches.

`{out_dir}` is substituted as the **absolute** path of `<repository root>/dist/release` (git-ignored by convention), so it is the same for any `working_dir`; every `output` and `result_file` must be a path under `dist/release/` (finding `schema-violation` with pointer, rule `output-outside-out-dir`). `{output}` is the target's `output` path.

Placeholders are a closed set with one table per field in the spec ([placeholder table](../spec.md#placeholders-normative)); this document does not repeat it. An unknown placeholder, or one not allowed in the field, is `placeholder-unknown`.

## Build and target tables

A `per-target` build runs once for each target that references it, with that target's `os`, `arch`, `target` and `output` placeholders available. A `once` build runs a single time; its placeholders are limited to version, commit and epoch placeholders and `{out_dir}`; every referencing target's `output` or `result_file` must exist afterwards. The build order is the order of `[[build]]` entries. Targets of role `manifest` or `metadata` are ordinary files produced by a build (for example a skill-pack manifest); `stw` adds `SHA256SUMS` and `build-manifest.json` itself.

## JSON Schema (source of truth, `schemas/v1/release-config.schema.json`)

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://wstein.github.io/sourcesteward/schemas/v1/release-config.schema.json",
  "title": "SourceSteward release contract (schema 1)",
  "type": "object",
  "additionalProperties": false,
  "required": ["schema_version", "project", "version", "build", "target", "outputs", "reproducibility", "allowlist", "ci"],
  "properties": {
    "schema_version": {"const": 1},
    "project": {
      "type": "object", "additionalProperties": false, "required": ["name"],
      "properties": {
        "name": {"type": "string", "pattern": "^[A-Za-z0-9._ -]{1,64}$"},
        "product": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,31}$"}
      }
    },
    "version": {
      "type": "object", "additionalProperties": false, "required": ["scheme", "source"],
      "properties": {
        "scheme": {"enum": ["semver", "calver", "pattern"]},
        "tag_prefix": {"type": "string", "pattern": "^([a-z0-9._-]+/)*$", "maxLength": 64},
        "tag_template": {"type": "string", "pattern": "^[A-Za-z0-9._/{}-]{1,128}$"},
        "tag_pattern": {"type": "string", "minLength": 1, "maxLength": 256},
        "monotonicity": {"enum": ["strict", "non-decreasing"]},
        "order": {"type": "array", "items": {"type": "string", "pattern": "^[a-z][a-z0-9_]{0,15}$"}, "minItems": 1, "maxItems": 8},
        "source": {"enum": ["input", "file"]},
        "file": {"$ref": "#/$defs/path"},
        "file_pattern": {"type": "string", "minLength": 1, "maxLength": 256},
        "file_form": {"enum": ["with-v", "without-v"]}
      },
      "allOf": [
        {"if": {"properties": {"scheme": {"const": "pattern"}}}, "then": {"required": ["tag_pattern", "tag_template", "order"], "not": {"required": ["tag_prefix"]}}, "else": {"not": {"anyOf": [{"required": ["tag_pattern"]}, {"required": ["tag_template"]}, {"required": ["order"]}, {"required": ["monotonicity"]}]}}},
        {"if": {"properties": {"source": {"const": "file"}}}, "then": {"required": ["file", "file_pattern", "file_form"]}, "else": {"not": {"anyOf": [{"required": ["file"]}, {"required": ["file_pattern"]}, {"required": ["file_form"]}]}}}
      ]
    },
    "build": {
      "type": "array", "minItems": 1, "maxItems": 16,
      "items": {
        "type": "object", "additionalProperties": false, "required": ["id", "scope", "argv"],
        "properties": {
          "id": {"$ref": "#/$defs/id"},
          "scope": {"enum": ["per-target", "once"]},
          "argv": {"$ref": "#/$defs/argv"},
          "env_inherit": {"type": "array", "maxItems": 32, "items": {"type": "string", "pattern": "^[A-Z_][A-Z0-9_]{0,63}$"}},
          "env_set": {"type": "array", "maxItems": 64, "items": {"type": "string", "pattern": "^[A-Z_][A-Z0-9_]{0,63}=[^\\x00]{0,1000}$"}},
          "working_dir": {"$ref": "#/$defs/path"},
          "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 7200},
          "network": {"enum": ["none", "fetch"]},
          "network_hosts": {"type": "array", "minItems": 1, "maxItems": 64, "items": {"type": "string", "pattern": "^[a-z0-9][a-z0-9.-]{0,252}$"}},
          "lock_files": {"type": "array", "maxItems": 16, "items": {"$ref": "#/$defs/path"}}
        },
        "allOf": [
          {"if": {"properties": {"network": {"const": "fetch"}}, "required": ["network"]}, "then": {"required": ["network_hosts"]}, "else": {"not": {"required": ["network_hosts"]}}}
        ]
      }
    },
    "target": {
      "type": "array", "minItems": 1, "maxItems": 64,
      "items": {
        "type": "object", "additionalProperties": false, "required": ["id", "kind", "build"],
        "properties": {
          "id": {"$ref": "#/$defs/id"},
          "kind": {"enum": ["file", "oci-image"]},
          "role": {"enum": ["artifact", "manifest", "metadata", "sbom"]},
          "group": {"$ref": "#/$defs/id"},
          "build": {"$ref": "#/$defs/id"},
          "os": {"type": "string", "pattern": "^[a-z0-9_]{1,16}$"},
          "arch": {"type": "string", "pattern": "^[a-z0-9_]{1,16}$"},
          "output": {"$ref": "#/$defs/pathTemplate"},
          "runner": {"type": "string", "pattern": "^[A-Za-z0-9._-]{1,64}$"},
          "smoke_argv": {"$ref": "#/$defs/argv"},
          "repository": {"type": "string", "pattern": "^[a-z0-9][a-z0-9._/-]{0,254}$"},
          "platforms": {"type": "array", "minItems": 1, "maxItems": 32, "items": {"type": "string", "pattern": "^[a-z0-9_]+/[a-z0-9_]+(/[a-z0-9_.]+)?$"}},
          "staging_tag": {"type": "string", "pattern": "^[A-Za-z0-9_.{}-]{1,128}$"},
          "result_file": {"$ref": "#/$defs/pathTemplate"},
          "lock_file": {"$ref": "#/$defs/path"},
          "tool_list_file": {"$ref": "#/$defs/path"},
          "source_offer": {"type": "string", "minLength": 1, "maxLength": 512}
        },
        "allOf": [
          {"if": {"properties": {"kind": {"const": "file"}}}, "then": {"required": ["os", "arch", "output"], "not": {"anyOf": [{"required": ["repository"]}, {"required": ["platforms"]}, {"required": ["staging_tag"]}, {"required": ["result_file"]}, {"required": ["lock_file"]}, {"required": ["tool_list_file"]}, {"required": ["source_offer"]}]}}},
          {"if": {"properties": {"kind": {"const": "oci-image"}}}, "then": {"required": ["repository", "platforms", "staging_tag", "result_file"], "not": {"anyOf": [{"required": ["os"]}, {"required": ["arch"]}, {"required": ["output"]}]}}}
        ]
      }
    },
    "outputs": {
      "type": "object", "additionalProperties": false,
      "properties": {
        "checksums": {"type": "string", "pattern": "^[A-Za-z0-9._-]{1,64}$"},
        "url_template": {"type": "string", "maxLength": 512}
      }
    },
    "reproducibility": {
      "type": "object", "additionalProperties": false, "required": ["policy", "source_date_epoch"],
      "properties": {"policy": {"enum": ["required", "best-effort", "none"]}, "source_date_epoch": {"enum": ["commit-time", "none"]}}
    },
    "allowlist": {
      "type": "object", "additionalProperties": false, "required": ["paths"],
      "properties": {"paths": {"type": "array", "maxItems": 256, "items": {"type": "string", "pattern": "^[A-Za-z0-9._*/-]{1,256}$"}}}
    },
    "ci": {
      "type": "object", "additionalProperties": false, "required": ["workflows", "release_workflow"],
      "properties": {
        "workflows": {"type": "array", "minItems": 1, "maxItems": 8, "items": {"type": "string", "pattern": "^[A-Za-z0-9._-]{1,64}\\.ya?ml$"}},
        "release_workflow": {"$ref": "#/$defs/path"}
      }
    },
    "readiness": {
      "type": "object", "additionalProperties": false, "required": ["files"],
      "properties": {"files": {"type": "array", "minItems": 1, "maxItems": 32, "items": {"$ref": "#/$defs/path"}}}
    },
    "promote": {
      "type": "object", "additionalProperties": false, "required": ["argv"],
      "properties": {"argv": {"$ref": "#/$defs/argv"}, "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 7200}}
    }
  },
  "$defs": {
    "id": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,31}$"},
    "path": {"type": "string", "pattern": "^[A-Za-z0-9._][A-Za-z0-9._/-]{0,255}$"},
    "pathTemplate": {"type": "string", "pattern": "^[A-Za-z0-9._{][A-Za-z0-9._/{}-]{0,255}$"},
    "argv": {"type": "array", "minItems": 1, "maxItems": 64, "items": {"type": "string", "minLength": 1, "maxLength": 4096}}
  }
}
```

Semantic checks the schema cannot express (all `check` findings; `calver` also requires a real calendar date, so `2026.02.31` is `scheme-invalid` at version validation, and each `pattern` group listed in `order` must be able to match only strings of 1 to 9 decimal digits, checked on the parsed RE2 tree: digit literals, digit classes, alternations and concatenations of those and bounded repeats such as `[0-9]{4}` or `[0-9]{1,9}`; a `pattern` product has **no calendar validation** unless its expressions encode it, as `calver` does): no `..` path segment and no leading `/` in any path field; `tag_pattern` compiles (RE2), has the shape required by the table above (literal prefix and suffix equal to the template's, one top-level `version` group). **Literal** is defined on the parsed RE2 syntax tree (`regexp/syntax`): the prefix and suffix consist only of literal nodes, so an unescaped `.` or any other metacharacter in them is `scheme-invalid`, and a text comparison is not enough (measured: `zlib.ng-v` passes a text comparison with the template and matches `zlibXng-v`). Every member of a monotonicity set must satisfy `tag == tag_for(version_of(tag))` and contains every `order` name as a named group nested in `version`; `fetch` builds with `reproducibility.policy = "required"` must list `lock_files` (`fetch-unpinned`); with an `oci-image` target, `[promote].argv` contains `{digest}` and `{version}` or `{version_number}` and no literal `latest`, `stable`, `edge`, `main` or `master` element (`promote-argv-invalid`; `stw` cannot see what the project's tool does beyond its argv); `product-tag-overlap` (above; `product-required` and `product-unknown` are refusals of the commands that take `--product`); `file_pattern` compiles with exactly one capture group; placeholder closure; references and unique ids; `once` builds only use the allowed placeholders; every `oci-image` target requires the `[promote]` table and vice versa; `env_inherit` and `env_set` names are not forbidden (`env-name-forbidden`); allowlist overlap rules above. Schema violations of the same document are reported once each, ordered by JSON pointer.

## Adapters

`build.argv` is the **own-script adapter**: the project command is a script or tool it already has (`scripts/release.sh`, `mix release`, `cargo build`, `make`, `just`, `mise`; none is mandated or needed). GoReleaser, cargo-dist and similar tools are optional later adapters; a later schema revision would add `build.adapter`. Their behavior that matters here (GoReleaser publishing from `dist`, `GORELEASER_CURRENT_TAG` handling) is unverified and not relied on.

## Compatibility

New files and new schemas; no existing contract changes. Unknown properties are rejected at `schema_version: 1`; a later `schema_version` is refused by `v1` readers with `schema-violation`.
