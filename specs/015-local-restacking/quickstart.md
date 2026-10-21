# Quickstart: `stw restack plan` (first slice)

Acceptance never needs the network: tests use real temporary Git repositories. Nothing here runs `rebase`, `push` or any write.

## Scenario: owner rebase-merged the parent

```sh
work=$(mktemp -d) && cd "$work" && git init -q -b main repo && cd repo
# main: A; parent branch P: B1 B2; child C on P: C1 C2
# the owner rebase-merges P into main: main gets B1' B2' (new SHAs)
stw restack plan \
  --branch refs/heads/child \
  --old-parent "$(git rev-parse refs/heads/parent)" \
  --new-parent refs/heads/main \
  --identity "Owner Name <owner@example.org>" --json
```

Expected: `status: ok`, `evidence.parent_dropped.all_dropped: true` (cherry rows `-`), `evidence.range_pairing.all_equal: true` for `C1 C2`, `evidence.identity.state: pass`, `texts.lease_push` printing the observed child tip and the literal `<NEW_FULL_SHA>`, and `limitations` containing `semantic-breaks-not-detected`.

## By hand, to compare with the plan

```sh
git rebase --onto <new parent> <old parent> child
git range-diff <old parent>..<old head> <new parent>..child      # all =
git cherry <new parent> <old parent>                             # all -
```

## Conflict, refusal and overlap cases

- A parent change that touches the same lines as `C1`: `outcome: conflicts-predicted`, `conflicts.items[].path`, exit 0.
- Dirty target, branch checked out in another worktree, a merge commit in the range, unrelated histories: `status: error`, the diagnostic code of the contract, exit 1, no `evidence`.
- A parent change to `a.go` that `C1` also edits cleanly: `overlap.indicator: some`, a risk indicator only.
- A parent that changed a signature the branch calls in another file: replay is clean, `overlap` is `none`, and the plan cannot see the break: build and test after restacking.

## Offline checks

```sh
go test ./internal/restack ./internal/cli ./internal/contracts -run 'Restack' -count=1
```
