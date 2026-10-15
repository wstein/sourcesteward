# Feature Specification: Wide Tie Branding

**Feature Branch**: `codex/002-wide-tie-brand`
**Created**: 2026-10-15
**Status**: Specified
**Input**: Use the selected wider tie as the logo, create flat SVG assets, update the site/favicon
with navy, orange and white, test small sizes, and publish using atomic Conventional Commits.

## User Scenarios & Testing

### User Story 1 — Recognize the selected identity (P1)
Visitors see the wide navy tie, separated orange knot and white branch consistently in the
website and repository. Independent test: compare the rendered mark with the selected concept.

1. Given a light background, the mark retains all three colors, a broad tie body and circular nodes.
2. Given a dark background, the website uses a light tile so the navy tie stays visible.
3. Given the asset files, they contain flat geometry and no gradients, raster content or external dependencies.

### User Story 2 — Recognize the small icon (P2)
Visitors can distinguish the knot, tie and branch at small sizes. Independent test: inspect
16/24/32/48/64-pixel renders on light/dark backgrounds and the website favicon reference.

1. At 16/24/32 pixels, the favicon has a visible knot/body gap and connected branch with three nodes.
2. The favicon and site logo share the same silhouette and color identities; small-size optical adjustments are allowed.

### User Story 3 — Read and navigate the updated site (P3)
The palette becomes navy/orange/white with readable text and visible keyboard focus.
Independent test: desktop/mobile browser inspection and measured foreground/background contrast.

1. Text and controls meet applicable contrast targets; the base logo orange need not be reused verbatim for small text.
2. The site has no horizontal overflow at 375 pixels and its local assets/links resolve.
3. The published site serves the exact revision whose checks passed, after focused Conventional Commits.

## Requirements
- FR-001: Preserve the user's selected broad tie proportions, orange knot, navy body and white three-node branch.
- FR-002: Provide standalone transparent mark, light-tile logo and small-size favicon as accessible, self-contained SVGs.
- FR-003: Replace turquoise site accents with orange-family tokens; retain readable navy/white surfaces and local fonts.
- FR-004: Validate small sizes, desktop/mobile layouts, contrast, asset packaging and source checks before publication.
- FR-005: Record concept provenance accurately; generated raster sketches are references, not production SVG content.

- FR-006: Supply the WorkHarbor-style full asset inventory (outlined lockups, banner/social preview, PNG/ICO/touch exports), plus truthful CI/Go/license/pre-release badges.
- FR-007: Provide a documented make build target producing bin/stw without publishing a release.

## Success Criteria
- SC-001: All SVGs are flat, locally renderable and recognizable at their documented target sizes.
- SC-002: Small text pairs reach 4.5:1 contrast, and focus indicators remain visible.
- SC-003: At 375 and 1280 pixels, content has no horizontal overflow.
- SC-004: Source checks and independent review pass; live files match the checked publication revision.

## Assumptions
The user explicitly authorizes this branding publication. Existing CLI/backend scope and
workflow approval gates remain unchanged. The mark is drawn from the selected concept, without
copying Git's downloadable artwork; the branch motif is independently constructed.
