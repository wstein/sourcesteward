# SourceSteward brand

The user-approved wide tie replaces the original guide-rail mark. A separated orange knot
(#f05032), broad navy tie (#0b2545) and white three-node branch define the identity. All
production vectors use flat geometry: no gradients, filters, raster inserts or external resources.
Circular nodes stay circular; the compact favicon uses a broader body and thicker branch.

## Asset set

| Asset | Use |
| --- | --- |
| docs/assets/logo.svg / logo.png | 256×256 white rounded tile; header, README icon, dark surfaces |
| docs/assets/logo-mark.svg / logo-mark.png | Transparent standalone mark; light surfaces only |
| docs/assets/logo-light.svg / logo-light.png | Outlined horizontal lockup on light backgrounds |
| docs/assets/logo-dark.svg / logo-dark.png | Outlined horizontal lockup on dark backgrounds |
| docs/assets/wordmark-light.svg / wordmark-dark.svg | Standalone outlined lettering |
| docs/assets/banner.svg / banner.png | 1000×150 README banner |
| docs/assets/social-preview.svg / social-preview.png | 1280×640 social/Open Graph preview |
| docs/assets/favicon.svg | Optically strengthened small tile icon |
| docs/assets/favicon-16/24/32/48/64.png | Native-size favicon exports |
| docs/assets/favicon.ico | 16/32/48/64-pixel browser fallback |
| docs/assets/apple-touch-icon.png | Opaque 180×180 white square; iOS supplies its own mask |
| docs/assets/android-chrome-192.png / android-chrome-512.png | Opaque launcher-sized square exports |

Paths in this table are repository-relative. View [the review sheet](preview.html) or
[native-pixel raster evidence](small-sizes.png). [SHA256SUMS](SHA256SUMS) identifies every
production SVG/PNG/ICO asset. The preview sheet is not shipped in the Pages artifact.

## Typography and palette
Bricolage Grotesque Bold supplies outlined brand lettering; IBM Plex Sans supplies outlined
taglines and website body text. Local font files and adjacent SIL OFL notices are retained
in docs/assets/fonts/. SVG lockups need no runtime font installation or network access.

Website backgrounds remain navy (#07192e, #0b2545, #13315c), with white text. Small orange text,
links, focus indicators and button fills use the lighter #ff8066 for contrast on navy. The
base #f05032 orange is reserved for the knot and large brand geometry. Hover orange is #ffab97.
Blue-grey borders/secondary text are derived neutral tokens, not additional brand accents.
Never use a transparent navy tie directly on navy; use the white-tile logo instead.

## Provenance and licensing
The wider navy tie/orange knot/white branch direction was developed through AI-generated
raster concept studies and selected by the user on 2026-10-15. Production SVG geometry is
independently drawn from that approved concept; generated pixels are not embedded or traced.
No Git downloadable artwork is copied. Original geometry, generator, CSS and layout are
EUPL-1.2. Font licenses are separate and preserved. Navy and font choices retain the
BundleDock/WorkHarbor family connection; orange is SourceSteward's selected accent.
WorkHarbor's asset inventory/dimensions informed the set, not its repository policies.

## Regenerate

```sh
uv venv .venv-brand
uv pip install --python .venv-brand/bin/python -r brand/requirements.txt
.venv-brand/bin/python brand/generate.py
make check
```

The isolated generator dependencies are pinned, development-only and absent from the CLI.
SVGs are the raster sources; never edit PNG/ICO exports by hand. The generator also produces
brand/small-sizes.png for native-pixel inspection. Changes require a visual check of actual
16/24/32-pixel renders, light/dark usage and accessible site contrast before publication.
