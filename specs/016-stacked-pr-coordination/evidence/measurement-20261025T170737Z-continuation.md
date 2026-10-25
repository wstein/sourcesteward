# Measurement run 20261025T170737Z, continuation

Continuation of run `20261025T170737Z` under section 13 of
`specs/016-stacked-pr-coordination/measurement-plan.md`. The first part is
`measurement-20261025T170737Z.md` (never edited). Counts, names, statuses and
field values only; no token, header value or trace line is recorded.

## 1. Header

- UTC start 2026-10-25T18:44:48Z, end 2026-10-25T18:49:33Z. Fixture
  `wstein/stw-forge-fixture`. Operator agent type: Claude.
- Plan text: `main` at `089fa6f246a113307f53dde4f9ffa9d57f879952`, which
  contains the amendment merged as `3b93e229` (pull request 275). The tree of
  that merge equals the amendment head
  `1932fc5b575b3fb0afc3a3e768ab559905185d09` (empty diff checked). Pull
  request 274 (first part) is merged.
- Gates: G1 the amendment head `1932fc5b` is recorded CLEAR in the foreman
  ledger (`wstein/sourcesteward` 275 `1932fc5b`: Opus delta rounds and a
  middle-tier CLEAR). No GitHub review object exists; what was checked here is
  only that the merged tree equals that head, the review itself rests on the
  ledger. G2 is satisfied by the owner decisions of 2026-10-25 (P0 403
  accepted, request cap 300, the continuation planned and merged through pull
  request 275) as relayed by the foreman, who instructed the start after pull
  requests 274 and 275 were merged; there is no separate owner go message for
  the continuation run itself. G3 released by the owner for the fixture. G4
  the API guard passed before every write; no git push happened in this part
  (the nine branches were reused), so the Git guard had no push to cover.
- Versions: `gh` 2.102.0, `git` 2.56.0, `gh stack` v0.2.1.
- Fixture `main` at start and end: `0e17a3a74e110ecf935a0a8609cdea85e6c4820e`.
- Requests: cumulative counter 293 of 300 (104 in the first part). The
  counter charges every call, including guards; it overcounts (see the first
  file). N3c began at 257 (below the stop) and the experiments ended at 265,
  about 5 requests past the stop of about 260 (rule 9), within the reserve of
  40; cleanup used the rest (293 of 300). Core rate limit remaining: 4816
  after the P0 re-run, 4751 at the end (other users of the same token also
  draw on it; not separable).

## 2. Experiments

PRs created in this part (all by the measured create form of W2a/W2d, `POST
pulls` with `draft`): 4 (`w1`, Draft), 5 (`w2d`, non-Draft), 6 (`w4`, Draft),
7 (`w5`, Draft), 8 (`n1`), 9 (`n2`, base `m016/RUN/n1`), 10 (`n3`, base
`m016/RUN/n2`), all Draft except 5, bases as stated, each with the section 5
read after creation (`absent` empty) and the ref read before. The stack number
is 11.

### P0 re-run (read only)

Private true; `rulesets`, `rules/branches/main` and `branches/main/protection`
each answered 403; `protected` false; trace scan: 1 request header line, 0
unmasked, 0 token hits, 0 `github_pat_` hits; rate limit 4816; `main` as
expected (`0e17a3a7`); the nine branches present (SHAs below); `w1b` 404;
PRs 1 to 3 closed, no open PR. Result: `recorded`.

```text
bd099b2414897e9a56d31d679df7156ab3916834 n1
92ba5159873e86043feec632ce2222fd16b9e531 n2
e64e6def9115820e7d28c51b4961aa373906527e n3
d8580ef2042692e00755278765097cb8ce2fa9bd w1
ecbec59b1826180c7febe9f9e4f74f1b61383e1e w2d
df2ad8fc2454027718e5d3331f15f1da40a5e49d w2e
08a702b4a77228b01cde702a404221a56cf3f4f1 w2o
54667bb134edd9488822d1bb81ca830b5c9b08da w4
fd7183b967ea3ac4452c77eceebc65557c59e020 w5
```

### W3b (repeated, PR 4)

Read PR 4 and its ETag (strong), first body edit (200, body length 20 to 21,
digest `5cd72dedeafc` to `643c852ebf89`), then a second edit with
the stale ETag as `If-Match`: HTTP 400 "Bad Request"; re-read: body still the
first edit. Result: `refuted` (400 is refute-class by the plan, section 7):
REST has no conditional body write through `If-Match`; the body is unchanged.
Both runs of W3b answered 400. Not "applied", so the confirm outcome (stale
`If-Match` applied) did not occur: the digest guard remains a client-side
read-before-write.

### O-W3

`PATCH pulls/4` with a body of 65537 bytes: HTTP 200, stored length 65537
(re-read). The body was restored to the 21-byte edit (200, length 21).
Result: `recorded` (no 65536 limit observed at this size).

### W4 (PRs 6 and 5)

| Id | Request | Status | Changed fields (before to after) |
| --- | --- | --- | --- |
| W4a | `PATCH pulls/6` `base=m016/RUN/w1` (was `main`) | 200 | `base.ref`, `base.sha` (`0e17a3a7` to `d8580ef2`), `updated_at`; `head.sha`, `state`, `draft`, body and the computed fields unchanged |
| W4b | `PATCH pulls/5` the same base | 200 | the same three fields; `draft` stays false, `head.sha` `ecbec59b` unchanged |
| W4c | `PATCH pulls/6` `base=m016/RUN/nonexistent` | 422 (`base` invalid) | none |
| O-W4 | `PATCH pulls/6` to its current base | 200 | none (`updated_at` unchanged) |

W4a, W4b, W4c `confirmed`; O-W4 `recorded`; W4d `confirmed` (outside base, the
computed merge fields and `updated_at`, no section 5 field moved in W4a to W4c;
the computed fields did not move either).

### W5 Ready (PR 7)

- W5a: `PATCH pulls/7` with `draft=false`: HTTP 200, `draft` stays `true`,
  `state` open. Result: `confirmed` (REST cannot set Ready). No GraphQL and no
  alternative was tried by this step.
- W5b: `gh pr ready 7 -R wstein/stw-forge-fixture` (traced with `GH_DEBUG=api`
  to a file, scan clean: 2 header lines, 0 unmasked, 0 token hits): exit 0,
  `draft` false; between the reads only `draft` and `updated_at` changed.
  Requests in the trace: two `POST /graphql`: a query named
  `PullRequestByNumber` and a mutation named `PullRequestReadyForReview`
  containing the field `markPullRequestReadyForReview`, with an `input` object
  holding `pullRequestId`. Result: `confirmed`; the name
  `markPullRequestReadyForReview` is PRESENT in the trace (fixture evidence for
  the fixed form; nothing was sent by hand).
- O-W5: the same command on the now-Ready PR: exit 0, message `Pull request
  wstein/stw-forge-fixture#7 is already "ready for review"`; one `POST
  /graphql` (the lookup), nothing changed (`updated_at` same). Result:
  `recorded`.

### N0

`gh extension list`: `gh stack github/gh-stack v0.2.1`. The help of `gh stack`,
`link` and `unstack` was captured locally (not committed) and matches the plan's
summary: `link` takes branches, PR numbers or URLs, `--base`, `--open`,
`--remote`; a numeric first argument is a stack number only when it matches an
existing stack; `unstack` takes a stack number, `--local` only drops local
tracking. `--open` and `--local` were never used. `gh stack` ran from an empty
directory that is not a Git repository, with `GH_REPO` set; it did not need a
repository. Result: `recorded`.

### N1 (PRs 8 and 9)

Number guard (rule 5): `GET pulls/8` and `pulls/9` showed state open and the
expected heads. `GH_REPO=wstein/stw-forge-fixture gh stack link --base main 8 9`
(traced): exit 0, no output on stdout. Trace requests: `GET
/repos/wstein/stw-forge-fixture/stacks?per_page=100&page=1`, four `POST
/graphql` (query `FindPRByNumber`), then `POST
/repos/wstein/stw-forge-fixture/stacks` with the body field `pull_requests`
(`[8, 9]`), HTTP 201. After: `git ls-remote` of `refs/heads/m016/RUN/*`
identical to before; the section 5 reads of PRs 8 and 9 equal their reads after
creation (both Draft); one stack, number 11, members 8 and 9 in order, base
`main`.
- N1a `confirmed`. N1b `confirmed`: write endpoint `POST
  /repos/{r}/stacks` (body `pull_requests`), read path `GET /repos/{r}/stacks`
  (list; each entry has `id`, `number`, `base.ref`, `open`, `created_at`,
  `pull_requests[]` with `number`, `state`, `draft`, `merged_at`, `head.ref`,
  `head.sha`).
- O-N1 `recorded`: the stack number is 11; it equals no existing PR number
  (PRs are 1 to 10; stack and PR numbers appear to share one sequence).

### N2

Number guard (rule 5): `GET pulls/8` and `pulls/9`, both open. The identical
command: exit 0; trace: the `GET .../stacks` and four `POST
/graphql`, no write request; one stack (11), same members. Result: `confirmed`.

### N3 (PR 10)

- N3b: `link 11 10` (stack number first; read-back: stack 11 exists, PR 10
  open): exit 0; trace: `GET .../stacks`, two `POST /graphql`, `POST
  /repos/{r}/stacks/11/add` (HTTP 200). Members 8, 9, 10 in order. `confirmed`.
- N3a: number guard: one read of the open PR list showed 4 to 10 open.
  `link --base main 8 9 10`: exit 0, no write request in the trace,
  members unchanged, one stack. `confirmed`.
- N3c: number guard: one read of the open PR list showed 4 to 10 open.
  `link --base main 9 8` (reversed): exit 5, message `Cannot update stack:
  this would remove #10 from the stack`, `Current stack: #8, #9, #10`, `Include
  all existing PRs in the command to update the stack`; membership and order
  unchanged. Result: `refuted` by the plan (an error), not skipped as the help
  predicted; the stack was not rewritten.

### N4 and N5

Not run: N3c began at 257 and ended at 265, past the stop of about 260 for
experiments (rule 9); an expected budget stop. See D1 for what the cleanup
unstack shows.

## 3. Deviations, stop and cleanup

- D1: STOP of the experiments at the budget (265 counted, 5 past the stop of
  about 260, within the reserve; N4, N5, O-N5 not run).
  Cleanup step 1 then ran `GH_REPO=wstein/stw-forge-fixture gh stack
  unstack 11` (explicit number): exit 0, trace `POST
  /repos/{r}/stacks/11/unstack`, HTTP 204, message `Stack removed on GitHub
  (stack #11)`. This is cleanup, not the N5 experiment (no before/after set
  beyond this): `GET stacks` returned an empty list afterwards, the seven open
  PRs kept state, draft, base and head (list read before and after identical),
  the nine branches remained. It is evidence for N5a only in that sense and is
  not counted as a verdict. A second unstack (O-N5) was not run.
- D2: the cleanup read back the open PR list once (numbers and `m016/RUN/` head
  refs) instead of one read per close; then PRs 4 to 10 were closed with `PATCH
  state=closed` (each 200) under the API guard; the `mergeable` re-reads were
  skipped as the plan says. Final: no open PR, no stack, `main` unchanged.
- D3: the section 5 before/after pairs of W4b, W4c and O-W4 were taken from the
  reads of the run (compared by field; only differences are quoted). The
  hypothesis texts and the per-request ETag/rate-limit values are not copied
  (as in D5 of the first file); the run log and raw reads are not committed.
- D4: two of my own helper redirects wrote the reads of W4b and W4c to the
  wrong file; the reads were taken from the timestamped copies the helper keeps;
  no extra request was made.
- Branches left for the owner (nine, none deleted):
  `m016/20261025T170737Z/{w1,w2d,w2e,w2o,w4,w5,n1,n2,n3}`. Pull requests 1 to
  10 are closed; no stack exists. The fixed GraphQL mutation was not run by
  hand. M1 to M3 were not run.

## 4. Verdicts

| Id | Result |
| --- | --- |
| P0 (re-run) | recorded |
| W3b | refuted (HTTP 400, body unchanged) |
| O-W3 | recorded (65537 bytes accepted, 200) |
| W4a | confirmed |
| W4b | confirmed |
| W4c | confirmed |
| O-W4 | recorded |
| W4d | confirmed |
| W5a | confirmed (REST cannot set Ready) |
| W5b | confirmed (`markPullRequestReadyForReview` present in the trace) |
| O-W5 | recorded |
| N0 | recorded |
| N1a | confirmed |
| N1b | confirmed |
| O-N1 | recorded (stack 11) |
| N2 | confirmed |
| N3b | confirmed |
| N3a | confirmed |
| N3c | refuted (error, exit 5, nothing changed) |
| N4 | not-run (budget) |
| N5a | not-run (budget; see D1) |
| O-N5 | not-run |
