# Branch cleanup and recovery

[SourceSteward 101](../README.md) · [Command reference](command-reference.md) · [Remote kinds and the git runner](development.md#remote-kinds-and-the-git-runner)

## Backup branches

Branches named `r9-*`, `r10-*`, `pre-restack*`, `archive/*` or `backup/*` are backups. `tidy plan` classifies them
after the proofs (a backup that is an ancestor of the base, has equal patches or a merged pull request is `delete`
by that proof) and before `review`: a backup is never `review`. Without a proof it is `keep` (`backup-recent`)
while the tip commit is younger than N days and `delete` (`backup-older-than-days`, "backup older than N days")
from exactly N days on. `--backup-days N`, default 7, on `tidy plan` and `tidy apply`.

The age is the committer date of the tip commit, not the date the branch was created. Weakness: an old commit
under a freshly created backup name looks old and is `delete` at once. The reflog was measured as an alternative
and is not used: a branch created locally has a `branch: Created from ...` entry that survives `gc`, but a clone
has no local branch and no reflog for it (only remote-tracking refs), a branch created in another clone has none
here, and entries expire (`gc.reflogExpire`, 90 days by default). The plan is reversible with `tidy restore`, within limits: `apply` deletes the ref together with its reflog, so an
unmerged backup commit survives only until `git gc` prunes unreachable objects (about two weeks by default,
`gc.pruneExpire`); after that `restore` reports `missing-object`. Commits that are merged or on a remote stay
recoverable.

## Similar patches in `tidy plan`

A branch can have commits without an equal patch on the base (`equal-patches` fails) although the change is on the base: a cherry-pick with a
conflict resolution, a rebase that touched a line. The similarity stage compares each such commit (the `+` commits of `git cherry`) with the commits of
the base. Method and its limits: `spikes/stw/tidy-similarity/README.md`; code: `src/tidy_similarity.rs` and the stage in `src/tidy.rs`.

Proof `similar-patches`, decision `review`, never `delete`: every commit of the branch without an equal patch has a commit on the base that is at least
`--similarity PCT` (default 95) similar; the detail says `min`, `median` and the threshold. `tidy apply` does not act on it (it acts on `delete`
only), so such a branch is not deleted by `apply`; the plan prints no command for it. The order: after `equal-patches`, `merged-pull-request-head` and
the backup proofs, before `pull-request-merged-branch-moved`, `upstream-gone` and `unmerged`. The gates stay: an open pull request keeps the branch
(`open-pull-request`), a branch checked out in a worktree is `in-worktree` (the `similarity` object stays in the entry), a backup branch is never `review`.
The stage runs only where the proof would be `pull-request-merged-branch-moved`, `upstream-gone` or `unmerged`.

Flags on `tidy plan`: `--similarity PCT` (1 to 100, default 95), `--similarity-low PCT` (1 to 100, default 70, not above `--similarity`; else exit 2),
`--no-similarity` (the stage does not run: the text plan is the plan without it, JSON has `similarity: null`; conflicts with the other two). `tidy apply` has
no flag and plans without the stage. The window of 7 days (a candidate is at most 7 days older than the commit, author time) is a constant. The index of the base
holds the commits from one window before the oldest `+` commit on; a commit whose parent is missing (the boundary of a shallow clone) is left out and counted.

JSON, additive in schema 2, per branch `similarity` (null where the stage did not run: a proof applies, a gate holds, `--no-similarity`):
`class` (`landed`: all `+` commits at or above the threshold; `modified`: all at or above `threshold_low`, one below the threshold; `partial`: one without candidate, without
value or below `threshold_low`; `none`: no commit to judge), `threshold`, `threshold_low`, `window_days`, `plus_commits`, `at_or_above`, `min` and `median` (percent with one decimal or null),
`without_candidate`, `skipped{merges,empty,too_large,shallow}` (`shallow`: commits of the base left out of the index), `bounds{candidates_cut,main_commits_indexed}` and
`commits[{commit,subject,outcome,skipped,candidate,candidate_subject,reason,similarity,similarity_no_path,evidence}]`. `outcome` is `compared`, `no-candidate` or `skipped`
(`skipped`: `merge`, `empty` or `too-large`); `reason` says how the candidate was found (`subject`, `subject-stripped`, `files`, else `none`); `evidence` has up to 5 lines
`{path,sign,line,count_a,count_b}` for a commit below the threshold (untrusted text, escaped). A value that rounds to a threshold but is below it is shown one tenth lower.
Text: one line per `similar-patches` branch with the figures, and `(N of them similar-patches)` in the summary line.
`skipped.merges` and `skipped.empty` are always 0 in the plan: the `+` set holds only commits that have a patch (a merge commit and an empty commit have none), so these two keys
count only where the function of the stage is called with such commits (it is a pure function with its own tests).

## Rebase evidence in `tidy plan`

GitHub's rebase merge writes every commit of a pull request again (new ID, new committer, new parent) and keeps author, author time and subject. The stage maps the
commits of a branch to the first-parent chain of the base by that key and says how the branch landed. Method and limits: `spikes/stw/tidy-similarity/README.md`
(packages 1b and 2b); code: `src/tidy_rebase.rs` and the stage in `src/tidy.rs`.

Proof `rebase-identity`, decision `review`, never `delete`: every commit of the branch (merge commits of the branch do not count) has a commit on the chain with the same author
name, author mail (lower case), author second and subject, the targets are one block in branch order, every commit has a content value of at least 95 % (the owner's floor) and
none is a later version. The detail reads `rebase-identity (3 of 3 commits, run of 3, min 100 %)`. `tidy apply` does not act on it (it acts on `delete` only and plans without the
stage), a plan file edited to say `delete` for such a branch is refused (`the proof no longer holds`), and the plan prints no command. The order: after `equal-patches`,
`merged-pull-request-head` and the backup proofs, before `similar-patches`, `pull-request-merged-branch-moved`, `upstream-gone` and `unmerged` (the existing proofs and the gates keep
their place; a branch in a worktree is `in-worktree` and its `rebase` object stays). A hint of the stage (identity without one run, a partial landing) takes nothing away from the similarity stage: it judges every commit as before, so
the default plan is never weaker than the plan with `--no-rebase-evidence`. A stage that fails for a branch (or the index of the base) leaves the plan standing: `rebase` is null and the detail says `; rebase evidence not computed (<reason>)`.

Flag: `--no-rebase-evidence` (the stage does not run: no `rebase-identity`, `rebase` is null, no pull request commits are read; `landed_via` stays `merge`/`ancestor` for an ancestor and
is `unknown` otherwise; independent of `--no-similarity`). The thresholds are constants, not flags (the floor, the stop after 64 old author times, a window of at most 50 000 chain commits).
The patch IDs come from the `PatchCache` of the `equal-patches` walk; one `SimCache` serves the rebase and the similarity stage. The two stages walk the base twice (all ancestors for the
similarity index, the first-parent chain for the rebase index): they ask for different commits.

Pull request (read only, optional): where a branch would be `pull-request-merged-branch-moved`, `upstream-gone` or `unmerged`, has a merged pull request, and the commits do not already
prove it, one request `GET pulls/N/commits?per_page=100` reads the commits of the pull request (at most 30 requests per repository, the rest is `request-limit`; a full page of 100 is `too-many-commits`). The token
needs `Pull requests: Read` and `Metadata: Read`, no more. Without network, token or with a rate limit the plan uses the commits alone: no error, no change of the exit code; the reason is in
`rebase.api.reason`: `offline`, `no-token`, `no-pull-request`, `rate-limited` (GitHub said so), `request-limit` (the 30 requests are used), `request-failed`, `evaluate-failed` (the local check failed), `too-many-commits`, `not-merged`, `merge-commit-not-in-clone`, `not-on-base`, `not-needed`
(a proof that needs no pull request). The mapping: two parents of the merge commit are `merge`; one parent and the k commits behind it equal to the k pull request commits (author, time,
subject) are `rebase` (strong only when nothing remains and the pull request head is in the branch, else a hint); one parent and an author time equal to `merged_at` plus or minus 2 s are `squash`. `merge` and `squash` are strong only when the branch tip is the pull request head or behind it; a branch with commits after the head gets a hint.

JSON, additive in schema 2: `landed_via {kind merge|rebase|squash|unknown, basis ancestor|commit-map|pr-api|single-commit-map|none, strength strong|hint|none}` always; precedence: the tip is an
ancestor of the base (`merge`), a strong commit mapping (`rebase`), the pull request, one commit with an equal patch but no identity (`unknown`, a hint for a squash), else `unknown`. The
decision never depends on it. Per branch `rebase` (null where not computed: `--no-rebase-evidence`, an open pull request, a merged ancestor, a backup, a failure):
`state`, `strength`, `n`, `mapped`, `by_patch_id`, `by_identity`, `by_similarity`, `unmapped`, `ambiguous`, `contiguous`, `in_order`, `gap`, `runs`, `run_len`, `partial`, `content_ok`, `modified`,
`skipped{merges,empty}`, `window{commits,stop,cut,stopped_at}` (`stop`: `root`, `time`, `window`, `missing`: a shallow clone, the walk ends there), `api{state,reason,pr,merge_commit,merge_parents,k,
tip_matches_run,head_in_branch,kind,strength}`, `mapping[{commit,subject,main,position,method,value,identity,tz_differs,ambiguous,later}]` (`method`: `patch-id`, `identity`, `similarity`, `api`, `none`;
`value` in percent or null) and `remaining[{commit,subject,files,files_total}]` (at most 20 paths, escaped, untrusted text). `summary.rebase` counts the branches with `rebase-identity`; they are
counted in `review`, too. Text: one line per `rebase-identity` branch, `(N of them rebase-identity)` in the summary line, and `; landed via <kind> (<basis>, <strength>)` on a line whose
branch landed by a mapping or the pull request (not for an ancestor).

## Squash evidence in `tidy plan`

A squash merge writes the whole branch as one commit on the base (new ID, one parent, the author time of the merge, mostly `(#N)` in the subject). The squash stage is layer 1 of the
squash metrics (package 5a; specification `scratch/stw-tidy-squash/spec.md`, summary in `spikes/stw/tidy-similarity/README.md`): it reads what is cheap and exact, the merged pull
request of the branch and the subjects on the base. It proves nothing about the content (layer 2, package 5b, will) and decides nothing: **it never changes a decision or a proof**, and a
squash is never `delete`. Code: `src/tidy_squash.rs` (pure functions) and the stage in `src/tidy.rs`; tests `tests/it/tidy_squash.rs` and `tests/it/tidy_squash_plan.rs`; mutations `SQ11` to `SQ56`
(a gap in the numbers: the others belong to packages 5b and 5c).

For which branches: those whose proof is `pull-request-merged-branch-moved`, `upstream-gone`, `unmerged` or `similar-patches` (also where a worktree makes them `in-worktree`). Not for a deletion proof,
an open pull request, a backup branch, a branch without a commit of its own or `rebase-identity` (a strong rebase did not land as a squash): `squash` is null there. There is no flag; the stage
runs with `--offline` and without a token, too.

Pull request (`squash.pr`): from the list of pull requests the plan already holds (no request of its own). The pull request of the branch is the merged one with this head branch whose head is in
the repository itself (`head.repo.full_name` equals the slug of `origin`, compared without regard to case; a fork with the same branch name and a deleted fork do not count; with no slug nothing is
compared); where the plan names a merged pull request already (the proof `pull-request-merged-branch-moved`) that one, else the latest merge time, then the higher number. The existing proofs still
match by branch name alone (a fork pull request with the name of a branch still counts there; open question). `merge_commit_on_base` is true when `merge_commit_sha` is an ancestor of the base and has
exactly one parent; two parents are a real merge commit (false, `merge_commit_parents` 2). `head_is_tip`, `head_in_branch` (the head is the tip or an ancestor of it) and `commits_after_head`
(commits of the branch that are not in the head; only where the head is in the branch). A commit or head that is not in the clone, or a walk that fails on a missing object, leaves the answer null.
Where the proof is `pull-request-merged-branch-moved` and the merge commit is on the base the detail gets `; pull request merge commit <sha7> on <base>, N commit(s) after the pull request head`
(or `, the pull request head is not in the branch`). Without a usable pull request: `state` `not-read` and `reason` `offline`, `no-token`, `no-pull-request` (the list has none for the branch),
`rate-limited` or `request-failed`: no error, no change of the exit code.

Subject (`squash.subject`): the commits of the first-parent chain of the base (the same walk as the rebase index, no diff), non-merge, with an author time at most 7 days older than the oldest commit of
the branch. The candidate, strongest first: (1) the merge commit of the pull request where it is on the base (`basis` `merge-commit`, the sure one); (2) the commit whose subject ends on `(#N)` with
the number of the pull request (`pr-number`); (3) a commit whose title equals the title of a branch commit (`subject`). Title: the subject without a trailing `(#N)`, a leading `<digits>: ` (JBS issue)
and leading `[group]` words; a subject that is only such decoration is never compared. Of several, the oldest (ties by ID); `candidates` counts what the basis found. Further: `pr_number_in_subject`
(the number at the end of the candidate's subject, whatever it is), `subject_equal`, `subjects_total` (non-merge commits of the branch), `subjects_in_body` (branch subjects that are a line `* <subject>`
of the candidate's body), `pull_request_trailer` (the body has `Pull-request: .../pull/N` with the number of the pull request), `window{days,commits,cut}` (commits of the base read; `cut`: the walk
stopped at 50 000). The candidate subject is masked like all commit text. The class of a squash is not set here: the same title with other content ("revert and again") would match.

JSON, additive in schema 2: per branch `squash` null or `{pr, subject}`; `pr`: `state`, `reason`, `number`, `merged` (true where read), `merge_commit`, `merge_commit_in_clone`, `merge_commit_on_base`,
`merge_commit_parents`, `head_is_tip`, `head_in_branch`, `commits_after_head`; `subject`: `candidate`, `basis`, `candidate_subject`, `candidates`, `pr_number_in_subject`, `subject_equal`,
`subjects_total`, `subjects_in_body`, `pull_request_trailer`, `window`. No new key in `summary`. `landed_via` is unchanged (the pull request mapping of the rebase stage already gives `squash`).
Text without `--explain`: only the detail above is new. With `--explain`: `squash: pull request #N merged, merge commit <sha7> is on <base> with 1 parent(s); the head is in the branch, K commit(s)
after it`, or `squash: pull request not read (<reason>)`, then `squash: subject: candidate <sha7> '<subject>' (<basis>), N candidate(s), number #M in the subject, title equal to a branch commit: yes|no, S
of T branch subject(s) listed in its body, Pull-request trailer: yes|no` or `squash: subject: no commit on <base> with a title of one of the T branch commit(s)`, and a line where the walk was cut.

Deviations from the specification (5a), one by one: (1) the detail says `pull request merge commit` where the spec says `squash merge commit`: a one-parent merge commit is also a rebase of one commit
(this owner's repositories merge by rebase); (2) `squash.pr` has `merge_commit_in_clone` (spec: a reason text for a missing object) and the subject object has `basis`, `candidate_subject`, `candidates` and
`window`; (3) a pull request whose `head.repo` is empty counts as a fork where the slug is known; (4) no squash object for `rebase-identity` and for deletion proofs (spec silent); (5) the oldest of
several candidates is taken (the spec leaves the tie open). Not here: the content layer, the classes `squashed-patches` and `partial`, `summary.squashed`, the oracle (5b, 5c), `landed_via` from layer 2.

## Shallow clones

In a clone made with `git clone --depth N` (or `--shallow-since`, `--shallow-exclude`) the commits listed in `.git/shallow` are the boundary: for git they have no parents, whether the
parents are in the object database or not (another ref can hold them), and `git log` stops there. Before this rule `tidy plan` failed for the whole repository with
`An object with id ... could not be found` as soon as a branch reached a boundary commit whose parent is missing (`--depth 3` and `--depth 4` of a small repository; the ledger entry of
2026-11-18). Now the plan is made and nothing is concluded from a part of the history:

- The commits of `base..tip` are those of `git rev-list tip ^base` in this clone (`repo::range_commits`): a listed commit has no parents on either side. The walk of gix hides everything
  behind a listed commit on the `base` side where the objects are there, and so counts fewer commits than git; it is used only in a full clone. `unique_commits`, the upstream
  counts, the walks of the rebase and squash stages and the first-parent index of the rebase stage follow the same view.
- A commit of `base..tip` that is at the boundary (listed, a merge commit too, or with a parent that is missing) is `unreadable` (`shallow.unreadable_commits`): what is behind it is not
  known and a non-merge one has no patch to read. The equal-patches walk (`unmatched_commits`) does not read it, and as long as one exists the branch has no `equal-patches` proof; the
  similarity and rebase judgment of that branch's commits does not run (`similarity` and `rebase` are null). The squash stage is not changed by this rule: it runs where its own rule says
  (it reads heads and subjects, no patch).
- A commit of the base at the boundary is no partner. Where the walk over `tip..base` reached one while a commit of the branch still had no partner (`shallow.base_cut`), a partner may lie behind it
  (`shallow.without_partner` commits have none in the part that is there).
- Where either holds and no other proof applies (the proof would be `unmerged`), the branch is `review` with the proof `shallow-history` and the detail names the cause and `git fetch --unshallow`.
  Never `delete`, never `keep` with `unmerged`. A proof that needs no history (`merged-ancestor`, a merged pull request with exactly this head, `open-pull-request`, a backup name) is unchanged;
  `pull-request-merged-branch-moved` and `upstream-gone` stay `review` with their proof; a stage that finds evidence for a branch with only `base_cut` still replaces `shallow-history` by its own
  `review` proof (`similar-patches`, `rebase-identity`).
- JSON per branch `shallow`: null (a full clone, or nothing hidden) or `{unreadable_commits, without_partner, base_cut}`. `--explain` says `why: review: N of the M commit(s) not in <base> reach the shallow boundary: their patches are unknown, ...`
  or `... have a patch without a partner in the part of it this clone holds; the boundary hides the rest`. `similarity.skipped.shallow` is unchanged.
- Not covered: a partial clone (`--filter`; UNKNOWN); a shallow clone whose branch is merged into the base, with the
  commits on the base inside the clone, is judged as in a full clone.

## Explanation in `tidy plan --explain`

`stw tidy plan --explain` prints under each branch of the text plan (indented by four spaces) the evidence that led to the decision, or the evidence that is missing. It reads
only the data the plan already holds (classes, numbers, reasons): no diff, no request, no other read; it interprets nothing, prints no command and gives no advice. The decision, the
order of the branches and the JSON are the same with and without the flag, so with `--json` the flag changes nothing and is no error. Most of the data is in the JSON already (`similarity.commits[].evidence`,
`rebase.mapping` with `ambiguous`, `later` and `tz_differs`, `rebase.remaining`, `rebase.api.reason`, `rebase.api.head_in_branch`, `landed_via`); only in the text are the table of the pull
request mapping, the list `remaining after the pull request head` and the number of the merged pull request of a branch whose proof a later stage replaced (the counts of the
sentences are derived from `unique_commits` and the `similarity` and `rebase` objects). `tidy apply` has no such flag (exit 2). `tidy plan` has no `--long`; the flag does not
need one. Without the flag the text is byte for byte the text it was. The same plan gives the same text.

Lines, in this order (a section is left out where its stage did not run: `--no-similarity` leaves out `similarity:`, `--no-rebase-evidence` leaves out `mapping:`, `remaining:` and `pull request:`):

- `why: <decision>: <sentence>`: the proof that decided, or the proof that is missing (`keep: 2 of 4 commit(s) have no patch partner on origin/main`). Where `similar-patches` or
  `rebase-identity` took the place of `pull-request-merged-branch-moved` or `upstream-gone`, the sentence ends with `; pull request #N was merged and the branch moved on` or
  `; upstream <name> is gone` (the plan keeps the number of the merged pull request for this; the JSON does not carry it).
- `landed via: <kind> (<basis>, <strength>)` where the branch landed by a mapping, the pull request or an ancestor.
- `delete` branches get these two lines and nothing more (the proof is clear). The sentence counts only what the plan proves: `N commit(s) not in <base>` includes merge and empty commits,
  `P of the N commit(s) not in <base> have a patch without a partner there` counts only commits that have a patch.
- `similarity: class <class>, N commit(s) judged, A at or above 95 %, lowest x %, median y %, W without candidate`, then a line where the base was read from a shallow clone (`N commit(s) of the base were left out of the index (shallow boundary)`) or the candidate list of a commit was cut
  (`the list of candidates of a commit was cut at 100`), then for each commit below the threshold (at most 20, then
  `… N more commit(s)`): `<sha7> '<subject>' best candidate <sha7> <value> % (<reason>; without path <value> %)`, `no candidate` or `skipped (too-large)`, and below it up to 5 evidence
  lines `<sign> <path>: <line>` (a line of at most 120 characters, cut with `…`).
- `mapping: strength <s>, M of N commit(s) mapped (patch-id a, identity b, similarity c), contiguous|not contiguous (gap g), in order|not in order`, then for each mapped commit (at most 20,
  then `… N more`): `<sha7> -> <sha7> <method> <value> %`, with `(later version, hint only)` where the branch commit was committed after its commit on the base, and `(time zone differs)`; a commit whose key matches more than one free commit on the base and no neighbour decides is `<sha7> -> ambiguous (more than one commit on the base has the
  same author, time and subject)` and does not count as mapped; a
  `modified: a commit is at least 70 % and below 95 % like its commit on <base> (hint only)` line where a value is between the two thresholds.
- `remaining: N commit(s) without a commit on <base>` and one line per commit with its files (at most 20 commits, at most 20 paths over all of them).
- `pull request: PR mapping not read (<reason>)` with the reason of `rebase.api.reason`; where the pull request was read: `pull request: #N <kind> (<strength>), k commit(s), the head is in
  (or not in) the branch` and, for a rebase only, `, r commit(s) of the branch come after it` (only the commit mapping of a rebase lists them), then `hint only: ...` or `strong, but ...`
  with the reason why the pull request mapping is a hint (commits after the head; for a merge or a squash `the branch has commits after the pull request head`, derived from
  `head_in_branch` because the commits are not listed; the head not in the branch) and that it gives no proof of its own (only the commit mapping makes `rebase-identity`), the `pull request mapping:` table (method `api`) and
  `remaining after the pull request head`.
- `squash: ...` (where the branch has a `squash` object; no flag leaves it out): the pull request line (`squash: pull request #N merged, merge commit <sha7> is on|is not on <base>|is not in the clone|has N parent(s), not one`,
  `; the head is the tip of the branch|is in the branch, K commit(s) after it|is not in the branch|could not be checked`, or `squash: pull request not read (<reason>)`), the subject line and, where the walk over the base was cut, one line saying so. Layer 1 only: no proof.

All text from a commit (subjects, paths, lines) is escaped (control, invisible and direction characters; bytes that are not UTF-8 as `\xNN`), and so is the name of the upstream.

## Oracle of `tidy plan`

The oracle compares `tidy plan` with real repositories and with what GitHub says (package 4; the numbers are in `spikes/stw/tidy-similarity/README.md`). It is read-only and starts nothing but `git`, `gh` and `cargo`:

```
ORACLE_STW=target/release/stw bash tests/oracle/tidy_oracle.sh /abs/path/outside/the/git/tree
```

The directory (absolute, without `..`, outside a Git tree or ignored by it) takes the clones, the raw GitHub data (`gh api`, stops below 500 remaining requests), the results and the report `bericht.md`; nothing else is written and
nothing is deleted unless `ORACLE_CLEAN=1`. Variables: `ORACLE_HUNK` (slug or path of a repository with many commits, default `flix/flix`), `ORACLE_REBASE` (slugs with merged pull requests, default the three repositories
of the owner), `ORACLE_RAWDIR` (same guards as the output directory), `ORACLE_LOCK` (lock file for cargo, warns where its directory is missing), `ORACLE_PRS`, `ORACLE_HUNK_PRS`, `ORACLE_HUNK_COMMITS`, `ORACLE_MATRIX_COMMITS`. The report has the hunk oracle (the keys of gix against `git diff -U0 --no-renames`), the matrix
(the same commits against every `diff.algorithm`, with and without the indent heuristic), the rebase oracle (every merged pull request as a branch: strong `landed_via` rebase onto the commits GitHub wrote, or `equal-patches`;
a pull request into another branch is named `stacked`) and the cost of `stw tidy plan` (seconds, peak memory). The same tests run alone with `STW_ORACLE_HUNK_REPO`, `STW_ORACLE_REBASE_REPOS` and `STW_ORACLE_OUT` set:
`cargo test --test it oracle_tidy -- --ignored --nocapture` (they skip with a message without the variable; a finding is a result, `STW_ORACLE_STRICT=1` makes it a failure). The comparisons are pure functions in
`src/tidy_oracle.rs` with ordinary tests in `tests/it/tidy_oracle.rs`; mutations `OR1` to `OR35`. No new deviation from the Go behavior.

## tidy apply and tidy restore

`tidy apply` acts on a fresh plan, or on `--plan FILE` (the output of `tidy plan --json`). With `--plan`, only the
`delete` entries and the `keep`/`in-worktree` entries that name a removable `worktree` are candidates, and each
one is verified again against a fresh plan before anything happens: the branch must still point at the planned
commit and the proof must still hold (a proof that needs GitHub holds only without `--offline`); for a worktree
entry the branch must still be checked out in the planned worktree path. A branch still held by a dead
registration is skipped (the file has no field for registrations, so `apply --plan` does not remove them).
The backup age is judged by the `--backup-days` of the `apply` run (default 7), not the one the plan was made
with. Otherwise the entry is skipped with the reason. `review` and `keep`
branches, the base branch, branches checked out in a worktree that stays, and remote state are never touched.
`tidy plan` shows the plan and changes nothing; `tidy apply` executes it, with no confirmation flag. The safety is the
manifest written first, the fresh check of every entry and the proof rules above.

Order per repository: verify, write the manifest (fsync of file and directory), remove dead registrations, remove
worktrees, delete branches (the ref is deleted only if it still has the planned commit). If the manifest cannot
be written nothing is deleted (exit 1). A failure in one branch does not stop the others (exit 1).

Worktree places: `<repo>/.worktrees/<name>` is the normal place. A worktree anywhere else is marked `outside
.worktrees/` in the text plan and `outside_worktrees_dir: true` in JSON. It is removed on the same proof as any other
(below), the mark only tells where it was. The text plan lists every kept branch whose worktree is removable, also
without `--all`.

Worktrees: a linked worktree whose branch would be `delete` is removed only if it is not the main worktree, not
locked, has no staged, unstaged or untracked file and no running operation, contains no checked-out submodule,
its `.git` file points at its registration, no other worktree (or any other git repository, registered or not)
lies inside it (an agent's worktree under `.worktrees/` of a worktree would go with it), and its tip is in the base branch or its upstream (nothing unpushed).
A registration whose checkout path contains `..` (what `git worktree add --relative-paths` leaves, or a hand-made
`gitdir`) is never followed: it is not removed, not judged clean or dead, and its branch is not deleted. It is
reported once, as `skipped`, with its path shown without `..` (display only: nothing is read, tested or removed
through the normalized path, because `..` after a symbolic link names another directory than the lexical one).
Only the worktree directory and its registration are removed (ignored files in it go with the directory, as
`status` does not count them as changes). Otherwise
worktree and branch stay, with the reason. Unlocked registrations whose directory is gone are removed and locked ones are reported, but only when `apply`
uses a live plan: a run with `--plan FILE` leaves registrations alone (the file has no field for them). A removed worktree is not recorded for `restore` (only the branch is); its path is in the manifest.

`.worktrees/`: every `tidy apply` makes sure the repository's `info/exclude` (in the common git directory, found
through gix) lists `.worktrees/`, so agent worktrees under `<repo>/.worktrees/` never show up as untracked. The
line is appended once (the file is opened for append; bytes already there, CRLF included, stay as they are) (`.worktrees/`, `/.worktrees/`, `.worktrees` and `/.worktrees` count as present), existing content
is kept, the `.gitignore` is never touched. `--json`: per repository `exclude` {path, action (`added`, `present`,
`failed`), reason}, null for a repository without a work tree.

Manifest: JSON Lines, one file per run, `<DIR>/<run>.jsonl`, written before the first deletion. `DIR` is
`--manifest-dir`, else `~/Library/Application Support/stw/manifests` (macOS) or
`$XDG_DATA_HOME/stw/manifests` / `~/.local/share/stw/manifests`. The run ID is `<ms>-<pid>-<n>` and sorts by time.
Each line: `manifest_version` (1), `run`, `repo` (path), `branch`, `sha`, `proof`, `time` (seconds), `worktree`
(path or null).

`tidy restore` reads the newest run (or `--run ID`; `--branch NAME` limits it) and creates each branch at its
`sha`. It is idempotent: a branch that exists at the `sha` is `already-present`; one at another commit is
`not-overwritten` (never moved); a commit that is gone from the repository is `missing-object`. Exit 1 unless
every entry is restored or present. A run that was killed after the manifest and before the deletions restores
as `already-present`. The reflog entry (`stw: tidy restore <run>`) uses the identity rules below. Left behind by
a delete: `branch.<name>.*` configuration (upstream) stays, so a restored branch keeps its upstream.

### tidy history

`stw tidy history [--limit N] [--manifest-dir DIR] [--json]` is the port of the Go `tidy history` (read-only; Go lists the stored
contract-3 journals). Here the journals are the manifests of `tidy apply` above, and their format is not changed: `tidy restore` reads
the same files. One row per manifest, newest run first (run IDs sort by time, so the first row is the run `tidy restore` takes
without `--run`): `run`, `time` (modification time of the file, `YYYY-MM-DDTHH:MM:SSZ`), `mode` (`local-refs`, `unknown` when not read),
`deleted` (entries of the manifest: the branches the run recorded before it deleted them; a run killed before the deletions
counts too), `restorable` (of those, how many `tidy restore` could create now: the repository opens, the branch does not exist, the commit
is in it) and `status`: `complete`, `incomplete` (not checked because of a bound), `unreadable` (not JSON or not a manifest entry; listed with
0 counts). The entries are data: `repo` must be an absolute path, the branch a valid `refs/heads/` name, `sha` 40 lower-case hex digits,
otherwise the entry is not restorable. Files that are no manifests (other extension, a directory, a symbolic link, a hidden
or unusual name) are not listed. A directory that does not exist is an empty history. Nothing is written and no repository is changed; no
`git`, `gh` or `ssh` is started.

Bounds, as in Go, each one visible: at most 4096 manifests are scanned (`scan_truncated`), `--limit N` (1 to 1024, default 256; a value
outside it, a repeated flag or an unknown flag is a usage error, exit 2) rows are shown, at most 65536 entries are checked against the
repositories, and the check stops after 30 seconds. A bound that was reached is a marker `{unit, shown, total}` with `unit` `journals`,
`objects` or `checked-journals` (text: `truncated: <unit> <shown> of <total>`). `--json`: `schema_version` 2, `command` `tidy-history`,
`read_only` true, `manifest_dir`, `engine`, `status` (`ok`), `total`, `scan_truncated`, `entries[{run, time, mode, deleted, restorable,
status}]`, `truncated[]`. Text: `N journal(s) in <dir>, newest first`, one line per row, the markers; text from outside is escaped.

Deviations from Go: (1) the journals are the `tidy apply` manifests (one JSON Lines file per run, outside the repository), not the plan and
journal files of the contract-3 state directory; `--contract`, `--state-dir`, `PATH`, `tidy restore --list` and the contract-4
(administrative registration prune) history are not ported, `--manifest-dir` is the state directory; (2) the fields `plan_id`, `retained`,
`recovery_refs`, `decision_kinds` and `run_mode` have no counterpart (a manifest has no plan, no recovery refs, no reviewer decisions and no run
mode); `run` is the run ID; (3) `status` cannot say `incomplete` for a killed run, since the manifest is written before the first deletion
and has no completion mark; (4) the order is by run ID (Go: modification time); (5) a missing directory is an empty history (Go: an error);
(6) the unit of the entry bound is named `objects` as in Go but counts manifest entries; the 30 second budget and the bounds are the Go values.

Output: `--json` has `command` `tidy-apply` / `tidy-restore`, `run`, per branch `action`
(`deleted`, `skipped`, `failed`) and `reason`, `manifest` (path), worktrees and registrations.
It carries state and results only, no commands. Limits: a file created in a worktree after the check and before
the removal is lost with it; the age rule for backups is only as good as the commit date (above).

## The op log: `stw oplog timeline | evolog | undo-plan | undo` (new)

New behavior without a Go counterpart: the three read-only commands over the logs that the accepted spikes
`spikes/stw/oplog-reflog`, `oplog-hooks` and `oplog-refhook` write. The format is the contract in
`docs/oplog-format.md` (reviewed CLEAR); this package builds the readers only. Not part of it, each its own package:
the hook install (stw's own `refs.log` lines and the rotation are the next section, `stw oplog undo` the section after, `stw oplog hooks` the one after that). Nothing is written by these commands (tests compare the repository byte for byte before and
after), no hook is installed and no `git` program runs. `--root PATH` and `--repo NAME` as for `stw prove`; `--json`.

- `stw oplog timeline [--ref GLOB] [--remotes] [--all]`: the reflogs (`HEAD` of the main and the linked worktrees,
  `refs/heads`, `refs/stash`, `refs/stw`; `refs/remotes` with `--remotes`, every other ref with `--all`) and `refs.log`
  as one list sorted by time, then by file order. A reflog row and the `refs.log` line of the same ref and new value are
  one entry (origin `reflog`, flag `covered`); a line no reflog row has is its own entry (origin `hook` or `stw`, flag
  `hook-only`; `old-unknown` for a delete without a marker). Markers are joined as in the spec (3.3). A reflog row without
  a line is `uncovered` (the hook was installed then), `pre-install`, `hook-absent` (after an uninstall),
  `unknown-window` (no `events-refhook.log`) or `not-expected` (`refs/remotes` while `remotes=off`; `refs/bisect/*`,
  `refs/worktree/*`, `refs/rewritten/*`). A `HEAD` row is covered by a line of any `refs/heads/*` ref with the same new
  value. Groups are `suspected` only (same text stem, within one second). The gaps list is always printed: `backend`,
  `config`, `truncated`, `empty-reflog`, `unreadable`, `bad`, `capture-gap`, `rotated`, `uncovered`, `pre-install`,
  `hook-absent`, `no-install-record`, `still-open`, `suspected`. JSON: `schema_version` 1, `command` `oplog timeline`,
  `entries[]`, `other_lines[]` (a well-formed line with an unknown context: kept, not in the timeline), `gaps[]`.
- `stw oplog evolog REV [--limit N]`: the chain of recorded rewrites (`rewrites.log`, both generations) from a commit
  back, with the semantics of the spike: first of equal pairs wins, self pairs ignored, `merge=N`, `split=N`, `cycle`,
  `seen-above`, `limit`; an unknown kind is shown. The reflog of the branch is compared with the pairs (`commit (amend)`
  and `rebase ... (finish)` entries): `uncovered-rewrite`, `pre-install`, `hook-absent`, `unknown-window`.
- `stw oplog undo-plan REF [--to OLD | --steps N] [--keep]`: the checks of the spike: the ref exists and is no symbolic ref, a local
  branch (other refs are previewed and marked not undoable), not checked out in any worktree (hint `--keep` or `git reset --keep`; with `--keep` see the section on `stw oplog undo`),
  no registration without a readable git dir, no running operation (`rebase-merge`, `rebase-apply`, `MERGE_HEAD`,
  `CHERRY_PICK_HEAD`, `REVERT_HEAD`, `BISECT_LOG`, `sequencer` in the git dirs), the ref still equals the newest reflog
  value, the target is not the creation entry and is a commit; notes: ancestry and the commits that only the reflog would
  keep. Exit 1 when a check is refused. When all pass it prints the `stw oplog undo` command that applies it
  (`--json`: new key `apply`, null unless executable).

Rules shared with the spec: a log that is a symbolic link, not a regular file, in a symbolic `stw` or `stw/oplog`
directory, or over 64 MiB is not read (`unreadable`); a line is skipped and counted as `bad` when it is over 4 KB,
holds a NUL byte, is no UTF-8, has too few fields, a wrong hash, time or name, or has a field 7 or later that is not
`key=value` (a glued line); an empty line is skipped and counts as nothing (a lost lock race of two writers can leave one; a line of white space is still `bad`); all text from logs is escaped (`escape_untrusted`). Deviations from the spikes and the
spec, one by one: (1) reflogs are read from the files directly, not through the gix reflog iterator (same lines; the
reftable backend is reported as `backend` and no reflog row is shown); (2) a `refs.log` line that a reflog row covers is
not listed twice (the spec says the two sources are merged; the spike had no hook rows); (3) the timeline has no
`--since`, no `expiring`, `tags` and `deleted-branch` gaps and no `no-reflog` gap of the reflog spike (later); (4) JSON of
the spikes is not reproduced field for field: `command`, `engine` and `schema_version` 1 as in the other stw documents;
(5) `undo-plan` counts the commits that only the reflog would keep with a gix walk (at most 10 000) against every other ref
and `HEAD`, not with `git rev-list`; (6) the evolog counts as capture gaps only the post-rewrite kinds (`amend`,
`rebase`, `other`) in `errors.log`; the spike counted every line of the file. UNKNOWN: SHA-256 repositories (spec Q9; the
parser accepts 64 digits, nothing ran), macOS, and a reflog read while another process writes it.

### stw writes its own ref updates (package 2)

Hooks never see gix writes, so stw appends them itself to the same `refs.log` (six TAB fields, context `stw`, names
`%XX`-encoded and cut at 200 bytes with the djb2 suffix, spec 3). Logged: `stw sync` (recovery ref create and prune,
branch fast-forward and put-back; the branch, not `HEAD`), `stw tidy apply` (branch delete), `stw tidy restore` (branch
create). Skipped: `old == new`, names other than `HEAD` and `refs/*`, `refs/remotes/**` without `remotes=on`. The line is
written even with `core.logAllRefUpdates=false`. A write never fails the command: a failure is a `write-failed` gap line
and one notice on stderr. Rotation (spec 6): after an append, a log above 10 MiB is renamed to `<name>.1` (one old
generation, replacing an older one) under `try_lock` on `rotate.lock`; a held lock means no rotation and no waiting; a
`rotated` gap line names the file. The timeline reports `truncated` for a file that rotated twice (ledger nit N2).
Open questions with recommendations: spec section 10, Q13 to Q18. Deviations from the spec text: the `rotated` gap line
carries the file name instead of `-` (Q15); `rust-version` 1.89 (Q18).

### `stw oplog undo` and the backup refs (package 3)

New behavior without a Go counterpart; the owner accepts it live (recipe: `scratch/abnahme-rezepte/rezept-stw-oplog-undo.md`).

`stw oplog undo REF --to OLD --expect NEW --yes` moves one local branch back to an older value of its reflog. REF is a
branch name or a full ref name; `--to` is a lower-case hex prefix of an old reflog value (as in `undo-plan`; no `--steps`);
`--expect` is the full current value; `--yes` is required. It runs the checks of `undo-plan` (the same function), then:
(1) takes a run number: the larger of the counter ref `refs/stw/backup-counter` (a blob `<run> <pid>-<nanos>`, compare-and-swap, up to 5
tries against other writers) and the highest run that exists, plus one (a run number is never reused; a counter that was deleted or lowered by hand does not hand out a taken number), (2) creates `refs/stw/backup/<run>/1` on the current value
(create-only: an existing ref of that name is an error, whatever its value), (3) removes the backup refs of all but the 10
highest runs (the counter and the run just taken are never removed; names that are not `<run>/<k>` with canonical positive decimals, that is no sign and no leading zero, are left alone: `+1/1` and `01/1` are no runs), (4) sets
the branch to OLD only if it still holds NEW (`MustExistAndMatch`, no-deref); a changed ref leaves the backup and the
branch as the other writer left it. Reflog entries for the backup ref and the branch (message `stw oplog undo`) are written
also with `core.logAllRefUpdates=false`. `refs.log` lines (context `stw`) are written for the counter, the backup ref, each
pruned ref and the branch update; a failed append is a notice and a `write-failed` gap, the undo stands.
Refused with exit 1 and nothing written: a failed check of `undo-plan` (a branch checked out in a worktree gets the hint
`--keep`), `--expect` not equal to the current value.
Usage errors (exit 2): no `--yes`, `--expect` not a full lower-case hash, a ref that is no local branch.
Only the ref moves: index, working tree, other worktrees and pushes are not covered.

`--keep` (package 5; recipe: `scratch/abnahme-rezepte/rezept-stw-oplog-undo-keep.md`; rules and differences: `docs/oplog-format.md`, 8a):
`stw oplog undo REF --to OLD --expect NEW --keep --yes` (and `undo-plan ... --keep`) undoes the branch that the current worktree has
checked out. stw runs `git reset --keep OLD` through the runner (hooks off, no `--hard`), after the usual backup ref, and writes the
`refs.log` line. Refused (exit 1, nothing written): the branch is not the `HEAD` branch of this worktree (detached, or another branch),
another worktree holds it, a staged or unstaged change or an unmerged path (untracked files stay and do not count), a running merge or
other operation, a gitlink that differs between the two values, `core.sparseCheckout`, no usable git or git older than 2.36, a target
that is not 40 hex digits. A git failure after the backup (an untracked file in the way, a `filter.*` driver) is exit 1; the branch is
unchanged and the backup stays; a git that was killed or timed out (120 s) may have written part of the work tree, and the message says so. Also refused: a path the target adds where an untracked or ignored file lies (the ignored ones are not protected by git), an intent-to-add, assume-unchanged or skip-worktree entry. `undo-plan --keep` starts `git --version`. `--json` gains `keep` (undo and undo-plan) and, for `undo`, `via` (`git`, else null): additive, `schema_version` stays 1. Mutations `OK1` to `OK50`.
Output: backup ref and the way back as a command (`stw oplog undo REF --to <old> --expect <new> --yes`); in the text output (here and in `undo-plan`: `Apply with:`) a ref name with shell characters is written in POSIX single quotes, other names as they are; the JSON fields `way_back` and `apply` keep the raw name. `--json`
(`schema_version` 1, `command` `oplog undo`): `ref`, `executed`, `previous`, `current`, `backup_ref`, `run`, `pruned`,
`way_back`, `notices`, and with package 5 `keep`, `via` (additive, `schema_version` stays 1); a refusal prints the `undo-plan` document with `command` `oplog undo` and `executed` false.
Deviations from the spec text: none in behavior; the spec sections 8 and Q12 now say the reflog mechanism works with
gix 0.90, and Q7 (`<run>/<k>`) is decided by the owner (2026-11-13). UNKNOWN: macOS, ext4,
SHA-256 repositories, a concurrent `git` writing the same refs.

### `stw oplog hooks status | install | uninstall | print` (package 4)

New behavior without a Go counterpart; the owner accepts it live (recipe: `scratch/abnahme-rezepte/rezept-stw-oplog-hooks-install.md`).
Rules and deviations: `docs/oplog-format.md`, section 11.

`stw oplog hooks install --yes` writes the two hooks of the spikes (`post-rewrite` -> `rewrites.log`, `reference-transaction` ->
`refs.log`; the script texts are the spike texts, `src/oplog/hooks/*.sh`) into the hooks directory of one repository, `uninstall --yes` removes
them, `status` shows where they are, `print NAME` prints a script (for a chain by hand). Opt-in per repository: nothing else installs a hook.
Without `--yes` the plan is printed, nothing is written, exit 1.
- `core.hooksPath` (any config source, `GIT_CONFIG_COUNT`, `GIT_CONFIG_PARAMETERS`, also empty) is read, never written: when it is
  set, `install` refuses and names the place the hooks would belong. No git config is written.
- A hook of the same name that is not ours is never overwritten or rebuilt: refusal with a chain proposal. One refusal refuses both.
- The directory is `<git-common-dir>/hooks` (linked worktrees share it and the logs); a symbolic link there is refused; a submodule is
  `--repo NAME`. `uninstall` removes only a regular file whose line 2 is our header; the logs stay.
- `install` appends `install` to `events.log` and `events-refhook.log` (the window the timeline and `evolog` read), `uninstall` appends `uninstall`.
- No git program runs (`PATH=/nonexistent` in the tests); the installed hooks are tested end to end with a real `git` (amend, tag, branch delete).
`--json`: `schema_version` 1, `command` `oplog hooks <sub>`. Mutations `OH1` to `OH48`.

The hint in `stw status` (new behavior without a Go counterpart; the owner accepts it live, recipe `scratch/abnahme-rezepte/rezept-stw-status-hook-hint.md`): under each repository that was read, one line when at least one of the two hooks is not ours, nothing when both are ours and nothing blocks them. `op log hooks: not installed (post-rewrite, reference-transaction); install with: stw oplog hooks install` names the hooks that are missing. Where `install` would refuse (`core.hooksPath` set, a hook that is not ours, a hooks directory that is a symbolic link) the line is `op log hooks: not active, install is refused here (<reason>); see: stw oplog hooks status`, without the install command. A set `core.hooksPath` or a linked hooks directory counts as blocked even when both files are ours, because git does not run them there. Only a hint: status installs nothing and writes nothing, starts no program, the exit code does not change, and a repository that could not be read has no line. It is the one place where stw prints a command (owner decision 2026-11-13); the JSON has the state only. JSON (additive, `schema_version` stays 2; also in `status PATH`, whose one-line text has no hint line): per repository `oplog_hooks`, null for a repository that could not be read, else `{state, reason, missing}`: `state` `installed`, `missing` or `blocked`; `reason` null, `hooks-path`, `foreign-hook` or `hooks-dir-symlink`; `missing` the names of the hooks that are not ours. There is no switch to hide the hint (no other status hint has one). Mutations `HH1` to `HH11`.
