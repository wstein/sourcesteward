# Specification Quality Checklist: Release process v2

**Purpose**: Author review of specification completeness before analysis.
**Created**: 2026-10-17
**Feature**: [Specification](../spec.md)

## Content, completeness and readiness

- [x] Focused on user value: releasing any project once, reviewably and verifiably
- [x] All mandatory sections completed
- [x] No unresolved clarification markers remain (open questions are listed with recommendations in the handoff and in analysis.md)
- [x] Requirements are testable and unambiguous (one normative table each for commands, codes, version schemes, build environment, backend capability, limits and exit statuses)
- [x] Success criteria are measurable and describe observable outcomes
- [x] Acceptance scenarios and edge cases are defined (rebase merge, moved main, replaced draft asset, hostile text, readiness)
- [x] Scope bounded: package channels, Windows, signing, adapters other than own-script, network enforcement for fetch builds and readiness execution are out of scope
- [x] Multiple products per repository and fetch-type builds specified with per-command treatment (D9)
- [x] Dependencies identified: Specs 020, 021, 022, 025; the constitution amendment proposal; unverified WorkHarbor and CrewBook policies

## Constitution alignment

- [x] I. Text and JSON from one result, no prompts, versioned schemas
- [x] II. Plan then apply for the only remote write; `not-run` never passes; a plan is not authorization
- [x] III. Only one new local file; remote effects disclosed with recovery limits; tags never re-created
- [x] IV. Failing tests first; hostile, concurrent and limit cases specified; effect sentinels; jj verification gap stated
- [x] V. Capability per backend stated; unsupported capability refuses; no fallback to raw Git in jj
- [ ] Scope: release automation needs a constitutional amendment, proposed separately and not applied (blocks WI6 to WI11)

## Artifacts

- [x] Five worked examples (six contract files, six records) validated against the schemas by a scratch program, with ten negative mutants
- [x] Every Git claim about ancestry, diffs, filters and tags was run; every GitHub and jj claim is marked unverified
- [x] A planned entry in docs/feature-status.json (renamed from 026-release-hygiene)

## Notes

The author review is not independent analysis. Independent analysis is task T001 and precedes implementation.
