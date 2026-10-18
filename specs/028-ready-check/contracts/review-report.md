# Contract: public review report

## Authenticated binding

Use full `GET .../commits/HEAD/statuses`, never combined `/status`, to obtain status IDs and creator metadata. For the policy's single review context, select the newest status by numeric ID across the complete history before authenticating it. A newer foreign/failing/pending status cannot reveal an older convenient success. It may deny service, but cannot grant eligibility.

The selected status must be `success`, have an allowlisted `creator.id`, and exactly this description grammar:

```text
stw-review-v1 sha256=<64 lowercase hex>
```

Its target URL must match the explicitly selected hosting web origin and repository's PR URL plus `#issuecomment-<positive ID>`. Do not fetch this URL. Extract the bounded numeric comment ID, call the fixed comment reader, validate issue_url against the same repository/PR and validate author ID against `report_author_ids`. Reject missing/deleted/malformed/foreign comment metadata. The entire comment body is one JSON report, with no Markdown wrapper or alternate embedded candidates. Compare SHA-256 of its exact UTF-8 body bytes against the status description.

The status proves an authenticated account published that digest at this head; the comment author proves who supplied the bytes. Neither proves the declared reviewer/model actually executed. The report's policy/head/base/repository/PR bindings must match the sampled policy and PR. Recheck selected status and report on final resample. Source timestamps are informative and mutation detectors, not substitutes for identity binding.

## Fields and semantics

| Field | Meaning |
| --- | --- |
| schema_version / report_id | Version 1 and unique opaque public report token |
| repository_id / number / head_sha / base_sha | Exact full-PR review identity |
| policy_commit / policy_sha256 | Approved immutable policy binding |
| scope / files_sha256 | `full-pr` and complete changed-file scope digest |
| verdict | `clear` or `not-clear`; only clear can pass |
| reviewer | Bounded public reviewer label, not a private session identifier or authenticated issuer proof |
| model | Exact configured ID and effort; evidence is `host-configured` |
| independence | Explicit `declared-execution` with separate-author/context assertions; not runtime attestation |
| rounds | Ordered disclosed reviews, each with ordinal, reviewed head, verdict and finding IDs |
| findings | Unique public IDs, severity, state and optional accepted-follow-up owner/evidence |
| blockers | Explicit remaining project blockers; nonempty blocks review success |

Round ordinals start at 1 and are consecutive, at most 20. The final round binds the current head and matches the report verdict. Earlier rounds may bind old heads; they do not confer clearance on the current one. Count all disclosed `not-clear` rounds against policy max_rounds. All referenced finding IDs exist; all findings appear in at least one round. Duplicate IDs, missing referenced findings and contradictory dispositions fail. The trusted issuer is accountable for disclosure completeness; the parser cannot independently reconstruct omitted rounds.

Finding severities are critical/high/medium/low. States are open/resolved/accepted-followup. Any open finding blocks. Accepted-followup is valid only for Low under an enabling policy, with nonempty owner and evidence, and the final clear report's authenticated issuer acceptance. It never silently deletes the finding or substitutes for unresolved higher severity. Owner/evidence are public bounded text; URLs therein are display data, never fetched. Private transcripts, conversation URLs and agent handles are not required and should not be posted.

Required tier is computed from the baseline and complete changed files, not chosen by the report. The model ID/effort must exactly belong to a tier at least that strong. The result records `configured` evidence and `runtime_attestation=unavailable`. A model string is not proof of actual runtime identity. Declared author/reviewer separation is accepted only under the explicit v1 project policy, and output labels it as an authenticated declaration. No reviewer identity is inferred from commit author email. A future independently attested profile needs another reviewed contract.

## Scope digest

Collect all changed-file rows; match PR changed_files and reject duplicate filenames. Include previous_filename for renames. Sort triples `(filename, previous_filename-or-empty, status)` lexicographically by raw UTF-8 bytes. For each triple append three frames `decimal-byte-length:raw-UTF8-bytes` without separators or normalization; hash the concatenated frames with SHA-256. Lengths use minimal decimal, including `0:`. Empty list hashes the empty byte string. This frames unusual filenames without delimiter collisions or JSON-encoder dependence. The digest binds scope metadata, while exact head/base commits bind the reviewed contents.

## Failure matrix

| Failure | Review component |
| --- | --- |
| Unknown schema/field, duplicate keys, invalid encoding/depth/size, trailing JSON | unknown / report-malformed |
| Duplicate round/finding/report identity or incomplete declared history | unknown / report-history-invalid |
| Wrong repository/PR/head/base/policy/files digest | fail / review-binding-mismatch |
| Edited report/digest mismatch or deleted comment | fail / report-binding-mismatch |
| Missing/foreign status creator or comment author | fail / review-issuer-untrusted |
| Missing model/tier/scope/independence metadata | unknown / report-metadata-missing |
| Unsupported stronger runtime proof required | unknown / independence-unverifiable |
| Not-clear, open finding, invalid follow-up, threshold reached, explicit blocker | fail / review-not-clear |
| Incomplete statuses/files or relevant final-resample drift | unknown / evidence-incomplete or sample-changed |

Known contradictory evidence may produce fail alongside unknown reasons; neither can pass. The report is a new public format; it does not reinterpret or modify strict CrewBook gitnote lines.
