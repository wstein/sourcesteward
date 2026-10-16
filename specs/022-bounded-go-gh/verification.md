# Bounded go-gh transport verification

## Baseline and analysis

Baseline main ee86751 passed `PATH=/private/tmp/stw-jj-research/0.46.0:/opt/homebrew/bin:$PATH GOCACHE=/private/tmp/stw-go-cache STW_REQUIRE_JJ=1 make check` on macOS with Git 2.54.0 and jj 0.46.0. This is baseline evidence only.

Independent pre-code analysis and resolved design findings are recorded in [analysis](analysis.md). Implementation, final review and hosted CI acceptance remain pending.
