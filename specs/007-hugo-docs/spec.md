# Feature 007: Generated public documentation

## User stories
### US1 — Browse trustworthy documentation (P1)
A human opens SourceSteward Pages, follows Documentation, and reads the current CLI
reference and selected guides with the existing logo, local fonts and theme selector.
Acceptance: landing appearance survives; every generated page has navigation, keyboard
focus, system/light/dark selection and no horizontal overflow at 375 and 1280 pixels.

### US2 — Read command reference matching the executable (P1)
A human or agent reads the reference for the actual supported command tree.
Acceptance: generation reads Cobra command metadata without executing repository commands;
help/version exist, future commands appear when registered, and generated output is deterministic.

### US3 — Publish only intended public content (P1)
An owner builds one reviewed revision into a Pages artifact.
Acceptance: selected source Markdown is rendered once, schema JSON files retain exact bytes,
internal specs/progress/harness files stay unpublished, and revision.txt identifies the checked SHA.

## Requirements
- FR001: Pin official Hugo v0.168.0, verify downloaded release archives against committed SHA256 values, and reject a mismatched runtime version.
- FR002: Use repository-owned templates without remote Hugo modules, themes, Sass, Node dependencies, external assets or runtime services.
- FR003: Keep landing content accurate to implemented scope and preserve navy/orange/white identity, local fonts, favicon and accessible theme selector; generated pages use the same styles/script.
- FR004: Render an explicit public-guide allowlist; do not recursively publish docs/, specs/, .git/ or arbitrary repository files.
- FR005: Derive CLI reference from Cobra metadata; do not execute command Run/RunE callbacks or inspect the working repository during generation.
- FR006: Serve schemas/v1/*.schema.json unchanged when present, rejecting symlinks and unexpected files. Both schema files and all selected public guides are mandatory after feature 006 integration.
- FR007: Keep unsafe Markdown HTML disabled, disable template access to environment/commands/network, and escape authored content through Hugo rendering.
- FR008: CI checks and builds the exact source SHA; deployment uploads the identical checked artifact, preserving revision.txt proof. No automatic merges.
- FR009: Source Markdown has one owner; public navigation and links resolve correctly under /sourcesteward/.
- FR010: Tests begin failing, cover allowlisting, symlinks, URL rewriting, offline assets, generated command drift and artifact revision/schema integrity; record source and browser verification.

- FR011: README, landing page and public command catalog enumerate every implemented command family, including native Git/jj workspace/tidy and optional gh jj hosting, with links to generated reference and operation guides.
- FR012: Examples disclose local observation, private plan-state writes, explicitly authorized apply effects and authenticated network reads. Separate planned capabilities from bounded implemented behavior; no implicit deletion, automatic restoration or forge-write claim.

## US4 — Keep implemented scope discoverable (2026-10-16 amendment)
A human sees all currently registered command families rather than an obsolete scaffold-only summary.
Acceptance: README and Pages show help/version, inspect/inventory, all workspace/tidy leaves,
hosting repo/pr/ci and the optional jj-only extension; examples use supported flags and
explicit placeholders. Planned Specs012–019 and GitButler remain separate. Local guide links
resolve to generated Pages routes; public-source allowlisting and schema bytes remain intact.

## Success criteria and exclusions
All local source checks and generated-site checks pass. Independent review and browser
acceptance precede publication. No search, blog, release automation, backend operations,
remote theme downloads or copying of internal agent instructions into public guides.
