# Wide tie branding verification

## Local acceptance — 2026-10-15

- Started from main 944841322dfb935c3650f94f89a6eb4beaf971fc.
- The initial flat-asset contract failed against the old turquoise mark before implementation.
- make check passed: Go tests, vet, module verification, seven Python tests, documentation,
  architecture boundaries and whitespace. Asset tests cover flat self-contained SVGs,
  native raster dimensions, opaque iOS output, ICO entries, hashes and Pages packaging.
- make build produced bin/stw; ./bin/stw version --json returned schema_version 1,
  name SourceSteward, version dev. bin/ stays ignored.
- Regenerating the 24 production assets reproduced SHA256SUMS without differences.
- Chromium inspected the local site at 1280×800 and 375×812: loaded local fonts/images,
  scrollWidth equalled viewport width, intact controls/layout and visible keyboard skip link.
- Browser review sheet loaded all 30 SVG instances; native-pixel raster sheet inspected
  at 16/24/32/48/64 pixels on white and navy. The optical favicon has stronger strokes;
  at 16 pixels the tie/knot silhouette is clearer than the individual branch nodes.
- Banner, outlined light/dark lockups and social preview inspected without clipping.
- WCAG luminance ratios: #ff8066 on #07192e 7.18:1, on #13315c 5.26:1,
  on #122e48 5.65:1; muted #c1cfdf on #13315c 8.18:1, white on #13315c 12.95:1.
  The inverse primary button is 7.18:1. Logo artwork uses base orange #f05032.
- README badges describe CI, Go requirement, license and pre-release scope; no release
  or backend completion claim is added. CLI behavior remains help/version only.

## Review and publication

Independent review cleared e65220c2677d84a626c6f9ef9bcc43d9e3f26382 with no actionable
findings and independently passing make check. Asset regeneration produced no differences;
go test -race ./... also passed. Exact-source CI/Pages verification remains pending.
Publication uses focused Conventional Commits; revision.txt binds the live site to checked source.

## Limits

Browser acceptance used Chromium, not Safari/Firefox or a physical launcher. App icon
exports are provided; no installable web app or CLI release is introduced.
