# Implementation Plan: Wide Tie Branding

Use native SVG geometry with a 64×64 viewBox. Keep a separate transparent mark, white rounded
tile logo and optically strengthened favicon. No image generator or raster tracing is used for
the production assets. Keep the approved orange knot (#f05032), navy tie (#0b2545), white branch.

Use a lighter orange (#ff8066) for small text/buttons on navy when base orange lacks contrast.
Keep fonts and static HTML/CSS layout. Extend the existing offline checks with a flat-brand
asset contract; use a local browser review sheet for 16/24/32/48/64/128 sizes and both backgrounds.

Constitution gates: PASS — scoped original SVGs, explicit publication authorization, test-first
asset contract, no repository/backend feature changes, source checks and exact-SHA Pages gate.

Tasks: record specification; write a failing asset contract; author assets and provenance;
update website/palette; inspect sizes/layout/focus and measure contrast; run make check;
independent exact-revision review; focused commits; fast-forward publish and verify live revision.

No runtime dependency, public API or CLI behavior changes. A browser-only review sheet stays in
brand/ and is excluded from the Pages artifact. The small-icon check covers actual SVG rendering,
not generated concept thumbnails. Future visual changes must update this evidence.
