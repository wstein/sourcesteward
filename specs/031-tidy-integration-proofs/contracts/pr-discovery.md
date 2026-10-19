# Merged-PR discovery contract

Subordinate to the [specification](../spec.md) FR-013..FR-018 and to Spec 029's [online contract](../../029-tidy-dogfood-safety/contracts/online.md), whose authentication, redirect, compression, byte and deadline rules apply unchanged.

## Endpoint set

Closed set, all `GET`, exact allow-listed routes (add or remove only together with the transport route test):

1. `repos/OWNER/REPO` (identity);
2. `repos/OWNER/REPO/pulls?state=closed&per_page=100&page=N`, `N` from 1 to 10 (candidate collection, at most 1,000 records);
3. `repos/OWNER/REPO/pulls/NUMBER` (full re-read of each chosen PR);
4. `repos/OWNER/REPO/branches?per_page=100&page=N` (existing tracking-ref feature, unchanged).

No search endpoint, commit-association endpoint or GraphQL is added by this Spec. No filters by head or base name.

## Algorithm

1. Collect distinct unproven tip OIDs from the selected refs (after forge-free proofs). Zero OIDs: no request.
2. For each OID without a hint, collect candidates from the closed collection; with a hint, read that PR directly.
3. Re-read each chosen candidate (endpoint 3) and apply Spec 029 FR-007 tuple validation.
4. Emit one DiscoveryRecord per OID listing all refs with that OID.

## Request budget

Shared with Spec 029 FR-025: 30-second online plan deadline, at most 128 requests and 32 MiB raw response bytes per run, shared with tracking-ref reads, failures counted. Accounting: 1 repository read, at most 10 collection pages, one re-read per chosen PR. Re-reads run in ascending OID order; when the remaining budget cannot cover the next re-read, that OID and all later OIDs are `discovery-budget-exhausted` (retained) and the plan completes with a note. The 60-second forge-free proof budget is separate, runs first and does not extend the online deadline.

## Hint rules

| Input | Result |
| --- | --- |
| `--pr OID=N` twice, identical | accepted once |
| same OID, different N | usage error: `--pr OID=N1` conflicts with `--pr OID=N2` for OID; names both selectors |
| OID not a tip of any selected ref | usage error naming the OID |
| malformed OID or N | usage error naming the full flag value (escaped, at most 4,096 bytes) |
| multiple admissible PRs, no hint | `ambiguous-needs-hint` naming the OID and refs |

## Coverage

`complete` when pagination terminates within 10 pages and 1,000 records; otherwise `truncated`. Truncation never fails the plan and never claims absence.
