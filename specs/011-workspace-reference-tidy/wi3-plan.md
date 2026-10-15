# WI3 Plan: Owned Workspace Retirement

## Goal and dependency
US1/US2/US3: retire exactly owned Spec010 tasks after explicit base/integration/cleanliness evidence,
with distinct registration and filesystem authorizations. Depends on accepted WI2 recovery/journal.
A matching task label/path is insufficient; complete creation plan+journal+task+native identity must agree.
New retirement result contract v2 preserves published tidy v1 bytes and existing workspace APIs.

## Concrete native effects
Git: present worktree registration-only unsupported/retain. Both registration and exact --remove-files
permit native combined worktree remove only after clean full raw/stored disk proof. Admit only the
exact owned Spec010 lock, disclose coupled native lock removal, journal and preserve native
recovery ref first. No force dirty removal, manual metadata deletion or broad prune.
jj: native workspace forget removes exact registration but keeps disk; separate files action is opt-in,
confined Go filesystem removal. Pin native recovery bookmark before forget. No abandon/rawGit writes.
Current execution workspace, source/root workspace, foreign owner/registration and uncertain state retain.

## Filesystem confinement
For Git native parent grants: canonical inode-pinned destination parent and native worktrees parent;
exact target is writable, every preexisting sibling (regular files AND directories) read-only. Add
reviewed sandbox read-only regular-file support; reject symlink/special siblings, oversized list and
unsafe parent/source/store overlaps. Sample parent identities and complete sibling lists before and
after action. Native target/metadata-root deletion cannot use writable bindmounts of just target
on Linux; narrow parent grants are required. New future sibling/ABA races remain disclosed, never
claimed transactionality; no unsandboxed/broad-permission fallback. Independent sandbox sentinel
fixtures must prove regular sibling contents and directories stay read-only in both platforms.
For jj files: canonical exact owned root opened with os.Root; verified complete bounded manifest,
no-follow regular file opens, type/device/inode/content checks before removal. Remove only manifest
files then verified empty directories bottom-up, finally exact root through pinned parent with
identity/list recheck. Do not use unbounded RemoveAll or follow symlinks; late new files keep directory
nonempty and report partial outcome. Native forget and file removal have separate journal outcomes.

## Freshness and integration
Git: compare pinned stored/index/raw disk including ignored/untracked entries, executable types and
all paths. Retain filters/transforms, sparse, symlinks/gitlinks, conflicts, unsupported/unknown nodes.
jj: pinned native full tree path/type/executable/conflict manifest, literal root-file raw byte queries,
confined actual disk comparison including ignored/untracked extras, all sparse paths required.
Native ordinary ancestry is sufficient only with clean disk. Empty jj WC alternative requires exactly
one integrated parent AND complete stored tree equality to that parent AND complete clean disk;
empty alone/multiparent/root/unique tree cannot prove disposal. Operation sampled before/after.
Bound: 1024 entries, 64KiB per content query/file, 1MiB encoded records, apply60seconds; oversize retains.
Revalidate all preconditions per action including after earlier own effects; cancellation cannot imply rollback.

## Gate and review
Pre-design and post-design constitution PASS: exact v2 model/interface native/core author
confirmation and independent WI3 analyze cleared 84455ef. Concrete fields are in contracts/wi3.md.
Any inability to enforce parent/sibling or confined deletion proof blocks WI3 source/acceptance;
never ship an advertised native jj retirement that always refuses disk freshness.
See [tasks](wi3-tasks.md), [contract](contracts/wi3.md). Test-first implementation in native/core/CLI
worktrees, independent source+sandbox review/full checks/race/both pinned lanes, then WI3 PR.
