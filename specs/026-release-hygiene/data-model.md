# Data model: Release hygiene

Gate ids and waivability are defined once in the [spec gate table](spec.md#release-gates-normative) and codes and exit statuses in the [code table](spec.md#codes-normative); `scripts/release-gates.json` mirrors them and a test compares the two. Platforms, evidence classes, approvals and smoke ids are defined in the spec and not repeated here. All text fields are bounded (at most 2,000 bytes) and are data.

## PlanResult (`release.py plan --json`, `tool_schema: 1`, not a `schemas/v1` file)

| Field | Type | Notes |
| --- | --- | --- |
| `tool_schema` | integer | `1` |
| `command` | string | `plan`, `build` or `verify` |
| `version` | string | as given |
| `commit` | string \| null | full 40-hex code commit |
| `status` | `pass` \| `fail` \| `refused` | `refused` for exit 3 only; usage errors (exit 2) produce no JSON |
| `gates` | Gate[] | one per gate id, sorted by id |
| `platforms` | PlatformResult[] | exactly the three candidate platforms of the platform table (`darwin/arm64`, `linux/amd64`, `linux/arm64`), sorted; out-of-scope platforms are never listed |
| `effects` | Effects | what ran and what was written |
| `error` | `{code, message}` \| null | refusals only |

### Gate

| Field | Type | Notes |
| --- | --- | --- |
| `id` | string | `G01`..`G14` |
| `outcome` | `pass` \| `fail` \| `not-run` \| `waived` | |
| `codes` | string[] | codes from the spec code table; empty on pass |
| `evidence` | string \| null | evidence class or `null` |
| `detail` | string | bounded |

### PlatformResult

| Field | Type | Notes |
| --- | --- | --- |
| `platform` | string | `linux/amd64`, ... |
| `included` | boolean | false for a platform without required evidence |
| `reason` | string \| null | `evidence-missing` when not included |
| `evidence` | string[] | classes found in the record |
| `execution` | `run` \| `not-run` \| `n/a` | `verify` only; `not-run` for a foreign platform |

### Effects

| Field | Type | Notes |
| --- | --- | --- |
| `linked_binary` | boolean | false for `plan` |
| `network` | boolean | false always for `plan`, `build`, `verify` |
| `wrote` | string[] | paths, relative; empty for `plan` |
| `commands` | string[] | argument vectors that were run, bounded to 50 |

## ReleaseManifest (`release-manifest.json`, `manifest_schema: 1`)

| Field | Type | Notes |
| --- | --- | --- |
| `manifest_schema` | integer | `1` |
| `name` | string | `SourceSteward` |
| `version` | string | version format |
| `commit` | string | 40-hex code commit |
| `source_date_epoch` | integer | commit time |
| `go_version` | string | `go1.27.2` |
| `env` | object | exactly the pinned recipe variables; no caller paths (`HOME`, `GOMODCACHE`, `GOCACHE`, `PATH` are excluded) |
| `args` | string[] | the `go build` arguments with `<out>` placeholders |
| `artifacts` | Artifact[] | sorted by platform |
| `signing` | `{"signed": false, "notarized": false, "attestation": false}` | stated, not implied |
| `sbom` | string \| null | file name if produced |

### Artifact

| Field | Type | Notes |
| --- | --- | --- |
| `platform` | string | `os/arch` |
| `binary_sha256` | string | reproducibility subject |
| `archive` | string | file name |
| `archive_sha256` | string | listed in `SHA256SUMS` |
| `stamp_verified` | `execution` \| `none` | the stamp is verified only by running the binary; build information cannot show `-ldflags` under `-trimpath` |

## ReadinessRecord

Markdown with fixed sections, defined in [contracts/readiness-record.md](contracts/readiness-record.md). Entries: Gate row (id, outcome, evidence class, reference, date), Approval row (A-id, date, who, verbatim quote, named values), Platform row, NotRun list.

## Example JSON

```json
{
  "tool_schema": 1,
  "command": "plan",
  "version": "v0.1.0",
  "commit": "0123456789abcdef0123456789abcdef01234567",
  "status": "fail",
  "gates": [
    {"id": "G07", "outcome": "fail", "codes": ["notices-incomplete"], "evidence": null, "detail": "github.com/spf13/cobra v1.10.2 has no entry in third_party/README.md"},
    {"id": "G09", "outcome": "pass", "codes": [], "evidence": "local-recorded", "detail": "go1.27.2"}
  ],
  "platforms": [
    {"platform": "darwin/arm64", "included": false, "reason": "evidence-missing", "evidence": [], "execution": "n/a"},
    {"platform": "linux/amd64", "included": true, "reason": null, "evidence": ["ci"], "execution": "n/a"},
    {"platform": "linux/arm64", "included": false, "reason": "evidence-missing", "evidence": [], "execution": "n/a"}
  ],
  "effects": {"linked_binary": false, "network": false, "wrote": [], "commands": ["go version"]},
  "error": null
}
```
(Gate list abbreviated; a real result has all fourteen.)
