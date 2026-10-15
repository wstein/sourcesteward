# Research and Decisions

## Native release/capability baseline
**Decision**: Mandatory conformance uses Git2.46.0/2.56.0 and jj0.45.1/0.46.0, exact versions/checksums in the implementation manifest.
**Rationale**: Git2.46 introduces the reviewed `show-ref --exists` distinction; jj0.45.1 and0.46.0 expose required scalar JSON templates. No map/object-literal template is assumed because that is new in0.46.
**Alternatives**: Floating latest/runner versions cannot establish reproducible minimum/current coverage. Four cross-pair jobs add no adapter-specific evidence beyond two paired lanes in this bounded slice.
**Sources**: [Git official tags](https://github.com/git/git/tags), [jj0.45.1](https://github.com/jj-vcs/jj/releases/tag/v0.45.1), [jj0.46.0](https://github.com/jj-vcs/jj/releases/tag/v0.46.0), [jj CLI](https://docs.jj-vcs.dev/latest/cli-reference/). Installer authors verify primary release bytes/hashes before commits.

## Read-side effects require enforcement
**Decision**: Native jj runs only inside enforced read-only Linux bubblewrap or macOS sandbox-exec; absent/denied capability is an actionable failure, never an unsandboxed retry. The OS sandbox denies network and filesystem writes, preserving no-target-write inspection semantics. macOS additionally enforces a process executable allowlist; Linux bubblewrap does not enforce or claim helper-execution denial. Helpers are prevented separately through trusted fixed native identity built-ins, exclusion of all configuration text/templates and tested sentinel cases.
**Rationale**: Pinned jj source can persist missing/corrupt index reconstruction in `get_index_at_op` and missing type-file compatibility migration in `read_store_type_compat`, even during reads. Snapshot suppression and operation pinning do not prevent these. Preflight validation cannot close races or establish a general zero-write guarantee. Both index/type-repair fixtures are required acceptance cases.
**Alternatives**: Weakening read-only claims violates the feature contract; copying repositories changes identity/performance and introduces snapshot races; preflight-only checks are insufficient; `--no-integrate-operation` can still write unpublished operation objects.
**Sources**: [jj0.46 index store](https://github.com/jj-vcs/jj/blob/v0.46.0/lib/src/default_index/store.rs), [jj0.46 repository loader](https://github.com/jj-vcs/jj/blob/v0.46.0/lib/src/repo.rs), [bubblewrap](https://github.com/containers/bubblewrap). macOS profile experiment and Linux namespace availability must be evidenced by the implementation, without changing host security settings.

## Stable operation and unsnapshotted state
**Decision**: Every repository invocation includes `--ignore-working-copy`, `--no-pager`, `--color=never` and explicit operation selection. Initially use `--at-operation=@`, then pin remaining queries to its full128hex operation. Re-observe operation/workspace identity for visible concurrent changes. Success always says stored-unsnapshotted with freshness/transaction limitations; no boolean “fresh” is fabricated.
**Rationale**: `--at-operation=@` alone still allows snapshots; `--ignore-working-copy` alone can allow automatic divergent-operation merging. Explicit operation pinning plus snapshot suppression and enforcement are all needed. Workspace operation identity is not assumed equal to latest repository operation.
**Alternatives**: Ordinary jj reads, automatic workspace updates and parsing human output violate effect/contract requirements.
**Sources**: [jj0.46 CLI helper](https://github.com/jj-vcs/jj/blob/v0.46.0/cli/src/cli_util.rs), [CLI reference](https://docs.jj-vcs.dev/latest/cli-reference/).

## Workspace and identity discovery
**Decision**: Use native workspace-root queries and explicit scalar JSON templates; resolve secondary `.jj/repo` UTF-8 pointer files as well as primary repository directories. Report full32reverse-hex change,40hex commit and128hex operation IDs separately.
**Rationale**: Secondary workspace `.jj/repo` is a relative-path text pointer, not a symlink/directory. Native identities cannot be substituted with colocated Git HEAD. Bounded native workspace listing, if necessary, is used only to match the selected root and never exposes unrelated workspace records.
**Alternatives**: Assuming `.jj/repo` always a directory rejects valid secondary workspaces; parsing generated descriptions/short IDs or universalizing branch/bookmark semantics loses meaning.

## Configuration and machine interfaces
**Decision**: Fixed builtin argv, explicit scalar JSON templates and safe environment; strip inherited JJ_*/GIT_* injections. Set `JJ_CONFIG=` and fresh owned `XDG_CONFIG_HOME`; repository/workspace secure configuration metadata must be isolated too. Read bounded regular 20-hex repository/workspace config-id files and copy only their corresponding opaque `metadata.binpb` records into isolated `jj/repos|workspaces/<id>` locations, excluding all config.toml. No ID requires no copy; legacy/direct/missing/mismatched records fail closed, never auto-migrate. Reject symlinks and bound inputs64KiB/directory entries128/pointer hops8. The primary researcher verified genuine scalar JSON succeeds with malicious json alias in original config omitted. Both pinned releases must be covered by implementation fixtures. No helper invocation, raw stderr or arbitrary templates are permitted. Fixed built-ins/config exclusion establish helper non-invocation; sandboxing enforces writes/network, with an additional executable allowlist on macOS only.
**Rationale**: There is no environment switch that suppresses every repository/workspace config layer. Trusted fixed commands are insufficient without effects enforcement.
**Alternatives**: Inheriting caller routing/config/trace or believing `JJ_CONFIG=` suppresses local config would produce false isolation claims.

## Schema compatibility
**Decision**: Keep version/Git schema1 files and output unchanged. Selected native jj results use schema2 with `repository:null` and optional `jj_repository`, complete on success and absent on error. Ambiguous/GitButler/pre-selection results remain schema1. No CLI contract-version flag.
**Rationale**: Existing v1 schema fixes successful backend to Git. Its documented compatibility rules require a new version for backend/result alternatives. Keeping an optional separate typed jj model avoids modifying Git identity fields or serialization.
**Alternatives**: Widening v1 silently breaks published validation expectations; forcing all Git callers to v2 adds unnecessary migration.
