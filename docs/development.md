# Development

## Prerequisites and checks
Go 1.27.2+, Git 2.46+, Python 3, Node.js 22+ and pinned Hugo 0.168.0 are required for full development checks.
Node runs dependency-free theme behavior tests; CI selects Node 24. Python stages/checks public documentation and assets; the CLI has no Python dependency. Cobra v1.10.2 is pinned in go.mod/go.sum.

```sh
python3 scripts/install-hugo.py # once, checked official download
make check
go test -race ./...
go run ./cmd/stw version --json
```

`make check` checks formatting, Go tests, vet, module verification, local documentation links,
SVG structure, theme behavior/security, site assets and whitespace. It does not create release artifacts or mutate a
user repository. Tests construct a fresh command for each invocation.

## Git/jj conformance
Native jj fixture tests require stable jj 0.45.1+ and usable Linux bubblewrap or macOS
sandbox-exec. No host security restriction is disabled to make a sandbox pass. Hosts without
usable enforcement fail native inspection safely. Local tests may report explicit missing-tool
or sandbox skips; they are not conformance acceptance. CI sets `STW_REQUIRE_JJ=1`, so missing
executables, enforcement or mandatory cases fail instead of being counted as coverage.

Two pinned CI lanes use Git 2.46.0/jj 0.45.1 and Git 2.56.0/jj 0.46.0. Checked official
artifacts/source hashes are recorded in `scripts/backend-releases.json`. To install one lane
locally on a supported installer host, run `python3 scripts/install-backends.py minimum`
(or `current`) and put its ignored `.tools/conformance/<lane>/git/bin` and
`.tools/conformance/<lane>/jj` directories on PATH. Installation downloads/builds tools;
it is separate from the offline CLI. Run `python3 scripts/smoke-jj-inspection.py --preflight minimum`,
then `STW_REQUIRE_JJ=1 go test -count=1 -race ./...` and
`python3 scripts/smoke-jj-inspection.py`. Use the matching lane name for current versions.

Native inspection leaves target repository/workspace bytes unchanged. It creates and removes
private temporary isolation/probe files outside the target. Effects fixtures include edited
files, stale workspaces, divergent operation heads, configuration helpers and attempted
index/type-file repairs; source checks alone do not establish those guarantees.

## Build the local CLI

```sh
make build
./bin/stw version --json
```

The build target runs `go build -o bin/stw ./cmd/stw`. The ignored bin/ directory contains
local development output; this does not create a release or publish a binary.

## Agent guidance

The [agent guide](agent-guide.md) documents current command and output contracts;
[llms.txt](../llms.txt) indexes troubleshooting sources. Root help links to both raw
main-branch files without fetching them. Installed help remains authoritative for the
installed version. `make check` validates these repository-owned URL destinations
offline, including CLI notice links, along with ordinary local documentation links.
The guidance files remain source documentation and are excluded from the Pages artifact.

## Specification workflow
Feature designs live under specs/. The current native jj slice is
[Spec 008](../specs/008-jj-inspection/spec.md), with plan, tasks, analysis and verification.
The roadmap separates implemented inspection from broader future operations.

Project-local Codex skills were installed with:

```sh
specify init --here --force --non-interactive --integration codex --integration-options='--skills' --script sh
```

Do not rerun `--force` without reviewing managed-file changes. Spec Kit's current feature
pointer is machine-local and ignored. In a fresh clone, set it using the normal Specify flow
or point `SPECIFY_FEATURE_DIRECTORY` at `specs/001-project-foundation` for the setup scripts.
No extension hooks are configured. `specify preset resolve spec-template` resolves the active template.

## Website
The published site is static HTML/CSS with local SVG/font assets and one local theme
script, generated with pinned Hugo. The Theme select follows the system by default or
saves Light/Dark locally when storage is available. Returning to System clears that
preference. Without JavaScript the switch is hidden and CSS follows system appearance.
Visitors need no packages, external assets or service connection.

`docs/index.html` remains the landing source. Public documentation and command reference
are generated through the controlled build below; a raw HTTP server over docs/ previews
only the authored landing, not the final generated routes.

A single GitHub Actions workflow checks every PR and push. Its check job builds and
validates the artifact from the exact run SHA; mandatory minimum/current Git/jj conformance
lanes must also pass before deployment. The deployment job runs only for main
pushes after all required jobs succeed, consuming its identical uploaded artifact without
rebuilding or checking out another revision. Pages uses GitHub Actions as its build source.
There is no PR/manual-dispatch deployment path, CLI release or Git-tag publication.

## Generated public documentation

The site uses pinned Hugo standard 0.168.0, repository-owned templates and local assets.
Install the official binary locally with `python3 scripts/install-hugo.py` (network required,
SHA256 checked); it writes ignored `.tools/hugo` without installing system packages.
Then run `make pages` to generate and validate ignored `dist/pages/` from the current revision.
The destination must not already exist; remove only your previous build output before rebuilding.
Set `HUGO` to an explicitly installed Hugo 0.168.0 executable if preferred. Other runtime
versions are rejected. Linux amd64 and macOS universal official downloads are pinned;
other platforms can supply the same official version manually.

Public guides come from an explicit allowlist, while command reference is generated from
Cobra metadata without executing command callbacks. Hugo shortcodes are rejected in guide
sources, unsafe raw HTML is disabled, and templates cannot execute tools or fetch remote
resources. Internal specifications, progress and agent instructions are not included in the
public artifact. Published schemas retain source bytes. CI validates the exact artifact and
main-only Pages deploys that same artifact after checks pass; `revision.txt` records its SHA.
