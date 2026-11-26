# What `stw` does not do

[SourceSteward 101](../README.md) · [Roadmap](../ROADMAP.md) · [Development and compatibility](development.md)

This list keeps expectations clear. Each line names the reason or the alternative.

- **No writing to GitHub.** `stw watch`, `stw review`, `stw ready-check` and `stw inspect` only read. Nothing is posted and nothing is marked ready ([review and watch](review-and-watch.md)). Use `gh` or the web interface to comment, mark ready or merge.
- **No merge, tag or release command.** The command list in the [command reference](command-reference.md) has none. The repository owner decides on merges and releases.
- **No long-running service.** Each command runs once. `stw watch --stream` stays in the foreground and prints until you stop it.
- **Recovery scope.** `stw oplog undo` restores one branch from the operation log. Use `stw tidy restore` and `stw sync restore` for their respective recovery flows.
- **No jj (Jujutsu) support.** It may work in a colocated Git repository, without a test and without a promise. Functions that jj offers, such as undo and conflict resolution, are added to `stw` instead ([development](development.md)).
- **No in-memory rerere.** Git's reuse of recorded resolutions works during a manual resolution. `stw restack plan` predicts conflicts without it ([migration plan](migration-plan.md), decision 7).
- **No tested sparse checkout support.** `stw oplog undo --keep` refuses a repository with `core.sparseCheckout`. Sparse checkout and `skip-worktree` are not covered by tests elsewhere ([cleanup and recovery](cleanup-and-recovery.md), [development](development.md)).

For what is planned, see the [roadmap](../ROADMAP.md).
