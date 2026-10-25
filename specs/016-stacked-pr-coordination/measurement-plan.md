# Measurement Plan: OQ-WRITES and OQ-NATIVE

**Status**: PLAN MERGED (pull request 271, `f4558cfb`); the update that records
the owner release of gate G3 is pull request 272 and is UNDER REVIEW; its
reviewed head becomes the G1 head once it is merged. Document only. No
measurement has been run and no request has been sent to any forge. This plan
authorizes nothing by itself: section 2 lists the gates that must all be open
before any run. Every GitHub behaviour below is a HYPOTHESIS to be tested,
never a finding.

## 1. Owner decisions this plan rests on (restated, dated 2026-10-25)

- OQ-WRITES: measure first in a separately authorized disposable forge fixture,
  then review a small closed write allowlist; comments deferred; no GraphQL or
  arbitrary endpoint fallback in stw, with the single exception of the fixed
  Ready mutation decided below (the exception is also stated at `research.md`
  lines 139 and 192, `contracts/owner-decisions.md` line 212,
  `contracts/forge-effects.md` line 101 and `tasks.md` line 50, T021).
- OQ-NATIVE: Git publication first; measure native link/unstack separately;
  native jj publication stays `capability-unavailable`, no Git fallback
  (`research.md` line 140). In this plan: native stacks are measured, jj is
  BLOCKED and no jj command touches the fixture.
- Fixture authorization: the owner created ONE private repository
  `wstein/stw-forge-fixture` (README only, default branch `main`, no rulesets)
  for these measurements only. Write measurements in it are allowed ONLY after
  an Opus review of this plan reports CLEAR for the exact plan head. Deletion of
  the fixture, every setting change and the owner-only probes M1 to M3 (section
  7) stay with the owner.
- Owner decision of 2026-10-25, gate G3 RELEASED: running the installed
  convenience commands `gh pr ready` and `gh stack` (link / unstack) as
  measurement acts is allowed in `wstein/stw-forge-fixture` ONLY and nowhere
  else. They may send GraphQL mutations or extension requests internally; the
  "no GraphQL" and "no extension execution" rules bind the stw implementation,
  not these measurements. W5b, O-W5 and N1 to N5 are therefore no longer held
  back by an open owner item.
- Owner decision of 2026-10-25 on W5a: if REST cannot set Ready, stw may use
  exactly ONE fixed GraphQL mutation, `markPullRequestReadyForReview`, with the
  pull request node id as its only variable. It is sent only through one fixed
  query string, as its own effect class, validated like every other effect; no
  other GraphQL is allowed in stw. The mutation name and its input
  (`MarkPullRequestReadyForReviewInput`, required field `pullRequestId: ID!`)
  were checked read-only against the GitHub GraphQL schema by `gh api graphql`
  introspection on 2026-10-25. This decision is conditional on W5a CONFIRMING
  that REST cannot set Ready; if W5a refutes that, the REST form is used and
  no GraphQL is admitted. The decision allows the mutation in the stw
  implementation; it does not run it as a measurement here.

These decisions are recorded here so that a reader needs no session context.
The fixture authorization is not recorded in another repository file; the
owner's statement of 2026-10-25 is the source.

## 2. Run gates (all must hold; the run records each)

- G1: an independent Opus review of the exact plan head reports CLEAR; record
  that head SHA.
- G2: the owner gives the go for a run of this plan.
- G3: for W5b, O-W5 and N1 to N5 only; RELEASED by the owner on 2026-10-25
  (section 1), for the fixture only. G3 no longer holds back any experiment;
  G1, G2 and G4 still apply. The run order is unchanged.
- G4: the target guard of section 3 passes before every write.

## 3. Scope, run rules and guards

Measures the effects that `research.md` line 29 and `contracts/forge-effects.md`
lines 71-77 and 144-146 leave unmeasured: `create-pr`, `edit-body`, `retarget`,
`ready`, absent-ref creation, `native-link` and `native-unstack`. Comments are
deferred and not measured (`spec.md` lines 96-97). Results are evidence for a
later reviewed write allowlist (`tasks.md` line 50), never a contract change.

1. One operator, one run id `RUN` (UTC `YYYYMMDDTHHMMSSZ`), strictly sequential.
2. API guard before EVERY write: the repository argument is the literal
   `wstein/stw-forge-fixture`; `gh api repos/wstein/stw-forge-fixture` shows
   `private == true` and the expected `full_name`. Mismatch aborts.
3. Convenience-command guard: every `gh pr ...` command carries
   `-R wstein/stw-forge-fixture`; every `gh stack ...` command carries
   `GH_REPO=wstein/stw-forge-fixture` (it has no `-R`, see
   `contracts/forge-effects.md` line 112); both run from an empty scratch
   directory outside any workspace checkout.
4. Git guard before EVERY push: in the scratch clone, `git remote get-url
   --push REMOTE` equals the fixture HTTPS URL, and the read-only
   `git config --get-regexp '^url\..*insteadof$'` and the `pushinsteadof`
   variant print nothing; any hit aborts. Pushes use per-command `git -c ...`
   settings only. No `git config` write, no `gh auth setup-git`. A throwaway
   identity is given through `GIT_AUTHOR_*` / `GIT_COMMITTER_*` of one command.
5. Numeric-argument guard for `gh stack link`: its help says a numeric argument
   that is not an existing PR is treated as a branch name, and branch arguments
   are pushed (N0 re-records this). Before each link, read back every numeric
   argument as an existing open PR of the fixture
   (`gh api repos/wstein/stw-forge-fixture/pulls/N`, `state == open`); any
   miss aborts. A stack-number first argument is read back the same way
   against the recorded stack number.
6. Writes use `gh api --method M PATH -f/-F FIELD=VALUE` so that method, path
   and body fields are explicit. Convenience commands are run only where an
   experiment says so.
7. Every branch is named `m016/RUN/NAME`; nothing else is created.
8. Before and after every write, read the observation set of section 5 for each
   affected PR and ref. After an unknown outcome (timeout, lost response) do
   not repeat the write: read, and record `unknown` or the proven outcome.
9. Budget: at most 150 requests and 20 minutes. Read `gh api rate_limit` before
   and after. The estimate in section 4 is a plan figure, not a measurement.
10. Trace and secret handling (no trace reaches the transcript or terminal):
    - `GH_DEBUG`, `GIT_TRACE`, `GIT_TRACE_CURL` and `GIT_CURL_VERBOSE` are
      unset in every command except those named below.
    - A traced command runs as `GH_DEBUG=api cmd 2>TRACEFILE` (or
      `GIT_TRACE=1 git ... 2>TRACEFILE`) with `umask 077`, TRACEFILE under the
      session scratchpad (mode 0600), and nothing from it is printed.
    - Before any excerpt is read or copied, scan TRACEFILE. The scan prints
      nothing but a verdict (`grep -q` / `grep -c`, never a plain `grep`, so a
      matching line cannot reach the transcript). It aborts the run, and the
      file is neither read nor committed, on any of: a request header line
      (`^> Authorization:`) that is not fully masked; `gh[opsu]_` followed by 20
      or more token characters; `github_pat_`. Rules that match benign trace
      content are deliberately absent: a 40-hex rule (commit SHAs, tree ids and
      ETags fill every trace) and a bare `Authorization:` or `Basic ` text
      match (response bodies, such as PR text, can contain those words). Tested
      2026-10-25 on real read-only traces (gh 2.102.0 `GH_DEBUG=api` for a pull
      request and a commit list, `GIT_TRACE=1` for `git ls-remote`), counts
      only: one request header line in each gh trace (none in the git trace), masked
      with the block character;
      zero unmasked header lines; zero token-pattern hits; 40-hex strings
      present in 7, 51 and 0 lines (which is why that rule is dropped). A false
      positive aborts (the safe direction); the scan is re-run against a fresh
      read-only trace as part of P0. Only the scanned file may be excerpted, and
      only method, path, operation name and body field names.
    - `GIT_TRACE_CURL` and `GIT_CURL_VERBOSE` are forbidden outright.

## 4. Object inventory and request estimate

Branches created (9), all by the W1-proven push path from a scratch clone:
`w1`, `w2d`, `w2e`, `w2o`, `w4`, `w5`, `n1`, `n2`, `n3` (W1 also tries `w1b`, which must stay
absent). PRs created (7, plus rejected attempts; `w2e` must create none and `w2o` creates one only if the unknown field is ignored): `w1` (Draft, W2a; reused by
W3), `w2d` (created with `draft=false`, W2d, the non-Draft PR for W4b), `w4`
(Draft), `w5` (Draft), `n1` (base `main`), `n2` (base `m016/RUN/n1`), `n3`
(base `m016/RUN/n2`, created but only linked in N3). Estimated requests: P0 12,
W1 15, W2 24, W3 14, W4 16, W5 12, N0 to N5 40: about 135, below the cap of 150.
If the running count would pass 150, stop.

## 5. Observation set (recorded before and after each write)

Per PR, `gh api repos/wstein/stw-forge-fixture/pulls/N` fields: `number`,
`state`, `draft`, `merged`, `title`, body length and a digest of the body,
`base.ref`, `base.sha`, `head.ref`, `head.sha`, `head.repo.full_name`,
`mergeable`, `mergeable_state`, `rebaseable`, `merge_commit_sha`, `auto_merge`,
`user.login`, `updated_at`. `rebaseable` and `mergeable` may be null while
GitHub computes them: re-read up to 5 times, 2 s apart, and record every value.
A missing field is recorded as absent, not as null.

Per ref: `gh api repos/wstein/stw-forge-fixture/git/ref/heads/NAME` (status and
`object.sha`) and `git ls-remote` of the fixture URL for `refs/heads/m016/RUN/*`.
Per request: HTTP status, the `ETag` and `x-ratelimit-remaining` headers (via
`gh api -i`). Per command: argv, exit code, stdout and stderr bounded to 8 KiB
(trace files excluded, see rule 10). Preflight P0 also records, read only:
`allow_rebase_merge`, `allow_merge_commit`, `allow_squash_merge`,
`delete_branch_on_merge`, the rulesets list, branch protection of `main`, the
viewer's permission and the versions of `gh`, `git` and the `gh stack` extension.

## 6. Abort, stop and cleanup

Two terms. A STOP ends the experiments: it happens on an outcome that is neither
confirm, refute nor an allowed observation, on a failed gate, or on any abort
trigger. After a stop the run executes no experiment step. CLEANUP is the only
write that may follow a stop, and only the steps below.

Abort triggers: the API or Git guard fails; the fixture is not private or has
rulesets or protection the preflight did not record; a response names another
repository (fork, transfer, redirect); an unexpected 401 or 403; rate limit
remaining below 100; the budget is spent; a trace scan hit.

Cleanup after a stop, limited to objects whose names carry `m016/RUN/`:

1. If the API guard still passes: for a stack the run created and whose stack
   number is recorded, `gh stack unstack STACK_NUMBER` (explicit number, never
   without an argument, never `--local`); this needs G3 (released, section 1)
   and is skipped if the number is unknown.
2. Close the PRs the run opened (`PATCH state=closed`).
3. Leave the `m016/RUN/*` branches; list them and every open PR or stack that
   cleanup could not close in the evidence file for the owner.

If the guard itself failed, or a trace scan hit, cleanup writes NOTHING and the
open-object list goes to the owner. A stop in the middle of N1 to N5 follows the
same steps: unstack first (when a stack exists and its number is known), then
close. Deleting branches, the fixture or any setting is the owner's. The run
never touches `main`, tags, settings, rulesets, secrets, webhooks or another
repository, and never merges.

## 7. OQ-WRITES experiments

Each hypothesis states its confirming and its refuting outcome. An outcome in
neither list stops the run (section 6). "Observations" (O-ids) are recorded
without confirm or refute, with result `recorded`, and never stop the run.

### P0 Preflight (read only)

Run the rule 10 scan against a fresh read-only `GH_DEBUG=api` trace (verdict
only). Record section 5 preflight data, `gh api user` login and the SHA of `main`.
Confirms: repository private, empty rulesets and protection, `main` readable,
write permission. Otherwise abort. The agent changes no setting.

### W1 Ref creation and expected-absence lease

The vector under test (one line, for an absent ref, empty expected value):

```text
git push --porcelain --force-with-lease=refs/heads/m016/RUN/w1: REMOTE FULL_SHA:refs/heads/m016/RUN/w1
```

Hypothesis W1a: this creates the absent ref. Confirm: exit 0, one porcelain line
flagged new, the ref present at the pushed SHA. Refute: rejection, or a ref at
another SHA.
Hypothesis W1b: with the ref now present at SHA X, pushing a different commit
Y with the same empty-expectation lease is rejected and the remote stays at X.
Confirm: non-zero exit, remote ref still X. Refute: remote ref moves.
Hypothesis W1c: a lease naming a nonexistent SHA for the absent topic `w1b`
is rejected. Confirm: non-zero exit and `w1b` still 404. Refute: `w1b` created.
Observation O-W1: the identical push repeated (exit code and porcelain line),
and whether any hook or credential helper ran (`GIT_TRACE=1` to a file, rule 10).

Steps in a scratch clone with a commit on top of `main`: read the ref (expect
404), push (W1a), repeat (O-W1), advance the ref with a SHA lease and push the
other commit with the empty lease (W1b), push `w1b` with the nonexistent-SHA
lease (W1c). Every later experiment needs W1a confirmed; otherwise stop.

### W2 PR create and duplicate behaviour

Hypothesis W2a: `POST repos/wstein/stw-forge-fixture/pulls` with `title`, `body`,
`head` (`m016/RUN/w1`), `base` (`main`), `draft` (`true`) creates one Draft PR.
Confirm: status 201, `draft == true`, `state == open`, every section 5 field
present. Refute: any other status, or `draft == false`.
Hypothesis W2b: an identical second POST creates nothing and fails with a
client error, and `GET pulls?head=wstein:BRANCH&state=open` still lists exactly
one PR. Confirm: 4xx and one listed PR. Refute: 2xx or two PRs.
Hypothesis W2d: the same POST with `draft` (`false`), head `m016/RUN/w2d`,
creates a non-Draft open PR. Confirm: 201 and `draft == false`. Refute: Draft.
Hypothesis W2e: a POST with head `m016/RUN/w2e` (a pushed branch with no open
PR, so a duplicate cannot be the cause) and a nonexistent base creates nothing. Confirm: 4xx and
no new PR. Refute: a PR exists.
Observation O-W2: a POST with head `m016/RUN/w2o` (a pushed branch with no open
PR) and one made-up body field `m016_unknown` (no labels
or reviewers are ever sent): record whether it is ignored or rejected.

### W3 Own-body edit and conditional write

Hypothesis W3a: `PATCH pulls/N` with only `body` changes `body` and
`updated_at` and no other section 5 field. Confirm exactly that. Refute: any
other field differs.
Hypothesis W3b: REST has no compare-and-swap: a PATCH carrying a stale
`If-Match` (the before-read ETag after a first edit) is applied. Confirm: 2xx
and the body changed. Refute: 412 or 409 and the body unchanged. If confirmed,
the digest guard of `contracts/forge-effects.md` line 73 is a client-side
read-before-write with a residual race window.
Observation O-W3: the status for a 65537-byte body on the `w1` PR (the plan
bound is 64 KiB, `contracts/forge-effects.md` line 178; GitHub's limit is not
assumed); the body is restored afterwards.

### W4 Retarget

Hypothesis W4a: `PATCH pulls/N` with only `base` (draft PR `w4`, base `main` to
base `m016/RUN/w1`) changes `base.ref` and `base.sha` and no other section 5
field except `mergeable`, `mergeable_state`, `rebaseable` and `updated_at`.
Confirm exactly that, including `head.sha` unchanged. Refute: `head.sha`,
`state`, `draft` or the body differ.
Hypothesis W4b: the same base-only PATCH works on the non-Draft PR `w2d` and
leaves `draft == false`. Confirm: 200, base changed, `draft` false. Refute: any
other change or a refusal.
Hypothesis W4c: a retarget to a nonexistent branch fails and leaves the base.
Confirm: 4xx and base unchanged. Refute: base changed.
Observation O-W4: a retarget to the current base (status and whether
`updated_at` changed).
Hypothesis W4d (narrowed): no section 5 field outside base, the computed merge
fields and `updated_at` moved in W4a to W4c. This is the head-unchanged rule of
`contracts/forge-effects.md` lines 166-168; CI status is not testable here
(the fixture has no workflows).

### W5 Ready

Hypothesis W5a: the REST update (`PATCH pulls/N` with `draft=false`) does not
leave Draft state. Confirm: 2xx or 4xx with `draft` still true. Refute:
`draft == false`; the REST triple is then the Ready candidate. If W5a is
CONFIRMED it is a recorded outcome, not a stop: Ready then has no REST form.
The owner decision of 2026-10-25 (section 1) resolves the three former choices:
stw may use exactly ONE fixed GraphQL mutation, `markPullRequestReadyForReview`
(variable: the pull request node id only), through a fixed query string, as its
own effect class, validated, with no other GraphQL; `ready` is not left
`capability-unavailable` and no other path is sought. W5a itself still sends no
GraphQL and tries no alternative; W5b and O-W5 run under the released G3, and N1
to N5 follow their own precondition (section 8). Consequence for the spec
files: this PR adds the exception clause to `research.md` lines 139 and 192,
`contracts/owner-decisions.md` line 212, `contracts/forge-effects.md` line 101
and `tasks.md` line 50 (T021). The `ready` row of `contracts/forge-effects.md`
(line 76) still says the method is unmeasured and `contracts/` needs the
fixed-mutation edge, fields and tests in the Spec 016 implementation PR; no
checkbox is ticked here.
Hypothesis W5b (needs G3): `gh pr ready N -R wstein/stw-forge-fixture` on a
Draft PR sets `draft == false`; the requests it sends are recorded to a trace
file (rule 10) as method, path and operation name only. Confirm: exit 0,
`draft == false`, no other section 5 field changed except computed ones.
Refute: still Draft or another field changed. This observes what exists; it
does not adopt a transport, and no hand-written GraphQL is sent. It also
records, by name only, whether the trace shows the operation or field
`markPullRequestReadyForReview`; that is the fixture evidence for the fixed
form (an absent name is recorded as such).
Observation O-W5 (needs G3): the same command on an already-Ready PR (exit code
and message).

The ordering gates of Ready (CLEAR, CI, `rebaseable`) are policy, not forge
behaviour, and are not measured.

## 8. OQ-NATIVE experiments (all need G3)

Precondition: W2 and W4 confirmed. `gh stack` is used only if already installed
(extension version recorded); no installation or upgrade. If it refuses to run
without a Git repository, record that and retry in a scratch clone of the
fixture after the Git guard (rule 4), never in a workspace checkout.

### N0 Reference capture (read only)

Record `gh extension list` and the full `--help` of `gh stack`, `gh stack link`
and `gh stack unstack`. The help observed locally on 2026-10-25 (extension
v0.2.1; to be re-recorded, not trusted): link takes branches, PR numbers or URLs;
a numeric argument that is not an existing PR is treated as a branch name;
branch arguments are pushed first; a first numeric argument is a stack number
only when it matches an existing stack; arguments already in the stack are
skipped; `--base` sets the bottom base; `--open` marks PRs ready; `unstack`
takes a stack number, leaves stacked the PRs queued for merge or with
auto-merge, and `--local` only drops local tracking. `--open` is never run.

### N1 Numeric link of two PRs

Setup: PRs `n1` (base `main`) and `n2` (base `m016/RUN/n1`). Number guard,
rule 5. Command: `GH_REPO=wstein/stw-forge-fixture gh stack link --base main
N1 N2` (numbers of PRs `n1`, `n2`), traced to a file.
Hypothesis N1a: it creates one stack of two, pushes no branch, leaves every
section 5 field of both PRs unchanged and leaves both Draft. Confirm: exit 0,
`git ls-remote` unchanged, fields unchanged. Refute: a ref changes, a field
changes, or a PR leaves Draft.
Hypothesis N1b: the trace reveals the stack write endpoint (method, path, body
field names) and a read path for membership. Confirm: both found. Refute: no
read path found; then record "no stack read evidence" and skip N2 to N5 (no
guessing of endpoints), which is a documented stop with cleanup.
Observation O-N1: the stack number and whether it equals any PR number.
N1 measures a two-member stack, the shape stw may publish (`spec.md` lines 78-81).

### N2 Idempotency

Hypothesis N2: repeating the identical command changes nothing. Confirm: exit 0
or a no-change message, same membership, one stack. Refute: a second stack or a
membership change.

### N3 Growth and order (three members: a shape stw refuses)

The third open layer is measured as native behaviour only; stw blocks depth over
two open layers (`spec.md` lines 80-81), so only the two-member results feed T024.
Order: N3b first, then N3a.
Hypothesis N3b: the stack-number form (`gh stack link STACK N3`, on the stack
of two from N1 that does not yet contain N3) appends `n3`. Confirm: three
members in order, none lost. Refute: rejected, or a member lost or reordered.
Hypothesis N3a: then `gh stack link --base main N1 N2 N3` on the stack of three
is idempotent. Confirm: same three members in order, one stack. Refute: a member
lost or reordered, or a second stack.
Hypothesis N3c: reversed input `N2 N1` on the existing stack is skipped as
already in the stack (the help predicts this). Confirm: membership and order
unchanged. Refute: reordered or an error.

### N4 Retarget a linked member

Retarget `n3` to `main` by W4's PATCH, then read the stack view.
Hypothesis N4: membership stays consistent after the base edit. Confirm:
membership unchanged and no error. Refute: GitHub drops, breaks or rewrites
membership. Either result is recorded; it is the nearest non-merge probe of the
rule that stw must not race a native rewrite (`spec.md` line 80).

### N5 Unstack

`GH_REPO=wstein/stw-forge-fixture gh stack unstack STACK` (explicit number).
Hypothesis N5a: the stack disappears while PRs stay open with unchanged base,
head and state, and no branch is deleted. Confirm those. Refute: any PR closed,
changed or branch removed.
Observation O-N5: a second unstack of the same number (error text and exit).
`unstack` without an argument and `--local` are never run.

## 9. Owner-executed probes (separate run id, separate owner go)

Merging, rebase-merge and settings stay with the owner. They run after N5, on a
fresh stack with real commits prepared by the same N1 command (a further agent
write, within G3), under their own run id. The agent only records the section 5
set before and after; the owner merges and flips any setting.

- M1: rebase-merge of the bottom PR with `delete_branch_on_merge` as recorded in
  P0. Hypothesis: GitHub rewrites and retargets a native stack member (child
  base follows the new default tip, child head SHA changes). Confirm: child
  `base.ref == main` and `head.sha` changed. Refute: child unchanged, closed, or
  base left on the merged branch. Relates to `research.md` lines 25-27 and
  `contracts/receipts.md` lines 210-213.
- M2: the same with the opposite `delete_branch_on_merge` value (the owner flips
  it). Confirm and refute as M1; closes the gap that only one value is measured.
- M3: retarget the child first (W4), then merge the bottom
  (`contracts/receipts.md` line 211 order). Confirm: child base `main` after the
  merge and head unchanged by GitHub. Refute: child closed or head rewritten.

M1 to M3 are optional and are not needed for the OQ-WRITES allowlist.

## 10. Recording format

Evidence goes in `specs/016-stacked-pr-coordination/evidence/`, one file per
run, `measurement-RUN.md`, committed by a separate evidence PR after the run
and never edited later (a correction is a new file). Structure:

1. Header: run id, UTC start and end, fixture, operator agent type, the
   reviewed PR head SHA of this plan AND the `main` commit that contains the
   plan with an identical tree for this file, the gates G1 to G4 with their
   evidence, `gh`, `git` and `gh stack` versions, `main` SHA at start and end,
   requests used, rate limit before and after.
2. One section per experiment id: the hypothesis text copied from this plan, a
   table with step, argv or `METHOD PATH` plus body fields, exit or HTTP status,
   before and after (selected fields), result (`confirmed`, `refuted`,
   `recorded`, `inconclusive`, `not-run`) and note, then raw JSON excerpts
   trimmed with `jq` to section 5 fields, at most 8 KiB each.
3. Deviations: every departure from the plan, abort, stop and cleanup action;
   the list of `m016/RUN/*` branches, PR numbers and stacks left behind.
4. Verdict table: one line per hypothesis, no interpretation beyond the result.

Sample output in evidence files is pasted from real output only.

## 11. What the results unblock (and what they do not)

An accepted evidence file (independent review of the evidence PR, then the
owner's acceptance of the allowlist proposal) may unblock, never tick by itself:

| Result | Unblocks |
| --- | --- |
| W2 | `create-pr` row of `contracts/forge-effects.md` line 72; `tasks.md` T020, T021, T023 (create part) |
| W3 | `edit-body` row (`contracts/forge-effects.md` line 73); T021, T023, T022 raced-body test design |
| W4 | `retarget` row (`contracts/forge-effects.md` line 75); T021, T023, T022 |
| W5 | `ready` row (`contracts/forge-effects.md` line 76); T031 (Ready wiring through the admitted edge, `tasks.md` line 68) and T021 for the fixed Ready mutation if W5a confirms that REST cannot set Ready (owner decision, section 1); T029 and T030 are policy tests and are NOT unblocked by measurement |
| W1 | absent-ref creation part of T019 (`contracts/forge-effects.md` lines 144-146); the hook and credential boundary stays unmeasured |
| N1 to N5 | `native-link` / `native-unstack` row (`contracts/forge-effects.md` line 77); T024 (two-member results only); the native link/unstack no-write sentinel fixtures of T034 (`tasks.md` line 75), not its jj refusal part; native lines of `quickstart.md` section 3 |
| M1 to M3 | the unmeasured on/unreadable row (`contracts/receipts.md` line 211) for the measured setting and shape only; T032 landing-handoff fixtures (`tasks.md` line 69) conditionally |

Native jj publication and the Git push hook / credential-helper confinement
(`contracts/forge-effects.md` lines 129-137) are not unblocked.

## 12. Explicitly NOT measured

- Rulesets, branch protection, repository settings, secrets, webhooks,
  environments, collaborators, visibility, repository deletion or transfer:
  owner only; P0 reads them, no step writes them. Flipping
  `delete_branch_on_merge` (M2) is the owner's.
- Tags, releases, `main`, any push to `main`, auto-merge, review submission, CI
  reruns, labels, reviewers, assignees, comments, notifications.
- Any merge by the agent, landing, fast-forward push of a PR head to `main`,
  remote branch deletion by the agent, and Spec 043 policy.
- Any repository other than the fixture; forks; cross-repository PRs; the
  owner's workspace repositories.
- Native jj publication and colocated jj; extension installation or upgrade; raw
  hand-written GraphQL (the fixed Ready mutation is an stw implementation
  decision, not a measurement); arbitrary REST endpoints beyond those listed in
  sections 7 and 8 (stack endpoints are only observed through the command's own
  trace in N1).
- Git hook, credential-helper and signing-helper behaviour (separate gate).
- Whether results hold in another repository, with rulesets or protected
  branches (the fixture has none); every result is scoped to the recorded
  fixture shape.
