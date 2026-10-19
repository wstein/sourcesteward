# Hosting SDK dependency provenance

Pinned SDK: github.com/cli/go-gh/v2 v2.16.2 (official GitHub CLI SDK).
Only pkg/api is used; native discovery and bounded authentication remain owned by SourceSteward.

This hosting compile closure is pinned in go.mod/go.sum; no third-party source is vendored.
Identical license terms are stored once. MIT copyright notices are collected with the
shared permission/disclaimer text; Go modules share their byte-identical BSD license
and patent grant. Distinct safeexec, timezone and YAML terms/notices remain intact.
This hosting-specific inventory does not replace the license inventory needed for
any future full binary distribution, including existing Cobra/schema dependencies.

| Module | Version | Retained attribution and terms |
| --- | --- | --- |
| github.com/aymanbagabas/go-osc52/v2 | v2.0.1 | [MIT](licenses/MIT.txt) |
| github.com/cli/go-gh/v2 | v2.16.2 | [MIT](licenses/MIT.txt) |
| github.com/cli/safeexec | v1.0.1 | [BSD-2](licenses/BSD-2-safeexec.txt) |
| github.com/cli/shurcooL-graphql | v0.0.4 | [MIT](licenses/MIT.txt) |
| github.com/henvic/httpretty | v0.2.0 | [MIT](licenses/MIT.txt) |
| github.com/lucasb-eyer/go-colorful | v1.4.1 | [MIT](licenses/MIT.txt) |
| github.com/mattn/go-isatty | v0.0.24 | [MIT](licenses/MIT.txt) |
| github.com/muesli/termenv | v0.16.0 | [MIT](licenses/MIT.txt) |
| github.com/rivo/uniseg | v0.4.7 | [MIT](licenses/MIT.txt) |
| github.com/thlib/go-timezone-local | v0.0.9 | [Unlicense](licenses/Unlicense.txt); upstream is dual-licensed Unlicense OR Apache-2.0 and SourceSteward relies on the Unlicense, so no Apache text is retained |
| golang.org/x/sys | v0.49.0 | [BSD-3](licenses/BSD-3-Go.txt), [patent grant](licenses/Go-PATENTS.txt) |
| golang.org/x/term | v0.47.0 | [BSD-3](licenses/BSD-3-Go.txt), [patent grant](licenses/Go-PATENTS.txt) |
| golang.org/x/text | v0.41.0 | [BSD-3](licenses/BSD-3-Go.txt), [patent grant](licenses/Go-PATENTS.txt) |
| gopkg.in/yaml.v3 | v3.0.1 | [MIT/Apache terms](licenses/yaml-LICENSE.txt), [NOTICE](licenses/yaml-NOTICE.txt) |
