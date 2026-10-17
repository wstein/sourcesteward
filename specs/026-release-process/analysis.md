# Pre-implementation analysis

## Round 0

Author draft, 2026-10-17. Not an independent review. This round rewrote the 2026-10-16 "release hygiene" draft (five earlier analysis rounds, recorded in git history of `specs/026-release-hygiene/analysis.md`) because the owner's decisions D1 to D8 replaced its design: a Python script with Go-specific recipe, a markdown readiness record, four approvals and a per-release double build. What was reused and dropped is in [research.md](research.md#reuse-and-drops-from-the-release-hygiene-draft).

Inputs received while writing, all applied before the first review:

| Input | Where applied |
| --- | --- |
| D6: artifact kinds `file` and `oci-image` from v1, per-project version scheme (`semver`, `calver`, pattern), promote means tagging the same digest, activation outside the release | spec D6, FR-003, FR-004, version table; contracts/config.md and record.md (both kinds in the schemas); data-model; build `--mode promote`; examples 2 and 3 |
| D7: bootstrap with the existing tag-workflow pattern, `stw` self-hosts from the next release, `stw` v0.1 implemented right after READY and used for all releasable repositories | spec D7 and FR-024, contracts/bootstrap.md, tasks WI10 and WI11 |
| D8: waves (SheetMill, SourceSteward, BundleDock CLI; BundleDock toolbox bundle, WorkHarbor; CrewBook), several `[[build]]` and `[[target]]` per project, optional `readiness` gate | spec D8, FR-021, FR-022, gate `readiness-missing`; config `[[build]]`, `scope`, `group`, `role`; examples 3 and 4; WorkHarbor release policy and CrewBook criteria recorded as unverified (not read) |

Measurements made in this round are in research.md (rebase ancestry, filter-free raw diff, tomllib leniency, schema validation of four contracts and four records with seven negative mutants, `gh` flag inventory). Not verified: everything about jj (not installed), GitHub draft and attestation behavior, WorkHarbor and CrewBook.

Known weak points the author wants the reviewer to attack: the unverified GitHub behavior list; whether `build` as a `stw` command is the right home for execution (open question 7); attestation binding to C through the attested manifest; the allowlist's reliance on project declaration; the jj capability table; the exit status table (3 for a failed verdict); the readiness gate's honesty; the plan/verify split for tags.

Additional work done in this round after measurement: the four example contracts were found to violate the profile rules the author had just written once (readiness files under an allowlisted `docs/**`), which is why the readiness files in example 4 live outside the allowlist; the regular expression `\u0000` and repeat counts above 1000 in the first schema draft were rejected by the schema library (RE2) and corrected.
