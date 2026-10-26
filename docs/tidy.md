# Evidence-based tidy

`stw tidy plan` reviews local Git branches or native jj bookmarks against an explicit base.
It reports every selected target as `delete` or `retain`, with native identities, evidence,
workspace blockers and capability limits. A `delete` decision describes eligibility in this
sample; it grants no authorization and the planning command performs no deletion.

```sh
stw tidy plan /path/to/repository --base refs/heads/main --json
stw tidy plan /path/to/repository --base refs/heads/main --ref refs/heads/finished-task --json
```

PATH defaults to the current directory. `--ref` is repeatable and literal; omitting it selects
all bounded local references. Git selections use full names such as `refs/heads/finished-task`;
jj selections use bookmark names such as `finished-task`. Names are not shell commands, globs or arbitrary revsets.
An unknown or duplicate selected reference refuses the plan. Git base selectors also require a full reference name or full commit ID.
The base is required; SourceSteward
does not infer `main`, `master` or `origin`.

## The tidy wizard

`stw tidy` cleans up a whole workspace instead of one repository. It is for the person at a terminal; agents use `stw tidy plan`
(see below). Outside a Git workspace it exits 2 with `workspace-not-found` (discovery is Git only and starts from the working directory; `stw tidy --workspace PATH` names another workspace, or use the
expert layer described in the rest of this guide).

```text
$ stw tidy
Workspace: /ws
Derived:   workspace /ws (discovery), remote:a origin (origin), remote:b origin (origin), online default (human wizard)
Mode:      human (stdin tty, stdout tty, no agent marker)
Analysed 2 repositories (0 skipped, 0 with errors)

Repositories:
a   base refs/heads/main
b   base refs/heads/main

owner claude (own)
  a
    [1] safe     worktree     /w/own1  task t1  integrated (ancestor)  effects remove-registration, remove-files
    [2] safe     branch       claude/old  integrated (ancestor)  effects delete-branch:claude/old
  b
    [-] retain   worktree     /w/dirty
    [3] safe     tracking-ref refs/remotes/origin/x  integrated (ancestor)  effects remove-registration, remove-files

owner codex (other)
  a
    [4] safe     worktree     /w/whr1  owner codex (path-convention) [other]  integrated (ancestor)  effects remove-registration, remove-files  not preselected

owner unknown
  b
    [5] decision worktree     /w/dec  owner unknown (unknown) [unknown]

summary: 4 safe (3 own, 1 other), 1 retain, 1 decision
Apply which? [a]ll own safe  [1,2,6 / 1-2,6]  [o]wner  [d]etails N  [n]one (default n):
Stored plan set 3f9c2a7e41b8 (5 effects in 2 repositories). Nothing has been changed.
Apply it now? Run: stw tidy apply 3f9c2a7e41b8 (or answer y here) [y/N]:
```

The sample is the real output of the wizard on the fixture of `TestTidyExampleMatchesTheDocs` (the answers `a` and `n` are typed and not echoed; the plan id is the first 12 hex characters of a SHA-256 over the plan content and is shown here as `3f9c2a7e41b8` so the sample stays stable).

**Reading the summary.** `summary: N safe (A own, B other)` splits the safe rows by scope: `A` counts scope `own`, and `B` counts every other safe row, so `B` includes the rows of scope `unknown` as well as those of scope `other`; the sample above has one `other` and no `unknown` safe row. In agent mode only an item owned by a task record is `own`: a branch or worktree that merely follows the naming convention, such as `claude/*`, shows `owner claude (path-convention) [unknown]` even when the caller is `claude`, appears under `owner unknown`, is counted in `B`, is refused for an agent apply (`scope-not-authorized`) and needs the typed confirmation of a human at a terminal. The summary does not number `unknown` rows separately; they are listed under the heading `owner unknown`, and `--json` gives each candidate's `scope`.

**Flags.** Bare `stw tidy` takes `--workspace`, `--mine`, `--by-owner`, `--member`, `--member-base`, `--only`, a repeatable `--state-dir`, `--online`, `--offline`, `--timeout`, `--json`, `--human`, `--rows`, `--verbose`, `--quiet`, `--progress`, `--trace` and `--contract 4` (read-only overview 4, below); `stw tidy plan` takes the same and `--save`. Bare `stw tidy` refuses `--yes` and `--save` with exit 2 (`--yes` belongs to `apply PLAN-ID` and never authorizes a plan made in the same run; `--save` is `plan` only). `stw tidy --json` prints the overview document like `stw tidy plan --json`: it never prompts, stores or applies and has no `plan_id`.

**Online reads (Spec 044, D13).** The human wizard, that is bare `stw tidy` without `--json` on a terminal for both stdin and stdout and without an agent marker, reads GitHub by default; `--offline` turns that off and makes no network read. The reads are the contract-3 tracking evidence (so tracking references now appear in the human wizard) and the pull request evidence of open tips; both are GET requests to github.com through the fixed bounded hosting transport with the credential of `gh`, and no write call exists. `--online` with `--offline` is a usage error (exit 2). Everything else stays offline unless `--online` is given: `stw tidy plan`, `--json`, `--save`, agent mode and the expert layer (a `PATH` or any expert flag). `--offline` is accepted on `stw tidy` and on `stw tidy plan`, where it changes nothing; with a `PATH` or an expert flag it is refused with exit 2 like every wizard flag.

The pull request evidence asks, per repository, about at most 64 distinct tips: first the safe rows whose proof is only `patch-id` or `merge-noop` (sorted by row id), then the rows retained as `unmerged` (sorted by row id); a strong proof is not asked and the rest is not checked. For each tip it sends one `GET repos/{owner}/{repo}/commits/{tip}/pulls?per_page=30&page=1`, keyed by the full object id, and keeps only pull requests of the same repository whose head is exactly the tip. One session per repository keeps the limits of the contract-3 reads (128 requests, 32 MiB, 30 seconds); one run budget of 512 requests, counted before they are sent, serves the tracking reads of the observation and these reads together. The fact per tip is `merged-head` with the pull request number (a closed pull request merged into the default branch of the repository), `closed-unmerged` or `unknown`; a tip that was not asked has no fact. `unknown` also covers an empty answer, a fork, a pull request merged into another branch (a stacked child) and a commit GitHub does not know: the read never says that no pull request exists. A merged head can only raise a safe row to strong, a row retained as `unmerged` keeps its level, and nothing online is a deletion proof by itself.

A repository whose reads cannot run is judged offline on its own and the run goes on with the levels of an offline run: `gh` is not authenticated, the remote is not a github.com repository, one repository ran into its own request or time budget, a response exceeded its size bound, or one read failed. A 429, a 403 that reports an exhausted rate limit (`X-RateLimit-Remaining: 0` or `Retry-After`), a 5xx, the cap of 512 requests or the end of `--timeout` judges every repository that has not finished offline, tracking reads included; nothing is retried. The wizard says so once on stderr, `GitHub: checked N of M repositories` (`repository` when M is 1), followed by `; the others are judged offline (` and the reasons when some were judged offline, and the exit code does not change. The derived entry `online` of the overview text is `default`, `flag` (an explicit `--online`) or `off` (`--offline`, or every repository fell back, with `fallback: ` and the reasons); runs outside the human wizard print no such entry, so their output is unchanged.

**Explicit overview 4 (read-only first slice).**
`stw tidy --contract 4 --json` opts into the separate, unpublished
`schemas/v4/tidy-overview.schema.json` contract. Without `--json`, the same
explicit selector prints text. Both forms only observe: they never read a choice,
store a plan or apply cleanup, even at a human terminal. The default overview
and expert `plan` / `apply` dispatch retain their existing contracts.

The additional `registration_prunes` array describes owned, locked Git
registrations whose checkout directory is missing. Each row carries its own
native absence, ownership and recovery witnesses; integration ancestry is not
its proof. A matching legacy worktree row is replaced rather than counted twice.
The legacy `summary` continues to count legacy candidates; the text output shows
separate registration verdict counts. Owner views and `--only stale-worktrees`
filter the new rows too. `safe` describes observed eligibility and authorizes
nothing. jj cannot use this Git-specific operation.

This slice does not save manifest-2 plan sets or add wizard selection / apply-set
dispatch. Those remain later T012b integration work. Its `plan_id` is empty;
manifest-2 identities, when saving is implemented, use full 64-hex digests rather
than legacy 12-hex IDs. The combined candidate bound is 4096 and either output
format is bounded to 4 MiB. Exit 0 means complete observation, exit 1 means
incomplete evidence or truncated / failed output, and exit 2 means invalid
usage. Registration observation stays offline. Legacy reads follow the Spec 044
D13 policy above: only the human wizard is online by default; `--offline` opts
out. Agent runs, `plan` and `--json` keep their offline default.

Only visible registration rows suppress matching legacy worktree rows, by exact
repository and destination. Output bounds may then cut either representation;
`truncated: true` and exit 1 disclose that a target can be absent from both arrays.
The unpublished v4 schema keeps all objects closed. Candidates admit only the
optional typed Spec 044 fields `risk`, `risk_reasons`, `group` and `pr`; the
summary admits optional `risk` counts as a closed object with five nonnegative
integer counts. Derived records keep their existing fields. Required legacy
fields remain unchanged. Spec 044 T008 must update the v2 schema and its freeze
digest together with its producer tests; this PR does not modify frozen v2/v3
schemas or emit the new risk fields.

**Observation window.** The analysis observes each repository under a deadline of 60 s plus 1 s per worktree (jj: workspace) and per local branch (jj: bookmark) of that repository, at most 10 minutes; the Git proof phase gets the rest of that window, at least 60 s, so without `--timeout` it can run up to 60 s past the window; with `--timeout` the given deadline bounds everything. `--timeout` is the deadline over the whole analysis; when given it also replaces that window, so it may be larger than the old fixed 60 s, up to 1 hour (more is a usage error, exit 2). The per-call limits (10 s per jj workspace, 10 s per proof command, 20 s per tip) and the plan, apply and restore deadlines of the expert layer are not scaled. When candidates are retained as `evidence-incomplete` with the evidence `deadline-exceeded` or `proof-budget-exhausted`, the overview ends with a notice `N of M not judged (deadline or proof budget); retry with a larger window, for example --timeout 15m`: N is the number of those candidates (`--timeout` widens the window, the content reads and the proof phase, but not the 20 s per-tip proof budget nor the per-worktree content budget, which also end as `proof-budget-exhausted` and are counted too, so the hint can fail for those items) and M the number of all candidates of the overview, counted after the view is narrowed (`--only`, `--mine`, `--by-owner`) and before the output is cut at the candidate bound. The notice is the text line `notice: ...` and an entry of the `notices` array of `--json`.

### The grouped overview and the retain table

The text of `stw tidy plan`, and of bare `stw tidy` without a terminal or in an agent run, is a short grouped overview followed by a
retain table (Spec 044). Internal codes are replaced by sentences; the codes stay in `--json` and `--rows`. The `Derived:` and `Mode:` lines
are those of the row view. This is the real output on a recording of the owner's workspace of 2026-10-26 (7 repositories, 1,129 candidates;
`testdata/workspace-20261026.rec` of `internal/tidywizard/render`, tested against these bytes):

```text
Workspace /workspaces/Anchorage   7 repositories analyzed
Derived:   workspace /workspaces/Anchorage (discovery), remote:. origin (origin), base:. refs/heads/main (refs/remotes/origin/HEAD), remote:bundledock origin (origin), base:bundledock refs/heads/main (refs/remotes/origin/HEAD), remote:chartscribe origin (origin), base:chartscribe refs/heads/main (refs/remotes/origin/HEAD), remote:crewbook origin (origin), base:crewbook refs/heads/main (refs/remotes/origin/HEAD), remote:sheetmill origin (origin), base:sheetmill refs/heads/main (refs/remotes/origin/HEAD), remote:sourcesteward origin (origin), base:sourcesteward refs/heads/main (refs/remotes/origin/HEAD), remote:workharbor origin (origin), base:workharbor refs/heads/main (refs/remotes/origin/HEAD), state-dirs /home/vscode/.local/state/stw/workspace (7 records) (default)
Mode:      agent (stdin no tty, stdout no tty, no agent marker)

Can be cleaned (165 of 1,129 candidates in 15 groups)                                   risk
   1  40 directories  /tmp/whr*          files deleted, codex                           yellow  (2 green, 38 yellow)
   2  24 branches     in sourcesteward   reversible, 3 owners                           yellow
   3  15 directories  /tmp/whr*          files deleted, branch too, codex               yellow  (1 green, 14 yellow)
   4  9 branches      in bundledock      reversible, hint Werner Stein and claude       yellow
   5  5 branches      in crewbook        reversible, hint Werner Stein                  yellow
   6  51 directories  /tmp/workharbor*   files deleted, hint Werner Stein               orange  (1 green, 50 orange)
   7  7 directories   agent scratchpads  files deleted, hint Werner Stein               orange
   8  4 directories   other in /tmp      files deleted, hint Werner Stein               orange
   9  3 directories   /tmp/crewbook*     files deleted, branch too, hint Werner Stein   orange
  10  6 more groups   7 rows in all      the smallest groups; their keys are in --json  orange  (2 green, 3 yellow, 2 orange)
Undo (branches only): stw tidy restore PATH --contract 3 --plan PLAN_ID --state-dir DIR --apply --authorize
      PLAN_ID: see stw tidy history --contract 3 --state-dir DIR; deleted directories are not restored

Kept (964): nothing here is offered for deletion
  394 may hold unique work (red): not merged, uncommitted or untracked files, not judged
  570 kept for another reason (record missing, branch in use, recently active, ...)
notice: remote-tracking references are not read without --online (7 repositories)
notice: Evidence is sampled and nontransactional; nothing was changed and nothing was fetched. (7 repositories)
The rows with their codes: stw tidy --rows (10 per repository and owner)   Every row: stw tidy --json
```

```text
Kept: 964 candidates. Nothing here is offered for deletion.
  count  level  why                                                                              what to do
    316  kept   created by stw, record not found; integrated, not checked for uncommitted files  re-run with the --state-dir that created it
    301  RED    not merged: it has commits the base lacks                                        nothing is deleted; review: stw tidy plan REPO --contract 3 --report-dir DIR
    217  kept   branch is checked out in a kept worktree                                         decide on that worktree first, then run again
     47  RED    created by stw, record not found; its commits are not on the base branch         do not delete; find the right --state-dir and judge it
     27  kept   changed within the last 24 hours; integrated, not checked for uncommitted files  wait, then run again
     23  RED    untracked or ignored files                                                       look inside; remove them yourself if junk
     17  RED    not judged (for example a deadline or Git refused)                               assume unique work; after a deadline run again with --timeout 15m
      7  kept   main checkout of a repository                                                    never offered
      6  RED    uncommitted changes                                                              commit or discard them yourself
      3  kept   worktree entry whose directory is gone                                           no stw action yet
Rows with their codes: stw tidy --rows (10 per repository and owner); every row: stw tidy --json
```

**Groups.** A group is the rows of one effect class in one place: the branches of one repository (reversible), the tracking references of
one repository, the worktree directories, and the worktree directories that also delete their branch (a group of their own, `files deleted,
branch too`). Directories are grouped by place: agent scratchpads (a path component named `scratchpad`), a `/tmp` family (the leading lowercase
ASCII letters of the first component below `/tmp`, needing three distinct directories in all effect classes together), `other in /tmp`, and any
other parent directory. The recording has 15 groups (7 branch and 8 directory groups). The first screen shows at most 10 group lines: when
there are more groups, the smallest ones are folded into the last line (`6 more groups`) and keep their keys in `--json`. Lines are ordered by
their level, green first, then by size. The level of a group is the level of its worst row, and a group with several levels lists how many rows
each has. The owner is information only: one owner is named, two are named together, more are counted, and a commit-author hint is shown as
`hint NAME`.

**Levels.** `green`: the work is on the base branch as the same commits (`ancestor`, `no-net-change`), the branch is gone from the remote
(`github-branch-absent`), the worktree has no commit of its own, or a merged pull request had the tip as its head. `yellow`: the work is on the
base as equal patches (`patch-id`, `merge-noop`), for example after a rebase merge. `orange`: a row with one more fact. Each of these facts raises
a row one step, green to yellow or yellow to orange, never beyond orange: a worktree of another or an unknown owner whose last change is at most 72
hours old or unknown, and an equal-patches worktree that no record and no path convention claims; a `decision` row is orange. `red`: the row may hold
unique work: its commits are not on the base, it has uncommitted or untracked files, or it could not be judged. `kept`: kept for a structural
reason without a known danger. Red and kept rows are never in a group and never offered. A level is a presentation of the evidence and never permission: the verdict,
the proof, the choosable rows and the apply path are the ones of the analysis. The overview's activity time is the one the analysis already reads
for `recently-active`; where it is not known, as in the recording above (which stores no activity times), every worktree of another owner
is raised one step. The screen above assumes that every directory was last changed more than 72 hours before the run; without that assumption
the same recording has 2 green, 45 yellow and 118 orange rows instead of 6, 93 and 66, and the groups of directories are all orange.

**The retain table** has one line per reason and level of the kept rows: the count, `RED` (may hold unique work) or `kept`, a sentence and what to do,
largest first. The counts add up to the retained rows (here 964: 394 red and 570 kept). A reason that this version does not describe is
named, escaped, instead of being left out. The rows themselves are shown on request: `stw tidy --rows` (10 per repository and owner, with the codes) or `stw tidy --json` (every row). A kept worktree row says `integrated, not checked for uncommitted files` where the view does not claim to know its dirt; a worktree whose uncommitted or extra files were observed is red even when it is kept for another reason first.

**`--rows`** prints the row view of Spec 041, byte for byte as before (one line per candidate with the codes, grouped by owner, at most 10 rows per
repository in each owner group; `--json` lists every candidate). It is a wizard-layer flag (not accepted with a `PATH` or an expert flag, exit 2) and
changes nothing in `--json`. In this version a person on a terminal who runs bare `stw tidy` still chooses by the numbers of the row view; the
grouped steps of Spec 044 are not implemented yet.

**JSON.** The `tidy-overview` document gains optional members, present whenever the command knows the time of the run (`stw tidy plan --json`
and `stw tidy --json` always do): each candidate has `risk` (`green`, `yellow`, `orange`, `red` or `kept`), `risk_reasons[]` (stable words such as
`proof-equivalent` or `not-integrated`; absent for a kept row), `group` (an opaque key shared by the rows of one group, only for green, yellow and
orange rows) and `pr` (the number of a merged pull request whose head is the tip, only when such evidence is set), and `summary.risk` counts the
candidates per level. The existing members, their values and the order of the candidates are unchanged.

### Long runs: progress, `--verbose`, `--trace` and agent markers

A large workspace takes minutes: the analysis reads every worktree and proves every distinct branch tip. The repositories are analyzed four at a time, and inside one repository four worktrees, four branches and four tips at a time, so at most sixteen items are read at once.

**Progress.** When stderr is a terminal and the run is not `--json` or `--quiet`, a block of lines on stderr is redrawn in place as the analysis proceeds, at most five times a second. The first line is the whole run (repositories finished of all, elapsed time of the analysis, and the slowest repository so far once two have started); then comes one line per repository in the sorted order of the analysis: a mark (`·` waiting, `▸` running, `✓` analyzed, `!` finished with another state such as `error` or `skipped`, or stopped by a deadline or an interrupt), the name, the phase (`starting`, `discovery`, `observing worktrees`, `observing branches`, `proofs`, `tracking`), a bar with the items done of the phase, the time of that repository and, once a phase is 5 percent and 3 seconds in, a cautious estimate of the time left in that phase (an average over the phase, smoothed, rounded up to 5 seconds, prefixed `~`; it is not an estimate for the whole run). A finished repository shows its total time. The repositories are analyzed four at a time, so up to four lines run at once. A sample in the middle of a run of five repositories on a terminal 100 columns wide:

```text
stw tidy: 2/5 repos, 38s, slowest sheetmill 38s
✓ chartscribe   analysed            9.0s
✓ crewbook      analysed            14s
▸ sheetmill     tracking            [#-------] 2/9 38s
▸ sourcesteward observing worktrees [###-----] 412/1,098 38s ~1m05s left
▸ workharbor    observing worktrees [--------] 0/469 24s
```

The block is drawn with cursor-up, erase-line and erase-below sequences only (no alternate screen), so the terminal keeps its scroll-back. When the analysis ends, on success, error, deadline or Ctrl-C, the final block stays on the screen and the cursor is left at the start of the line below it, so that the output and the prompts that follow start on a clean line; a repository that did not finish is marked `!` and `stopped`, one that never started `not started`. This is the final block of the same run:

```text
stw tidy: 5/5 repos, 1m16s, slowest sourcesteward 1m06s
✓ chartscribe   analysed            9.0s
✓ crewbook      analysed            14s
✓ sheetmill     analysed            46s
✓ sourcesteward analysed            1m06s
✓ workharbor    analysed            1m02s
```

The size of the terminal is read from the terminal at every redraw (80 columns by 24 rows when it cannot be read), so a resized window is followed at the next redraw; when the window got narrower, the cursor moves up over the rows the old lines were rewrapped to, never over more than the screen holds. A line longer than the width is cut to one column less than it with `…`, counting wide characters and emoji (a rune followed by U+FE0F) as two columns, East Asian ambiguous ones as one, and never wrapping; the bar is dropped first. When the block would not fit in the height of the terminal minus 3 lines, the finished repositories collapse into one line such as `✓ 5 repos done (largest: workharbor 61s)`, and if running and waiting repositories still do not fit, a last line says how many are not shown. Names and paths pass the same escaping as everywhere else, so a repository name with an escape, carriage return or bidirectional character shows as an escape, never as a sequence. The marks and the header are colored (`✓` green, running and header bold, waiting dim) only when stderr is a terminal and `NO_COLOR` is unset or empty; stdout is unchanged byte for byte. The block needs a terminal that takes cursor movement and at least 5 rows: with `TERM` unset or `dumb`, or fewer than 5 rows, stderr gets the plain lines described below instead (every 10 seconds and at the start of each repository), and nothing is left at the end. When the locale (the first of `LC_ALL`, `LC_CTYPE` and `LANG` that is set) does not name UTF-8, the marks are ASCII (`.` waiting, `>` running, `v` analyzed, `!`) and the cut is `...`. A terminal that clips its old lines instead of rewrapping them (xterm, st, urxvt, the Linux console) loses up to its height minus one lines of earlier output above the block when the window gets smaller, because the redraw moves the cursor up over them; this is inherent to redrawing in place, and `--quiet` or `--progress` (plain lines) avoid it. `--quiet` turns the block off. Without a terminal on stderr nothing is printed; `--progress` (or `--verbose`) then writes an ordinary line at the start of each repository and otherwise at most every 10 seconds instead (`[3/7] sourcesteward: observing worktrees 412/1,098 (38s, ~1m05s left)`). `--json` never prints progress.

**`--verbose`** (on `stw tidy` and `stw tidy plan`) prints a report to stderr after the analysis: time per repository and phase with counts, the number and summed duration of the Git processes per repository, and those of the discovery on the line of the discovery (summed over concurrent processes, so it can exceed the wall time), the ten slowest items, and each repository and phase where the observation window ended. A sample from a real run on a small repository with two linked worktrees (the progress clock of the test moves one second per report, so the phase times are round; the Git counts, the times of the Git processes and the slowest items depend on the machine):

```text
Timings: total 17s
  discovery and references: 1.0s
  [1/1] fx: analysed in 13s
    reading references: 1.0s
    observing worktrees 3/3: 4.0s
    reading branch history 4/4: 5.0s
    proving integration 1/1: 2.0s
    git processes: 120, 105ms in total
  slowest 8 items:
    25ms  fx observing worktrees: /tmp/wt-a
    24ms  fx observing worktrees: /tmp/wt-b
    10ms  fx observing worktrees: /tmp/fx
    1ms  fx proving integration: /tmp/wt-a
    0ms  fx reading branch history: refs/heads/old
    0ms  fx reading branch history: refs/heads/main
    0ms  fx reading branch history: refs/heads/task-a
    0ms  fx reading branch history: refs/heads/task-b
  observation window: not reached
```

`--trace FILE` writes one JSON line per Git process of the analysis of the repositories to a new file (the processes of the discovery of the workspace are counted by `--verbose` but not written; the file is created after the discovery) (`repo`, `argv` without the fixed safety options and without URL credentials, `ms`, `exit`); an existing file is refused, a path inside the workspace or one of its repositories is refused with `trace-inside-repository` (exit 2: a new file there is an untracked file and would turn a safe worktree into a retained one; a path inside another linked worktree has the same effect on that worktree, so choose a path outside every worktree), and the file stops at 8 MiB with a `{"truncated":true}` line. For a long run, widen the window with `--timeout` (up to 1h) or narrow the work with `--member NAME`, `--only` and `--workspace`.

A terminal that stops accepting output holds up the worker that writes the progress block (the others drop their frames), and the end of the run waits for that write, so a stuck terminal can delay the end of a run past `--timeout`; `--quiet` avoids it.

**Colors.** The human text is colored only when stdout is a terminal and `NO_COLOR` is unset or empty (`safe` green, `retain` and owner and reason text dim, `decision` yellow, repository errors red, repository headings and effects bold). The colors wrap text that has already been escaped, so a name with an escape sequence stays visible as an escape, and without color the bytes are the plain text; `--json` is never colored and the prompts stay plain.

**Agent markers.** `stw tidy` treats the variables `STW_AGENT`, `CLAUDECODE` and every variable that starts with `CLAUDE_CODE_`, `CODEX_` or `AGY_` as agent markers (`internal/tidywizard/owner/mode.go`). With a marker in the environment the run is in agent mode even on a terminal, so it never prompts. The integrated terminal of an agent IDE extension inherits such a variable (for example `CLAUDE_CODE_SSE_PORT` in VS Code), and the overview says so on its `Mode:` line:

```text
Mode:      agent (stdin tty, stdout tty, agent marker CLAUDE_CODE_SSE_PORT)
```

List the markers of your shell with `env | grep -E '^(CLAUDECODE|CLAUDE_CODE_|CODEX_|AGY_|STW_AGENT)'`. To run as a person, use a terminal without them, or remove the variable for one command: `env -u CLAUDE_CODE_SSE_PORT stw tidy`. There is deliberately no option that declares a run human. The marker rule is an accident guard against a careless agent run (Spec 041 FR-006g), not a security boundary: an evasive process can unset the variable too. The protection is the typed confirmation of foreign and unknown rows, which is read from `/dev/tty` and never from stdin.

**Flow.** Analysis is read only and offline unless `--online`, except in the human wizard, which is online by default (see Online reads above). Each repository gets a derived remote (`origin`, else the single remote)
and base (the local branch of the remote's symbolic HEAD, else the remote-tracking reference); override a base with
`--member-base NAME=REF`. A repository whose remote or base cannot be derived is shown as skipped (`remote-ambiguous`, `base-ambiguous`,
`base-missing`) and makes the run exit 1 after the overview. Creation records are read from the shared state directory
(`${XDG_STATE_HOME:-~/.local/state}/stw/workspace` or `$STW_STATE_DIR`) and every `--state-dir`; the first valid store holds the plan set.

**Modes.** Human mode needs a terminal on both stdin and stdout and no agent marker (`STW_AGENT`, `CLAUDECODE`, `CLAUDE_CODE_*`,
`CODEX_*`, `AGY_*`). Every other run is agent mode, and `--mine` forces it. In the row view (`--rows`, and bare `stw tidy` on a terminal) a human sees every owner, grouped: one heading per owner (`owner NAME (own)`, `owner NAME (other)`, `owner unknown`; own first, other owners sorted, unknown last) with a sub-heading per repository, at most 10 rows per repository in a group, and the owner column;
nothing owned by another owner or of unknown owner is preselected. An agent should narrow the view with `--mine`; in agent mode only
items owned by a task record count as `own`. `--by-owner LABEL` narrows the view in any mode and never selects the mode. The mode is
recorded in the plan set, the apply journal and `stw tidy history`.

**Verdicts.** `safe` (every proof holds), `decision` (for example a `locked-stw` worktree whose lock does not match), `retain`
(a reason such as `unmerged`, `dirty`, `recently-active`, `registration-record-missing`, `evidence-incomplete`,
`stale-registration-no-plan-kind`) rows never carry an effect; items of another or an unknown owner are shown with their `scope` and are never preselected. Only `safe` and `decision` rows get a number.
A worktree whose directory is missing is shown as a `retain` row and is not pruned. Such a registration does not stop `plan --save`: the plan for the other items of the same repository is stored, and the stale registration stays untouched and bound into the stored native evidence (one that appears or disappears before the apply refuses with `evidence-changed`). Only a registration that Git marks `prunable`, that is not locked and whose directory is absent below a real parent directory is accepted this way; a locked, replaced or aliased one still refuses the repository with `unknown-workspace-path`.
The row view shows at most 10 rows per repository inside each owner group (`and N more`); `--json` lists all candidates.

**Prompts.** The answer is parsed strictly and the default is none. `a` chooses all safe rows of scope own (never `decision` rows and
never rows of other or unknown owners), numbers and ranges (`1,2,6`, `1-3`) choose rows, `o` asks per owner, `d N` shows the details of
row N, and Enter or `n` chooses nothing. Anything else asks again; after three invalid answers the wizard exits 1 and stores nothing.
A `decision` row asks a second confirmation that names its lock and path. A foreign item needs the typed confirmation read from
`/dev/tty` (the owner label, or `unknown`, followed by the first six characters of the plan id) while the plan set is built
and again when it is applied; `y`, standard input and `--yes` never replace it. With no terminal, in agent mode or when nothing is
choosable, the wizard prints the overview, stores nothing, never reads stdin and exits 0 unless the evidence is incomplete, which exits 1 as for `plan`: a repository that could not be analysed, a repository skipped as `base-ambiguous`, `base-missing` or `remote-ambiguous`, a state directory skipped as unsafe, or an overview cut at its bounds (`output-limit`).

**Plan and apply.** The chosen rows become one stored plan set with a content-addressed id; its sub-plans are the existing
reference, tracking and retirement plans. Storing changes nothing. `stw tidy apply PLAN-ID` applies the sub-plans in a fixed order with
fresh checks, a journal and a recovery reference before each native effect; a retirement's dependent branch deletion runs after, and
only after, that retirement succeeded. A partial failure is reported per sub-plan and exits 1; replaying a completed set is a verified
no-op. In an agent run, an `other` or `unknown` item is refused with `scope-not-authorized`; agents never apply foreign items and must
not drive the wizard through a pty wrapper or `send-keys`. The wizard's own `Apply it now?` question accepts only an explicit `y`, which
runs the same apply code for that plan id.

**Several sub-plans of one repository.** Every sub-plan stores the whole repository as it was when the set was planned, so the first
retirement of a repository would make the next one look changed (a registration is gone, a recovery reference exists). Apply therefore
explains exactly those effects: before it compares a sub-plan with the repository, it adds the effects recorded in the journal of every
other sub-plan of the same repository and state directory whose journal is complete, and those of the branch deletions this run applied,
to the stored baseline. The same baseline serves the integration proofs that are re-evaluated at apply. Three owned worktrees of one
repository retire in one run; retirement and branch sub-plans of a repository explain one another. Nothing else is tolerated: a new
commit, branch, worktree or reference and a new entry beside a target or its registration directory still end the later sub-plan as
`stale` with `evidence-changed`, a dirty or changed target is refused as before, and a sub-plan that failed or was skipped, or whose
journal is not complete, explains nothing. Limit: the branch deletion that follows a retirement is planned and journaled at apply time
under a new plan id that no stored record links to the retirement. A run knows only the branch deletions it applied itself. If an
earlier run applied a retirement with its branch deletion and a later retirement of that repository was not applied in that run (it was
stale, refused, failed or never reached), the later run cannot explain the earlier branch deletion, so that retirement reports `stale`
and needs a new plan.

**Non-interactive use.**

```sh
stw tidy plan --mine --json            # overview of the caller's own items (needs $STW_OWNER, else exit 2; use --by-owner NAME)
stw tidy plan --save --json            # store the safe own items; prints plan_id
stw tidy apply PLAN-ID --yes --json    # explicit id authorizes; refused without an id
```

**Exit codes.** 0 analysis complete (also with retained or other-owner candidates, and the no-terminal degradation when the evidence is complete) or apply fully
completed. 1 refusal, incomplete evidence, a repository `error`, a repository skipped as `base-ambiguous`, `base-missing` or `remote-ambiguous`, an unsafe state directory, stale or failed sub-plan, cancellation
(`--timeout`, interrupt), a store problem, or output cut at its bounds (`output-limit`); the overview is printed first. 2 invalid usage:
mixed layers, `--yes` without an id, `--plan` with a positional id, unknown `--only`, `--mine` without `$STW_OWNER`, a missing workspace.

The JSON form is the `tidy-overview` document (`schemas/v2/tidy-overview.schema.json`, not yet published to Pages).

### Lifecycle and position rule

The lifecycle of a task worktree is *workspace -> work -> retirement*. `stw workspace apply` creates it detached at the recorded
creation commit and locks it `stw:<task>`; the agent then works in it, typically `git switch -c <branch>`, commits and pushes
(`git push -u` writes `branch.<b>.remote` and `branch.<b>.merge` into the common config); the branch is integrated into the base,
possibly by rebase-merge with different commit ids; the wizard then retires the worktree.

An owned retirement used to be refused as `changed-ownership` after such a branch switch (issue #174). It now follows a
**position rule**: HEAD may be the detached creation commit, attached to a local branch, or detached at a descendant, provided its
history contains the recorded creation commit; an unrelated checkout is still `changed-ownership`. Retirement checks the identity of
the worktree (the `stw:<task>` lock, the gitdir backlink, the worktrees parent, the layout and the creation journal entry) and the
**current** safety state, instead of requiring the safety digest of the creation time: an unrelated config key, a Git upgrade or the
upstream written by `git push -u` no longer blocks it, while a hook or key added between plan and apply refuses with
`evidence-changed`. Integration is proved on the real HEAD with the same forge-free proofs as local references (ancestor, `patch-id`,
`merge-noop`, `no-net-change`). The proof-policy flags `--integration-proof` and `--accept-whitespace-differences` belong to the expert
layer (`stw tidy plan PATH`), not to the wizard. In the overview a head the proofs do not cover is `retain` `unmerged`, and a `retain` row cannot be chosen. If a row that was `safe` when observed is no longer proven when the retirement plan re-proves the head, the plan retains it with `integration-not-proven` and building the plan set fails with that reason. The plan binds HEAD reference, commit id, branch tip and base, so any change refuses with
`evidence-changed`. A worktree whose HEAD is attached to the base branch itself never gets a branch deletion.

When the owner chose `delete-branch:NAME` for a retired worktree, the **dependent branch leg** runs right after, and only after, that
retirement succeeded: the branch is re-observed and deleted through the unchanged local-reference rules (recovery reference and
journal included). The leg is derived at apply time, not stored; a failed retirement skips it (code `retirement-not-applied`), a branch
that is not the retirement plan's head refuses the whole set (`branch-leg-mismatch`), and a chosen deletion without a leg is skipped
(code `branch-leg-absent`), so the set is `partial`.

### Derived defaults

Every value the wizard assumed is shown in the overview and in `derived[]` (`name`, `value`, `source`, `overridden`).

| Value | Derivation |
| --- | --- |
| Workspace | `--workspace`, else discovery from the working directory |
| Members | the direct members plus the workspace repository `.` (notice `workspace-members-none` when there is no other member); an uninitialized member is named in a `submodule-uninitialized` notice |
| Remote | `origin`, else the single remote; several remotes without `origin` are `remote-ambiguous` |
| Base | the local branch of the remote's symbolic HEAD, else the remote-tracking reference; `--member-base NAME=REF` overrides; none: `base-ambiguous` or `base-missing`; the wizard never fetches |
| Caller label | `$STW_OWNER`, else (human mode only) the operating-system user; `--mine` without one is a usage error |
| Item owner | the task record, then path and branch convention (`/tmp/whr*` is `codex`; the branch prefixes `codex/`, `claude/` and `claude-`), then a commit author hint that never makes an item `own`, then `unknown` |
| State directories | the repeatable `--state-dir`, `$STW_STATE_DIR`, the shared default; the first valid one holds the plan set |

### Reason codes

Every code below is a string of the code base; the file names are the places to check.

**Why an item is kept (`retain`, or the one `decision`).** The reason is the `reason` of the candidate and the evidence line says why
(`internal/tidywizard/classify.go`).

| Applies to | Reasons |
| --- | --- |
| Worktrees, automatic protections | `primary-checkout`, `current-worktree` |
| Worktrees, in the order they are tested | `evidence-incomplete` (an observation failed or was bounded, or no activity time was observed for an item of another or unknown owner), `locked-foreign` (locked by something other than stw), `registration-record-missing`, `registration-record-ambiguous` (an stw lock whose creation record is in no known store, or in several with different content), `dirty`, `ignored-extras` (untracked or ignored files the stored tree does not hold), `unmerged` (commits ahead of the base), `recently-active` (HEAD, index or a tracked file changed within 24 hours, only for an owner other than the caller or unknown), `jj-retirement-unsupported` (a jj workspace owned by a creation record; no plan kind retires one yet) |
| Worktrees, the `decision` row | `locked-stw`: an stw lock whose creation record does not match; choosing it asks a second confirmation naming the lock |
| Worktrees whose directory is missing | `unregistered` (no stw lock), `registration-record-missing`, `registration-record-ambiguous`, `lock-mismatch` (an stw lock whose creation record does not match), `evidence-incomplete` (the Git administrative entry is not provably prunable), `stale-registration-no-plan-kind` (prunable, but no plan kind removes it yet) |
| Branches | `evidence-incomplete`, `unmerged` (not proven integrated), `branch-attached-elsewhere` (checked out in a worktree that is retained or kept), and for jj the keep reason of the native tidy passed on unchanged (for example `remote-counterpart`, `recovery-reference`, `selected-base`, `workspace-used`) |
| Remote-tracking references | `evidence-incomplete`, `unmerged` (not integrated into the base), and the observer's reason for a reference that stays: `remote-source-present`, `symbolic-reference`, `selected-base`, `workspace-used`. Any other reason is replaced by `evidence-incomplete` with the evidence text `tracking-reason-unrecognized` (`adapter.go`). `github-branch-absent` is not a keep reason: it is the proof of a deletable reference, which is `safe` |

A `safe` worktree without an stw lock carries the reason `unregistered`, and the evidence `no-work` when it has no commit of its own.
Integration proofs of a safe row are `ancestor`, `patch-id`, `merge-noop`, `no-net-change`, `empty-working-copy-integrated-parent`
(jj) and `github-branch-absent`.

**Why a chosen item cannot become a sub-plan** (building the plan set fails; nothing is guessed;
`internal/tidywizard/planset/v3planner.go`):
`plan-repository-unknown` (the repository has no path), `plan-base-missing` (no derived base), `plan-shape-invalid` (an empty
sub-plan, an unknown kind, or a retirement that does not cover exactly one worktree), `plan-state-dir-missing` (no plan store),
`branch-name-unmappable` (not a short branch name), `tracking-remote-missing` (no derived remote), `tracking-online-required` (tracking
references are planned only with `--online`), `tracking-ref-unmappable` (not a reference of that remote), `legacy-owner-unknown` (a
legacy completion needs one plain owner label, not an unknown owner or an author hint), `stale-registration-no-plan-kind`, and the reason
of the creation-record lookup (for example `registration-record-missing`). When the existing plan kind itself retains a chosen item,
that plan's own reason is reported (for example `integration-not-proven`; otherwise `retirement-not-eligible`, `no-candidate` or
`retained`). A planning failure carries the diagnostic code of the failed plan. Before any sub-plan is made, choosing a `retain` row, a `decision` row without its second confirmation, an unknown id, or an item that is not plannable in the current mode (for example an `other` or `unknown` item in agent mode) fails with the untyped error `item cannot be planned: ...` (`planset/planset.go:344-364`).

**Why an apply is refused** (status `refused`, nothing changed, exit 1; `internal/tidywizard/planset/apply.go`):
`mode-evidence-invalid` (the mode evidence cannot be recorded, for example an overlong `STW_OWNER`), `plan-set-not-found`,
`plan-set-invalid` (the stored set fails its id verification; sets from before the branch-leg decision are not migrated),
`scope-not-authorized` (an `other` or `unknown` item in an agent-mode apply, or without `authorized_scopes`), `confirmation-refused`
(the typed confirmation failed or there is no terminal), `authorization-required` (no explicit plan id) and `branch-leg-mismatch`.

**What an apply reports per sub-plan.** The status is `applied`, `stale`, `failed` or `skipped`. A `skipped` sub-plan carries the code
`cancelled` (the run was cancelled before it), `retirement-not-applied` (the retirement a branch deletion depends on did not succeed) or
`branch-leg-absent`. A `stale` sub-plan carries `evidence-changed` (or a code containing `stale`); a `failed` one carries the diagnostic
code of the native apply. The set status is `ok` (every sub-plan applied), `failed` (none applied) or `partial`; anything but `ok` is
exit 1.

## Default v1/v2 evidence and retention

Only native ancestry proves integration in the default v1/v2 contracts. Squash equivalence and
forge proof are unsupported there; those invocations never contact GitHub or another forge. A missing upstream, empty commit,
branch-name match or merged PR alone provides no deletion permission.

Checked-out references, workspace-used revisions, the selected integration base, conflicted or
ambiguous bookmark targets and uncertain integration remain retained. Git detached worktrees
also protect references pointing at their HEAD. jj bookmarks and changes are distinct: removing
a bookmark would not imply abandoning a change. Remote-associated jj bookmarks remain retained.

Native Git commit IDs may use SHA1 or SHA256. jj commit, change and operation IDs remain separate.
Native jj observes pinned stored state without snapshotting; disk cleanliness is not established
by stored-unsnapshotted state. Owned task retirement has a separate versioned plan and explicit registration/files selections; see below.

## Stateless and stored plans

Without `--state-dir`, planning writes no plan store and returns an empty `plan_id`.
Native repository references, registrations, files, configuration and jj operations stay unchanged;
native sandbox setup may create private temporary isolation files outside the target.

To save an immutable plan, provide an already existing private mode0700 state directory:

```sh
stw tidy plan /path/to/repository --base refs/heads/main --ref refs/heads/finished-task --state-dir /private/task-state --json
```

The store must be disjoint from repository/workspace metadata and task destinations. Persistence
writes only disclosed private state records. Store access is the trust boundary; a saved plan
is still evidence, not authorization. Apply supports authorized local reference deletion with durable recovery; see below.

## Bounds, output and failures

Offline planning has a sixty-second total deadline (it was a fixed ten seconds that a loaded runner or a repository of a thousand files could exceed; it is deterministic, not scaled by the repository), a 1 MiB encoded record/result limit (8 MiB for retirement plans, which record every file) and
an aggregate observation limit of 1024 entries. Complete base/target tree metadata and
all-path filter observations use at most 4,325,376 stdout bytes and 4,224 bytes per logical
record, retaining the 16,384-leaf, 4,096-byte path, 16 MiB/blob and 64 MiB aggregate blob
bounds. The 16,384-leaf limit is shared with workspace creation; in tidy it only bounds cost, because these
streams are all-or-refuse (final delimiter, exit status and EOF required) and evidence is never truncated.
The limits that decide a deletion keep their values. Other native output and every stderr channel remain bounded to 64 KiB. Paths and selectors
are bounded UTF-8 text of at most 4096 bytes; IDs are at most 128 bytes. Missing native tools,
unsupported versions, unavailable sandboxing, malformed or truncated output, cancellation,
ambiguous/GitButler ownership and changed observations refuse or report incomplete evidence.
There is no unsandboxed retry or backend fallback.

Human output safely quotes opaque text and reports the same evidence as `--json`. Complete
operational results use the independent [tidy schema v1](../schemas/v1/tidy.schema.json).
Exit 0 means complete planning, including retained candidates. Exit 1 means refusal, incomplete
or partial evidence, cancellation or operational/output failure. Exit 2 means invalid CLI usage.
Writer failures may leave incomplete JSON; always check the exit status and parse validity.

Observations are samples, not a transaction. External changes and ABA races cannot all be ruled
out. Apply revalidates each action and records durable recovery before effects; remote
reference deletion, automatic garbage collection, jj abandonment, hooks and network are excluded.

## Authorized local reference cleanup

Create a fresh stored plan, review its candidates and declared recovery/deletion effects, then
explicitly authorize that exact plan:

```sh
stw tidy plan /path/to/repository --base refs/heads/main --ref refs/heads/finished-task --state-dir /private/task-state --json
stw tidy apply --plan GENERATED_PLAN_ID --state-dir /private/task-state --authorize --json
```

The apply command accepts no repository path, base or replacement targets: it loads the immutable
plan from the same private store. Omitting `--authorize` produces an operational refusal, including
structured JSON when requested. A stateless plan cannot be applied. Plans created by the earlier
planning-only WI lack the reviewed mutation effects and refuse with `replan-required`; generate and
review a new plan rather than editing a stored record.

Before each eligible action, apply checks the selected base, native tip, references, workspace
registrations/identities and safety evidence again. A moved tip/base, new workspace or unrelated
reference/operation change refuses stale proof. Git creates an exact private recovery ref before
old-OID compare-and-swap deletion. jj creates an exact native recovery bookmark before deleting the
selected local bookmark, checks its own operation parent and full allowed reference delta, and never
abandons the underlying change. Remote-associated jj bookmarks remain retained. Neither backend
fetches, pushes, runs hooks or garbage collects recovery roots.

Git cleanup and owned retirement reuse the [workspace layout proof](workspaces.md), including
initialized submodule and separate-Git-directory primary or linked sources. Replacing a
layout pointer or common config, even with identical bytes and unchanged commit IDs, stales
stored evidence. Apply rechecks before recovery and the next deletion/removal; an already
created recovery root stays retained after refusal. A linked task never owns the primary
checkout named by shared `core.worktree`. Layout admission grants no superproject, primary
checkout or sibling metadata writes. Fresh CLI/version acceptance remains a separate
[verification gate](../specs/010-isolated-task-workspaces/verification.md#issue-62-source-implementation).

A durable journal is recorded before native effects. Human/JSON output identifies its location,
recovery pins, action outcomes and limitations. Recovery roots remain after success and protect
native committed identities; they do not back up untracked files, restore registrations/files or
promise a general undo command. Review recovery evidence manually before removing any native pin.

Apply has a sixty-second deadline with the same native-output, record and aggregate bounds as plans.
Cancellation or failure after native action start may leave partial effects; results report known
completed/attempted actions and recovery location. An incomplete journal is not permission to retry
blindly. A completed retry succeeds only after its recorded postconditions are verified. Sampled
checks cannot exclude every external or ABA race; jj deletion is not an atomic Git-style CAS.
Reference cleanup excludes workspace registration removal, filesystem removal and jj abandonment;
default owned retirement uses a separately reviewed version 2 plan.

## Owned task retirement

Retirement selects one completed SourceSteward task, its exact owner and an explicit integration
base. Creation plans, completed journals, native registration identity and physical destination
identity must agree. A matching path or task label alone provides no ownership proof.

```sh
# Plan native registration removal; jj preserves files. Git retains this selection as unsupported.
stw tidy plan /path/to/source --base refs/heads/main --state-dir /private/task-state --task task-id --owner owner-id --remove-registration --json

# Separately select the exact owned destination for filesystem removal.
stw tidy plan /path/to/source --base refs/heads/main --state-dir /private/task-state --task task-id --owner owner-id --remove-registration --remove-files /absolute/owned/task --json
stw tidy apply --plan GENERATED_RETIREMENT_PLAN_ID --state-dir /private/task-state --authorize --json
```

Use a native jj literal base/bookmark instead of the Git full reference in these examples. Retirement
flags are mutually exclusive with `--ref`. The private store, task, owner and registration selection
are required; `--remove-files` must equal the recorded canonical owned destination. These selections
are inspectable proposed effects, while `apply --authorize` grants execution of the reviewed plan.

Git cannot forget a present worktree registration while preserving its files. Registration-only
plans retain that unsupported action. With both selections and complete safe evidence, fixed native
Git removal performs the coupled registration and filesystem effects and removes only the exact
SourceSteward-owned lock. Both action outcomes are reported; no dirty-state override is offered.
jj registration removal natively forgets the exact workspace while preserving disk content. A
separate files action revalidates physical identity and the full manifest, removes only verified
entries through confined descriptors, and retains unexpected/new files rather than following links
or recursively deleting an inferred path. Forgetting alone never authorizes filesystem removal.

The current/source workspace, foreign or incomplete ownership, dirty/untracked/ignored-extra files,
conflicts, symlinks, special files, transformed/sparse trees, unsupported metadata and exceeded bounds
retain or refuse retirement. A full bounded stored/disk comparison includes executable modes and
native metadata identities. jj stored-unsnapshotted state alone is never cleanliness proof; an empty
working-copy commit is eligible only with one integrated parent, identical stored trees and a full
clean disk comparison. Known activity or uncertain freshness retains work; labels cannot establish
that every external editor or agent is idle.

Retirement uses [tidy schema v2](../schemas/v2/tidy.schema.json), with `plan: null` and an optional
`retirement` record (present for complete successful observations, potentially absent on early
failure). It binds original creation ownership, expected source/target identities, full physical
manifests, separate actions/effects/blockers and recovery limits. Reference cleanup retains schema
v1 unchanged. Dispatch on `schema_version`, including operational failures.

A private durable retirement journal and native recovery pin precede removal. Before each action,
apply rechecks ownership, integration, registration and physical state. Recovery retains native
committed content/identities and reports the journal location; it does not promise automatic file
restoration or recovery of untracked content. Interrupted or uncertain effects remain partial and
require manual review; no automatic rollback or blind retry is attempted. Completed retries verify
recorded postconditions and remaining files. Native writes and disk removal still have sampled
external/ABA race limits rather than a repository-wide transaction.

Retirement uses the same sixty-second planning deadline and an apply deadline that scales with the plan (60 s + 5 s + 50 ms per recorded entry), 8 MiB encoded record
bound (a retirement plan records every file and directory) and aggregate 1024-entry bound for references; each stored/disk raw file in the default v2 contract
is bounded to 64 KiB; the retirement blob sample and the manifest are 8,192 entries and user content
64 MiB in total, while the shared 16,384-leaf tree limit only bounds cost. Hooks, fsmonitor and submodule
policy stay strict for retirement. Oversized or
incomplete evidence never authorizes a truncated removal target. Remote deletion, garbage collection,
jj change abandonment and arbitrary forced cleanup remain excluded.


## Explicit Git contract 3

Source contract 3 adds Git-only merged-head proof, local tracking-ref cleanup and explicitly
completed legacy retirement. Select it with `--contract 3`; omission preserves the existing
v1/v2 dispatch and zero-network behavior. These new capabilities do not permit Git writes in
jj- or GitButler-owned workspaces. Source/native checks and remaining acceptance limits are
recorded in the Spec 029 integration evidence. The v3 schema is retained in the source tree at
`schemas/v3/tidy.schema.json` (the review report and the decision file have the candidate schemas
`schemas/v3/tidy-report.schema.json` and `schemas/v3/tidy-decisions.schema.json`, the restore and
history results `schemas/v3/tidy-restore.schema.json` and `schemas/v3/tidy-history.schema.json`);
none is part of the published schema inventory.

A local reference plan remains useful offline. Online merged-head evidence requires explicit
`--online --remote NAME` and one admitted GitHub.com/SHA-1 repository:

```sh
stw tidy plan /absolute/source --contract 3 --base refs/heads/main \
  --ref refs/heads/finished-task --online --remote origin \
  --state-dir /private/task-state --json
stw tidy apply --contract 3 --plan PLAN_ID --state-dir /private/task-state \
  --authorize --online --json
```

Optional `--repo OWNER/NAME` must agree with that remote; `--host` accepts only `github.com`.
An optional repeatable `--pr FULL_HEAD_OID=NUMBER` selects exact-head PR candidates. A branch
name only helps discovery. Proof binds stable repository/PR identities, the exact local tip,
the provider's complete merged tuple and its reported integration commit reaching the exact
chosen local base. The original PR base can be an intermediate stack branch. Amended tips,
forks, missing local integration objects, ambiguity or changed provider facts retain/refuse;
no patch equivalence, inferred PR chain or fetch substitutes for proof. Selected bases,
symbolic references and every registered workspace-used identity remain protected.

Online reads use one 30-second plan/60-second apply deadline, at most 128 HTTP requests and
32 MiB raw response bytes including failures/resampling, 1 MiB per raw/normalized body and
16 KiB per auth channel. Collections require proven exhaustion within ten pages/1,000 unique
records. Native processes remain network-denied. Apply requires `--online` exactly when the
stored plan declares network effects, alongside separate `--authorize` for its local effects;
a saved online plan never enables networking by itself.


### Review report bounds

`stw tidy plan` with `--report-dir` writes the review report; a reviewer decision (`--decisions`)
clears a `needs-review` branch only if its report entry is complete, so every bound that can cut
an entry has a flag. Defaults and ceilings (the plan binds them; the three per-entry ones only
when they differ from the default):

| Flag | Default | Ceiling |
| --- | --- | --- |
| `--report-max-entries` | 512 | 4,096 |
| `--report-max-bytes` (per file, Markdown and JSON) | 4 MiB | 16 MiB |
| `--report-max-diff-bytes` (per entry) | 64 KiB | 1 MiB |
| `--report-max-files` (per entry diff and gitlink list) | 16 | 4,096 |
| `--report-max-lines` (per entry diff) | 1,000 | 200,000 |
| `--report-max-commits` (per entry) | 50 | 1,000 |
| `--timeout` | scaled | none |

The bounds interact: the diff stops at the first of the file, line and byte bound, so past
roughly 5 bytes per line the line bound is never reached unless `--report-max-diff-bytes` is raised
as well (1 MiB holds about 200,000 short lines), and the report files must be big enough for the
entries (`--report-max-bytes`). An entry cut by any bound, including a truncated gitlink list, is
"not fully reviewed": its decision is ignored with a note. To review very large branches raise
all of them, for example `--report-max-entries 4096 --report-max-bytes 16777216
--report-max-diff-bytes 1048576 --report-max-files 4096 --report-max-lines 200000
--report-max-commits 1000`.

### Local tracking refs

Tracking selection is a separate mode. The selected remote must have one canonical fetch URL
and exactly one standard optional-plus full-head mapping; duplicate, negative, custom, mirror,
overlapping or rewritten mappings refuse. Select exact local tracking refs, or omit `--ref`
for the complete bounded namespace:

```sh
stw tidy plan /absolute/source --contract 3 --base refs/heads/main \
  --tracking-remote origin --online --ref refs/remotes/origin/finished-task \
  --state-dir /private/task-state --json
```

Two complete remote branch advertisements must agree at plan time. Fresh complete inventories
and native/configuration proof must still match before recovery and final prepared deletion.
A present source branch retains even at a different OID. Symbolic remote HEAD and its resolved
target, the selected base and workspace-used refs retain. Deletion preserves the exact old OID
in recovery and changes only the local tracking ref; there is no fetch, synchronization, prune,
server deletion or config repair. Remote absence is sampled and cannot prevent later recreation.

### One-operation legacy completion

Legacy retirement selects one exact canonical absolute linked worktree and owner label. The
completion flag declares that the task is complete, no person or agent is using it, and the
supplied protection set includes every caller-known active worktree. This is cooperative
one-operation authority, not authentication, activity detection, adoption or historical stw
creation evidence. Keep active worktrees protected explicitly:

```sh
stw tidy plan /absolute/source --contract 3 --base refs/heads/main \
  --state-dir /private/task-state --completed-worktree /absolute/completed-task \
  --owner operator --protect-worktree /absolute/active-task \
  --remove-registration --remove-files /absolute/completed-task --json
stw tidy apply --contract 3 --plan PLAN_ID --state-dir /private/task-state --authorize \
  --completed-worktree /absolute/completed-task --owner operator \
  --protect-worktree /absolute/active-task --json
```

Repeat the exact completion path, owner and protection set at apply. Newly known active work
requires refusal and replanning. Primary/current/source/ancestor, protected, foreign, locked,
dirty, ignored-extra, aliased, overlapping or incompletely observed paths remain blocked.
Native Git removal couples separately selected registration and exact-path file effects;
legacy removal has no force/unlock fallback and keeps its attached branch. Generate a fresh
reference plan after retirement before selecting that branch for cleanup. Online retirement
adds the same explicit remote selection and apply acknowledgment as other v3 online plans.

Apply has a deadline of 60 s plus 5 s per planned effect plus 50 ms per recorded manifest entry,
with no ceiling (the work bounds it, and the caller's context still ends it). Restore uses the
same rule (60 s plus 5 s per restored branch). `--timeout DURATION` on `tidy plan`, `tidy apply`
and `tidy restore` (contract 3) replaces the default deadline of the whole operation; the default
is the scaled formula above, and `0` or a negative value is a usage error. Restore reads the
repository and the journal before its deadline is set (bounded local reads that end only with the
caller's own context); the deadline then covers the evaluation and the branch creations. If the deadline still fires,
the result is `partial` and lists each ref's journaled outcome; unconfirmed deletions are never
replayed automatically. Restore needs the original absolute repository and state-directory paths
(a copy is refused as `journal-unreadable`); refs of an incomplete journal were not confirmed
deleted and restoring them is a no-op.

A plan retires exactly one worktree and needs `--state-dir`; `--report-dir` and the other review
report flags apply to reference plans only. Retirement refuses, with a named code and reason (also
without `--json`): `retirement-file-limit` (more than 8,192 tracked files, or more than 8,192 files and
directories together, or an oversized file; larger trees need a digest-only record, a follow-up), `retirement-unrecorded-entry` (an untracked, ignored or generated path the stored tree does
not contain), `retirement-sibling-refused` (the parent directory holds a symbolic link, device or
socket; its siblings are recorded as plain files and directories) and `git-tree-refused` (the
commit holds symbolic links or submodule gitlinks: native removal would also delete the Git
directory of an initialized submodule, so retirement, unlike creation, does not admit gitlinks).
Batching several worktrees in one plan is not supported.

Owned v3 retirement instead selects `--task ID --owner LABEL` with the original completed
creation records and exact owned-lock rules. It never substitutes a completion declaration
for owned provenance. Both Git-v3 retirement modes stream regular user files up to 16 MiB
and 64 MiB per user-content pass in buffers no larger than 64 KiB. Metadata remains 64 KiB
per file, except the registration `index` (up to 4 MiB, since it grows with the tracked file
count); old v2/jj policy is unchanged. Full content/identity checks run at three plan/eight
apply observations, with one shared byte/process/deadline budget and no reused fresh hashes.
Timestamp and inode checks do not exclude every same-user/ABA race.

### Restoring deleted branches and the journal history

A local-reference apply keeps a recovery ref `refs/stw/recovery/v3/PLAN/HASH` and a journal entry
for every branch it deletes. `stw tidy restore` creates the branch again from them. It is a
read-only plan by default and works on the clone it is pointed at (`PATH`, default `.`):

```sh
stw tidy restore /absolute/clone --contract 3 --plan PLAN_ID --state-dir /private/task-state --json
stw tidy restore /absolute/clone --contract 3 --plan PLAN_ID --state-dir /private/task-state \
  --action ACTION_ID --as refs/heads/finished-task-again
stw tidy restore /absolute/clone --contract 3 --plan PLAN_ID --state-dir /private/task-state \
  --apply --authorize --json
```

**Flags and ids.** `restore`, `restore --list` and `history` all require `--contract 3` and `--state-dir DIR` (an absolute canonical path of the private directory that holds the plan and its journals). Without them they exit 2 before any work: `--contract 3 is required`, then `--state-dir is required: the private directory that holds the plan and journals of the tidy apply`. Unlike `plan`, `apply` and `stw workspace`, these three do not read `$STW_STATE_DIR`: pass the directory even when the variable is set.

`PLAN_ID` is a sub-plan id, not the plan set id that `stw tidy plan --save` prints. One tidy apply runs one sub-plan per worktree, and each writes its own journal; `history` and `restore` key on the journal ids, so `restore --plan SETID` is refused with `plan-not-found`. The retirement of a worktree and the deletion of its branch are two journals:

| Journal mode | Plan id | `restorable` | What `restore` does |
| --- | --- | --- | --- |
| `owned-retirement` (worktree retirement) | the `Sub-plan:` id that `tidy apply` prints (`outcomes[].sub_plan` with `--json`) | always 0: a retired worktree is not restored | refused as `unsupported-mode` |
| `local-refs` (branch deletion, also the `branch-leg` outcome of a retirement) | its own id, which `tidy apply` does not print: take it from `history` | the deleted branches | plans or creates the branches |

So to restore the branch of a retired worktree, run `history`, pick the `local-refs` journals (the one written right after the retirement of the same apply is the branch leg), and read the branch of each with the read-only `restore --plan ID`. A real run of a two-worktree plan set:

```text
$ stw tidy apply 9a3a491b6c55 --yes
Tidy plan set: "9a3a491b6c55"
Status: "ok"
Sub-plan: "b0e76e648be0ffccf27bd0ebf3c7ddfc" repo="." kind="retire" worktree="/w/t-one" status="applied" code="" message=""
Sub-plan: "b0e76e648be0ffccf27bd0ebf3c7ddfc" repo="." kind="branch-leg" worktree="/w/t-one" status="applied" code="" message=""
Sub-plan: "b4b9aaca02903830abdf1d96e2fde7e6" repo="." kind="retire" worktree="/w/t-two" status="applied" code="" message=""
Sub-plan: "b4b9aaca02903830abdf1d96e2fde7e6" repo="." kind="branch-leg" worktree="/w/t-two" status="applied" code="" message=""
$ stw tidy history --contract 3 --state-dir /private/task-state
Tidy operation: "tidy-history"
Contract: 3
Status: "ok"
Total: 4 scan_truncated=false
Journal: plan="1e690ba8b5d5ca680b168109223ffa84" time="2026-10-26T03:56:37.408680671Z" mode="local-refs" deleted=1 retained=0 restorable=1 recovery_refs="present" decisions_human=0 decisions_ai=0 status="complete"
Journal: plan="b4b9aaca02903830abdf1d96e2fde7e6" time="2026-10-26T03:56:35.882674516Z" mode="owned-retirement" deleted=1 retained=0 restorable=0 recovery_refs="present" decisions_human=0 decisions_ai=0 status="complete"
Journal: plan="74946c45f3708d48a82b6e30d162ee00" time="2026-10-26T03:56:33.906666546Z" mode="local-refs" deleted=1 retained=0 restorable=1 recovery_refs="present" decisions_human=0 decisions_ai=0 status="complete"
Journal: plan="b0e76e648be0ffccf27bd0ebf3c7ddfc" time="2026-10-26T03:56:32.331660193Z" mode="owned-retirement" deleted=1 retained=0 restorable=0 recovery_refs="present" decisions_human=0 decisions_ai=0 status="complete"
$ stw tidy restore --contract 3 --state-dir /private/task-state --plan 9a3a491b6c55
Diagnostic: "plan-not-found" "No stored tidy journal has this plan identity."
$ stw tidy restore --contract 3 --state-dir /private/task-state --plan b4b9aaca02903830abdf1d96e2fde7e6
Diagnostic: "unsupported-mode" "Only local-reference journals can be restored."
```

The sample is real output on a throwaway workspace; each `Journal:` line is followed by a `Run mode:` line that is omitted here, `Diagnostic:` lines are the one relevant line of each refused result (the result also carries `Status: "refused"`, the plan id and counts, and the command exits 1), and the worktree paths are shortened.

Per journal entry that reached a confirmed recovery the plan shows the full branch ref, the tip
OID, the proof class, flags and the reviewer decision (who, kind, why), and one state:

| State | Meaning | Apply |
| --- | --- | --- |
| `restorable` | the recovery ref is a direct ref at the journal OID, the commit is readable and the branch name is free | creates the branch |
| `already-present` | the branch exists at the journal OID | nothing |
| `name-taken` | the branch exists at another OID, or is a symbolic ref (reason `symbolic-reference`) | nothing; `--as` restores one action under another name |
| `recovery-missing` | the recovery ref is absent (`recovery-ref-absent`), symbolic (`recovery-ref-symbolic`), points elsewhere (`recovery-ref-retargeted`), or its commit is gone (`object-unreadable`); a re-clone has no recovery refs | nothing; the reflog is never searched |
| `journal-incomplete` | the entry has no confirmed recovery, or its branch name is not a valid `refs/heads/` name (`invalid-ref-name`) | nothing; an incomplete journal is never replayed |

Journal text (branch names, reasons, reviewer names) is data: it is validated, escaped on output and
never passed to Git as an option. Only journals of plan mode `local-refs` are restorable; a
tracking-ref or retirement journal is refused as `unsupported-mode`. An unknown plan or action is a
refusal (exit 1); argument errors (a duplicate `--action`, `--as` without exactly one `--action` or
not a `refs/heads/` name, `--authorize` without `--apply` and the reverse, `--list` with `--plan`, a
malformed plan or `--limit`) exit 2 before any store or native work. The plan status is `ok` whenever
the journal was read, even if nothing is restorable.

`--apply --authorize` is the same explicit authorization as `tidy apply`; a plan or a reviewer
decision is never authorization. There is no stored restore plan: the effects are computed at apply
time and every entry is evaluated again immediately before its creation. A branch is created with
`git update-ref --no-deref` against the zero object ID, so an existing branch is never
overwritten or moved and a symbolic ref is never followed; a branch created in between is reported
as `name-taken` and a symbolic one as `symbolic-reference`. The new branch gets one reflog entry
`stw restore PLAN ACTION`. Apply keeps the recovery refs and touches no worktree, remote,
configuration, index, other ref or file; a repository whose hooks would run refuses first. Before each
creation an `intent` record is durable in the `tidy-v3-restores` namespace of the private state
store and an `outcome` record (`created`, `created-recovered`, `name-taken`, `symbolic-reference`,
`recovery-missing` or `error`) follows; records are appended, never rewritten, and the tidy journal is
never modified. A rerun is idempotent. After a crash between intent and outcome the rerun finds the
branch at the journal OID and records `created-recovered`, or finds it absent and creates it; it never
overwrites. A branch that someone else created at the same OID after the crash is reported
`created-recovered` too. The apply status is `ok` when every in-scope entry is `created`,
`created-recovered` or `already-present`, `partial` when some were created and others were not,
`refused` when nothing was created and an entry was not already present, and `error` for an
unexpected failure. `git branch NAME OID` stays a valid manual restore.

Only the tip commit and its history are restored, in this clone. Reflog tips (recorded as evidence
only), untracked files and worktree registrations are not.

A journal written by this version also records, before each branch deletion, the `branch.NAME.*`
configuration of exactly that branch (at most 16 keys, 256 bytes per value, a truncation marker) and the
reflog tip or `unavailable`; an unreadable reflog never stops a deletion. Journals written earlier
lack the record and restore exactly as before. The plan then adds per entry a `branch_config` with
the key names, the remote name of `remote` and `pushremote` (never another value) and one state per
key: `reapply`, `skip-remote-missing` (the remote is gone from this clone, or a recorded remote is not reapplied, which also leaves `merge` and `rebase` alone), `skip-not-recorded` (a key
other than `remote`, `merge`, `rebase` and `pushRemote`, which are never reapplied, or a key written without a value) or another `skip-*`
reason (`skip-already-set`, `skip-renamed` under `--as`, `skip-truncated`, `skip-multi-valued`,
`skip-not-restorable`). `--apply --authorize` writes only the `reapply` keys with `git config --local`
after the branch exists and reports each key `applied`, `skipped` or `failed`; a failed key never
removes the branch, adds the diagnostic `restore-config-failed` and makes the run `partial`. A rerun
writes nothing.

Tidy deletes only the branch reference (`git update-ref -d`); it does not remove the `branch.NAME.remote` and `branch.NAME.merge` keys of `.git/config`, so a deleted branch leaves them behind (a branch with both keys leaves two) until `git config --remove-section branch.NAME` removes them. The journal records them before the deletion, and the restore plan therefore reports each key still present as `skip-already-set`: nothing is written and the restored branch picks the existing keys up again. They are written back (`reapply`) only when the keys are gone. A leftover section of a branch that is never restored is harmless and is not touched by tidy.

`stw tidy history` and `stw tidy restore --list` (same reader, read-only, both with the required `--contract 3 --state-dir DIR` and the optional `--limit`, default 256, at most 1,024; the optional `PATH` is the clone: omitted it is the current directory, an explicit one must be an absolute canonical path and an explicit `.` is refused with exit 2) list the stored journals newest first by journal time: plan
ID, time, mode, numbers deleted, retained and restorable, whether the recovery refs are `present`,
`partial` or `absent`, the number of `human` and `ai` decisions and the journal status `complete`,
`incomplete` or `unreadable` (an unreadable journal is listed with its ID, never skipped). Journals of
other modes show their mode and `restorable` 0. The store scan stops after 4,096 records, the
commit checks are one batched `git cat-file --batch-check` of at most 65,536 lines and 4 MiB, and the
whole call has a 30-second budget. A bound that is reached marks the affected journals `incomplete`
and adds a truncation marker `{unit, shown, total}` (`journals`, `objects` or `checked-journals`);
`total` is a lower bound when `scan_truncated` is true.

V3 human and JSON results report completeness, sampled freshness, actual network attempts,
measured counters, precise candidates/actions, recovery and effects. Complete negative plans
exit 0; unavailable, changed, refused, partial or failed observations exit 1; malformed usage
exits 2. Incomplete evidence has no executable plan. Interrupted effects retain recovery and
report known outcomes; only a completed journal can retry after read-only postcondition
verification. Recovery preserves selected committed history, not untracked files, original
index/reflog/configuration, automatic worktree restoration or a general undo command.

### Owner-size acceptance tests

The normal test run covers worktree retirement, one-plan branch deletion and restore with small
fixtures (3 worktrees with 6 branches, and 20 branches). The owner-size runs (60 worktrees with 120
branches, and 120 branches, or `STW_SCALE_BRANCHES=200`) take many minutes and are not part of CI.
Run them by hand before a release:

```sh
STW_ACCEPTANCE_SCALE=1 go test ./internal/workspace -run 'TestTidyV3ScaleSixty|TestTidyV3ManyBranches' -timeout 60m -v
```

## Administrative registration restore (contract 4)

Contract 4 keeps its registration plans, prune journals and private recovery
payloads separate from branch recovery. Inspect one completed prune before
restoring its administrative entry:

```sh
stw tidy restore --contract 4 --plan PLAN_ID --state-dir /private/state --json
stw tidy restore --contract 4 --plan PLAN_ID --state-dir /private/state --apply --authorize --json
stw tidy history /repository --contract 4 --state-dir /private/state --limit 256 --json
stw tidy restore /repository --contract 4 --list --state-dir /private/state --limit 256 --json
```

`PLAN_ID` is the original full 32-character lowercase hexadecimal prune plan ID.
Inspection writes nothing. Mutation needs both `--apply` and `--authorize`, the
stored caller/mode and fresh typed confirmation for another owner's registration.
The original name must be unused, the original checkout must still be absent,
and the saved ownership, ancestor/mount witnesses, recovery payload and native
history pin must validate. An incomplete prune or prior restore refuses; a
completed restore also refuses another restore because the occupied slot cannot
be proved to be the same directory from the closed journal representation.

Applying restore compares the full stored caller/mode evidence, including
editor, agent and terminal markers. A later shell with different evidence is
refused even when the owner identity is unchanged. This slice does not relax
that comparison or migrate stored authorization evidence.

Restore copies the exact bounded metadata into a private staging directory in
the repository's common directory and publishes it at the original name with
native no-replace rename. A separate restore journal records intent and outcome;
the original prune journal and recovery payload stay unchanged. Failure after
intent is partial and retains its journal and any staging directory for
inspection; it never automatically retries, overwrites or removes a reused slot.

Native maintenance can remove an empty `worktrees` directory after the final
registration is pruned. Restore refuses if that original parent disappeared or
was replaced; it does not recreate it or weaken the directory-identity check.
The retained payload remains available for separately reviewed manual recovery.

The original checkout stays missing and locked. Restore does not recreate user
files, their ownership, inodes or timestamps, or execute hooks or repair.

If the owner finds the moved checkout, native `git worktree repair` is a
separately authorized owner action. Inspection prints escaped argv data with an
explicit found-path placeholder; it is guidance, not an executed transcript.
After repair, verify the native registration, original lock, HEAD/index and
user files. The source test fixture performs repair only in a temporary repo.

History and `restore --list` select the repository from optional PATH, or the
current directory when PATH is omitted. `--state-dir` alone does not select a
repository. They filter the shared store to that repository, newest first within
the disclosed scan bound. Totals count matching records examined within the
directory and byte bounds; a truncated total can be a lower bound.

These listings read prune journals only, not restore journals. They cannot
identify a completed or partial restore. Preserve the restore ID and journal
pointer from the restore result for separate inspection. They do not read raw
recovery payloads or verify the pin, so a recorded payload is `unknown`, not verified `available`. The default
limit is 256, the ceiling is 1024, and output is bounded to 4 MiB. Truncation is
explicit and succeeds; invalid records or operational failures refuse. Exit
codes are 0 for complete inspection/mutation/listing, 1 for refusal, partial or
operational failure, and 2 for invalid usage. Like contract-3 history, listing
skips entries whose names lack a `.json` suffix in the journal namespace.
An entry with that suffix must be a valid private regular journal file. Restore
inspection uses its stricter restore-journal reader and refuses stray entries
there. Old contracts are unchanged.

[Restore/history evidence and limits](../specs/011-workspace-reference-tidy/evidence/registration-prune-v4-restore-history-20261026.md)
separate source checks from pending native platform and acceptance evidence.
