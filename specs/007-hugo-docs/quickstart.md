# Validation guide

Run make check for source tests. Install the pinned Hugo with python3 scripts/install-hugo.py
and build using make pages. Serve the resulting ignored artifact locally, checking landing,
docs and reference at desktop/mobile widths and in light/dark/system themes. Verify
revision.txt against git rev-parse HEAD. Compare schema bytes with source when feature 006
is integrated. Confirm specs/, docs/progress.md and .git/ are absent from generated output.
