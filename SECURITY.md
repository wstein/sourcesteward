# Security Policy

## Supported versions
SourceSteward is pre-release; only the latest main revision receives fixes. No stable release
is supported yet. The current CLI implements help/version only; repository adapters and
mutation/recovery operations are planned and must not be assumed safe or available.

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
