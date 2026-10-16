# Contract: release readiness record and release notes

Files: `docs/releases/<version>-readiness.md` and `docs/releases/<version>.md`. Both are checked by `python3 scripts/check-docs.py` for links. A template lives at `docs/releases/TEMPLATE-readiness.md` (WI6). Entries use the vocabulary of the spec; the record states what happened, not what is planned, except in the `pending` placeholders that `plan --final` rejects.

## Readiness record sections (in order)

1. **Header**: version, commit (40-hex), date, author, status (`draft`, `ready`, `released`, `abandoned`).
2. **Toolchain and tools**: `go version`, Git, jj and Python versions, host list (`uname -sm`, OS version).
3. **Platforms**: one row per candidate platform with `included`, evidence classes and references.
4. **Gates**: G01 to G14 with outcome, evidence class, reference (run URL, command and output excerpt, or record section) and date. `not-run` and `waived` rows carry a reason; a waiver names who accepted it.
5. **Reproducibility**: per platform `match`, `mismatch`, `waived` or `not-run`, the two binary hashes and the two build directories.
6. **Approvals**: A1 to A4 with date, who, the verbatim authorization, and the named values. An entry exists only after the user gave it.
7. **Not run**: every check that was skipped, with reason (mandatory, may be `none`).
8. **Post-release verification**: S1 to S5 per platform with host and outcome; the Mac mini entry for darwin/arm64 is `manual`.
9. **Open issues**.

## Release notes sections

1. Version, date, commit, tag.
2. Platforms and files (names, `SHA256SUMS` reference) and the evidence class per platform.
3. What is included: implemented features only (status `implemented` or `verified`); hidden commands and planned features are not listed.
4. Requirements and limits: Git 2.46+, jj 0.45.1+ with bubblewrap or `sandbox-exec`, backend capability limits, schema versions.
5. Integrity and trust: checksums unsigned, binaries not signed or notarized, reproducibility status, macOS Gatekeeper note.
6. Known issues and deferred work.
7. Changes since the previous release: a list of Conventional Commit subjects (first release: "initial release").

## Example row

```markdown
| G12 | waived | n/a | maintainer waiver 2026-10-20: second build not run | 2026-10-20 |
```
