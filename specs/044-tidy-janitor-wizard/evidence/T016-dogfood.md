# T016: dogfood dry run of the janitor wizard on a copy of the live workspace

Spec 044 task T016. The wizard (`stw tidy`, P1 to P4b merged) ran in dry mode on a COPY of the live
workspace, offline, with no apply. Base: `origin/main` `d92e6415`. Date of the runs: 2026-10-28.
This file is evidence, not a fix: every finding is listed as an observation D1.. below and nothing
was changed in the product. Task and progress checkboxes are not touched.

Contents: [Verdict](#verdict), [Setup](#setup-the-copy-and-the-proof-that-the-live-workspace-is-unchanged),
[Commands and runs](#commands-and-runs), [Counts against the reference](#counts-per-level-against-the-reference),
[Screens](#real-screens), [What the wizard offered for the 17 groups](#what-the-wizard-offered-for-the-groups),
[Observations](#observations), [Release-blocking proposal](#release-blocking-proposal), [Limits](#limits-of-this-run).

## Verdict

The screens, the levels, the counts and the typed confirmation behave as specified, and nothing leaked
to the live workspace. But the wizard offers rows that it cannot plan, and on the live data that is almost
all of them: D1 and D2 together cover all 150 offered directories, D3 covers 40 of the 43 offered branches, so
at most 3 of the 193 offered rows can be planned (the 2 branches in the workspace repository, seen to pass,
and the 1 branch in workharbor, untried). It follows that `a` ("take all green") always fails: 4 of the 6
green rows are directories (3 under D1, 1 under D2). Of 17 groups only group 1 reached the typed prompt
(group 7 was not tried); every other group tried (2, 3, 9) ended the whole run with exit 1 and an error after the
person had walked all groups. D1 to D4 and the 11 branches of D17 are proposed as release-blocking (see the proposal below). The
other observations are usability findings.

## Setup: the copy and the proof that the live workspace is unchanged

How `stw tidy` finds its inputs (read from the code and `docs/tidy.md` before copying):

- the workspace: `--workspace`, else discovery from the working directory (Git only); members are the
  direct members plus `.`; here `/workspaces/Anchorage` with `bundledock`, `chartscribe`, `crewbook`,
  `sheetmill`, `sourcesteward`, `workharbor`;
- the creation records: `$STW_STATE_DIR` or `${XDG_STATE_HOME:-~/.local/state}/stw/workspace`, here
  `/home/vscode/.local/state/stw/workspace` (13 records on the day of the runs);
- the worktrees: whatever each repository registers (`git worktree list`, 796 registrations, 776 with an
  existing directory), at their absolute paths, most of them under `/tmp`.

Because the registrations carry absolute paths, a plain copy under another prefix would make the wizard
read the LIVE directories (and change the path-based owner and `/tmp` rules). So the copy is shadowed
over the original paths inside a private mount namespace:

1. `cp -a` of every root (the workspace `/workspaces/Anchorage` with all `.git/modules`, and each
   registered worktree directory that is not inside another root: 692 roots, 12 GB, 6 min 18 s; the copy stays at `/home/vscode/dogfood-t016-copy`, outside the scratchpad, and is not deleted yet) and of the
   state directory (176 KB) to `/home/vscode/dogfood-t016-copy` (`$S/dogfood-copy` is a symlink to it, so
   that the copy is not inside the scratchpad it copies; `$S` is the scratchpad of the session).
2. `unshare -Urm` (user and mount namespace, the mounts stay private to the namespace), then
   `mount --bind COPY ORIGINAL` for every root and for the state directory, then the agent markers
   (`CLAUDECODE`, `CLAUDE_CODE_*`) are removed from the environment. Inside the namespace the original
   paths show the copy, so a write can only reach the copy. The earlier runs used `STW_OWNER=vscode`
   and uid 0 mapped to the user; the later runs add a nested `unshare -U --map-user=1000` so that the
   process runs as uid 1000 like a person (the results do not differ; the scripts are `ns-run.sh` and
   `ns-run2.sh` in the session scratchpad, quoted below).
3. The wizard runs in a pseudo-terminal driven by a small Python script (`pty.fork`, scripted answers
   matched against the prompt text, a window of 120 by 50 columns).

Script of the namespace (the whole of it):

```sh
while IFS=$'\t' read -r id root; do mount --bind "$C/roots/$id" "$root"; done < $C/roots.map
mount --bind $C/state/stw /home/vscode/.local/state/stw
# unset CLAUDECODE, CLAUDE_CODE_*, CODEX_*, AGY_*, STW_AGENT; cd into a worktree of the workspace
exec unshare -U --map-user=1000 --map-group=1000 "$@"       # ns-run2.sh only
```

Proof that the live workspace is unchanged. A metadata manifest (path, type, size, mode, mtime, uid of
912,809 entries under the 692 roots) and the SHA-256 of every file of the state directory were taken
before the copy (08:16 UTC) and after the last run (09:37 UTC):

| Check | Before | After | Result |
| --- | --- | --- | --- |
| state directory, SHA-256 of 45 files | `95f98614b7b896bd...` | `95f98614b7b896bd...` | identical; the metadata of the 52 entries is identical too |
| roots, manifest of 912,809 entries | `77cba7e0406e4a23...` | `acccd07eec9745a0...` | 245 entries differ, see below |
| `/tmp/whr*`, `/tmp/workharbor*`, every other worktree directory except one | n/a | n/a | no difference |

The roots manifest differs only where other agents were working concurrently in the live workspace
while the runs went on: 170 entries below `/workspaces/Anchorage/.git/modules` (the new worktree
`wt-rev330`, refs, logs and objects of sourcesteward), 73 entries in the worktree `wt-tidy-apply-eof`
(another agent's active branch), and 2 in the workspace directory itself. None is a path of this task.
(Only this task's own `git worktree add` for the branch `docs/tidy-dogfood-t016` preceded the first
manifest and is therefore part of both.) The wizard wrote only into the copy: its state directory gained
four plan files (three after the failed builds RD, RE and RE2, one after the refused confirmation of RG, see D4); the live state directory did not.

## Commands and runs

Every run is `go run ./cmd/stw tidy --offline --workspace /workspaces/Anchorage` from the worktree of this
branch (no `go build`, no `go install`), inside the namespace, in the pseudo-terminal. `--online` was not
run (see Limits): no GitHub request is expected from any run (`--offline`; not measured, the namespaces had network access). Exit codes are those of `go run` (the
wizard's exit code; the line `exit status 1` is `go run`'s).

| Run | Namespace | Scripted answers | Exit | Wall time |
| --- | --- | --- | --- | --- |
| R0 | ns-run | a `q` typed during the analysis (driver artifact, see D14) | 0 | 164 s |
| R1 | ns-run | Enter; `l`; `q` at the first step | 0 | 176 s |
| RA | ns-run | Enter; group 1 `l`, `d 1`, `y`; group 2 `l`, Enter (next page), `s`; group 3 `r` (`n` for the first row, `y` for the second, `n` for the rest); group 6 `d 1`; every other group `s`; `q` at "Next" | 0 | 198 s |
| RB | ns-run | as RA but `q` at the third row of the review | 0 | 175 s |
| RC | ns-run | groups 1 and 2 `y`, group 3 `r` (`n`, `y`, rest `n`), group 9 `d 84`, `l`, `y`, others `s`; Enter at the summary | 1 | 177 s |
| RD | ns-run | groups 1, 2, 3 `y`; others `s`; Enter | 1 | 201 s |
| RE, RE2 | ns-run, ns-run2 | groups 1 and 2 `y`; others `s`; Enter | 1, 1 | 159 s, 157 s |
| RF | ns-run2 | group 2 `r`: `y` for row 3, `n` for the other 39; others `s`; Enter | 1 | 181 s |
| RG | ns-run2 | group 1 `y`; others `s`; Enter; a wrong word at the typed prompt | 1 | 159 s |
| RH | ns-run2 | `a` (all green) at the first question; at the summary `e`, then `q` three times | 1 | 151 s |
| RI | ns-run2 | `zzz`, `d 99999`, `s` at group 1; group 2 `l`; `q` at the pager | 0 | 175 s |
| PLAN | ns-run2 | `stw tidy --offline --json` (agent mode, no prompts) | 0 | 167 s (user 4 m 7 s, sys 7 m 19 s) |
| FIX | none and a user namespace only | a throwaway repository under `/tmp/whr-t016fix` (not the live workspace), used to tell a defect of the product from an artifact of the copy (D1) | 1 | 2 s |

Analysis time of the 7 repositories (the progress block, runs RA to RI): 2 min 25 s to 2 min 48 s; the
slowest are `sourcesteward` (2 min 6 s to 2 min 24 s) and `workharbor` (2 min 17 s to 2 min 39 s); the
others take 3 to 18 s. Sample of the last frames of the progress block (stderr, redrawn in place on a
terminal; shown here after removing the escape sequences):

```text
stw tidy: 7/7 repos, 2m48s, slowest workharbor 2m39s
✓ .             analysed            9.6s
✓ bundledock    analysed            18s
✓ chartscribe   analysed            3.4s
✓ crewbook      analysed            8.8s
✓ sheetmill     analysed            5.5s
✓ sourcesteward analysed            2m24s
✓ workharbor    analysed            2m39s
```

## Counts per level against the reference

The reference is `overview.json` of 2026-10-26 (7 repositories, 1,129 candidates, 165 choosable, 964 retained; the per-level counts
6 / 93 / 66 / 394 / 570 are those of `spec.md` "Measured data", the file itself has no risk field). The copy was taken two days
later; `stw tidy --offline --json` on the copy and the wizard agree with each other (193 choosable, 1,029 retained).

| Level | Reference 2026-10-26 | Copy 2026-10-28 | Difference |
| --- | --- | --- | --- |
| choosable (`safe`) | 165 | 193 | +28 directories; the 43 branches are the same |
| green | 6 | 6 | none |
| yellow | 93 | 94 | +1 |
| orange | 66 | 93 | +27 |
| red | 394 | 517 | +123 |
| kept | 570 | 512 | -58 |
| retained in all | 964 | 1,029 | +65 |
| candidates | 1,129 | 1,222 | +93 |
| groups | 15 | 17 | +2 |

Explanation of the differences, from the `--json` documents of both days:

- choosable +28 directories (122 to 150): new worktrees since 10-26, 27 of them orange and 1 yellow.
- orange 66 to 93: the rows with R2 (nobody vouches) are 86 now (65 only R2, 21 with R1 too), so +20 new
  worktrees; R1 (recent activity, 24 to 72 hours old) applies to 28 rows, of which 7 are orange only because of
  it (+7). The reference file had no activity times, so R1 could not be evaluated there. The two extra groups
  come from the 28 new directories, split by owner, effect and place (D9).
- red +123: unmerged branches 135 to 159 and unmerged worktrees 166 to 171 (+29, new work); record-missing
  worktrees with proof `none` 47 to 83 (+36); 53 record-missing worktrees that are integrated but hold
  untracked, ignored or uncommitted files (48 `extra-files`, 5 `uncommitted-files`); ignored-extras 23 to 26 (+3),
  not judged 17 to 18 (+1), the current worktree (+1), dirty 6 (0). The reference file carried no dirt for integrated
  record-missing rows (Spec 044, "Measured data"), so those 53 were kept there and are red now: the fact is observed
  now, the ladder is unchanged.
- kept 570 to 512: the 27 `recently-active` rows aged past 24 hours (0 now); the integrated record-missing rows
  299 to 261 (53 moved to red, 15 new); branches attached elsewhere 217 to 224 (+7); 17 + 3 stale rows and 7
  primary checkouts are unchanged.
- the safe rows 193 = 43 branches + 150 directories; levels 6 + 94 + 93 = 193; 517 + 512 = 1,029.

## Real screens

All screens are the output of the wizard on the copy (escape sequences removed; the answers are echoed
by the pseudo-terminal, which a person's terminal also does). The session directory in the scratchpad
paths is replaced by `<session>`. Two of the lines are very long and are shown as they are (D12).

### Group overview and retain table (offline)

```text
Workspace /workspaces/Anchorage   7 repositories analyzed
Derived:   workspace /workspaces/Anchorage (--workspace), remote:. origin (origin), base:. refs/heads/main (refs/remotes/origin/HEAD), remote:bundledock origin (origin), base:bundledock refs/heads/main (refs/remotes/origin/HEAD), remote:chartscribe origin (origin), base:chartscribe refs/heads/main (refs/remotes/origin/HEAD), remote:crewbook origin (origin), base:crewbook refs/heads/main (refs/remotes/origin/HEAD), remote:sheetmill origin (origin), base:sheetmill refs/heads/main (refs/remotes/origin/HEAD), remote:sourcesteward origin (origin), base:sourcesteward refs/heads/main (refs/remotes/origin/HEAD), remote:workharbor origin (origin), base:workharbor refs/heads/main (refs/remotes/origin/HEAD), state-dirs /home/vscode/.local/state/stw/workspace (13 records) (default), online off (--offline)
Mode:      human (stdin tty, stdout tty, no agent marker)

Can be cleaned (193 of 1,222 candidates in 17 groups)                                             risk
   1  2 branches      in .                reversible, hint Werner Stein                           green
   2  40 directories  /tmp/whr*           files deleted, codex                                    yellow  (2 green, 38 yellow)
   3  24 branches     in sourcesteward    reversible, 3 owners                                    yellow
   4  9 branches      in bundledock       reversible, hint Werner Stein and claude                yellow
   5  8 branches      + 4 smaller groups  reversible, hint Werner Stein                           yellow
   6  72 directories  /tmp/workharbor*    files deleted, hint Werner Stein                        orange  (1 green, 1 yellow, 70 orange)
   7  20 directories  /tmp/whr*           files deleted, branch too, codex and Codex              orange  (1 green, 14 yellow, 5 orange)
   8  7 directories   agent scratchpads   files deleted, hint Werner Stein                        orange
   9  6 directories   + 3 smaller groups  files deleted, branch too, hint Werner Stein and Codex  orange
  10  5 directories   + 3 smaller groups  files deleted, hint Werner Stein                        orange
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored

Kept (1,029): nothing here is offered for deletion
  517 may hold unique work (red): not merged, uncommitted or untracked files, not judged
  512 kept for another reason (record missing, branch in use, recently active, ...)
notice: remote-tracking references are not read without --online (7 repositories)
notice: Evidence is sampled and nontransactional; nothing was changed and nothing was fetched. (7 repositories)
The rows with their codes: stw tidy --rows (10 per repository and owner)   Every row: stw tidy --json

Kept: 1,029 candidates. Nothing here is offered for deletion.
  count  level  why                                                                    what to do
    330  RED    not merged: it has commits the base lacks                              nothing is deleted; review: stw tidy plan REPO --contract 3 --base BASE --report-dir DIR
    278  kept   created by stw, record not found                                       re-run with the --state-dir that created it
    224  kept   branch is checked out in a kept worktree                               decide on that worktree first, then run again
    136  RED    created by stw, record not found; it holds untracked or ignored files  do not delete; find the right --state-dir and judge it
     26  RED    untracked or ignored files                                             look inside; remove them yourself if junk
     18  RED    not judged (for example a deadline or Git refused)                     assume unique work; after a deadline run again with --timeout 15m
      7  kept   main checkout of a repository                                          never offered
      6  RED    uncommitted changes                                                    commit or discard them yourself
      3  kept   worktree entry whose directory is gone                                 no stw action yet
      1  RED    the worktree you are in; its commits are not on the base branch        nothing is deleted; unique work may exist
Rows with their codes: stw tidy --rows (10 per repository and owner); every row: stw tidy --json
Continue? [Enter] step through the groups  [a] take all green  [q] quit:
```

### Group steps: green (group 1), with `l`, `d 1`, `y`

```text
Step 1 of 17   2 branches in .   risk green
  Owner     hint Werner Stein (2)
  Evidence  all of its work is already on the base branch (2)
  Effect    2 branches are deleted in 1 repository (restorable)
  Recover   history stays under refs/stw/recovery/v3/...; undo with stw tidy restore (see stw tidy history)
  Why green  provably on the base branch
      1  green   feat/devcontainer-volume-and-history  .
      2  green   tmp/ci-wait  .

Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = take): l
      1  green   feat/devcontainer-volume-and-history  .
      2  green   tmp/ci-wait  .
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = take): d 1
[1] feat/devcontainer-volume-and-history: reason ; proof ancestor; owner hint Werner Stein (commit-author, scope unknown); risk green (proof-ancestor)
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = take): y
```

### Group step: yellow (group 2), with `l` and the pager

```text
Step 2 of 17   40 directories /tmp/whr*   risk yellow (2 green, 38 yellow)
  Owner     codex (40)
  Evidence  the work is on the base as equal patches (a rebase merge), not as the same commits (38)
            it has no commit of its own (2)
  Effect    40 directories are deleted for good in 1 repository (their branches stay)
  Recover   committed history stays pinned under refs/stw/recovery/v3/...; deleted files are not restored
  Why yellow  on the base as equal patches, not as the same commits
      3  yellow  /tmp/whr-009-all-defaults-review-91d8585
      4  yellow  /tmp/whr-009-defaults-restack-review-de19272
      5  yellow  /tmp/whr-h33-restack-review-3e272d0d
      ... 37 more (l = list)

Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = skip): l
      3  yellow  /tmp/whr-009-all-defaults-review-91d8585
      4  yellow  /tmp/whr-009-defaults-restack-review-de19272
      5  yellow  /tmp/whr-h33-restack-review-3e272d0d
      6  yellow  /tmp/whr-h33-restack-review-415091f9
      7  yellow  /tmp/whr-h33-review-019bc93f
      8  yellow  /tmp/whr-h33-test-quality-019bc93f
      9  green   /tmp/whr-lanea-options-opus-review-20261020
     10  yellow  /tmp/whr-pr165-independent-review-0b78cbb
     11  yellow  /tmp/whr-pr165-review-20261020
     12  yellow  /tmp/whr-pr173-review-786045d4
     13  yellow  /tmp/whr-qa-t025-e742
     14  yellow  /tmp/whr-review-feature007-ce3cfc8
     15  yellow  /tmp/whr-t025-opus-recovery-20261020
     16  yellow  /tmp/whr-t025-restack-sol-source-20261020
     17  yellow  /tmp/whr-t025-restack-sol-source-f5d08dea-20261020
     18  yellow  /tmp/whr-t028-opus-review-20261020
     19  yellow  /tmp/whr-t030-apply-sol-binding-55f0c274
     20  yellow  /tmp/whr-t030-apply-testqa-55f0c274
     21  yellow  /tmp/whr-t030-chain-sol-binding-2dded35d
[... 19 lines omitted from this excerpt ...]
     40  yellow  /tmp/whr010-plan-independent-review
     41  yellow  /tmp/whr010-plan-independent-review-r2
     42  yellow  /tmp/whr190-ci-review-3044021
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = skip): s
```

### Review of a group with `r` (group 3, yellow, `n` for the first row, `y` for the second)

```text
Step 3 of 17   24 branches in sourcesteward   risk yellow
  Owner     hint Werner Stein (19), claude (3), codex (2)
  Evidence  the work is on the base as equal patches (a rebase merge), not as the same commits (24)
            the upstream branch is gone (2)
  Effect    24 branches are deleted in 1 repository (restorable)
  Recover   history stays under refs/stw/recovery/v3/...; undo with stw tidy restore (see stw tidy history)
  Why yellow  on the base as equal patches, not as the same commits
     43  yellow  claude/spec041-wp4a-plan-set  sourcesteward
     44  yellow  claude/spec041-wp4b-planner  sourcesteward
     45  yellow  claude/spec041-wp4d-branch-leg  sourcesteward
      ... 21 more (l = list)

Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = skip): r
     43  yellow  claude/spec041-wp4a-plan-set  sourcesteward
Take it? [y/n/q] (default n): n
     44  yellow  claude/spec041-wp4b-planner  sourcesteward
Take it? [y/n/q] (default n): y
     45  yellow  claude/spec041-wp4d-branch-leg  sourcesteward
Take it? [y/n/q] (default n): n
     46  yellow  codex/011-prune-v4-history-author-20261026  sourcesteward
Take it? [y/n/q] (default n): n
     47  yellow  codex/011-prune-v4-history-fixes-20261026  sourcesteward
Take it? [y/n/q] (default n): n
     48  yellow  docs/041-acceptance-evidence  sourcesteward
Take it? [y/n/q] (default n): n
     49  yellow  feat/032-sync-restore  sourcesteward
Take it? [y/n/q] (default n): n
     50  yellow  fix/217-ref-transaction-descendants  sourcesteward
Take it? [y/n/q] (default n): n
     51  yellow  fix/release-ci-guard-retry  sourcesteward
[... 28 lines omitted from this excerpt ...]
Take it? [y/n/q] (default n): n
     66  yellow  r9-spec031  sourcesteward
Take it? [y/n/q] (default n): n
```

### Group step: orange (group 9), with `d 84`, `l`

```text
Step 9 of 17   72 directories /tmp/workharbor*   risk orange (1 green, 1 yellow, 70 orange)
  Owner     hint Werner Stein (72)
  Evidence  the work is on the base as equal patches (a rebase merge), not as the same commits (70)
            no record says whose it is (70)
            it changed in the last 3 days or its last change is unknown (22)
            and 1 more fact
  Effect    72 directories are deleted for good in 1 repository (their branches stay)
  Recover   committed history stays pinned under refs/stw/recovery/v3/...; deleted files are not restored
  Why orange  on the base as equal patches and one more fact raises the risk; no record says whose it is; it changed in the last 3 days or its last change is unknown
     84  orange  /tmp/workharbor-baseline-0jbykfta/checkout
     85  orange  /tmp/workharbor-baseline-18gue17r/checkout
     86  orange  /tmp/workharbor-baseline-1rbjh7mc/checkout
      ... 69 more (l = list)

Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = skip): d 84
[84] /tmp/workharbor-baseline-0jbykfta/checkout: reason unregistered; proof patch-id; owner hint Werner Stein (commit-author, scope unknown); risk orange (proof-equivalent, nobody-vouches)
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = skip): l
     84  orange  /tmp/workharbor-baseline-0jbykfta/checkout
     85  orange  /tmp/workharbor-baseline-18gue17r/checkout
     86  orange  /tmp/workharbor-baseline-1rbjh7mc/checkout
     87  orange  /tmp/workharbor-baseline-1rko7xas/checkout
     88  orange  /tmp/workharbor-baseline-1tbbh4dx/checkout
     89  orange  /tmp/workharbor-baseline-2_np6jpu/checkout
     90  orange  /tmp/workharbor-baseline-2yhtosek/checkout
     91  orange  /tmp/workharbor-baseline-3gwtx1i0/checkout
     92  orange  /tmp/workharbor-baseline-3jjza9i7/checkout
[... 66 lines omitted from this excerpt ...]
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = skip): y
```

### Summary after a walk (RA: group 1 `y`, one branch of group 3 taken, `q` at "Next")

```text
Summary: nothing has been changed yet
  group                      items  risk    effect
  2 branches in .                2  green   delete 2 branches
  1 branch in sourcesteward      1  yellow  delete 1 branch
  skipped: 15 groups (150 directories, 43 branches)
  total: 3 items in 2 repositories; 1029 kept rows untouched
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored
Typed words that will be asked (on /dev/tty): claude, unknown
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit: q

Quit; nothing was stored.
```

### `q` while reviewing, `q` at the pager, an invalid answer, `d 99999`

```text
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = skip): r
     43  yellow  claude/spec041-wp4a-plan-set  sourcesteward
Take it? [y/n/q] (default n): n
     44  yellow  claude/spec041-wp4b-planner  sourcesteward
Take it? [y/n/q] (default n): y
     45  yellow  claude/spec041-wp4d-branch-leg  sourcesteward
Take it? [y/n/q] (default n): q

Quit; nothing was stored.
```

```text
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = take): zzz
not understood: a word that is not a term. Answer y, s, l, r, q, d N, or numbers like 1,2 or 3-5 or a selector (a b w t o:LABEL m:REPO, except).
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = take): d 99999
That number is not offered.
Take this group?  [y] take  [s] skip  [l] list  [r] review each  [q] quit  (Enter = take): s
...
-- 20 more (Enter = next 20, q = stop): q

Quit; nothing was stored.
```

### The typed prompt (RG: group 1 only, a wrong word)

```text
Summary: nothing has been changed yet
  group            items  risk    effect
  2 branches in .      2  green   delete 2 branches
  skipped: 16 groups (150 directories, 67 branches)
  total: 2 items in 1 repository; 1029 kept rows untouched
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored
Typed words that will be asked (on /dev/tty): unknown
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit:
This deletes 0 directories for good and 2 branches (restorable).
Foreign items (unknown). Type "unknown 0e9951" to confirm: wrong words
Error: confirmation refused
exit status 1
```

`a` ("all green") and `e` (edit the selection), RH:

```text
Summary: nothing has been changed yet
  group                         items  risk    effect
  2 branches in .                   2  green   delete 2 branches
  2 directories /tmp/whr*           2  green   delete 2 directories
  1 directory /tmp/workharbor*      1  green   delete 1 directory
  1 directory /tmp/whr*             1  green   delete 1 directory and 1 branch
  skipped: 13 groups (18 directories, 47 branches)
  total: 6 items in 2 repositories; 1029 kept rows untouched
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored
Typed words that will be asked (on /dev/tty): codex, unknown
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit: e
Terms (numbers, ranges, a b w t o:LABEL m:REPO, except; Enter keeps the selection): q
not understood: a word that is not a term.
Terms (numbers, ranges, a b w t o:LABEL m:REPO, except; Enter keeps the selection): q
not understood: a word that is not a term.
Terms (numbers, ranges, a b w t o:LABEL m:REPO, except; Enter keeps the selection): q
Error: too many invalid answers; nothing was chosen
exit status 1
```

### Failures at the end of a walk (exit 1)

RC: groups 1, 2 and 72 directories of group 9 and one branch chosen:

```text
Summary: nothing has been changed yet
  group                            items  risk    effect
  2 branches in .                      2  green   delete 2 branches
  40 directories /tmp/whr*            40  yellow  delete 40 directories
  1 branch in sourcesteward            1  yellow  delete 1 branch
  72 directories /tmp/workharbor*     72  orange  delete 72 directories
  skipped: 13 groups (38 directories, 43 branches)
  total: 115 items in 3 repositories; 1029 kept rows untouched
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored
Typed words that will be asked (on /dev/tty): claude, codex, unknown
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit:
This deletes 112 directories for good and 3 branches (restorable).
Error: item cannot be planned: legacy-owner-unknown: worktree "/tmp/workharbor-baseline-0jbykfta/checkout" has no owner label that can be declared
exit status 1
```

RD: groups 1, 2, 3 chosen:

```text
Summary: nothing has been changed yet
  group                         items  risk    effect
  2 branches in .                   2  green   delete 2 branches
  40 directories /tmp/whr*         40  yellow  delete 40 directories
  24 branches in sourcesteward     24  yellow  delete 24 branches
  skipped: 14 groups (110 directories, 43 branches)
  total: 66 items in 3 repositories; 1029 kept rows untouched
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored
Typed words that will be asked (on /dev/tty): claude, codex, unknown
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit:
This deletes 40 directories for good and 26 branches (restorable).
Error: planning branches sub-plan of sourcesteward: planning branches sub-plan of sourcesteward: unknown-workspace-path: A registered workspace root is missing, replaced or not a canonical real directory; state-store disjointness is unproven.
exit status 1
```

RE2 (faithful uid): groups 1 and 2 chosen:

```text
Summary: nothing has been changed yet
  group                     items  risk    effect
  2 branches in .               2  green   delete 2 branches
  40 directories /tmp/whr*     40  yellow  delete 40 directories
  skipped: 15 groups (110 directories, 67 branches)
  total: 42 items in 2 repositories; 1029 kept rows untouched
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored
Typed words that will be asked (on /dev/tty): codex, unknown
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit:
This deletes 40 directories for good and 2 branches (restorable).
Error: planning retire sub-plan of workharbor: planning retire sub-plan of workharbor: workspace-failed: workspace operation could not complete: v3 physical owner capability unavailable or foreign
exit status 1
```

RF: one directory of group 2 chosen:

```text
Summary: nothing has been changed yet
  group                  items  risk    effect
  1 directory /tmp/whr*      1  yellow  delete 1 directory
  skipped: 16 groups (110 directories, 69 branches)
  total: 1 item in 1 repository; 1029 kept rows untouched
Typed words that will be asked (on /dev/tty): codex
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit:
This deletes 1 directory for good and 0 branches (restorable).
Error: planning retire sub-plan of workharbor: planning retire sub-plan of workharbor: workspace-failed: workspace operation could not complete: v3 physical owner capability unavailable or foreign
exit status 1
```

FIX, a throwaway repository, one worktree directly under `/tmp` (`/tmp/whr-t016fix-wt`, the branch
`codex/x` on the base), no namespace and no copy:

```text
Summary: nothing has been changed yet
  group                      items  risk    effect
  1 directory other in /tmp      1  green   delete 1 directory and 1 branch
  total: 1 item in 1 repository; 1 kept row untouched
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored
Typed words that will be asked (on /dev/tty): codex
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit:
This deletes 1 directory for good and 1 branch (restorable).
Error: planning retire sub-plan of .: planning retire sub-plan of .: workspace-failed: workspace operation could not complete: v3 physical owner capability unavailable or foreign
exit status 1
```

FIX again, the worktree below a directory the user owns (`/tmp/whr-t016fix/owned/wt2`): the run reaches the
typed prompt and refuses the wrong word:

```text
Summary: nothing has been changed yet
  group                      items  risk    effect
  1 directory other in /tmp      1  green   delete 1 directory and 1 branch
  total: 1 item in 1 repository; 1 kept row untouched
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored
Typed words that will be asked (on /dev/tty): codex
Next: [Enter] continue  [e] edit the selection  [a] add all green  [q] quit:
This deletes 1 directory for good and 1 branch (restorable).
Foreign items (codex). Type "codex 0a74ee" to confirm: wrong
Error: confirmation refused
exit status 1
```

## What the wizard offered for the groups

The first line of each step of RA (group, level, counts):

```text
Step 1 of 17   2 branches in .   risk green
Step 2 of 17   40 directories /tmp/whr*   risk yellow (2 green, 38 yellow)
Step 3 of 17   24 branches in sourcesteward   risk yellow
Step 4 of 17   9 branches in bundledock   risk yellow
Step 5 of 17   5 branches in crewbook   risk yellow
Step 6 of 17   1 branch in chartscribe   risk yellow
Step 7 of 17   1 branch in workharbor   risk yellow
Step 8 of 17   1 branch in sheetmill   risk yellow
Step 9 of 17   72 directories /tmp/workharbor*   risk orange (1 green, 1 yellow, 70 orange)
Step 10 of 17   20 directories /tmp/whr*   risk orange (1 green, 14 yellow, 5 orange)
Step 11 of 17   7 directories agent scratchpads   risk orange
Step 12 of 17   3 directories /tmp/crewbook*   risk orange
Step 13 of 17   2 directories /tmp/stw*   risk orange
Step 14 of 17   2 directories /tmp/stw*   risk orange
Step 15 of 17   2 directories other in /tmp   risk orange
Step 16 of 17   1 directory /tmp/crewbook*   risk orange
Step 17 of 17   1 directory in /workspaces/Anchorage/scratch   risk orange
```

| Groups | Rows | Level | Reached the typed prompt? |
| --- | --- | --- | --- |
| 1 | 2 branches in `.` | green | yes (RG, `unknown`) |
| 2 | 40 directories `/tmp/whr*` (owner codex by path) | yellow (2 green, 38 yellow) | no, D1 (RE, RE2, RF) |
| 3 | 24 branches in sourcesteward | yellow | no, D3 (RD) |
| 4, 5, 6, 8 | 9 + 5 + 1 + 1 branches in bundledock, crewbook, chartscribe, sheetmill | yellow | not tried; D3 applies (each repository has a locked registration with a missing directory) |
| 7 | 1 branch in workharbor | yellow | not tried (workharbor has no such registration) |
| 9 | 72 directories `/tmp/workharbor*` | orange (1 green, 1 yellow, 70 orange) | no, D2 (RC) |
| 10 | 20 directories `/tmp/whr*` with branch (owner codex, Codex) | orange | not tried; D1 applies to the rows directly under `/tmp` |
| 11 to 17 | 7 + 3 + 2 + 2 + 2 + 1 + 1 directories | orange | not tried; D2 applies to the rows with a commit-author owner |

## Observations

Severity: High (the wizard cannot do what it offers, or a wrong promise), Medium (misleading or
inconsistent with the specification), Low (wording, layout, polish). "Blocking" is a proposal for v0.2.0.

**D1 (High, proposed blocking). Directories directly under `/tmp` are offered, even as green, but cannot be planned.**
Choosing any such row ends the run at the end of the walk with
`Error: planning retire sub-plan of workharbor: planning retire sub-plan of workharbor: workspace-failed: workspace operation could not complete: v3 physical owner capability unavailable or foreign`
(exit 1). Seen with all 40 rows of group 2 (RE, RE2) and with a single row (RF, `/tmp/whr-009-all-defaults-review-91d8585`). It is not an artifact
of the copy or the namespace: a throwaway repository with one worktree `/tmp/whr-t016fix-wt` fails the same way with no
namespace at all (FIX), and the same run with the worktree below a directory the user owns reaches the typed prompt.
Cause (from the independent review of this document): the classifier and the ladder offer an unregistered worktree directly under `/tmp` as `safe` and
check nothing about its physical parent; the retirement admission then calls `retirementV3Directory(filepath.Dir(dest))`
(`internal/workspace/tidy_v3_legacy.go:272`), which refuses because `/tmp` is owned by root and not by the effective user
(`internal/workspace/retirement_manifest_unix.go:22`); a second barrier is the sibling scan of the same parent (`tidy_v3_legacy.go:316`:
every entry of `/tmp` must be a plain file or directory owned by the user). `RegistrationParent` and `RegistrationSiblings` are the
parent and siblings of the Git registration directory and are not the cause. It affects the 70 of the 150 offered
directories that sit directly in `/tmp` (the rule `/tmp/whr*`, `/tmp/stw*`, `/tmp/crewbook*`, `/tmp/review-*` and the like).

**D2 (High, proposed blocking; needs an owner decision). Directories whose owner is only a commit-author hint are offered but cannot be planned.**
The planner refuses a legacy worktree whose owner is `unknown` (`legacy-owner-unknown`, `internal/tidywizard/planset/v3planner.go:181-182`):
`Error: item cannot be planned: legacy-owner-unknown: worktree "/tmp/workharbor-baseline-0jbykfta/checkout" has no owner label that can be declared`
(RC, exit 1; this error comes from the mapping preflight, which stores nothing). 88 of the 150 offered directories have `scope unknown` and the owner source `commit-author`
(all of groups 9, 11, 12, 14, 15, 16 and 17). This is a contradiction between the specification and the planner, not only a bug:
`spec.md:202` makes these rows choosable in human mode, while the planner (the Spec 029 legacy completion) refuses them; the comment at
`v3planner.go:17` says the wizard retains them, but no caller does. The owner decided on 2026-10-28 to retain such rows (code `legacy-owner-unknown`)
and to change the specification accordingly; this document records that decision and the fix is not part of this pull request.

**D3 (High, proposed blocking). Branches of a repository with an stw-locked registration whose directory is gone cannot be planned.**
`Error: planning branches sub-plan of sourcesteward: ... unknown-workspace-path: A registered workspace root is missing, replaced or not a canonical real directory; state-store disjointness is unproven.` (RD, exit 1).
The live workspace has 17 such registrations (locked by stw, directory missing: 2 in sourcesteward, 5 in bundledock, 8 in crewbook, 1 each in
chartscribe and sheetmill; found by `git worktree list --porcelain` on the copy; 3 more are `prunable` and are accepted by the rule of
`qualifyTidyV3Registrations`), so 40 of the 43 offered branches are in repositories where the plan build is refused
(`internal/workspace/tidy.go:385`). The weaker rule at `tidy.go:405` admits only unlocked prunable registrations, so a locked one has no exemption and one stale
locked worktree blocks the whole repository, even for an unrelated green branch (reproduced on a fixture in the review of this document).
Only the repository `.` (2 branches) was observed to pass (RG); workharbor has no such registration and was not tried.
These locks are written by stw itself (`stw:<task>`), and stw has no plan kind that removes such a registration (`stale-registration-no-plan-kind`), so the person cannot
clear D3 through stw; only `git worktree unlock PATH` followed by `git worktree prune` does.

**D4 (Medium, part of the blocking class). The failure comes at the end, as a raw error, and can leave plans behind.**
Two kinds of failure, from two sets of runs. RC (the mapping preflight, `legacy-owner-unknown`) prints a single error and stores nothing. The planner errors (RD, RE, RE2, RF)
come after the summary (RE: `This deletes 40 directories for good and 2 branches`), carry the prefix twice
(`planning retire sub-plan of workharbor: planning retire sub-plan of workharbor: ...`: it is added at `internal/tidywizard/planset/planset.go:444` and already present from
`v3planner.go:211`), name no group or row, offer no way to deselect, and the CLI ends the run with exit 1 (`internal/cli/tidy_wizard_flow.go:233-239`), after
about 3 minutes of analysis and a walk of 17 groups. `assemble` runs only the mapping preflight, then plans the sub-plans in id order (branches before worktrees), so earlier
sub-plans are stored before a later one fails. The state directory of the copy gained four plan files: three after the failed builds RD, RE and RE2, and one after the refused
typed confirmation of RG, which is by design (`planset.go:486-487`).

**D5 (Low, wording). `q = stop` at the pager quits the whole wizard.** The pager prompt reads `-- 20 more (Enter = next 20, q = stop):`, but `q` returns
`ErrQuit` (`internal/tidywizard/prompt/steps.go:367-373`): the run prints `Quit; nothing was stored.` and the selection made so far is lost
(RI, and an earlier run). The word "stop" promises that only the list stops.
The behavior matches the key table of `spec.md:445` (`q` quits at any question and stores nothing); only the label is wrong.

**D6 (Medium). `q` is not accepted at the edit prompt.** The specification table says `q` quits at any question. At
`Terms (numbers, ranges, a b w t o:LABEL m:REPO, except; Enter keeps the selection):` the answer `q` is `not understood: a word that is not a term.`, and the third
invalid answer ends the run with `Error: too many invalid answers; nothing was chosen` (exit 1) (RH).

**D7 (Medium). The `skipped:` line of the summary does not add up.** It counts only groups with no selection, counts the branch that rides along
with a directory row as a branch, and leaves out the unselected rows of a partly taken group. RA: 3 rows chosen, line says
`skipped: 15 groups (150 directories, 43 branches)`, which are 193 items although 190 rows were not chosen and 23 of them (the declined rows of group 3)
are not in a skipped group. RH: 6 chosen, `skipped: 13 groups (18 directories, 47 branches)` (the unselected 38, 71 and 19 directories of groups 2, 9, 10 are in neither).

**D8 (Medium). Owner labels differ by letter case.** Task records name the owner `codex` (path convention) and `Codex`
(record of 5 rows); the steps show `Owner codex (18), Codex (2)` and `Owner Codex (2)`, and the selector `o:LABEL` and the typed word are exact, so one agent needs two words.

**D9 (Low). Groups with the same name.** Two groups are named `/tmp/whr*` (2 and 10), two `/tmp/stw*` (13 and 14) and two `/tmp/crewbook*` (12 and 16); they differ
only by effect or owner. The summary then shows `2 directories /tmp/whr*` and `1 directory /tmp/whr*` (RH), which cannot be told apart.

**D10 (Low). Plurals.** `1 directory is deleted for good in 1 repository (their branches stay)` and `..., and their branches too (restorable)` for one directory
(steps 16 and 17); `Kept: 1 candidates.` in the retain table of the throwaway repository (FIX). The singular forms elsewhere are right (`1 branch is deleted`).

**D11 (Low). `d N` and `l` details.** `d 1` for a safe row prints `reason ; proof ancestor; ...` with an empty reason; `d N` answers for a
number of another group (RA, `d 1` at group 6); `l` on a group that fits on its screen (group 1) prints the same two rows again.

**D12 (Low). Long lines and vague evidence.** The `Derived:` line has 804 characters on one line (it wraps in 120 columns); the line
`Why orange  on the base as equal patches and one more fact raises the risk; no record says whose it is; it changed in the last 3 days or its last change is unknown` has 163 (without its indent);
step 9 lists `and 1 more fact` without naming it.

**D13 (Low). The current worktree is red with `not-integrated` although its commit is on the base.** The worktree of this branch (`wt-t016`, HEAD
`d92e6415` equal to `origin/main`, no commit of its own) is the row `current-worktree`, proof `none`, level red, reason `not-integrated`; the retain table says
`the worktree you are in; its commits are not on the base branch`. The row is rightly never offered (kept for being the current worktree), but the red level and the sentence say something
that is wrong for it. (Wrong level argued: row `wt-t016`, shown level red, evidence `git rev-parse HEAD origin/main` returns one commit; it should be kept.)

**D14 (Low). Keys typed during the analysis answer the first question.** The analysis takes about 2 min 40 s and prints no prompt; a `q` typed at the start (R0, sent by the driver at 7.8 s, log `explore1`)
was held in the terminal and answered "Continue?" with `Quit; nothing was stored.`; an impatient Enter would equally start the walk.

**D15 (Info). Time.** 2 min 25 s to 2 min 48 s per start, every start, because the wizard re-analyzes; the person who quits at a prompt pays the analysis again. The time is
dominated by two repositories and the high `sys` time (7 min 19 s) is partly this environment (bind mounts on an overlay file system); it was not measured outside it.

**D16 (Info). Hostile and odd names.** None of the 1,222 candidates has a control character, ESC, bidi or non-ASCII character in its path, branch or owner label.
Odd but harmless: the owner label `author:Werner Stein` (591 rows, with a space; shown as `hint Werner Stein`), `author:probe` (1), an empty owner (45),
`Codex` (5), directories whose names start with an underscore (`/tmp/workharbor-baseline-_5d42_a1`), paths up to 109 characters and branches up to 50, shown cut with `...`
(`codex/011-prune-versioned-c...`). The sample is real and therefore says nothing about the escaping tests (SC-009), which use fixtures.

**D17 (Info, corrected). Risk levels I could argue with.** Besides D13, one class: 11 of the 40 branches of D3 (`bundledock feat/catalog-smill` and `feat/catalog-zk`;
`fix/release-ci-guard-retry` in chartscribe, sheetmill and sourcesteward; `sourcesteward docs/041-acceptance-evidence`; 5 in crewbook) are checked out in one of the 17 stale locked
registrations. The v3 planner keeps such a branch (`workspace-used`, `tidy_v3_refs.go:151-162`, matching any registration by `HeadRef` or `HeadOID`), but the wizard's
classifier applies `branch-attached-elsewhere` only to kept worktrees (`classify.go:500`), so it offers them as yellow (a fixture shows one offered as green). They should be
kept; lifting D3 alone would turn them into `the branches plan of X retains an item` (`v3planner.go:213-214`). Other spot checks hold: the 6 green rows have proof `ancestor` or
`no-net-change` or no own commit; the 7 task-record rows with recent activity are orange for the right reason; the 70 orange workharbor rows say "no record says whose it is" and R1
for 22 of them (21 in a later rerun: the 24 to 72 hour window moves with the time of the run); the 53 integrated record-missing worktrees that are red for untracked or uncommitted
files are consistent with the red rule.

**D18 (Low, found in review; the 11 branches above are part of the blocking class). Further wording and layout.** (a) `This deletes 1 directory for good and 0 branches (restorable).`
names a zero count with "(restorable)" (`summary.go:36`). (b) The progress block prints the British `analysed` (the state value, `classify.go:276`) while the overview says
`analyzed`. (c) The overview numbers lines 1 to 10 while the steps are numbered 1 to 17: overview line 6 (72 directories) is `Step 9 of 17`, and `8 branches + 4 smaller groups`
stands for 4 groups in all (steps 5 to 8), which reads as "plus 4". This document uses step numbers. (d) `Why orange` names R1 ("changed in the last 3 days") for a whole group
although it applies to 21 or 22 of 72 rows.

## Release-blocking proposal

- D1, D2, D3, D4 and the 11 stale-attached branches (D17) together: blocking for v0.2.0 as one class, "the wizard offers a row the planner refuses". Either the wizard does not offer such rows (it retains
  them with the code, as the comment of the planner says; for D2 the owner decided this on 2026-10-28 and the specification changes) or the planner accepts them, and the failure must name the group and not end the run. After that, T016 should be run again
  for the groups concerned. Without the fix the owner's one live element for #70 still works if it is one of the two branches of group 1 (RG reaches the typed prompt).
- D5, D6 and D7: not blocking; D6 contradicts the table of keys and should be fixed with the others; D5 is only a wrong label.
- D8 to D18: not blocking (the 11 branches of D17 excepted).

## Limits of this run

- No `--online` run: the credential of `gh` has the scopes `gist`, `project`, `read:org`, `repo` and `workflow`, so it is not read-only; the task allowed the online run
  only with a read-only token. No request is expected (`--offline` and the code; the namespaces of the runs had network access, so it was not measured; a rerun of the review in a namespace without network reproduced the same screens). The online levels (C2, the budget, SC-005) are therefore not dogfooded.
- The tool-driving of a pseudo-terminal by an agent is against the advice of `docs/tidy.md` for agents; it was done on a copy, with no apply and no store, at the owner's
  instruction for T016. The answers at "Store this plan set?" and "Apply it now?" were never reached: every run ended before or at the typed prompt (a wrong word was typed in
  RG, FIX); no plan set was stored and nothing was applied.
- `--workspace` was given; discovery from the working directory was not exercised on the copy.
- Groups 4 to 8, 10 to 17 were walked but not chosen to the end, except as stated in the table; the cause of D1 was found by experiment, not traced in the code.
- The copy is a snapshot (08:16 to 08:23 UTC); the live workspace changed while the runs went on (see the proof above).

## Rerun after the D1-D4 fix (stw #332, head ecbbe8e9fb717db16387ef6679cd26317527f5aa, 2026-10-29)

The wizard ran again as a dry run, offline, on the same copy (`--workspace /workspaces/Anchorage`, same namespace, uid 1000, pseudo-terminal driver, 120x50)
with the code of stw #332 at `ecbbe8e9fb71`. The repositories are the 2026-10-28 snapshot, so the counts are comparable; the copy was not taken again.
No apply, no store and no real confirmation word was typed. Runs repeated: R1 (overview, `q`), rA (group 1 `y`, group 2 `y`, Enter, wrong word),
rB (`a`, take all green) and `--json`. The other runs (RC, RD, RE, RE2, RF, RA, RB, RI) could not be repeated: their rows (directories and branches of other repositories)
are no longer offered.

**Result.** The wizard offers 3 of 1,248 candidates in 2 groups (earlier 193 of 1,222 in 17): group 1 is 2 branches in `.` (green), group 2 is 1 branch in
workharbor (yellow, `archive/w1-pre-ingress-f9ee51f5`). `--json` reports safe 3 and retain 1245 (green 2, yellow 1, orange 0, red 517, kept 728), exit 0.

| Finding | Before | After the fix |
| --- | --- | --- |
| D1, directories in `/tmp` | offered, then refused by the planner | 75 rows kept ("directory in a place that is not yours"); none offered, none reaches the planner |
| D2, owner known only from a commit author | offered, then refused by the planner | 75 rows kept ("no stw record and no owner label, only a commit author"); none offered |
| D3, locked or missing worktree registrations | offered, then refused by the planner | 29 rows kept; the overview prints the notice `worktree-registration-refused: every row is kept; run "git worktree unlock PATH", then "git worktree prune"` for 5 repositories |
| D4, late raw error | the run ended with a raw error | gone: the plan is built without error, `This deletes 0 directories for good and 3 branches (restorable).`, the typed prompt `Type "unknown 2878ca" to confirm`, a wrong word gives `Error: confirmation refused`, exit 1 |

D4 in detail. In rA all 3 offered rows were chosen: the summary reads `total: 3 items in 2 repositories` and all sub-plans, including the one of workharbor, were built.
The refusal with exit 1 is the intended one. In rB (`a`, take all green) 2 green items (2 branches in `.`) were planned without error, the typed prompt was reached, the wrong
word was refused, exit 1. Earlier, take all green had 6 green rows including 4 directories and failed.

The 29 registrations of D3 are cleaned by the owner by hand (owner decision 2026-10-29), as the notice says (`git worktree unlock PATH`, `git worktree prune`).
The branches of sourcesteward, bundledock, crewbook, chartscribe and sheetmill are not offered, and the 11 branches of D17 are not offered either.

**New observations.** (1) The wizard now offers only 3 rows, so the cleanup value for this workspace is nearly nil until the owner has unlocked and pruned the 29 registrations.
(2) The `Next:` line "Typed words" shows only `unknown`.

**Caveats.**

- The copy is not pristine: its state directory held 4 plan files from the first run and now holds 6 (two more from the refused typed confirmations here, by design).
- Planning of a directory row was not exercised, because no directory is offered.
- D6 (`q` at the edit prompt) was not exercised. D7 (skipped line) was seen only in rB, where `skipped: 1 group (1 branch)` is right.

Runtime: 2m34s to 2m58s per wizard run (R1 2m53s, rA 2m35s, rB 2m58s), `--json` 2m24s, about 13 minutes in all; the first-run analysis dominates as before.
