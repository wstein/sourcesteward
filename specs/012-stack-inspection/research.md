# Research: Stack inspection and plan (v2)

Evidence gathered on 2026-10-17. Environment: Git 2.56.0 (`/usr/local/bin/git`) and Git 2.47.3 (`/usr/bin/git`) on linux/arm64; `gh` 2.102.0 with the extension `gh stack` v0.2.1 (`github/gh-stack`); **no jj binary** in this environment (only a throwaway fake under another session's scratch directory), so every jj statement below is design, not measurement. Git experiments ran in scratch repositories under the session scratchpad (`exp/`). GitHub facts come from the owner's spike in `wstein/stack-spike-20261017` (a private repository that allows merge, squash and rebase merges; PRs merged by the owner in the web UI) and from read-only `GET` calls against that repository made while writing this document. No write call was made.

## Measurements

### M1. Merge commit (spike scenario A)

Plain chain `main <- s1 <- s2 <- s3`, PR #1 merged with a merge commit. Commits keep their ids and `main` gets a merge commit. The child pull request #2 keeps base `s1` and stays mergeable and clean; GitHub did not retarget it. Consequence: after the base is fetched, `s1` is an ancestor of `main`, the range `main..s3` no longer contains it, and nothing needs to be rewritten.

### M2. Rebase merge without a native stack (spike scenario R)

Plain chain `main <- r1 <- r2 <- r3`, PR #4 rebase-merged. `main` received a new commit id (`e8bf4de`, the pull request commit was `b2c70a1`) even though a fast-forward was possible: GitHub's rebase merge always rewrites. The child #5 keeps base `r1` (head branch not deleted) and stays clean. Retargeting #5 to `main` would carry the old `r1` commit and conflicts locally (`git merge-tree main r2`). Merging #5 and #6 bottom-up landed them in the intermediate branches `r1` and `r2`, not in `main`. Consequence: for a plain chain the maintainer must rebase the remaining layers (`git rebase --onto`), bottom up, with the old tips recorded first.

### M3. Native stack (spike scenario T)

The same chain was created as plain PRs, then linked with `gh stack link 7 8 9`, which created native Stack #10 with base `main`. The repository had "delete head branches" on, which confounds the retarget. The issue timeline orders the events: #8 `automatic_base_change_succeeded` at 17:33:51, then #7 `head_ref_deleted` at 17:33:52, then the force-push of `t2` at 17:33:54; the ordering suggests the retarget is native, but a retarget triggered by head deletion on a plain chain is unmeasured (plain chains #1 and #4 never had their head branch deleted). Treat both as confounded. After PR #7 was rebase-merged (new commit `233133d`) GitHub itself retargeted #8 to `main`, force-pushed `t2` rebased onto the new `main` (a single commit, no conflict) and rebased `t3` too; #9 stayed based on `t2` until #8 merged, then was retargeted. Re-read on 2026-10-17: PRs 7, 8, 9 are `closed`, `merged`, base `main`, `merge_commit_sha` `233133d`, `1ab44a9`, `c4df8f0`. Consequence: for a native stack the rewrite after a merge is GitHub's; a local rebase step would race with it and duplicate it.

### M4. The `stack` object on a pull request

`GET repos/O/N/pulls/9` and the items of `GET repos/O/N/pulls?state=all&per_page=3` carry `stack`:

```json
{"base":{"ref":"main","sha":null},"id":2264365,"number":10,"position":3,"size":3}
```

The key is **absent** (not `null`) on pull requests of plain chains (#1, #4, #5 checked). `base.sha` is `null`. Stack number 10 is not a pull request number: `GET pulls/10` is `404`. `mergeable_state` is `unknown` for these closed pull requests, so it is not used.

### M4a. The `head=` filter

`GET repos/O/N/pulls?head=wstein:t2&state=all&per_page=5` returned only #8, with its `stack` object (position 2), although the head branch had been deleted on merge: a deleted head branch still matches. Refs containing `/` remain unverified.

### M5. The stack endpoints

`GET repos/O/N/stacks` returns a JSON array (one stack here) and honours `page`: `page=2&per_page=1` returned `[]`; whether `per_page` and `Link` headers behave as for other collections is unverified (no `Link` header on the one-page answer). `GET repos/O/N/stacks/10` returns:

```text
id, number, node_id, url, base.ref, open (false after the merges), created_at,
pull_requests[]: url, id, number, head.{ref,sha,repo.{id,url,name}}, base.{ref,sha,repo},
                 node_id, title, state, merged_at, draft, user
```

in stack order, bottom first (7, 8, 9). After the merges every member shows `base.ref` `main` and its own `base.sha` (the base commit at merge time). `GET stacks/999` is `404`. The documentation URL in the error body is `https://docs.github.com/rest/pulls/stacks#get-a-pull-request-stack`, so the endpoints are documented REST resources (the page itself was not read here). Response header `X-GitHub-Api-Version-Selected: 2022-11-28`. Anonymous access answered `404` for the repository and for `stacks` because the repository is private, so anonymous behavior for a public repository is unmeasured. An **open** stack's `open: true` shape and the base refs of its members while open are unmeasured (every spike stack was merged before this re-read).

### M6. The `gh stack` command surface (v0.2.1, `--help` only)

Commands: `init`, `add`, `submit`, `sync`, `rebase`, `merge`, `view`, `checkout`, `modify`, `unstack` (alias `delete`), `link`, `push` and navigation commands. Relevant semantics from the help text: `link` "does not rely on gh-stack local tracking state" and is "designed for users who manage branches with external tools (e.g. jj, Sapling, ghstack, git-town)"; arguments are bottom to top, branches are pushed automatically, branches without a pull request get one created, existing stacks are extended ("existing PRs are never removed"). `sync` fetches, fast-forwards the trunk, cascade-rebases, pushes with `--force-with-lease --atomic` and may prompt on divergence in an interactive terminal. `merge` is "all-or-nothing", checks only "open and not a draft" itself, defers branch protection to GitHub's evaluation, and does not support bypass. `unstack` leaves PRs that are queued or have auto-merge enabled stacked. These are the semantics a later Spec 016 effect delegates to.

### M7. `gh stack` local state

`gh stack init a b` in a scratch repository created `.git/gh-stack` (JSON, `schemaVersion` 1, `repository`, `stacks[].trunk{branch,head}`, `stacks[].branches[]{branch,base}`) plus two lock files, and `gh stack view --json` printed `isMerged`, `isQueued`, `needsRebase` per branch. Consequence: that file exists only for users of `gh stack init`; it is repository-writable, therefore untrusted data, and absent for jj users. `stw` does not read it and does not run `gh stack`.

### M8. `gh stack link` constraints (spike)

`gh stack link` rejects `-R`; the repository must be given as `GH_REPO`. Native stacks are not created automatically for chained pull requests (M2 chain stayed plain).

### M9. Git: detecting and repairing a stale child (Git 2.56.0)

Scratch repository: `main` with commit `base`; branches `s1 <- s2 <- s3` one commit each; then `main` received the cherry-pick of `s1` with a different committer date (new id `adcdcf6` against `5758b57`), simulating a rebase merge.

| Command (read-only unless noted) | Result |
| --- | --- |
| `git merge-base --is-ancestor <old s1> main` | exit 1: not contained |
| `git cherry -v main s2` | `- 5758b57 s1` (patch-equivalent in `main`) and `+ eceac4e s2` |
| `git cherry -v main s1` | `- 5758b57 s1` (all commits equivalent: the layer is applied) |
| `git rev-list --count main..s2` | 2 (the old parent still counts) |
| `git rebase --onto main 5758b57…(full id) s2` (writes) | `Successfully rebased`; `main..s2` is one commit |
| then `git rebase --onto s2 eceac4e…(full id of the old s2) s3` (writes) | `Successfully rebased`; `main..s3` is `s3` and `s2` |
| `git rebase main` on the stale `s2` (writes) | also succeeds and skips the already applied commit, with an advice message |

The exact printed form therefore is `git rebase --onto <new parent> <old parent tip id> <branch>` applied bottom up, recording old tips before the first rewrite. The same `git cherry` call on Git 2.47.3 reproduced the `-`/`+` classification for the rebased state. The minimum lane (Git 2.46.0) was not run here.

### M10. A merge simulation writes objects

`git merge-tree --write-tree p1 q1` on two diverged branches increased the loose object count from 27 to 28 (a new tree). Consequence: conflict prediction through `merge-tree --write-tree` is not read-only in the constitution's sense (Spec 014 allows disclosed temporary writes; this spec does not), so conflicts are not predicted here.

### M11. `git cherry` runs no helper

With a repository-controlled `.git/info/attributes` (`* diff=foo filter=bar`) and `diff.foo.command`, `diff.foo.textconv`, `filter.bar.clean` and `diff.external` each set to a command that creates a marker file, `git cherry`, `rev-list`, `merge-base --is-ancestor`, `for-each-ref` and `rev-parse --verify` created no marker (Git 2.56.0, text patch). The runner of Spec 021 additionally passes `--no-replace-objects`, `core.hooksPath=/dev/null`, `core.fsmonitor=false`, all transports off and `core.commitGraph=false`. Binary and large patches under `cherry` were not exercised; a hostile-input test is in the tasks.

### M12. The existing Git runners

`internal/inspection/git.go` `runGit` caps output at 64 KiB and fails on overflow; the Spec 021 runner (`pins_run.go`: `pinsIsolation()`, `pinsEnvironment()`, `pinsRunner.run`) adds the isolation prefix, explicit `--git-dir`/`--work-tree`, per-call timeout, a scan allowance and streaming callbacks (`Keep`, `Each`, `Chunks`), and requires the caller to validate ownership first (Git skips its own ownership check with an explicit `--git-dir`). The range read of 500 commits with parents is about 500 x 83 bytes = 41.5 KB, below 64 KiB; the design streams anyway.

### M13. jj (measured by the round 2 reviewer on 0.46.0 and 0.45.1, identical results; evidence under the session scratchpad `r2/`)

The pinned versions are 0.45.1 (minimum) and 0.46.0 (current) (`scripts/backend-releases.json`); both are installed in the session. The existing adapter reads bookmarks with a template and pins the operation (`--no-pager --color=never --ignore-working-copy --at-operation=OP`). Measured: `-d` is an alias of `-o`/`--onto` for `jj rebase` with no warning (the help page of both versions documents `--onto/-o`, which the printed step uses); an unquoted `s1-` fails as a revset (and silently means the parent of `s1` when a bookmark `s1` exists; measured in round 3), a tracked remote bookmark resolves as `main@origin` or `"main"@"origin"` but not as `"main@origin"`, and `"s1-"`, `"a-b"`, `"feat/x"` and `"x+y"` resolve; the template keywords `conflict`, `immutable`, `divergent` and `empty` work; reads with `--ignore-working-copy` add no operation while a plain `jj log` snapshots and adds one; `jj git fetch --remote` exists; after a rebase onto a base that already contains the layer's patch, the offline already-applied layer 1 becomes an empty commit that keeps its bookmark. Fetch moves tracked bookmarks and abandons the old commits, so on jj the "diverged forever" state of Git does not occur for a bookmark the maintainer did not move; after a local rebase of a tracked bookmark the divergence from the remote persists, and when the remote also rewrites that bookmark it becomes conflicted on both versions, which FR-004 refuses with `stack-ambiguous-topology`. A PR head can also be unreachable: the spike's PR #8 head `6e019b0` is reachable only through the retained `refs/pull/8/head` (measured with `matching-refs`), not through any branch. Still unmeasured on jj: tracked remote bookmark relation reads for the upstream finding, and the exact range revsets of the adapter (task T002).

### M14. A gone upstream, cherry and merge commits, and a jj landed layer (round 4 and 5 reviewers, Git 2.56.0, jj 0.46.0 and 0.45.1)

After a server rebase-merge with the head branch deleted, `git fetch --prune` leaves the local `s1` with `%(upstream:track)` `[gone]` and `s1@{upstream}` no longer resolving; the layer is `applied` by patch equivalence, so the `gone` relation is outside the advice groups. `git cherry` omits merge commits entirely: with a collaborator's "Update branch" merge commit adding a file `x` on the upstream and the maintainer's rebase of `s1`, `git cherry s1 origin/s1` printed a single `-` although a lease-protected push would have dropped the merge and `x`; therefore no advice relies on `cherry`, and a diverged upstream is described by counts only. On jj the same remote merge and head deletion makes `jj git fetch` remove both `s1` and `s1@origin`; no allowed read records it (`jj op diff` does, and is not allowlisted), the landed commit sits unbookmarked inside layer 2, and a printed `jj rebase -s <old s1 change> -o '"main"'` would move it (an empty commit inside PR s2); hence the stated gap `jj-landed-layer-undetectable`.

### M15. Round 6 measurements (reviewer, jj 0.46.0 and 0.45.1 identical, Git 2.56.0 files ref backend)

- **head.sha in range:** for a native stack, a merged member's `head.sha` (a field of `stacks/K`) is an exact test of whether the landed commit lies in the jj range; on a plain chain the merged member is not reachable from the allowed reads (the layer ref name no longer exists, GitHub's auto-retarget sets the next base to `main`), so no test exists.
- **Unguarded jj rebase of the landed commit:** `jj rebase -s <old s1 change> -o "main"` after a rebase-merge makes the old `s1` an empty commit inside layer 2 and `--skip-emptied` abandons it. After a multi-commit squash the outcome depends on content: commits changing different files both become empty and `--skip-emptied` reports "Abandoned 2 newly emptied commits"; if the second commit rewrites the first's lines the first is conflicted, the second is non-empty and `--skip-emptied` changes nothing.
- **gone versus unknown:** for a remote-tracking ref whose object is absent, `%(upstream:track)` prints `[gone]` and `rev-parse --verify --quiet REF^{commit}` exits 1 like a deleted ref; the unpeeled `rev-parse --verify --quiet REF` exits 0 and prints the id, which separates the two. Verified on the files ref backend only: the reftable backend refuses a ref to a missing object, so the `unknown` state could not be built there.

## Decisions and rejected alternatives

**D1. Read-only first, confirmed.** The intended shape (observe, then print, then optional effects as separate items) is confirmed by M1 to M3: the merge shape decides whether any local action exists at all, and for a native stack GitHub performs it. Challenge raised by the evidence: "head not rebased after parent merge" and "stale child after rebase-merge" are drift only for **non-native** chains; for a native stack the finding stays (the local branch is stale) but the repair is GitHub's rewrite, which the plan only describes (`take-upstream` after the fetch, M3), so the plan splits by `native_stack`. A second narrowing: a missing stack link is only meaningful for two or more chained open pull requests.

**D2. Observation sources.** Local topology from the repository; remote evidence from the pull requests' own `stack` objects and `stacks/K` (M4, M5). Rejected: (a) `gh stack view --json` (M7: local tracking state, jj users have none, subprocess, extension must be installed, not versioned); (b) the repository-wide `stacks` list (pages over unrelated stacks; membership is already on each pull request); (c) GraphQL (excluded by Specs 020/022); (d) reading `.git/gh-stack` (untrusted, absent for jj).

**D3. Local detection first.** After a rebase-merge the stale child is visible without a network read on Git (`git cherry`, M9), so `show`/`plan` work offline and the pull request evidence strengthens rather than creates the finding. For jj no read-only patch-equivalence is known; it is a stated capability gap (Tier 1 allows gaps, principle V) closed by pull request evidence when requested. Rejected: copying a jj-specific approximation (comparing `jj diff --git` text) as it would produce different semantics under the same finding name.

**D4. No conflict prediction.** M10. Rejected: `git merge-tree --write-tree` (writes), a temporary worktree (Spec 014 territory).

**D5. Printed commands use full old-tip ids.** M9. Branch names move after the first rewrite; only ids keep the recorded old parent. Rejected: `git rebase <base>` (works by patch skipping, but depends on advice-driven behavior and rewrites ids the next step needs), `git rebase --update-refs` (a one-command alternative since Git 2.38, rejected for the first release because it rewrites every ref pointing into the range, including the `fork-point` refs the finding warns about (documented behavior, not measured here); recorded as a possible option in the plan).

**D6. Explicit `--base` and `--tip`.** No default tip from HEAD: a jj workspace has no current branch, and peers should behave the same. Rejected: discover the stack from all local branches (ambiguity, FR-004).

**D7. Network is opt-in.** `--pull-requests` makes the network effect visible in the command line (constitution: network calls are effects a contract must disclose; Spec 016 FR-002). Rejected: always read when a remote exists (surprise authentication and rate use).

**D8. Delegation, not reimplementation, for native stacks.** M6 shows `gh stack link` is the supported path for jj-style workflows. For the later link effect the owner wants the bounded transport, not a subprocess; the write endpoints are unmeasured (writes need explicit authorization), so the effect catalog ([contracts/later-effects.md](contracts/later-effects.md)) lists the measurement as a precondition and keeps the printed `gh stack link` line as the only 012 output.

**D9. Merge-method neutrality.** The maintainer's choice is respected; findings cover all three shapes:

| GitHub merge shape | Local state after fetch | Finding | Plan |
| --- | --- | --- | --- |
| merge commit (M1) | layer commits are ancestors of the base | none | none |
| rebase merge, plain chain (M2) | old commits stay in range, patch-equivalent in the base | `layer-applied-in-base` / `parent-merged-child-stale` (`rebase`) | `rebase --onto` per remaining layer |
| rebase merge, native stack (M3) | GitHub already rewrote the remote heads (non-descendant of the old local tips) | `parent-merged-child-stale` (`native_stack`), `head-sha-differs` `diverged` (`unknown` before the fetch) | one `observe` (group 4: `take-upstream`, or `native-rewrite-pending` when the pull request head equals or is behind the local tip); `fetch` and `observe` (group 2) only while an object is missing; with the merged member's local branch deleted see scenario 8b (`landed-undetectable`, group 5 guard) |
| squash merge, one-commit layer | the squashed patch equals the layer's patch | `layer-applied-in-base` (patch equivalence, measured by the round 2 reviewer: `git cherry` gives `-`) | `rebase --onto` per remaining layer (group 5) |
| squash merge, multi-commit layer | old commits in range, no patch equivalent | `parent-merged-child-stale` (`rewritten`, remote evidence only), inferred | `rebase --onto` per remaining layer (group 5) when `merge_commit_sha` is in `--base`; conflicts possible and found by the real rebase (unverified here) |
| merge commit not contained in `--base` (stale base) | the merged layer's commits stay in range because `--base` was not updated | `parent-merged-child-stale` (`merge`) | `observe` note `stale-base` (group 3); no rebase onto the stale base |
| any shape, offline (no `--pull-requests`) | native status unknown | local findings only | rebase steps with the boundary limitation `native_checked: false`; upstream `behind`/`diverged` switches to a describing `observe` (group 4) |

## Unverified (each needs measurement before or during the named task)

| Id | Claim | How to verify | Task |
| --- | --- | --- | --- |
| U1 | jj: tracked remote bookmark relation reads for the upstream finding, the adapter's range revsets, the one-step `jj rebase -s C -o N` moving descendants and the working-copy commit (flag, quoting, keywords and snapshot behavior are measured, M13) | scratch jj repositories on 0.45.1 and 0.46.0 | T002 |
| U2 | Git 2.46.0 (minimum) results for M9 and M11 | minimum lane run | T012 |
| U3 | Refs containing `/` with the `head=O:REF` filter (the filter itself and `stack` on filtered items are measured, M4a) | read-only call in a repository with such a branch | T023 |
| U4 | Open native stack shapes (`open: true`, member base refs while open), `stacks` pagination and `per_page`, public-repository and anonymous behavior, plan or permission requirements, GitHub Enterprise Server availability | read-only calls against an open stack; documentation read | T023 |
| U5 | `merge_commit_sha` for multi-commit rebase merges and for squash merges; whether it can be absent for a merged pull request | spike repository additions (owner action) | T023 |
| U6 | GitHub's maximum stack size; behavior with genuine conflicts between layers; timing of server-side rebases after a merge (timeline: the rebased heads appeared 4 s (#8) and 5 s (#9) after the merge; eventual consistency otherwise unmeasured) | documentation; owner spike | T023 |
| U7 | The write endpoints for stack creation, extension and removal | out of scope; precondition of later effects | later-effects |
| U8 | Fork pull requests (`head.repo` other than the base repository) and deleted head repositories | fixtures only until measured | T023 |
| U9 | `git cherry` with binary files, huge patches, textconv drivers and `merge` attributes | hostile-input tests | T014 |
| U10 | `Link`/pagination headers of the `pulls` list at 100 items are those of other collections | transport tests with fixtures; the design reads at most 5 records per request, so no paging is needed | T023 |
