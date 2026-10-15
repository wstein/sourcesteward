# Site theme verification

## Scope and review gate
Feature 004 promotes the existing README project-site link and adds a local progressive
appearance selector. Root/coordinator and an independent reviewer reviewed spec/plan/tasks
before implementation. CLI, backend adapters and feature 003 are untouched. No remote
publication, merge or PR creation is performed by the author.

## Failing-first evidence
Baseline `make check` passed (7 Python tests plus Go/source checks). Before implementation,
three new Python tests failed: the labeled selector was absent, the checker rejected the
intended theme script, and packaging rejected theme.js. The Node actual-asset harness failed
because theme.js did not exist. Then the implementation made these tests pass.

## Source and behavior evidence
`make check` passes: formatting, Go tests, vet, module verification, 10 Python cases,
12 Node behavior cases, documentation/architecture checks and whitespace. Node executes the
actual theme asset in an isolated built-in VM; these tests cover first visit, saved light/dark,
selection and reload, system reset, invalid values, storage method exceptions and a blocked
localStorage property getter. CSS owns system preference changes; actual browser evidence
is still required and must not be inferred from the stubs.

Negative source tests reject inline script bodies, missing defer, remote/protocol-relative
sources, a second script, extra script attributes and inline event handlers. Packaging accepts
only top-level assets/theme.js and rejects other/nested JavaScript. Existing symlink rejection,
source exclusion and exact-revision packaging tests remain in place.

## Measured palette contrast
Calculated WCAG relative luminance from authored CSS hex values (rounded ratios):

| Context | Foreground / background | Ratio |
| --- | --- | ---: |
| Light body | #0b2545 / #ffffff | 15.39 |
| Light muted body | #45566c / #ffffff | 7.50 |
| Light orange / primary inverse | #b8331c / #ffffff | 5.95 |
| Light muted on darkest light surface | #45566c / #e5ebf4 | 6.26 |
| Light orange on darkest light surface | #b8331c / #e5ebf4 | 4.97 |
| Light pill | #b8331c / #f6e6e1 | 4.91 |
| Light focus on control surface | #b8331c / #f2f5fa | 5.45 |
| Light control outline | #72849b / #f2f5fa | 3.50 |
| Dark muted on brightest dark surface | #c1cfdf / #13315c | 8.18 |
| Dark orange on brightest dark surface | #ff8066 / #13315c | 5.26 |
| Dark pill | #ff8066 / #122e48 | 5.65 |
| Dark primary | #07192e / #ff8066 | 7.18 |
| Navy terminal muted | #c1cfdf / #0b2545 | 9.71 |
| Navy terminal orange | #ff8066 / #0b2545 | 6.25 |

The terminal scopes its own palette in both page themes; the branch logo remains unchanged.
These measurements cover authored solid color combinations, not a browser pixel audit.

## Coordinator browser acceptance
The coordinator inspected implementation candidate `2ba0a577eb92f5307b07a201d28ed5b6123fc9d5`:

- Both light and dark at 375×812 and 1280×800: no horizontal overflow.
- Additional 720/768 CSS-pixel tablet widths: no overflow; at 720 the header fits and logo remains 42×42.
- Native keyboard selection (letter d, then Tab) chooses Dark and shows visible focus.
- Explicit Dark and Light survive reload. System reset removes data-theme and follows the current light OS appearance.
- An ephemeral no-script HTML copy retaining the original CSS has zero scripts, a hidden/display-none inactive control, readable system-light styling and no overflow at 1280 pixels.

Limit: real OS dark preference and a live OS preference change were not exercised. The CSS
prefers-color-scheme rule was reviewed; Node state tests prove override removal, not browser
media-query behavior. These limits remain explicit rather than inferred from stubs.

## Delivery status
Source and browser acceptance are complete for the tested scope; feature status verified
means local source/UI acceptance, not publication or exhaustive OS/browser coverage.
Independent implementation review and PR/CI handoff are owned by the coordinator. No merge
or Pages deployment is claimed. The next documentation commit records this evidence without
changing implementation behavior.

## PR handoff and cross-artifact analysis

The coordinator opened [PR #5](https://github.com/wstein/sourcesteward/pull/5) for this topic.
PR CI and human review/merge govern further delivery; opening the PR does not publish Pages.

Post-implementation cross-artifact analysis checked spec, plan, tasks and acceptance evidence:
all 7 functional requirements and 4 success criteria have implementation/verification coverage,
and all 10 tasks are complete. No consistency findings remain after narrowing SC-002/SC-003
wording to the measured Node state coverage and separately inspected browser/no-script scope.
Real OS dark preference and live preference changes remain the documented acceptance limitation.
