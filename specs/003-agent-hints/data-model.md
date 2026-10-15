# Data Model: Agent Hints

No persistent data or schema changes.

## Help notice
- Destinations: exactly `https://raw.githubusercontent.com/wstein/sourcesteward/main/docs/agent-guide.md` and `https://raw.githubusercontent.com/wstein/sourcesteward/main/llms.txt`.
- Visibility: root help only, once per invocation.
- Effects: output only; no URL retrieval.
- Failure transition: any write failure → existing runtime/output failure status 1.

## Guidance graph
- Entry nodes: root-help notice, README, knowledge map.
- Document nodes: `docs/agent-guide.md` and root `llms.txt`.
- Validation: raw notice URLs map to existing checkout files; local index links resolve offline.
- Scope claims: only implemented help/version features; future operations explicitly planned.
