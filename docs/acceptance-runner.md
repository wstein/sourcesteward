# Plan-only acceptance runner

The runner is an opt-in Go test (`internal/acceptance`, Spec 031 T040). It plans the
local branches of a repository with the in-process v3 planner (`workspace.PlanTidyV3`),
writes the review report to a directory you name, prints a compact summary and proves
that the target repository did not change. It needs no build or installation, and it
never applies a plan: the apply entry point is not referenced.

```sh
STW_ACCEPTANCE=1 \
STW_ACCEPT_REPO=/abs/path/to/repository \
STW_ACCEPT_BASE=refs/heads/main \
STW_ACCEPT_REPORT_DIR=/abs/path/to/new-or-empty-report-dir \
go test -count=1 -v -run TestPlanOnlyAcceptance ./internal/acceptance
```

Optional: `STW_ACCEPT_ONLINE=1` adds the forge-backed proof for the remote named by
`STW_ACCEPT_REMOTE` (default `origin`) and may contact GitHub. Without
`STW_ACCEPTANCE=1` the test skips, so `go test ./...` never runs it.

## Safety properties

- Refusal before any work (non-zero exit, message starting `acceptance runner refused`):
  relative paths, an unknown or non-`refs/heads/` base, a non-repository, a report
  directory that is, contains or lies inside the target working tree, its common Git
  directory or any linked worktree, or that is not empty.
- Temporary state: the plan state, a private `HOME` and a private `TMPDIR` are created
  under the test's temporary directory. The `merge-tree` proof objects go to the
  planner's private object directory (`GIT_OBJECT_DIRECTORY`, with the target objects as
  alternates), so they never land in the target.
- Constant Git environment: no global or system configuration, hooks path `/dev/null`,
  `GIT_OPTIONAL_LOCKS=0`, no prompts.
- Non-mutation: before and after the plan the runner records SHA-256 digests of
  `for-each-ref`, `worktree list --porcelain`, `config --local --list` and
  `status --porcelain` (plus its line count) and the number of files below the common
  object directory. Any difference fails the run and names the part.

## Output

The summary has the counts per decision class (`delete:<proof>`, `retain:<reason>`),
the plan's review summary per reason, the runtime and the planner's process and request
counts. The report directory receives `tidy-report.md`, `tidy-report.json`,
`result.json` (the full plan result) and `summary.txt`.

A separate section, "worktree facts (not part of the plan)", lists linked worktrees
with dirty, locked, detached, missing and prunable flags, read with
`git worktree list --porcelain` and `git status --porcelain`. The v3 plan itself only
reports `workspace-used`; these facts are context for a reviewer, not evidence.

The runner's tests (`go test ./internal/acceptance`) use a fixture repository with a
branch integrated by ancestry, one by patch-id, an unmerged one and a branch held by a
dirty linked worktree.
