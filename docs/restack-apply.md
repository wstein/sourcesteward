# `stw restack prepare`, `apply` and `undo`

`prepare BRANCH --onto ONTO --out PLAN` replays one direct local branch onto one direct local branch and writes the plan file. `apply` and `undo` take `PLAN`. `apply` moves that
branch. `undo` moves it back. `stw restack plan` stays the read-only preview and is a different command.

The branch must not be checked out. A merge commit or a conflict is refused and nothing is written. The replayed
commits keep the author of the original commits. The committer is `user.name` and `user.email` from the Git
configuration, so a user config is enough. `author.*` and `committer.*` in that configuration are refused.

Apply runs no hooks. Prepare and undo do not either. The replay is the in-process engine, and the ref update is a
gix transaction: the commands do not call `git commit`, `git rebase` or `git update-ref`. The replay does not use
`diff.*`, `rebase.*`, `core.hooksPath`, `commit.gpgsign`, `rerere.*` or `apply.whitespace`. The only `git`
program is `range-diff` for the plan text, through the existing runner, without a patch (`--no-patch`). An empty
replay skips that call. A range-diff failure writes no plan file and no prepared ref. The demonstration below
sets no repository identity: the configured user identity is the committer.

The plan file is created only when it does not exist and holds the plan id. The objects stay reachable from
`refs/stw/restack-prepared/<id>`. Apply checks both tips again. A branch or an onto tip that moved is left at the
newer value and nothing is written. The old tip is stored at `refs/stw/backup/<run>/1` in the same counter and
retention as `stw oplog undo`. One transaction then checks the onto tip and the prepared record and moves the
branch. A failure after that backup says that the counter, the backup refs and retention may remain, and that the
transaction outcome is unknown. Undo checks the apply receipt, writes its own backup (and keeps the old tip in
that run) before it moves the branch.

Not in this command: conflicts left for later, merges, a stack of more than one branch, push, review carry-over,
and Change-Id.

## Disposable demonstration

This run used only `/tmp/cursor-restack-demo`. The owner has not accepted it.

```text
git init -q -b main /tmp/cursor-restack-demo/repo
printf 'base\n' > /tmp/cursor-restack-demo/repo/base
git -C /tmp/cursor-restack-demo/repo add base
git -C /tmp/cursor-restack-demo/repo commit -qm base
git -C /tmp/cursor-restack-demo/repo branch topic
printf 'upstream\n' > /tmp/cursor-restack-demo/repo/upstream
git -C /tmp/cursor-restack-demo/repo add upstream
git -C /tmp/cursor-restack-demo/repo commit -qm upstream
git -C /tmp/cursor-restack-demo/repo switch -q topic
printf 'topic\n' > /tmp/cursor-restack-demo/repo/topic
git -C /tmp/cursor-restack-demo/repo add topic
git -C /tmp/cursor-restack-demo/repo commit -qm topic
git -C /tmp/cursor-restack-demo/repo switch -q main
stw restack prepare topic --onto main --out /tmp/cursor-restack-demo/plan
stw restack apply /tmp/cursor-restack-demo/plan --yes
stw restack undo /tmp/cursor-restack-demo/plan --yes
```

`prepare` printed:

```text
plan 0cbf633e9f1d5627ee40dd2ac8324198
branch refs/heads/topic
onto refs/heads/main bd90c578a522ec1d98e01c822c24a9df62668091
old 9f85e16c1ee6e1651d42553fd12277965aed5973
new 1c7daa1e69367c9a510742e446b0ab6e0bcf47a5
prepared refs/stw/restack-prepared/0cbf633e9f1d5627ee40dd2ac8324198
9f85e16c1ee6e1651d42553fd12277965aed5973 1c7daa1e69367c9a510742e446b0ab6e0bcf47a5 clean
1:  9f85e16 = 1:  1c7daa1 topic
Apply with: stw restack apply PLAN --yes
```

`apply` printed:

```text
backup: refs/stw/backup/1/1 9f85e16c1ee6e1651d42553fd12277965aed5973
applied refs/heads/topic 9f85e16c1ee6e1651d42553fd12277965aed5973 -> 1c7daa1e69367c9a510742e446b0ab6e0bcf47a5; undo: stw restack undo PLAN --yes
```

`topic` was `1c7daa1e69367c9a510742e446b0ab6e0bcf47a5`, the new id from the plan. `undo` printed:

```text
retained undo target: refs/stw/backup/2/2 9f85e16c1ee6e1651d42553fd12277965aed5973
backup: refs/stw/backup/2/1 1c7daa1e69367c9a510742e446b0ab6e0bcf47a5
undone refs/heads/topic 1c7daa1e69367c9a510742e446b0ab6e0bcf47a5 -> 9f85e16c1ee6e1651d42553fd12277965aed5973; backup refs/stw/backup/2/1
```

`topic` was again `9f85e16c1ee6e1651d42553fd12277965aed5973`. `main` stayed
`bd90c578a522ec1d98e01c822c24a9df62668091`. These backup refs were still reachable:

```text
9f85e16c1ee6e1651d42553fd12277965aed5973 commit	refs/stw/backup/1/1
1c7daa1e69367c9a510742e446b0ab6e0bcf47a5 commit	refs/stw/backup/2/1
9f85e16c1ee6e1651d42553fd12277965aed5973 commit	refs/stw/backup/2/2
```
