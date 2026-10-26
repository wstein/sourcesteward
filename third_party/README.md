# Third-party notices of the stw binary

Every module linked into `stw` for the shipped targets (linux and darwin) is listed below with its
retained license text, plus the Go standard library and runtime. The list is the output of
`go list -deps ./cmd/stw` for both operating systems; `internal/releaseworkflow` fails when a linked
module has no row or a row names a license file that is missing, so a new dependency cannot ship
without a notice. Modules used only by tests or by other operating systems (the schema validator
`santhosh-tekuri/jsonschema/v6`, `inconshreveable/mousetrap`, which Cobra links on Windows only) are
not linked into the shipped binaries and have no row. The release archives contain this directory.

Pinned SDK: github.com/cli/go-gh/v2 v2.17.0 (official GitHub CLI SDK). Only pkg/api is used; native
discovery and bounded authentication remain owned by SourceSteward. No third-party source is vendored;
versions are pinned in go.mod/go.sum. Identical license terms are stored once. MIT copyright notices
are collected with the shared permission/disclaimer text; Go modules share their byte-identical BSD
license and patent grant. Distinct safeexec, pflag, timezone and YAML terms/notices remain intact.

| Module | Version | Retained attribution and terms |
| --- | --- | --- |
| github.com/aymanbagabas/go-osc52/v2 | v2.0.1 | [MIT](licenses/MIT.txt) |
| github.com/cli/go-gh/v2 | v2.17.0 | [MIT](licenses/MIT.txt) |
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
| golang.org/x/text | v0.43.0 | [BSD-3](licenses/BSD-3-Go.txt), [patent grant](licenses/Go-PATENTS.txt) |
| gopkg.in/yaml.v3 | v3.0.1 | [MIT/Apache terms](licenses/yaml-LICENSE.txt), [NOTICE](licenses/yaml-NOTICE.txt) |
| github.com/spf13/cobra | v1.10.2 | [Apache-2.0](licenses/Apache-2.0.txt) |
| github.com/spf13/pflag | v1.0.10 | [BSD-3](licenses/BSD-3-pflag.txt), Copyright (c) 2012 Alex Ogier and The Go Authors |
| Go standard library and runtime | go.mod toolchain | [BSD-3](licenses/BSD-3-Go.txt), [patent grant](licenses/Go-PATENTS.txt) |
