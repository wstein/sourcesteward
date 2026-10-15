# Development

## Prerequisites and checks
Go 1.27.2+, Git, Python 3 and Node.js 22+ are required for development.
Node runs dependency-free theme behavior tests; CI selects Node 24. Python validates documentation
and assets only; the CLI has no Python dependency. Cobra v1.10.2 is pinned in go.mod/go.sum.

```sh
make check
go test -race ./...
go run ./cmd/stw version --json
```

`make check` checks formatting, Go tests, vet, module verification, local documentation links,
SVG structure, theme behavior/security, site assets and whitespace. It does not create release artifacts or mutate a
user repository. Tests construct a fresh command for each invocation.

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
The completed foundation design is in ../specs/001-project-foundation/ (from the repository
root: specs/001-project-foundation). Its spec, plan, research, data model, contract, quickstart
and tasks describe the current milestone. The roadmap describes future work only.

Project-local Codex skills were installed with:

```sh
specify init --here --force --non-interactive --integration codex --integration-options='--skills' --script sh
```

Do not rerun `--force` without reviewing managed-file changes. Spec Kit's current feature
pointer is machine-local and ignored. In a fresh clone, set it using the normal Specify flow
or point `SPECIFY_FEATURE_DIRECTORY` at `specs/001-project-foundation` for the setup scripts.
No extension hooks are configured. `specify preset resolve spec-template` resolves the active template.

## Website
The site is plain HTML/CSS with local SVG and font assets, plus one local theme script.
The Theme select follows the system by default, or saves Light/Dark locally when storage
is available. Returning to System clears that preference. Without JavaScript the switch is
hidden and CSS follows system appearance. No network or package dependency is required. Preview with:

```sh
python3 -m http.server 8000 --directory docs
```

Open http://localhost:8000/. `docs/index.html` and `docs/assets/` are deployed; development
Markdown, scripts, source files and specs are not copied into the Pages artifact.

A single GitHub Actions workflow checks every PR and push. The deployment job runs only
for main pushes, depends on the successful check job in that same workflow run, and checks
out that exact run SHA. Pages uses GitHub Actions as its build source. There is no PR or
manual-dispatch deployment path. Source CI does not publish CLI releases or Git tags.
