# Quickstart: Pull request merge-state explanation

Acceptance never needs the network: the Go tests use real temporary repositories and an in-process fake HTTP transport. The live steps below are an optional read-only check against a public repository; they need `gh auth login` and make only the fixed `GET` requests of the Sources table. They do not change the repository. The command does not exist until the work items land, so the "expected" lines come from the design and the 2026-10-16 measurements in `research.md`, not from a run.

## Offline reproduction of the incident

```sh
go test ./internal/hosting -run 'MergeState' -count=1
go test ./internal/cli ./internal/contracts -run 'MergeState' -count=1
```

The incident fixture is a strict, linear-history, rebase-only base branch with the required context `check` pending and the head 3 commits behind the base tip. Expected: `required-check` (`pending`, `waits`, `observed`) and `head-behind-base` (`waits`, `observed`), plus the information items of User Story 1, on ordinary Git, linked Git worktree, standalone, colocated and secondary jj repositories, through `stw hosting` and `gh jj`.

## Optional live read-only check (temporary repositories only)

Choose any open pull request of a public repository you can read, and substitute its owner, name and number. Pull request 112 of `wstein/bundledock`, measured on 2026-10-16, merged at 13:11:59Z and now yields only `pull-request-closed`, so it is not a stable example.

```sh
work=$(mktemp -d) && cd "$work"
git init -q repo && cd repo
git remote add origin https://github.com/OWNER/NAME.git
stw hosting merge-state NUMBER
stw hosting merge-state NUMBER --json
```

The design expects, for a read-only token on a branch with classic protection, `classic-protection-unreadable` (`unknown`, `not-found-ambiguous`) and `merge-methods` `unknown` with reason `repository-setting-hidden`, and never a statement that no protection exists; with an admin token the classic fields are read. When GitHub reports `blocked` and nothing readable explains it, `blocked-unexplained` appears as `unknown`.

A jj variant, in another temporary directory:

```sh
jj git init --colocate repo-jj && cd repo-jj
jj git remote add origin https://github.com/OWNER/NAME.git
stw hosting merge-state NUMBER
gh jj merge-state NUMBER
```

## Checks to run on any output

- No output line outside the labelled "GitHub reports" line and the fixed notice says the pull request is mergeable, ready, can merge or will merge; `verdict` is `not-asserted`.
- `stw hosting merge-state NUMBER --json | python3 -c 'import json,sys; r=json.load(sys.stdin); assert r["verdict"]=="not-asserted"; assert all(i["reason"] is not None for i in r["items"] if i["confidence"]=="unknown")'`.
- `git -C "$work/repo" status --porcelain` and `git -C "$work/repo" for-each-ref` print the same before and after; nothing is fetched.
- A wrong number (`stw hosting merge-state 99999999`) exits 1 with `merge-state-pr-failed` and no items; `stw hosting merge-state 0` exits 2 with no JSON.

## What the output does not prove

A result with no waiting item says only that none was found in the evidence read. It does not say the pull request can be merged, will merge or will merge soon: bypass actors, review-thread resolution, merge queue state and reviewer eligibility are not read, GitHub's own state can disagree (measured), and the sample is not a transaction.
