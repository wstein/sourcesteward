# Research
Decision: santhosh-tekuri/jsonschema/v6 v6.0.3, test-only.
Rationale: established Go validator supports draft2020-12 and embeds standard metaschemas; no runtime CLI import. Official release page identifies v6.0.3 as latest stable (checked2026-10-15): https://github.com/santhosh-tekuri/jsonschema/releases/tag/v6.0.3 . Standard library has no schema engine. Reimplementing schema validation would add risk; Python validation would add a separate CI environment/dependency layer.
Decision: permissive unknown object fields, constrained existing fields and relationships. This matches005's additive schema1 evolution. Do not enumerate diagnostic codes as a closed set.
Decision: schemas are authoritative source files outside site content;006 packages them using an exact two-file allowlist, and007 preserves /schemas URLs and byte identity.
