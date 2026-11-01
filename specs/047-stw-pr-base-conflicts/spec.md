# Specification: base conflicts of open pull requests, stack status and restack plan

**Created**: 2026-11-01 | **Status**: proposed (spec-light: this page and [contracts/conflicts.md](contracts/conflicts.md) are the whole design).
Issue #355. It amends [Spec 036](../036-stw-pulls/spec.md) (`stw pulls`) and builds on [Spec 046](../046-stw-pr-review-state/spec.md)
(review state; its stack view is a later pull request of that spec, so this spec carries the small tree it needs).

A pull request that was Ready and CLEAR becomes CONFLICTING when its base changes the same file, and nothing in `stw` shows it. The
owner needs to see, per open pull request and per stack level, whether it still merges with its base, which files conflict and which
commit on the base touched them, and an exact restack plan with a brief for the agent that must do it. `stw` shows and prints; it never
rebases, pushes, labels, marks Ready or merges.

## Detection (read-only, opt-in)

`stw pulls --conflicts` (one repository, a local checkout, see Reads) probes every open pull request against the **current tip of its
base branch**; for a stacked child that tip is the head of its parent. Per pull request:

1. `git merge-tree --write-tree --name-only -z --no-messages <base> <head>`: exit 0 is `clean`, exit 1 is `conflicts-with-base` with the
   file list; anything else is `unknown`. The probe writes no object to the repository (its objects go to a temporary quarantine
   directory that is removed) and does not touch HEAD, the index, the working tree, the configuration or any ref.
2. For each conflicting file: the last commit in `merge-base..base` that changed it (`git log -1`), empty when none is found.
3. Early warning for a `clean` pull request: the files the base changed since the merge base **and** the pull request changed too
   (`overlap`). It says "the base moved on a file of this pull request", not that a conflict exists.

The one local effect is the fetch of `refs/pull/N/head` and the base branches into the private namespace `refs/stw/conflicts/`
(`--no-tags --no-write-fetch-head`), disclosed in `stw pulls --help`. A head that moved between the list and the fetch is `unknown`
(`head-moved`). At most 100 pull requests are probed per run (the rest `unknown`, `past-probe-limit`); lists are cut at 200 files
(`truncated`). A base branch name outside a plain allowlist is never put in a refspec (`ref-invalid`). `unknown` is never shown as clean.

## Status in the stack view

Pull requests form trees: B is the child of A when `B.base == A.head_ref` and neither is a fork pull request; a head ref shared by
several pull requests links nothing; a cycle is cut. A tree is shown when it has a child or its root conflicts. Per level:
number, head, CLEAR (`valid` for `clear` and `clear-carried`, `invalid` for `clear-stale`, `none`, `unknown` without review reads), CI,
the probe state and a status:

- a level that conflicts: `blocked: restack needed`, group `conflict`, `ready` false;
- a level above a conflicting one: `blocked by #n` (n the lowest conflicting ancestor), a Ready child becomes `blocked`, reason
  `parent-conflict`, `ready` false; the hint `ready-mergeable` is not given to either.

A conflict never appears as Ready. A Draft keeps its group `draft` and still shows the probe result.

## Restack plan and agent brief (`--restack-plan`, implies `--conflicts`)

Output only. Per tree with a conflict: the lowest conflicting level and every level above it, bottom first. Per level the commands
`git fetch`, `git switch <branch>`, `git rebase --onto <new> <old> <branch>` (lowest level: `<remote>/<base>`, with the remote of `--remote` (default `origin`), and the merge base from the
probe; above it: the rebased parent branch and its old head), `git range-diff <old>..<old head> <new>..<branch>` and, separately, the
push `git push --force-with-lease=<branch>:<old head> <remote> <branch>` that runs only after the review of the new head. For pull
requests that form a gh stack the same cascade as `gh stack checkout <root>`, `gh stack rebase`, `gh stack push`. A fork head, a name
that fails the allowlist or a missing SHA gets no commands and a reason.

The brief is Markdown: the conflicts with files and base commit, the order, the commands and the required checks: take over the
conflict wording of the base unchanged, range-diff, tests, back to Draft, author and committer only the owner, no session link, a new
independent review of the new head before the push and before Ready (strong model for rule, contract or security text), push only the
own branch with the lease. The SHAs are valid only while heads and base are unchanged; the brief says so.

## Reads and cost

No forge call is added. The command needs `--repo OWNER/NAME` (exactly one repository) and a checkout whose remote `origin`
(`--remote`) points to that repository (`--checkout PATH`, default the current directory); otherwise a usage error (exit 2). Cost per
run: one fetch, then per probed pull request one `rev-parse` pair, `merge-base`, `merge-tree` and, per conflicting file, one `log`; for
a clean pull request two `diff --name-only`.

## JSON and compatibility

Additive and optional, in place in the unpublished `stw pulls` schema 1 (byte-frozen by digest, updated in the same commit): see the
[contract](contracts/conflicts.md). `schema_version` stays 1. `stw status` is unchanged.

## Non-goals

No rebase, push, label, Ready or merge, no conflict resolution, no probe of pull requests of other repositories in the same run, no use
of `gh stack` by `stw`, no search for the commit that introduced a conflict beyond the last base commit touching the file.

## Verification

Temporary repositories for the git runner (state unchanged, no object written, namespace only), fakes for the logic. Every rule has a
test and a mutation that makes it fail; the mutation list is in the pull request.
