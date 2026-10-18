# Security Policy

## Supported versions
SourceSteward is pre-release; only the latest main revision receives fixes. No stable release
is supported yet. Current source includes help/version, native Git/jj inspection and
inventory, multi-repository inventory, submodule pin audits, workspace planning and
authorized creation, evidence-based local reference/workspace tidy, and GitHub
repository/PR/CI inspection. The hidden release commands `check`, `plan`, `verify`,
`record`, `build` and `dispatch` are implemented in source; live workflow
acceptance, the release workflows and any published release remain pending. See the [command guide](docs/commands.md),
[backend capabilities](docs/backends.md) and [feature status](docs/feature-status.json)
for capability limits and acceptance evidence.

## Report a vulnerability privately
Use [GitHub private vulnerability reporting](https://github.com/wstein/sourcesteward/security/advisories/new).
Include affected revision, environment, reproducible steps, impact and proposed mitigation
if known. Redact credentials and private repository content. Do not open a public issue with
exploitable details. If the form is unavailable, use contact methods listed on the owner's
GitHub profile; do not send secrets through public channels.

The maintainer will assess reports and coordinate fixes/disclosure; no response-time SLA
is promised. Dependency updates are proposed by Dependabot and require passing checks.

Future backend operations must disclose hooks, network calls and filesystem effects,
validate targets and stale state, and preserve worktree/backend ownership. Repository
content is untrusted data. No prompt, plan or MCP request grants extra permissions.
