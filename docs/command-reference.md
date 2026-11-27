# SourceSteward command reference

Generated from the Rust CLI by `stw dev command-reference`.

## `stw`

```text
SourceSteward: Plan Git changes, inspect branch history, and hand off work across repositories.

Usage: stw <COMMAND>

Commands:
  status       Where am I in every repository (read-only)
  tidy         Branch cleanup: `plan` shows it, `apply` carries it out, `restore` undoes it
  sync         Fast-forward every repository to its upstream (--plan or --dry-run only shows what it would do)
  add          Bind an existing GitHub repository as a project (git submodule) of the workspace; one local commit
  ready-check  Check the read-only preconditions for marking a pull request ready
  review       Review helpers (read-only)
  prove        Prove locally that a rewritten branch is the old one rebased and nothing else (read-only, no network)
  absorb       Plan where the staged changes would go: the unpushed commit that owns each hunk (read-only, writes nothing)
  restack      Predict a rebase (`plan`, read-only), or replay one branch (`prepare`, `apply`, `undo`)
  oplog        The op log: ref changes (timeline), rewrites of a commit (evolog), undo of one branch and the opt-in git hooks that write it
  workspace    Save and resume a local task checkpoint
  inspect      Inspect published PR changes or compare local revisions without checking them out
  watch        Watch GitHub activity and print transitions (read-only)
  lint         Check work products against the working rules (read-only)
  help         Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

  -V, --version
          Print version

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw status`

```text
Where am I in every repository (read-only)

Usage: stw status [OPTIONS] [PATH]...

Arguments:
  [PATH]...
          Only these repositories, in one line each with the stack of the current branch and the GitHub counts; with --json the same document plus `kind`, `depth` and `stack` per repository. A PATH that is a repository (a checkout or a linked worktree) shows it with its nested submodules; any other directory is scanned below for repositories. The key of a repository is its path relative to the current directory. Not with --long, --icons, --worktrees, --review, --stats, --verbose, --repo. Without a PATH: the repositories of the workspace

Options:
      --json
          Print one JSON object instead of text

      --offline
          Do not talk to GitHub (no pull request data, no fetch)

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Only this repository of the workspace (name as printed by stw); repeatable

      --rules-path <PATH>
          agent-rules checkout [default: <workspace>/.agents]

  -l, --long
          Detailed text: a card of facts per repository and the full explanation of every reason it needs action (the JSON is the same)

      --timeout <DURATION>
          Overall deadline of the GitHub reads, 1s to 10m, like 90s or 2m (not with --offline)

          [default: 90s]

      --stats
          Print diagnostics at the end: API requests, rate limit, elapsed time (not with --offline); with --json the document gets a `stats` member

      --verbose
          Print the diagnostics per repository and source, then the stats line (not with --offline); with --json they go to stderr

      --fetch
          Fetch origin of every repository first: remote-tracking refs only, no prune, no tags, no submodules; the one thing that is not read-only

      --worktrees
          List the linked worktrees of each repository (the main checkout is not listed)

      --review
          Also read review state from conversation comments and submitted reviews (two bounded lists per pull request, at most 200 per run; not with --offline)

      --icons <SET>
          Glyph set of the text: unicode, nerd or ascii [default: unicode on a terminal with a UTF-8 locale, else ascii]

          [possible values: unicode, nerd, ascii]

      --depth <N>
          Only when a PATH is a directory that is scanned: how many levels below it repositories and their submodules are listed, 0 to 64 [default: 16]; a cut is a notice

      --limit <N>
          Only when a PATH is a directory that is scanned: the most repositories of a run, 1 to 1024 [default: 256]; more is a usage error (exit 2)

  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw tidy`

```text
Branch cleanup: `plan` shows it, `apply` carries it out, `restore` undoes it

Usage: stw tidy <COMMAND>

Commands:
  plan     Which local branches and worktree registrations can be cleaned up (read-only, deletes nothing)
  apply    Carry out the plan: delete the local branches it marks `delete`, manifest first; undo with `restore`
  restore  Re-create branches deleted by `apply` from the manifest (never overwrites)
  history  List the stored manifests (the journals of `apply`), newest first, with the number of branches each recorded and how many `restore` could create now (read-only)
  help     Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw tidy plan`

```text
Which local branches and worktree registrations can be cleaned up (read-only, deletes nothing)

Usage: stw tidy plan [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --offline
          Do not talk to GitHub (no pull request data, no fetch)

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Only this repository of the workspace (name as printed by stw); repeatable

      --all
          Also list the branches that are kept

      --backup-days <N>
          Backup branches (r9-*, r10-*, pre-restack*, archive/*, backup/*) whose tip commit is at least N days old are `delete`

          [default: 7]

      --similarity <PCT>
          A branch whose commits without an equal patch on the base all have a patch at least this similar (percent, 1 to 100) is `review` with the proof `similar-patches` (never `delete`)

          [default: 95]

      --similarity-low <PCT>
          Lower threshold in percent (1 to 100, not above --similarity): between the two the class is `modified`, below `partial`

          [default: 70]

      --no-similarity
          Do not compare commits by similarity (the plan is as without that stage; saves the time of the comparison)

      --no-rebase-evidence
          Do not map the commits of a branch to the base by author, time and subject (no `rebase-identity`, no `landed via` from the commits; no pull request commit reads)

      --explain
          Under each branch of the text plan: the evidence that led to the decision, or the evidence that is missing (read-only; the decision and the JSON are the same, which already holds the data)

  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw tidy apply`

```text
Carry out the plan: delete the local branches it marks `delete`, manifest first; undo with `restore`

Usage: stw tidy apply [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --offline
          Do not talk to GitHub (no pull request data, no fetch)

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Only this repository of the workspace (name as printed by stw); repeatable

      --plan <FILE>
          Act on this `stw tidy plan --json` output (each entry is verified again first) [default: a fresh plan]

      --manifest-dir <DIR>
          Where the manifest goes [default: the user's data directory, stw/manifests]

      --backup-days <N>
          Backup branches whose tip commit is at least N days old are `delete`

          [default: 7]

  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw tidy restore`

```text
Re-create branches deleted by `apply` from the manifest (never overwrites)

Usage: stw tidy restore [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --run <ID>
          Run ID printed by `apply` [default: the newest run]

      --branch <NAME>
          Only this branch

      --manifest-dir <DIR>
          Where the manifests are [default: the user's data directory, stw/manifests]

  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw tidy history`

```text
List the stored manifests (the journals of `apply`), newest first, with the number of branches each recorded and how many `restore` could create now (read-only)

Usage: stw tidy history [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --limit <N>
          Most journals listed, 1 to 1024

          [default: 256]

      --manifest-dir <DIR>
          Where the manifests are [default: the user's data directory, stw/manifests]

  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw sync`

```text
Fast-forward every repository to its upstream (--plan or --dry-run only shows what it would do)

Usage: stw sync [OPTIONS] [COMMAND]

Commands:
  restore  Move the branches of a sync run back to the tips they had before it: only where a branch still stands on the tip the run left (else `changed-since`, nothing touched). Pruned refs, stash reapplies and submodule moves stay
  help     Print this message or the help of the given subcommand(s)

Options:
      --json
          Print one JSON object instead of text

      --offline
          Do not talk to GitHub (no pull request data, no fetch)

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Only this repository of the workspace (name as printed by stw); repeatable

      --plan
          Fetch the remote-tracking refs of origin (not with --offline) and print what a run would do; change nothing else (alias: --dry-run)

      --dry-run
          Alias of --plan

      --prune
          Fetch with --prune: delete remote-tracking refs of origin (refs/remotes/origin/* only) that are gone there. Overrides fetch.prune. With --plan it prunes as a run does

      --no-prune
          Fetch without pruning (overrides fetch.prune for a fetch by the installed git)

      --rebase
          Rebase a diverged branch (ahead and behind its upstream) onto the fetched upstream, with the installed git: a conflict stops the rebase as git leaves it. Without --rebase and --merge a diverged branch is only reported. The Git configuration (pull.rebase, branch.<name>.rebase, rebase.autoStash, ...) is not followed

      --merge
          Merge the fetched upstream into a diverged branch (the only way to get a merge commit; a branch that can be fast-forwarded is still fast-forwarded), with the installed git

      --autostash
          Stash local changes of tracked files around a --rebase or --merge and reapply them (git's autostash); without it a rebase or merge is refused while tracked files have changes

      --no-autostash
          Never stash (the default): --rebase and --merge refuse a repository with local changes of tracked files

      --submodules
          Also move each pinned (detached) submodule to the commit the superproject records when that is a fast-forward: git submodule sync and update --no-fetch --checkout; never a clone, init or extra fetch. An uninitialized submodule or a missing commit is reported as unreachable. --plan says what would move

      --rules
          Also handle the agent-rules checkout (the submodule .agents of a whr workspace): fetch it and fast-forward HEAD to origin's default branch; with --plan it fetches origin and only reports. Refuses only to protect local changes or local commits. Never runs install.sh

      --rules-path <PATH>
          agent-rules checkout [default: <workspace>/.agents]

      --owner-identity <NAME <EMAIL>>
          With --rules: owner identity `Name <email>` that the author and the committer of every new commit must be (repeatable) [default: user.name and user.email of the Git configuration of the workspace]. A commit by anyone else refuses the update

      --allowed-committer <NAME <EMAIL>>
          With --rules: a further identity `Name <email>` allowed as committer (not as author), such as the GitHub rebase-merge committer `<login>@users.noreply.github.com` (repeatable)

  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw sync restore`

```text
Move the branches of a sync run back to the tips they had before it: only where a branch still stands on the tip the run left (else `changed-since`, nothing touched). Pruned refs, stash reapplies and submodule moves stay

Usage: stw sync restore [OPTIONS] [PATH]

Arguments:
  [PATH]
          Workspace root (same as --root)

Options:
      --run <ID>
          Run ID printed by `sync` [default: the highest run ID found in the workspace]

      --json
          Print one JSON object instead of text

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw add`

```text
Bind an existing GitHub repository as a project (git submodule) of the workspace; one local commit

Usage: stw add [OPTIONS] <SPEC> [PATH]

Arguments:
  <SPEC>
          OWNER/REPO of an existing repository, or REPO (the owner of the workspace's GitHub origin is used)

  [PATH]
          Where the project goes, relative to the workspace [default: projects/REPO]

Options:
      --json
          Print one JSON object instead of text

      --dry-run
          Run every check and change nothing

      --root <PATH>
          Workspace root, the top level of its git repository [default: repository or superproject of the current directory, else $STW_ROOT]

  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw ready-check`

```text
Check the read-only preconditions for marking a pull request ready

Usage: stw ready-check [OPTIONS] <NUMBER>

Arguments:
  <NUMBER>
          Pull request number

Options:
      --repo <REPO>
          Repository OWNER/NAME (default: the single GitHub repository in the workspace)

      --human
          Print the human list (the default)

      --json
          Print one JSON object

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw review`

```text
Review helpers (read-only)

Usage: stw review <COMMAND>

Commands:
  carry-over  May the review of a pull request head be carried over to a rewritten head? Prints the `Carried-over:` line for a yes; posts nothing
  help        Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw review carry-over`

```text
May the review of a pull request head be carried over to a rewritten head? Prints the `Carried-over:` line for a yes; posts nothing

Usage: stw review carry-over [OPTIONS] [ID]...

Arguments:
  [ID]...
          OLD_BASE OLD_HEAD NEW_BASE NEW_HEAD

Options:
      --json
          Print one JSON object instead of text

      --pr <NUMBER|none>
          Pull request number written into the line, or `none` for a local review

      --fetch
          Fetch the head of the pull request from origin and require it to equal NEW_HEAD (needs a number)

      --checkout <PATH>
          Repository or worktree that holds the objects [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

All three conditions must hold: (1) the patch-id of `BASE...HEAD` is equal for the old and the new head, each
against its own base; (2) there is no merge commit in NEW_BASE..NEW_HEAD; (3) `git range-diff OLD_BASE OLD_HEAD
NEW_BASE NEW_HEAD` shows `=` for every commit, at the same position. The four arguments are full 40-hex commit ids.
A step that could not run is `unknown` and the answer is no. Only for a yes the output ends with the line
`Carried-over: from=<40-hex> to=<40-hex> patch-id=<40-hex> pr=<n|none>`. A carry-over also needs `from` to be
CLEAR; stw does not read reviews here. --fetch fetches refs/pull/N/head of origin into refs/stw/carry-over/pr-N
(objects and that private ref only) and requires it to equal NEW_HEAD.

Exit codes:
  0  carry-over allowed
  1  not allowed, or a step could not run (the result is still printed)
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw prove`

```text
Prove locally that a rewritten branch is the old one rebased and nothing else (read-only, no network)

Usage: stw prove [OPTIONS] [REV]...

Arguments:
  [REV]...
          ALT NEU

Options:
      --json
          Print one JSON object instead of text

      --base <BASIS>
          The base NEU was rebased onto (an ancestor of NEU) [default: the merge base of ALT and NEU, a rewrite on the same base]

      --old-parent <REV>
          The old parent of ALT, an ancestor of it: the old base when ALT was a child branch whose parent was rebased (`git rebase --onto NEWPARENT OLDPARENT ALT`) [default: the merge base of ALT and NEU]

      --pr <NUMBER|none>
          Pull request number written into the line, or `none` [default: none]

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart). ALT is the old branch or head, NEU the new one (one revision each, not a range).
Eight checks, all must hold: (1) `git range-diff` shows `=` for every commit at the same position; (2) the patch-id of
every commit is equal, old against new; (3) the patch-id of `BASE...HEAD` is equal; (4) no merge commit in the new range;
(5) the same set of changed paths; (6) author and committer of every new commit are user.name <user.email> of the Git
configuration (read only); (7) every Co-Authored-By line of an old commit is in the new commit at the same position;
(8) no session link in the new commit texts. The old base is the
merge base of ALT and NEU, or, with --old-parent REV, REV (the old parent of ALT, which must be an ancestor of it: a child
branch whose parent was rebased). The new base is the same commit (a rewrite on the same base) or, with --base BASIS, BASIS
(a rebase onto it; BASIS must be an ancestor of NEU). A step that could not run is `unknown` and the verdict is `not
proved`. Only for `proved` the last line is `Carried-over: from=<ALT> to=<NEU> patch-id=<hex> pr=<n|none>`. Nothing is
posted and nothing is written; `git` runs for `range-diff` only, everything else is read in process.
The repository is the one of the current directory (it must belong to the workspace), or --repo NAME.

Exit codes:
  0  proved
  1  not proved, or a step could not run (the result is still printed)
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw absorb`

```text
Plan where the staged changes would go: the unpushed commit that owns each hunk (read-only, writes nothing)

Usage: stw absorb [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --human
          Print the human list (the default)

      --plan
          The plan is the default; accepted so that `--apply --plan-sum SUM` reads the same way

      --apply
          Fold the staged changes of the plan. Requires `--plan-sum`. Writes a backup ref first

      --plan-sum <SUM>
          SHA-256 `plan_sum` of the plan this fold must match

      --base <REV>
          The stack is REV..HEAD (REV an ancestor of HEAD); the stack limit does not apply [default: the commits no remote-tracking branch contains]

      --max-stack <N>
          At most N commits in the stack (without --base)

          [default: 10]

      --ambiguous-newest
          A hunk whose lines come from several commits goes to the newest of them instead of being skipped as ambiguous

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); specification and limits: README.md, section `stw absorb plan`. Without --apply,
computes the PLAN of folding the staged changes into the unpushed commits that own the changed lines (the method of
git-absorb: the newest commit of the stack whose changes do not commute with the hunk) and writes nothing. --apply
folds that plan, and only when --plan-sum is the plan's checksum: an op log backup ref is written first, then the
fixup commits, then `git rebase --autosquash`. The way back is `stw oplog undo` (named in the output). Git only: a jj
or GitButler checkout is refused. The stack: the first-parent commits of HEAD that no remote-tracking branch (and,
without --base, no other local branch) contains, by the author of `user.name` and `user.email`, up to a merge commit.
A hunk that cannot be placed is skipped with its reason (no-candidate, ambiguous, conflict, outside-range, published,
foreign). The plan's only git program is `git blame` for the removed lines (read-only, GIT_NO_LAZY_FETCH=1).

Exit codes:
  0  a plan was produced (also when every hunk is skipped), or --apply folded the plan
  1  refused with a typed error code (nothing-staged, plan-sum-mismatch, nothing-to-apply, not-owned, published,
     unmerged-index, environment-override, partial-clone-unsupported, blame-failed, unsupported-backend, ...);
     with --json the error is the document
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw restack`

```text
Predict a rebase (`plan`, read-only), or replay one branch (`prepare`, `apply`, `undo`)

Usage: stw restack <COMMAND>

Commands:
  plan             Which level of the stack rebases cleanly onto the new base, where a conflict arises (files) and which level only inherits it
  prepare          Replay one local branch onto another and write a plan file. The branch is not moved
  apply            Move the branch recorded in a plan file, after a backup ref
  undo             Move the branch back, using the apply receipt and its backup ref
  conflict-report  Persist a conflicting plan and its available blob versions as versioned JSON
  show-conflict    Reopen a stored conflict report and check its input revisions
  help             Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw restack plan`

```text
Which level of the stack rebases cleanly onto the new base, where a conflict arises (files) and which level only inherits it

Usage: stw restack plan [OPTIONS] [BRANCH]...

Arguments:
  [BRANCH]...
          Local branches, bottom to top [default: derived from the local branches]

Options:
      --json
          Print one JSON object instead of text

      --onto <BASIS>
          The new base: a revision [default: the default branch of the repository (origin/HEAD)]

      --old-parent <REV>
          Old parent of the bottom branch, an ancestor of it (one branch only) [default: the merge base of the bottom branch and the base]

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

Port of the single-branch prediction of the Go `restack plan` (BRANCH --old-parent REV --onto BASIS) plus a whole-stack
preview that is new in stw. BRANCH... are local branches, bottom to top; each sits on the one before. Without any branch
the chain is derived from the local branches: it starts at the checked-out branch and goes down to the base, each branch
the nearest one below the previous; anything ambiguous (two candidates, two branches at one commit, a detached head) is
refused and the explicit spelling is suggested. --onto is the new base [default: the default branch of the repository
(origin/HEAD), else a usage error]. Each level is replayed in memory onto the new tip of the level below (gix merge); a
level above a conflict only inherits it and is not computed. The first line is the verdict: `plan: clean` or `plan:
conflicts at A (files ...)` (also `refused`, `unknown`). The replay approximates `git rebase`: directory renames, rename
detection, binary files, submodules and merge drivers are covered in README.md; a case the replay cannot judge is
`unknown` with the reason, never `clean`; rerere is not considered. Nothing is written (no ref, file or object), no
network, no `git` process.

Exit codes:
  0  plan: clean
  1  conflicts, refused or unknown (the plan is printed)
  2  usage error (also: no --onto and no default branch, a branch that does not exist)

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw restack prepare`

```text
Replay one local branch onto another and write a plan file. The branch is not moved

Usage: stw restack prepare [OPTIONS] <BRANCH> <ONTO> <FILE>

Arguments:
  <BRANCH>
          Local branch to replay. It must not be checked out

  <ONTO>
          Local branch to replay onto

  <FILE>
          Plan file to create. It is never overwritten

Options:
      --json
          Print one JSON object instead of text

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

Replay BRANCH onto ONTO and write FILE. FILE is created only when it does not exist and contains the plan id.
The branch must be a direct local branch and must not be checked out. ONTO is a direct local branch. A merge
commit or a conflict refuses the command and writes nothing. The replayed commits keep their author; the
committer is user.name and user.email of the repository. author.* and committer.* overrides are refused.
Nothing is pushed. Apply runs no hooks, and neither does this command: the replay is in process and does not
call git commit, git rebase or git update-ref. A global Git config (diff, rebase, hooks, gpg, rerere,
whitespace) is not read. The objects are kept by refs/stw/restack-prepared/<id>.
Demonstration with the values of one run: docs/restack-apply.md. Apply runs no hooks.

Exit codes:
  0  the plan file was written
  1  refused (the reason is printed; a plan file that could not be created leaves the prepared ref)
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw restack apply`

```text
Move the branch recorded in a plan file, after a backup ref

Usage: stw restack apply [OPTIONS] --yes <FILE>

Arguments:
  <FILE>
          Plan file written by prepare

Options:
      --json
          Print one JSON object instead of text

      --yes
          Confirm the branch update

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

Move the branch in FILE to the replayed tip. --yes is required. Both tips are checked again; a branch or onto
that moved since the plan is left at the newer value and nothing is written. The old tip is stored at
refs/stw/backup/<run>/1 in the same counter and retention as oplog undo, then one ref transaction checks the
onto tip and the prepared record and moves the branch. Apply runs no hooks. A failure after the backup says
that the counter, the backup refs and retention may remain, and that the transaction outcome is unknown.

Exit codes:
  0  the branch was moved
  1  refused, or the outcome of the transaction is unknown
  2  usage error (also: --yes missing)

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw restack undo`

```text
Move the branch back, using the apply receipt and its backup ref

Usage: stw restack undo [OPTIONS] --yes <FILE>

Arguments:
  <FILE>
          Plan file whose apply receipt is undone

Options:
      --json
          Print one JSON object instead of text

      --yes
          Confirm the branch update

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

Move the branch in FILE back to the tip recorded at apply. --yes is required. The branch must still be at the
applied tip and the backup ref from that apply must still hold the old tip. Undo writes its own backup first,
including the old tip, and then moves the branch. A branch that moved since apply is left as it is.

Exit codes:
  0  the branch was moved back
  1  refused, or the outcome of the transaction is unknown
  2  usage error (also: --yes missing)

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw restack conflict-report`

```text
Persist a conflicting plan and its available blob versions as versioned JSON

Usage: stw restack conflict-report [OPTIONS] --store <STORE> --onto <ONTO> <BRANCHES>...

Arguments:
  <BRANCHES>...
          Ordered stack of local branches

Options:
      --repo <REPO>
          Repository or worktree path

          [default: .]

      --store <STORE>
          Existing report directory outside the repository

      --onto <ONTO>
          Target revision

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw restack show-conflict`

```text
Reopen a stored conflict report and check its input revisions

Usage: stw restack show-conflict [OPTIONS] --store <STORE> <ID>

Arguments:
  <ID>
          Report ID returned by conflict-report

Options:
      --repo <REPO>
          Repository or worktree path

          [default: .]

      --store <STORE>
          Existing report directory outside the repository

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog`

```text
The op log: ref changes (timeline), rewrites of a commit (evolog), undo of one branch and the opt-in git hooks that write it

Usage: stw oplog <COMMAND>

Commands:
  timeline   Ref changes from the reflogs and the hook log `refs.log`, newest last, with what the logs do not cover
  evolog     The recorded rewrites (amend, rebase) of a commit, from the hook log `rewrites.log`
  undo-plan  What undoing one local branch to an older reflog value would check; changes nothing
  undo       Move one local branch back to an older reflog value: backup ref, compare-and-swap update; writes
  hooks      Install, check or remove the two git hooks that write the op log (post-rewrite, reference-transaction); opt-in per repository
  help       Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); format and rules: docs/oplog-format.md. timeline, evolog and undo-plan are read-only:
nothing is written. `undo` and `hooks install|uninstall` write (see below; `hooks` has its own help). Nothing here installs a hook except `hooks install --yes`. No git program runs, except `git --version` of `undo-plan --keep` and `undo --keep` and the `git reset --keep` of `undo --keep`. The logs are `<git-common-dir>/stw/oplog/refs.log`, `rewrites.log` (written by
the hooks of the spikes `stw-oplog-refhook` and `stw-oplog-hook`) and the reflogs of the repository. A log that is a
symbolic link or not a regular file is not read. Reflogs are read for the files ref backend only.
timeline  A reflog row and the log line of the same change are one entry (flag `covered`); a log line without a reflog
          row (a deleted branch, a tag) is its own entry (`hook-only`). Groups are suspected only. The gaps list says what
          the logs do not show (never a proof of completeness). --ref GLOB filters by ref name (`*` matches any run);
          refs/remotes only with --remotes, other refs than refs/heads, refs/stash, refs/stw only with --all.
evolog    REV is a branch, a revision or a 40-digit commit id; the chain follows the recorded pairs back (--limit N commits).
undo-plan REF is a branch name, a full ref name or HEAD. --to OLD (lower-case hex prefix of an old reflog value) or
          --steps N (default 1) picks the target; --keep plans the undo of the checked-out branch (see undo). Exit 1 when a
          check is refused. It prints the `undo` command to apply.
undo      REF --to OLD --expect NEW [--keep] --yes: the same checks as undo-plan, then a backup ref refs/stw/backup/<run>/1
          on the current value, then the branch is set to OLD only if it still holds NEW (compare-and-swap). Local branches
          only; a branch that is checked out in a worktree is refused unless --keep is given. The newest 10 runs of backup
          refs are kept. Reflog entries (message `stw oplog undo`; with --keep git writes its own) and refs.log lines are written. Without --keep only the
          ref moves: index, working tree, other worktrees and pushes are not covered. The result names the backup ref and
          the way back.
          --keep: the branch must be the one this worktree has checked out (not another worktree's, not a detached HEAD);
          the index and the work tree must be clean (untracked files do not count, but one that the target would overwrite
          refuses, ignored ones too), no merge or other operation may run, no intent-to-add, assume-unchanged or
          skip-worktree entry may exist and no gitlink may differ between the two values. stw then runs `git reset --keep OLD` (the installed git, no hook,
          no --hard); branch, index and work tree move together. A refusal writes nothing.

Exit codes:
  0  done (undo-plan: no check refused)
  1  undo-plan or undo: a check is refused; the update failed; the repository could not be read
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog timeline`

```text
Ref changes from the reflogs and the hook log `refs.log`, newest last, with what the logs do not cover

Usage: stw oplog timeline [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --ref <GLOB>
          Only refs whose name matches GLOB (`*` matches any run of characters)

      --remotes
          Also refs/remotes

      --all
          Also refs/remotes and every other ref with a reflog

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); format and rules: docs/oplog-format.md. timeline, evolog and undo-plan are read-only:
nothing is written. `undo` and `hooks install|uninstall` write (see below; `hooks` has its own help). Nothing here installs a hook except `hooks install --yes`. No git program runs, except `git --version` of `undo-plan --keep` and `undo --keep` and the `git reset --keep` of `undo --keep`. The logs are `<git-common-dir>/stw/oplog/refs.log`, `rewrites.log` (written by
the hooks of the spikes `stw-oplog-refhook` and `stw-oplog-hook`) and the reflogs of the repository. A log that is a
symbolic link or not a regular file is not read. Reflogs are read for the files ref backend only.
timeline  A reflog row and the log line of the same change are one entry (flag `covered`); a log line without a reflog
          row (a deleted branch, a tag) is its own entry (`hook-only`). Groups are suspected only. The gaps list says what
          the logs do not show (never a proof of completeness). --ref GLOB filters by ref name (`*` matches any run);
          refs/remotes only with --remotes, other refs than refs/heads, refs/stash, refs/stw only with --all.
evolog    REV is a branch, a revision or a 40-digit commit id; the chain follows the recorded pairs back (--limit N commits).
undo-plan REF is a branch name, a full ref name or HEAD. --to OLD (lower-case hex prefix of an old reflog value) or
          --steps N (default 1) picks the target; --keep plans the undo of the checked-out branch (see undo). Exit 1 when a
          check is refused. It prints the `undo` command to apply.
undo      REF --to OLD --expect NEW [--keep] --yes: the same checks as undo-plan, then a backup ref refs/stw/backup/<run>/1
          on the current value, then the branch is set to OLD only if it still holds NEW (compare-and-swap). Local branches
          only; a branch that is checked out in a worktree is refused unless --keep is given. The newest 10 runs of backup
          refs are kept. Reflog entries (message `stw oplog undo`; with --keep git writes its own) and refs.log lines are written. Without --keep only the
          ref moves: index, working tree, other worktrees and pushes are not covered. The result names the backup ref and
          the way back.
          --keep: the branch must be the one this worktree has checked out (not another worktree's, not a detached HEAD);
          the index and the work tree must be clean (untracked files do not count, but one that the target would overwrite
          refuses, ignored ones too), no merge or other operation may run, no intent-to-add, assume-unchanged or
          skip-worktree entry may exist and no gitlink may differ between the two values. stw then runs `git reset --keep OLD` (the installed git, no hook,
          no --hard); branch, index and work tree move together. A refusal writes nothing.

Exit codes:
  0  done (undo-plan: no check refused)
  1  undo-plan or undo: a check is refused; the update failed; the repository could not be read
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog evolog`

```text
The recorded rewrites (amend, rebase) of a commit, from the hook log `rewrites.log`

Usage: stw oplog evolog [OPTIONS] <REV>

Arguments:
  <REV>
          A branch, a revision or a commit id

Options:
      --json
          Print one JSON object instead of text

      --limit <N>
          Commits of the chain shown at most [default: 200]

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); format and rules: docs/oplog-format.md. timeline, evolog and undo-plan are read-only:
nothing is written. `undo` and `hooks install|uninstall` write (see below; `hooks` has its own help). Nothing here installs a hook except `hooks install --yes`. No git program runs, except `git --version` of `undo-plan --keep` and `undo --keep` and the `git reset --keep` of `undo --keep`. The logs are `<git-common-dir>/stw/oplog/refs.log`, `rewrites.log` (written by
the hooks of the spikes `stw-oplog-refhook` and `stw-oplog-hook`) and the reflogs of the repository. A log that is a
symbolic link or not a regular file is not read. Reflogs are read for the files ref backend only.
timeline  A reflog row and the log line of the same change are one entry (flag `covered`); a log line without a reflog
          row (a deleted branch, a tag) is its own entry (`hook-only`). Groups are suspected only. The gaps list says what
          the logs do not show (never a proof of completeness). --ref GLOB filters by ref name (`*` matches any run);
          refs/remotes only with --remotes, other refs than refs/heads, refs/stash, refs/stw only with --all.
evolog    REV is a branch, a revision or a 40-digit commit id; the chain follows the recorded pairs back (--limit N commits).
undo-plan REF is a branch name, a full ref name or HEAD. --to OLD (lower-case hex prefix of an old reflog value) or
          --steps N (default 1) picks the target; --keep plans the undo of the checked-out branch (see undo). Exit 1 when a
          check is refused. It prints the `undo` command to apply.
undo      REF --to OLD --expect NEW [--keep] --yes: the same checks as undo-plan, then a backup ref refs/stw/backup/<run>/1
          on the current value, then the branch is set to OLD only if it still holds NEW (compare-and-swap). Local branches
          only; a branch that is checked out in a worktree is refused unless --keep is given. The newest 10 runs of backup
          refs are kept. Reflog entries (message `stw oplog undo`; with --keep git writes its own) and refs.log lines are written. Without --keep only the
          ref moves: index, working tree, other worktrees and pushes are not covered. The result names the backup ref and
          the way back.
          --keep: the branch must be the one this worktree has checked out (not another worktree's, not a detached HEAD);
          the index and the work tree must be clean (untracked files do not count, but one that the target would overwrite
          refuses, ignored ones too), no merge or other operation may run, no intent-to-add, assume-unchanged or
          skip-worktree entry may exist and no gitlink may differ between the two values. stw then runs `git reset --keep OLD` (the installed git, no hook,
          no --hard); branch, index and work tree move together. A refusal writes nothing.

Exit codes:
  0  done (undo-plan: no check refused)
  1  undo-plan or undo: a check is refused; the update failed; the repository could not be read
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog undo-plan`

```text
What undoing one local branch to an older reflog value would check; changes nothing

Usage: stw oplog undo-plan [OPTIONS] <REF>

Arguments:
  <REF>
          A branch name, a full ref name or HEAD

Options:
      --json
          Print one JSON object instead of text

      --keep
          Plan the undo of the branch this worktree has checked out (`git reset --keep`, clean index and work tree only)

      --to <OLD>
          The old value to go back to: a lower-case hex prefix of an old value of the reflog

      --steps <N>
          Go back this many reflog entries [default: 1]

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); format and rules: docs/oplog-format.md. timeline, evolog and undo-plan are read-only:
nothing is written. `undo` and `hooks install|uninstall` write (see below; `hooks` has its own help). Nothing here installs a hook except `hooks install --yes`. No git program runs, except `git --version` of `undo-plan --keep` and `undo --keep` and the `git reset --keep` of `undo --keep`. The logs are `<git-common-dir>/stw/oplog/refs.log`, `rewrites.log` (written by
the hooks of the spikes `stw-oplog-refhook` and `stw-oplog-hook`) and the reflogs of the repository. A log that is a
symbolic link or not a regular file is not read. Reflogs are read for the files ref backend only.
timeline  A reflog row and the log line of the same change are one entry (flag `covered`); a log line without a reflog
          row (a deleted branch, a tag) is its own entry (`hook-only`). Groups are suspected only. The gaps list says what
          the logs do not show (never a proof of completeness). --ref GLOB filters by ref name (`*` matches any run);
          refs/remotes only with --remotes, other refs than refs/heads, refs/stash, refs/stw only with --all.
evolog    REV is a branch, a revision or a 40-digit commit id; the chain follows the recorded pairs back (--limit N commits).
undo-plan REF is a branch name, a full ref name or HEAD. --to OLD (lower-case hex prefix of an old reflog value) or
          --steps N (default 1) picks the target; --keep plans the undo of the checked-out branch (see undo). Exit 1 when a
          check is refused. It prints the `undo` command to apply.
undo      REF --to OLD --expect NEW [--keep] --yes: the same checks as undo-plan, then a backup ref refs/stw/backup/<run>/1
          on the current value, then the branch is set to OLD only if it still holds NEW (compare-and-swap). Local branches
          only; a branch that is checked out in a worktree is refused unless --keep is given. The newest 10 runs of backup
          refs are kept. Reflog entries (message `stw oplog undo`; with --keep git writes its own) and refs.log lines are written. Without --keep only the
          ref moves: index, working tree, other worktrees and pushes are not covered. The result names the backup ref and
          the way back.
          --keep: the branch must be the one this worktree has checked out (not another worktree's, not a detached HEAD);
          the index and the work tree must be clean (untracked files do not count, but one that the target would overwrite
          refuses, ignored ones too), no merge or other operation may run, no intent-to-add, assume-unchanged or
          skip-worktree entry may exist and no gitlink may differ between the two values. stw then runs `git reset --keep OLD` (the installed git, no hook,
          no --hard); branch, index and work tree move together. A refusal writes nothing.

Exit codes:
  0  done (undo-plan: no check refused)
  1  undo-plan or undo: a check is refused; the update failed; the repository could not be read
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog undo`

```text
Move one local branch back to an older reflog value: backup ref, compare-and-swap update; writes

Usage: stw oplog undo [OPTIONS] --to <OLD> --expect <NEW> <REF>

Arguments:
  <REF>
          A branch name or a full ref name

Options:
      --json
          Print one JSON object instead of text

      --to <OLD>
          The old value to go back to: a lower-case hex prefix of an old value of the reflog

      --expect <NEW>
          The full current value of the ref; the update happens only if the ref still holds it

      --keep
          Undo the branch this worktree has checked out: stw runs `git reset --keep OLD` (clean index and clean work tree only, untracked files stay); without it a checked-out branch is refused

      --yes
          Do it (without it nothing is written; `undo-plan` shows the checks)

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); format and rules: docs/oplog-format.md. timeline, evolog and undo-plan are read-only:
nothing is written. `undo` and `hooks install|uninstall` write (see below; `hooks` has its own help). Nothing here installs a hook except `hooks install --yes`. No git program runs, except `git --version` of `undo-plan --keep` and `undo --keep` and the `git reset --keep` of `undo --keep`. The logs are `<git-common-dir>/stw/oplog/refs.log`, `rewrites.log` (written by
the hooks of the spikes `stw-oplog-refhook` and `stw-oplog-hook`) and the reflogs of the repository. A log that is a
symbolic link or not a regular file is not read. Reflogs are read for the files ref backend only.
timeline  A reflog row and the log line of the same change are one entry (flag `covered`); a log line without a reflog
          row (a deleted branch, a tag) is its own entry (`hook-only`). Groups are suspected only. The gaps list says what
          the logs do not show (never a proof of completeness). --ref GLOB filters by ref name (`*` matches any run);
          refs/remotes only with --remotes, other refs than refs/heads, refs/stash, refs/stw only with --all.
evolog    REV is a branch, a revision or a 40-digit commit id; the chain follows the recorded pairs back (--limit N commits).
undo-plan REF is a branch name, a full ref name or HEAD. --to OLD (lower-case hex prefix of an old reflog value) or
          --steps N (default 1) picks the target; --keep plans the undo of the checked-out branch (see undo). Exit 1 when a
          check is refused. It prints the `undo` command to apply.
undo      REF --to OLD --expect NEW [--keep] --yes: the same checks as undo-plan, then a backup ref refs/stw/backup/<run>/1
          on the current value, then the branch is set to OLD only if it still holds NEW (compare-and-swap). Local branches
          only; a branch that is checked out in a worktree is refused unless --keep is given. The newest 10 runs of backup
          refs are kept. Reflog entries (message `stw oplog undo`; with --keep git writes its own) and refs.log lines are written. Without --keep only the
          ref moves: index, working tree, other worktrees and pushes are not covered. The result names the backup ref and
          the way back.
          --keep: the branch must be the one this worktree has checked out (not another worktree's, not a detached HEAD);
          the index and the work tree must be clean (untracked files do not count, but one that the target would overwrite
          refuses, ignored ones too), no merge or other operation may run, no intent-to-add, assume-unchanged or
          skip-worktree entry may exist and no gitlink may differ between the two values. stw then runs `git reset --keep OLD` (the installed git, no hook,
          no --hard); branch, index and work tree move together. A refusal writes nothing.

Exit codes:
  0  done (undo-plan: no check refused)
  1  undo-plan or undo: a check is refused; the update failed; the repository could not be read
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog hooks`

```text
Install, check or remove the two git hooks that write the op log (post-rewrite, reference-transaction); opt-in per repository

Usage: stw oplog hooks <COMMAND>

Commands:
  status     Where the hooks are, whether they are ours, `core.hooksPath`, worktrees, submodules, logs (read-only)
  install    Write both hooks into this repository's hooks directory; without --yes only the plan
  uninstall  Remove our two hook files (only files that carry our header); the logs stay; without --yes only the plan
  print      Print the text of one hook (post-rewrite or reference-transaction); writes nothing
  help       Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); rules: docs/oplog-format.md, 11. The hooks `post-rewrite` (-> rewrites.log) and
`reference-transaction` (-> refs.log) are the scripts of the spikes; they never make git fail. Opt-in: only this command
installs them, `stw add`, `tidy` and `status` never do. No git program runs, no git config is written.
install    Both hooks into <git-common-dir>/hooks (shared by all linked worktrees), mode 755, plus one `install` line
           in events.log / events-refhook.log. Refused, with nothing written, when core.hooksPath is set (any source,
           also empty), when the hooks directory is a symbolic link, or when a hook of that name exists that is not ours
           (never overwritten or chained; the output gives a chain proposal). Without --yes: the plan, exit 1.
uninstall  Removes a hook file only if it is a regular file whose line 2 is our header; other files are not touched.
           Without --yes: the plan, exit 1. Logs are not deleted.
status     Read-only. A submodule has its own hooks directory: use `--repo NAME`.

Exit codes:
  0  done, already installed, nothing to remove, or status
  1  refused, or the plan without --yes, or a write failed
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog hooks status`

```text
Where the hooks are, whether they are ours, `core.hooksPath`, worktrees, submodules, logs (read-only)

Usage: stw oplog hooks status [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); rules: docs/oplog-format.md, 11. The hooks `post-rewrite` (-> rewrites.log) and
`reference-transaction` (-> refs.log) are the scripts of the spikes; they never make git fail. Opt-in: only this command
installs them, `stw add`, `tidy` and `status` never do. No git program runs, no git config is written.
install    Both hooks into <git-common-dir>/hooks (shared by all linked worktrees), mode 755, plus one `install` line
           in events.log / events-refhook.log. Refused, with nothing written, when core.hooksPath is set (any source,
           also empty), when the hooks directory is a symbolic link, or when a hook of that name exists that is not ours
           (never overwritten or chained; the output gives a chain proposal). Without --yes: the plan, exit 1.
uninstall  Removes a hook file only if it is a regular file whose line 2 is our header; other files are not touched.
           Without --yes: the plan, exit 1. Logs are not deleted.
status     Read-only. A submodule has its own hooks directory: use `--repo NAME`.

Exit codes:
  0  done, already installed, nothing to remove, or status
  1  refused, or the plan without --yes, or a write failed
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog hooks install`

```text
Write both hooks into this repository's hooks directory; without --yes only the plan

Usage: stw oplog hooks install [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

      --yes
          Do it (without it only the plan is printed and nothing is written)

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); rules: docs/oplog-format.md, 11. The hooks `post-rewrite` (-> rewrites.log) and
`reference-transaction` (-> refs.log) are the scripts of the spikes; they never make git fail. Opt-in: only this command
installs them, `stw add`, `tidy` and `status` never do. No git program runs, no git config is written.
install    Both hooks into <git-common-dir>/hooks (shared by all linked worktrees), mode 755, plus one `install` line
           in events.log / events-refhook.log. Refused, with nothing written, when core.hooksPath is set (any source,
           also empty), when the hooks directory is a symbolic link, or when a hook of that name exists that is not ours
           (never overwritten or chained; the output gives a chain proposal). Without --yes: the plan, exit 1.
uninstall  Removes a hook file only if it is a regular file whose line 2 is our header; other files are not touched.
           Without --yes: the plan, exit 1. Logs are not deleted.
status     Read-only. A submodule has its own hooks directory: use `--repo NAME`.

Exit codes:
  0  done, already installed, nothing to remove, or status
  1  refused, or the plan without --yes, or a write failed
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog hooks uninstall`

```text
Remove our two hook files (only files that carry our header); the logs stay; without --yes only the plan

Usage: stw oplog hooks uninstall [OPTIONS]

Options:
      --json
          Print one JSON object instead of text

      --root <PATH>
          Workspace root [default: repository or superproject of the current directory, else $STW_ROOT]

      --repo <NAME>
          Repository of the workspace (name as printed by stw) [default: the repository of the current directory]

      --yes
          Do it (without it only the plan is printed and nothing is written)

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); rules: docs/oplog-format.md, 11. The hooks `post-rewrite` (-> rewrites.log) and
`reference-transaction` (-> refs.log) are the scripts of the spikes; they never make git fail. Opt-in: only this command
installs them, `stw add`, `tidy` and `status` never do. No git program runs, no git config is written.
install    Both hooks into <git-common-dir>/hooks (shared by all linked worktrees), mode 755, plus one `install` line
           in events.log / events-refhook.log. Refused, with nothing written, when core.hooksPath is set (any source,
           also empty), when the hooks directory is a symbolic link, or when a hook of that name exists that is not ours
           (never overwritten or chained; the output gives a chain proposal). Without --yes: the plan, exit 1.
uninstall  Removes a hook file only if it is a regular file whose line 2 is our header; other files are not touched.
           Without --yes: the plan, exit 1. Logs are not deleted.
status     Read-only. A submodule has its own hooks directory: use `--repo NAME`.

Exit codes:
  0  done, already installed, nothing to remove, or status
  1  refused, or the plan without --yes, or a write failed
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw oplog hooks print`

```text
Print the text of one hook (post-rewrite or reference-transaction); writes nothing

Usage: stw oplog hooks print <NAME>

Arguments:
  <NAME>
          post-rewrite or reference-transaction

Options:
  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart); rules: docs/oplog-format.md, 11. The hooks `post-rewrite` (-> rewrites.log) and
`reference-transaction` (-> refs.log) are the scripts of the spikes; they never make git fail. Opt-in: only this command
installs them, `stw add`, `tidy` and `status` never do. No git program runs, no git config is written.
install    Both hooks into <git-common-dir>/hooks (shared by all linked worktrees), mode 755, plus one `install` line
           in events.log / events-refhook.log. Refused, with nothing written, when core.hooksPath is set (any source,
           also empty), when the hooks directory is a symbolic link, or when a hook of that name exists that is not ours
           (never overwritten or chained; the output gives a chain proposal). Without --yes: the plan, exit 1.
uninstall  Removes a hook file only if it is a regular file whose line 2 is our header; other files are not touched.
           Without --yes: the plan, exit 1. Logs are not deleted.
status     Read-only. A submodule has its own hooks directory: use `--repo NAME`.

Exit codes:
  0  done, already installed, nothing to remove, or status
  1  refused, or the plan without --yes, or a write failed
  2  usage error

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace`

```text
Save and resume a local task checkpoint

Usage: stw workspace <COMMAND>

Commands:
  checkpoint       Save the index and working tree under a local task ref
  inspect          Show the published checkpoint of a task
  retirement-plan  Preview whether a checkpoint covers the worktree before retirement
  worktree-plan    Plan a detached linked worktree at a local revision
  worktree-apply   Create a detached linked worktree from a saved plan
  commit-plan      Preview a commit made from selected index or worktree content
  commit-apply     Apply a saved selective commit plan after revalidating its inputs
  resume           Restore a checkpoint into a new linked worktree
  help             Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace checkpoint`

```text
Save the index and working tree under a local task ref

Usage: stw workspace checkpoint [OPTIONS] --task <TASK>

Options:
      --task <TASK>


      --include <INCLUDE>
          Include this exact non-ignored untracked file, relative to the worktree root

      --plan
          Report the selection without writing Git objects or refs.

          With --json, save the "plan" object to a file outside the source worktree. Apply it with --apply-plan FILE and the same task and --include paths.

      --apply-plan <FILE>
          Read the saved JSON plan object and verify it before capturing current contents

      --repo <REPO>
          Repository or worktree path (default: discover from the working directory)

      --json
          Emit one versioned JSON document

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace inspect`

```text
Show the published checkpoint of a task

Usage: stw workspace inspect [OPTIONS] --task <TASK>

Options:
      --task <TASK>


      --repo <REPO>
          Repository or worktree path (default: discover from the working directory)

      --json
          Emit one versioned JSON document

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace retirement-plan`

```text
Preview whether a checkpoint covers the worktree before retirement

Usage: stw workspace retirement-plan [OPTIONS] --task <TASK>

Options:
      --task <TASK>


      --repo <REPO>
          Repository or worktree path (default: discover from the working directory)

      --json
          Emit one versioned JSON document

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace worktree-plan`

```text
Plan a detached linked worktree at a local revision

Usage: stw workspace worktree-plan [OPTIONS] <REVISION> <DESTINATION>

Arguments:
  <REVISION>
          Local revision to resolve to a commit

  <DESTINATION>
          Path of the new linked worktree

Options:
      --repo <REPO>
          Repository or worktree path (default: discover from the working directory)

      --json
          Emit one versioned JSON document

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace worktree-apply`

```text
Create a detached linked worktree from a saved plan

Usage: stw workspace worktree-apply [OPTIONS] <PLAN>

Arguments:
  <PLAN>
          File containing the JSON plan object

Options:
      --repo <REPO>
          Repository or worktree path (default: discover from the working directory)

      --json
          Emit one versioned JSON document

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace commit-plan`

```text
Preview a commit made from selected index or worktree content

Usage: stw workspace commit-plan [OPTIONS] [PATHS]...

Arguments:
  [PATHS]...
          Existing regular files relative to the worktree root

Options:
      --source <SOURCE>
          Source for the selected paths. Hunk selection requires `worktree`

          [possible values: index, worktree]

      --list-hunks
          List work-tree hunks against HEAD. Optional PATHS limit the list

      --hunk <HUNK>
          Hunk id from `--list-hunks`, repeatable. Plans those hunks and refuses PATHS

      --repo <REPO>
          Repository or worktree path (default: discover from the working directory)

      --json
          Emit one versioned JSON document

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace commit-apply`

```text
Apply a saved selective commit plan after revalidating its inputs

Usage: stw workspace commit-apply [OPTIONS] --message <MESSAGE> <PLAN>

Arguments:
  <PLAN>
          Saved JSON output from commit-plan

Options:
      --message <MESSAGE>
          Message for the selective commit

      --plan-sum <PLAN_SUM>
          Expected plan_sum of a hunk plan. The stored sum is always checked

      --repo <REPO>
          Repository or worktree path (default: discover from the working directory)

      --json
          Emit one versioned JSON document

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw workspace resume`

```text
Restore a checkpoint into a new linked worktree

Usage: stw workspace resume [OPTIONS] <CHECKPOINT> <DESTINATION>

Arguments:
  <CHECKPOINT>
          Full checkpoint commit SHA

  <DESTINATION>
          Path of the new linked worktree

Options:
      --repo <REPO>
          Repository or worktree path (default: discover from the working directory)

      --json
          Emit one versioned JSON document

  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw inspect`

```text
Inspect published PR changes or compare local revisions without checking them out

Usage: stw inspect <COMMAND>

Commands:
  github  Compare a GitHub pull request using its pinned base and head
  git     Compare local commits using their merge base
  help    Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw inspect github`

```text
Compare a GitHub pull request using its pinned base and head

Usage: stw inspect github <REPO> <PR>

Arguments:
  <REPO>


  <PR>


Options:
  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw inspect git`

```text
Compare local commits using their merge base

Usage: stw inspect git <REPO> <BASE> <HEAD>

Arguments:
  <REPO>


  <BASE>


  <HEAD>


Options:
  -h, --help
          Print help (see a summary with '-h')

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw watch`

```text
Watch GitHub activity and print transitions (read-only)

Usage: stw watch [OPTIONS]

Options:
      --all
          Watch all event categories, including each repository’s default branch

      --branch <NAME>
          Watch a named branch in each repository. Repeat to select more branches

      --all-branches
          Watch every branch in each repository

      --prs
          Watch pull requests, their checks, mergeability and restack events

      --wiki
          Watch GitHub wiki Git head updates

      --reviews
          Watch submitted PR reviews and inline review comments

      --discussions
          Watch new and updated GitHub discussions

      --workflows
          Emit workflow-completed for failed Actions runs outside open PR head checks

      --issues
          Watch open issues and emit issue-changed for opens, updates and closures

  -v, --verbose...
          Print polling diagnostics to stderr. Repeat for repository and PR decisions

      --repo <REPO>
          OWNER/NAME or a GitHub HTTPS repository URL, repeatable. Defaults to .stw/watch.toml if present, otherwise the current repository’s GitHub origin

      --workspace
          Watch all GitHub repositories in the workspace, or the current standalone Git repository

      --config <PATH>
          Read this watch.toml instead of `.stw/watch.toml`

      --ignore-author <IGNORE_AUTHOR>
          Do not report pull requests by this author. Repeatable. Overrides watch.toml when set

      --ignore-label <IGNORE_LABEL>
          Do not report pull requests with this label. Repeatable. Overrides watch.toml when set

      --max-open-prs <MAX_OPEN_PRS>
          Most open pull requests read per repository, 1 to 2000. [default: 2000]

      --detail-every <DETAIL_EVERY>
          Cycles between refreshes of pull request detail. [default: 10]

      --once
          Poll once, record transitions, and exit. The open list is not changed on a failed read

      --stream
          Poll until SIGINT, SIGTERM, or --max-idle

      --until-event <UNTIL_EVENT>
          Poll until one of these event kinds is printed, or --timeout. Comma-separated or repeatable

      --interval <DURATION>
          Polling interval, at least 10s. Only with --stream. [default: 60s]

      --max-idle <DURATION>
          With --stream, exit 0 after this long without an event. 0 is off. [default: 0]

      --timeout <DURATION>
          With --until-event, exit 4 after this long without a matching event. [default: 590s]

      --human
          Print one human-readable line per event instead of JSON Lines

      --template <PATH>
          TOML file with event-specific [templates] overrides

      --template-string <KIND=TEXT>
          Override an event template inline. Repeatable as KIND=TEXT

      --status
          Read the heartbeat and the accepted state. No network and no lock

      --baseline
          Force the next successful read to be a silent baseline, and repair a corrupt state

      --state-dir <STATE_DIR>
          Absolute state directory. The default is under the XDG state home and is not a Go watch directory

  -h, --help
          Print help (see a summary with '-h')

Select --all or one or more categories when polling. --status reads saved state.
Event kinds emitted by the watcher:
  --branch NAME / --all-branches: branch-head-changed
  --prs: new-pr, head-changed, ready-state-changed, base-changed, merged, closed
  PR checks: checks-green, checks-red, checks-pending-timeout
  Mergeability: mergeable-changed, restack-needed
  Health: watch-error, watch-recovered
  --workflows: workflow-completed
  --issues: issue-changed
  --reviews: review-state-changed, review-comment-added
  --discussions: discussion-changed
  --wiki: wiki-changed

With .stw/watch.toml, start configured repositories with stw watch --stream --all.

See https://github.com/wstein/sourcesteward/blob/main/docs/review-and-watch.md

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw lint`

```text
Check work products against the working rules (read-only)

Usage: stw lint <COMMAND>

Commands:
  pr       Check the commits, text and comments of a pull request against the machine-checkable working rules (read-only)
  review   Check the text of a review comment from a file against the review line rules (read-only, no network)
  pr-text  Check the text of a pull request from a file: length, Blocker line, attribution line, session link (read-only, no network)
  help     Print this message or the help of the given subcommand(s)

Options:
  -h, --help
          Print help (see a summary with '-h')

Exit codes:
  0  everything asked for worked
  1  the command ran, but a repository could not be read, `sync` failed in one, `tidy apply`
     failed a step (a branch, worktree, registration or the exclude file), `tidy restore` left an
     entry not restored, or `sync restore` found no run or left an entry unrestored
  2  usage error or unusable workspace (nothing was done)

--json prints one JSON object with "schema_version" (see README.md).

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw lint pr`

```text
Check the commits, text and comments of a pull request against the machine-checkable working rules (read-only)

Usage: stw lint pr [OPTIONS] <PR> [PATH]

Arguments:
  <PR>
          Pull request number

  [PATH]
          Workspace whose single GitHub repository is meant [default: the current directory]

Options:
      --repo <OWNER/NAME>
          Pull request repository [default: the single GitHub repository of the workspace]

      --owner-identity <NAME <EMAIL>>
          Owner identity for the author and committer rules (repeatable) [default: user.name and user.email of the Git configuration]

      --human
          Print the human list (the default)

      --json
          Print one JSON object instead of text

  -h, --help
          Print help (see a summary with '-h')

Read-only (GET only). Reads one pull request, its commits (up to 300) and its conversation comments (up to 300) and
reports one finding per violated rule with the rule ID, the severity and the location (PR title or text, comment ID,
commit SHA, line). Rules: no session link (Claude session trailer or code-session URL) in commit messages, PR title, PR
text and comments; author and committer of every commit is an owner identity; Co-Authored-By trailer; Conventional
Commit subjects; PR text about 25 lines (warning above 25, error above 30), a Blocker line and an Author agent line; the
first line of a review comment follows `Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n>` (or the
Carried-over form) for this pull request; a review comment has at most 15 lines. `--json` lists the rule table.
The repository is --repo or, without it, the single GitHub repository of the workspace of PATH (default: the current
directory). Owner identities are --owner-identity (repeatable), else user.name and user.email of the Git configuration
read from the workspace (or the current directory with --repo); without any, usage error. The token is the one of the
other commands. Nothing is written to disk or to the forge.

Exit codes:
  0  no finding of severity error (warnings do not fail)
  1  a read failed or there is no GitHub token (no output)
  2  usage error
  3  at least one error finding, or a list was cut at its bound (the output is printed; the result is not a pass)

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw lint review`

```text
Check the text of a review comment from a file against the review line rules (read-only, no network)

Usage: stw lint review [OPTIONS] <FILE>

Arguments:
  <FILE>
          File with the text of the review comment

Options:
      --base <REV>
          Base revision of the patch-id (with --head)

      --head <REV>
          Head revision: patch-id of BASE...HEAD and the head= field are compared (with --base)

      --pr <NUMBER|none>
          The pr= field must be this pull request number, or none

      --checkout <PATH>
          Repository or worktree for --base and --head [default: the repository of the current directory]

      --human
          Print the human list (the default)

      --json
          Print one JSON object instead of text

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart). Reads the text of one review comment from FILE (UTF-8) and reports one finding per
violated rule with the rule ID, the severity and the line. Rules: the first line is exactly
`Review: <CLEAR|FINDINGS> head=<40 hex> patch-id=<40 hex> pr=<n|none>` or
`Carried-over: from=<40 hex> to=<40 hex> patch-id=<40 hex> pr=<n|none>` (fixed keys and order, lower-case hex, read by the
strict parser of `review lines`); at most 15 lines; no carriage return anywhere; no session link. With --pr the `pr=` field must equal it. With --base and --head (both) the patch-id of
`BASE...HEAD` is computed by git (`git diff BASE...HEAD | git patch-id --stable`, first field; a tag is peeled to its
commit, no merge base is a usage error) and compared with `patch-id=`, and `head=` (`to=` of the Carried-over form) is compared with the id HEAD
resolves to. The repository is --checkout or the one around the current directory. Reads FILE and the repository only;
no network, nothing is written; `git` runs only for --base and --head. `--json` lists the rule table.

Exit codes:
  0  no finding of severity error
  3  at least one error finding (the output is printed)
  2  usage error (also: FILE not readable or not UTF-8, a revision that does not resolve, no merge base)

For AI agents: https://wstein.github.io/sourcesteward/agents/
```

## `stw lint pr-text`

```text
Check the text of a pull request from a file: length, Blocker line, attribution line, session link (read-only, no network)

Usage: stw lint pr-text [OPTIONS] <FILE>

Arguments:
  <FILE>
          File with the text of the pull request

Options:
      --ready
          The pull request is to be set Ready: `Blocker: None.` is required (without it, a Draft: a named blocker is required)

      --human
          Print the human list (the default)

      --json
          Print one JSON object instead of text

  -h, --help
          Print help (see a summary with '-h')

New in stw (no Go counterpart). Reads the text of one pull request from FILE (UTF-8) and reports one finding per violated
rule with the rule ID, the severity and the line. Rules: the text is about 25 lines (warning above 25, error above 30,
the limits of `lint pr`); exactly one Blocker line of the form `Blocker: <text>` (a Draft names its blocker) or, with
--ready, exactly `Blocker: None.`; the last non-blank line is `🤖 Generated with [Claude Code](https://claude.com/claude-code)`;
no session link; no carriage return. A reference `#n` to the wrong repository is not checked. Reads FILE only; no network,
nothing is written. `--json` lists the rule table.

Exit codes:
  0  no finding of severity error (warnings do not fail)
  3  at least one error finding (the output is printed)
  2  usage error (also: FILE not readable or not UTF-8)

For AI agents: https://wstein.github.io/sourcesteward/agents/
```
