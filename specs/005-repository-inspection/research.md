# Research: Repository Inspection

## Decisions
- Use documented rev-parse identity queries and symbolic-ref/rev-parse for HEAD. Avoid status and index-related inspection because read-only claims must include index refresh effects.
- Preserve path values including newlines by separate calls and removing exactly one output terminator. NUL parsing is not available consistently across these identity queries.
- Strip inherited GIT_* and set explicit no-lock/no-lazy-fetch/no-prompt controls, while preserving normal system/global ownership configuration. Do not neutralize safe.directory by disabling all config or whitelisting paths.
- Detect recognized jj/GitButler markers without executing their CLIs. Refuse managed backends in this slice. Evidence is a bounded detection heuristic, not an ownership proof for arbitrary future versions.
- Sample HEAD twice. This detects observed concurrent changes, with an explicit ABA/transaction limitation.

## Alternatives considered
A universal adapter interface would add complexity before supported backend contracts exist. Shelling out to status would violate the bounded identity scope. Calling jj could snapshot working copies; calling GitButler could have forge/metadata effects. Returning untyped stderr would weaken the stable agent contract.

## Primary references
- [Git rev-parse](https://git-scm.com/docs/git-rev-parse): identity queries, path-format and object format.
- [Git symbolic-ref](https://git-scm.com/docs/git-symbolic-ref): symbolic HEAD and detached exit behavior.
- [Git environment](https://git-scm.com/docs/git): routing, optional locks and lazy-fetch controls.
- [Git configuration](https://git-scm.com/docs/git-config): safe.directory ownership protection.
- [Backend readiness](../../docs/backends.md): existing project effect and ownership boundaries.

## Independently validated marker boundaries
The reviewer verified current primary documentation on 2026-10-15:
- [jj CLI reference](https://docs.jj-vcs.dev/latest/cli-reference/): `.jj` ancestor discovery, working-copy snapshots and divergent operation reconciliation. Do not run jj in this feature.
- [GitButler recovery](https://docs.gitbutler.com/troubleshooting/recovering-stuff): `.git/gitbutler` and `refs/gitbutler/*` evidence.
- [GitButler workspace branch](https://docs.gitbutler.com/workspace-branch): `gitbutler/workspace` and installed hooks.
- [GitButler current upstream CLI reference](https://github.com/gitbutlerapp/gitbutler/blob/master/crates/but/skill/references/reference.md): experimental lazy single-branch registration may lack a workspace branch. Detection cannot prove absence.
- [Git show-ref](https://git-scm.com/docs/git-show-ref): --exists distinguishes absent2 from lookup-error1; require Git2.46+ rather than imitating this with --verify.

Marker rules, nested repository boundary, symlink refusal and exact ref-prefix filtering are locked in the plan. HEAD's absent symbolic branch is distinguished from lookup errors and missing/noncommit objects. Upstream [show-ref source](https://github.com/git/git/blob/master/builtin/show-ref.c) confirms --verify conflates missing/corrupt errors, so use --exists and a Git2.46+ floor. Fixed rev-parse, symbolic-ref, show-ref and for-each-ref are built-ins and do not execute hooks/fsmonitor; no status/diff/log is used. Identity queries require Git 2.46+ compatibility.
Research is design evidence, not an assertion that native adapters exist. Independent artifact clearance remains the implementation gate.
