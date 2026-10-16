# Data model: Release hygiene

Gate ids and waivability are defined once in the [spec gate table](spec.md#release-gates-normative) and codes and exit statuses in the [code table](spec.md#codes-normative); `scripts/release-gates.json` is the single machine-readable source for gate ids, codes, waivable flags and `hidden_commands`, and a test checks it against the code table. Platforms, evidence classes, approvals and smoke ids are defined in the spec and not repeated here. Text fields are data: control characters are removed before printing and each field is truncated at 2,000 bytes with a trailing `…`. `release.py` prints text only; the two JSON documents below are the manifest it writes and the machine block of the record it reads.

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

The manifest never claims a stamp verification: nothing in `build` executes the binary, and the manifest is anchored by `SHA256SUMS`. Stamp verification is reported by `verify` and recorded in the record.

## MachineRecord (fenced `json` block of the readiness record, `record_schema: 1`)

| Field | Type | Notes |
| --- | --- | --- |
| `record_schema` | integer | `1` |
| `version` | string | version format |
| `code_commit` | string | 40-hex |
| `status` | `draft` \| `ready` \| `released` \| `abandoned` | |
| `anchor` | string \| null | SHA-256 of `SHA256SUMS` from `build` |
| `platforms` | PlatformEntry[] | candidate platforms |
| `gates` | GateEntry[] | `G01`..`G14` |
| `reproducibility` | ReproEntry[] | per released platform |
| `approvals` | Approval[] | `A1` to `A4`, waivers |
| `prepublication` | PrePublication[] | per released platform |

| Entry | Fields |
| --- | --- |
| PlatformEntry | `platform`, `included` (boolean), `evidence`: `[{class, commit, reference, date, anchor}]` (`anchor` required for `manual`) |
| GateEntry | `id`, `outcome` (`pass`, `fail`, `not-run`, `waived`), `evidence_class` (or `null`), `reference`, `reason` (for `not-run` and `waived`) |
| ReproEntry | `platform`, `outcome` (`match`, `mismatch`, `waived`, `not-run`), `binary_sha256`: two hashes, `hosts`: one or two host strings |
| Approval | `id` (`A1`..`A4` or `waiver:G12`), `date`, `who` (who relayed it), `quote` (verbatim), `commit`, `version`, `platforms`, `tag` (A2/A3) |
| PrePublication | `platform`, `verify` (`pass`, `fail`, `not-run`), `buildinfo` (`pass`, `fail`), `stamp` (`execution`, `none`), `host`, `evidence_class`, `anchor` |

`plan --final` checks: all evidence and A1 commits equal `code_commit`; every included platform has a passing PrePublication entry whose `anchor` equals `anchor`; `reproducibility` is `match` (equal hashes) or `waived` with a waiver approval; A1 and A2 quotes contain the version, every platform (A1) or the tag (A2), and at least 12 hex characters of the commit.

## Example (abbreviated, `plan --final` report lines)

```text
G07 fail notices-incomplete  github.com/spf13/cobra v1.10.2 has no entry in third_party/README.md
G09 pass
platform linux/amd64 included (ci)
platform linux/arm64 dropped evidence-missing
plan: fail (1 gate)
```

```json
{"record_schema":1,"version":"v0.1.0","code_commit":"0123456789abcdef0123456789abcdef01234567","status":"draft","anchor":null,"platforms":[{"platform":"linux/amd64","included":true,"evidence":[{"class":"ci","commit":"0123456789abcdef0123456789abcdef01234567","reference":"run URL","date":"2026-10-20","anchor":null}]}],"gates":[{"id":"G09","outcome":"pass","evidence_class":null,"reference":"go1.27.2","reason":null}],"reproducibility":[],"approvals":[],"prepublication":[]}
```
