# Contract: release readiness record and release notes

Files: `docs/releases/<version>-readiness.md` and `docs/releases/<version>.md`, committed on branch `codex/release-<version>` (rooted at the code commit; see the release contract). Both are checked by `python3 scripts/check-docs.py` for links (when committed) and are read by `release.py` from `HEAD`'s tree, not from the export of the code commit. A template lives at `docs/releases/TEMPLATE-readiness.md` (WI6). The record states what happened; `pending` placeholders other than the manual post-release items are rejected by `plan --final`. `release.py` parses only the fenced `json` block under `Machine record` ([data-model.md](../data-model.md#machinerecord-fenced-json-block-of-the-readiness-record-record_schema-1)); the prose sections are for people and the reviewer.

## Readiness record sections (in order)

1. **Header**: version, code commit (40-hex), date, author, status, trust anchor (SHA-256 of `SHA256SUMS`).
2. **Toolchain and tools**: `go version`, Git, jj and Python versions, host list (`uname -sm`, OS version).
3. **Platforms**: one row per candidate platform with `included`, evidence classes and references.
4. **Gates**: G01 to G14 with outcome, evidence class, reference (run URL, command and output excerpt, or record section) and date. `not-run` and `waived` rows carry a reason; a waiver names the user's verbatim quote.
5. **Reproducibility**: per platform `match`, `mismatch`, `waived` or `not-run`, the two binary hashes, the two output directories and whether the builds ran on one host or two.
6. **Approvals**: A1 to A4 and any waiver with date, who relayed it, the verbatim authorization and the named values. An A1 quote contains the version, every platform and at least 12 hex characters of the code commit. An entry exists only after the user gave it. The `quote` in the machine block is canonical; prose may repeat it, and the reviewer compares it with the user's message.
7. **Pre-publication verification**: per platform the `verify` outcome, build information outcome, executed stamp (`execution` or `none`), host, evidence class (`local-recorded` or `manual`, never `ci`) and the verified anchor, on the built files before publication.
8. **Not run**: every check that was skipped, with reason (mandatory, may be `none`).
9. **Post-release verification (manual)**: S1 to S5 per platform on the downloaded files with host and outcome; the Mac mini entry for darwin/arm64 is `manual`. Recorded by hand in a later record commit after publication and checked by the reviewer. A3 and A4 are not after-the-fact items: they are recorded in section 6 in a record commit before publication or activation. No script mode requires these items.
10. **Open issues**.

Every evidence entry of every class records the code commit; `manual` entries also record the verified `SHA256SUMS` anchor.

## Release notes sections

1. Version, date, commit, tag.
2. Platforms and files (names, `SHA256SUMS` reference), the evidence class per platform, and each dropped platform with the reason (`evidence-missing`).
3. What is included: a list of feature ids, one per line as `- <id>`, compared by G05 with `docs/feature-status.json` (status `implemented` or `verified` only); hidden commands and planned features are not listed.
4. Requirements and limits: Git 2.46+, jj 0.45.1+ with bubblewrap or `sandbox-exec`, backend capability limits, schema versions.
5. Integrity and trust: checksums unsigned, binaries not signed or notarized, reproducibility status, macOS Gatekeeper note.
6. Known issues and deferred work.
7. Changes since the previous release: Conventional Commit subjects (first release: "initial release").

## Example gate entry

```json
{"id":"G12","outcome":"waived","evidence_class":null,"reference":"waiver:G12","reason":"user quote recorded in approvals"}
```
