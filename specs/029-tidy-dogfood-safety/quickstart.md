# Validation and eventual dogfood guide

Status: proposed commands for the future implemented v3 contract. They are not available in the current installed CLI. This guide authorizes no build, install or cleanup.

## Source and temporary acceptance

After plan/tasks/analysis gates and test-first implementation, run source tests with `umask 022`. Exercise [mandatory stream fixtures](contracts/streams.md#mandatory-acceptance), the provider failure matrix and native reference/retirement concurrency cases on disposable repositories. Validate real v3 JSON and all old schema fixtures. Run minimum/current Git and existing jj lanes; record skips/missing capabilities. Run docs/architecture/whitespace checks; a test invoking actual Hugo packaging needs separate build authorization or an explicitly recorded exclusion. Do not install missing tools or build a CLI just to complete a source check without its required authorization.

For each six-worktree fixture, record tracked file count315–358, maximum blob301,117 bytes, complete tracked/staged/untracked/ignored scopes, native proof sample counts3/8, raw-object process counts≤3/8, all read/channel totals and elapsed time. Exact size limits are accepted only when other guards/deadlines pass; limit/stale/actor failures retain and preserve source/siblings. After retirement, the original branch must still exist; a fresh local-ref plan then proves/deletes it independently with recovery.

## Fresh real-repository run after separate activation

The coordinator first obtains the exact candidate build/install authorization, verifies installed executable identity and uses the existing explicit authorization for the requested concrete cleanup effects. No owner merge/PR gate is substituted for local build authority. Recollect current reference/workspace/protection/base facts; original54/13 counts remain a historical comparison, not the new denominator. Protect the primary checkout, every active agent worktree and SheetMill's untracked plan document/ignored cache. Reconfirm completion of each selected legacy worktree; old clean snapshots are not fresh apply evidence.

Illustrative local-ref planning, using caller-supplied canonical paths and exact repository selection:

```sh
stw tidy plan "$repo_path" --contract 3 --base refs/remotes/origin/main --state-dir "$state_dir" --online --remote origin --repo OWNER/REPOSITORY --json
```

Review every retained/eligible target, complete status, network/local effects and plan identity. Apply that exact plan with `--contract 3 --plan PLAN_ID --state-dir "$state_dir" --authorize --online`. No generic Git cleanup fallback if it retains or fails.

A legacy retirement plan selects exactly one completed destination with `--completed-worktree "$completed_path" --owner LABEL --remove-registration --remove-files "$completed_path"` and repeatable `--protect-worktree "$active_path"`, plus the same explicit base/online evidence. Apply repeats completion path/owner/protection set and both apply acknowledgments. Native removal preserves the branch; generate a new local-ref plan afterward. A blocked dirty/ignored/locked target is retained, not detached or force-cleaned.

Tracking cleanup is another plan: `--contract 3 --base refs/remotes/origin/main --tracking-remote origin --online`, optional exact `--ref refs/remotes/origin/NAME`, explicit private state and repo identity. It lists only local tracking actions. It neither deletes server branches nor fetches/prunes. Apply its exact plan with local and online acknowledgments.

Report before/after tables separately for local branches, registered worktrees and local tracking refs; count completed/refused/retained/unknown actions, list exact blockers and recovery pointers, and verify protected files/workspaces unchanged. Missing provider evidence, budget exhaustion or any changed state requires a fresh bounded plan, never treating the earlier RC investigation as permission or proof.
