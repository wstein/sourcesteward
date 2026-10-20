# Merged-PR discovery contract

Subordinate to the [specification](../spec.md) FR-013..FR-018 and to Spec 029's [online contract](../../029-tidy-dogfood-safety/contracts/online.md), whose authentication, redirect, compression, byte and deadline rules apply unchanged.

## Endpoint set

Closed set, all `GET`, exact allow-listed routes (add or remove only together with the transport route test):

1. `repos/OWNER/REPO` (identity);
2. `repos/OWNER/REPO/pulls?state=closed&per_page=100&page=N`, `N` from 1 to 10 (candidate collection, at most 1,000 records);
3. `repos/OWNER/REPO/pulls/NUMBER` (full read of each chosen PR);
4. `repos/OWNER/REPO/branches?per_page=100&page=N` (existing tracking-ref feature, unchanged).

No search endpoint, commit-association endpoint or GraphQL is added by this Spec. No filters by head or base name.

## Algorithm

1. Collect distinct unproven tip OIDs from the selected refs (after forge-free proofs). Zero OIDs: no request.
2. For each OID without a hint, collect candidates from the closed collection; with a hint, read that PR directly.
3. Re-read each chosen candidate (endpoint 3) and apply Spec 029 FR-007 tuple validation.
4. Emit one DiscoveryRecord per OID listing all refs with that OID.

## Request budget

Shared with Spec 029 FR-025: 30-second online plan deadline, at most 128 requests and 32 MiB raw response bytes per run, shared with tracking-ref reads, failures counted. Accounting (formula and source citations in spec FR-018): fixed repository reads 2, at most 10 page reads, one PR read per candidate, so at most (128 - 2 - 10) / 1 = 116 candidate reads fit. `RevalidatePullRequest` pairs (repository plus PR, `tidy_evidence_pr.go:159-173`) run at apply inside the apply budget, outside this plan budget. Candidate reads run in ascending OID order; when the remaining budget cannot cover the next candidate PR read, that OID and all later OIDs are `discovery-budget-exhausted` (retained) and the plan completes with a note. The 60-second forge-free proof budget is separate, runs first and does not extend the online deadline.

## Hint rules

| Input | Result |
| --- | --- |
| `--pr OID=N` twice, identical (same number however spelled, `07` and `7`) | accepted once |
| same OID, different N | usage error: `--pr OID=N1` conflicts with `--pr OID=N2` for OID; names both selectors |
| OID not a tip of any selected ref | usage error naming the OID |
| malformed OID or N | usage error naming the full flag value (escaped, at most 4,096 bytes) |
| hinted PR that is not an admissible merged head | `discovery-incomplete` (a hint skips the collection, so no absence is claimed) |
| multiple admissible PRs, no hint | `ambiguous-needs-hint` naming the OID and refs |

## Coverage

`complete` when pagination terminates within 10 pages and 1,000 records; otherwise `incomplete`. This amends Spec 029's refusal of a full final page without proven exhaustion: positive evidence (an exact-head merged PR found and fully verified) stays proof; only absence is inconclusive, so OIDs without a found PR are retained `discovery-incomplete` and the plan never refuses for this reason.

## Stored discovery list

The plan stores one `DiscoveryRecord` per distinct unproven OID in `online.discovery` (additive, optional, absent for plans without provider discovery). It is informational and never makes a plan refuse: at most 256 records, 512 refs in total and 128 KiB (a fixed allowance per record plus the ref names), all outside the 1,024 aggregate-entry bound of Spec 029. Records beyond a cap are dropped in ascending OID order and a `discovery-records-truncated` note is added; the `limitations` notes for undecided results are built from the full set. The v3 schema uses the same caps (256 records, 512 refs per record).
